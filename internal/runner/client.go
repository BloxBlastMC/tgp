// Package runner is the tgp-runner agent: it registers with a tgp server,
// long-polls for jobs, runs each job's steps in a Docker container and
// streams logs and results back.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
)

// APIError is an error response from the server.
type APIError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
	Details map[string]any
}

func (e *APIError) Error() string {
	return fmt.Sprintf("server: %d %s: %s", e.Status, e.Code, e.Message)
}

// Gone reports whether the server no longer considers the job running on
// this runner (so the runner should abort it).
func (e *APIError) Gone() bool {
	return e.Status == http.StatusNotFound || e.Status == http.StatusConflict
}

// Client talks to the runner API.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient creates a client for the server at baseURL.
func NewClient(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// do posts in as JSON and decodes the response into out. It returns
// (false, nil) for 204 No Content.
func (c *Client) do(ctx context.Context, path string, in, out any) (bool, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+runnerapi.Prefix+path, bytes.NewReader(b))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return false, err
	}
	if resp.StatusCode == http.StatusNoContent {
		return false, nil
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error APIError `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		e.Error.Status = resp.StatusCode
		if e.Error.Message == "" {
			e.Error.Message = strings.TrimSpace(string(data))
		}
		if d, ok := extractDetails(data); ok {
			e.Error.Details = d
		}
		return false, &e.Error
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return false, fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return true, nil
}

func extractDetails(data []byte) (map[string]any, bool) {
	var e struct {
		Error struct {
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) != nil || e.Error.Details == nil {
		return nil, false
	}
	return e.Error.Details, true
}

// retry calls f until it succeeds, the error is permanent (4xx) or ctx
// ends, backing off up to 30s.
func retry(ctx context.Context, f func() error) error {
	delay := 500 * time.Millisecond
	for {
		err := f()
		var ae *APIError
		if err == nil || (errors.As(err, &ae) && ae.Status < 500 && ae.Status != http.StatusTooManyRequests) {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), err)
		case <-time.After(delay):
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func (c *Client) Register(ctx context.Context, req runnerapi.RegisterRequest) (*runnerapi.RegisterResponse, error) {
	var out runnerapi.RegisterResponse
	_, err := c.do(ctx, "/register", req, &out)
	return &out, err
}

// Fetch long-polls for a job; it returns nil when none is ready.
func (c *Client) Fetch(ctx context.Context, req runnerapi.FetchRequest) (*runnerapi.JobSpec, error) {
	var out runnerapi.FetchResponse
	ok, err := c.do(ctx, "/fetch", req, &out)
	if err != nil || !ok {
		return nil, err
	}
	return out.Job, nil
}

func (c *Client) Heartbeat(ctx context.Context, req runnerapi.HeartbeatRequest) (*runnerapi.HeartbeatResponse, error) {
	var out runnerapi.HeartbeatResponse
	_, err := c.do(ctx, "/heartbeat", req, &out)
	return &out, err
}

func (c *Client) Logs(ctx context.Context, job int64, req runnerapi.LogRequest) (*runnerapi.LogResponse, error) {
	var out runnerapi.LogResponse
	_, err := c.do(ctx, fmt.Sprintf("/jobs/%d/logs", job), req, &out)
	return &out, err
}

func (c *Client) Update(ctx context.Context, job int64, req runnerapi.UpdateRequest) (*runnerapi.UpdateResponse, error) {
	var out runnerapi.UpdateResponse
	_, err := c.do(ctx, fmt.Sprintf("/jobs/%d/update", job), req, &out)
	return &out, err
}

func (c *Client) Complete(ctx context.Context, job int64, req runnerapi.CompleteRequest) error {
	_, err := c.do(ctx, fmt.Sprintf("/jobs/%d/complete", job), req, nil)
	return err
}

// raw sends a request with a raw body (nil for none) and returns the
// response for 2xx statuses. The caller closes the body.
func (c *Client) raw(ctx context.Context, method, path string, body io.Reader, size int64, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+runnerapi.Prefix+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = size
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	// Transfers can take long; the default client's timeout covers the
	// whole body, so use one without it (ctx still applies).
	hc := *c.HTTP
	hc.Timeout = timeout
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var e struct {
			Error APIError `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		e.Error.Status = resp.StatusCode
		if e.Error.Message == "" {
			e.Error.Message = strings.TrimSpace(string(data))
		}
		return nil, &e.Error
	}
	return resp, nil
}

func (c *Client) rawJSON(ctx context.Context, method, path string, body io.Reader, size int64, out any) error {
	resp, err := c.raw(ctx, method, path, body, size, 0)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

// UploadArtifact uploads a zip of size bytes.
func (c *Client) UploadArtifact(ctx context.Context, job int64, name string, retentionDays int, overwrite bool, zip io.Reader, size int64) (*runnerapi.Artifact, error) {
	q := url.Values{"name": {name}}
	if retentionDays > 0 {
		q.Set("retention_days", strconv.Itoa(retentionDays))
	}
	if overwrite {
		q.Set("overwrite", "1")
	}
	var out runnerapi.Artifact
	err := c.rawJSON(ctx, http.MethodPut, fmt.Sprintf("/jobs/%d/artifacts?%s", job, q.Encode()), zip, size, &out)
	return &out, err
}

// ListArtifacts lists the artifacts of the job's run.
func (c *Client) ListArtifacts(ctx context.Context, job int64) ([]runnerapi.Artifact, error) {
	var out runnerapi.ArtifactList
	err := c.rawJSON(ctx, http.MethodGet, fmt.Sprintf("/jobs/%d/artifacts", job), nil, 0, &out)
	return out.Artifacts, err
}

// DownloadArtifact streams an artifact's zip.
func (c *Client) DownloadArtifact(ctx context.Context, job, id int64) (io.ReadCloser, error) {
	resp, err := c.raw(ctx, http.MethodGet, fmt.Sprintf("/jobs/%d/artifacts/%d", job, id), nil, 0, 0)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// LookupCache finds a cache for the job.
func (c *Client) LookupCache(ctx context.Context, job int64, req runnerapi.CacheLookupRequest) (*runnerapi.CacheLookupResponse, error) {
	var out runnerapi.CacheLookupResponse
	_, err := c.do(ctx, fmt.Sprintf("/jobs/%d/caches/lookup", job), req, &out)
	return &out, err
}

// DownloadCache streams a cache archive.
func (c *Client) DownloadCache(ctx context.Context, job, id int64) (io.ReadCloser, error) {
	resp, err := c.raw(ctx, http.MethodGet, fmt.Sprintf("/jobs/%d/caches/%d", job, id), nil, 0, 0)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// SaveCache uploads a cache archive. A 409 APIError means another job
// saved the key first.
func (c *Client) SaveCache(ctx context.Context, job int64, key, version string, archive io.Reader, size int64) (*runnerapi.CacheEntry, error) {
	q := url.Values{"key": {key}, "version": {version}}
	var out runnerapi.CacheEntry
	err := c.rawJSON(ctx, http.MethodPut, fmt.Sprintf("/jobs/%d/caches?%s", job, q.Encode()), archive, size, &out)
	return &out, err
}
