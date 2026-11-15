package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/scheduler"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// fetchWait is how long a fetch long-polls before answering 204.
var fetchWait = 25 * time.Second

type runnerCtxKey struct{}

func runnerFrom(ctx context.Context) *model.Runner {
	r, _ := ctx.Value(runnerCtxKey{}).(*model.Runner)
	return r
}

// runnerAPI serves the protocol in internal/ci/runnerapi.
func (s *Server) runnerAPI() http.Handler {
	r := chi.NewRouter()
	// Artifact and cache uploads enforce their own size limits.
	r.Use(limitBody(8<<20, func(r *http.Request) bool { return r.Method == http.MethodPut }))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		apierr.Write(w, apierr.NotFound("no runner API route for %s %s", r.Method, r.URL.Path))
	})
	r.Post("/register", s.handleRunnerRegister)
	r.Group(func(r chi.Router) {
		r.Use(s.runnerAuth)
		r.Post("/fetch", s.handleRunnerFetch)
		r.Post("/heartbeat", s.handleRunnerHeartbeat)
		r.Post("/jobs/{id}/logs", s.handleRunnerLogs)
		r.Post("/jobs/{id}/update", s.handleRunnerUpdate)
		r.Post("/jobs/{id}/complete", s.handleRunnerComplete)
		r.Put("/jobs/{id}/artifacts", s.handleRunnerUploadArtifact)
		r.Get("/jobs/{id}/artifacts", s.handleRunnerListArtifacts)
		r.Get("/jobs/{id}/artifacts/{artifact}", s.handleRunnerDownloadArtifact)
		r.Post("/jobs/{id}/caches/lookup", s.handleRunnerCacheLookup)
		r.Get("/jobs/{id}/caches/{cache}", s.handleRunnerCacheDownload)
		r.Put("/jobs/{id}/caches", s.handleRunnerCacheUpload)
	})
	return r
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return apierr.BadRequest("invalid JSON body: %v", err)
	}
	return nil
}

func (s *Server) runnerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := bearerToken(r)
		if !strings.HasPrefix(tok, runnerapi.RunnerTokenPrefix) {
			apierr.Write(w, apierr.Unauthenticated("runner token required"))
			return
		}
		rn := new(model.Runner)
		if err := s.db.NewSelect().Model(rn).Where("token_hash = ?", auth.HashToken(tok)).Scan(r.Context()); err != nil {
			apierr.Write(w, apierr.Unauthenticated("unknown runner token; the runner was removed, register it again"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), runnerCtxKey{}, rn)))
	})
}

var runnerLabelRe = strings.NewReplacer(" ", "", "\t", "")

func cleanLabels(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, l := range in {
		l = runnerLabelRe.Replace(strings.TrimSpace(l))
		if l == "" || len(l) > 64 || seen[strings.ToLower(l)] {
			continue
		}
		seen[strings.ToLower(l)] = true
		out = append(out, l)
		if len(out) == 50 {
			break
		}
	}
	return out
}

func (s *Server) handleRunnerRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req runnerapi.RegisterRequest
	if err := decodeJSON(r, &req); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		apierr.Write(w, apierr.Validation("runner name must be 1-100 characters"))
		return
	}
	token := runnerapi.RunnerTokenPrefix + auth.RandomToken(32)
	now := s.now()
	rn := &model.Runner{Name: name, Labels: mustJSON(cleanLabels(req.Labels)), TokenHash: auth.HashToken(token),
		Capacity: min(max(req.Capacity, 1), 256), Version: req.Version, OS: req.OS, Arch: req.Arch, CreatedAt: now, LastSeenAt: &now}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		rt := new(model.RunnerToken)
		err := tx.NewSelect().Model(rt).Where("token_hash = ?", auth.HashToken(req.Token)).Scan(ctx)
		switch {
		case errors.Is(err, sql.ErrNoRows) || !strings.HasPrefix(req.Token, runnerapi.RegistrationTokenPrefix):
			return apierr.Unauthenticated("invalid registration token; create one under Admin → Runners")
		case err != nil:
			return err
		case rt.UsedAt != nil:
			return apierr.Unauthenticated("this registration token was already used; create a new one")
		case now.After(rt.ExpiresAt):
			return apierr.Unauthenticated("this registration token expired; create a new one")
		}
		// Claim the token before creating the runner. The conditional update
		// makes the claim atomic: of concurrent requests (possible on
		// Postgres) only one sees a row change; the others roll back.
		res, err := tx.NewUpdate().Model((*model.RunnerToken)(nil)).Set("used_at = ?", now).
			Where("id = ? AND used_at IS NULL", rt.ID).Exec(ctx)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return apierr.Unauthenticated("this registration token was already used; create a new one")
		}
		rn.CreatedByID = rt.CreatedByID
		if _, err := tx.NewInsert().Model(rn).Exec(ctx); err != nil {
			return err
		}
		rt.UsedAt, rt.RunnerID = &now, &rn.ID
		if _, err := tx.NewUpdate().Model(rt).Column("runner_id").WherePK().Exec(ctx); err != nil {
			return err
		}
		e := audit.Entry{Action: audit.RunnerRegister, TargetType: "runner", Target: rn.Name, IP: clientIP(r),
			ActorName: "runner", Details: map[string]any{"id": rn.ID, "labels": jsonStrings(rn.Labels), "capacity": rn.Capacity}}
		return audit.Record(ctx, tx, e)
	})
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	s.log.Info("runner registered", "id", rn.ID, "name", rn.Name)
	writeJSON(w, runnerapi.RegisterResponse{ID: rn.ID, Name: rn.Name, Token: token, Labels: jsonStrings(rn.Labels)})
}

