package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/scheduler"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// runnerOnline reports whether a runner has been heard from recently.
func runnerOnline(r *model.Runner, now time.Time, cs ciSettings) bool {
	return r.LastSeenAt != nil && now.Sub(*r.LastSeenAt) < cs.offlineAfter()
}

func toSchedRunner(r *model.Runner, busy int, now time.Time, cs ciSettings) scheduler.Runner {
	sr := scheduler.Runner{ID: r.ID, Labels: jsonStrings(r.Labels), Capacity: r.Capacity, Busy: busy, Disabled: r.Disabled, Online: runnerOnline(r, now, cs)}
	if r.CPUPercent != nil {
		sr.CPUPercent = *r.CPUPercent
	}
	return sr
}

// schedState snapshots the running jobs.
func (s *Server) schedState(ctx context.Context, db bun.IDB) (scheduler.State, map[int64]int, error) {
	var running []struct {
		RepoID   int64      `bun:"repo_id"`
		RunID    int64      `bun:"run_id"`
		Key      string     `bun:"key"`
		RunnerID *int64     `bun:"runner_id"`
		Started  *time.Time `bun:"started_at"`
	}
	if err := db.NewSelect().Model((*model.Job)(nil)).Column("repo_id", "run_id", "key", "runner_id", "started_at").
		Where("status = ?", model.JobInProgress).Scan(ctx, &running); err != nil {
		return scheduler.State{}, nil, err
	}
	st := scheduler.State{RepoRunning: map[int64]int{}, RepoLastRun: map[int64]time.Time{}, GroupRunning: map[string]int{}}
	perRunner := map[int64]int{}
	for _, r := range running {
		st.Running++
		st.RepoRunning[r.RepoID]++
		st.GroupRunning[groupKey(r.RunID, r.Key)]++
		if r.RunnerID != nil {
			perRunner[*r.RunnerID]++
		}
	}
	var last []struct {
		RepoID int64     `bun:"repo_id"`
		At     time.Time `bun:"at"`
	}
	if err := db.NewSelect().Model((*model.Job)(nil)).ColumnExpr("repo_id, MAX(started_at) AS at").
		Where("started_at > ?", s.now().Add(-24*time.Hour)).Group("repo_id").Scan(ctx, &last); err != nil {
		return st, perRunner, err
	}
	for _, l := range last {
		st.RepoLastRun[l.RepoID] = l.At
	}
	return st, perRunner, nil
}

func groupKey(runID int64, key string) string { return strconv.FormatInt(runID, 10) + "/" + key }

// queuedJobs lists queued jobs for the scheduler, oldest first.
func queuedJobs(ctx context.Context, db bun.IDB) ([]scheduler.Job, error) {
	var rows []model.Job
	if err := db.NewSelect().Model(&rows).Column("id", "repo_id", "run_id", "key", "labels", "queued_at", "max_parallel").
		Where("status = ?", model.JobQueued).Order("queued_at", "id").Limit(2000).Scan(ctx); err != nil {
		return nil, err
	}
	out := make([]scheduler.Job, len(rows))
	for i, r := range rows {
		out[i] = scheduler.Job{ID: r.ID, RepoID: r.RepoID, Labels: jsonStrings(r.Labels), Group: groupKey(r.RunID, r.Key), GroupMax: r.MaxParallel}
		if r.QueuedAt != nil {
			out[i].QueuedAt = *r.QueuedAt
		}
	}
	return out, nil
}

