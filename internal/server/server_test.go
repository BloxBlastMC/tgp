package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/config"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
)

func newTestServer(t *testing.T) *httptest.Server {
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
	web := SPAHandler(fstest.MapFS{
		"index.html":        {Data: []byte("<html>app</html>")},
		"assets/app-123.js": {Data: []byte("console.log(1)")},
	})
	ts := httptest.NewServer(New(Options{Config: cfg, DB: d, Logger: log, Web: web}).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	for _, p := range []string{"/healthz", "/readyz"} {
		if res, _ := get(t, ts.URL+p); res.StatusCode != 200 {
			t.Errorf("%s = %d", p, res.StatusCode)
		}
	}
}

func TestMetaViaGeneratedClient(t *testing.T) {
	ts := newTestServer(t)
	c, err := apigen.NewClientWithResponses(ts.URL + "/api/v1")
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.GetMetaWithResponse(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.JSON200 == nil {
		t.Fatalf("status %d body %s", res.StatusCode(), res.Body)
	}
	if !res.JSON200.SetupRequired {
		t.Error("fresh server should require setup")
	}
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	ts := newTestServer(t)
	for _, p := range []string{"/api/v1/nope", "/api/v2/x"} {
		res, body := get(t, ts.URL+p)
		if res.StatusCode != 404 {
			t.Fatalf("%s status = %d", p, res.StatusCode)
		}
		var e struct {
			Error struct{ Code string } `json:"error"`
		}
		if err := json.Unmarshal([]byte(body), &e); err != nil || e.Error.Code != "not_found" {
			t.Fatalf("%s body = %s", p, body)
		}
	}
}

func TestSPAFallback(t *testing.T) {
	ts := newTestServer(t)
	res, body := get(t, ts.URL+"/BloxBlast/repo/pulls/3")
	if res.StatusCode != 200 || !strings.Contains(body, "app") {
		t.Fatalf("fallback: %d %q", res.StatusCode, body)
	}
	res, _ = get(t, ts.URL+"/assets/app-123.js")
	if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %q", res.StatusCode, res.Header.Get("Cache-Control"))
	}
	if res, _ := get(t, ts.URL+"/assets/missing.js"); res.StatusCode != 404 {
		t.Fatalf("missing asset = %d", res.StatusCode)
	}
}
