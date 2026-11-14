package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/version"
)

// target is a resolved server plus the credentials to use with it.
type target struct {
	Base   string // base URL, e.g. https://git.example.com
	Key    string // host key, e.g. git.example.com
	User   string
	Token  string
	Source string // where the token came from: keyring, file, env, or ""
}

// transport adds auth and identification headers, logs requests when
// TGP_DEBUG is set, and (when failOnError) turns error responses into *Error.
type transport struct {
	hc          *http.Client
	token       string
	csrf        bool // echo the tgp_csrf cookie (cookie sessions)
	failOnError bool
	debug       io.Writer
}

func (t *transport) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "tgp-cli/"+version.Version)
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	if t.csrf && t.hc.Jar != nil {
		for _, c := range t.hc.Jar.Cookies(req.URL) {
			if c.Name == "tgp_csrf" {
				req.Header.Set("X-CSRF-Token", c.Value)
			}
		}
	}
	start := time.Now()
	resp, err := t.hc.Do(req)
	if t.debug != nil {
		status := "error: "
		if err == nil {
			status = resp.Status
		} else {
			status += err.Error()
		}
		fmt.Fprintf(t.debug, "* %s %s → %s (%s)\n", req.Method, req.URL.Redacted(), status, time.Since(start).Round(time.Millisecond))
	}
	if err != nil {
		return nil, &Error{Code: CodeNetwork, Exit: ExitUnavailable,
			Message: fmt.Sprintf("can't reach %s: %v", req.URL.Host, unwrapURLError(err))}
	}
	if t.failOnError && resp.StatusCode >= 400 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, apiError(resp.StatusCode, body)
	}
	return resp, nil
}

func unwrapURLError(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return ue.Err
	}
	return err
}

func debugWriter() io.Writer {
	if v := os.Getenv("TGP_DEBUG"); v != "" && v != "0" {
		return os.Stderr
	}
	return nil
}

func newHTTPClient(jar http.CookieJar) *http.Client {
	return &http.Client{Timeout: 2 * time.Minute, Jar: jar}
}

// apiClient wraps the generated client. Every method returns *Error for
// non-2xx responses, so callers only check err.
type apiClient struct {
	*apigen.ClientWithResponses
	t     *target
	trans *transport
}

func newAPIClient(t *target) (*apiClient, error) {
	tr := &transport{hc: newHTTPClient(nil), token: t.Token, failOnError: true, debug: debugWriter()}
	c, err := apigen.NewClientWithResponses(t.Base+"/api/v1", apigen.WithHTTPClient(tr))
	if err != nil {
		return nil, err
	}
	return &apiClient{ClientWithResponses: c, t: t, trans: tr}, nil
}

// newSessionClient returns a client that keeps cookies (for password login).
func newSessionClient(base string) (*apiClient, error) {
	jar, _ := cookiejar.New(nil)
	tr := &transport{hc: newHTTPClient(jar), csrf: true, failOnError: true, debug: debugWriter()}
	c, err := apigen.NewClientWithResponses(base+"/api/v1", apigen.WithHTTPClient(tr))
	if err != nil {
		return nil, err
	}
	return &apiClient{ClientWithResponses: c, t: &target{Base: base}, trans: tr}, nil
}

// raw sends a request without turning errors into *Error (for `tgp api`).
func (c *apiClient) raw(ctx context.Context, method, path string, header http.Header, body []byte) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.t.Base+path, rd)
	if err != nil {
		return nil, usageErr("invalid request: %v", err)
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	tr := *c.trans
	tr.failOnError = false
	return tr.Do(req)
}

// must returns an *Error when a 2xx response unexpectedly lacks a body.
func must[T any](v *T, what string) (*T, error) {
	if v == nil {
		return nil, errorf(CodeBadResponse, ExitError, "unexpected response from the server for %s", what)
	}
	return v, nil
}

// remoteURL parses a git remote URL into (base host key, path).
func splitRemote(raw string) (key, path string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", "", false
	}
	return strings.ToLower(u.Host), strings.Trim(u.Path, "/"), true
}

// strings helpers shared by commands.

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
