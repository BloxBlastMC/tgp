package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// validArtifactName rejects names that can't be file names on common
// systems (artifacts download as <name>.zip and unpack into <name>/).
func validArtifactName(n string) error {
	switch {
	case strings.TrimSpace(n) == "":
		return apierr.Validation("artifact name can't be empty")
	case len(n) > runnerapi.MaxArtifactName:
		return apierr.Validation("artifact name is longer than %d characters", runnerapi.MaxArtifactName)
	case strings.ContainsAny(n, "/\\:<>|*?\"\r\n\x00"), n == ".", n == "..":
		return apierr.Validation("artifact name %q can't contain / \\ : < > | * ? \" or line breaks", n)
	}
	return nil
}

// ---- runner side ----

// handleRunnerUploadArtifact stores a zip uploaded by a running job.
func (s *Server) handleRunnerUploadArtifact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j := s.runnerJob(w, r)
	if j == nil {
		return
	}
	q := r.URL.Query()
	name := q.Get("name")
	if err := validArtifactName(name); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	cs, err := s.ciSettings(ctx)
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	days := cs.ArtifactRetentionDays
	if v := q.Get("retention_days"); v != "" {
		d, err := strconv.Atoi(v)
		if err != nil || d < 1 {
			apierr.Write(w, apierr.Validation("retention-days must be a whole number of days ≥ 1"))
			return
		}
		days = min(d, cs.ArtifactRetentionDays)
	}
	overwrite := q.Get("overwrite") == "1" || q.Get("overwrite") == "true"
	exists, err := s.db.NewSelect().Model((*model.Artifact)(nil)).
		Where("run_id = ? AND attempt = ? AND name = ?", j.RunID, j.Attempt, name).Exists(ctx)
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	if exists && !overwrite {
		apierr.Write(w, apierr.Conflict("an artifact named %q already exists in this run; use a different name or set overwrite: true", name))
		return
	}
	key := blobKey("artifacts", j.RepoID, ".zip")
	size, sum, err := s.putBlob(ctx, key, r.Body, r.ContentLength, mb(cs.ArtifactMaxMB), "artifact "+strconv.Quote(name))
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	now := s.now()
	a := &model.Artifact{RepoID: j.RepoID, RunID: j.RunID, JobID: &j.ID, JobName: j.Name, Attempt: j.Attempt, Name: name,
		Size: size, SHA256: sum, BlobKey: key, CreatedAt: now, ExpiresAt: now.Add(time.Duration(days) * 24 * time.Hour)}
	var old []*model.Artifact
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := tx.NewSelect().Model(&old).Where("run_id = ? AND attempt = ? AND name = ?", j.RunID, j.Attempt, name).Scan(ctx); err != nil {
			return err
		}
		if len(old) > 0 {
			if !overwrite {
				return apierr.Conflict("an artifact named %q already exists in this run", name)
			}
			if _, err := tx.NewDelete().Model((*model.Artifact)(nil)).Where("id = ?", old[0].ID).Exec(ctx); err != nil {
				return err
			}
		}
		_, err := tx.NewInsert().Model(a).Exec(ctx)
		return err
	})
	if err != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), key)
		apierr.Write(w, apierr.From(err))
		return
	}
	for _, o := range old {
		if err := s.store.Delete(ctx, o.BlobKey); err != nil {
			s.log.Warn("delete replaced artifact blob", "key", o.BlobKey, "err", err)
		}
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, runnerapi.Artifact{ID: a.ID, Name: a.Name, Size: a.Size, SHA256: a.SHA256, Attempt: a.Attempt, JobName: a.JobName})
}

