package runner

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// pathSet is a list of include and exclude (!) patterns from a step's
// multi-line path input, made absolute. Patterns use glob syntax: *
// matches within one directory, ** across directories, ? one character
// and [..] a character class. A pattern matching a directory matches
// everything in it.
type pathSet struct {
	includes []string
	excludes []string
}

// parsePaths reads a path input: one pattern per line, relative to the
// workspace, ~ for the home directory, # comments.
func parsePaths(input, workspace, home string) (pathSet, error) {
	var ps pathSet
	for _, line := range strings.Split(input, "\n") {
		p := strings.TrimSpace(line)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		neg := strings.HasPrefix(p, "!")
		p = strings.TrimSpace(strings.TrimPrefix(p, "!"))
		switch {
		case p == "~" || strings.HasPrefix(p, "~/"):
			p = home + p[1:]
		case !path.IsAbs(p):
			p = path.Join(workspace, p)
		}
		p = path.Clean(p)
		if _, err := globRe(p); err != nil {
			return ps, fmt.Errorf("invalid path pattern %q: %w", line, err)
		}
		if neg {
			ps.excludes = append(ps.excludes, p)
		} else {
			ps.includes = append(ps.includes, p)
		}
	}
	if len(ps.includes) == 0 {
		return ps, errors.New("path doesn't list any files (only exclusions or nothing)")
	}
	return ps, nil
}

func hasGlob(s string) bool { return strings.ContainsAny(s, "*?[") }

// staticRoot is the longest leading part of a pattern without glob
// characters: the directory to search.
func staticRoot(p string) string {
	if !hasGlob(p) {
		return p
	}
	parts := strings.Split(p, "/")
	var out []string
	for _, seg := range parts {
		if hasGlob(seg) {
			break
		}
		out = append(out, seg)
	}
	r := strings.Join(out, "/")
	if r == "" {
		return "/"
	}
	return r
}

// globRe compiles a path pattern.
func globRe(p string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '*' && strings.HasPrefix(p[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case c == '*' && strings.HasPrefix(p[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[':
			j := strings.IndexByte(p[i:], ']')
			if j < 0 {
				return nil, errors.New("unclosed [")
			}
			class := p[i+1 : i+j]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + class + "]")
			i += j
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// matcher matches absolute paths against a pattern, including everything
// below a matching directory.
type matcher struct {
	literal string
	re      *regexp.Regexp
}

func newMatcher(p string) matcher {
	if !hasGlob(p) {
		return matcher{literal: p}
	}
	re, _ := globRe(p)
	return matcher{re: re}
}

func (m matcher) match(full string) bool {
	if m.re == nil {
		return full == m.literal || strings.HasPrefix(full, strings.TrimSuffix(m.literal, "/")+"/")
	}
	for p := full; ; p = path.Dir(p) {
		if m.re.MatchString(p) {
			return true
		}
		if p == "/" || p == "." {
			return false
		}
	}
}

// collect walks the static roots of the include patterns in env and calls
// fn for every matching entry (directories, regular files, symlinks) not
// excluded, with its content readable from r for regular files. Roots that
// don't exist are skipped. hidden controls whether entries with a path
// component starting with "." below a root are kept.
func collect(ctx context.Context, env Env, ps pathSet, hidden bool, fn func(full string, hdr *tar.Header, r io.Reader) error) (found int, err error) {
	roots := map[string][]matcher{}
	var order []string
	for _, p := range ps.includes {
		r := staticRoot(p)
		if _, ok := roots[r]; !ok {
			order = append(order, r)
		}
		roots[r] = append(roots[r], newMatcher(p))
	}
	sort.Strings(order)
	var excl []matcher
	for _, p := range ps.excludes {
		excl = append(excl, newMatcher(p))
	}
	seen := map[string]bool{}
	for _, root := range order {
		rc, err := env.CopyOut(ctx, root)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return found, err
		}
		parent := path.Dir(root)
		tr := tar.NewReader(rc)
		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				rc.Close()
				return found, err
			}
			full := path.Join(parent, path.Clean("/" + hdr.Name)[1:])
			if seen[full] {
				continue
			}
			ok := false
			for _, m := range roots[root] {
				ok = ok || m.match(full)
			}
			for _, m := range excl {
				if m.match(full) {
					ok = false
				}
			}
			if ok && !hidden && full != root {
				rel := strings.TrimPrefix(full, strings.TrimSuffix(root, "/")+"/")
				for _, seg := range strings.Split(rel, "/") {
					if strings.HasPrefix(seg, ".") {
						ok = false
					}
				}
			}
			if !ok {
				continue
			}
			seen[full] = true
			if hdr.Typeflag == tar.TypeLink {
				// Hard links name their target relative to the archive root.
				hdr.Linkname = path.Join(parent, path.Clean("/" + hdr.Linkname)[1:])
			}
			if hdr.Typeflag == tar.TypeReg {
				found++
			}
			if err := fn(full, hdr, tr); err != nil {
				rc.Close()
				return found, err
			}
		}
		if err := rc.Close(); err != nil {
			return found, err
		}
	}
	return found, nil
}

// commonRoot is the directory artifact paths are made relative to, like
// upload-artifact: the least common ancestor of the search roots, where a
// literal file's root is its directory.
func commonRoot(ctx context.Context, env Env, ps pathSet) string {
	var roots []string
	for _, p := range ps.includes {
		r := staticRoot(p)
		if !hasGlob(p) {
			if isFile(ctx, env, r) {
				r = path.Dir(r)
			}
		}
		roots = append(roots, r)
	}
	lca := roots[0]
	for _, r := range roots[1:] {
		for lca != "/" && r != lca && !strings.HasPrefix(r, lca+"/") {
			lca = path.Dir(lca)
		}
	}
	return lca
}

func isFile(ctx context.Context, env Env, p string) bool {
	code, err := env.Exec(ctx, ExecOptions{Cmd: []string{"test", "-f", p}, Output: discard{}})
	return err == nil && code == 0
}
