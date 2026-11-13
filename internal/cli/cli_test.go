package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/testserver"
)

func TestLoginStatusTokenLogout(t *testing.T) {
	e := newCLIEnv(t)

	var st authStatus
	r := e.ok(testserver.AdminPassword+"\n", "auth", "login", "--host", e.srv.URL, "-u", "admin", "--password-stdin", "--json")
	validateOutput(t, []string{"auth", "login"}, r.stdout)
	if err := json.Unmarshal([]byte(r.stdout), &st); err != nil {
		t.Fatal(err)
	}
	if !st.LoggedIn || st.User != "admin" || st.Role != "admin" || st.TokenSource != "file" || !st.Default {
		t.Fatalf("status after login: %+v", st)
	}
	if strings.Join(st.TokenScopes, ",") != "read,write,admin" {
		t.Fatalf("admin token scopes = %v, want read,write,admin", st.TokenScopes)
	}

	// Token stored in hosts.toml (no keyring in tests) with private permissions.
	fi, err := os.Stat(filepath.Join(e.home, "tgp", "hosts.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("hosts.toml mode = %v, want 0600", fi.Mode().Perm())
	}

	var sts []authStatus
	e.jsonOK(&sts, "auth", "status")
	if len(sts) != 1 || !sts[0].LoggedIn || sts[0].User != "admin" {
		t.Fatalf("auth status: %+v", sts)
	}
	tok := strings.TrimSpace(e.ok("", "auth", "token").stdout)
	if !strings.HasPrefix(tok, "tgp_pat_") {
		t.Fatalf("auth token printed %q", tok)
	}

	// Logging out revokes the token the CLI created.
	var lo map[string]any
	e.jsonOK(&lo, "auth", "logout")
	if lo["token_revoked"] != true {
		t.Fatalf("logout: %v", lo)
	}
	if status, _ := e.srv.WithToken(tok).Raw(http.MethodGet, "/auth/session", nil); status != http.StatusUnauthorized {
		t.Fatalf("revoked token still works: %d", status)
	}
	e.fail(ExitAuth, CodeNotLoggedIn, "auth", "status", "--json")
	e.fail(ExitAuth, CodeNotLoggedIn, "repo", "list", "--json")
}

func TestLoginWithTokenAndEnvToken(t *testing.T) {
	e := newCLIEnv(t)
	e.admin.ActivateUser("mia", "maintainer", "mia-password-1", true)
	tok := e.srv.Login("mia", "mia-password-1").Token("read")

	r := e.ok(tok+"\n", "auth", "login", "--host", e.srv.URL, "--with-token", "--json")
	var st authStatus
	_ = json.Unmarshal([]byte(r.stdout), &st)
	if st.User != "mia" || strings.Join(st.TokenScopes, ",") != "read" {
		t.Fatalf("with-token login: %+v", st)
	}
	// Logout doesn't revoke a token the user supplied.
	var lo map[string]any
	e.jsonOK(&lo, "auth", "logout")
	if lo["token_revoked"] != false {
		t.Fatalf("logout revoked a user-supplied token: %v", lo)
	}
	if status, _ := e.srv.WithToken(tok).Raw(http.MethodGet, "/auth/session", nil); status != http.StatusOK {
		t.Fatalf("user token was revoked: %d", status)
	}

	// TGP_HOST + TGP_TOKEN work without any stored login.
	t.Setenv("TGP_HOST", e.srv.URL)
	t.Setenv("TGP_TOKEN", tok)
	var sts []authStatus
	e.jsonOK(&sts, "auth", "status")
	if len(sts) != 1 || sts[0].TokenSource != "env" || sts[0].User != "mia" {
		t.Fatalf("env status: %+v", sts)
	}
	e.ok("", "repo", "list")
	// A read-only token can't write.
	e.fail(ExitForbidden, "forbidden", "repo", "create", "x", "--json")
}

func TestLoginErrors(t *testing.T) {
	e := newCLIEnv(t)
	r := e.run("wrong\n", "auth", "login", "--host", e.srv.URL, "-u", "admin", "--password-stdin", "--json")
	if r.code != ExitAuth || r.errCode() != "unauthenticated" {
		t.Fatalf("wrong password: exit %d %s", r.code, r.stderr)
	}
	// Non-interactive without credentials: input_required, not a hang.
	e.fail(ExitUsage, CodeInputRequired, "auth", "login", "--host", e.srv.URL, "--json")
	e.fail(ExitUsage, CodeInputRequired, "auth", "login", "--json")
	// Not a tgp server.
	e.fail(ExitError, CodeNotTgp, "auth", "login", "--host", e.srv.URL+"/nope", "--with-token", "--json")
	// Server that hasn't been set up.
	fresh := testserver.Start(t)
	e.fail(ExitConflict, "setup_required", "auth", "login", "--host", fresh.URL, "--with-token", "--json")
	// Unreachable server.
	e.fail(ExitUnavailable, CodeNetwork, "auth", "login", "--host", "http://127.0.0.1:1", "--with-token", "--json")
	// Human errors are plain text with a hint.
	r = e.run("", "repo", "list")
	if r.code != ExitAuth || !strings.HasPrefix(r.stderr, "error: no tgp server configured") || !strings.Contains(r.stderr, "hint:") {
		t.Fatalf("human error: %d %q", r.code, r.stderr)
	}
}

func TestLoginWithTwoFactor(t *testing.T) {
	e := newCLIEnv(t)
	var setup struct{ Secret string }
	e.admin.Do(http.MethodPost, "/account/totp", nil, &setup)
	code, _ := totp.GenerateCode(setup.Secret, time.Now())
	var rc struct{ Codes []string }
	e.admin.Do(http.MethodPost, "/account/totp/enable", map[string]string{"code": code}, &rc)

	// Without a code and no terminal: ask for --otp.
	r := e.run(testserver.AdminPassword+"\n", "auth", "login", "--host", e.srv.URL, "-u", "admin", "--password-stdin", "--json")
	if r.code != ExitUsage || r.errCode() != CodeInputRequired || !strings.Contains(r.stderr, "--otp") {
		t.Fatalf("2FA without code: %d %s", r.code, r.stderr)
	}
	// A recovery code works through --otp.
	e.ok(testserver.AdminPassword+"\n", "auth", "login", "--host", e.srv.URL, "-u", "admin", "--password-stdin", "--otp", rc.Codes[0])
	// Interactive: prompted for the code.
	e.ok("", "auth", "logout")
	r = e.runTTY("admin\n"+testserver.AdminPassword+"\n"+rc.Codes[1]+"\nn\n", "auth", "login", "--host", e.srv.URL)
	if r.code != 0 || !strings.Contains(r.stderr, "2FA code") || !strings.Contains(r.stdout, "Logged in to") {
		t.Fatalf("interactive 2FA login: %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
}

func TestForcedPasswordChange(t *testing.T) {
	e := newCLIEnv(t)
	tmp := e.admin.CreateUser("nina", "tester", true)

	// Non-interactive: explain how to fix it.
	r := e.run(tmp+"\n", "auth", "login", "--host", e.srv.URL, "-u", "nina", "--password-stdin", "--json")
	if r.code != ExitAuth || r.errCode() != "password_change_required" {
		t.Fatalf("non-interactive temp password: %d %s", r.code, r.stderr)
	}
	// Interactive: mismatch, too short, then a good password; decline git setup.
	in := strings.Join([]string{tmp, "aaaaaaaaaaaa", "bbbbbbbbbbbb", "short", "short", "nina-new-password", "nina-new-password", "n", ""}, "\n")
	r = e.runTTY(in, "auth", "login", "--host", e.srv.URL, "-u", "nina")
	if r.code != 0 || !strings.Contains(r.stderr, "don't match") || !strings.Contains(r.stderr, "Password changed") {
		t.Fatalf("interactive password change: %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	e.srv.Login("nina", "nina-new-password") // the new password works
	var sts []authStatus
	e.jsonOK(&sts, "auth", "status")
	if sts[0].User != "nina" || strings.Join(sts[0].TokenScopes, ",") != "read,write" {
		t.Fatalf("tester status: %+v", sts[0])
	}
}

func TestRepoCommands(t *testing.T) {
	e := newCLIEnv(t)
	e.login("admin", testserver.AdminPassword)

	var repo map[string]any
	e.jsonOK(&repo, "repo", "create", "game-server", "-d", "Paper plugins", "--init")
	if repo["name"] != "game-server" || repo["empty"] != false || repo["clone_url"] != e.srv.URL+"/game-server.git" {
		t.Fatalf("created: %v", repo)
	}
	e.jsonOK(nil, "repo", "create", "website")
	e.fail(ExitConflict, "conflict", "repo", "create", "website", "--json")

	var repos []map[string]any
	e.jsonOK(&repos, "repo", "list")
	if len(repos) != 2 {
		t.Fatalf("list: %v", repos)
	}
	e.jsonOK(nil, "repo", "view", "game-server")
	e.fail(ExitNotFound, "not_found", "repo", "view", "nope", "--json")
	e.fail(ExitUsage, CodeNoRepo, "repo", "view", "--json")

	// Field selection: --json=a,b and the gh-style --json a,b.
	for _, args := range [][]string{
		{"repo", "view", "game-server", "--json=name,default_branch"},
		{"repo", "view", "game-server", "--json", "name,default_branch"},
		{"repo", "view", "--json", "name,default_branch", "game-server"},
	} {
		r := e.ok("", args...)
		if strings.TrimSpace(r.stdout) != `{"default_branch":"main","name":"game-server"}` {
			t.Fatalf("%v: %s", args, r.stdout)
		}
	}
	r := e.fail(ExitUsage, CodeUsage, "repo", "list", "--json=name,bogus")
	if !strings.Contains(r.stderr, "bogus") || !strings.Contains(r.stderr, "clone_url") {
		t.Fatalf("unknown field error should list the fields: %s", r.stderr)
	}
	// --jq prints strings raw.
	if r := e.ok("", "repo", "list", "--jq", ".[].name"); r.stdout != "game-server\nwebsite\n" && r.stdout != "website\ngame-server\n" {
		t.Fatalf("jq: %q", r.stdout)
	}
	e.fail(ExitUsage, CodeUsage, "repo", "list", "--jq", ".[")
	if r := e.ok("", "repo", "list", "--limit", "1", "--jq", "length"); r.stdout != "1\n" {
		t.Fatalf("limit: %q", r.stdout)
	}
	if r := e.ok("", "repo", "list", "-q", "web", "--jq", ".[].name"); r.stdout != "website\n" {
		t.Fatalf("search: %q", r.stdout)
	}

	e.jsonOK(&repo, "repo", "edit", "website", "-d", "Public site", "--archive")
	if repo["description"] != "Public site" || repo["archived"] != true {
		t.Fatalf("edit: %v", repo)
	}
	if r := e.ok("", "repo", "list", "--archived", "--jq", ".[].name"); r.stdout != "website\n" {
		t.Fatalf("--archived: %q", r.stdout)
	}
	e.jsonOK(&repo, "repo", "edit", "website", "--rename", "site", "--unarchive")
	if repo["name"] != "site" || repo["archived"] != false {
		t.Fatalf("rename: %v", repo)
	}
	e.fail(ExitUsage, CodeUsage, "repo", "edit", "site", "--json")
	e.fail(ExitUsage, CodeUsage, "repo", "edit", "site", "--archive", "--unarchive", "--json")

	// Delete needs --yes without a terminal, or the typed name in one.
	e.fail(ExitUsage, CodeInputRequired, "repo", "delete", "site", "--json")
	if r := e.runTTY("wrong\n", "repo", "delete", "site"); r.code != ExitError {
		t.Fatalf("delete with wrong name: %d %s", r.code, r.stderr)
	}
	if r := e.runTTY("site\n", "repo", "delete", "site"); r.code != 0 {
		t.Fatalf("interactive delete: %d %s", r.code, r.stderr)
	}
	var del map[string]any
	e.jsonOK(&del, "repo", "delete", "game-server", "--yes")
	if del["deleted"] != true {
		t.Fatalf("delete: %v", del)
	}
	if r := e.ok("", "repo", "list", "--jq", "length"); r.stdout != "0\n" {
		t.Fatalf("after delete: %q", r.stdout)
	}

	// Human output.
	e.ok("", "repo", "create", "docs", "--init")
	if r := e.ok("", "repo", "list"); !strings.Contains(r.stdout, "NAME") || !strings.Contains(r.stdout, "docs") {
		t.Fatalf("human list: %q", r.stdout)
	}
	if r := e.ok("", "repo", "view", "docs"); !strings.Contains(r.stdout, "default branch:") {
		t.Fatalf("human view: %q", r.stdout)
	}
}

func TestPermissionsAndExitCodes(t *testing.T) {
	e := newCLIEnv(t)
	e.admin.Do(http.MethodPost, "/repos", map[string]any{"name": "secret", "auto_init": true}, nil)
	e.admin.Do(http.MethodPost, "/repos", map[string]any{"name": "shared", "auto_init": true}, nil)
	e.admin.ActivateUser("tess", "tester", "tess-password-1", false, "shared")
	e.login("tess", "tess-password-1")

	if r := e.ok("", "repo", "list", "--jq", ".[].name"); r.stdout != "shared\n" {
		t.Fatalf("tester sees %q", r.stdout)
	}
	e.fail(ExitNotFound, "not_found", "repo", "view", "secret", "--json")
	e.fail(ExitForbidden, "forbidden", "repo", "create", "mine", "--json")
	e.fail(ExitForbidden, "forbidden", "repo", "edit", "shared", "-d", "x", "--json")
	e.fail(ExitForbidden, "forbidden", "admin", "user", "list", "--json")
	e.fail(ExitUsage, CodeUsage, "repo", "frobnicate", "--json")
	e.fail(ExitUsage, CodeUsage, "repo", "list", "--bogus", "--json")
}

func TestCloneAndGitCredentials(t *testing.T) {
	e := newCLIEnv(t)
	e.admin.Do(http.MethodPost, "/repos", map[string]any{"name": "game", "auto_init": true}, nil)
	e.admin.ActivateUser("max", "maintainer", "max-password-1", true)
	e.login("max", "max-password-1")

	var cl map[string]string
	e.jsonOK(&cl, "repo", "clone", "game")
	dir := filepath.Join(e.home, "game")
	if cl["directory"] != "game" {
		t.Fatalf("clone: %v", cl)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	// The clone pushes using the stored login via the credential helper.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.git(dir, "add", ".")
	e.git(dir, "commit", "-m", "add a")
	e.git(dir, "push", "origin", "HEAD")

	// Inside the clone, the repository comes from the git remote.
	t.Chdir(dir)
	var repo map[string]any
	e.jsonOK(&repo, "repo", "view")
	if repo["name"] != "game" || repo["empty"] != false {
		t.Fatalf("view from remote: %v", repo)
	}
	t.Chdir(e.home)

	// Extra git flags after --.
	e.ok("", "repo", "clone", "game", "shallow", "--", "--depth", "1")
	if out := e.git(filepath.Join(e.home, "shallow"), "rev-list", "--count", "HEAD"); strings.TrimSpace(out) != "1" {
		t.Fatalf("shallow clone has %s commits", out)
	}

	// setup-git configures the global helper; plain git clone then works.
	var gs map[string]any
	e.jsonOK(&gs, "auth", "setup-git")
	if out := e.git(e.home, "config", "--global", "--get-all", "credential."+e.srv.URL+".helper"); !strings.Contains(out, "auth git-credential") {
		t.Fatalf("global helper: %q", out)
	}
	e.git(e.home, "clone", e.srv.URL+"/game.git", "plain")

	// The helper answers only for known hosts.
	r := e.ok("protocol=http\nhost="+strings.TrimPrefix(e.srv.URL, "http://")+"\n\n", "auth", "git-credential", "get")
	if !strings.Contains(r.stdout, "username=max\n") || !strings.Contains(r.stdout, "password=tgp_pat_") {
		t.Fatalf("helper output: %q", r.stdout)
	}
	if r := e.ok("protocol=https\nhost=github.com\n\n", "auth", "git-credential", "get"); r.stdout != "" {
		t.Fatalf("helper answered for another host: %q", r.stdout)
	}
	e.ok("protocol=http\nhost=x\n\n", "auth", "git-credential", "store")
}

func TestTesterCantPushThroughCLIClone(t *testing.T) {
	e := newCLIEnv(t)
	e.admin.Do(http.MethodPost, "/repos", map[string]any{"name": "game", "auto_init": true}, nil)
	e.admin.ActivateUser("tess", "tester", "tess-password-1", true)
	e.login("tess", "tess-password-1")
	e.ok("", "repo", "clone", "game")
	dir := filepath.Join(e.home, "game")
	e.git(dir, "commit", "--allow-empty", "-m", "nope")
	if out, err := e.gitErr(dir, "push", "origin", "HEAD"); err == nil {
		t.Fatalf("tester push succeeded: %s", out)
	}
}

func TestEnvTokenClone(t *testing.T) {
	e := newCLIEnv(t)
	e.admin.Do(http.MethodPost, "/repos", map[string]any{"name": "game", "auto_init": true}, nil)
	t.Setenv("TGP_HOST", e.srv.URL)
	t.Setenv("TGP_TOKEN", e.admin.Token("read", "write"))
	e.ok("", "repo", "clone", "game")
	// The env token isn't written into the clone's config.
	cfg, _ := os.ReadFile(filepath.Join(e.home, "game", ".git", "config"))
	if strings.Contains(string(cfg), "tgp_pat_") {
		t.Fatal("token leaked into .git/config")
	}
}

func TestAPICommand(t *testing.T) {
	e := newCLIEnv(t)
	e.login("admin", testserver.AdminPassword)

	r := e.ok("", "api", "POST", "repos", "-f", "name=game", "-F", "auto_init=true")
	var repo map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &repo); err != nil || repo["empty"] != false {
		t.Fatalf("api POST: %s %v", r.stdout, err)
	}
	if r := e.ok("", "api", "repos/game", "--jq", ".default_branch"); r.stdout != "main\n" {
		t.Fatalf("api GET jq: %q", r.stdout)
	}
	if r := e.ok("", "api", "-X", "PATCH", "/repos/game", "-f", "description=hello", "--jq", ".description"); r.stdout != "hello\n" {
		t.Fatalf("api PATCH: %q", r.stdout)
	}
	// Fields become the query string for GET.
	if r := e.ok("", "api", "repos", "-X", "GET", "-f", "q=zzz"); strings.TrimSpace(r.stdout) != "[]" {
		t.Fatalf("api query: %q", r.stdout)
	}
	body := filepath.Join(e.home, "body.json")
	_ = os.WriteFile(body, []byte(`{"name":"other"}`), 0o600)
	e.ok("", "api", "POST", "repos", "--input", body)
	e.ok(`{"description":"from stdin"}`, "api", "PATCH", "repos/other", "--input", "-")
	if r := e.ok("", "api", "/api/v1/repos/other", "--json=description"); strings.TrimSpace(r.stdout) != `{"description":"from stdin"}` {
		t.Fatalf("absolute path + fields: %q", r.stdout)
	}
	if r := e.ok("", "api", "repos/game", "-i"); !strings.Contains(r.stdout, "200 OK") || !strings.Contains(r.stdout, "Content-Type: application/json") {
		t.Fatalf("include: %q", r.stdout)
	}
	// Non-JSON responses are written as-is.
	if r := e.ok("", "api", "repos/game/raw?path=README.md"); !strings.HasPrefix(r.stdout, "# game") {
		t.Fatalf("raw: %q", r.stdout)
	}
	if r := e.ok("", "api", "DELETE", "repos/other"); r.stdout != "" {
		t.Fatalf("204 printed %q", r.stdout)
	}
	r = e.fail(ExitNotFound, "not_found", "api", "repos/nope", "--json")
	if r.stdout != "" {
		t.Fatalf("error wrote to stdout: %q", r.stdout)
	}
	e.fail(ExitUsage, CodeUsage, "api", "repos", "-f", "novalue", "--json")
	e.fail(ExitConflict, "validation_failed", "api", "POST", "repos", "-f", "name=bad name!", "--json")
	// The spec is public.
	if r := e.ok("", "api", "/api/v1/openapi.json", "--jq", ".info.title"); r.stdout != "tgp API\n" {
		t.Fatalf("spec: %q", r.stdout)
	}
}

func TestAdminUserCommands(t *testing.T) {
	e := newCLIEnv(t)
	e.login("admin", testserver.AdminPassword)
	for _, n := range []string{"game", "site", "docs"} {
		e.ok("", "repo", "create", n)
	}

	var created struct {
		TempPassword string `json:"temp_password"`
		User         struct {
			Username string
			Role     string
		}
	}
	e.jsonOK(&created, "admin", "user", "create", "bob", "--role", "tester", "--name", "Bob", "--repos", "game,site")
	if created.TempPassword == "" || created.User.Role != "tester" {
		t.Fatalf("create: %+v", created)
	}
	var acc struct {
		AllRepos bool     `json:"all_repos"`
		Repos    []string `json:"repos"`
	}
	e.jsonOK(&acc, "admin", "user", "access", "bob")
	if acc.AllRepos || strings.Join(acc.Repos, ",") != "game,site" {
		t.Fatalf("access: %+v", acc)
	}
	e.jsonOK(&acc, "admin", "user", "access", "bob", "--grant", "docs", "--revoke", "game")
	if strings.Join(acc.Repos, ",") != "docs,site" {
		t.Fatalf("grant/revoke: %+v", acc)
	}
	e.jsonOK(&acc, "admin", "user", "access", "bob", "--all")
	if !acc.AllRepos || len(acc.Repos) != 2 {
		t.Fatalf("--all keeps grants: %+v", acc)
	}
	e.jsonOK(&acc, "admin", "user", "access", "bob", "--selected", "--set", "")
	if acc.AllRepos || len(acc.Repos) != 0 {
		t.Fatalf("--set '': %+v", acc)
	}
	e.fail(ExitNotFound, "not_found", "admin", "user", "access", "bob", "--grant", "nope", "--json")

	var u map[string]any
	e.jsonOK(&u, "admin", "user", "edit", "bob", "--role", "maintainer", "--disable")
	if u["role"] != "maintainer" || u["disabled"] != true {
		t.Fatalf("edit: %v", u)
	}
	e.fail(ExitUsage, CodeUsage, "admin", "user", "edit", "bob", "--role", "god", "--json")
	e.fail(ExitUsage, CodeUsage, "admin", "user", "edit", "bob", "--json")
	e.jsonOK(nil, "admin", "user", "edit", "bob", "--enable")
	e.jsonOK(nil, "admin", "user", "view", "bob")
	e.jsonOK(&created, "admin", "user", "reset-password", "bob")
	e.srv.Login("bob", created.TempPassword)
	e.fail(ExitConflict, "conflict", "admin", "user", "reset-2fa", "bob", "--json")
	var users []map[string]any
	e.jsonOK(&users, "admin", "user", "list")
	if len(users) != 2 {
		t.Fatalf("users: %v", users)
	}
	if r := e.ok("", "admin", "user", "create", "carol", "--role", "maintainer", "--all-repos"); !strings.Contains(r.stdout, "Temporary password:") {
		t.Fatalf("human create: %q", r.stdout)
	}
	var page struct {
		Entries []map[string]any
	}
	e.jsonOK(&page, "admin", "audit", "--action", "user.create")
	if len(page.Entries) != 2 {
		t.Fatalf("audit: %v", page.Entries)
	}
	e.ok("", "admin", "audit")
}

func TestSchemaCommand(t *testing.T) {
	e := newCLIEnv(t)
	var index []map[string]string
	e.jsonOK(&index, "schema")
	if len(index) < 20 {
		t.Fatalf("schema index has %d entries", len(index))
	}
	// Every command's schema resolves, with every $ref defined in $defs.
	root := newRootCmd(&app{io: &IOStreams{}})
	for _, c := range commandsWithOutput(root) {
		s, err := jsonSchema(c)
		if err != nil {
			t.Fatalf("%s: %v", c.CommandPath(), err)
		}
		b, _ := json.Marshal(s)
		defs := s["$defs"].(map[string]any)
		for _, part := range strings.Split(string(b), `"$ref":"#/$defs/`)[1:] {
			name := part[:strings.IndexByte(part, '"')]
			if defs[name] == nil {
				t.Fatalf("%s: $ref to undefined %s", c.CommandPath(), name)
			}
		}
		if strings.Contains(string(b), "#/components/") || strings.Contains(string(b), `"nullable"`) {
			t.Fatalf("%s: OpenAPI-isms left in JSON Schema: %s", c.CommandPath(), b)
		}
	}
	r := e.ok("", "schema", "repo", "list")
	var s map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &s); err != nil || s["type"] != "array" {
		t.Fatalf("schema repo list: %s", r.stdout)
	}
	if r := e.ok("", "schema", "repo", "view", "--jq", `."$defs".Repo.properties.pushed_at.type`); r.stdout != "[\"string\",\"null\"]\n" {
		t.Fatalf("nullable conversion: %q", r.stdout)
	}
	e.fail(ExitUsage, CodeUsage, "schema", "nope", "--json")
	e.fail(ExitUsage, CodeUsage, "schema", "auth", "token", "--json")
	// Help lists JSON fields.
	if r := e.ok("", "repo", "view", "--help"); !strings.Contains(r.stdout, "JSON fields: archived, clone_url") {
		t.Fatalf("help: %q", r.stdout)
	}
	e.jsonOK(nil, "version")
}
