package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/testserver"
)

func TestAdminHealth(t *testing.T) {
	e := newCLIEnv(t)
	e.admin.Do(http.MethodPost, "/repos", map[string]any{"name": "game", "auto_init": true}, nil)
	e.admin.ActivateUser("mia", "maintainer", "mia-password-1", true)

	e.login("mia", "mia-password-1")
	e.fail(ExitForbidden, "forbidden", "admin", "health", "--json")

	e.login("admin", testserver.AdminPassword)
	// The exit code follows the status: 1 only when a check fails (the
	// test machine's disk may be nearly full, so either is possible).
	r := e.run("", "admin", "health", "--json")
	var h struct {
		Status string
		Checks []struct{ Name, Status string }
		Repos  struct{ Items []struct{ Name string } }
	}
	if err := json.Unmarshal([]byte(r.stdout), &h); err != nil {
		t.Fatalf("%v: %s %s", err, r.stdout, r.stderr)
	}
	validateOutput(t, []string{"admin", "health"}, r.stdout)
	wantExit := ExitOK
	if h.Status == "fail" {
		wantExit = ExitError
	}
	if r.code != wantExit || len(h.Checks) == 0 || len(h.Repos.Items) != 1 || h.Repos.Items[0].Name != "game" {
		t.Fatalf("exit %d, status %s: %s", r.code, h.Status, r.stdout)
	}

	r = e.run("", "admin", "health", "--refresh")
	for _, want := range []string{"Status:", "database", "sqlite", "storage", "game"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("text output lacks %q:\n%s", want, r.stdout)
		}
	}
	if r.code != wantExit {
		t.Fatalf("text exit %d", r.code)
	}
}
