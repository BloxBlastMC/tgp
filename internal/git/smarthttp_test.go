package git

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestBodyLimitsDecodedGzip(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(make([]byte, 1<<20)) // 1 MiB of zeros compresses to ~1 KiB
	_ = zw.Close()
	compressed := buf.Bytes()

	read := func(max int64) (int, error) {
		r := httptest.NewRequest("POST", "/x", bytes.NewReader(compressed))
		r.Header.Set("Content-Encoding", "gzip")
		body, err := requestBody(httptest.NewRecorder(), r, max)
		if err != nil {
			t.Fatal(err)
		}
		defer body.Close()
		b, err := io.ReadAll(body)
		return len(b), err
	}
	var tooLarge *http.MaxBytesError
	if n, err := read(64 << 10); !errors.As(err, &tooLarge) || n > 64<<10 {
		t.Fatalf("limited: read %d bytes, err %v", n, err)
	}
	if n, err := read(0); err != nil || n != 1<<20 {
		t.Fatalf("unlimited: read %d bytes, err %v", n, err)
	}

	r := httptest.NewRequest("POST", "/x", bytes.NewReader([]byte("not gzip")))
	r.Header.Set("Content-Encoding", "gzip")
	if _, err := requestBody(httptest.NewRecorder(), r, 10); err == nil {
		t.Fatal("bad gzip accepted")
	}
}
