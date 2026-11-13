package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileChange is a file written by CommitFiles.
type FileChange struct {
	Path    string
	Content []byte
}

// CommitOptions describes a commit made without a work tree.
type CommitOptions struct {
	Branch  string
	Message string
	Author  Signature
	Files   []FileChange
}

// CommitFiles creates a commit on Branch containing Files on top of the
// branch's current tree (or as a root commit if the branch doesn't exist),
// using a temporary index so no work tree is needed. The branch update is
// compare-and-swap, so a concurrent push makes it fail instead of losing
// work. Returns the new commit id.
func (r *Repo) CommitFiles(ctx context.Context, o CommitOptions) (string, error) {
	if !ValidRefName(o.Branch) {
		return "", fmt.Errorf("invalid branch %q", o.Branch)
	}
	tmp, err := os.MkdirTemp("", "tgp-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	env := []string{
		"GIT_INDEX_FILE=" + filepath.Join(tmp, "index"),
		"GIT_AUTHOR_NAME=" + o.Author.Name,
		"GIT_AUTHOR_EMAIL=" + o.Author.Email,
		"GIT_COMMITTER_NAME=" + o.Author.Name,
		"GIT_COMMITTER_EMAIL=" + o.Author.Email,
	}
	if !o.Author.Date.IsZero() {
		d := o.Author.Date.Format(time.RFC3339)
		env = append(env, "GIT_AUTHOR_DATE="+d, "GIT_COMMITTER_DATE="+d)
	}
	run := func(stdin string, args ...string) (string, error) {
		var in *strings.Reader
		if stdin != "" {
			in = strings.NewReader(stdin)
		}
		var out []byte
		var err error
		if in != nil {
			out, err = r.g.runEnv(ctx, r.Path, env, in, args...)
		} else {
			out, err = r.g.runEnv(ctx, r.Path, env, nil, args...)
		}
		return strings.TrimSpace(string(out)), err
	}

	ref := "refs/heads/" + o.Branch
	parent, _ := run("", "rev-parse", "--verify", "--quiet", ref)
	if parent != "" {
		if _, err := run("", "read-tree", "--end-of-options", parent); err != nil {
			return "", err
		}
	} else if _, err := run("", "read-tree", "--empty"); err != nil {
		return "", err
	}
	for _, f := range o.Files {
		p, ok := CleanPath(f.Path)
		if !ok || p == "" {
			return "", fmt.Errorf("invalid path %q", f.Path)
		}
		sha, err := run(string(f.Content), "hash-object", "-w", "--stdin")
		if err != nil {
			return "", err
		}
		if _, err := run("", "update-index", "--add", "--cacheinfo", "100644,"+sha+","+p); err != nil {
			return "", err
		}
	}
	tree, err := run("", "write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", "-m", o.Message}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	args = append(args, tree)
	commit, err := run("", args...)
	if err != nil {
		return "", err
	}
	old := parent
	if old == "" {
		old = ZeroID
	}
	if _, err := run("", "update-ref", ref, commit, old); err != nil {
		return "", err
	}
	return commit, nil
}

// RenameDir moves a repository directory.
func RenameDir(from, to string) error {
	if _, err := os.Stat(to); err == nil {
		return fmt.Errorf("%s already exists", to)
	}
	return os.Rename(from, to)
}
