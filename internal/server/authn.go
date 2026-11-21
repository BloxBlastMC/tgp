package server

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

const (
	sessionCookie = "tgp_session"
	csrfCookie    = "tgp_csrf"
	csrfHeader    = "X-CSRF-Token"

	sessionTTL         = 30 * 24 * time.Hour
	sessionTouchPeriod = 5 * time.Minute
	tokenTouchPeriod   = time.Minute
)

// AuthMethod is how a request authenticated.
type AuthMethod string

const (
	MethodSession AuthMethod = "session"
	MethodToken   AuthMethod = "token"
	// MethodJob: a CI job token (TGP_TOKEN in a running job).
	MethodJob AuthMethod = "job"
)

// Principal is the authenticated caller.
type Principal struct {
	User    *model.User
	Method  AuthMethod
	Session *model.Session     // when Method == MethodSession
	Token   *model.AccessToken // when Method == MethodToken
	Scopes  []string           // token scopes; nil for sessions (full access)
	// Job and JobRepo are the running job and its repository when Method
	// == MethodJob. User is then a stand-in that can only read JobRepo.
	Job     *model.Job
	JobRepo *model.Repo
}

// HasScope reports whether the principal may act with scope. Sessions have
// every scope; "write" implies "read".
func (p *Principal) HasScope(scope string) bool {
	if p.Method == MethodSession {
		return true
	}
	if slices.Contains(p.Scopes, scope) {
		return true
	}
	return scope == "read" && slices.Contains(p.Scopes, "write")
}

type ctxKey int

const (
	principalKey ctxKey = iota
	httpKey
)

type httpPair struct {
	w http.ResponseWriter
	r *http.Request
}

// principal returns the caller, or nil when unauthenticated.
func principal(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey).(*Principal)
	return p
}

// mustPrincipal returns the caller; the policy layer guarantees one exists
// for non-public operations.
func mustPrincipal(ctx context.Context) *Principal {
	p := principal(ctx)
	if p == nil {
		panic("no principal in context for authenticated operation")
	}
	return p
}

// httpFrom returns the raw request/response for the current API call, for
// handlers that need cookies, headers or the client IP.
func httpFrom(ctx context.Context) (http.ResponseWriter, *http.Request) {
	p := ctx.Value(httpKey).(httpPair)
	return p.w, p.r
}

// clientIP is the caller's IP (the realIP middleware has applied proxy headers
// when trust_proxy is on).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// authenticate resolves the Principal from an Authorization header or the
// session cookie. A bad explicit token fails the request; a bad cookie is
// ignored (the request continues anonymously).
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if raw := bearerToken(r); raw != "" {
			p, err := s.principalFromToken(ctx, raw)
			if err != nil {
				s.writeHandlerError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, principalKey, p)))
			return
		}
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			p, err := s.principalFromSession(ctx, c.Value, r)
			if err != nil {
				s.writeHandlerError(w, r, err)
				return
			}
			if p != nil {
				ctx = context.WithValue(ctx, principalKey, p)
			} else {
				s.clearSessionCookie(w, r)
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bearerToken extracts a token from "Authorization: Bearer X" or
// "Authorization: token X".
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	for _, prefix := range []string{"Bearer ", "bearer ", "token ", "Token "} {
		if strings.HasPrefix(h, prefix) {
			return strings.TrimSpace(h[len(prefix):])
		}
	}
	return ""
}

func (s *Server) principalFromToken(ctx context.Context, raw string) (*Principal, error) {
	invalid := apierr.Unauthenticated("invalid or expired access token")
	if strings.HasPrefix(raw, runnerapi.JobTokenPrefix) {
		p, err := s.jobPrincipal(ctx, raw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.Unauthenticated("invalid job token, or its job has finished")
		}
		return p, err
	}
	if !strings.HasPrefix(raw, auth.PATPrefix) {
		return nil, invalid
	}
	tok := new(model.AccessToken)
	err := s.db.NewSelect().Model(tok).Where("token_hash = ?", auth.HashToken(raw)).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, invalid
	} else if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if tok.ExpiresAt != nil && now.After(*tok.ExpiresAt) {
		return nil, invalid
	}
	u, err := s.loadActiveUser(ctx, tok.UserID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, invalid
	}
	if tok.LastUsedAt == nil || now.Sub(*tok.LastUsedAt) > tokenTouchPeriod {
		tok.LastUsedAt = &now
		if _, err := s.db.NewUpdate().Model(tok).Column("last_used_at").WherePK().Exec(ctx); err != nil {
			s.log.Warn("touch token", "err", err)
		}
	}
	return &Principal{User: u, Method: MethodToken, Token: tok, Scopes: splitScopes(tok.Scopes)}, nil
}

