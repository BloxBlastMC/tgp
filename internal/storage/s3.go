package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // S3's DeleteObjects requires Content-MD5
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// S3Config configures an S3-compatible bucket (AWS S3, MinIO, Ceph RGW,
// Cloudflare R2, Backblaze B2, ...).
type S3Config struct {
	// Endpoint is the service URL, e.g. https://s3.eu-west-1.amazonaws.com
	// or http://minio:9000. Empty means AWS for Region.
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretKey string
	// PathStyle addresses the bucket as endpoint/bucket instead of
	// bucket.endpoint (MinIO and most self-hosted servers need this).
	PathStyle bool
	// Insecure skips TLS certificate verification.
	Insecure bool
}

// S3 stores blobs as objects in a bucket. Requests are signed with AWS
// Signature Version 4.
type S3 struct {
	cfg    S3Config
	base   *url.URL
	client *http.Client
	now    func() time.Time
}

// NewS3 validates the configuration. It doesn't contact the server; call
// Check for that.
func NewS3(cfg S3Config) (*S3, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("storage: s3 bucket is required")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	ep := cfg.Endpoint
	if ep == "" {
		ep = "https://s3." + cfg.Region + ".amazonaws.com"
	}
	if !strings.Contains(ep, "://") {
		ep = "https://" + ep
	}
	u, err := url.Parse(ep)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("storage: invalid s3 endpoint %q", cfg.Endpoint)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	cfg.Prefix = strings.Trim(cfg.Prefix, "/")
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicitly configured
	}
	return &S3{cfg: cfg, base: u, client: &http.Client{Transport: tr}, now: time.Now}, nil
}

func (s *S3) objectKey(key string) (string, error) {
	clean := strings.Trim(key, "/")
	if clean == "" || strings.Contains(key, "..") || strings.Contains(key, "\\") {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	if s.cfg.Prefix != "" {
		clean = s.cfg.Prefix + "/" + clean
	}
	return clean, nil
}

// url builds the URL of an object ("" for the bucket) with a query.
func (s *S3) url(object string, q url.Values) *url.URL {
	u := *s.base
	if s.cfg.PathStyle {
		u.Path = u.Path + "/" + s.cfg.Bucket
	} else {
		u.Host = s.cfg.Bucket + "." + u.Host
	}
	if object != "" {
		u.Path += "/" + object
	} else if s.cfg.PathStyle {
		u.Path += "/"
	} else if u.Path == "" {
		u.Path = "/"
	}
	// Send the path exactly as it's signed.
	u.RawPath = uriEncode(u.Path, true)
	if q != nil {
		u.RawQuery = canonicalQuery(q)
	}
	return &u
}

const (
	emptySHA256     = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	unsignedPayload = "UNSIGNED-PAYLOAD"
)

func (s *S3) do(ctx context.Context, method string, u *url.URL, body io.Reader, size int64, payloadHash string, hdr http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = size
		if size == 0 {
			req.Body = http.NoBody
		}
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	if payloadHash == "" {
		payloadHash = emptySHA256
	}
	signV4(req, s.cfg.AccessKey, s.cfg.SecretKey, s.cfg.Region, payloadHash, s.now())
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("storage: s3 %s: %w", method, err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return resp, s3Error(resp)
	}
	return resp, nil
}

// s3ErrorBody is S3's XML error.
type s3ErrorBody struct {
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

// S3Error is an error response from the server.
type S3Error struct {
	Status  int
	Code    string
	Message string
}

func (e *S3Error) Error() string {
	return fmt.Sprintf("storage: s3: %d %s: %s", e.Status, e.Code, e.Message)
}

func s3Error(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var e s3ErrorBody
	_ = xml.Unmarshal(b, &e)
	if e.Code == "" {
		e.Code = http.StatusText(resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound && (e.Code == "NoSuchKey" || e.Code == "Not Found") {
		return ErrNotFound
	}
	return &S3Error{Status: resp.StatusCode, Code: e.Code, Message: e.Message}
}

// Put uploads an object. Objects of unknown size are spooled to a
// temporary file first (S3 needs the length up front).
func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	obj, err := s.objectKey(key)
	if err != nil {
		return err
	}
	if size < 0 {
		f, err := os.CreateTemp("", "tgp-s3-*")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		defer f.Close()
		n, err := io.Copy(f, r)
		if err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		r, size = f, n
	}
	hdr := http.Header{"Content-Type": {"application/octet-stream"}}
	resp, err := s.do(ctx, http.MethodPut, s.url(obj, nil), io.NopCloser(io.LimitReader(r, size)), size, unsignedPayload, hdr)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.objectKey(key)
	if err != nil {
		return nil, err
	}
	resp, err := s.do(ctx, http.MethodGet, s.url(obj, nil), nil, 0, "", nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	obj, err := s.objectKey(key)
	if err != nil {
		return err
	}
	resp, err := s.do(ctx, http.MethodDelete, s.url(obj, nil), nil, 0, "", nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

type listResult struct {
	Contents []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

// list returns up to max keys (0 = all) under prefix.
func (s *S3) list(ctx context.Context, prefix string, max int) ([]string, error) {
	var keys []string
	token := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}}
		if max > 0 {
			q.Set("max-keys", fmt.Sprint(max))
		}
		if token != "" {
			q.Set("continuation-token", token)
		}
		resp, err := s.do(ctx, http.MethodGet, s.url("", q), nil, 0, "", nil)
		if err != nil {
			return nil, err
		}
		var lr listResult
		err = xml.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&lr)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("storage: s3 list: %w", err)
		}
		for _, c := range lr.Contents {
			keys = append(keys, c.Key)
		}
		if !lr.IsTruncated || lr.NextContinuationToken == "" || (max > 0 && len(keys) >= max) {
			return keys, nil
		}
		token = lr.NextContinuationToken
	}
}

// DeletePrefix deletes every object under prefix/ in batches of 1000.
func (s *S3) DeletePrefix(ctx context.Context, prefix string) error {
	p, err := s.objectKey(prefix)
	if err != nil {
		return err
	}
	keys, err := s.list(ctx, p+"/", 0)
	if err != nil {
		return err
	}
	for len(keys) > 0 {
		n := min(len(keys), 1000)
		if err := s.deleteObjects(ctx, keys[:n]); err != nil {
			return err
		}
		keys = keys[n:]
	}
	return nil
}

type deleteRequest struct {
	XMLName xml.Name `xml:"Delete"`
	Quiet   bool     `xml:"Quiet"`
	Objects []struct {
		Key string `xml:"Key"`
	} `xml:"Object"`
}

type deleteResult struct {
	Errors []struct {
		Key     string `xml:"Key"`
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	} `xml:"Error"`
}

func (s *S3) deleteObjects(ctx context.Context, keys []string) error {
	dr := deleteRequest{Quiet: true}
	for _, k := range keys {
		dr.Objects = append(dr.Objects, struct {
			Key string `xml:"Key"`
		}{k})
	}
	body, err := xml.Marshal(dr)
	if err != nil {
		return err
	}
	sum := md5.Sum(body) //nolint:gosec // required by the protocol
	sha := sha256.Sum256(body)
	hdr := http.Header{"Content-Md5": {base64.StdEncoding.EncodeToString(sum[:])}, "Content-Type": {"application/xml"}}
	resp, err := s.do(ctx, http.MethodPost, s.url("", url.Values{"delete": {""}}), bytes.NewReader(body), int64(len(body)), hex.EncodeToString(sha[:]), hdr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var res deleteResult
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&res); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("storage: s3 delete: %w", err)
	}
	if len(res.Errors) > 0 {
		e := res.Errors[0]
		return fmt.Errorf("storage: s3 delete %s: %s: %s (%d errors)", e.Key, e.Code, e.Message, len(res.Errors))
	}
	return nil
}

// Check verifies the bucket is reachable with the credentials.
func (s *S3) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := s.list(ctx, s.cfg.Prefix, 1)
	return err
}

