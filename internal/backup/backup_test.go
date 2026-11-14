package backup_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/backup"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/storage"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/testserver"
)

func TestMain(m *testing.M) {
	testserver.RunHookIfRequested()
	os.Exit(m.Run())
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@x")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func cloneURL(srvURL, tok, repo string) string {
	u, _ := url.Parse(srvURL)
	u.User = url.UserPassword("admin", tok)
	u.Path = "/" + repo + ".git"
	return u.String()
}

// populate fills a server with a bit of everything a backup must keep.
func populate(t *testing.T, s *testserver.Server) (head string) {
	admin := s.Setup()
	admin.Do(http.MethodPost, "/repos", map[string]any{"name": "Game", "auto_init": true, "description": "Ünïcode ✓ 'quotes' ? marks"}, nil)
	admin.Do(http.MethodPost, "/repos", map[string]any{"name": "empty"}, nil)
	admin.ActivateUser("mia", "maintainer", "mia-password-1", true)
	admin.Do(http.MethodPost, "/repos/Game/issues", map[string]any{"title": "Crash on start", "body": "multi\nline ?"}, nil)
	admin.Do(http.MethodPut, "/admin/secrets/DEPLOY_KEY", map[string]any{"value": "s3cret"}, nil)
	rcv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(rcv.Close)
	admin.Do(http.MethodPost, "/repos/Game/webhooks", map[string]any{"url": rcv.URL, "events": []string{"push"}, "secret": "hooksecret"}, nil)
	var rel struct {
		ID int64 `json:"id"`
	}
	admin.Do(http.MethodPost, "/repos/Game/releases", map[string]any{"tag": "v1.0", "name": "First"}, &rel)
	res := admin.Send(http.MethodPost, "/repos/Game/releases/"+strconv.FormatInt(rel.ID, 10)+"/assets?name=game.zip", strings.NewReader("asset-bytes"), "application/octet-stream")
	res.Body.Close()
	if res.StatusCode != 201 {
		t.Fatalf("upload asset: %d", res.StatusCode)
	}
	// A second commit, pushed over HTTP.
	tok := admin.Token("read", "write")
	dir := t.TempDir()
	git(t, dir, "clone", "-q", cloneURL(s.URL, tok, "game"), "w")
	w := filepath.Join(dir, "w")
	if err := os.WriteFile(filepath.Join(w, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, w, "add", ".")
	git(t, w, "commit", "-qm", "Add main")
	git(t, w, "push", "-q", "origin", "HEAD")
	return git(t, w, "rev-parse", "HEAD")
}

func TestBackupRestore(t *testing.T) {
	src := testserver.Start(t)
	head := populate(t, src)
	ctx := context.Background()

	var buf bytes.Buffer
	m, st, err := backup.Create(ctx, &buf, backup.Options{Config: src.Config, DB: src.DB, Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	if m.Driver != "sqlite" || len(m.Migrations) == 0 || !m.MasterKey || !m.Storage || st.Repos != 2 || st.Blobs != 1 || st.Rows == 0 {
		t.Fatalf("manifest %+v stats %+v", m, st)
	}
	// users before repos before issues: parents load first.
	pos := map[string]int{}
	for i, tb := range m.Tables {
		pos[tb] = i
	}
	if !(pos["users"] < pos["repos"] && pos["repos"] < pos["issues"] && pos["issues"] < pos["pull_requests"]) {
		t.Fatalf("load order: %v", m.Tables)
	}
	if rm, err := backup.ReadManifest(bytes.NewReader(buf.Bytes())); err != nil || rm.Version != m.Version {
		t.Fatalf("read manifest: %v %+v", err, rm)
	}

	// Restore into a brand-new database and into one migrations already
	// seeded (as after starting a fresh server once).
	for _, premigrate := range []bool{false, true} {
		dir := t.TempDir()
		cfg := testserver.Config(dir)
		d, err := db.Open(ctx, cfg.Database)
		if err != nil {
			t.Fatal(err)
		}
		if premigrate {
			if err := db.Migrate(ctx, d, quiet); err != nil {
				t.Fatal(err)
			}
		}
		store, _ := storage.NewLocal(filepath.Join(dir, "storage"))
		ro := backup.RestoreOptions{Config: cfg, DB: d, Store: store, Log: quiet}
		_, rst, err := backup.Restore(ctx, bytes.NewReader(buf.Bytes()), ro)
		if err != nil {
			t.Fatalf("restore (premigrate=%v): %v", premigrate, err)
		}
		if rst.Rows != st.Rows || rst.Repos != 2 || rst.Blobs != 1 || !rst.MasterKey {
			t.Fatalf("restore stats %+v, backup %+v", rst, st)
		}
		// A second restore into the now non-empty target is refused.
		if _, _, err := backup.Restore(ctx, bytes.NewReader(buf.Bytes()), ro); !errors.Is(err, backup.ErrNotEmpty) {
			t.Fatalf("restore into non-empty target: %v", err)
		}
		d.Close()
		if key, _ := os.ReadFile(cfg.Security.MasterKeyFile); !bytes.Equal(key, testserver.MasterKey) {
			t.Fatal("master key not restored")
		}

		// A server on the restored data has everything.
		s := testserver.StartIn(t, dir)
		admin := s.Login("admin", testserver.AdminPassword)
		var repo struct {
			Name, Description string
		}
		admin.Do(http.MethodGet, "/repos/Game", nil, &repo)
		if repo.Name != "Game" || repo.Description != "Ünïcode ✓ 'quotes' ? marks" {
			t.Fatalf("repo: %+v", repo)
		}
		var issue struct {
			Title, Body string
		}
		admin.Do(http.MethodGet, "/repos/Game/issues/1", nil, &issue)
		if issue.Title != "Crash on start" || issue.Body != "multi\nline ?" {
			t.Fatalf("issue: %+v", issue)
		}
		var secrets []struct{ Name string }
		admin.Do(http.MethodGet, "/admin/secrets", nil, &secrets)
		if len(secrets) != 1 || secrets[0].Name != "DEPLOY_KEY" {
			t.Fatalf("secrets: %+v", secrets)
		}
		var hooks []struct {
			HasSecret bool `json:"has_secret"`
		}
		admin.Do(http.MethodGet, "/repos/Game/webhooks", nil, &hooks)
		if len(hooks) != 1 || !hooks[0].HasSecret {
			t.Fatalf("webhooks: %+v", hooks)
		}
		var tagged struct {
			ID     int64 `json:"id"`
			Assets []struct {
				ID int64 `json:"id"`
			} `json:"assets"`
		}
		admin.Do(http.MethodGet, "/repos/Game/releases/tags/v1.0", nil, &tagged)
		if len(tagged.Assets) != 1 {
			t.Fatalf("release: %+v", tagged)
		}
		code, body := admin.Raw(http.MethodGet, "/repos/Game/releases/"+strconv.FormatInt(tagged.ID, 10)+"/assets/"+strconv.FormatInt(tagged.Assets[0].ID, 10), nil)
		if code != 200 || string(body) != "asset-bytes" {
			t.Fatalf("asset: %d %q", code, body)
		}
		s.Login("mia", "mia-password-1")
		// New rows get fresh ids, and git has the pushed commit.
		admin.Do(http.MethodPost, "/repos/Game/issues", map[string]any{"title": "After restore"}, &issue)
		tok := admin.Token("read")
		cl := t.TempDir()
		git(t, cl, "clone", "-q", cloneURL(s.URL, tok, "game"), "c")
		if got := git(t, filepath.Join(cl, "c"), "rev-parse", "HEAD"); got != head {
			t.Fatalf("head %s, want %s", got, head)
		}
		git(t, filepath.Join(cl, "c"), "fsck", "--no-progress")
	}
}

func TestBackupOptions(t *testing.T) {
	src := testserver.Start(t)
	populate(t, src)
	ctx := context.Background()
	var buf bytes.Buffer
	m, st, err := backup.Create(ctx, &buf, backup.Options{Config: src.Config, DB: src.DB, Log: quiet, NoMasterKey: true, NoStorage: true, NoCaches: true})
	if err != nil {
		t.Fatal(err)
	}
	if m.MasterKey || m.Storage || m.Caches || st.Blobs != 0 || st.MasterKey {
		t.Fatalf("manifest %+v stats %+v", m, st)
	}
	// Not a backup at all.
	if _, err := backup.ReadManifest(strings.NewReader("nope")); err == nil {
		t.Fatal("garbage accepted")
	}
}
