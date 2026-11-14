package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ConflictError is returned when a merge can't be done automatically.
type ConflictError struct {
	Files []string
	// Commit is set for rebases: the head commit that didn't apply.
	Commit string
}

func (e *ConflictError) Error() string {
	if e.Commit != "" {
		return fmt.Sprintf("commit %s conflicts in: %s", short(e.Commit), strings.Join(e.Files, ", "))
	}
	return "conflicts in: " + strings.Join(e.Files, ", ")
}

// ErrMergeCommits is returned when rebasing a range that contains merges.
var ErrMergeCommits = errors.New("the branch contains merge commits, which can't be rebased")

// ErrRefChanged is returned when a compare-and-swap ref update loses a race.
var ErrRefChanged = errors.New("the branch changed while the operation ran")

func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// MergeBase returns the best common ancestor of a and b ("" if none).
func (r *Repo) MergeBase(ctx context.Context, a, b string) (string, error) {
	out, err := r.run(ctx, "merge-base", "--end-of-options", a, b)
	if err != nil {
		var ce *CmdError
		if errors.As(err, &ce) {
			var ee *exec.ExitError
			if errors.As(ce.Err, &ee) && ee.ExitCode() == 1 {
				return "", nil // unrelated histories
			}
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// AheadBehind counts commits in head not in base (ahead) and in base not
// in head (behind).
func (r *Repo) AheadBehind(ctx context.Context, base, head string) (ahead, behind int, err error) {
	out, err := r.run(ctx, "rev-list", "--left-right", "--count", "--end-of-options", base+"..."+head)
	if err != nil {
		return 0, 0, err
	}
	f := strings.Fields(string(out))
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output %q", out)
	}
	behind, _ = strconv.Atoi(f[0])
	ahead, _ = strconv.Atoi(f[1])
	return ahead, behind, nil
}

// MergeTree merges theirs into ours without touching any ref or work tree.
// It returns the resulting tree, or a *ConflictError. mergeBase overrides
// the computed merge base (used to cherry-pick one commit).
func (r *Repo) MergeTree(ctx context.Context, ours, theirs, mergeBase string) (string, error) {
	args := []string{"merge-tree", "--write-tree", "--name-only", "--no-messages", "-z"}
	if mergeBase != "" {
		args = append(args, "--merge-base="+mergeBase)
	}
	args = append(args, "--end-of-options", ours, theirs)
	out, err := r.run(ctx, args...)
	parts := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	if err != nil {
		var ce *CmdError
		var ee *exec.ExitError
		if errors.As(err, &ce) && errors.As(ce.Err, &ee) && ee.ExitCode() == 1 && len(parts) > 0 && len(parts[0]) >= 40 {
			seen := map[string]bool{}
			var files []string
			for _, p := range parts[1:] {
				if p != "" && !seen[p] {
					seen[p] = true
					files = append(files, p)
				}
			}
			return "", &ConflictError{Files: files}
		}
		return "", err
	}
	if len(parts) == 0 || len(parts[0]) < 40 {
		return "", fmt.Errorf("unexpected merge-tree output %q", out)
	}
	return parts[0], nil
}

// Identity is who authors or commits a server-made commit.
type Identity struct {
	Name  string
	Email string
	Date  time.Time // zero: now
}

// CommitTree creates a commit object for tree with the given parents.
func (r *Repo) CommitTree(ctx context.Context, tree string, parents []string, message string, author, committer Identity) (string, error) {
	now := time.Now().UTC()
	date := func(t time.Time) string {
		if t.IsZero() {
			t = now
		}
		return t.Format(time.RFC3339)
	}
	env := []string{
		"GIT_AUTHOR_NAME=" + author.Name, "GIT_AUTHOR_EMAIL=" + author.Email, "GIT_AUTHOR_DATE=" + date(author.Date),
		"GIT_COMMITTER_NAME=" + committer.Name, "GIT_COMMITTER_EMAIL=" + committer.Email, "GIT_COMMITTER_DATE=" + date(committer.Date),
	}
	args := []string{"commit-tree", "-F", "-"}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	args = append(args, "--end-of-options", tree)
	out, err := r.g.runEnv(ctx, r.Path, env, strings.NewReader(message), args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// UpdateRef points ref at newID if it currently points at oldID (ZeroID:
// must not exist; "": no check). Returns ErrRefChanged if it doesn't.
func (r *Repo) UpdateRef(ctx context.Context, ref, newID, oldID string) error {
	args := []string{"update-ref", "-m", "tgp", "--end-of-options", ref, newID}
	if oldID != "" {
		args = append(args, oldID)
	}
	if _, err := r.run(ctx, args...); err != nil {
		if strings.Contains(err.Error(), "but expected") || strings.Contains(err.Error(), "cannot lock ref") ||
			strings.Contains(err.Error(), "reference already exists") {
			return ErrRefChanged
		}
		return err
	}
	return nil
}

// DeleteRef deletes ref if it points at oldID ("" to skip the check).
func (r *Repo) DeleteRef(ctx context.Context, ref, oldID string) error {
	args := []string{"update-ref", "-d", "--end-of-options", ref}
	if oldID != "" {
		args = append(args, oldID)
	}
	if _, err := r.run(ctx, args...); err != nil {
		if strings.Contains(err.Error(), "but expected") || strings.Contains(err.Error(), "cannot lock ref") {
			return ErrRefChanged
		}
		return err
	}
	return nil
}

// RefCommit returns the commit a full ref name points to ("" if missing).
func (r *Repo) RefCommit(ctx context.Context, ref string) (string, error) {
	out, err := r.run(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		var ce *CmdError
		var ee *exec.ExitError
		if errors.As(err, &ce) && errors.As(ce.Err, &ee) && ee.ExitCode() == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// RefCommitTree returns the tree of a commit.
func (r *Repo) RefCommitTree(ctx context.Context, commit string) (string, error) {
	out, err := r.run(ctx, "rev-parse", "--verify", "--end-of-options", commit+"^{tree}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// rawCommit is the parts of a commit needed to replay it.
type rawCommit struct {
	sha, parent string
	parents     int
	author      Identity
	message     string
}

func (r *Repo) rangeCommits(ctx context.Context, base, head string) ([]rawCommit, error) {
	out, err := r.run(ctx, "log", "--reverse", "--topo-order", "--format=%H%x00%P%x00%an%x00%ae%x00%aI%x00%B%x1e",
		"--end-of-options", base+".."+head)
	if err != nil {
		return nil, err
	}
	var cs []rawCommit
	for _, rec := range strings.Split(string(out), "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\x00", 6)
		if len(f) < 6 {
			continue
		}
		parents := strings.Fields(f[1])
		c := rawCommit{sha: f[0], parents: len(parents), message: f[5]}
		if len(parents) > 0 {
			c.parent = parents[0]
		}
		c.author = Identity{Name: f[2], Email: f[3]}
		c.author.Date, _ = time.Parse(time.RFC3339, f[4])
		cs = append(cs, c)
	}
	return cs, nil
}

// MergeMethod is how a pull request lands on its base branch.
type MergeMethod string

const (
	MethodMerge  MergeMethod = "merge"  // merge commit with both parents
	MethodSquash MergeMethod = "squash" // one new commit with the combined change
	MethodRebase MergeMethod = "rebase" // replay each commit onto the base
)

// MergeRequest describes a server-side merge of head into base.
type MergeRequest struct {
	Method    MergeMethod
	Base      string // base branch tip
	Head      string // head commit
	Message   string // merge or squash commit message (ignored for rebase)
	Author    Identity
	Committer Identity
}

// Merge computes the commit the base branch should move to. No refs are
// changed. Conflicts return *ConflictError.
func (r *Repo) Merge(ctx context.Context, m MergeRequest) (string, error) {
	switch m.Method {
	case MethodMerge:
		tree, err := r.MergeTree(ctx, m.Base, m.Head, "")
		if err != nil {
			return "", err
		}
		return r.CommitTree(ctx, tree, []string{m.Base, m.Head}, m.Message, m.Committer, m.Committer)
	case MethodSquash:
		tree, err := r.MergeTree(ctx, m.Base, m.Head, "")
		if err != nil {
			return "", err
		}
		return r.CommitTree(ctx, tree, []string{m.Base}, m.Message, m.Author, m.Committer)
	case MethodRebase:
		if ok, err := r.IsAncestor(ctx, m.Base, m.Head); err != nil {
			return "", err
		} else if ok {
			return m.Head, nil // already on top of base: fast-forward
		}
		commits, err := r.rangeCommits(ctx, m.Base, m.Head)
		if err != nil {
			return "", err
		}
		cur := m.Base
		for _, c := range commits {
			if c.parents > 1 {
				return "", ErrMergeCommits
			}
			tree, err := r.MergeTree(ctx, cur, c.sha, c.parent)
			if err != nil {
				var ce *ConflictError
				if errors.As(err, &ce) {
					ce.Commit = c.sha
				}
				return "", err
			}
			if curTree, err := r.RefCommitTree(ctx, cur); err == nil && curTree == tree {
				continue // already in base: drop it, like git rebase
			}
			if cur, err = r.CommitTree(ctx, tree, []string{cur}, c.message, c.author, m.Committer); err != nil {
				return "", err
			}
		}
		return cur, nil
	}
	return "", fmt.Errorf("unknown merge method %q", m.Method)
}

// DiffStat summarises the diff between two commits.
func (r *Repo) DiffStat(ctx context.Context, from, to string) (files, additions, deletions int, err error) {
	out, err := r.run(ctx, "diff", "--numstat", "-z", "-M", "--no-ext-diff", "--no-textconv", "--end-of-options", from, to)
	if err != nil {
		return 0, 0, 0, err
	}
	// Records: "add\tdel\tpath\0" or, for renames, "add\tdel\t\0old\0new\0".
	parts := strings.Split(string(out), "\x00")
	for i := 0; i < len(parts); i++ {
		f := strings.SplitN(parts[i], "\t", 3)
		if len(f) != 3 {
			continue
		}
		files++
		a, _ := strconv.Atoi(f[0]) // "-" for binary files counts as 0
		d, _ := strconv.Atoi(f[1])
		additions += a
		deletions += d
		if f[2] == "" {
			i += 2 // rename: skip the two path fields
		}
	}
	return files, additions, deletions, nil
}

// CountCommits counts commits in base..head.
func (r *Repo) CountCommits(ctx context.Context, base, head string) (int, error) {
	out, err := r.run(ctx, "rev-list", "--count", "--end-of-options", base+".."+head)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// PathChanged reports whether path differs between two commits.
func (r *Repo) PathChanged(ctx context.Context, from, to, path string) (bool, error) {
	out, err := r.run(ctx, "diff", "--name-only", "--no-ext-diff", "--end-of-options", from, to, "--", path)
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}
