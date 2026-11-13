package server

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// SPAHandler serves a single-page app from files: existing files are served
// directly, any other GET path falls back to index.html so client-side
// routing works. Files under /assets/ are content-hashed by Vite and cached
// forever.
func SPAHandler(files fs.FS) http.Handler {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		index = nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if f, err := files.Open(name); err == nil {
				defer f.Close()
				if st, err := f.Stat(); err == nil && !st.IsDir() {
					if strings.HasPrefix(name, "assets/") {
						h.Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					if rs, ok := f.(io.ReadSeeker); ok {
						http.ServeContent(w, r, name, st.ModTime(), rs)
						return
					}
				}
			} else if !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			// Missing files with an extension are real 404s, not app routes.
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}
		h.Set("Cache-Control", "no-cache")
		if index == nil {
			h.Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(placeholderPage))
			return
		}
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}

const placeholderPage = `<!doctype html><html><head><meta charset="utf-8"><title>tgp</title>
<style>body{font:14px system-ui;background:#0b0d10;color:#c9d1d9;display:grid;place-items:center;height:100vh;margin:0}code{background:#161b22;padding:2px 6px;border-radius:4px}</style>
</head><body><div><h1>tgp-server is running</h1><p>The web UI was not built into this binary. Run <code>make web</code> then rebuild.</p></div></body></html>`
