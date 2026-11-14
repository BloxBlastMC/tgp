package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

func validCacheKey(k string) error {
	switch {
	case strings.TrimSpace(k) == "":
		return apierr.Validation("cache key can't be empty")
	case len(k) > runnerapi.MaxCacheKey:
		return apierr.Validation("cache key is longer than %d characters", runnerapi.MaxCacheKey)
	case strings.ContainsAny(k, ",\r\n\x00"):
		return apierr.Validation("cache key %q can't contain commas or line breaks", k)
	}
	return nil
}

// cacheScopes are the refs a job reads caches from, in order: its own ref,
// the pull request's base branch, then the default branch. Jobs only
// write to their own ref, so a branch can't poison another's caches.
func (s *Server) cacheScopes(ctx context.Context, j *model.Job) (string, []string, error) {
	run := new(model.WorkflowRun)
	if err := s.db.NewSelect().Model(run).Column("id", "ref", "pull_number").Where("id = ?", j.RunID).Scan(ctx); err != nil {
		return "", nil, err
	}
	repo := new(model.Repo)
	if err := s.db.NewSelect().Model(repo).Column("id", "default_branch").Where("id = ?", j.RepoID).Scan(ctx); err != nil {
		return "", nil, err
	}
	scopes := []string{run.Ref}
	if run.PullNumber != nil {
		var base string
		err := s.db.NewSelect().Table("pull_requests").Column("pull_requests.base_branch").
			Join("JOIN issues ON issues.id = pull_requests.issue_id").
			Where("issues.repo_id = ? AND issues.number = ?", j.RepoID, *run.PullNumber).Scan(ctx, &base)
		if err == nil && base != "" {
			scopes = append(scopes, "refs/heads/"+base)
		}
	}
	if def := "refs/heads/" + repo.DefaultBranch; def != scopes[0] && (len(scopes) < 2 || scopes[1] != def) {
		scopes = append(scopes, def)
	}
	return run.Ref, scopes, nil
}

// findCache implements the lookup rules of runnerapi.CacheLookupRequest.
func (s *Server) findCache(ctx context.Context, repoID int64, scopes []string, req *runnerapi.CacheLookupRequest) (*model.CacheEntry, bool, error) {
	for _, ref := range scopes {
		e := new(model.CacheEntry)
		err := s.db.NewSelect().Model(e).Where("repo_id = ? AND ref = ? AND version = ? AND key = ?", repoID, ref, req.Version, req.Key).Scan(ctx)
		if err == nil {
			return e, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, false, err
		}
		for _, prefix := range req.RestoreKeys {
			if prefix == "" {
				continue
			}
			// Prefix match without LIKE so % and _ in keys are literal.
			err := s.db.NewSelect().Model(e).Where("repo_id = ? AND ref = ? AND version = ?", repoID, ref, req.Version).
				Where("SUBSTR(key, 1, ?) = ?", len(prefix), prefix).OrderExpr("created_at DESC, id DESC").Limit(1).Scan(ctx)
			if err == nil {
				return e, false, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, false, err
			}
		}
	}
	return nil, false, nil
}

func (s *Server) handleRunnerCacheLookup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j := s.runnerJob(w, r)
	if j == nil {
		return
	}
	var req runnerapi.CacheLookupRequest
	if err := decodeJSON(r, &req); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	if err := validCacheKey(req.Key); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	_, scopes, err := s.cacheScopes(ctx, j)
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	e, exact, err := s.findCache(ctx, j.RepoID, scopes, &req)
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	resp := runnerapi.CacheLookupResponse{Exact: exact}
	if e != nil {
		resp.Entry = &runnerapi.CacheEntry{ID: e.ID, Key: e.Key, Ref: e.Ref, Size: e.Size}
		if _, err := s.db.NewUpdate().Model(e).Set("last_used_at = ?", s.now()).WherePK().Exec(ctx); err != nil {
			s.log.Warn("touch cache", "id", e.ID, "err", err)
		}
	}
	writeJSON(w, resp)
}

