package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

func TestSetupFlow(t *testing.T) {
	e := newEnv(t)
	anon := e.browser()

	var meta apigen.Meta
	anon.mustDo("GET", "/meta", nil, 200).decode(t, &meta)
	if !meta.SetupRequired {
		t.Fatal("setup should be required")
	}
	if r := anon.do("POST", "/auth/login", map[string]any{"username": "x", "password": "y"}); r.code() != "setup_required" {
		t.Fatalf("login before setup: %d %s", r.status, r.body)
	}

	bad := map[string]any{
		"setup_token": "wrong",
		"admin":       map[string]any{"username": "root", "password": adminPassword},
		"org":         map[string]any{"name": "X"},
	}
	if r := anon.do("POST", "/setup", bad); r.status != 403 {
		t.Fatalf("bad token: %d %s", r.status, r.body)
	}
	// Validation failures roll back completely and leave setup pending.
	weak := map[string]any{
		"setup_token": e.token,
		"admin":       map[string]any{"username": "root", "password": "short"},
		"org":         map[string]any{"name": "X"},
	}
	if r := anon.do("POST", "/setup", weak); r.code() != "validation_failed" || !strings.Contains(string(r.body), "admin:") {
		t.Fatalf("weak password: %d %s", r.status, r.body)
	}

	admin, temps := e.setup(
		map[string]any{"username": "alice", "role": "maintainer", "all_repos": true},
		map[string]any{"username": "bob", "role": "tester"},
	)
	if len(temps) != 2 || temps["alice"] == "" || temps["bob"] == "" {
		t.Fatalf("temps = %v", temps)
	}

	var sess apigen.Session
	admin.mustDo("GET", "/auth/session", nil, 200).decode(t, &sess)
	if sess.User.Username != "root" || sess.User.Role != "admin" || !sess.User.AllRepos {
		t.Fatalf("session = %+v", sess)
	}
	var org apigen.Org
	admin.mustDo("GET", "/org", nil, 200).decode(t, &org)
	if org.Name != "BloxBlast" || org.BaseUrl != "https://git.example.com" {
		t.Fatalf("org = %+v", org)
	}
	if r := anon.do("POST", "/setup", bad); r.status != 409 {
		t.Fatalf("second setup: %d %s", r.status, r.body)
	}
	anon.mustDo("GET", "/meta", nil, 200).decode(t, &meta)
	if meta.SetupRequired || meta.OrgName != "BloxBlast" {
		t.Fatalf("meta after setup = %+v", meta)
	}
	if tok, _ := e.srv.EnsureSetupToken(context.Background()); tok != "" {
		t.Fatal("no setup token should be issued after setup")
	}
}

func TestCSRF(t *testing.T) {
	e := newEnv(t)
	admin, _ := e.setup()
	admin.noCSRF = true
	if r := admin.do("POST", "/account/tokens", map[string]any{"name": "x", "scopes": []string{"read"}}); r.status != 403 || !strings.Contains(string(r.body), "CSRF") {
		t.Fatalf("missing CSRF header accepted: %d %s", r.status, r.body)
	}
	admin.noCSRF = false
	tok := e.createToken(admin, "read", "write")
	// Token clients never need CSRF.
	e.withToken(tok).mustDo("POST", "/account/tokens", map[string]any{"name": "y", "scopes": []string{"read"}}, 201)
}