// touchRunner records a runner's report.
func (s *Server) touchRunner(ctx context.Context, rn *model.Runner, capacity int, load runnerapi.Load, labels []string, version, hostMode string) error {
	now := s.now()
	rn.LastSeenAt = &now
	cpu, mem := load.CPUPercent, load.MemoryPercent
	rn.CPUPercent, rn.MemoryPercent = &cpu, &mem
	cols := []string{"last_seen_at", "cpu_percent", "memory_percent"}
	if capacity > 0 {
		rn.Capacity = min(capacity, 256)
		cols = append(cols, "capacity")
	}
	if labels != nil {
		rn.Labels = mustJSON(cleanLabels(labels))
		cols = append(cols, "labels")
		// Fetches always report the host mode; older runners don't send
		// one and never run host jobs.
		if !runnerapi.ValidHostMode(hostMode) {
			hostMode = runnerapi.HostOff
		}
		rn.HostMode = hostMode
		cols = append(cols, "host_mode")
	}
	if version != "" {
		rn.Version = version
		cols = append(cols, "version")
	}
	_, err := s.db.NewUpdate().Model(rn).Column(cols...).WherePK().Exec(ctx)
	return err
}

func (s *Server) handleRunnerFetch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rn := runnerFrom(ctx)
	var req runnerapi.FetchRequest
	if err := decodeJSON(r, &req); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	deadline := time.NewTimer(fetchWait)
	defer deadline.Stop()
	for {
		wake := s.ci.wake.wait()
		if err := s.touchRunner(ctx, rn, req.Capacity, req.Load, req.Labels, req.Version, req.HostMode); err != nil {
			apierr.Write(w, apierr.From(err))
			return
		}
		spec, err := s.assignJob(ctx, rn)
		if err != nil {
			s.log.Error("ci: assign job", "runner", rn.Name, "err", err)
			apierr.Write(w, apierr.From(err))
			return
		}
		if spec != nil {
			s.log.Info("ci: job assigned", "job", spec.ID, "repo", spec.Repo, "run", spec.RunNumber, "runner", rn.Name)
			writeJSON(w, runnerapi.FetchResponse{Job: spec})
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			w.WriteHeader(http.StatusNoContent)
			return
		case <-wake:
		case <-time.After(2 * time.Second): // jobs queued by other server nodes
		}
	}
}

func (s *Server) handleRunnerHeartbeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rn := runnerFrom(ctx)
	var req runnerapi.HeartbeatRequest
	if err := decodeJSON(r, &req); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	if err := s.touchRunner(ctx, rn, req.Capacity, req.Load, nil, "", ""); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	now := s.now()
	var jobs []*model.Job
	if err := s.db.NewSelect().Model(&jobs).Where("runner_id = ? AND status = ?", rn.ID, model.JobInProgress).Scan(ctx); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	reported := map[int64]bool{}
	for _, id := range req.Running {
		reported[id] = true
	}
	resp := runnerapi.HeartbeatResponse{Cancel: []int64{}, Unknown: []int64{}}
	known := map[int64]bool{}
	changed := false
	for _, j := range jobs {
		known[j.ID] = true
		switch {
		case reported[j.ID]:
			if _, err := s.db.NewUpdate().Model(j).Set("heartbeat_at = ?", now).WherePK().Exec(ctx); err != nil {
				apierr.Write(w, apierr.From(err))
				return
			}
			if j.CancelRequested {
				resp.Cancel = append(resp.Cancel, j.ID)
			}
		case j.StartedAt != nil && now.Sub(*j.StartedAt) > 3*runnerapi.HeartbeatInterval:
			// The runner no longer has this job (restarted or crashed).
			job := j
			if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
				return s.completeJob(ctx, tx, job, model.ConclusionFailure, "The runner lost the job (it restarted or crashed)", nil)
			}); err != nil {
				apierr.Write(w, apierr.From(err))
				return
			}
			changed = true
		}
	}
	for id := range reported {
		if !known[id] {
			resp.Unknown = append(resp.Unknown, id)
		}
	}
	if changed {
		s.ci.wake.notify()
		s.ci.logs.notify()
	}
	writeJSON(w, resp)
}