func (s *Server) handleRunnerCacheDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j := s.runnerJob(w, r)
	if j == nil {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "cache"), 10, 64)
	_, scopes, err := s.cacheScopes(ctx, j)
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	e := new(model.CacheEntry)
	if err := s.db.NewSelect().Model(e).Where("id = ? AND repo_id = ? AND ref IN (?)", id, j.RepoID, bun.In(scopes)).Scan(ctx); err != nil {
		apierr.Write(w, apierr.NotFound("cache %d not found (it may have been evicted)", id))
		return
	}
	if err := s.serveBlob(ctx, w, e.BlobKey, "application/gzip", "cache.tgz", e.Size); err != nil {
		s.log.Warn("serve cache", "id", e.ID, "err", err)
	}
}

func (s *Server) handleRunnerCacheUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j := s.runnerJob(w, r)
	if j == nil {
		return
	}
	key, version := r.URL.Query().Get("key"), r.URL.Query().Get("version")
	if err := validCacheKey(key); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	if version == "" || len(version) > 128 {
		apierr.Write(w, apierr.Validation("cache version is required"))
		return
	}
	ref, _, err := s.cacheScopes(ctx, j)
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	exists, err := s.db.NewSelect().Model((*model.CacheEntry)(nil)).
		Where("repo_id = ? AND ref = ? AND key = ? AND version = ?", j.RepoID, ref, key, version).Exists(ctx)
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	if exists {
		apierr.Write(w, apierr.Conflict("a cache with key %q already exists for %s", key, ref))
		return
	}
	cs, err := s.ciSettings(ctx)
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	limit := mb(cs.ArtifactMaxMB) // no single cache may exceed the artifact limit or the quota
	if cs.CacheQuotaMB > 0 {
		limit = min(limit, mb(cs.CacheQuotaMB))
	}
	blob := blobKey("caches", j.RepoID, ".tgz")
	size, _, err := s.putBlob(ctx, blob, r.Body, r.ContentLength, limit, "cache "+strconv.Quote(key))
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	now := s.now()
	e := &model.CacheEntry{RepoID: j.RepoID, Ref: ref, Key: key, Version: version, Size: size, BlobKey: blob, CreatedAt: now, LastUsedAt: now}
	if _, err := s.db.NewInsert().Model(e).Exec(ctx); err != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), blob)
		if ok, _ := s.db.NewSelect().Model((*model.CacheEntry)(nil)).
			Where("repo_id = ? AND ref = ? AND key = ? AND version = ?", j.RepoID, ref, key, version).Exists(ctx); ok {
			apierr.Write(w, apierr.Conflict("a cache with key %q was saved by another job meanwhile", key))
			return
		}
		apierr.Write(w, apierr.From(err))
		return
	}
	if cs.CacheQuotaMB > 0 {
		if err := s.enforceCacheQuota(ctx, j.RepoID, mb(cs.CacheQuotaMB), e.ID); err != nil {
			s.log.Warn("cache quota", "repo", j.RepoID, "err", err)
		}
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, runnerapi.CacheEntry{ID: e.ID, Key: e.Key, Ref: e.Ref, Size: e.Size})
}

// enforceCacheQuota evicts a repository's least recently used caches
// (never keep) until its total is within quota.
func (s *Server) enforceCacheQuota(ctx context.Context, repoID, quota, keep int64) error {
	var entries []*model.CacheEntry
	if err := s.db.NewSelect().Model(&entries).Column("id", "size", "blob_key").Where("repo_id = ?", repoID).
		OrderExpr("last_used_at DESC, id DESC").Scan(ctx); err != nil {
		return err
	}
	var total int64
	var evict []*model.CacheEntry
	for _, e := range entries {
		if total+e.Size > quota && e.ID != keep {
			evict = append(evict, e)
			continue
		}
		total += e.Size
	}
	return s.deleteCacheEntries(ctx, evict)
}

func (s *Server) deleteCacheEntries(ctx context.Context, entries []*model.CacheEntry) error {
	var ids []int64
	var firstErr error
	for _, e := range entries {
		if err := s.store.Delete(ctx, e.BlobKey); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ids = append(ids, e.ID)
	}
	if len(ids) > 0 {
		if _, err := s.db.NewDelete().Model((*model.CacheEntry)(nil)).Where("id IN (?)", bun.In(ids)).Exec(ctx); err != nil {
			return err
		}
	}
	return firstErr
}

