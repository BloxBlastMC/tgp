package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// mergeFixture: main has "base" then "main change"; feat branches from
// base with two commits.
func mergeFixture(t *testing.T, conflict bool) (*fixture, *Repo, string, string) {
	f := newFixture(t)
	f.write("a.txt", "one\ntwo\nthree\n")
	f.sh(f.work, "add", ".")
	f.sh(f.work, "commit", "-qm", "base")
	f.sh(f.work, "checkout", "-qb", "feat")
	f.write("b.txt", "feature\n")
	f.sh(f.work, "add", ".")
	f.sh(f.work, "commit", "-qm", "feat 1")
	if conflict {
		f.write("a.txt", "one\nTWO-feat\nthree\n")
	} else {
		f.write("c.txt", "more\n")
	}
	f.sh(f.work, "add", ".")
	f.sh(f.work, "commit", "-qm", "feat 2")
	f.sh(f.work, "checkout", "-q", "main")
	f.write("a.txt", "one\nTWO-main\nthree\n")
	f.sh(f.work, "commit", "-qam", "main change")
	f.sh(f.work, "push", "-q", "origin", "main", "feat")
	r := f.g.Open(f.bare)
	ctx := context.Background()
	base, _ := r.RefCommit(ctx, "refs/heads/main")
	head, _ := r.RefCommit(ctx, "refs/heads/feat")
	return f, r, base, head
}

var bot = Identity{Name: "Merger", Email: "m@example.com"}

func TestMergeMethods(t *testing.T) {
	ctx := context.Background()
	_, r, base, head := mergeFixture(t, false)

	if ahead, behind, err := r.AheadBehind(ctx, base, head); err != nil || ahead != 2 || behind != 1 {
		t.Fatalf("ahead/behind = %d/%d %v", ahead, behind, err)
	}
	if ok, _ := r.IsAncestor(ctx, base, head); ok {
		t.Fatal("base shouldn't be an ancestor of head")
	}

	m, err := r.Merge(ctx, MergeRequest{Method: MethodMerge, Base: base, Head: head, Message: "Merge feat", Committer: bot})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := r.GetCommit(ctx, m)
	if len(c.Parents) != 2 || c.Parents[0] != base || c.Parents[1] != head || c.Subject != "Merge feat" {
		t.Fatalf("merge commit: %+v", c)
	}

	sq, err := r.Merge(ctx, MergeRequest{Method: MethodSquash, Base: base, Head: head, Message: "Feat (#1)",
		Author: Identity{Name: "Alice", Email: "alice@example.com"}, Committer: bot})
	if err != nil {
		t.Fatal(err)
	}
	c, _ = r.GetCommit(ctx, sq)
	if len(c.Parents) != 1 || c.Parents[0] != base || c.Author.Name != "Alice" || c.Committer.Name != "Merger" {
		t.Fatalf("squash commit: %+v", c)
	}
	if tm, _ := r.RefCommitTree(ctx, m); tm != mustTree(t, r, sq) {
		t.Fatal("merge and squash trees differ")
	}

	rb, err := r.Merge(ctx, MergeRequest{Method: MethodRebase, Base: base, Head: head, Committer: bot})
	if err != nil {
		t.Fatal(err)
	}
	log, _ := r.Log(ctx, rb, "", 0, 10)
	if len(log) != 4 || log[0].Subject != "feat 2" || log[1].Subject != "feat 1" || log[2].SHA != base || log[0].Author.Name != "Alice" || log[0].Committer.Name != "Merger" {
		t.Fatalf("rebase log: %+v", log)
	}
	if mustTree(t, r, rb) != mustTree(t, r, m) {
		t.Fatal("rebase tree differs from merge tree")
	}

	// Already up to date: rebase fast-forwards.
	if ff, err := r.Merge(ctx, MergeRequest{Method: MethodRebase, Base: base, Head: rb, Committer: bot}); err != nil || ff != rb {
		t.Fatalf("ff rebase = %s %v", ff, err)
	}

	// CAS ref updates.
	if err := r.UpdateRef(ctx, "refs/heads/main", m, head); !errors.Is(err, ErrRefChanged) {
		t.Fatalf("stale CAS: %v", err)
	}
	if err := r.UpdateRef(ctx, "refs/heads/main", m, base); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateRef(ctx, "refs/pull/1/head", head, ZeroID); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateRef(ctx, "refs/pull/1/head", head, ZeroID); !errors.Is(err, ErrRefChanged) {
		t.Fatalf("create existing: %v", err)
	}
	if err := r.DeleteRef(ctx, "refs/heads/feat", head); err != nil {
		t.Fatal(err)
	}
	if got, err := r.RefCommit(ctx, "refs/heads/feat"); err != nil || got != "" {
		t.Fatalf("deleted ref: %q %v", got, err)
	}
	if mb, _ := r.MergeBase(ctx, m, head); mb != head {
		t.Fatalf("merge base = %s", mb)
	}
}