// runnerJob loads a job that must be running on the calling runner.
func (s *Server) runnerJob(w http.ResponseWriter, r *http.Request) *model.Job {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		apierr.Write(w, apierr.BadRequest("invalid job id"))
		return nil
	}
	j := new(model.Job)
	err = s.db.NewSelect().Model(j).Where("id = ? AND runner_id = ?", id, runnerFrom(r.Context()).ID).Scan(r.Context())
	if err != nil {
		apierr.Write(w, apierr.NotFound("job %d isn't assigned to this runner", id))
		return nil
	}
	if j.Status != model.JobInProgress {
		apierr.Write(w, apierr.Conflict("job %d is no longer running (%s); stop it", id, j.Conclusion))
		return nil
	}
	return j
}

func (s *Server) handleRunnerLogs(w http.ResponseWriter, r *http.Request) {
	j := s.runnerJob(w, r)
	if j == nil {
		return
	}
	var req runnerapi.LogRequest
	if err := decodeJSON(r, &req); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	if req.First < 1 {
		apierr.Write(w, apierr.BadRequest("first must be at least 1"))
		return
	}
	resp, err := s.appendLogs(r.Context(), j, &req)
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	writeJSON(w, resp)
}

func (s *Server) applyStepUpdates(ctx context.Context, db bun.IDB, j *model.Job, steps []runnerapi.StepUpdate) error {
	for _, u := range steps {
		if u.Number < 0 {
			continue
		}
		switch u.Status {
		case runnerapi.StatusPending, runnerapi.StatusInProgress, runnerapi.StatusCompleted:
		default:
			return apierr.BadRequest("step %d: invalid status %q", u.Number, u.Status)
		}
		switch u.Conclusion {
		case "", runnerapi.ConclusionSuccess, runnerapi.ConclusionFailure, runnerapi.ConclusionCancelled, runnerapi.ConclusionSkipped:
		default:
			return apierr.BadRequest("step %d: invalid conclusion %q", u.Number, u.Conclusion)
		}
		q := db.NewUpdate().Model((*model.JobStep)(nil)).Set("status = ?, conclusion = ?", u.Status, u.Conclusion).
			Where("job_id = ? AND number = ?", j.ID, u.Number)
		if u.StartedAt != nil {
			q = q.Set("started_at = ?", u.StartedAt.UTC())
		}
		if u.CompletedAt != nil {
			q = q.Set("completed_at = ?", u.CompletedAt.UTC())
		}
		if _, err := q.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleRunnerUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j := s.runnerJob(w, r)
	if j == nil {
		return
	}
	var req runnerapi.UpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	if err := s.applyStepUpdates(ctx, s.db, j, req.Steps); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	if _, err := s.db.NewUpdate().Model(j).Set("heartbeat_at = ?", s.now()).WherePK().Exec(ctx); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	s.ci.logs.notify()
	writeJSON(w, runnerapi.UpdateResponse{Cancel: j.CancelRequested})
}

func (s *Server) handleRunnerComplete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j := s.runnerJob(w, r)
	if j == nil {
		return
	}
	var req runnerapi.CompleteRequest
	if err := decodeJSON(r, &req); err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	switch req.Conclusion {
	case runnerapi.ConclusionSuccess, runnerapi.ConclusionFailure, runnerapi.ConclusionCancelled, runnerapi.ConclusionTimedOut:
	default:
		apierr.Write(w, apierr.BadRequest("invalid conclusion %q", req.Conclusion))
		return
	}
	conclusion, reason := req.Conclusion, req.FailureReason
	if conclusion == runnerapi.ConclusionCancelled && j.StartedAt != nil &&
		s.now().Sub(*j.StartedAt) >= time.Duration(j.TimeoutMinutes)*time.Minute {
		conclusion = runnerapi.ConclusionTimedOut // stopped because the server saw the timeout
	}
	if conclusion == runnerapi.ConclusionCancelled && j.FailureReason != "" {
		reason = "" // keep the server's reason (who cancelled it, or why)
	}
	outputs := req.Outputs
	if outputs == nil {
		outputs = map[string]string{}
	}
	for k, v := range outputs {
		if len(k) > 100 || len(v) > 1<<20 {
			delete(outputs, k)
		}
	}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := s.applyStepUpdates(ctx, tx, j, req.Steps); err != nil {
			return err
		}
		return s.completeJob(ctx, tx, j, conclusion, reason, outputs)
	})
	if err != nil {
		apierr.Write(w, apierr.From(err))
		return
	}
	s.log.Info("ci: job completed", "job", j.ID, "conclusion", conclusion, "runner", runnerFrom(ctx).Name)
	s.ci.wake.notify()
	s.ci.logs.notify()
	w.WriteHeader(http.StatusNoContent)
}