// expireCaches deletes caches unused for the retention period and
// re-applies quotas (which an admin may have lowered).
func (s *Server) expireCaches(ctx context.Context, cs ciSettings) error {
	var old []*model.CacheEntry
	cutoff := s.now().AddDate(0, 0, -cs.CacheRetentionDays)
	if err := s.db.NewSelect().Model(&old).Where("last_used_at < ?", cutoff).Limit(500).Scan(ctx); err != nil {
		return err
	}
	if err := s.deleteCacheEntries(ctx, old); err != nil {
		return err
	}
	if cs.CacheQuotaMB <= 0 {
		return nil
	}
	var over []struct {
		RepoID int64 `bun:"repo_id"`
	}
	if err := s.db.NewSelect().Model((*model.CacheEntry)(nil)).Column("repo_id").Group("repo_id").
		Having("SUM(size) > ?", mb(cs.CacheQuotaMB)).Scan(ctx, &over); err != nil {
		return err
	}
	for _, o := range over {
		if err := s.enforceCacheQuota(ctx, o.RepoID, mb(cs.CacheQuotaMB), 0); err != nil {
			return err
		}
	}
	return nil
}

// ---- API ----

func toAPICache(e *model.CacheEntry) apigen.CacheEntry {
	return apigen.CacheEntry{Id: e.ID, Key: e.Key, Version: e.Version, Ref: e.Ref, SizeBytes: e.Size, CreatedAt: e.CreatedAt, LastUsedAt: e.LastUsedAt}
}

func (a *api) ListCaches(ctx context.Context, req apigen.ListCachesRequestObject) (apigen.ListCachesResponseObject, error) {
	s := a.s
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	p := req.Params
	limit, offset := pageParams(p.Page, p.PerPage)
	var entries []*model.CacheEntry
	q := s.db.NewSelect().Model(&entries).Where("repo_id = ?", repo.ID)
	if p.Key != nil && *p.Key != "" {
		q = q.Where("SUBSTR(key, 1, ?) = ?", len(*p.Key), *p.Key)
	}
	if p.Ref != nil && *p.Ref != "" {
		q = q.Where("ref = ?", *p.Ref)
	}
	switch deref(p.Sort) {
	case apigen.ListCachesParamsSortCreated:
		q = q.OrderExpr("created_at DESC, id DESC")
	case apigen.ListCachesParamsSortSize:
		q = q.OrderExpr("size DESC, id DESC")
	default:
		q = q.OrderExpr("last_used_at DESC, id DESC")
	}
	total, err := q.Limit(int64(limit)).Offset(int64(offset)).ScanAndCount(ctx)
	if err != nil {
		return nil, err
	}
	var sum struct {
		Total int64 `bun:"total"`
	}
	if err := s.db.NewSelect().Model((*model.CacheEntry)(nil)).ColumnExpr("COALESCE(SUM(size), 0) AS total").
		Where("repo_id = ?", repo.ID).Scan(ctx, &sum); err != nil {
		return nil, err
	}
	cs, err := s.ciSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := apigen.CachePage{Caches: make([]apigen.CacheEntry, len(entries)), Total: int(total), TotalBytes: sum.Total, QuotaBytes: mb(cs.CacheQuotaMB)}
	for i, e := range entries {
		out.Caches[i] = toAPICache(e)
	}
	return apigen.ListCaches200JSONResponse(out), nil
}

func (a *api) DeleteCaches(ctx context.Context, req apigen.DeleteCachesRequestObject) (apigen.DeleteCachesResponseObject, error) {
	s := a.s
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	var entries []*model.CacheEntry
	q := s.db.NewSelect().Model(&entries).Where("repo_id = ?", repo.ID)
	if k := deref(req.Params.Key); k != "" {
		q = q.Where("key = ?", k)
	}
	if r := deref(req.Params.Ref); r != "" {
		q = q.Where("ref = ?", r)
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	if err := s.deleteCacheEntries(ctx, entries); err != nil {
		return nil, err
	}
	return apigen.DeleteCaches200JSONResponse{Deleted: len(entries)}, nil
}

func (a *api) DeleteCache(ctx context.Context, req apigen.DeleteCacheRequestObject) (apigen.DeleteCacheResponseObject, error) {
	s := a.s
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	e := new(model.CacheEntry)
	if err := s.db.NewSelect().Model(e).Where("id = ? AND repo_id = ?", req.Id, repo.ID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.NotFound("cache %d not found in %s", req.Id, repo.Name)
		}
		return nil, err
	}
	if err := s.deleteCacheEntries(ctx, []*model.CacheEntry{e}); err != nil {
		return nil, err
	}
	return apigen.DeleteCache204Response{}, nil
}
