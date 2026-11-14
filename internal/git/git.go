// Package git wraps the git command line for repository storage, reads
// (refs, trees, blobs, history, diffs, blame) and the smart HTTP protocol.
//
// Every invocation passes user-controlled values after --end-of-options or
// "--" so they can't be interpreted as options.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// EmptyTreeID is the hash of the empty tree (SHA-1 object format).
const EmptyTreeID = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// ZeroID is the all-zero object ID git uses for "no object".
const ZeroID = "0000000000000000000000000000000000000000"

// Errors returned for missing objects so callers can map them to 404s.
var (
	ErrNotFound = errors.New("not found")
	ErrNotDir   = errors.New("not a directory")
	ErrNotFile  = errors.New("not a file")
)

// Git runs git commands.
type Git struct {
	Binary string
	// Home is used as $HOME for git processes so the server user's own
	// ~/.gitconfig never affects repositories.
	Home string
	// Timeout bounds read commands (not smart HTTP transfers).
	Timeout time.Duration
}

// New creates a Git runner. home is created if missing.
func New(binary, home string) (*Git, error) {
	if binary == "" {
		binary = "git"
	}
	if _, err := exec.LookPath(binary); err != nil {
		return nil, fmt.Errorf("git binary %q not found: %w", binary, err)
	}
	if err := os.MkdirAll(home, 0o750); err != nil {
		return nil, err
	}
	return &Git{Binary: binary, Home: home, Timeout: 30 * time.Second}, nil
}

// baseEnv is the environment for every git process.
func (g *Git) baseEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + g.Home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
		"TZ=UTC",
	}
}

// CmdError is a failed git command with its stderr.
type CmdError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *CmdError) Error() string {
	return fmt.Sprintf("git %s: %v: %s", strings.Join(e.Args, " "), e.Err, strings.TrimSpace(e.Stderr))
}

func (e *CmdError) Unwrap() error { return e.Err }

// run executes git in dir and returns stdout.
func (g *Git) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return g.runEnv(ctx, dir, nil, nil, args...)
}

func (g *Git) runEnv(ctx context.Context, dir string, env []string, stdin io.Reader, args ...string) ([]byte, error) {
	if g.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, g.Binary, args...)
	cmd.Dir = dir
	cmd.Env = append(g.baseEnv(), env...)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &limitedBuffer{buf: &stderr, max: 64 << 10}
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), &CmdError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}

// limitedBuffer stops recording after max bytes.
type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room > 0 {
		if len(p) > room {
			l.buf.Write(p[:room])
		} else {
			l.buf.Write(p)
		}
	}
	return len(p), nil
}

// InitBare creates a bare repository with the given default branch.
func (g *Git) InitBare(ctx context.Context, path, defaultBranch string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	if _, err := g.run(ctx, "", "init", "--bare", "--quiet", "--initial-branch="+defaultBranch, "--", path); err != nil {
		return err
	}
	// Server-side settings: reject corrupt objects, allow uploadpack of
	// any reachable sha (needed for shallow fetches by CI), keep reflogs.
	for _, kv := range [][2]string{
		{"receive.fsckObjects", "true"},
		{"uploadpack.allowAnySHA1InWant", "true"},
		{"core.logAllRefUpdates", "true"},
		{"receive.advertisePushOptions", "true"},
	} {
		if _, err := g.run(ctx, path, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// Repo is a handle to one bare repository.
type Repo struct {
	g    *Git
	Path string
}

// Open returns a handle for the repository at path.
func (g *Git) Open(path string) *Repo { return &Repo{g: g, Path: path} }

func (r *Repo) run(ctx context.Context, args ...string) ([]byte, error) {
	return r.g.run(ctx, r.Path, args...)
}

var refNameRe = regexp.MustCompile(`^[A-Za-z0-9._/@+-]+$`)

// ValidRefName reports whether s is safe and plausible as a branch or tag
// name or commit-ish (no leading dash, no "..", no control characters).
func ValidRefName(s string) bool {
	if s == "" || len(s) > 255 || strings.HasPrefix(s, "-") || strings.Contains(s, "..") ||
		strings.HasSuffix(s, ".lock") || strings.HasSuffix(s, "/") || strings.HasPrefix(s, "/") || strings.Contains(s, "//") {
		return false
	}
	return refNameRe.MatchString(s)
}

// ValidBranchName checks a branch name with git's own rules.
func (g *Git) ValidBranchName(ctx context.Context, name string) bool {
	if !ValidRefName(name) {
		return false
	}
	_, err := g.run(ctx, "", "check-ref-format", "--branch", name)
	return err == nil
}

// IsEmpty reports whether the repository has no refs at all.
func (r *Repo) IsEmpty(ctx context.Context) (bool, error) {
	out, err := r.run(ctx, "for-each-ref", "--count=1", "--format=%(refname)")
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(out)) == 0, nil
}

// HeadBranch returns the branch HEAD points to.
func (r *Repo) HeadBranch(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// SetHead points HEAD at branch.
func (r *Repo) SetHead(ctx context.Context, branch string) error {
	_, err := r.run(ctx, "symbolic-ref", "HEAD", "refs/heads/"+branch)
	return err
}

// ResolveCommit resolves a ref, branch, tag or sha to a full commit id.
func (r *Repo) ResolveCommit(ctx context.Context, rev string) (string, error) {
	if !ValidRefName(rev) {
		return "", ErrNotFound
	}
	out, err := r.run(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", ErrNotFound
	}
	return strings.TrimSpace(string(out)), nil
}

// ObjectType returns the type of rev:path ("blob", "tree", "commit") or
// ErrNotFound.
func (r *Repo) ObjectType(ctx context.Context, commit, path string) (string, error) {
	out, err := r.run(ctx, "cat-file", "-t", "--end-of-options", objectSpec(commit, path))
	if err != nil {
		return "", ErrNotFound
	}
	return strings.TrimSpace(string(out)), nil
}

func objectSpec(commit, path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return commit + "^{tree}"
	}
	return commit + ":" + path
}

// CleanPath normalises a repository path and rejects traversal.
func CleanPath(p string) (string, bool) {
	p = strings.Trim(p, "/")
	if p == "" {
		return "", true
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.ContainsRune(seg, 0) {
			return "", false
		}
	}
	return p, true
}