func TestTempPasswordFlow(t *testing.T) {
	e := newEnv(t)
	admin, _ := e.setup()
	r := admin.mustDo("POST", "/admin/users", map[string]any{"username": "carol", "role": "maintainer", "email": "carol@example.com"}, 201)
	var created apigen.UserWithTempPassword
	r.decode(t, &created)
	if !created.User.MustChangePassword || created.TempPassword == "" {
		t.Fatalf("created = %+v", created)
	}

	c := e.browser()
	var sess apigen.Session
	c.mustDo("POST", "/auth/login", map[string]any{"username": "carol@example.com", "password": created.TempPassword}, 200).decode(t, &sess)
	if !sess.MustChangePassword {
		t.Fatal("login should report must_change_password")
	}
	if r := c.do("GET", "/users", nil); r.code() != "password_change_required" {
		t.Fatalf("pending user reached /users: %d %s", r.status, r.body)
	}
	other := e.login("carol", created.TempPassword)

	if r := c.do("POST", "/account/password", map[string]any{"current_password": created.TempPassword, "new_password": "short"}); r.code() != "validation_failed" {
		t.Fatalf("weak new password: %s", r.body)
	}
	if r := c.do("POST", "/account/password", map[string]any{"current_password": created.TempPassword, "new_password": created.TempPassword}); r.code() != "validation_failed" {
		t.Fatalf("same password: %s", r.body)
	}
	c.mustDo("POST", "/account/password", map[string]any{"current_password": created.TempPassword, "new_password": "carols-new-password"}, 200)
	c.mustDo("GET", "/users", nil, 200)
	// The other session was signed out.
	if r := other.do("GET", "/auth/session", nil); r.status != 401 {
		t.Fatalf("other session survived password change: %d", r.status)
	}
	if r := e.browser().do("POST", "/auth/login", map[string]any{"username": "carol", "password": created.TempPassword}); r.status != 401 {
		t.Fatal("temp password still works")
	}
	e.login("carol", "carols-new-password")
}

func TestExpiredTempPassword(t *testing.T) {
	e := newEnv(t)
	admin, temps := e.setup(map[string]any{"username": "dave", "role": "tester"})
	_ = admin
	past := time.Now().Add(-time.Hour)
	if _, err := e.db.NewUpdate().Model((*model.User)(nil)).Set("temp_password_expires_at = ?", past).Where("username = 'dave'").Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := e.browser().do("POST", "/auth/login", map[string]any{"username": "dave", "password": temps["dave"]})
	if r.status != 401 || !strings.Contains(string(r.body), "expired") {
		t.Fatalf("expired temp password: %d %s", r.status, r.body)
	}
	// Admin reset issues a working one.
	var reset apigen.UserWithTempPassword
	admin.mustDo("POST", "/admin/users/dave/reset-password", nil, 200).decode(t, &reset)
	e.login("dave", reset.TempPassword)
}

