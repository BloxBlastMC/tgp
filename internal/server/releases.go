package server

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/git"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/githook"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

const tagPrefix = "refs/tags/"

// releaseCtx is a release loaded for a request.
type releaseCtx struct {
	repo *model.Repo
	perm authz.Perm
	rel  *model.Release
}

// releaseOrder lists drafts first, then releases by publish date.
const releaseOrder = "CASE WHEN rel.draft THEN 0 ELSE 1 END, COALESCE(rel.published_at, rel.created_at) DESC, rel.id DESC"

func visibleReleases(q *bun.SelectQuery, perm authz.Perm) *bun.SelectQuery {
	if perm < authz.Write {
		q = q.Where("NOT rel.draft")
	}
	return q
}

func (s *Server) loadRelease(ctx context.Context, repoName string, need authz.Perm, where func(*bun.SelectQuery) *bun.SelectQuery, what string) (*releaseCtx, error) {
	repo, perm, err := s.repoFor(ctx, repoName, need)
	if err != nil {
		return nil, err
	}
	rel := new(model.Release)
	q := where(visibleReleases(s.db.NewSelect().Model(rel).Where("rel.repo_id = ?", repo.ID), perm))
	if err := q.Limit(1).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.NotFound("release %s not found in %s", what, repo.Name)
		}
		return nil, err
	}
	return &releaseCtx{repo: repo, perm: perm, rel: rel}, nil
}

func (s *Server) releaseByID(ctx context.Context, repoName string, id int64, need authz.Perm) (*releaseCtx, error) {
	return s.loadRelease(ctx, repoName, need, func(q *bun.SelectQuery) *bun.SelectQuery { return q.Where("rel.id = ?", id) }, "#"+strconv.FormatInt(id, 10))
}

