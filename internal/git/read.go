package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Ref is a branch or tag.
type Ref struct {
	Name   string    `json:"name"`
	Commit string    `json:"commit"`
	Date   time.Time `json:"date"`
	// Subject is the target commit's first line.
	Subject string `json:"subject"`
	Author  string `json:"author"`
}

// Branches lists branches, most recently committed first.
func (r *Repo) Branches(ctx context.Context) ([]Ref, error) {
	return r.refs(ctx, "refs/heads/")
}

// Tags lists tags (peeled to commits), newest first.
func (r *Repo) Tags(ctx context.Context) ([]Ref, error) {
	return r.refs(ctx, "refs/tags/")
}

func (r *Repo) refs(ctx context.Context, prefix string) ([]Ref, error) {
	// For annotated tags %(*objectname) is the peeled commit.
	format := "%(refname)%00%(objectname)%00%(*objectname)%00%(committerdate:iso-strict)%00%(*committerdate:iso-strict)%00%(contents:subject)%00%(authorname)%00%(*authorname)"
	out, err := r.run(ctx, "for-each-ref", "--sort=-committerdate", "--sort=-*committerdate", "--format="+format, prefix)
	if err != nil {
		return nil, err
	}
	var refs []Ref
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 8 {
			continue
		}
		ref := Ref{Name: strings.TrimPrefix(f[0], prefix), Commit: f[1], Subject: f[5], Author: f[6]}
		date := f[3]
		if f[2] != "" { // annotated tag
			ref.Commit, date = f[2], f[4]
			if f[7] != "" {
				ref.Author = f[7]
			}
		}
		ref.Date, _ = time.Parse(time.RFC3339, date)
		refs = append(refs, ref)
	}
	return refs, nil
}

// TreeEntry is one entry of a directory listing.
type TreeEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Type is "file", "dir", "symlink" or "submodule".
	Type string `json:"type"`
	Mode string `json:"mode"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

// Tree lists the directory at path in commit. Directories come first.
func (r *Repo) Tree(ctx context.Context, commit, path string) ([]TreeEntry, error) {
	typ, err := r.ObjectType(ctx, commit, path)
	if err != nil {
		return nil, err
	}
	if typ != "tree" {
		return nil, ErrNotDir
	}
	out, err := r.run(ctx, "ls-tree", "-l", "-z", "--end-of-options", objectSpec(commit, path))
	if err != nil {
		return nil, err
	}
	prefix := strings.Trim(path, "/")
	if prefix != "" {
		prefix += "/"
	}
	var dirs, files []TreeEntry
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		// "<mode> <type> <sha> <size>\t<name>"
		meta, name, ok := bytes.Cut(rec, []byte{'\t'})
		if !ok {
			continue
		}
		f := strings.Fields(string(meta))
		if len(f) != 4 {
			continue
		}
		e := TreeEntry{Name: string(name), Path: prefix + string(name), Mode: f[0], SHA: f[2]}
		e.Size, _ = strconv.ParseInt(f[3], 10, 64)
		switch {
		case f[1] == "tree":
			e.Type = "dir"
		case f[1] == "commit":
			e.Type = "submodule"
		case f[0] == "120000":
			e.Type = "symlink"
		default:
			e.Type = "file"
		}
		if e.Type == "dir" {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	return append(dirs, files...), nil
}

// Blob is file content read from the repository.
type Blob struct {
	SHA       string
	Size      int64
	Content   []byte
	Binary    bool
	Truncated bool
}

// ReadBlob reads the file at path in commit, keeping at most max bytes.
func (r *Repo) ReadBlob(ctx context.Context, commit, path string, max int64) (*Blob, error) {
	spec := objectSpec(commit, path)
	// --batch-check reads the object name from stdin, so it can't be
	// mistaken for an option.
	info, err := r.g.runEnv(ctx, r.Path, nil, strings.NewReader(spec+"\n"), "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	if err != nil {
		return nil, err
	}
	f := strings.Fields(string(info))
	if len(f) < 2 || f[1] == "missing" {
		return nil, ErrNotFound
	}
	if f[1] != "blob" {
		return nil, ErrNotFile
	}
	size, _ := strconv.ParseInt(f[2], 10, 64)
	b := &Blob{SHA: f[0], Size: size}
	rc, err := r.StreamBlob(ctx, f[0])
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b.Content, err = io.ReadAll(io.LimitReader(rc, max))
	if err != nil {
		return nil, err
	}
	b.Truncated = int64(len(b.Content)) < size
	b.Binary = IsBinary(b.Content)
	return b, nil
}

// IsBinary uses git's heuristic: a NUL byte in the first 8000 bytes, or
// invalid UTF-8.
func IsBinary(b []byte) bool {
	head := b
	if len(head) > 8000 {
		head = head[:8000]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	// Allow a multi-byte rune cut off at the end of a truncated read.
	for i := 0; i < 4 && len(head) > 0 && !utf8.Valid(head); i++ {
		head = head[:len(head)-1]
	}
	return !utf8.Valid(head)
}

// StreamBlob streams a blob by object id. The caller must Close it.
func (r *Repo) StreamBlob(ctx context.Context, sha string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, r.g.Binary, "cat-file", "blob", "--end-of-options", sha)
	cmd.Dir = r.Path
	cmd.Env = r.g.baseEnv()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &cmdReader{ReadCloser: stdout, cmd: cmd}, nil
}

type cmdReader struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (c *cmdReader) Close() error {
	_ = c.ReadCloser.Close()
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
	return nil
}

// Signature is an author or committer.
type Signature struct {
	Name  string    `json:"name"`
	Email string    `json:"email"`
	Date  time.Time `json:"date"`
}

// Commit is commit metadata.
type Commit struct {
	SHA       string    `json:"sha"`
	Parents   []string  `json:"parents"`
	Author    Signature `json:"author"`
	Committer Signature `json:"committer"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
}

