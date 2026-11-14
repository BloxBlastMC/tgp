// Package server wires the tgp HTTP server: API routes, git smart HTTP and
// the embedded web UI.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/config"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
)

// Server is the tgp HTTP server.
type Server struct {
	cfg *config.Config
	db  *db.DB
	log *slog.Logger
	web http.Handler
}

// Options configures a Server.
type Options struct {
	Config *config.Config
	DB     *db.DB
	Logger *slog.Logger
	// Web serves the embedded UI. If nil a placeholder page is served.
	Web http.Handler
}

// New creates a Server.
func New(o Options) *Server {
	return &Server{cfg: o.Config, db: o.DB, log: o.Logger, web: o.Web}
}

// Handler builds the root HTTP handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if s.cfg.Server.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(s.recoverer)
	r.Use(s.requestLogger)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
	r.Get("/readyz", s.handleReady)

	r.Route("/api", func(r chi.Router) {
		r.Use(securityHeaders)
		r.Mount("/v1", s.apiV1())
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			apierr.Write(w, apierr.NotFound("no API route for %s %s", r.Method, r.URL.Path))
		})
	})

	if s.web != nil {
		r.NotFound(s.web.ServeHTTP)
	}
	return r
}

func (s *Server) apiV1() http.Handler {
	r := chi.NewRouter()
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		apierr.Write(w, apierr.NotFound("no API route for %s %s", r.Method, r.URL.Path))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		apierr.Write(w, apierr.New(http.StatusMethodNotAllowed, apierr.CodeMethodNotAllowed, "method %s not allowed on %s", r.Method, r.URL.Path))
	})
	strict := apigen.NewStrictHandlerWithOptions(&api{s: s}, nil, apigen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			apierr.Write(w, apierr.BadRequest("invalid request: %v", err))
		},
		ResponseErrorHandlerFunc: s.writeHandlerError,
	})
	return apigen.HandlerWithOptions(strict, apigen.ChiServerOptions{
		BaseRouter: r,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			apierr.Write(w, apierr.BadRequest("%v", err))
		},
	})
}

// writeHandlerError converts an error returned from a handler into a JSON
// error response, logging unexpected (non-API) errors.
func (s *Server) writeHandlerError(w http.ResponseWriter, r *http.Request, err error) {
	e := apierr.From(err)
	if e.Code == apierr.CodeInternal {
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "request_id", middleware.GetReqID(r.Context()), "err", err)
	}
	apierr.Write(w, e)
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		apierr.Write(w, apierr.New(http.StatusServiceUnavailable, apierr.CodeUnavailable, "database unreachable"))
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("ready\n"))
}

// ListenAndServe runs the server until ctx is cancelled, then shuts down
// gracefully within the configured timeout.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Server.Listen)
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}

// Serve is ListenAndServe on an existing listener.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	s.log.Info("tgp-server listening", "addr", ln.Addr().String())

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	timeout := s.cfg.Server.ShutdownTimeout.Duration
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	s.log.Info("shutting down", "timeout", timeout)
	shCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := srv.Shutdown(shCtx); err != nil {
		return err
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
