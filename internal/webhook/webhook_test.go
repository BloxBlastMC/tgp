package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestSignVerify(t *testing.T) {
	// Same algorithm as GitHub's X-Hub-Signature-256.
	if got := Sign("It's a Secret to Everybody", []byte("Hello, World!")); got != "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17" {
		t.Fatal(got)
	}
	if !Verify("s", []byte("x"), Sign("s", []byte("x"))) || Verify("s", []byte("y"), Sign("s", []byte("x"))) {
		t.Fatal("verify")
	}
}

func TestValidateURL(t *testing.T) {
	for _, ok := range []string{"https://example.com/hook", "http://10.0.0.5:8080/x"} {
		if err := ValidateURL(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"ftp://x", "https://", "https://u:p@example.com", "::"} {
		if ValidateURL(bad) == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestBlocked(t *testing.T) {
	cases := []struct {
		ip                  string
		always, withPrivate bool
	}{
		{"169.254.169.254", true, true},
		{"fe80::1", true, true},
		{"0.0.0.0", true, true},
		{"127.0.0.1", false, true},
		{"10.1.2.3", false, true},
		{"192.168.1.1", false, true},
		{"100.100.1.1", false, true},
		{"fd00::1", false, true},
		{"::ffff:169.254.169.254", true, true},
		{"0.1.2.3", true, true},
		{"64:ff9b::a9fe:a9fe", true, true},   // NAT64 of 169.254.169.254
		{"64:ff9b::7f00:1", false, true},     // NAT64 of 127.0.0.1
		{"64:ff9b:1::a00:1", false, true},    // local-use NAT64 of 10.0.0.1
		{"2002:a9fe:a9fe::1", true, true},    // 6to4 of 169.254.169.254
		{"2002:c0a8:101::1", false, true},    // 6to4 of 192.168.1.1
		{"64:ff9b::5db8:d822", false, false}, // NAT64 of a public address
		{"93.184.216.34", false, false},
	}
	for _, c := range cases {
		ip := netip.MustParseAddr(c.ip)
		if Blocked(ip, false) != c.always || Blocked(ip, true) != c.withPrivate {
			t.Errorf("%s: %v %v", c.ip, Blocked(ip, false), Blocked(ip, true))
		}
	}
}

func TestSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("X-Got", r.Header.Get("X-Tgp-Event")+"/"+string(b))
			_, _ = w.Write([]byte(strings.Repeat("a", MaxResponseBody+10)))
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/slow":
			time.Sleep(300 * time.Millisecond)
		}
	}))
	defer srv.Close()
	s := &Sender{Timeout: 100 * time.Millisecond, UserAgent: "tgp-test"}
	ctx := context.Background()
	r := s.Send(ctx, Request{URL: srv.URL + "/ok", Headers: map[string]string{"X-Tgp-Event": "push"}, Body: []byte("{}")})
	if !r.OK() || r.Headers["X-Got"] != "push/{}" || !r.Truncated || len(r.Body) != MaxResponseBody {
		t.Fatalf("ok: %+v", r.Err)
	}
	if r := s.Send(ctx, Request{URL: srv.URL + "/redirect"}); r.OK() || r.Status != 302 || r.Err == nil {
		t.Fatalf("redirect: %+v", r)
	}
	if r := s.Send(ctx, Request{URL: srv.URL + "/slow"}); r.OK() || r.Err == nil || r.Err.Error() != "timed out" {
		t.Fatalf("slow: %+v", r.Err)
	}
	blocking := &Sender{BlockPrivate: true}
	if r := blocking.Send(ctx, Request{URL: srv.URL + "/ok"}); !errors.Is(r.Err, ErrBlockedAddress) {
		t.Fatalf("loopback with block: %v", r.Err)
	}
	if r := s.Send(ctx, Request{URL: "http://169.254.169.254/latest/meta-data"}); !errors.Is(r.Err, ErrBlockedAddress) {
		t.Fatalf("metadata: %v", r.Err)
	}
}
