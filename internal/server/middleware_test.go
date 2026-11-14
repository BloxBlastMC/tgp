package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRealIP(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string][]string
		want    string
	}{
		{"no headers", nil, "10.0.0.9"},
		{"true-client-ip ignored", map[string][]string{"True-Client-Ip": {"6.6.6.6"}}, "10.0.0.9"},
		{"true-client-ip loses to xff", map[string][]string{"True-Client-Ip": {"6.6.6.6"}, "X-Forwarded-For": {"203.0.113.7"}}, "203.0.113.7"},
		{"rightmost xff wins", map[string][]string{"X-Forwarded-For": {"6.6.6.6, 203.0.113.7"}}, "203.0.113.7"},
		{"last xff header wins", map[string][]string{"X-Forwarded-For": {"6.6.6.6", "203.0.113.7"}}, "203.0.113.7"},
		{"xff beats x-real-ip", map[string][]string{"X-Forwarded-For": {"203.0.113.7"}, "X-Real-Ip": {"6.6.6.6"}}, "203.0.113.7"},
		{"x-real-ip fallback", map[string][]string{"X-Real-Ip": {"203.0.113.8"}}, "203.0.113.8"},
		{"garbage rightmost xff", map[string][]string{"X-Forwarded-For": {"203.0.113.7, nonsense"}}, "10.0.0.9"},
		{"ipv6", map[string][]string{"X-Forwarded-For": {"2001:db8::1"}}, "2001:db8::1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got string
			h := realIP(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = clientIP(r) }))
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = "10.0.0.9:51234"
			for k, vs := range c.headers {
				for _, v := range vs {
					req.Header.Add(k, v)
				}
			}
			h.ServeHTTP(httptest.NewRecorder(), req)
			if got != c.want {
				t.Fatalf("client ip = %q, want %q", got, c.want)
			}
		})
	}
}

// Proxy headers only count when trust_proxy is on.
func TestProxyHeadersNeedTrust(t *testing.T) {
	e := newEnv(t)
	for _, trust := range []bool{false, true} {
		e.srv.cfg.Server.TrustProxy = trust
		h := e.srv.Handler()
		req := httptest.NewRequest("GET", "/healthz", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		got := rec.Header().Get("Strict-Transport-Security")
		if trust && got != "max-age=31536000" {
			t.Errorf("trusted https: HSTS = %q", got)
		}
		if !trust && got != "" {
			t.Errorf("untrusted X-Forwarded-Proto produced HSTS %q", got)
		}
	}
}

func TestHSTS(t *testing.T) {
	e := newEnv(t)
	e.srv.cfg.Server.TrustProxy = true
	h := e.srv.Handler()
	for _, path := range []string{"/api/v1/openapi.json", "/", "/healthz"} {
		for _, proto := range []string{"", "http", "https"} {
			req := httptest.NewRequest("GET", path, nil)
			if proto != "" {
				req.Header.Set("X-Forwarded-Proto", proto)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			got := rec.Header().Get("Strict-Transport-Security")
			if want := proto == "https"; want != (got != "") {
				t.Errorf("%s proto=%q: HSTS = %q", path, proto, got)
			}
		}
	}
}
