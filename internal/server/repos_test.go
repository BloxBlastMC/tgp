package server

import (
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

// gitClient runs the real git CLI against the test server.
type gitClient struct {
	t    *testing.T
	env  *testEnv
	home string
}

func (e *testEnv) gitClient() *gitClient {
	return &gitClient{t: e.t, env: e, home: e.t.TempDir()}
}

// url returns a clone URL with the token embedded as the password.
func (g *gitClient) url(repo, token string) string {
	u, _ := url.Parse(g.env.ts.URL)
	if token != "" {
		u.User = url.UserPassword("x", token)
	}
	u.Path = "/" + repo + ".git"
	return u.String()
}

// run executes git in dir and returns combined output and error.
func (g *gitClient) run(dir string, args ...string) (string, error) {
	g.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + g.home, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Dev", "GIT_AUTHOR_EMAIL=dev@example.com", "GIT_COMMITTER_NAME=Dev", "GIT_COMMITTER_EMAIL=dev@example.com",
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (g *gitClient) must(dir string, args ...string) string {
	g.t.Helper()
	out, err := g.run(dir, args...)
	if err != nil {
		g.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func (g *gitClient) clone(repo, token string) string {
	g.t.Helper()
	dir := filepath.Join(g.t.TempDir(), repo)
	g.must("", "clone", "--quiet", g.url(repo, token), dir)
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRepoAccessAndGitTransport(t *testing.T) {
	e := newEnv(t)
	admin, _ := e.setup()
	maint := e.activeUser(admin, "mia", "maintainer", "mias-password-1")
	tester := e.activeUser(admin, "tom", "tester", "toms-password-1")
	outsider := e.activeUser(admin, "olga", "maintainer", "olgas-password")
	g := e.gitClient()

	// Only admins create repos.
	if r := maint.do("POST", "/repos", map[string]any{"name": "nope"}); r.status != 403 {
		t.Fatalf("maintainer created repo: %d", r.status)
	}
	var repo apigen.Repo
	admin.mustDo("POST", "/repos", map[string]any{"name": "Game", "description": "The game"}, 201).decode(t, &repo)
	if !repo.Empty || repo.DefaultBranch != "main" || !strings.HasSuffix(repo.CloneUrl, "/Game.git") || repo.Permission != "admin" {
		t.Fatalf("created = %+v", repo)
	}
	if r := admin.do("POST", "/repos", map[string]any{"name": "game"}); r.status != 409 {
		t.Fatal("case-insensitive duplicate accepted")
	}
	for _, bad := range []string{"admin", "x.git", "-x", "a/b", "settings"} {
		if r := admin.do("POST", "/repos", map[string]any{"name": bad}); r.code() != "validation_failed" {
			t.Errorf("bad name %q: %d %s", bad, r.status, r.body)
		}
	}

	// Without a grant the repo is invisible.
	var list []apigen.Repo
	maint.mustDo("GET", "/repos", nil, 200).decode(t, &list)
	if len(list) != 0 {
		t.Fatalf("ungranted maintainer sees %d repos", len(list))
	}
	if r := maint.do("GET", "/repos/game", nil); r.status != 404 {
		t.Fatalf("ungranted maintainer GET repo: %d", r.status)
	}

	// Grant: maintainer gets this repo, tester gets all repos.
	var access apigen.UserRepoAccess
	admin.mustDo("PUT", "/admin/users/mia/repos", map[string]any{"all_repos": false, "repos": []string{"game"}}, 200).decode(t, &access)
	if access.AllRepos || len(access.Repos) != 1 || access.Repos[0] != "Game" {
		t.Fatalf("access = %+v", access)
	}
	admin.mustDo("PUT", "/admin/users/tom/repos", map[string]any{"all_repos": true, "repos": []string{}}, 200)
	maint.mustDo("GET", "/repos/game", nil, 200).decode(t, &repo)
	if repo.Permission != "write" {
		t.Fatalf("maintainer permission = %s", repo.Permission)
	}
	var who []apigen.RepoAccess
	maint.mustDo("GET", "/repos/game/access", nil, 200).decode(t, &who)
	srcs := map[string]string{}
	for _, w := range who {
		srcs[w.User.Username] = string(w.Source) + "/" + string(w.Permission)
	}
	if srcs["root"] != "admin/admin" || srcs["mia"] != "grant/write" || srcs["tom"] != "all_repos/triage" || srcs["olga"] != "" {
		t.Fatalf("access list = %v", srcs)
	}

	mTok := e.createToken(maint, "read", "write")
	tTok := e.createToken(tester, "read", "write")
	oTok := e.createToken(outsider, "read", "write")
	roTok := e.createToken(maint, "read")

	// Unauthenticated and unauthorised git access.
	if out, err := g.run("", "ls-remote", g.url("game", "")); err == nil || !strings.Contains(out, "401") && !strings.Contains(out, "Authentication failed") && !strings.Contains(out, "could not read Username") {
		t.Fatalf("anonymous ls-remote: %v %s", err, out)
	}
	if out, err := g.run("", "ls-remote", g.url("game", oTok)); err == nil || !strings.Contains(out, "not found") {
		t.Fatalf("outsider ls-remote: %v %s", err, out)
	}

	// Maintainer pushes the first commit to the empty repo.
	work := g.clone("game", mTok)
	writeFile(t, work, "README.md", "# Game\n\nSee [docs](docs/guide.md).\n")
	writeFile(t, work, "src/main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, work, "page.html", "<script>alert(1)</script>\n")
	g.must(work, "add", "-A")
	g.must(work, "commit", "-q", "-m", "Initial import")
	g.must(work, "push", "-q", "origin", "main")

	maint.mustDo("GET", "/repos/game", nil, 200).decode(t, &repo)
	if repo.Empty || repo.PushedAt == nil {
		t.Fatalf("after push = %+v", repo)
	}

	// Code browsing.
	var tree apigen.Tree
	tester.mustDo("GET", "/repos/game/tree", nil, 200).decode(t, &tree)
	if tree.Ref != "main" || len(tree.Entries) != 3 || tree.Entries[0].Type != "dir" {
		t.Fatalf("tree = %+v", tree)
	}
	var blob apigen.Blob
	tester.mustDo("GET", "/repos/game/blob?path=src/main.go", nil, 200).decode(t, &blob)
	if blob.Encoding != "utf-8" || !strings.Contains(blob.Content, "func main") {
		t.Fatalf("blob = %+v", blob)
	}
	if r := tester.do("GET", "/repos/game/blob?path=../../etc/passwd", nil); r.code() != "validation_failed" {
		t.Fatalf("traversal: %d %s", r.status, r.body)
	}
	if r := tester.do("GET", "/repos/game/tree?ref=--output=/tmp/x", nil); r.status != 404 {
		t.Fatalf("option-like ref: %d", r.status)
	}
	var readme apigen.Readme
	tester.mustDo("GET", "/repos/game/readme", nil, 200).decode(t, &readme)
	if !strings.Contains(readme.Html, `href="/Game/blob?path=docs%2Fguide.md&amp;ref=main"`) {
		t.Fatalf("readme html = %s", readme.Html)
	}
	var commits apigen.CommitPage
	tester.mustDo("GET", "/repos/game/commits", nil, 200).decode(t, &commits)
	if len(commits.Commits) != 1 || commits.HasMore || commits.Commits[0].Subject != "Initial import" {
		t.Fatalf("commits = %+v", commits)
	}
	var detail apigen.CommitDetail
	tester.mustDo("GET", "/repos/game/commits/main", nil, 200).decode(t, &detail)
	if len(detail.Diff.Files) != 3 || detail.Diff.Additions != 7 {
		t.Fatalf("commit diff = %+v", detail.Diff)
	}
	var blame apigen.Blame
	tester.mustDo("GET", "/repos/game/blame?path=src/main.go", nil, 200).decode(t, &blame)
	if len(blame.Ranges) != 1 || len(blame.Ranges[0].Lines) != 3 {
		t.Fatalf("blame = %+v", blame)
	}
	// Raw never serves HTML as HTML.
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/v1/repos/game/raw?path=page.html", nil)
	req.Header.Set("Authorization", "Bearer "+tTok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" || !strings.Contains(res.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("raw html content-type = %q csp = %q", ct, res.Header.Get("Content-Security-Policy"))
	}

	// Tester can clone but not push.
	tWork := g.clone("game", tTok)
	writeFile(t, tWork, "x.txt", "x")
	g.must(tWork, "add", "-A")
	g.must(tWork, "commit", "-q", "-m", "tester change")
	if out, err := g.run(tWork, "push", "origin", "main"); err == nil || !strings.Contains(out, "can't push") {
		t.Fatalf("tester push: %v %s", err, out)
	}
	// Read-only token can't push.
	if out, err := g.run(work, "push", g.url("game", roTok), "main:ro-branch"); err == nil || !strings.Contains(out, "read-only") {
		t.Fatalf("read-only token push: %v %s", err, out)
	}

	// Pre-receive hook rules.
	if out, err := g.run(work, "push", "origin", "main:refs/pull/1/head"); err == nil || !strings.Contains(out, "reserved") {
		t.Fatalf("reserved ref push: %v %s", err, out)
	}
	if out, err := g.run(work, "push", "origin", ":main"); err == nil || !strings.Contains(out, "default branch") {
		t.Fatalf("delete default branch: %v %s", err, out)
	}
	g.must(work, "push", "-q", "origin", "main:feature")
	g.must(work, "push", "-q", "origin", "--delete", "feature")
	g.must(work, "tag", "v1")
	g.must(work, "push", "-q", "origin", "v1")
	var tags []apigen.Ref
	maint.mustDo("GET", "/repos/game/tags", nil, 200).decode(t, &tags)
	if len(tags) != 1 || tags[0].Name != "v1" {
		t.Fatalf("tags = %+v", tags)
	}

	// Archived repos reject pushes.
	if r := maint.do("PATCH", "/repos/game", map[string]any{"archived": true}); r.status != 403 {
		t.Fatal("maintainer archived repo")
	}
	admin.mustDo("PATCH", "/repos/game", map[string]any{"archived": true}, 200)
	writeFile(t, work, "y.txt", "y")
	g.must(work, "add", "-A")
	g.must(work, "commit", "-q", "-m", "after archive")
	if out, err := g.run(work, "push", "origin", "main"); err == nil || !strings.Contains(out, "archived") {
		t.Fatalf("push to archived: %v %s", err, out)
	}
	admin.mustDo("PATCH", "/repos/game", map[string]any{"archived": false}, 200)
	g.must(work, "push", "-q", "origin", "main")

	// Maintainers change the default branch; it must exist.
	if r := maint.do("PATCH", "/repos/game", map[string]any{"default_branch": "nope"}); r.code() != "validation_failed" {
		t.Fatal("missing default branch accepted")
	}
	g.must(work, "push", "-q", "origin", "main:develop")
	maint.mustDo("PATCH", "/repos/game", map[string]any{"default_branch": "develop", "description": "New"}, 200).decode(t, &repo)
	if repo.DefaultBranch != "develop" {
		t.Fatalf("default branch = %s", repo.DefaultBranch)
	}
	var branches []apigen.Ref
	maint.mustDo("GET", "/repos/game/branches", nil, 200).decode(t, &branches)
	if len(branches) != 2 || branches[0].Name != "develop" || !*branches[0].IsDefault {
		t.Fatalf("branches = %+v", branches)
	}

	// Rename moves storage and the clone URL.
	admin.mustDo("PATCH", "/repos/game", map[string]any{"name": "minigame"}, 200).decode(t, &repo)
	if !strings.HasSuffix(repo.CloneUrl, "/minigame.git") {
		t.Fatalf("clone url after rename = %s", repo.CloneUrl)
	}
	g.must("", "ls-remote", g.url("minigame", mTok))
	if r := maint.do("GET", "/repos/game", nil); r.status != 404 {
		t.Fatal("old name still resolves")
	}

	// Deleting removes it everywhere.
	if r := maint.do("DELETE", "/repos/minigame", nil); r.status != 403 {
		t.Fatal("maintainer deleted repo")
	}
	admin.mustDo("DELETE", "/repos/minigame", nil, 204)
	if out, err := g.run("", "ls-remote", g.url("minigame", mTok)); err == nil {
		t.Fatalf("deleted repo still served: %s", out)
	}
}

func TestFirstPushAdoptsBranchAndAutoInit(t *testing.T) {
	e := newEnv(t)
	admin, _ := e.setup()
	tok := e.createToken(admin, "read", "write")
	g := e.gitClient()

	admin.mustDo("POST", "/repos", map[string]any{"name": "legacy"}, 201)
	dir := t.TempDir()
	g.must(dir, "init", "-q", "-b", "master")
	writeFile(t, dir, "a.txt", "a")
	g.must(dir, "add", "-A")
	g.must(dir, "commit", "-q", "-m", "first")
	g.must(dir, "push", "-q", g.url("legacy", tok), "master")
	var repo apigen.Repo
	admin.mustDo("GET", "/repos/legacy", nil, 200).decode(t, &repo)
	if repo.DefaultBranch != "master" {
		t.Fatalf("default branch after first push = %s", repo.DefaultBranch)
	}
	admin.mustDo("GET", "/repos/legacy/tree", nil, 200)

	admin.mustDo("POST", "/repos", map[string]any{"name": "fresh", "auto_init": true, "description": "Hello"}, 201).decode(t, &repo)
	if repo.Empty {
		t.Fatal("auto_init repo is empty")
	}
	var blob apigen.Blob
	admin.mustDo("GET", "/repos/fresh/blob?path=README.md", nil, 200).decode(t, &blob)
	if blob.Content != "# fresh\n\nHello\n" {
		t.Fatalf("readme = %q", blob.Content)
	}
	work := g.clone("fresh", tok)
	if out := g.must(work, "log", "--format=%an <%ae>"); !strings.Contains(out, "root <root@example.com>") {
		t.Fatalf("auto-init author = %s", out)
	}

	// Empty repo browsing gives a clear 404.
	admin.mustDo("POST", "/repos", map[string]any{"name": "blank"}, 201)
	if r := admin.do("GET", "/repos/blank/tree", nil); r.status != 404 || !strings.Contains(string(r.body), "empty") {
		t.Fatalf("empty tree: %d %s", r.status, r.body)
	}
}

func TestHookEndpointRequiresSecret(t *testing.T) {
	e := newEnv(t)
	e.setup()
	res, err := http.Post(e.ts.URL+"/internal/hooks/pre-receive", "application/json", strings.NewReader(`{"repo_id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("hook without secret: %d", res.StatusCode)
	}
}