// assignJob picks a job for the runner and marks it started, atomically.
func (s *Server) assignJob(ctx context.Context, rn *model.Runner) (*runnerapi.JobSpec, error) {
	cs, err := s.ciSettings(ctx)
	if err != nil {
		return nil, err
	}
	s.ci.assignMu.Lock()
	defer s.ci.assignMu.Unlock()
	var spec *runnerapi.JobSpec
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if s.db.IsPostgres() {
			// Serialise assignment across server nodes so the limits hold.
			if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(7420001)"); err != nil {
				return err
			}
		}
		st, perRunner, err := s.schedState(ctx, tx)
		if err != nil {
			return err
		}
		queue, err := queuedJobs(ctx, tx)
		if err != nil || len(queue) == 0 {
			return err
		}
		now := s.now()
		pick, ok := scheduler.Pick(cs.limits(), toSchedRunner(rn, perRunner[rn.ID], now, cs), st, queue)
		if !ok {
			return nil
		}
		token := runnerapi.JobTokenPrefix + auth.RandomToken(24)
		hash := auth.HashToken(token)
		res, err := tx.NewUpdate().Model((*model.Job)(nil)).
			Set("status = ?, runner_id = ?, started_at = ?, heartbeat_at = ?, token_hash = ?", model.JobInProgress, rn.ID, now, now, hash).
			Where("id = ? AND status = ?", pick.ID, model.JobQueued).Exec(ctx)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil // taken concurrently; the runner asks again
		}
		j := new(model.Job)
		if err := tx.NewSelect().Model(j).Where("id = ?", pick.ID).Scan(ctx); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*model.WorkflowRun)(nil)).
			Set("status = ?, started_at = COALESCE(started_at, ?), updated_at = ?", model.RunInProgress, now, now).
			Where("id = ? AND status = ?", j.RunID, model.RunQueued).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*model.JobStep)(nil)).Set("status = ?, started_at = ?", model.JobInProgress, now).
			Where("job_id = ? AND number = 0", j.ID).Exec(ctx); err != nil {
			return err
		}
		spec, err = s.jobSpec(ctx, tx, j, token, cs)
		return err
	})
	if spec != nil {
		s.ci.logs.notify()
	}
	return spec, err
}

// jobSpec builds what the runner needs to run j.
func (s *Server) jobSpec(ctx context.Context, tx bun.IDB, j *model.Job, token string, cs ciSettings) (*runnerapi.JobSpec, error) {
	run, err := loadRunByID(ctx, tx, j.RunID)
	if err != nil {
		return nil, err
	}
	repo := new(model.Repo)
	if err := tx.NewSelect().Model(repo).Where("id = ?", j.RepoID).Scan(ctx); err != nil {
		return nil, err
	}
	d := parseDef(j)
	jobs, err := runJobs(ctx, tx, run)
	if err != nil {
		return nil, err
	}
	defs := map[int64]jobDef{}
	byKey := map[string][]*model.Job{}
	for _, o := range jobs {
		defs[o.ID] = parseDef(o)
		byKey[o.Key] = append(byKey[o.Key], o)
	}
	needs := map[string]any{}
	for _, n := range jsonStrings(j.Needs) {
		result, outputs := needResult(byKey[n], defs)
		needs[n] = map[string]any{"result": result, "outputs": outputs}
	}
	gh := jsonMap(run.GitHubContext)
	if gh == nil {
		gh = map[string]any{}
	}
	gh["job"] = j.Key
	matrix := jsonMap(j.Matrix)
	if matrix == nil {
		matrix = map[string]any{}
	}
	inputs := map[string]any{}
	for k, v := range jsonStringMap(run.Inputs) {
		inputs[k] = v
	}
	secrets, err := s.jobSecrets(ctx, tx, repo.ID, d.Secrets)
	if err != nil {
		return nil, err
	}
	env := map[string]string{"CI": "true", "TGP_ACTIONS": "true", "GITHUB_ACTIONS": "true"}
	str := func(k string) string {
		switch v := gh[k].(type) {
		case string:
			return v
		case float64:
			return strconv.FormatInt(int64(v), 10)
		}
		return ""
	}
	for name, key := range map[string]string{
		"SHA": "sha", "REF": "ref", "REF_NAME": "ref_name", "REF_TYPE": "ref_type", "REPOSITORY": "repository",
		"REPOSITORY_OWNER": "repository_owner", "RUN_ID": "run_id", "RUN_NUMBER": "run_number", "RUN_ATTEMPT": "run_attempt",
		"ACTOR": "actor", "TRIGGERING_ACTOR": "triggering_actor", "EVENT_NAME": "event_name", "HEAD_REF": "head_ref",
		"BASE_REF": "base_ref", "SERVER_URL": "server_url", "API_URL": "api_url", "WORKFLOW": "workflow", "JOB": "job",
	} {
		env["GITHUB_"+name] = str(key)
		env["TGP_"+name] = str(key)
	}
	spec := &runnerapi.JobSpec{
		ID: j.ID, RunID: run.ID, RunNumber: run.Number, Attempt: run.Attempt, Repo: repo.Name, Workflow: run.WorkflowName,
		JobKey: j.Key, Name: j.Name, Image: j.Image, ContainerEnv: d.ContainerEnv, Services: d.Services, Steps: d.Steps,
		WorkflowEnv: d.WorkflowEnv, JobEnv: d.JobEnv, Defaults: d.Defaults, Outputs: d.Outputs,
		TimeoutSeconds: j.TimeoutMinutes * 60,
		Resources:      runnerapi.Resources{CPUs: j.CPUs, MemoryBytes: j.MemoryBytes, Pids: j.Pids},
		Contexts: map[string]any{
			"github": gh, "tgp": gh, "matrix": matrix, "needs": needs, "inputs": inputs,
			"strategy": d.Strategy, "vars": map[string]any{},
		},
		Secrets:     secrets,
		Env:         env,
		Git:         runnerapi.GitSource{URL: "/" + repo.Name + ".git", Username: runnerapi.JobTokenUser, Token: token, Ref: run.Ref, SHA: run.HeadSHA},
		MaxLogBytes: int64(cs.MaxLogMB) << 20,
	}
	return spec, nil
}

