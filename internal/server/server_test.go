package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newEnv(t).ts
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

func TestSPAContentSecurityPolicy(t *testing.T) {
	// sha256 of "a()", the inline script below.
	csp := contentSecurityPolicy([]byte(`<head><script>a()</script><script type="module" src="/assets/x.js"></script></head>`))
	for _, want := range []string{
		"script-src 'self' 'wasm-unsafe-eval' 'sha256-qVpDBgj7bpq5hMAcGp3AOc79J3Y1Z4HvySTwKrWDoy4='; ",
		"object-src 'none'", "frame-ancestors 'none'", "base-uri 'self'",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	if strings.Count(csp, "sha256-") != 1 {
		t.Errorf("external scripts hashed: %s", csp)
	}
	ts := newTestServer(t)
	if res, _ := get(t, ts.URL+"/repos"); !strings.Contains(res.Header.Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatalf("UI served without a CSP: %q", res.Header.Get("Content-Security-Policy"))
	}
}