func TestTOTPFlow(t *testing.T) {
	e := newEnv(t)
	admin, _ := e.setup()
	u := e.activeUser(admin, "erin", "maintainer", "erins-password-1")

	var setup apigen.TotpSetup
	u.mustDo("POST", "/account/totp", nil, 200).decode(t, &setup)
	if !strings.HasPrefix(setup.OtpauthUrl, "otpauth://totp/BloxBlast:erin") {
		t.Fatalf("otpauth url %s", setup.OtpauthUrl)
	}
	if r := u.do("POST", "/account/totp/enable", map[string]any{"code": "000000"}); r.code() != "validation_failed" {
		t.Fatalf("wrong code accepted: %s", r.body)
	}
	code, _ := auth.TOTPCode(setup.Secret, time.Now())
	var rc apigen.RecoveryCodes
	u.mustDo("POST", "/account/totp/enable", map[string]any{"code": code}, 200).decode(t, &rc)
	if len(rc.Codes) != 10 {
		t.Fatalf("recovery codes = %v", rc.Codes)
	}

	login := func(extra map[string]any) resp {
		body := map[string]any{"username": "erin", "password": "erins-password-1"}
		for k, v := range extra {
			body[k] = v
		}
		return e.browser().do("POST", "/auth/login", body)
	}
	if r := login(nil); r.code() != "totp_required" {
		t.Fatalf("login without code: %s", r.body)
	}
	// The enable code was consumed: replay fails.
	if r := login(map[string]any{"totp_code": code}); r.code() != "totp_required" {
		t.Fatalf("replayed code accepted: %d %s", r.status, r.body)
	}
	next, _ := auth.TOTPCode(setup.Secret, time.Now().Add(30*time.Second))
	if r := login(map[string]any{"totp_code": next}); r.status != 200 {
		t.Fatalf("valid code rejected: %s", r.body)
	}
	if r := login(map[string]any{"recovery_code": strings.ToUpper(rc.Codes[0])}); r.status != 200 {
		t.Fatalf("recovery code rejected: %s", r.body)
	}
	if r := login(map[string]any{"recovery_code": rc.Codes[0]}); r.status != 401 {
		t.Fatal("recovery code reused")
	}

	// Admin can reset a lost device.
	admin.mustDo("POST", "/admin/users/erin/reset-totp", nil, 200)
	if r := login(nil); r.status != 200 {
		t.Fatalf("login after reset-totp: %s", r.body)
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	e.setup()
	c := e.browser()
	for i := 0; i < 10; i++ {
		if r := c.do("POST", "/auth/login", map[string]any{"username": "root", "password": "nope"}); r.status != 401 {
			t.Fatalf("attempt %d: %d", i, r.status)
		}
	}
	r := c.do("POST", "/auth/login", map[string]any{"username": "root", "password": adminPassword})
	if r.code() != "rate_limited" {
		t.Fatalf("expected rate limit, got %d %s", r.status, r.body)
	}
}

func TestTokenScopes(t *testing.T) {
	e := newEnv(t)
	admin, _ := e.setup()
	m := e.activeUser(admin, "frank", "maintainer", "franks-password")

	ro := e.withToken(e.createToken(m, "read"))
	ro.mustDo("GET", "/users", nil, 200)
	var sess apigen.Session
	ro.mustDo("GET", "/auth/session", nil, 200).decode(t, &sess)
	if sess.AuthMethod != "token" || sess.TokenScopes == nil || (*sess.TokenScopes)[0] != "read" {
		t.Fatalf("token session = %+v", sess)
	}
	if r := ro.do("POST", "/account/tokens", map[string]any{"name": "x", "scopes": []string{"read"}}); r.status != 403 {
		t.Fatalf("read token wrote: %d", r.status)
	}
	if r := m.do("POST", "/account/tokens", map[string]any{"name": "x", "scopes": []string{"admin"}}); r.status != 403 {
		t.Fatal("maintainer created admin-scoped token")
	}
	// A write-only token can't mint an admin token even for an admin.
	aw := e.withToken(e.createToken(admin, "read", "write"))
	if r := aw.do("POST", "/account/tokens", map[string]any{"name": "x", "scopes": []string{"admin"}}); r.status != 403 {
		t.Fatal("token escalated to admin scope")
	}
	if r := aw.do("GET", "/admin/users", nil); r.status != 403 {
		t.Fatal("admin endpoint without admin scope")
	}
	e.withToken(e.createToken(admin, "read", "admin")).mustDo("GET", "/admin/users", nil, 200)

	if r := e.withToken("tgp_pat_bogus").do("GET", "/users", nil); r.status != 401 {
		t.Fatal("bogus token accepted")
	}

	// Revocation.
	var toks []apigen.AccessToken
	m.mustDo("GET", "/account/tokens", nil, 200).decode(t, &toks)
	m.mustDo("DELETE", "/account/tokens/"+itoa(toks[0].Id), nil, 204)
	if r := ro.do("GET", "/users", nil); r.status != 401 {
		t.Fatal("revoked token still works")
	}

	// Disabling a user kills their tokens.
	rw := e.withToken(e.createToken(m, "read", "write"))
	admin.mustDo("PATCH", "/admin/users/frank", map[string]any{"disabled": true}, 200)
	if r := rw.do("GET", "/users", nil); r.status != 401 {
		t.Fatal("disabled user's token works")
	}
	if r := m.do("GET", "/users", nil); r.status != 401 {
		t.Fatal("disabled user's session works")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestAdminGuards(t *testing.T) {
	e := newEnv(t)
	admin, _ := e.setup()
	m := e.activeUser(admin, "gina", "maintainer", "ginas-password")
	tester := e.activeUser(admin, "hank", "tester", "hanks-password")
	for _, c := range []*client{m, tester} {
		if r := c.do("GET", "/admin/users", nil); r.status != 403 {
			t.Fatalf("non-admin reached admin users: %d", r.status)
		}
		if r := c.do("PATCH", "/admin/org", map[string]any{"name": "x"}); r.status != 403 {
			t.Fatal("non-admin updated org")
		}
	}
	if r := admin.do("PATCH", "/admin/users/root", map[string]any{"role": "tester"}); r.status != 403 {
		t.Fatal("admin demoted self")
	}
	if r := admin.do("PATCH", "/admin/users/root", map[string]any{"disabled": true}); r.status != 403 {
		t.Fatal("admin disabled self")
	}
	// Promote gina, then gina can't strand the org by demoting root and herself.
	admin.mustDo("PATCH", "/admin/users/gina", map[string]any{"role": "admin"}, 200)
	var u apigen.User
	admin.mustDo("GET", "/admin/users/gina", nil, 200).decode(t, &u)
	if u.Role != "admin" || !u.AllRepos {
		t.Fatalf("promoted user = %+v", u)
	}
	m.mustDo("PATCH", "/admin/users/root", map[string]any{"role": "maintainer", "all_repos": false}, 200)
	if r := admin.do("GET", "/admin/users", nil); r.status != 403 {
		t.Fatal("demoted admin kept access")
	}
	if r := m.do("POST", "/admin/users", map[string]any{"username": "gina", "role": "tester"}); r.status != 409 {
		t.Fatalf("duplicate username: %d", r.status)
	}
	if r := m.do("POST", "/admin/users", map[string]any{"username": "Bad Name", "role": "tester"}); r.code() != "validation_failed" {
		t.Fatal("bad username accepted")
	}
	if r := m.do("POST", "/admin/users", map[string]any{"username": "zed", "role": "owner"}); r.status != 400 && r.status != 422 {
		t.Fatalf("bad role: %d", r.status)
	}
}

func TestAuditLogAndOrg(t *testing.T) {
	e := newEnv(t)
	admin, _ := e.setup(map[string]any{"username": "ivy", "role": "tester"})
	e.browser().do("POST", "/auth/login", map[string]any{"username": "ivy", "password": "wrong-password"})

	admin.mustDo("PATCH", "/admin/org", map[string]any{"name": "Blox", "description": "Minecraft network"}, 200)
	if r := admin.do("PATCH", "/admin/org", map[string]any{"base_url": "ftp://x"}); r.code() != "validation_failed" {
		t.Fatal("bad base url accepted")
	}

	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")
	admin.mustDo("PUT", "/admin/org/logo", png, 200)
	if r := admin.do("PUT", "/admin/org/logo", []byte("<svg></svg>")); r.code() != "validation_failed" {
		t.Fatalf("svg logo accepted: %s", r.body)
	}
	res, err := http.Get(e.ts.URL + "/api/v1/org/logo")
	if err != nil || res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("logo fetch: %v %v", res, err)
	}
	res.Body.Close()

	var page apigen.AuditPage
	admin.mustDo("GET", "/admin/audit-log?limit=100", nil, 200).decode(t, &page)
	seen := map[string]bool{}
	for _, en := range page.Entries {
		seen[en.Action] = true
	}
	for _, a := range []string{"setup.complete", "user.create", "auth.login_failed", "org.update"} {
		if !seen[a] {
			t.Errorf("audit log missing %s (have %v)", a, seen)
		}
	}
	admin.mustDo("GET", "/admin/audit-log?action=auth.login_failed", nil, 200).decode(t, &page)
	if len(page.Entries) != 1 || page.Entries[0].Target != "ivy" {
		t.Fatalf("filtered audit = %+v", page.Entries)
	}
	// Pagination.
	admin.mustDo("GET", "/admin/audit-log?limit=1", nil, 200).decode(t, &page)
	if len(page.Entries) != 1 || page.NextBefore == nil {
		t.Fatalf("page 1 = %+v", page)
	}
	first := page.Entries[0].Id
	admin.mustDo("GET", "/admin/audit-log?limit=1&before="+itoa(*page.NextBefore), nil, 200).decode(t, &page)
	if len(page.Entries) != 1 || page.Entries[0].Id >= first {
		t.Fatal("pagination not descending")
	}
}

func TestLogout(t *testing.T) {
	e := newEnv(t)
	admin, _ := e.setup()
	admin.mustDo("POST", "/auth/logout", nil, 204)
	if r := admin.do("GET", "/auth/session", nil); r.status != 401 {
		t.Fatal("session survived logout")
	}
}

// Every operation under /admin must be marked admin-only in the policy map.
func TestEveryAdminOperationIsGuarded(t *testing.T) {
	spec, err := apigen.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			id := strings.ToUpper(op.OperationID[:1]) + op.OperationID[1:]
			lvl, listed := operationAccess[id]
			if strings.HasPrefix(path, "/admin") && lvl != accessAdmin {
				t.Errorf("%s %s (%s) must be accessAdmin", method, path, id)
			}
			specPublic := op.Security != nil && len(*op.Security) == 0
			if specPublic != (listed && lvl == accessPublic) {
				t.Errorf("%s %s (%s): public in spec = %v, public in operationAccess = %v", method, path, id, specPublic, listed && lvl == accessPublic)
			}
		}
	}
}