func mustTree(t *testing.T, r *Repo, c string) string {
	t.Helper()
	tr, err := r.RefCommitTree(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestMergeConflicts(t *testing.T) {
	ctx := context.Background()
	_, r, base, head := mergeFixture(t, true)
	for _, m := range []MergeMethod{MethodMerge, MethodSquash, MethodRebase} {
		_, err := r.Merge(ctx, MergeRequest{Method: m, Base: base, Head: head, Message: "x", Committer: bot, Author: bot})
		var ce *ConflictError
		if !errors.As(err, &ce) || strings.Join(ce.Files, ",") != "a.txt" {
			t.Fatalf("%s: %v", m, err)
		}
		if m == MethodRebase && ce.Commit != head {
			t.Fatalf("rebase conflict commit = %s, want %s", ce.Commit, head)
		}
	}
}

func TestRebaseRejectsMergeCommits(t *testing.T) {
	ctx := context.Background()
	f, r, base, _ := mergeFixture(t, false)
	f.sh(f.work, "checkout", "-q", "feat")
	f.sh(f.work, "merge", "-q", "--no-edit", "main~1")
	f.sh(f.work, "checkout", "-qb", "side", "main~1")
	f.write("d.txt", "d\n")
	f.sh(f.work, "add", ".")
	f.sh(f.work, "commit", "-qm", "side")
	f.sh(f.work, "checkout", "-q", "feat")
	f.sh(f.work, "merge", "-q", "--no-edit", "side")
	f.sh(f.work, "push", "-q", "origin", "feat")
	head, _ := r.RefCommit(ctx, "refs/heads/feat")
	if _, err := r.Merge(ctx, MergeRequest{Method: MethodRebase, Base: base, Head: head, Committer: bot}); !errors.Is(err, ErrMergeCommits) {
		t.Fatalf("got %v", err)
	}
}

func TestDiffStatAndCounts(t *testing.T) {
	ctx := context.Background()
	f, r, base, head := mergeFixture(t, false)
	mb, _ := r.MergeBase(ctx, base, head)
	files, add, del, err := r.DiffStat(ctx, mb, head)
	if err != nil || files != 2 || add != 2 || del != 0 {
		t.Fatalf("diffstat = %d %d %d %v", files, add, del, err)
	}
	f.sh(f.work, "checkout", "-q", "feat")
	f.sh(f.work, "mv", "b.txt", "renamed.txt")
	f.sh(f.work, "commit", "-qm", "rename")
	f.sh(f.work, "push", "-q", "origin", "feat")
	head2, _ := r.RefCommit(ctx, "refs/heads/feat")
	if files, add, del, _ := r.DiffStat(ctx, mb, head2); files != 2 || add != 2 || del != 0 {
		t.Fatalf("diffstat with rename = %d %d %d", files, add, del)
	}
	if n, err := r.CountCommits(ctx, mb, head2); err != nil || n != 3 {
		t.Fatalf("count = %d %v", n, err)
	}
	if ch, _ := r.PathChanged(ctx, head, head2, "c.txt"); ch {
		t.Fatal("c.txt didn't change")
	}
	if ch, _ := r.PathChanged(ctx, head, head2, "b.txt"); !ch {
		t.Fatal("b.txt was renamed away")
	}
}
