package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/git"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/githook"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// gitPathRe matches smart HTTP URLs: /<repo>[.git]/info/refs etc.
var gitPathRe = regexp.MustCompile(`^/([A-Za-z0-9][A-Za-z0-9._-]*?)(?:\.git)?/(info/refs|git-upload-pack|git-receive-pack)$`)

// gitRoutes serves git smart HTTP before the normal router sees the request.
func (s *Server) gitRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := gitPathRe.FindStringSubmatch(r.URL.Path)
		if m == nil {
			next.ServeHTTP(w, r)
			return
		}
		s.serveGit(w, r, m[1], m[2])
	})
}

func gitError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, msg+"\n")
}

func askForCredentials(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Basic realm="tgp"`)
	gitError(w, http.StatusUnauthorized, msg)
}

// gitToken extracts a token from Basic auth (the password; the username is
// ignored) or a Bearer header.
func gitToken(r *http.Request) string {
	if _, pw, ok := r.BasicAuth(); ok {
		return pw
	}
	return bearerToken(r)
}

func (s *Server) serveGit(w http.ResponseWriter, r *http.Request, repoName, action string) {
	ctx := r.Context()
	var svc git.Service
	switch {
	case action == "info/refs" && r.Method == http.MethodGet:
		var ok bool
		if svc, ok = git.ParseService(r.URL.Query().Get("service")); !ok {
			gitError(w, http.StatusForbidden, "only smart HTTP is supported; use git 1.6.6 or newer")
			return
		}
	case action == "git-upload-pack" && r.Method == http.MethodPost:
		svc = git.UploadPack
	case action == "git-receive-pack" && r.Method == http.MethodPost:
		svc = git.ReceivePack
	default:
		gitError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if done, err := s.setupComplete(ctx); err != nil || !done {
		gitError(w, http.StatusServiceUnavailable, "this server hasn't been set up yet")
		return
	}
	raw := gitToken(r)
	if raw == "" {
		askForCredentials(w, "authentication required: use your username and a personal access token as the password")
		return
	}
	if strings.HasPrefix(raw, runnerapi.JobTokenPrefix) {
		s.serveGitForJob(w, r, repoName, action, svc, raw)
		return
	}
	p, err := s.principalFromToken(ctx, raw)
	if err != nil {
		askForCredentials(w, "invalid credentials: use a personal access token (Settings → Personal access tokens) as the password")
		return
	}
	if p.User.MustChangePassword {
		gitError(w, http.StatusForbidden, "you must change your temporary password in the web UI first")
		return
	}
	repo, err := s.repos.Get(ctx, s.db, repoName)
	if err != nil {
		gitError(w, http.StatusNotFound, "repository not found")
		return
	}
	perm, err := authz.RepoPerm(ctx, s.db, p.User, repo)
	if err != nil {
		s.log.Error("git authz", "err", err)
		gitError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if perm < authz.Read || !p.HasScope("read") {
		gitError(w, http.StatusNotFound, "repository not found")
		return
	}
	opts := git.RPCOptions{}
	if svc == git.ReceivePack {
		switch {
		case !p.HasScope("write"):
			gitError(w, http.StatusForbidden, "this token is read-only; create one with the write scope to push")
			return
		case perm < authz.Write:
			gitError(w, http.StatusForbidden, fmt.Sprintf("your role (%s) can't push to %s", p.User.Role, repo.Name))
			return
		case repo.Archived:
			gitError(w, http.StatusForbidden, repo.Name+" is archived and read-only")
			return
		}
		opts.HooksPath = s.hooksDir
		opts.Env = []string{
			githook.EnvURL + "=" + s.internalURL(),
			githook.EnvSecret + "=" + s.hookSecret,
			githook.EnvRepo + "=" + strconv.FormatInt(repo.ID, 10),
			githook.EnvUser + "=" + strconv.FormatInt(p.User.ID, 10),
		}
		if r.Method == http.MethodPost && s.cfg.Repos.MaxPushBytes > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, s.cfg.Repos.MaxPushBytes)
		}
	}

	release, err := s.acquireGit(ctx)
	if err != nil {
		w.Header().Set("Retry-After", "10")
		gitError(w, http.StatusServiceUnavailable, "server busy: too many concurrent git operations, retry shortly")
		return
	}
	defer release()

	path := s.repos.Path(repo)
	if action == "info/refs" {
		err = s.git.AdvertiseRefs(w, r, path, svc, opts)
	} else {
		err = s.git.ServeRPC(w, r, path, svc, opts)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		s.log.Warn("git transfer failed", "repo", repo.Name, "service", svc, "user", p.User.Username, "err", err)
	}
}

// serveGitForJob lets a running CI job clone its own repository with its
// job token (read-only, valid only while the job runs).
func (s *Server) serveGitForJob(w http.ResponseWriter, r *http.Request, repoName, action string, svc git.Service, token string) {
	ctx := r.Context()
	j, err := s.jobByToken(ctx, token)
	if err != nil {
		askForCredentials(w, "invalid or expired job token")
		return
	}
	repo, err := s.repos.Get(ctx, s.db, repoName)
	if err != nil || repo.ID != j.RepoID {
		gitError(w, http.StatusNotFound, "repository not found")
		return
	}
	if svc != git.UploadPack {
		gitError(w, http.StatusForbidden, "job tokens are read-only")
		return
	}
	release, err := s.acquireGit(ctx)
	if err != nil {
		w.Header().Set("Retry-After", "10")
		gitError(w, http.StatusServiceUnavailable, "server busy: too many concurrent git operations, retry shortly")
		return
	}
	defer release()
	path := s.repos.Path(repo)
	if action == "info/refs" {
		err = s.git.AdvertiseRefs(w, r, path, svc, git.RPCOptions{})
	} else {
		err = s.git.ServeRPC(w, r, path, svc, git.RPCOptions{})
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		s.log.Warn("git transfer failed", "repo", repo.Name, "service", svc, "job", j.ID, "err", err)
	}
}

// acquireGit limits concurrent git transfers so a burst of clones (e.g. CI)
// can't exhaust the server. Waits up to 30 seconds for a slot.
func (s *Server) acquireGit(ctx context.Context) (func(), error) {
	if s.gitSem == nil {
		return func() {}, nil
	}
	t := time.NewTimer(30 * time.Second)
	defer t.Stop()
	select {
	case s.gitSem <- struct{}{}:
		return func() { <-s.gitSem }, nil
	case <-t.C:
		return nil, errors.New("git concurrency limit")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// internalURL is how hook processes reach this server process.
func (s *Server) internalURL() string {
	if v, _ := s.hookURL.Load().(string); v != "" {
		return v
	}
	return "http://127.0.0.1" + s.cfg.Server.Listen
}

// SetInternalURL overrides the URL hooks use to call back (tests).
func (s *Server) SetInternalURL(u string) { s.hookURL.Store(u) }

// handleHook serves POST /internal/hooks/{kind} for hook processes.
func (s *Server) handleHook(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(githook.SecretHeader)), []byte(s.hookSecret)) != 1 {
		apierr.Write(w, apierr.Forbidden("bad hook secret"))
		return
	}
	var req githook.Request
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&req); err != nil {
		apierr.Write(w, apierr.BadRequest("bad hook request: %v", err))
		return
	}
	ctx := r.Context()
	repo := new(model.Repo)
	if err := s.db.NewSelect().Model(repo).Where("id = ?", req.RepoID).Scan(ctx); err != nil {
		writeJSON(w, githook.Response{Allowed: false, Messages: []string{"tgp: repository no longer exists"}})
		return
	}
	user, err := s.loadActiveUser(ctx, req.UserID)
	if err != nil {
		s.writeHandlerError(w, r, err)
		return
	}
	var resp githook.Response
	switch chi.URLParam(r, "kind") {
	case githook.PreReceive:
		resp, err = s.preReceive(ctx, repo, user, req.Updates)
	case githook.PostReceive:
		resp, err = s.postReceive(ctx, repo, user, req.Updates)
	default:
		apierr.Write(w, apierr.NotFound("unknown hook"))
		return
	}
	if err != nil {
		s.log.Error("hook failed", "kind", chi.URLParam(r, "kind"), "repo", repo.Name, "err", err)
		resp = githook.Response{Allowed: false, Messages: []string{"tgp: internal error checking push"}}
	}
	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// preReceive decides whether a push is accepted: write access, archived
// state, reserved refs and branch protection rules.
func (s *Server) preReceive(ctx context.Context, repo *model.Repo, user *model.User, updates []githook.Update) (githook.Response, error) {
	deny := func(format string, args ...any) (githook.Response, error) {
		return githook.Response{Allowed: false, Messages: []string{"tgp: " + fmt.Sprintf(format, args...)}}, nil
	}
	if user == nil {
		return deny("your account is disabled")
	}
	perm, err := authz.RepoPerm(ctx, s.db, user, repo)
	if err != nil {
		return githook.Response{}, err
	}
	if perm < authz.Write {
		return deny("you don't have write access to %s", repo.Name)
	}
	if repo.Archived {
		return deny("%s is archived", repo.Name)
	}
	rules, err := s.protectionRules(ctx, s.db, repo.ID)
	if err != nil {
		return githook.Response{}, err
	}
	for _, u := range updates {
		switch {
		case strings.HasPrefix(u.Ref, "refs/heads/"):
			branch := strings.TrimPrefix(u.Ref, "refs/heads/")
			if u.IsDelete() && branch == repo.DefaultBranch {
				return deny("can't delete the default branch %q; change the default branch first", branch)
			}
			if msg := checkProtectedPush(matchRule(rules, branch), branch, user.IsAdmin(), u.IsCreate(), u.IsDelete(), u.Forced); msg != "" {
				return deny("%s", msg)
			}
		case strings.HasPrefix(u.Ref, "refs/tags/"):
		default:
			return deny("%s is reserved; only branches (refs/heads/*) and tags (refs/tags/*) can be pushed", u.Ref)
		}
	}
	return githook.Response{Allowed: true}, nil
}

// postReceive records the push. CI triggers and webhooks plug in here.
func (s *Server) postReceive(ctx context.Context, repo *model.Repo, user *model.User, updates []githook.Update) (githook.Response, error) {
	now := time.Now().UTC()
	repo.PushedAt = &now
	cols := []string{"pushed_at"}

	// If HEAD points at a branch that doesn't exist (first push used a
	// different branch name), adopt a pushed branch as the default.
	g := s.repos.Open(repo)
	if _, err := g.ResolveCommit(ctx, repo.DefaultBranch); err != nil {
		for _, u := range updates {
			if strings.HasPrefix(u.Ref, "refs/heads/") && !u.IsDelete() {
				branch := strings.TrimPrefix(u.Ref, "refs/heads/")
				if err := g.SetHead(ctx, branch); err != nil {
					return githook.Response{Allowed: true}, err
				}
				repo.DefaultBranch = branch
				cols = append(cols, "default_branch")
				break
			}
		}
	}
	if _, err := s.db.NewUpdate().Model(repo).Column(cols...).WherePK().Exec(ctx); err != nil {
		return githook.Response{Allowed: true}, err
	}
	if err := s.syncPulls(ctx, repo, user, updates); err != nil {
		return githook.Response{Allowed: true}, err
	}
	for _, u := range updates {
		if u.Ref == branchRef(repo.DefaultBranch) && !u.IsCreate() && !u.IsDelete() && !u.Forced {
			if err := s.closeIssuesFromPush(ctx, repo, user, u.Old, u.New); err != nil {
				return githook.Response{Allowed: true}, err
			}
		}
	}
	s.ciOnPush(context.WithoutCancel(ctx), repo, user, updates)
	return githook.Response{Allowed: true, Messages: s.pushHints(ctx, repo, updates)}, nil
}
