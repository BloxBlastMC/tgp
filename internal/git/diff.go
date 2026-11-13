package git

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// DiffLine is one line of a hunk.
type DiffLine struct {
	// Type is "context", "add" or "del".
	Type    string `json:"type"`
	Content string `json:"content"`
	// OldLine / NewLine are 1-based line numbers; 0 when not applicable.
	OldLine int `json:"old_line,omitempty"`
	NewLine int `json:"new_line,omitempty"`
	// NoNewline marks the last line of a file without a trailing newline.
	NoNewline bool `json:"no_newline,omitempty"`
}

// Hunk is one @@ section.
type Hunk struct {
	Header   string     `json:"header"`
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Section  string     `json:"section,omitempty"`
	Lines    []DiffLine `json:"lines"`
}

// DiffFile is the change to one file.
type DiffFile struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
	// Status is "added", "deleted", "modified", "renamed" or "copied".
	Status    string `json:"status"`
	OldMode   string `json:"old_mode,omitempty"`
	NewMode   string `json:"new_mode,omitempty"`
	Binary    bool   `json:"binary"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Hunks     []Hunk `json:"hunks"`
	// Truncated means hunks were dropped because the diff hit a size limit.
	Truncated bool `json:"truncated"`
}

// Diff is a parsed diff between two trees.
type Diff struct {
	Files     []DiffFile `json:"files"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Truncated bool       `json:"truncated"`
}

// DiffLimits bounds how much of a diff is kept in memory.
type DiffLimits struct {
	MaxFiles     int // files beyond this are dropped entirely
	MaxLines     int // total hunk lines kept across all files
	MaxFileLines int // hunk lines kept per file
	MaxLineBytes int // longer lines are cut
}

// DefaultDiffLimits suit the web UI and API.
var DefaultDiffLimits = DiffLimits{MaxFiles: 500, MaxLines: 20000, MaxFileLines: 5000, MaxLineBytes: 4096}

// Diff computes the diff from one commit/tree to another. Use EmptyTreeID
// as from for a root commit.
func (r *Repo) Diff(ctx context.Context, from, to string, lim DiffLimits) (*Diff, error) {
	return r.diff(ctx, lim, "diff", "--no-color", "--no-ext-diff", "--no-textconv", "-M", "-U3",
		"--src-prefix=a/", "--dst-prefix=b/", "--end-of-options", from, to)
}

// DiffMergeBase diffs from the merge base of base and head to head
// (what a pull request changes).
func (r *Repo) DiffMergeBase(ctx context.Context, base, head string, lim DiffLimits) (*Diff, error) {
	return r.diff(ctx, lim, "diff", "--no-color", "--no-ext-diff", "--no-textconv", "-M", "-U3",
		"--src-prefix=a/", "--dst-prefix=b/", "--end-of-options", base+"..."+head)
}

func (r *Repo) diff(ctx context.Context, lim DiffLimits, args ...string) (*Diff, error) {
	if r.g.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.g.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, r.g.Binary, append([]string{"-c", "core.quotePath=false"}, args...)...)
	cmd.Dir = r.Path
	cmd.Env = r.g.baseEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	d, perr := ParseDiff(stdout, lim)
	_, _ = io.Copy(io.Discard, stdout)
	if err := cmd.Wait(); err != nil {
		return nil, &CmdError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return d, perr
}

var hunkHeaderPrefix = "@@ -"