// latestReleaseID is the newest published, non-prerelease release (0 if none).
func (s *Server) latestReleaseID(ctx context.Context, repoID int64) (int64, error) {
	var id int64
	err := s.db.NewSelect().Model((*model.Release)(nil)).Column("rel.id").
		Where("rel.repo_id = ? AND NOT rel.draft AND NOT rel.prerelease", repoID).
		OrderExpr("rel.published_at DESC, rel.id DESC").Limit(1).Scan(ctx, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func (s *Server) toAPIReleases(ctx context.Context, repo *model.Repo, rels []*model.Release) ([]apigen.Release, error) {
	out := make([]apigen.Release, len(rels))
	if len(rels) == 0 {
		return out, nil
	}
	latest, err := s.latestReleaseID(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(rels))
	for i, r := range rels {
		ids[i] = r.ID
	}
	var assets []*model.ReleaseAsset
	if err := s.db.NewSelect().Model(&assets).Where("release_id IN (?)", bun.In(ids)).Order("release_id", "name").Scan(ctx); err != nil {
		return nil, err
	}
	byRel := map[int64][]apigen.ReleaseAsset{}
	uc := s.users()
	for _, a := range assets {
		byRel[a.ReleaseID] = append(byRel[a.ReleaseID], apigen.ReleaseAsset{
			Id: a.ID, Name: a.Name, Label: a.Label, ContentType: a.ContentType, SizeBytes: a.Size, Sha256: a.SHA256,
			DownloadCount: a.Downloads, Uploader: uc.summaryPtr(ctx, a.UploaderID), CreatedAt: a.CreatedAt,
		})
	}
	g := s.repos.Open(repo)
	for i, r := range rels {
		html, err := renderMarkdown(r.Body, markdownCtx(repo, r.Tag))
		if err != nil {
			return nil, err
		}
		v := apigen.Release{
			Id: r.ID, Tag: r.Tag, Name: r.Name, Body: r.Body, BodyHtml: html, Draft: r.Draft, Prerelease: r.Prerelease,
			Latest: r.ID == latest, Target: r.Target, Author: uc.summaryPtr(ctx, r.AuthorID), CreatedAt: r.CreatedAt,
			PublishedAt: r.PublishedAt, Assets: byRel[r.ID],
		}
		if v.Assets == nil {
			v.Assets = []apigen.ReleaseAsset{}
		}
		if sha, err := g.RefCommit(ctx, tagPrefix+r.Tag); err == nil && sha != "" {
			v.Sha = &sha
		}
		out[i] = v
	}
	return out, nil
}

func (s *Server) toAPIRelease(ctx context.Context, repo *model.Repo, rel *model.Release) (apigen.Release, error) {
	l, err := s.toAPIReleases(ctx, repo, []*model.Release{rel})
	if err != nil {
		return apigen.Release{}, err
	}
	return l[0], nil
}

func (a *api) ListReleases(ctx context.Context, req apigen.ListReleasesRequestObject) (apigen.ListReleasesResponseObject, error) {
	s := a.s
	repo, perm, err := s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	limit, offset := pageParams(req.Params.Page, req.Params.PerPage)
	var rels []*model.Release
	total, err := visibleReleases(s.db.NewSelect().Model(&rels).Where("rel.repo_id = ?", repo.ID), perm).
		OrderExpr(releaseOrder).Limit(int64(limit)).Offset(int64(offset)).ScanAndCount(ctx)
	if err != nil {
		return nil, err
	}
	l, err := s.toAPIReleases(ctx, repo, rels)
	if err != nil {
		return nil, err
	}
	return apigen.ListReleases200JSONResponse{Releases: l, Total: int(total)}, nil
}

func (a *api) GetLatestRelease(ctx context.Context, req apigen.GetLatestReleaseRequestObject) (apigen.GetLatestReleaseResponseObject, error) {
	rc, err := a.s.loadRelease(ctx, req.Repo, authz.Read, func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("NOT rel.draft AND NOT rel.prerelease").OrderExpr("rel.published_at DESC, rel.id DESC")
	}, "latest")
	if err != nil {
		return nil, err
	}
	v, err := a.s.toAPIRelease(ctx, rc.repo, rc.rel)
	if err != nil {
		return nil, err
	}
	return apigen.GetLatestRelease200JSONResponse(v), nil
}

func (a *api) GetReleaseByTag(ctx context.Context, req apigen.GetReleaseByTagRequestObject) (apigen.GetReleaseByTagResponseObject, error) {
	rc, err := a.s.loadRelease(ctx, req.Repo, authz.Read, func(q *bun.SelectQuery) *bun.SelectQuery { return q.Where("rel.tag = ?", req.Tag) }, req.Tag)
	if err != nil {
		return nil, err
	}
	v, err := a.s.toAPIRelease(ctx, rc.repo, rc.rel)
	if err != nil {
		return nil, err
	}
	return apigen.GetReleaseByTag200JSONResponse(v), nil
}

func (a *api) GetRelease(ctx context.Context, req apigen.GetReleaseRequestObject) (apigen.GetReleaseResponseObject, error) {
	rc, err := a.s.releaseByID(ctx, req.Repo, req.Id, authz.Read)
	if err != nil {
		return nil, err
	}
	v, err := a.s.toAPIRelease(ctx, rc.repo, rc.rel)
	if err != nil {
		return nil, err
	}
	return apigen.GetRelease200JSONResponse(v), nil
}

func (s *Server) checkReleaseFields(ctx context.Context, tag, name string) error {
	switch {
	case strings.TrimSpace(tag) == "":
		return apierr.Validation("tag can't be empty")
	case !s.git.ValidTagName(ctx, tag):
		return apierr.Validation("%q isn't a valid tag name", tag)
	case utf8.RuneCountInString(name) > 200:
		return apierr.Validation("name is longer than 200 characters")
	}
	return nil
}

// ensureTag returns the commit tag points to, creating the tag at target
// (default: the default branch) if it doesn't exist. created reports
// whether it was created.
func (s *Server) ensureTag(ctx context.Context, repo *model.Repo, tag, target string) (sha string, created bool, err error) {
	g := s.repos.Open(repo)
	sha, err = g.RefCommit(ctx, tagPrefix+tag)
	if err != nil || sha != "" {
		return sha, false, err
	}
	if target == "" {
		target = repo.DefaultBranch
	}
	commit, err := g.ResolveCommit(ctx, target)
	if err != nil {
		if empty, _ := g.IsEmpty(ctx); empty {
			return "", false, apierr.Validation("%s is empty; push a commit before creating a release", repo.Name)
		}
		return "", false, apierr.Validation("target %q isn't a branch or commit in %s", target, repo.Name)
	}
	if err := g.UpdateRef(ctx, tagPrefix+tag, commit, git.ZeroID); err != nil {
		if errors.Is(err, git.ErrRefChanged) {
			return "", false, apierr.Conflict("tag %s was created meanwhile; try again", tag)
		}
		return "", false, err
	}
	return commit, true, nil
}

// published runs what follows publishing a release: CI for a new tag and
// release workflows.
func (s *Server) published(ctx context.Context, repo *model.Repo, actor *model.User, rel *model.Release, sha string, tagCreated bool) {
	ctx = context.WithoutCancel(ctx)
	if tagCreated {
		s.ciOnPush(ctx, repo, actor, []githook.Update{{Ref: tagPrefix + rel.Tag, Old: git.ZeroID, New: sha}})
	}
	s.ciOnRelease(ctx, repo, actor, rel, sha)
}

func (a *api) CreateRelease(ctx context.Context, req apigen.CreateReleaseRequestObject) (apigen.CreateReleaseResponseObject, error) {
	s := a.s
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	if repo.Archived {
		return nil, apierr.Forbidden("%s is archived", repo.Name)
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	b := req.Body
	tag := strings.TrimSpace(b.Tag)
	if err := s.checkReleaseFields(ctx, tag, deref(b.Name)); err != nil {
		return nil, err
	}
	exists, err := s.db.NewSelect().Model((*model.Release)(nil)).Where("rel.repo_id = ? AND rel.tag = ?", repo.ID, tag).Exists(ctx)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, apierr.Conflict("a release for tag %s already exists", tag)
	}
	user := mustPrincipal(ctx).User
	now := s.now()
	rel := &model.Release{RepoID: repo.ID, Tag: tag, Target: strings.TrimSpace(deref(b.Target)), Name: strings.TrimSpace(deref(b.Name)),
		Body: deref(b.Body), Draft: deref(b.Draft), Prerelease: deref(b.Prerelease), AuthorID: &user.ID, CreatedAt: now, UpdatedAt: now}
	if rel.Target == "" {
		rel.Target = repo.DefaultBranch
	}
	var sha string
	var created bool
	if rel.Draft {
		// Check the target now so publishing later doesn't fail.
		if c, _ := s.repos.Open(repo).RefCommit(ctx, tagPrefix+tag); c == "" {
			if _, err := s.repos.Open(repo).ResolveCommit(ctx, rel.Target); err != nil {
				return nil, apierr.Validation("target %q isn't a branch or commit in %s", rel.Target, repo.Name)
			}
		}
	} else {
		if sha, created, err = s.ensureTag(ctx, repo, tag, rel.Target); err != nil {
			return nil, err
		}
		rel.PublishedAt = &now
	}
	if _, err := s.db.NewInsert().Model(rel).Exec(ctx); err != nil {
		if ok, _ := s.db.NewSelect().Model((*model.Release)(nil)).Where("rel.repo_id = ? AND rel.tag = ?", repo.ID, tag).Exists(ctx); ok {
			return nil, apierr.Conflict("a release for tag %s already exists", tag)
		}
		return nil, err
	}
	if !rel.Draft {
		s.published(ctx, repo, user, rel, sha, created)
	}
	v, err := s.toAPIRelease(ctx, repo, rel)
	if err != nil {
		return nil, err
	}
	return apigen.CreateRelease201JSONResponse(v), nil
}

func (a *api) UpdateRelease(ctx context.Context, req apigen.UpdateReleaseRequestObject) (apigen.UpdateReleaseResponseObject, error) {
	s := a.s
	rc, err := s.releaseByID(ctx, req.Repo, req.Id, authz.Write)
	if err != nil {
		return nil, err
	}
	if rc.repo.Archived {
		return nil, apierr.Forbidden("%s is archived", rc.repo.Name)
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	b, rel, repo := req.Body, rc.rel, rc.repo
	wasDraft := rel.Draft
	if b.Tag != nil {
		rel.Tag = strings.TrimSpace(*b.Tag)
	}
	if b.Name != nil {
		rel.Name = strings.TrimSpace(*b.Name)
	}
	if b.Body != nil {
		rel.Body = *b.Body
	}
	if b.Target != nil && strings.TrimSpace(*b.Target) != "" {
		rel.Target = strings.TrimSpace(*b.Target)
	}
	if b.Draft != nil {
		rel.Draft = *b.Draft
	}
	if b.Prerelease != nil {
		rel.Prerelease = *b.Prerelease
	}
	if err := s.checkReleaseFields(ctx, rel.Tag, rel.Name); err != nil {
		return nil, err
	}
	if b.Tag != nil {
		taken, err := s.db.NewSelect().Model((*model.Release)(nil)).Where("rel.repo_id = ? AND rel.tag = ? AND rel.id <> ?", repo.ID, rel.Tag, rel.ID).Exists(ctx)
		if err != nil {
			return nil, err
		}
		if taken {
			return nil, apierr.Conflict("a release for tag %s already exists", rel.Tag)
		}
	}
	now := s.now()
	var sha string
	var created bool
	publishing := wasDraft && !rel.Draft
	if !rel.Draft {
		// A published release always has its tag.
		if sha, created, err = s.ensureTag(ctx, repo, rel.Tag, rel.Target); err != nil {
			return nil, err
		}
	}
	switch {
	case publishing:
		rel.PublishedAt = &now
	case rel.Draft:
		rel.PublishedAt = nil
	}
	rel.UpdatedAt = now
	if _, err := s.db.NewUpdate().Model(rel).Column("tag", "name", "body", "target", "draft", "prerelease", "published_at", "updated_at").
		WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	if publishing {
		s.published(ctx, repo, mustPrincipal(ctx).User, rel, sha, created)
	} else if created {
		s.ciOnPush(context.WithoutCancel(ctx), repo, mustPrincipal(ctx).User, []githook.Update{{Ref: tagPrefix + rel.Tag, Old: git.ZeroID, New: sha}})
	}
	v, err := s.toAPIRelease(ctx, repo, rel)
	if err != nil {
		return nil, err
	}
	return apigen.UpdateRelease200JSONResponse(v), nil
}

func (a *api) DeleteRelease(ctx context.Context, req apigen.DeleteReleaseRequestObject) (apigen.DeleteReleaseResponseObject, error) {
	s := a.s
	rc, err := s.releaseByID(ctx, req.Repo, req.Id, authz.Write)
	if err != nil {
		return nil, err
	}
	rel, repo := rc.rel, rc.repo
	var assets []*model.ReleaseAsset
	if err := s.db.NewSelect().Model(&assets).Where("release_id = ?", rel.ID).Scan(ctx); err != nil {
		return nil, err
	}
	for _, as := range assets {
		if err := s.store.Delete(ctx, as.BlobKey); err != nil {
			return nil, err
		}
	}
	if _, err := s.db.NewDelete().Model(rel).WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	tagDeleted := false
	if deref(req.Params.DeleteTag) {
		if repo.Archived {
			return nil, apierr.Forbidden("%s is archived; the release was deleted but the tag was kept", repo.Name)
		}
		g := s.repos.Open(repo)
		if sha, err := g.RefCommit(ctx, tagPrefix+rel.Tag); err == nil && sha != "" {
			if err := g.DeleteRef(ctx, tagPrefix+rel.Tag, ""); err != nil {
				return nil, err
			}
			tagDeleted = true
		}
	}
	if err := s.record(ctx, s.db, audit.ReleaseDelete, "repo", repo.Name,
		map[string]any{"tag": rel.Tag, "assets": len(assets), "tag_deleted": tagDeleted}); err != nil {
		return nil, err
	}
	return apigen.DeleteRelease204Response{}, nil
}

// validAssetName rejects names that aren't plain file names.
func validAssetName(n string) error {
	switch {
	case strings.TrimSpace(n) == "":
		return apierr.Validation("asset name can't be empty")
	case len(n) > 255:
		return apierr.Validation("asset name is longer than 255 bytes")
	case !utf8.ValidString(n), strings.ContainsAny(n, "/\\\x00\r\n"), n == ".", n == "..":
		return apierr.Validation("asset name %q must be a plain file name", n)
	}
	return nil
}

func (a *api) UploadReleaseAsset(ctx context.Context, req apigen.UploadReleaseAssetRequestObject) (apigen.UploadReleaseAssetResponseObject, error) {
	s := a.s
	rc, err := s.releaseByID(ctx, req.Repo, req.Id, authz.Write)
	if err != nil {
		return nil, err
	}
	if rc.repo.Archived {
		return nil, apierr.Forbidden("%s is archived", rc.repo.Name)
	}
	name := strings.TrimSpace(req.Params.Name)
	if err := validAssetName(name); err != nil {
		return nil, err
	}
	overwrite := deref(req.Params.Overwrite)
	old := new(model.ReleaseAsset)
	err = s.db.NewSelect().Model(old).Where("release_id = ? AND name = ?", rc.rel.ID, name).Scan(ctx)
	switch {
	case err == nil && !overwrite:
		return nil, apierr.Conflict("%s already has a file named %s; delete it first or upload with overwrite", rc.rel.Tag, name)
	case errors.Is(err, sql.ErrNoRows):
		old = nil
	case err != nil:
		return nil, err
	}
	cs, err := s.ciSettings(ctx)
	if err != nil {
		return nil, err
	}
	_, r := httpFrom(ctx)
	key := blobKey("releases", rc.repo.ID, "")
	size, sum, err := s.putBlob(ctx, key, req.Body, r.ContentLength, mb(cs.ReleaseAssetMaxMB), name)
	if err != nil {
		return nil, err
	}
	ct := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	if ct == "" {
		ct = "application/octet-stream"
	}
	user := mustPrincipal(ctx).User
	asset := &model.ReleaseAsset{ReleaseID: rc.rel.ID, RepoID: rc.repo.ID, Name: name, Label: strings.TrimSpace(deref(req.Params.Label)),
		ContentType: ct, Size: size, SHA256: sum, BlobKey: key, UploaderID: &user.ID, CreatedAt: s.now()}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if old != nil {
			if _, err := tx.NewDelete().Model(old).WherePK().Exec(ctx); err != nil {
				return err
			}
		}
		_, err := tx.NewInsert().Model(asset).Exec(ctx)
		return err
	})
	if err != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), key)
		if ok, _ := s.db.NewSelect().Model((*model.ReleaseAsset)(nil)).Where("release_id = ? AND name = ?", rc.rel.ID, name).Exists(ctx); ok {
			return nil, apierr.Conflict("%s already has a file named %s", rc.rel.Tag, name)
		}
		return nil, err
	}
	if old != nil {
		if err := s.store.Delete(ctx, old.BlobKey); err != nil {
			s.log.Warn("delete replaced asset blob", "key", old.BlobKey, "err", err)
		}
	}
	return apigen.UploadReleaseAsset201JSONResponse{
		Id: asset.ID, Name: asset.Name, Label: asset.Label, ContentType: asset.ContentType, SizeBytes: asset.Size,
		Sha256: asset.SHA256, DownloadCount: 0, Uploader: s.users().summaryPtr(ctx, asset.UploaderID), CreatedAt: asset.CreatedAt,
	}, nil
}

