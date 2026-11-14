package server

import (
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
)

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.log.Debug("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration", time.Since(start),
			"remote", r.RemoteAddr,
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				s.log.Error("panic serving request", "method", r.Method, "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				apierr.Write(w, apierr.New(http.StatusInternalServerError, apierr.CodeInternal, "internal server error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// realIP replaces r.RemoteAddr with the client address reported by a
// trusted reverse proxy. It assumes exactly one proxy hop: the rightmost
// X-Forwarded-For entry is the address that proxy saw (nginx's
// $proxy_add_x_forwarded_for appends it; Caddy replaces the header with
// it), and anything left of it came from the client and can be forged.
// X-Real-IP is the fallback. True-Client-IP is ignored because proxies
// pass it through untouched. Only installed when trust_proxy is on.
func realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ip := forwardedClientIP(r.Header); ip != "" {
			r.RemoteAddr = ip
		}
		next.ServeHTTP(w, r)
	})
}

func forwardedClientIP(h http.Header) string {
	if xff := h.Values("X-Forwarded-For"); len(xff) > 0 {
		parts := strings.Split(xff[len(xff)-1], ",")
		for i := len(parts) - 1; i >= 0; i-- {
			if p := strings.TrimSpace(parts[i]); p != "" {
				if net.ParseIP(p) != nil {
					return p
				}
				return ""
			}
		}
	}
	if ip := strings.TrimSpace(h.Get("X-Real-IP")); net.ParseIP(ip) != nil {
		return ip
	}
	return ""
}

// isHTTPS reports whether the client reached tgp over HTTPS: directly, or
// through a trusted proxy that says so.
func (s *Server) isHTTPS(r *http.Request) bool {
	return r.TLS != nil || (s.cfg.Server.TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https")
}

// hsts tells browsers to keep using HTTPS. It is only sent on HTTPS
// responses (browsers ignore it over plain HTTP anyway), and without
// includeSubDomains/preload since tgp doesn't own the parent domain.
func (s *Server) hsts(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isHTTPS(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// limitBody caps request bodies at n bytes, except for requests unlimited
// matches (uploads, which enforce their own limits).
func limitBody(n int64, unlimited ...func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			skip := false
			for _, f := range unlimited {
				skip = skip || f(r)
			}
			if r.Body != nil && !skip {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}