// jobPrincipal resolves a CI job token: a read-only principal limited to
// the job's repository, valid while the job runs.
func (s *Server) jobPrincipal(ctx context.Context, raw string) (*Principal, error) {
	j, err := s.jobByToken(ctx, raw)
	if err != nil {
		return nil, err
	}
	repo := new(model.Repo)
	if err := s.db.NewSelect().Model(repo).Where("id = ?", j.RepoID).Scan(ctx); err != nil {
		return nil, err
	}
	u := &model.User{Username: runnerapi.JobTokenUser, DisplayName: "CI job", Role: model.RoleTester, JobRepoID: repo.ID}
	return &Principal{User: u, Method: MethodJob, Scopes: []string{"read"}, Job: j, JobRepo: repo}, nil
}

func (s *Server) principalFromSession(ctx context.Context, raw string, r *http.Request) (*Principal, error) {
	sess := new(model.Session)
	err := s.db.NewSelect().Model(sess).Where("id = ?", auth.HashToken(raw)).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if now.After(sess.ExpiresAt) {
		_, _ = s.db.NewDelete().Model(sess).WherePK().Exec(ctx)
		return nil, nil
	}
	u, err := s.loadActiveUser(ctx, sess.UserID)
	if err != nil || u == nil {
		return nil, err
	}
	// Sliding expiry, written at most every few minutes.
	if now.Sub(sess.LastSeenAt) > sessionTouchPeriod {
		sess.LastSeenAt = now
		sess.ExpiresAt = now.Add(sessionTTL)
		sess.IP = clientIP(r)
		if _, err := s.db.NewUpdate().Model(sess).Column("last_seen_at", "expires_at", "ip").WherePK().Exec(ctx); err != nil {
			s.log.Warn("touch session", "err", err)
		}
	}
	return &Principal{User: u, Method: MethodSession, Session: sess}, nil
}

// loadActiveUser returns the user or nil if missing or disabled.
func (s *Server) loadActiveUser(ctx context.Context, id int64) (*model.User, error) {
	u := new(model.User)
	err := s.db.NewSelect().Model(u).Where("id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if u.Disabled {
		return nil, nil
	}
	return u, nil
}

func splitScopes(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// csrf enforces the double-submit cookie check on unsafe requests that a
// browser could have sent: anything carrying cookies or an Origin header,
// unless it authenticates with an Authorization header (which browsers
// never attach cross-site). It also hands every client a CSRF cookie.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(csrfCookie)
		if err != nil || c.Value == "" {
			c = &http.Cookie{
				Name:     csrfCookie,
				Value:    auth.RandomToken(24),
				Path:     "/",
				SameSite: http.SameSiteLaxMode,
				Secure:   s.secureCookies(r),
				MaxAge:   int(sessionTTL.Seconds()),
			}
			http.SetCookie(w, c)
			c.Value = "" // a freshly minted cookie can't have been echoed yet
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if bearerToken(r) != "" {
			next.ServeHTTP(w, r)
			return
		}
		browserish := r.Header.Get("Origin") != "" || r.Header.Get("Cookie") != ""
		if browserish {
			got := r.Header.Get(csrfHeader)
			if c.Value == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(c.Value)) != 1 {
				apierr.Write(w, apierr.Forbidden("missing or invalid CSRF token: send the %s cookie value in the %s header", csrfCookie, csrfHeader))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// secureCookies reports whether cookies get the Secure flag: as
// configured, else when the base URL is HTTPS or this request came over
// HTTPS (the base URL may only be set in the web UI's setup).
func (s *Server) secureCookies(r *http.Request) bool {
	if s.cfg.Security.SecureCookies != nil {
		return *s.cfg.Security.SecureCookies
	}
	return s.cfg.UseSecureCookies() || s.isHTTPS(r)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, raw string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookies(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookies(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// createSession stores a new session for u and sets the cookie.
func (s *Server) createSession(ctx context.Context, w http.ResponseWriter, r *http.Request, u *model.User) (*model.Session, error) {
	raw := auth.RandomToken(32)
	now := time.Now().UTC()
	ua := r.UserAgent()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	sess := &model.Session{
		ID:         auth.HashToken(raw),
		UserID:     u.ID,
		IP:         clientIP(r),
		UserAgent:  ua,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(sessionTTL),
	}
	if _, err := s.db.NewInsert().Model(sess).Exec(ctx); err != nil {
		return nil, err
	}
	s.setSessionCookie(w, r, raw)
	return sess, nil
}