// runArtifacts returns the newest artifact of each name from attempts up
// to attempt (0 = all), newest first.
func (s *Server) runArtifacts(ctx context.Context, runID int64, attempt int) ([]*model.Artifact, error) {
	var all []*model.Artifact
	q := s.db.NewSelect().Model(&all).Where("run_id = ?", runID).Where("expires_at > ?", s.now()).OrderExpr("attempt DESC, id DESC")
	if attempt > 0 {
		q = q.Where("attempt <= ?", attempt)
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := all[:0]
	for _, a := range all {
		if !seen[a.Name] {
			seen[a.Name] = true
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *Server) handleRunnerListArtifacts(w http.ResponseWriter, r *http.Request) {
	j := s.runnerJob(w, r)
	if j == nil {
		return
	}
	arts, err := s.runArtifacts(r.Context(), j.RunID, j.Attempt)
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	out := runnerapi.ArtifactList{Artifacts: []runnerapi.Artifact{}}
	for _, a := range arts {
		out.Artifacts = append(out.Artifacts, runnerapi.Artifact{ID: a.ID, Name: a.Name, Size: a.Size, SHA256: a.SHA256, Attempt: a.Attempt, JobName: a.JobName})
	}
	writeJSON(w, out)
}

func (s *Server) handleRunnerDownloadArtifact(w http.ResponseWriter, r *http.Request) {
	j := s.runnerJob(w, r)
	if j == nil {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "artifact"), 10, 64)
	a := new(model.Artifact)
	if err := s.db.NewSelect().Model(a).Where("id = ? AND run_id = ? AND expires_at > ?", id, j.RunID, s.now()).Scan(r.Context()); err != nil {
		apierr.Write(w, apierr.NotFound("artifact %d isn't part of this run", id))
		return
	}
	if err := s.serveBlob(r.Context(), w, a.BlobKey, "application/zip", a.Name+".zip", a.Size); err != nil {
		s.log.Warn("serve artifact", "id", a.ID, "err", err)
	}
}

// ---- API ----

func (s *Server) toAPIArtifact(a *model.Artifact, runNumber int64) apigen.Artifact {
	return apigen.Artifact{Id: a.ID, Name: a.Name, SizeBytes: a.Size, Sha256: a.SHA256, RunNumber: runNumber,
		Attempt: a.Attempt, JobId: a.JobID, JobName: a.JobName, CreatedAt: a.CreatedAt, ExpiresAt: a.ExpiresAt}
}

func (a *api) ListRunArtifacts(ctx context.Context, req apigen.ListRunArtifactsRequestObject) (apigen.ListRunArtifactsResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	run, err := a.s.loadRun(ctx, repo, req.Number)
	if err != nil {
		return nil, err
	}
	arts, err := a.s.runArtifacts(ctx, run.ID, 0)
	if err != nil {
		return nil, err
	}
	out := make([]apigen.Artifact, len(arts))
	for i, x := range arts {
		out[i] = a.s.toAPIArtifact(x, run.Number)
	}
	return apigen.ListRunArtifacts200JSONResponse(out), nil
}

// runNumbers maps run IDs to their numbers.
func (s *Server) runNumbers(ctx context.Context, ids []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(ids) == 0 {
		return out, nil
	}
	var runs []model.WorkflowRun
	if err := s.db.NewSelect().Model(&runs).Column("id", "number").Where("id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range runs {
		out[r.ID] = r.Number
	}
	return out, nil
}

func (a *api) ListArtifacts(ctx context.Context, req apigen.ListArtifactsRequestObject) (apigen.ListArtifactsResponseObject, error) {
	s := a.s
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	limit, offset := pageParams(req.Params.Page, req.Params.PerPage)
	var arts []*model.Artifact
	q := s.db.NewSelect().Model(&arts).Where("repo_id = ? AND expires_at > ?", repo.ID, s.now())
	if req.Params.Name != nil && *req.Params.Name != "" {
		q = q.Where("name = ?", *req.Params.Name)
	}
	total, err := q.OrderExpr("id DESC").Limit(int64(limit)).Offset(int64(offset)).ScanAndCount(ctx)
	if err != nil {
		return nil, err
	}
	var sum struct {
		Total int64 `bun:"total"`
	}
	if err := s.db.NewSelect().Model((*model.Artifact)(nil)).ColumnExpr("COALESCE(SUM(size), 0) AS total").
		Where("repo_id = ? AND expires_at > ?", repo.ID, s.now()).Scan(ctx, &sum); err != nil {
		return nil, err
	}
	ids := make([]int64, len(arts))
	for i, x := range arts {
		ids[i] = x.RunID
	}
	nums, err := s.runNumbers(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := apigen.ArtifactPage{Artifacts: make([]apigen.Artifact, len(arts)), Total: int(total), TotalBytes: sum.Total}
	for i, x := range arts {
		out.Artifacts[i] = s.toAPIArtifact(x, nums[x.RunID])
	}
	return apigen.ListArtifacts200JSONResponse(out), nil
}

func (s *Server) loadArtifact(ctx context.Context, repo *model.Repo, id int64) (*model.Artifact, int64, error) {
	art := new(model.Artifact)
	err := s.db.NewSelect().Model(art).Where("id = ? AND repo_id = ? AND expires_at > ?", id, repo.ID, s.now()).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, apierr.NotFound("artifact %d not found in %s (it may have expired)", id, repo.Name)
	}
	if err != nil {
		return nil, 0, err
	}
	nums, err := s.runNumbers(ctx, []int64{art.RunID})
	return art, nums[art.RunID], err
}

func (a *api) GetArtifact(ctx context.Context, req apigen.GetArtifactRequestObject) (apigen.GetArtifactResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	art, num, err := a.s.loadArtifact(ctx, repo, req.Id)
	if err != nil {
		return nil, err
	}
	return apigen.GetArtifact200JSONResponse(a.s.toAPIArtifact(art, num)), nil
}

func (a *api) DownloadArtifact(ctx context.Context, req apigen.DownloadArtifactRequestObject) (apigen.DownloadArtifactResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	art, _, err := a.s.loadArtifact(ctx, repo, req.Id)
	if err != nil {
		return nil, err
	}
	rc, err := a.s.store.Get(ctx, art.BlobKey)
	if err != nil {
		return nil, err
	}
	w, _ := httpFrom(ctx)
	w.Header().Set("Content-Disposition", contentDisposition(art.Name+".zip"))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	return apigen.DownloadArtifact200ApplicationzipResponse{Body: rc, ContentLength: art.Size}, nil
}

func (a *api) DeleteArtifact(ctx context.Context, req apigen.DeleteArtifactRequestObject) (apigen.DeleteArtifactResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	art, _, err := a.s.loadArtifact(ctx, repo, req.Id)
	if err != nil {
		return nil, err
	}
	if err := a.s.deleteArtifacts(ctx, []*model.Artifact{art}); err != nil {
		return nil, err
	}
	return apigen.DeleteArtifact204Response{}, nil
}

// deleteArtifacts removes blobs, then rows. A blob that can't be deleted
// keeps its row so the janitor retries.
func (s *Server) deleteArtifacts(ctx context.Context, arts []*model.Artifact) error {
	var ids []int64
	var firstErr error
	for _, a := range arts {
		if err := s.store.Delete(ctx, a.BlobKey); err != nil {
			s.log.Warn("delete artifact blob", "key", a.BlobKey, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ids = append(ids, a.ID)
	}
	if len(ids) > 0 {
		if _, err := s.db.NewDelete().Model((*model.Artifact)(nil)).Where("id IN (?)", bun.List(ids)).Exec(ctx); err != nil {
			return err
		}
	}
	return firstErr
}

// expireArtifacts deletes artifacts past their expiry.
func (s *Server) expireArtifacts(ctx context.Context) error {
	var arts []*model.Artifact
	if err := s.db.NewSelect().Model(&arts).Where("expires_at <= ?", s.now()).Limit(500).Scan(ctx); err != nil {
		return err
	}
	return s.deleteArtifacts(ctx, arts)
}
