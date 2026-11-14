// Package server wires the tgp HTTP server: API routes, git smart HTTP and
// the embedded web UI.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/config"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/git"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/repos"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/secretbox"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/storage"
)

// Server is the tgp HTTP server.
type Server struct {
	cfg *config.Config
	db  *db.DB
	log *slog.Logger
	web http.Handler
	box *secretbox.Box

	// Login rate limits: per username and per client IP.
	userLimiter *auth.FailureLimiter
	ipLimiter   *auth.FailureLimiter

	// setupDone caches setup completion; it never reverts once true.
	setupDone atomic.Bool

	git   *git.Git
	repos *repos.Store
	// gitSem bounds concurrent git transfers.
	gitSem chan struct{}
	// hooksDir holds the pre/post-receive scripts used by receive-pack.
	hooksDir string
	// hookSecret authenticates hook callbacks to /internal/hooks.
	hookSecret string
	hookURL    atomic.Value // string

	// store holds blobs (CI logs).
	store storage.Store
	ci    ciState

	hooks     hookState
	hooksOnce sync.Once
}

// Options configures a Server.
type Options struct {
	Config *config.Config
	DB     *db.DB
	Logger *slog.Logger
	// Web serves the embedded UI. If nil a placeholder page is served.
	Web http.Handler
	// Box encrypts secrets at rest with the master key.
	Box *secretbox.Box
	// Git runs git commands.
	Git *git.Git
	// HooksDir contains hook scripts (see githook.WriteScripts).
	HooksDir string
	// Storage holds blobs. If nil, local storage under the configured
	// storage path is used.
	Storage storage.Store
}

// New creates a Server.
func New(o Options) *Server {
	s := &Server{
		cfg:         o.Config,
		db:          o.DB,
		log:         o.Logger,
		web:         o.Web,
		box:         o.Box,
		userLimiter: auth.NewFailureLimiter(10, 15*time.Minute),
		ipLimiter:   auth.NewFailureLimiter(50, 15*time.Minute),
		git:         o.Git,
		hooksDir:    o.HooksDir,
		hookSecret:  auth.RandomToken(32),
	}
	s.repos = &repos.Store{DB: o.DB, Git: o.Git, Root: o.Config.Repos.Root}
	s.store = o.Storage
	if s.store == nil {
		p := o.Config.Storage.Local.Path
		if p == "" {
			p = filepath.Join(o.Config.DataDir, "storage")
		}
		st, err := storage.NewLocal(p)
		if err != nil {
			o.Logger.Error("can't create local storage", "path", p, "err", err)
			st = &storage.Local{Root: p}
		}
		s.store = st
	}
	n := o.Config.Repos.MaxConcurrentGit
	if n <= 0 {
		n = 2 * runtime.NumCPU()
	}
	s.gitSem = make(chan struct{}, n)
	return s
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
	r.Use(s.gitRoutes)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})
	r.Get("/readyz", s.handleReady)
	r.Post("/internal/hooks/{kind}", s.handleHook)

	r.Route("/api", func(r chi.Router) {
		r.Use(securityHeaders)
		r.Get("/v1/openapi.json", serveSpec)
		r.Mount("/runner/v1", s.runnerAPI())
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

var assetUploadRe = regexp.MustCompile(`^/api/v1/repos/[^/]+/releases/[0-9]+/assets$`)

// isAssetUpload matches release asset uploads, which have their own limit.
func isAssetUpload(r *http.Request) bool {
	return r.Method == http.MethodPost && assetUploadRe.MatchString(r.URL.Path)
}

func (s *Server) apiV1() http.Handler {
	r := chi.NewRouter()
	r.Use(limitBody(2<<20, isAssetUpload), s.csrf, s.authenticate)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		apierr.Write(w, apierr.NotFound("no API route for %s %s", r.Method, r.URL.Path))
	})
	r.Get("/repos/{repo}/jobs/{id}/logs/stream", s.streamJobLogs)
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		apierr.Write(w, apierr.New(http.StatusMethodNotAllowed, apierr.CodeMethodNotAllowed, "method %s not allowed on %s", r.Method, r.URL.Path))
	})
	strict := apigen.NewStrictHandlerWithOptions(&api{s: s}, []apigen.StrictMiddlewareFunc{s.policyMiddleware}, apigen.StrictHTTPServerOptions{
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
	s.StartBackground(ctx)
	if v, _ := s.hookURL.Load().(string); v == "" {
		s.SetInternalURL(loopbackURL(ln.Addr().String()))
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

// loopbackURL turns a listener address like "[::]:3000" or "0.0.0.0:3000"
// into a URL reachable from the same host.
func loopbackURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "::" || host == "0.0.0.0" || strings.HasPrefix(host, "[") {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// serveSpec serves the OpenAPI document (public, so clients and agents can
// discover the API before logging in).
var specJSON = sync.OnceValues(func() ([]byte, error) {
	doc, err := apigen.GetSwagger()
	if err != nil {
		return nil, err
	}
	return doc.MarshalJSON()
})

func serveSpec(w http.ResponseWriter, _ *http.Request) {
	b, err := specJSON()
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(b)
}
