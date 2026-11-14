package storage

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// The "GET Object" example from the AWS Signature Version 4 documentation.
func TestSignV4AWSExample(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	tm, _ := time.Parse("20060102T150405Z", "20130524T000000Z")
	signV4(req, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", emptySHA256, tm)
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("authorization:\n got %s\nwant %s", got, want)
	}
}

// fakeS3 is an in-memory, path-style S3 that checks request signatures.
type fakeS3 struct {
	t       *testing.T
	mu      sync.Mutex
	objects map[string][]byte
	secret  string
}

func (f *fakeS3) verify(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	i := strings.Index(auth, "SignedHeaders=")
	if i < 0 {
		return false
	}
	signed := strings.Split(strings.SplitN(auth[i+len("SignedHeaders="):], ",", 2)[0], ";")
	clone, _ := http.NewRequest(r.Method, "http://"+r.Host+r.URL.RequestURI(), nil)
	for _, h := range signed {
		if h != "host" {
			clone.Header.Set(h, r.Header.Get(h))
		}
	}
	tm, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil {
		return false
	}
	signV4(clone, "AK", f.secret, "us-east-1", r.Header.Get("X-Amz-Content-Sha256"), tm)
	return clone.Header.Get("Authorization") == auth
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !f.verify(r) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "<Error><Code>SignatureDoesNotMatch</Code><Message>bad signature</Message></Error>")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/bucket")
	key := strings.TrimPrefix(path, "/")
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		if int64(len(b)) != r.ContentLength {
			f.t.Errorf("content length %d, body %d", r.ContentLength, len(b))
		}
		f.objects[key] = b
	case r.Method == http.MethodGet && key == "":
		var keys []string
		for k := range f.objects {
			if strings.HasPrefix(k, q.Get("prefix")) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		// Two keys per page to exercise continuation.
		start := 0
		if tok := q.Get("continuation-token"); tok != "" {
			start = sort.SearchStrings(keys, tok)
		}
		end := min(start+2, len(keys))
		var out bytes.Buffer
		out.WriteString("<ListBucketResult>")
		for _, k := range keys[start:end] {
			out.WriteString("<Contents><Key>" + k + "</Key></Contents>")
		}
		if end < len(keys) {
			out.WriteString("<IsTruncated>true</IsTruncated><NextContinuationToken>" + keys[end] + "</NextContinuationToken>")
		}
		out.WriteString("</ListBucketResult>")
		_, _ = w.Write(out.Bytes())
	case r.Method == http.MethodGet:
		b, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "<Error><Code>NoSuchKey</Code></Error>")
			return
		}
		_, _ = w.Write(b)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && q.Has("delete"):
		if r.Header.Get("Content-Md5") == "" {
			f.t.Error("DeleteObjects without Content-MD5")
		}
		var dr deleteRequest
		if err := xml.NewDecoder(r.Body).Decode(&dr); err != nil {
			f.t.Error(err)
		}
		for _, o := range dr.Objects {
			delete(f.objects, o.Key)
		}
		_, _ = io.WriteString(w, "<DeleteResult></DeleteResult>")
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestS3(t *testing.T) {
	ctx := context.Background()
	f := &fakeS3{t: t, objects: map[string][]byte{}, secret: "SK"}
	srv := httptest.NewServer(f)
	defer srv.Close()
	s, err := NewS3(S3Config{Endpoint: srv.URL, Bucket: "bucket", Prefix: "/tgp/", AccessKey: "AK", SecretKey: "SK", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "a/1", strings.NewReader("one"), 3); err != nil {
		t.Fatal(err)
	}
	// Unknown size: spooled first.
	for _, k := range []string{"a/2", "a/3 x+y", "a/4", "b/1"} {
		if err := s.Put(ctx, k, io.MultiReader(strings.NewReader("data-"), strings.NewReader(k)), -1); err != nil {
			t.Fatal(k, err)
		}
	}
	if _, ok := f.objects["tgp/a/3 x+y"]; !ok {
		t.Fatalf("objects: %v", f.objects)
	}
	r, err := s.Get(ctx, "a/3 x+y")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "data-a/3 x+y" {
		t.Fatalf("get: %q", b)
	}
	if _, err := s.Get(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := s.Delete(ctx, "a/1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "a/1"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePrefix(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if len(f.objects) != 1 || f.objects["tgp/b/1"] == nil {
		t.Fatalf("after delete prefix: %v", f.objects)
	}
	if err := s.Put(ctx, "../x", strings.NewReader(""), 0); err == nil {
		t.Fatal("bad key accepted")
	}

	bad, _ := NewS3(S3Config{Endpoint: srv.URL, Bucket: "bucket", AccessKey: "AK", SecretKey: "wrong", PathStyle: true})
	var se *S3Error
	if err := bad.Check(ctx); !errors.As(err, &se) || se.Code != "SignatureDoesNotMatch" {
		t.Fatalf("wrong secret: %v", err)
	}
}

func TestS3VirtualHostURL(t *testing.T) {
	s, err := NewS3(S3Config{Region: "eu-west-1", Bucket: "bkt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.url("x/y", nil).String(); got != "https://bkt.s3.eu-west-1.amazonaws.com/x/y" {
		t.Fatal(got)
	}
	if got := s.url("", url.Values{"list-type": {"2"}, "prefix": {"a b"}}).String(); got != "https://bkt.s3.eu-west-1.amazonaws.com/?list-type=2&prefix=a%20b" {
		t.Fatal(got)
	}
}