// ---- admin ----

func (s *Server) toAPIRunner(r *model.Runner, jobs []apigen.RunnerJob, now time.Time, cs ciSettings) apigen.Runner {
	status := apigen.Offline
	switch {
	case r.Disabled:
		status = apigen.Disabled
	case runnerOnline(r, now, cs):
		status = apigen.Online
	}
	if jobs == nil {
		jobs = []apigen.RunnerJob{}
	}
	out := apigen.Runner{
		Id: r.ID, Name: r.Name, Labels: jsonStrings(r.Labels), Status: status, Version: r.Version, Os: r.OS, Arch: r.Arch,
		Capacity: r.Capacity, Busy: len(jobs), Jobs: jobs, LastSeenAt: r.LastSeenAt, CreatedAt: r.CreatedAt,
	}
	if r.CPUPercent != nil {
		v := float32(*r.CPUPercent)
		out.CpuPercent = &v
		over := scheduler.RunnerBlock(cs.limits(), scheduler.Runner{CPUPercent: *r.CPUPercent}, scheduler.State{}) != ""
		out.Overloaded = &over
	}
	if r.MemoryPercent != nil {
		v := float32(*r.MemoryPercent)
		out.MemoryPercent = &v
	}
	return out
}

// runnerJobs lists running jobs per runner.
func (s *Server) runnerJobs(ctx context.Context) (map[int64][]apigen.RunnerJob, error) {
	var rows []struct {
		ID        int64  `bun:"id"`
		RunnerID  int64  `bun:"runner_id"`
		Name      string `bun:"name"`
		RepoName  string `bun:"repo_name"`
		RunNumber int64  `bun:"run_number"`
	}
	err := s.db.NewSelect().TableExpr("jobs AS j").
		ColumnExpr("j.id, j.runner_id, j.name, r.name AS repo_name, wr.number AS run_number").
		Join("JOIN repos AS r ON r.id = j.repo_id").Join("JOIN workflow_runs AS wr ON wr.id = j.run_id").
		Where("j.status = ? AND j.runner_id IS NOT NULL", model.JobInProgress).Order("j.id").Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := map[int64][]apigen.RunnerJob{}
	for _, r := range rows {
		out[r.RunnerID] = append(out[r.RunnerID], apigen.RunnerJob{Id: r.ID, Repo: r.RepoName, RunNumber: r.RunNumber, Name: r.Name})
	}
	return out, nil
}

func (s *Server) loadRunner(ctx context.Context, id int64) (*model.Runner, error) {
	rn := new(model.Runner)
	err := s.db.NewSelect().Model(rn).Where("id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.NotFound("runner %d not found", id)
	}
	return rn, err
}