func (s *Server) loadAsset(ctx context.Context, rc *releaseCtx, id int64) (*model.ReleaseAsset, error) {
	as := new(model.ReleaseAsset)
	if err := s.db.NewSelect().Model(as).Where("id = ? AND release_id = ?", id, rc.rel.ID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.NotFound("asset %d not found on release %s", id, rc.rel.Tag)
		}
		return nil, err
	}
	return as, nil
}

func (a *api) DownloadReleaseAsset(ctx context.Context, req apigen.DownloadReleaseAssetRequestObject) (apigen.DownloadReleaseAssetResponseObject, error) {
	s := a.s
	rc, err := s.releaseByID(ctx, req.Repo, req.Id, authz.Read)
	if err != nil {
		return nil, err
	}
	as, err := s.loadAsset(ctx, rc, req.AssetId)
	if err != nil {
		return nil, err
	}
	body, err := s.store.Get(ctx, as.BlobKey)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.NewUpdate().Model(as).Set("downloads = downloads + 1").WherePK().Exec(ctx); err != nil {
		s.log.Warn("count asset download", "id", as.ID, "err", err)
	}
	w, _ := httpFrom(ctx)
	w.Header().Set("Content-Disposition", contentDisposition(as.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	return apigen.DownloadReleaseAsset200ApplicationoctetStreamResponse{Body: body, ContentLength: as.Size}, nil
}

func (a *api) DeleteReleaseAsset(ctx context.Context, req apigen.DeleteReleaseAssetRequestObject) (apigen.DeleteReleaseAssetResponseObject, error) {
	s := a.s
	rc, err := s.releaseByID(ctx, req.Repo, req.Id, authz.Write)
	if err != nil {
		return nil, err
	}
	as, err := s.loadAsset(ctx, rc, req.AssetId)
	if err != nil {
		return nil, err
	}
	if err := s.store.Delete(ctx, as.BlobKey); err != nil {
		return nil, err
	}
	if _, err := s.db.NewDelete().Model(as).WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	return apigen.DeleteReleaseAsset204Response{}, nil
}

// ---- source archives ----

func (a *api) GetArchive(ctx context.Context, req apigen.GetArchiveRequestObject) (apigen.GetArchiveResponseObject, error) {
	s := a.s
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	g := s.repos.Open(repo)
	commit, err := g.ResolveCommit(ctx, req.Params.Ref)
	if err != nil {
		return nil, apierr.NotFound("%q isn't a branch, tag or commit in %s", req.Params.Ref, repo.Name)
	}
	format := "tar.gz"
	if req.Params.Format != nil {
		format = string(*req.Params.Format)
	}
	label := strings.NewReplacer("/", "-", "\\", "-").Replace(strings.TrimPrefix(strings.TrimPrefix(req.Params.Ref, "refs/tags/"), "refs/heads/"))
	if strings.HasPrefix(label, ".") {
		label = "_" + label
	}
	name := repo.Name + "-" + label
	// Archives count against the git concurrency limit like clones.
	release, err := s.acquireGit(ctx)
	if err != nil {
		return nil, apierr.New(http.StatusServiceUnavailable, apierr.CodeUnavailable, "the server is busy; try again shortly")
	}
	body, err := g.Archive(ctx, commit, format, name+"/")
	if err != nil {
		release()
		return nil, err
	}
	body = &releaseOnClose{ReadCloser: body, release: release}
	w, _ := httpFrom(ctx)
	w.Header().Set("Content-Disposition", contentDisposition(name+"."+format))
	return apigen.GetArchive200ApplicationoctetStreamResponse{Body: body}, nil
}

// releaseOnClose frees a git slot when the archive stream is closed.
type releaseOnClose struct {
	io.ReadCloser
	release func()
	once    sync.Once
}

func (r *releaseOnClose) Close() error {
	err := r.ReadCloser.Close()
	r.once.Do(r.release)
	return err
}