const logFormat = "%H%x00%P%x00%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%s%x00%b%x1e"

func parseCommits(out []byte) []Commit {
	var cs []Commit
	for _, rec := range bytes.Split(out, []byte{0x1e}) {
		rec = bytes.TrimLeft(rec, "\n")
		if len(rec) == 0 {
			continue
		}
		f := strings.SplitN(string(rec), "\x00", 10)
		if len(f) < 10 {
			continue
		}
		c := Commit{SHA: f[0], Subject: f[8], Body: strings.TrimSpace(f[9]), Parents: []string{}}
		if f[1] != "" {
			c.Parents = strings.Fields(f[1])
		}
		c.Author = Signature{Name: f[2], Email: f[3]}
		c.Author.Date, _ = time.Parse(time.RFC3339, f[4])
		c.Committer = Signature{Name: f[5], Email: f[6]}
		c.Committer.Date, _ = time.Parse(time.RFC3339, f[7])
		cs = append(cs, c)
	}
	return cs
}

// Log returns up to limit commits reachable from commit, skipping skip,
// optionally only those touching path.
func (r *Repo) Log(ctx context.Context, commit, path string, skip, limit int) ([]Commit, error) {
	args := []string{"log", "--format=" + logFormat, "--skip=" + strconv.Itoa(skip), "--max-count=" + strconv.Itoa(limit), "--end-of-options", commit, "--"}
	if path != "" {
		args = append(args, path)
	}
	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseCommits(out), nil
}

// LogRange returns commits in base..head, oldest first (for PRs).
func (r *Repo) LogRange(ctx context.Context, base, head string, limit int) ([]Commit, error) {
	out, err := r.run(ctx, "log", "--reverse", "--format="+logFormat, "--max-count="+strconv.Itoa(limit), "--end-of-options", base+".."+head)
	if err != nil {
		return nil, err
	}
	return parseCommits(out), nil
}