// ParseDiff parses unified diff output from `git diff`.
func ParseDiff(rd io.Reader, lim DiffLimits) (*Diff, error) {
	br := bufio.NewReaderSize(rd, 64<<10)
	d := &Diff{Files: []DiffFile{}}
	var (
		file      *DiffFile
		hunk      *Hunk
		oldN      int
		newN      int
		kept      int
		fileKept  int
		dropFiles bool
	)
	flush := func() {
		if file == nil {
			return
		}
		if file.Status == "" {
			file.Status = "modified"
		}
		if file.Hunks == nil {
			file.Hunks = []Hunk{}
		}
		d.Additions += file.Additions
		d.Deletions += file.Deletions
		if !dropFiles {
			d.Files = append(d.Files, *file)
		}
		file, hunk = nil, nil
	}
	for {
		line, err := readLine(br, lim.MaxLineBytes)
		if line == "" && err != nil {
			break
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			if lim.MaxFiles > 0 && len(d.Files) >= lim.MaxFiles {
				dropFiles = true
				d.Truncated = true
			}
			file = &DiffFile{}
			file.OldPath, file.NewPath = parseGitHeader(strings.TrimPrefix(line, "diff --git "))
			fileKept = 0
		case file == nil:
			// preamble; ignore
		case hunk == nil && strings.HasPrefix(line, "new file mode "):
			file.Status, file.NewMode = "added", strings.TrimPrefix(line, "new file mode ")
		case hunk == nil && strings.HasPrefix(line, "deleted file mode "):
			file.Status, file.OldMode = "deleted", strings.TrimPrefix(line, "deleted file mode ")
		case hunk == nil && strings.HasPrefix(line, "old mode "):
			file.OldMode = strings.TrimPrefix(line, "old mode ")
		case hunk == nil && strings.HasPrefix(line, "new mode "):
			file.NewMode = strings.TrimPrefix(line, "new mode ")
		case hunk == nil && strings.HasPrefix(line, "rename from "):
			file.Status, file.OldPath = "renamed", unquote(strings.TrimPrefix(line, "rename from "))
		case hunk == nil && strings.HasPrefix(line, "rename to "):
			file.NewPath = unquote(strings.TrimPrefix(line, "rename to "))
		case hunk == nil && strings.HasPrefix(line, "copy from "):
			file.Status, file.OldPath = "copied", unquote(strings.TrimPrefix(line, "copy from "))
		case hunk == nil && strings.HasPrefix(line, "copy to "):
			file.NewPath = unquote(strings.TrimPrefix(line, "copy to "))
		case hunk == nil && strings.HasPrefix(line, "Binary files "):
			file.Binary = true
		case hunk == nil && strings.HasPrefix(line, "--- "):
			// git appends a tab after paths containing spaces.
			if p := strings.TrimSuffix(strings.TrimPrefix(line, "--- "), "\t"); p != "/dev/null" {
				file.OldPath = strings.TrimPrefix(unquote(p), "a/")
			}
		case hunk == nil && strings.HasPrefix(line, "+++ "):
			if p := strings.TrimSuffix(strings.TrimPrefix(line, "+++ "), "\t"); p != "/dev/null" {
				file.NewPath = strings.TrimPrefix(unquote(p), "b/")
			}
		case strings.HasPrefix(line, hunkHeaderPrefix):
			h, ok := parseHunkHeader(line)
			if !ok {
				return d, fmt.Errorf("bad hunk header %q", line)
			}
			oldN, newN = h.OldStart, h.NewStart
			if keep(lim, kept, fileKept) && !dropFiles {
				file.Hunks = append(file.Hunks, h)
				hunk = &file.Hunks[len(file.Hunks)-1]
			} else {
				file.Truncated, d.Truncated = true, true
				hunk = &Hunk{} // count but don't keep
			}
		case hunk != nil && line != "" && (line[0] == ' ' || line[0] == '+' || line[0] == '-'):
			dl := DiffLine{Content: line[1:]}
			switch line[0] {
			case ' ':
				dl.Type, dl.OldLine, dl.NewLine = "context", oldN, newN
				oldN++
				newN++
			case '+':
				dl.Type, dl.NewLine = "add", newN
				newN++
				file.Additions++
			case '-':
				dl.Type, dl.OldLine = "del", oldN
				oldN++
				file.Deletions++
			}
			if keep(lim, kept, fileKept) && !dropFiles && len(file.Hunks) > 0 && hunk == &file.Hunks[len(file.Hunks)-1] {
				hunk.Lines = append(hunk.Lines, dl)
				kept++
				fileKept++
			} else if !dropFiles {
				file.Truncated, d.Truncated = true, true
			}
		case hunk != nil && strings.HasPrefix(line, `\`):
			if n := len(hunk.Lines); n > 0 {
				hunk.Lines[n-1].NoNewline = true
			}
		}
		if err != nil {
			break
		}
	}
	flush()
	return d, nil
}

func keep(lim DiffLimits, kept, fileKept int) bool {
	return (lim.MaxLines <= 0 || kept < lim.MaxLines) && (lim.MaxFileLines <= 0 || fileKept < lim.MaxFileLines)
}

// readLine reads one line without its newline, cutting it at max bytes.
func readLine(br *bufio.Reader, max int) (string, error) {
	var sb strings.Builder
	for {
		chunk, isPrefix, err := br.ReadLine()
		if max <= 0 || sb.Len() < max {
			room := len(chunk)
			if max > 0 && sb.Len()+room > max {
				room = max - sb.Len()
			}
			sb.Write(chunk[:room])
		}
		if err != nil {
			return sb.String(), err
		}
		if !isPrefix {
			return sb.String(), nil
		}
	}
}

func parseHunkHeader(line string) (Hunk, bool) {
	// @@ -a[,b] +c[,d] @@ section
	h := Hunk{Header: line}
	rest := strings.TrimPrefix(line, "@@ ")
	end := strings.Index(rest, " @@")
	if end < 0 {
		return h, false
	}
	ranges := strings.Fields(rest[:end])
	if len(ranges) != 2 {
		return h, false
	}
	var ok1, ok2 bool
	h.OldStart, h.OldLines, ok1 = parseRange(strings.TrimPrefix(ranges[0], "-"))
	h.NewStart, h.NewLines, ok2 = parseRange(strings.TrimPrefix(ranges[1], "+"))
	h.Section = strings.TrimSpace(rest[end+3:])
	return h, ok1 && ok2
}

func parseRange(s string) (start, n int, ok bool) {
	a, b, has := strings.Cut(s, ",")
	start, err := strconv.Atoi(a)
	if err != nil {
		return 0, 0, false
	}
	n = 1
	if has {
		if n, err = strconv.Atoi(b); err != nil {
			return 0, 0, false
		}
	}
	return start, n, true
}

// parseGitHeader extracts paths from "a/old b/new" (used when there are no
// ---/+++ lines, e.g. binary or mode-only changes). Paths are equal in the
// common case, which makes splitting unambiguous even with spaces.
func parseGitHeader(s string) (string, string) {
	if strings.HasPrefix(s, `"`) {
		// Quoted paths: "a/x" "b/y"
		if i := strings.Index(s[1:], `" `); i >= 0 {
			return strings.TrimPrefix(unquote(s[:i+2]), "a/"), strings.TrimPrefix(unquote(s[i+3:]), "b/")
		}
	}
	if len(s)%2 == 1 {
		half := (len(s) - 1) / 2
		a, b := s[:half], s[half+1:]
		if strings.HasPrefix(a, "a/") && strings.HasPrefix(b, "b/") && a[2:] == b[2:] {
			return a[2:], b[2:]
		}
	}
	if i := strings.Index(s, " b/"); i >= 0 {
		return strings.TrimPrefix(s[:i], "a/"), s[i+3:]
	}
	return s, s
}

// unquote decodes git's C-style quoted paths ("a\tb", octal escapes).
func unquote(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	s = s[1 : len(s)-1]
	var out []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			out = append(out, c)
			continue
		}
		i++
		switch s[i] {
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case '"', '\\':
			out = append(out, s[i])
		case 'a':
			out = append(out, 7)
		case 'b':
			out = append(out, 8)
		case 'f':
			out = append(out, 12)
		case 'r':
			out = append(out, '\r')
		case 'v':
			out = append(out, 11)
		default:
			if i+2 < len(s) {
				if v, err := strconv.ParseUint(s[i:i+3], 8, 8); err == nil {
					out = append(out, byte(v))
					i += 2
					continue
				}
			}
			out = append(out, '\\', s[i])
		}
	}
	return string(out)
}
