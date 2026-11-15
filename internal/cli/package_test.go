package cli

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestWorkflowLint(t *testing.T) {
	e := newCLIEnv(t) // no login needed: lint is offline
	dir := t.TempDir()
	e.git(dir, "init", "-q")
	t.Chdir(dir)
	_ = os.MkdirAll(filepath.Join(dir, ".tgp/workflows"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, ".github"), 0o755)
	good := "on: push\njobs:\n  image:\n    container: none\n    steps:\n      - uses: checkout\n      - uses: registry-login\n      - run: docker build -t \"$TGP_IMAGE/issues:g${TGP_SHA::12}\" .\n"
	bad := "on: push\njobs:\n  test:\n    container: none\n    services: {db: {image: mysql}}\n    steps:\n      - run: x\n        bogus: 1\n"
	_ = os.WriteFile(filepath.Join(dir, ".tgp/workflows/good.yml"), []byte(good), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".github/labeler.yml"), []byte("issues: 'services/issues/**'\n"), 0o644)

	r := e.ok("", "workflow", "lint")
	if !strings.Contains(r.stdout, ".tgp/workflows/good.yml") || !strings.Contains(r.stdout, ".github/labeler.yml") {
		t.Fatalf("lint ok: %s", r.stdout)
	}
	var res LintResult
	e.jsonOK(&res, "workflow", "lint")
	if !res.Valid || len(res.Files) != 2 || res.Files[1].Kind != "labeler" {
		t.Fatalf("lint json: %+v", res)
	}

	_ = os.WriteFile(filepath.Join(dir, ".tgp/workflows/bad.yml"), []byte(bad), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".github/labeler.yml"), []byte("issues:\n  - branch: x\n"), 0o644)
	r = e.run("", "workflow", "lint")
	if r.code != ExitConflict {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	for _, want := range []string{`.tgp/workflows/bad.yml:8:9: jobs.test.steps[0]: unknown key "bogus"`, "can't have services", `.github/labeler.yml:2:5: issues: unknown key "branch"`} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("lint output lacks %q:\n%s", want, r.stdout)
		}
	}
	r = e.run("", "workflow", "lint", "--json")
	validateOutput(t, []string{"workflow", "lint", "--json"}, r.stdout)
	if r.code != ExitConflict || !strings.Contains(r.stdout, `"valid":false`) || r.stderr != "" {
		t.Fatalf("lint --json: exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	// Named files only.
	e.ok("", "workflow", "lint", ".tgp/workflows/good.yml")
	e.fail(ExitConflict, "", "workflow", "lint", "missing.yml")
}

func TestPackageCommands(t *testing.T) {
	e := newCLIEnv(t)
	e.admin.Do(http.MethodPost, "/repos", map[string]any{"name": "game", "auto_init": true}, nil)
	e.login("admin", "correct horse battery")
	tok := e.admin.Token("read", "write")
	u, _ := url.Parse(e.srv.URL)
	push := func(ref string) {
		t.Helper()
		r, err := name.ParseReference(u.Host+"/"+ref, name.Insecure)
		if err != nil {
			t.Fatal(err)
		}
		img, _ := random.Image(512, 1)
		if err := remote.Write(r, img, remote.WithAuth(&authn.Basic{Username: "admin", Password: tok})); err != nil {
			t.Fatalf("push %s: %v", ref, err)
		}
	}
	push("test-org/game/issues:g1")
	push("test-org/game/issues:master")
	push("test-org/game:latest")

	var list map[string]any
	e.jsonOK(&list, "package", "list", "-R", "game")
	imgs := list["images"].([]any)
	if list["namespace"] != "test-org" || len(imgs) != 2 {
		t.Fatalf("list: %v", list)
	}
	if r := e.ok("", "package", "list", "-R", "game"); !strings.Contains(r.stdout, u.Host+"/test-org/game/issues") {
		t.Fatalf("list human: %s", r.stdout)
	}
	var versions []map[string]any
	e.jsonOK(&versions, "package", "versions", "issues", "-R", "game")
	if len(versions) != 2 {
		t.Fatalf("versions: %v", versions)
	}
	e.fail(ExitUsage, CodeInputRequired, "package", "delete", "issues:g1", "-R", "game", "--json")
	e.jsonOK(nil, "package", "delete", "issues:g1", "-R", "game", "--yes")
	digest := versions[0]["digest"].(string)
	e.jsonOK(nil, "package", "delete", "issues@"+digest, "-R", "game", "--yes")
	e.jsonOK(nil, "package", "delete", ":latest", "-R", "game", "--yes")
	e.jsonOK(&versions, "package", "versions", "issues", "-R", "game")
	if len(versions) != 1 {
		t.Fatalf("versions after deletes: %v", versions)
	}
	e.fail(ExitNotFound, "not_found", "package", "delete", "issues:nope", "-R", "game", "--yes", "--json")
	e.fail(ExitUsage, CodeUsage, "package", "delete", "issues", "-R", "game", "--yes", "--json")

	var rs map[string]any
	e.jsonOK(&rs, "admin", "registry", "set", "quota_mb=100", "keep_last_tags_patterns=g*,sha-*")
	if rs["quota_mb"].(float64) != 100 || len(rs["keep_last_tags_patterns"].([]any)) != 2 {
		t.Fatalf("registry settings: %v", rs)
	}
	e.jsonOK(&rs, "admin", "registry", "view")
	e.fail(ExitUsage, CodeUsage, "admin", "registry", "set", "nope=1", "--json")
}
