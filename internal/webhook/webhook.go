// Package webhook signs and sends webhook deliveries.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Sign returns the signature header value for body: "sha256=" + hex HMAC.
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// Verify checks a signature header in constant time (for receivers and
// tests).
func Verify(secret string, body []byte, header string) bool {
	return hmac.Equal([]byte(Sign(secret, body)), []byte(header))
}

// ValidateURL checks a webhook URL's form (not its address, which is
// checked when connecting).
func ValidateURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	switch {
	case err != nil:
		return fmt.Errorf("invalid URL: %v", err)
	case u.Scheme != "http" && u.Scheme != "https":
		return errors.New("the URL must start with http:// or https://")
	case u.Host == "" || u.Hostname() == "":
		return errors.New("the URL needs a host")
	case u.User != nil:
		return errors.New("put credentials in the secret, not the URL")
	}
	return nil
}

// ErrBlockedAddress is returned when the destination resolves to an
// address deliveries may not go to.
var ErrBlockedAddress = errors.New("destination address not allowed")

// Blocked reports whether ip is refused: link-local and unspecified
// addresses always (cloud metadata services live there), loopback and
// private ranges when blockPrivate.
func Blocked(ip netip.Addr, blockPrivate bool) bool {
	ip = ip.Unmap()
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if blockPrivate && (ip.IsLoopback() || ip.IsPrivate()) {
		return true
	}
	// Carrier-grade NAT and other shared space count as private too.
	if blockPrivate && netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
		return true
	}
	return false
}

// Sender delivers requests with address checks applied to every
// connection (so DNS answers can't sneak past them) and no redirects.
type Sender struct {
	BlockPrivate bool
	Timeout      time.Duration
	UserAgent    string
}

func (s *Sender) client(insecure bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if Blocked(ip, s.BlockPrivate) {
				return fmt.Errorf("%w: %s", ErrBlockedAddress, ip)
			}
			return nil
		},
	}
	tr := &http.Transport{
		Proxy:                 nil, // a proxy would hide the destination from the check
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: s.timeout(),
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		DisableKeepAlives:     true,
	}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // the webhook owner opted in
	}
	return &http.Client{
		Transport: tr,
		Timeout:   s.timeout(),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (s *Sender) timeout() time.Duration {
	if s.Timeout <= 0 {
		return 10 * time.Second
	}
	return s.Timeout
}

// Request is a delivery to send.
type Request struct {
	URL      string
	Headers  map[string]string
	Body     []byte
	Insecure bool
}

// MaxResponseBody is how much of a response is kept.
const MaxResponseBody = 64 << 10

// Result is the outcome of one attempt.
type Result struct {
	Status    int // 0 if no response
	Headers   map[string]string
	Body      string
	Truncated bool
	Duration  time.Duration
	Err       error
}

// OK reports a 2xx response.
func (r *Result) OK() bool { return r.Err == nil && r.Status >= 200 && r.Status < 300 }

// Send POSTs the request once.
func (s *Sender) Send(ctx context.Context, req Request) *Result {
	start := time.Now()
	res := &Result{Headers: map[string]string{}}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		res.Err = err
		return res
	}
	for k, v := range req.Headers {
		hr.Header.Set(k, v)
	}
	if s.UserAgent != "" {
		hr.Header.Set("User-Agent", s.UserAgent)
	}
	resp, err := s.client(req.Insecure).Do(hr)
	res.Duration = time.Since(start)
	if err != nil {
		res.Err = cleanErr(err)
		return res
	}
	defer resp.Body.Close()
	res.Status = resp.StatusCode
	for k := range resp.Header {
		res.Headers[k] = resp.Header.Get(k)
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBody+1))
	if len(b) > MaxResponseBody {
		b, res.Truncated = b[:MaxResponseBody], true
	}
	res.Body = string(bytes.ToValidUTF8(b, []byte("�")))
	res.Duration = time.Since(start)
	if res.Status >= 300 && res.Status < 400 {
		res.Err = fmt.Errorf("redirects aren't followed (%d to %s)", res.Status, resp.Header.Get("Location"))
	}
	return res
}

// cleanErr strips url.Error's "Post \"...\":" prefix.
func cleanErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return errors.New("timed out")
		}
		return ue.Err
	}
	return err
}
