package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"testing/fstest"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/config"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/secretbox"
)

type testEnv struct {
	t     *testing.T
	srv   *Server
	ts    *httptest.Server
	db    *db.DB
	token string // setup token
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Database.DSN = filepath.Join(cfg.DataDir, "tgp.db")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	d, err := db.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(ctx, d, log); err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	web := SPAHandler(fstest.MapFS{
		"index.html":        {Data: []byte("<html>app</html>")},
		"assets/app-123.js": {Data: []byte("console.log(1)")},
	})
	srv := New(Options{Config: cfg, DB: d, Logger: log, Web: web, Box: box})
	tok, err := srv.EnsureSetupToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &testEnv{t: t, srv: srv, ts: ts, db: d, token: tok}
}

// client is a browser-like client: it keeps cookies and echoes the CSRF
// cookie, unless a bearer token is set.
type client struct {
	env    *testEnv
	hc     *http.Client
	bearer string
	noCSRF bool
}

func (e *testEnv) browser() *client {
	jar, _ := cookiejar.New(nil)
	return &client{env: e, hc: &http.Client{Jar: jar}}
}

func (e *testEnv) withToken(tok string) *client {
	return &client{env: e, hc: &http.Client{}, bearer: tok}
}

type resp struct {
	status int
	body   []byte
}

func (r resp) code() string {
	var e struct {
		Error struct{ Code string } `json:"error"`
	}
	_ = json.Unmarshal(r.body, &e)
	return e.Error.Code
}

func (r resp) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func (c *client) do(method, path string, body any) resp {
	c.env.t.Helper()
	var rd io.Reader
	if b, ok := body.([]byte); ok {
		rd = bytes.NewReader(b)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.env.ts.URL+"/api/v1"+path, rd)
	if _, ok := body.([]byte); ok {
		req.Header.Set("Content-Type", "application/octet-stream")
	} else if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	} else if method != http.MethodGet && !c.noCSRF {
		// Like the web UI: fetch the CSRF cookie first if needed, then echo it.
		u, _ := url.Parse(c.env.ts.URL)
		if csrfValue(c.hc.Jar, u) == "" {
			c.do(http.MethodGet, "/meta", nil)
		}
		req.Header.Set(csrfHeader, csrfValue(c.hc.Jar, u))
	}
	res, err := c.hc.Do(req)
	if err != nil {
		c.env.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, body: b}
}

func csrfValue(jar http.CookieJar, u *url.URL) string {
	if jar == nil {
		return ""
	}
	for _, c := range jar.Cookies(u) {
		if c.Name == csrfCookie {
			return c.Value
		}
	}
	return ""
}

func (c *client) mustDo(method, path string, body any, want int) resp {
	c.env.t.Helper()
	r := c.do(method, path, body)
	if r.status != want {
		c.env.t.Fatalf("%s %s: status %d, want %d: %s", method, path, r.status, want, r.body)
	}
	return r
}

const adminPassword = "admin-password-123"

// setup completes first-start setup and returns a logged-in admin browser
// plus the temp passwords of extra users.
func (e *testEnv) setup(extra ...map[string]any) (*client, map[string]string) {
	e.t.Helper()
	c := e.browser()
	body := map[string]any{
		"setup_token": e.token,
		"admin":       map[string]any{"username": "Root", "password": adminPassword, "email": "root@example.com"},
		"org":         map[string]any{"name": "BloxBlast", "base_url": "https://git.example.com/"},
		"users":       extra,
	}
	r := c.mustDo("POST", "/setup", body, 200)
	var out struct {
		Users []struct {
			User         struct{ Username string } `json:"user"`
			TempPassword string                    `json:"temp_password"`
		} `json:"users"`
	}
	r.decode(e.t, &out)
	temps := map[string]string{}
	for _, u := range out.Users {
		temps[u.User.Username] = u.TempPassword
	}
	return c, temps
}

// login logs a fresh browser in and returns it.
func (e *testEnv) login(username, password string) *client {
	e.t.Helper()
	c := e.browser()
	c.mustDo("POST", "/auth/login", map[string]any{"username": username, "password": password}, 200)
	return c
}

// activeUser creates a user via the admin, logs in and sets a real password.
func (e *testEnv) activeUser(admin *client, username, role string, password string) *client {
	e.t.Helper()
	r := admin.mustDo("POST", "/admin/users", map[string]any{"username": username, "role": role}, 201)
	var out struct {
		TempPassword string `json:"temp_password"`
	}
	r.decode(e.t, &out)
	c := e.login(username, out.TempPassword)
	c.mustDo("POST", "/account/password", map[string]any{"current_password": out.TempPassword, "new_password": password}, 200)
	return c
}

func (e *testEnv) createToken(c *client, scopes ...string) string {
	e.t.Helper()
	r := c.mustDo("POST", "/account/tokens", map[string]any{"name": "t", "scopes": scopes}, 201)
	var out struct {
		Token string `json:"token"`
	}
	r.decode(e.t, &out)
	return out.Token
}