// ---- Signature Version 4 ----

// signV4 signs req for service s3. Every header already set on the
// request is signed, plus host, x-amz-date and x-amz-content-sha256.
func signV4(req *http.Request, accessKey, secretKey, region, payloadHash string, t time.Time) {
	t = t.UTC()
	amzDate := t.Format("20060102T150405Z")
	day := t.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	headers := map[string]string{"host": host}
	for k, v := range req.Header {
		lk := strings.ToLower(k)
		switch lk {
		case "authorization", "user-agent", "content-length", "accept-encoding", "expect", "connection":
			continue
		}
		vals := make([]string, len(v))
		for i, x := range v {
			vals[i] = strings.Join(strings.Fields(x), " ")
		}
		headers[lk] = strings.Join(vals, ",")
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var ch strings.Builder
	for _, k := range names {
		ch.WriteString(k + ":" + headers[k] + "\n")
	}
	signed := strings.Join(names, ";")

	canonical := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL.Path),
		canonicalQuery(req.URL.Query()),
		ch.String(),
		signed,
		payloadHash,
	}, "\n")
	scope := day + "/" + region + "/s3/aws4_request"
	h := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(h[:])
	key := hmacSHA256([]byte("AWS4"+secretKey), day)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(key, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+", SignedHeaders="+signed+", Signature="+sig)
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// uriEncode percent-encodes everything but unreserved characters (and /
// when keepSlash).
func uriEncode(s string, keepSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && keepSlash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func canonicalURI(p string) string {
	if p == "" {
		return "/"
	}
	return uriEncode(p, true)
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, uriEncode(k, false)+"="+uriEncode(v, false))
		}
	}
	return strings.Join(parts, "&")
}
