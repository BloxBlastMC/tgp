// Package testserver starts a real tgp server on a temp dir for tests in
// other packages (the CLI tests use it). It is only imported from tests.
package testserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/config"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/git"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/githook"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/secretbox"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/server"
)

// RunHookIfRequested must be called first thing in TestMain of any package
// that uses Start: the server's git hooks exec the test binary with
// `hook <kind>` arguments.
func RunHookIfRequested() {
	if len(os.Args) == 3 && os.Args[1] == "hook" {
		os.Exit(githook.Run(os.Args[2], os.Stdin, os.Stderr))
	}
}

// Server is a running test server.
type Server struct {
	t          *testing.T
	URL        string
	SetupToken string
	// App is the server itself, for tests that drive background work.
	App *server.Server
	// Config and DB are the server's, for tests that work on its data.
	Config *config.Config
	DB     *db.DB
}

// MasterKey is the test servers' secrets key; Config(dir) writes it to
// the master key file.
var MasterKey = bytes.Repeat([]byte{7}, 32)

// Config returns the configuration test servers use for a data dir.
func Config(dir string) *config.Config {
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.Database.DSN = filepath.Join(dir, "tgp.db")
	cfg.Repos.Root = filepath.Join(dir, "repos")
	cfg.Security.MasterKeyFile = filepath.Join(dir, "master.key")
	blockPrivate := false // webhook receivers in tests listen on loopback
	cfg.Webhooks.BlockPrivateNetworks = &blockPrivate
	return cfg
}

// Start runs a server backed by SQLite in a temp dir until the test ends.
func Start(t *testing.T) *Server {
	t.Helper()
	return StartIn(t, t.TempDir())
}

// StartIn runs a server on an existing (or new) data dir until the test
// ends.
func StartIn(t *testing.T, dir string) *Server {
	t.Helper()
	cfg := Config(dir)
	if _, err := os.Stat(cfg.Security.MasterKeyFile); err != nil {
		if err := os.WriteFile(cfg.Security.MasterKeyFile, MasterKey, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var logOut io.Writer = io.Discard
	if os.Getenv("TGP_TEST_SERVER_LOG") != "" {
		logOut = os.Stderr
	}
	log := slog.New(slog.NewTextHandler(logOut, nil))
	ctx := context.Background()
	d, err := db.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(ctx, d, log); err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.New(MasterKey)
	if err != nil {
		t.Fatal(err)
	}
	g, err := git.New("git", filepath.Join(cfg.DataDir, "git-home"))
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hooks := filepath.Join(cfg.DataDir, "hooks")
	if err := githook.WriteScripts(hooks, exe); err != nil {
		t.Fatal(err)
	}
	web := server.SPAHandler(fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}})
	srv := server.New(server.Options{Config: cfg, DB: d, Logger: log, Web: web, Box: box, Git: g, HooksDir: hooks})
	tok, err := srv.EnsureSetupToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	srv.SetInternalURL(ts.URL)
	return &Server{t: t, URL: ts.URL, SetupToken: tok, App: srv, Config: cfg, DB: d}
}

// Client is a minimal API client for arranging test state.
type Client struct {
	s      *Server
	hc     *http.Client
	bearer string
}

// Do sends a JSON request to /api/v1 and decodes a JSON response into out
// (if non-nil). It fails the test on any non-2xx status.
func (c *Client) Do(method, path string, body, out any) {
	c.s.t.Helper()
	status, data := c.Raw(method, path, body)
	if status >= 300 {
		c.s.t.Fatalf("%s %s: %d %s", method, path, status, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			c.s.t.Fatalf("%s %s: decode %s: %v", method, path, data, err)
		}
	}
}

// Raw sends a request and returns the status and body.
func (c *Client) Raw(method, path string, body any) (int, []byte) {
	c.s.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.s.URL+"/api/v1"+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	} else if c.hc.Jar != nil {
		for _, ck := range c.hc.Jar.Cookies(req.URL) {
			if ck.Name == "tgp_csrf" {
				req.Header.Set("X-CSRF-Token", ck.Value)
			}
		}
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.s.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

// Send sends a request with a raw body (nil for none) and content type and
// returns the response; the caller closes its body.
func (c *Client) Send(method, path string, body io.Reader, contentType string) *http.Response {
	c.s.t.Helper()
	req, _ := http.NewRequest(method, c.s.URL+"/api/v1"+path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	} else if c.hc.Jar != nil {
		for _, ck := range c.hc.Jar.Cookies(req.URL) {
			if ck.Name == "tgp_csrf" {
				req.Header.Set("X-CSRF-Token", ck.Value)
			}
		}
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.s.t.Fatal(err)
	}
	return resp
}

func (s *Server) browser() *Client {
	jar, _ := cookiejar.New(nil)
	c := &Client{s: s, hc: &http.Client{Jar: jar}}
	c.Raw(http.MethodGet, "/meta", nil) // obtain the CSRF cookie
	return c
}

// AdminPassword is the password of the "admin" user created by Setup.
const AdminPassword = "correct horse battery"

// Setup completes first-start setup with admin user "admin" and the org
// base URL pointing at the test server. It returns a logged-in admin client.
func (s *Server) Setup() *Client {
	s.t.Helper()
	c := s.browser()
	c.Do(http.MethodPost, "/setup", map[string]any{
		"setup_token": s.SetupToken,
		"admin":       map[string]any{"username": "admin", "password": AdminPassword, "display_name": "Ada Admin"},
		"org":         map[string]any{"name": "Test Org", "base_url": s.URL},
	}, nil)
	return c
}

// Login returns a client with a session for username.
func (s *Server) Login(username, password string) *Client {
	s.t.Helper()
	c := s.browser()
	c.Do(http.MethodPost, "/auth/login", map[string]any{"username": username, "password": password}, nil)
	return c
}

// CreateUser creates a user as admin and returns the temporary password.
// grants lists repositories for non-admins without all_repos.
func (c *Client) CreateUser(username, role string, allRepos bool, grants ...string) string {
	c.s.t.Helper()
	var out struct {
		TempPassword string `json:"temp_password"`
	}
	c.Do(http.MethodPost, "/admin/users", map[string]any{"username": username, "role": role, "all_repos": allRepos}, &out)
	if len(grants) > 0 {
		c.Do(http.MethodPut, "/admin/users/"+username+"/repos", map[string]any{"all_repos": allRepos, "repos": grants}, nil)
	}
	return out.TempPassword
}

// ActivateUser creates a user and sets their password, so they can log in
// without the forced password change.
func (c *Client) ActivateUser(username, role, password string, allRepos bool, grants ...string) {
	c.s.t.Helper()
	tmp := c.CreateUser(username, role, allRepos, grants...)
	u := c.s.Login(username, tmp)
	u.Do(http.MethodPost, "/account/password", map[string]any{"current_password": tmp, "new_password": password}, nil)
}

// Token creates a personal access token for the client's user.
func (c *Client) Token(scopes ...string) string {
	c.s.t.Helper()
	var out struct {
		Token string `json:"token"`
	}
	c.Do(http.MethodPost, "/account/tokens", map[string]any{"name": fmt.Sprintf("test %v", scopes), "scopes": scopes}, &out)
	return out.Token
}

// WithToken returns a client that authenticates with a bearer token.
func (s *Server) WithToken(tok string) *Client {
	return &Client{s: s, hc: &http.Client{}, bearer: tok}
}