func (a *api) ListRunners(ctx context.Context, _ apigen.ListRunnersRequestObject) (apigen.ListRunnersResponseObject, error) {
	s := a.s
	cs, err := s.ciSettings(ctx)
	if err != nil {
		return nil, err
	}
	var rows []*model.Runner
	if err := s.db.NewSelect().Model(&rows).Order("rn.name", "rn.id").Scan(ctx); err != nil {
		return nil, err
	}
	jobs, err := s.runnerJobs(ctx)
	if err != nil {
		return nil, err
	}
	queued, err := s.db.NewSelect().Model((*model.Job)(nil)).Where("status = ?", model.JobQueued).Count(ctx)
	if err != nil {
		return nil, err
	}
	running, err := s.db.NewSelect().Model((*model.Job)(nil)).Where("status = ?", model.JobInProgress).Count(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := apigen.RunnerList{Runners: make([]apigen.Runner, len(rows)), QueuedJobs: int(queued), RunningJobs: int(running)}
	for i, r := range rows {
		out.Runners[i] = s.toAPIRunner(r, jobs[r.ID], now, cs)
	}
	return apigen.ListRunners200JSONResponse(out), nil
}

func (a *api) GetRunner(ctx context.Context, req apigen.GetRunnerRequestObject) (apigen.GetRunnerResponseObject, error) {
	s := a.s
	rn, err := s.loadRunner(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	cs, err := s.ciSettings(ctx)
	if err != nil {
		return nil, err
	}
	jobs, err := s.runnerJobs(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.GetRunner200JSONResponse(s.toAPIRunner(rn, jobs[rn.ID], s.now(), cs)), nil
}

func (a *api) UpdateRunner(ctx context.Context, req apigen.UpdateRunnerRequestObject) (apigen.UpdateRunnerResponseObject, error) {
	s := a.s
	rn, err := s.loadRunner(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	changes := map[string]any{}
	var cols []string
	if v := req.Body.Disabled; v != nil && *v != rn.Disabled {
		rn.Disabled = *v
		changes["disabled"] = *v
		cols = append(cols, "disabled")
	}
	if v := req.Body.Name; v != nil {
		name := strings.TrimSpace(*v)
		if name == "" || len(name) > 100 {
			return nil, apierr.Validation("runner name must be 1-100 characters").WithDetails(map[string]any{"field": "name"})
		}
		if name != rn.Name {
			changes["name"] = map[string]string{"from": rn.Name, "to": name}
			rn.Name = name
			cols = append(cols, "name")
		}
	}
	if len(cols) > 0 {
		if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			if _, err := tx.NewUpdate().Model(rn).Column(cols...).WherePK().Exec(ctx); err != nil {
				return err
			}
			return s.record(ctx, tx, audit.RunnerUpdate, "runner", rn.Name, changes)
		}); err != nil {
			return nil, err
		}
		s.ci.wake.notify()
	}
	return a.getRunnerResponse(ctx, rn)
}

func (a *api) getRunnerResponse(ctx context.Context, rn *model.Runner) (apigen.UpdateRunnerResponseObject, error) {
	cs, err := a.s.ciSettings(ctx)
	if err != nil {
		return nil, err
	}
	jobs, err := a.s.runnerJobs(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.UpdateRunner200JSONResponse(a.s.toAPIRunner(rn, jobs[rn.ID], a.s.now(), cs)), nil
}

func (a *api) DeleteRunner(ctx context.Context, req apigen.DeleteRunnerRequestObject) (apigen.DeleteRunnerResponseObject, error) {
	s := a.s
	rn, err := s.loadRunner(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var jobs []*model.Job
		if err := tx.NewSelect().Model(&jobs).Where("runner_id = ? AND status = ?", rn.ID, model.JobInProgress).Scan(ctx); err != nil {
			return err
		}
		for _, j := range jobs {
			if err := s.completeJob(ctx, tx, j, model.ConclusionFailure, "The runner was removed by an admin", nil); err != nil {
				return err
			}
		}
		if _, err := tx.NewDelete().Model(rn).WherePK().Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, audit.RunnerDelete, "runner", rn.Name, map[string]any{"id": rn.ID})
	})
	if err != nil {
		return nil, err
	}
	s.ci.wake.notify()
	s.ci.logs.notify()
	return apigen.DeleteRunner204Response{}, nil
}

func (a *api) CreateRunnerToken(ctx context.Context, req apigen.CreateRunnerTokenRequestObject) (apigen.CreateRunnerTokenResponseObject, error) {
	s := a.s
	hours := 24
	if req.Body != nil && req.Body.ExpiresInHours != nil {
		hours = *req.Body.ExpiresInHours
	}
	if hours < 1 || hours > 720 {
		return nil, apierr.Validation("expires_in_hours must be between 1 and 720")
	}
	token := runnerapi.RegistrationTokenPrefix + auth.RandomToken(24)
	now := s.now()
	user := mustPrincipal(ctx).User
	rt := &model.RunnerToken{TokenHash: auth.HashToken(token), CreatedByID: &user.ID, CreatedAt: now, ExpiresAt: now.Add(time.Duration(hours) * time.Hour)}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(rt).Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, audit.RunnerTokenCreate, "runner_token", strconv.FormatInt(rt.ID, 10), map[string]any{"expires_in_hours": hours})
	})
	if err != nil {
		return nil, err
	}
	base := s.publicBaseURL(ctx)
	if base == "" {
		base = "https://<your tgp server>"
	}
	cmd := fmt.Sprintf("tgp-runner register --url %s --token %s --name \"$(hostname)\"", base, token)
	return apigen.CreateRunnerToken201JSONResponse{Token: token, ExpiresAt: rt.ExpiresAt, Command: cmd}, nil
}
