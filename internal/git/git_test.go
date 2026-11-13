package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a bare repo with a small history via a work tree.
type fixture struct {
	t    *testing.T
	g    *Git
	bare string
	work string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	g, err := New("git", filepath.Join(dir, "home"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, g: g, bare: filepath.Join(dir, "repo.git"), work: filepath.Join(dir, "work")}
	if err := g.InitBare(context.Background(), f.bare, "main"); err != nil {
		t.Fatal(err)
	}
	f.sh("", "clone", "--quiet", f.bare, f.work)
	return f
}

func (f *fixture) sh(dir string, args ...string) string {
	f.t.Helper()
	if dir == "" {
		dir = filepath.Dir(f.work)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(f.g.baseEnv(), "GIT_AUTHOR_NAME=Alice", "GIT_AUTHOR_EMAIL=alice@example.com",
		"GIT_COMMITTER_NAME=Alice", "GIT_COMMITTER_EMAIL=alice@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *fixture) write(path, content string) {
	f.t.Helper()
	p := filepath.Join(f.work, path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) commit(msg string) string {
	f.sh(f.work, "add", "-A")
	f.sh(f.work, "commit", "--quiet", "-m", msg)
	f.sh(f.work, "push", "--quiet", "origin", "HEAD")
	return f.sh(f.work, "rev-parse", "HEAD")
}

func TestReadAPIs(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	r := f.g.Open(f.bare)

	if empty, _ := r.IsEmpty(ctx); !empty {
		t.Fatal("new repo should be empty")
	}
	f.write("README.md", "# Hello\n")
	f.write("src/main.go", "package main\n\nfunc main() {}\n")
	f.write("bin.dat", "a\x00b")
	c1 := f.commit("Initial commit\n\nWith a body.")

	f.write("src/main.go", "package main\n\nfunc main() {\n\tprintln(1)\n}\n")
	f.sh(f.work, "mv", "README.md", "README.markdown")
	f.sh(f.work, "rm", "-q", "bin.dat")
	c2 := f.commit("Second")
	f.sh(f.work, "tag", "-a", "v1.0", "-m", "release")
	f.sh(f.work, "push", "--quiet", "origin", "v1.0")
	f.sh(f.work, "checkout", "-q", "-b", "feature")
	f.write("new file.txt", "x\n")
	c3 := f.commit("Feature")
	f.sh(f.work, "push", "--quiet", "origin", "feature")

	if empty, _ := r.IsEmpty(ctx); empty {
		t.Fatal("repo should not be empty")
	}
	head, err := r.ResolveCommit(ctx, "main")
	if err != nil || head != c2 {
		t.Fatalf("resolve main = %s %v, want %s", head, err, c2)
	}
	if _, err := r.ResolveCommit(ctx, "--upload-pack=evil"); err != ErrNotFound {
		t.Fatal("option-like ref accepted")
	}

	branches, err := r.Branches(ctx)
	if err != nil || len(branches) != 2 || branches[0].Name != "feature" || branches[0].Subject != "Feature" {
		t.Fatalf("branches = %+v %v", branches, err)
	}
	tags, err := r.Tags(ctx)
	if err != nil || len(tags) != 1 || tags[0].Name != "v1.0" || tags[0].Commit != c2 {
		t.Fatalf("tags = %+v %v (annotated tag must peel to commit)", tags, err)
	}

	tree, err := r.Tree(ctx, c1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 3 || tree[0].Type != "dir" || tree[0].Name != "src" {
		t.Fatalf("tree = %+v", tree)
	}
	sub, err := r.Tree(ctx, c1, "src")
	if err != nil || len(sub) != 1 || sub[0].Path != "src/main.go" || sub[0].Size == 0 {
		t.Fatalf("subtree = %+v %v", sub, err)
	}
	if _, err := r.Tree(ctx, c1, "README.md"); err != ErrNotDir {
		t.Fatalf("tree of file: %v", err)
	}
	if _, err := r.Tree(ctx, c1, "nope"); err != ErrNotFound {
		t.Fatalf("tree of missing: %v", err)
	}

	b, err := r.ReadBlob(ctx, c1, "src/main.go", 10)
	if err != nil || !b.Truncated || len(b.Content) != 10 || b.Binary {
		t.Fatalf("blob = %+v %v", b, err)
	}
	b, err = r.ReadBlob(ctx, c1, "bin.dat", 1<<20)
	if err != nil || !b.Binary {
		t.Fatalf("binary blob = %+v %v", b, err)
	}
	if _, err := r.ReadBlob(ctx, c1, "src", 10); err != ErrNotFile {
		t.Fatalf("blob of dir: %v", err)
	}

	log, err := r.Log(ctx, c3, "", 0, 10)
	if err != nil || len(log) != 3 || log[0].SHA != c3 || log[2].Body != "With a body." || len(log[2].Parents) != 0 {
		t.Fatalf("log = %+v %v", log, err)
	}
	if log[0].Author.Name != "Alice" || log[0].Author.Date.IsZero() {
		t.Fatalf("author = %+v", log[0].Author)
	}
	pathLog, _ := r.Log(ctx, c3, "src/main.go", 0, 10)
	if len(pathLog) != 2 {
		t.Fatalf("path log len = %d", len(pathLog))
	}
	page2, _ := r.Log(ctx, c3, "", 2, 10)
	if len(page2) != 1 || page2[0].SHA != c1 {
		t.Fatal("log skip")
	}
	rng, _ := r.LogRange(ctx, "main", "feature", 10)
	if len(rng) != 1 || rng[0].SHA != c3 {
		t.Fatalf("range = %+v", rng)
	}

	d, err := r.Diff(ctx, c1, c2, DefaultDiffLimits)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]DiffFile{}
	for _, fd := range d.Files {
		byPath[fd.NewPath] = fd
	}
	if fd := byPath["README.markdown"]; fd.Status != "renamed" || fd.OldPath != "README.md" {
		t.Errorf("rename = %+v", fd)
	}
	if fd := byPath["bin.dat"]; fd.Status != "deleted" || !fd.Binary {
		t.Errorf("binary delete = %+v", fd)
	}
	mg := byPath["src/main.go"]
	if mg.Status != "modified" || mg.Additions != 3 || mg.Deletions != 1 || len(mg.Hunks) != 1 {
		t.Fatalf("modify = %+v", mg)
	}
	var sawAdd bool
	for _, l := range mg.Hunks[0].Lines {
		if l.Type == "add" && l.Content == "\tprintln(1)" && l.NewLine == 4 {
			sawAdd = true
		}
	}
	if !sawAdd {
		t.Fatalf("hunk lines = %+v", mg.Hunks[0].Lines)
	}
	root, err := r.Diff(ctx, EmptyTreeID, c1, DefaultDiffLimits)
	if err != nil || len(root.Files) != 3 || root.Files[0].Status != "added" {
		t.Fatalf("root diff = %+v %v", root, err)
	}
	pr, err := r.DiffMergeBase(ctx, "main", "feature", DefaultDiffLimits)
	if err != nil || len(pr.Files) != 1 || pr.Files[0].NewPath != "new file.txt" {
		t.Fatalf("merge-base diff = %+v %v", pr, err)
	}

	blame, err := r.Blame(ctx, c2, "src/main.go")
	if err != nil || len(blame) != 2 || blame[0].Commit != c1 || blame[1].Commit != c2 || blame[1].StartLine != 3 {
		t.Fatalf("blame = %+v %v", blame, err)
	}

	if ok, _ := r.IsAncestor(ctx, c1, c3); !ok {
		t.Fatal("c1 should be ancestor of c3")
	}
	if ok, err := r.IsAncestor(ctx, c3, c1); ok || err != nil {
		t.Fatalf("c3 ancestor of c1: %v %v", ok, err)
	}
	if p, _ := r.FindReadme(ctx, c2, ""); p != "README.markdown" {
		t.Fatalf("readme = %q", p)
	}
}

func TestParseDiffEdgeCases(t *testing.T) {
	input := `diff --git "a/sp\303\244ce\tx" "b/sp\303\244ce\tx"
index 1..2 100644
--- "a/sp\303\244ce\tx"
+++ "b/sp\303\244ce\tx"
@@ -1 +1 @@
-old
\ No newline at end of file
+new
\ No newline at end of file
diff --git a/big b/big
new file mode 100644
--- /dev/null
+++ b/big
@@ -0,0 +1,5 @@
+1
+2
+3
+4
+5
diff --git a/x b/x
old mode 100644
new mode 100755
`
	d, err := ParseDiff(strings.NewReader(input), DiffLimits{MaxLines: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 3 {
		t.Fatalf("files = %+v", d.Files)
	}
	f0 := d.Files[0]
	if f0.NewPath != "späce\tx" || !f0.Hunks[0].Lines[0].NoNewline || !f0.Hunks[0].Lines[1].NoNewline {
		t.Fatalf("quoted file = %+v", f0)
	}
	f1 := d.Files[1]
	if f1.Status != "added" || f1.Additions != 5 || !f1.Truncated || !d.Truncated || len(f1.Hunks[0].Lines) != 2 {
		t.Fatalf("truncated file = %+v", f1)
	}
	if d.Additions != 6 || d.Deletions != 1 {
		t.Fatalf("totals %d/%d", d.Additions, d.Deletions)
	}
	f2 := d.Files[2]
	if f2.NewPath != "x" || f2.OldMode != "100644" || f2.NewMode != "100755" || f2.Status != "modified" {
		t.Fatalf("mode change = %+v", f2)
	}
}

func TestValidation(t *testing.T) {
	for _, ok := range []string{"main", "feature/x", "v1.0", "abc123", "user@host"} {
		if !ValidRefName(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "-x", "a..b", "a b", "x.lock", "/x", "x/", "a//b", "a\x00"} {
		if ValidRefName(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
	for _, bad := range []string{"..", "a/../b", "./x", "a//b"} {
		if _, ok := CleanPath(bad); ok {
			t.Errorf("path %q should be rejected", bad)
		}
	}
	if p, ok := CleanPath("/src/x/"); !ok || p != "src/x" {
		t.Errorf("clean = %q", p)
	}
}