// GetCommit returns one commit.
func (r *Repo) GetCommit(ctx context.Context, sha string) (*Commit, error) {
	out, err := r.run(ctx, "log", "-1", "--format="+logFormat, "--end-of-options", sha)
	if err != nil {
		return nil, ErrNotFound
	}
	cs := parseCommits(out)
	if len(cs) == 0 {
		return nil, ErrNotFound
	}
	return &cs[0], nil
}

// BlameRange is a run of consecutive lines from the same commit.
type BlameRange struct {
	Commit    string    `json:"commit"`
	Author    string    `json:"author"`
	Email     string    `json:"email"`
	Date      time.Time `json:"date"`
	Summary   string    `json:"summary"`
	StartLine int       `json:"start_line"`
	Lines     []string  `json:"lines"`
}

// Blame annotates path at commit.
func (r *Repo) Blame(ctx context.Context, commit, path string) ([]BlameRange, error) {
	if typ, err := r.ObjectType(ctx, commit, path); err != nil {
		return nil, err
	} else if typ != "blob" {
		return nil, ErrNotFile
	}
	// blame mis-parses --end-of-options followed by "--"; the commit is
	// validated instead (callers pass resolved ids).
	if !ValidRefName(commit) {
		return nil, ErrNotFound
	}
	out, err := r.run(ctx, "blame", "--porcelain", commit, "--", path)
	if err != nil {
		return nil, err
	}
	type meta struct {
		author, email, summary string
		date                   time.Time
	}
	metas := map[string]*meta{}
	var ranges []BlameRange
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var cur string
	var lineNo int
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "\t") {
			m := metas[cur]
			text := line[1:]
			if n := len(ranges); n > 0 && ranges[n-1].Commit == cur && ranges[n-1].StartLine+len(ranges[n-1].Lines) == lineNo {
				ranges[n-1].Lines = append(ranges[n-1].Lines, text)
			} else {
				ranges = append(ranges, BlameRange{Commit: cur, Author: m.author, Email: m.email, Date: m.date, Summary: m.summary, StartLine: lineNo, Lines: []string{text}})
			}
			continue
		}
		f := strings.SplitN(line, " ", 4)
		if len(f[0]) == 40 && len(f) >= 3 {
			cur = f[0]
			lineNo, _ = strconv.Atoi(f[2])
			if metas[cur] == nil {
				metas[cur] = &meta{}
			}
			continue
		}
		m := metas[cur]
		if m == nil {
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "author":
			m.author = val
		case "author-mail":
			m.email = strings.Trim(val, "<>")
		case "author-time":
			if s, err := strconv.ParseInt(val, 10, 64); err == nil {
				m.date = time.Unix(s, 0).UTC()
			}
		case "summary":
			m.summary = val
		}
	}
	return ranges, sc.Err()
}

// IsAncestor reports whether a is an ancestor of b.
func (r *Repo) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	_, err := r.run(ctx, "merge-base", "--is-ancestor", "--end-of-options", a, b)
	if err == nil {
		return true, nil
	}
	var ce *CmdError
	if errors.As(err, &ce) {
		var ee *exec.ExitError
		if errors.As(ce.Err, &ee) && ee.ExitCode() == 1 {
			return false, nil
		}
	}
	return false, err
}

// FindReadme returns the path of the README in the root of commit, if any.
func (r *Repo) FindReadme(ctx context.Context, commit, dir string) (string, error) {
	entries, err := r.Tree(ctx, commit, dir)
	if err != nil {
		return "", err
	}
	best, bestRank := "", 99
	for _, e := range entries {
		if e.Type != "file" {
			continue
		}
		n := strings.ToLower(e.Name)
		rank := 99
		switch n {
		case "readme.md", "readme.markdown":
			rank = 0
		case "readme":
			rank = 1
		case "readme.txt", "readme.rst":
			rank = 2
		}
		if rank < bestRank {
			best, bestRank = e.Path, rank
		}
	}
	return best, nil
}
