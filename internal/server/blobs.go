package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
)

// blobKey makes a fresh storage key under prefix. Keys are random so a
// replaced upload never overwrites a blob that's still being read.
func blobKey(prefix string, repoID int64, ext string) string {
	return fmt.Sprintf("%s/%d/%s%s", prefix, repoID, auth.RandomToken(16), ext)
}

// hashingReader counts and hashes what passes through and fails once more
// than max bytes were read.
type hashingReader struct {
	r   io.Reader
	h   hash.Hash
	n   int64
	max int64
}

var errTooLarge = fmt.Errorf("upload too large")

func (h *hashingReader) Read(p []byte) (int, error) {
	n, err := h.r.Read(p)
	h.n += int64(n)
	h.h.Write(p[:n])
	if h.n > h.max {
		return n, errTooLarge
	}
	return n, err
}

// putBlob stores an upload of declared size (-1 if unknown) of at most max
// bytes. what names the upload in errors. On failure nothing is left in
// storage.
func (s *Server) putBlob(ctx context.Context, key string, r io.Reader, declared, max int64, what string) (int64, string, error) {
	tooLarge := apierr.New(http.StatusRequestEntityTooLarge, apierr.CodePayloadTooLarge, "%s is larger than the %d MiB limit", what, max>>20)
	if declared > max {
		return 0, "", tooLarge
	}
	hr := &hashingReader{r: r, h: sha256.New(), max: max}
	err := s.store.Put(ctx, key, hr, declared)
	if err == nil && declared >= 0 && hr.n != declared {
		err = apierr.BadRequest("%s was cut short (%d of %d bytes)", what, hr.n, declared)
	}
	if err != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), key)
		if hr.n > max {
			return 0, "", tooLarge
		}
		return 0, "", err
	}
	return hr.n, hex.EncodeToString(hr.h.Sum(nil)), nil
}

// serveBlob streams a stored blob.
func (s *Server) serveBlob(ctx context.Context, w http.ResponseWriter, key, contentType, filename string, size int64) error {
	rc, err := s.store.Get(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close()
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", fmt.Sprint(size))
	h.Set("Content-Disposition", contentDisposition(filename))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=0")
	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, rc)
	return err
}

// contentDisposition is an attachment header with an RFC 6266 UTF-8 name.
func contentDisposition(name string) string {
	ascii := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			c = '_'
		}
		ascii = append(ascii, c)
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, pathEscapeAll(name))
}

func pathEscapeAll(s string) string {
	const hexd = "0123456789ABCDEF"
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			b = append(b, c)
		} else {
			b = append(b, '%', hexd[c>>4], hexd[c&15])
		}
	}
	return string(b)
}