// waitReason explains why a queued job hasn't started.
func (s *Server) waitReasons(ctx context.Context, jobs []*model.Job) (map[int64]string, error) {
	out := map[int64]string{}
	var queued []*model.Job
	for _, j := range jobs {
		if j.Status == model.JobQueued {
			queued = append(queued, j)
		}
	}
	if len(queued) == 0 {
		return out, nil
	}
	cs, err := s.ciSettings(ctx)
	if err != nil {
		return nil, err
	}
	st, perRunner, err := s.schedState(ctx, s.db)
	if err != nil {
		return nil, err
	}
	var rows []*model.Runner
	if err := s.db.NewSelect().Model(&rows).Scan(ctx); err != nil {
		return nil, err
	}
	now := s.now()
	runners := make([]scheduler.Runner, len(rows))
	for i, r := range rows {
		runners[i] = toSchedRunner(r, perRunner[r.ID], now, cs)
	}
	for _, j := range queued {
		sj := scheduler.Job{ID: j.ID, RepoID: j.RepoID, Labels: jsonStrings(j.Labels), Group: groupKey(j.RunID, j.Key), GroupMax: j.MaxParallel}
		out[j.ID] = scheduler.WaitReason(cs.limits(), runners, st, sj)
	}
	return out, nil
}

// ---- janitor ----

// queueTimeout fails jobs that no runner picked up in time.
const queueTimeout = 24 * time.Hour

// cancelGrace is how long a runner has to stop a cancelled job before the
// server marks it cancelled anyway.
const cancelGrace = 2 * time.Minute

// ciJanitor enforces time limits the runners can't: lost runners, jobs
// past their timeout, runners that ignore cancellation, stale queues and
// log retention. It's idempotent and safe to run on every server node.
func (s *Server) ciJanitor(ctx context.Context) error {
	cs, err := s.ciSettings(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	var running []*model.Job
	if err := s.db.NewSelect().Model(&running).Where("status = ?", model.JobInProgress).Scan(ctx); err != nil {
		return err
	}
	changed := false
	for _, j := range running {
		var conclusion, reason string
		switch {
		case j.HeartbeatAt == nil || now.Sub(*j.HeartbeatAt) > cs.offlineAfter():
			conclusion, reason = model.ConclusionFailure, fmt.Sprintf("The runner stopped responding for over %d seconds (it crashed, lost its connection or was removed)", int(cs.offlineAfter().Seconds()))
		case j.StartedAt != nil && now.Sub(*j.StartedAt) > time.Duration(j.TimeoutMinutes)*time.Minute+cancelGrace:
			conclusion, reason = model.ConclusionTimedOut, fmt.Sprintf("The job exceeded its %d minute timeout", j.TimeoutMinutes)
		case j.StartedAt != nil && now.Sub(*j.StartedAt) > time.Duration(j.TimeoutMinutes)*time.Minute && !j.CancelRequested:
			// Ask the runner to stop; it reports timed_out itself.
			j.CancelRequested, j.CancelledAt = true, &now
			j.FailureReason = fmt.Sprintf("The job exceeded its %d minute timeout", j.TimeoutMinutes)
			if _, err := s.db.NewUpdate().Model(j).Column("cancel_requested", "cancelled_at", "failure_reason").WherePK().Exec(ctx); err != nil {
				return err
			}
			continue
		case j.CancelRequested && j.CancelledAt != nil && now.Sub(*j.CancelledAt) > cancelGrace:
			conclusion, reason = model.ConclusionCancelled, j.FailureReason
		default:
			continue
		}
		job := j
		if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			return s.completeJob(ctx, tx, job, conclusion, reason, nil)
		}); err != nil {
			return err
		}
		changed = true
	}
	// Jobs nobody picked up.
	var stale []*model.Job
	if err := s.db.NewSelect().Model(&stale).Where("status = ? AND queued_at < ?", model.JobQueued, now.Add(-queueTimeout)).Limit(500).Scan(ctx); err != nil {
		return err
	}
	for _, j := range stale {
		job := j
		if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			return s.completeJob(ctx, tx, job, model.ConclusionCancelled, "No runner picked up the job within 24 hours", nil)
		}); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		s.ci.wake.notify()
		s.ci.logs.notify()
	}
	if cs.LogRetentionDays > 0 {
		if err := s.expireLogs(ctx, now.Add(-time.Duration(cs.LogRetentionDays)*24*time.Hour)); err != nil {
			return err
		}
	}
	if _, err := s.db.NewDelete().Model((*model.RunnerToken)(nil)).Where("expires_at < ? AND used_at IS NULL", now.Add(-24*time.Hour)).Exec(ctx); err != nil {
		return err
	}
	// Storage retention: expired artifacts, unused caches, cache quotas.
	// Blob deletes may be slow (S3), so this runs at most once a minute.
	if last := s.ci.lastRetention.Load(); last != 0 && now.Sub(time.Unix(0, last)) < time.Minute {
		return nil
	}
	s.ci.lastRetention.Store(now.UnixNano())
	if err := s.expireArtifacts(ctx); err != nil {
		return fmt.Errorf("artifact retention: %w", err)
	}
	if err := s.expireCaches(ctx, cs); err != nil {
		return fmt.Errorf("cache retention: %w", err)
	}
	if err := s.expireDeliveries(ctx); err != nil {
		return fmt.Errorf("webhook delivery retention: %w", err)
	}
	return nil
}

// RunCIJanitor runs one janitor pass (tests).
func (s *Server) RunCIJanitor(ctx context.Context) error { return s.ciJanitor(ctx) }

// StartBackground runs the CI janitor until ctx ends.
func (s *Server) StartBackground(ctx context.Context) {
	go s.runHookWorker(ctx)
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := s.ciJanitor(ctx); err != nil && ctx.Err() == nil {
					s.log.Error("ci janitor", "err", err)
				}
			}
		}
	}()
}

// jobByToken finds the running job a job token belongs to.
func (s *Server) jobByToken(ctx context.Context, token string) (*model.Job, error) {
	if !strings.HasPrefix(token, runnerapi.JobTokenPrefix) {
		return nil, fmt.Errorf("not a job token")
	}
	j := new(model.Job)
	err := s.db.NewSelect().Model(j).Where("token_hash = ? AND status = ?", auth.HashToken(token), model.JobInProgress).Scan(ctx)
	return j, err
}
