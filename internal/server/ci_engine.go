package server

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/expr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// The run/job state machine. Every transition happens inside a database
// transaction and is written so it can run on any server node:
//
//	run:  waiting → queued → in_progress → completed
//	job:  pending → queued → in_progress → completed
//	                      ↘ completed (skipped, or cancelled before starting)
//
// A job is pending until every job it needs has completed, then its if:
// decides between queued and skipped. Queued jobs are handed to runners by
// the scheduler (ci_sched.go). A run completes when all its jobs have.

// runJobs loads the jobs of a run's current attempt, in creation order.
func runJobs(ctx context.Context, db bun.IDB, run *model.WorkflowRun) ([]*model.Job, error) {
	var jobs []*model.Job
	err := db.NewSelect().Model(&jobs).Where("run_id = ? AND attempt = ?", run.ID, run.Attempt).Order("id").Scan(ctx)
	return jobs, err
}

func loadRunByID(ctx context.Context, db bun.IDB, id int64) (*model.WorkflowRun, error) {
	run := new(model.WorkflowRun)
	return run, db.NewSelect().Model(run).Where("id = ?", id).Scan(ctx)
}

// advanceRun moves pending jobs whose needs are done to queued or skipped,
// and completes the run when every job has completed. It reports whether
// any job became queued.
func (s *Server) advanceRun(ctx context.Context, tx bun.IDB, run *model.WorkflowRun) (bool, error) {
	if run.Status == model.RunCompleted || run.Status == model.RunWaiting {
		return false, nil
	}
	jobs, err := runJobs(ctx, tx, run)
	if err != nil {
		return false, err
	}
	defs := map[int64]jobDef{}
	byKey := map[string][]*model.Job{}
	for _, j := range jobs {
		defs[j.ID] = parseDef(j)
		byKey[j.Key] = append(byKey[j.Key], j)
	}
	now := s.now()
	queued := false
	for changed := true; changed; {
		changed = false
		for _, j := range jobs {
			if j.Status != model.JobPending {
				continue
			}
			needs := jsonStrings(j.Needs)
			ready := true
			for _, n := range needs {
				for _, dep := range byKey[n] {
					if dep.Status != model.JobCompleted {
						ready = false
					}
				}
			}
			if !ready {
				continue
			}
			ok, reason, err := s.jobCondition(run, j, defs[j.ID], needs, byKey, defs)
			if err != nil {
				reason = err.Error()
			}
			if ok && err == nil {
				j.Status, j.QueuedAt = model.JobQueued, &now
				queued = true
				if _, err := tx.NewUpdate().Model(j).Column("status", "queued_at").WherePK().Exec(ctx); err != nil {
					return false, err
				}
			} else {
				j.Status, j.Conclusion, j.CompletedAt = model.JobCompleted, model.ConclusionSkipped, &now
				if err != nil {
					j.Conclusion, j.FailureReason = model.ConclusionFailure, reason
				}
				if _, err := tx.NewUpdate().Model(j).Column("status", "conclusion", "completed_at", "failure_reason").WherePK().Exec(ctx); err != nil {
					return false, err
				}
				if err := finishSteps(ctx, tx, j, now); err != nil {
					return false, err
				}
			}
			changed = true
		}
	}
	for _, j := range jobs {
		if j.Status != model.JobCompleted {
			return queued, nil
		}
	}
	return queued, s.finishRun(ctx, tx, run, jobs, defs)
}

// jobCondition evaluates a ready job's if: against its needs.
func (s *Server) jobCondition(run *model.WorkflowRun, j *model.Job, d jobDef, needs []string, byKey map[string][]*model.Job, defs map[int64]jobDef) (bool, string, error) {
	if run.CancelRequested {
		return false, "", nil
	}
	needsCtx := map[string]any{}
	allOK, anyFailed := true, false
	for _, n := range needs {
		result, outputs := needResult(byKey[n], defs)
		needsCtx[n] = map[string]any{"result": result, "outputs": outputs}
		if result != model.ConclusionSuccess {
			allOK = false
		}
		if result == model.ConclusionFailure {
			anyFailed = true
		}
	}
	c := &expr.Context{
		Contexts: map[string]any{
			"github": jsonMap(run.GitHubContext),
			"tgp":    jsonMap(run.GitHubContext),
			"inputs": jsonMap(run.Inputs),
			"vars":   map[string]any{},
			"needs":  needsCtx,
		},
		Status: func(fn string) bool {
			switch fn {
			case "success":
				return allOK && !run.CancelRequested
			case "failure":
				return anyFailed
			case "cancelled":
				return run.CancelRequested
			}
			return true // always
		},
	}
	ok, err := expr.Condition(d.If, c)
	if err != nil {
		return false, fmt.Sprintf("Invalid if: condition: %v", err), err
	}
	return ok, "", nil
}

// finishSteps closes the steps of a job that ended: running steps take the
// job's conclusion, steps that never started are skipped.
func finishSteps(ctx context.Context, tx bun.IDB, j *model.Job, now time.Time) error {
	running := j.Conclusion
	if running == model.ConclusionTimedOut || running == model.ConclusionSkipped {
		running = model.ConclusionFailure
		if j.Conclusion == model.ConclusionSkipped {
			running = model.ConclusionSkipped
		}
	}
	if _, err := tx.NewUpdate().Model((*model.JobStep)(nil)).
		Set("status = ?, conclusion = ?, completed_at = ?", model.JobCompleted, running, now).
		Where("job_id = ? AND status = ?", j.ID, model.JobInProgress).Exec(ctx); err != nil {
		return err
	}
	pending := model.ConclusionSkipped
	if j.Conclusion == model.ConclusionCancelled {
		pending = model.ConclusionCancelled
	}
	_, err := tx.NewUpdate().Model((*model.JobStep)(nil)).
		Set("status = ?, conclusion = ?", model.JobCompleted, pending).
		Where("job_id = ? AND status = ?", j.ID, model.JobPending).Exec(ctx)
	return err
}

// runConclusion derives a run's conclusion from its jobs.
func runConclusion(run *model.WorkflowRun, jobs []*model.Job, defs map[int64]jobDef) string {
	failed, timedOut, cancelled, skipped := false, false, false, 0
	for _, j := range jobs {
		if conclusionOK(j, defs[j.ID]) {
			continue
		}
		switch j.Conclusion {
		case model.ConclusionFailure:
			failed = true
		case model.ConclusionTimedOut:
			timedOut = true
		case model.ConclusionCancelled:
			cancelled = true
		case model.ConclusionSkipped:
			skipped++
		}
	}
	switch {
	case run.CancelRequested && (cancelled || len(jobs) == 0):
		return model.ConclusionCancelled
	case failed:
		return model.ConclusionFailure
	case timedOut:
		return model.ConclusionTimedOut
	case cancelled:
		return model.ConclusionCancelled
	case len(jobs) > 0 && skipped == len(jobs):
		return model.ConclusionSkipped
	}
	return model.ConclusionSuccess
}

// finishRun completes a run and lets the next run of its concurrency group
// start.
func (s *Server) finishRun(ctx context.Context, tx bun.IDB, run *model.WorkflowRun, jobs []*model.Job, defs map[int64]jobDef) error {
	now := s.now()
	run.Status, run.Conclusion, run.CompletedAt, run.UpdatedAt = model.RunCompleted, runConclusion(run, jobs, defs), &now, now
	if _, err := tx.NewUpdate().Model(run).Column("status", "conclusion", "completed_at", "updated_at").WherePK().Exec(ctx); err != nil {
		return err
	}
	s.hooksRun(ctx, tx, run, "completed")
	return s.releaseConcurrency(ctx, tx, run)
}

// releaseConcurrency starts the oldest waiting run in run's group once no
// run of the group is active.
func (s *Server) releaseConcurrency(ctx context.Context, tx bun.IDB, run *model.WorkflowRun) error {
	if run.ConcurrencyGroup == "" {
		return nil
	}
	active, err := tx.NewSelect().Model((*model.WorkflowRun)(nil)).
		Where("repo_id = ? AND concurrency_group = ? AND status IN (?)", run.RepoID, run.ConcurrencyGroup, bun.In([]string{model.RunQueued, model.RunInProgress})).Exists(ctx)
	if err != nil || active {
		return err
	}
	next := new(model.WorkflowRun)
	err = tx.NewSelect().Model(next).Where("repo_id = ? AND concurrency_group = ? AND status = ?", run.RepoID, run.ConcurrencyGroup, model.RunWaiting).
		Order("id").Limit(1).Scan(ctx)
	if err != nil {
		return ignoreNoRows(err)
	}
	next.Status, next.UpdatedAt = model.RunQueued, s.now()
	if _, err := tx.NewUpdate().Model(next).Column("status", "updated_at").WherePK().Exec(ctx); err != nil {
		return err
	}
	_, err = s.advanceRun(ctx, tx, next)
	return err
}

// completeJob records a job's end and advances its run.
func (s *Server) completeJob(ctx context.Context, tx bun.IDB, j *model.Job, conclusion, reason string, outputs map[string]string) error {
	if j.Status == model.JobCompleted {
		return nil
	}
	now := s.now()
	j.Status, j.Conclusion, j.CompletedAt, j.TokenHash = model.JobCompleted, conclusion, &now, nil
	if reason != "" {
		j.FailureReason = reason
	}
	if outputs != nil {
		j.Outputs = mustJSON(outputs)
	}
	res, err := tx.NewUpdate().Model(j).Column("status", "conclusion", "completed_at", "token_hash", "failure_reason", "outputs").
		WherePK().Where("status <> ?", model.JobCompleted).Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // completed concurrently
	}
	if err := finishSteps(ctx, tx, j, now); err != nil {
		return err
	}
	d := parseDef(j)
	if (conclusion == model.ConclusionFailure || conclusion == model.ConclusionTimedOut) && !d.ContinueOnError && d.FailFast && j.Matrix != "" {
		if err := s.cancelJobs(ctx, tx, tx.NewSelect().Model((*model.Job)(nil)).
			Where("run_id = ? AND attempt = ? AND key = ? AND id <> ? AND status <> ?", j.RunID, j.Attempt, j.Key, j.ID, model.JobCompleted),
			"Cancelled because "+j.Name+" failed (fail-fast)"); err != nil {
			return err
		}
	}
	run, err := loadRunByID(ctx, tx, j.RunID)
	if err != nil {
		return err
	}
	if run.Attempt != j.Attempt {
		return nil
	}
	_, err = s.advanceRun(ctx, tx, run)
	return err
}

// cancelJobs cancels the jobs selected by q: unstarted jobs immediately,
// running jobs by asking their runner to stop.
func (s *Server) cancelJobs(ctx context.Context, tx bun.IDB, q *bun.SelectQuery, reason string) error {
	var jobs []*model.Job
	if err := q.Scan(ctx, &jobs); err != nil {
		return err
	}
	now := s.now()
	for _, j := range jobs {
		switch j.Status {
		case model.JobPending, model.JobQueued:
			was := j.Status
			j.Status, j.Conclusion, j.CompletedAt, j.FailureReason = model.JobCompleted, model.ConclusionCancelled, &now, reason
			if _, err := tx.NewUpdate().Model(j).Column("status", "conclusion", "completed_at", "failure_reason").
				WherePK().Where("status = ?", was).Exec(ctx); err != nil {
				return err
			}
			if err := finishSteps(ctx, tx, j, now); err != nil {
				return err
			}
		case model.JobInProgress:
			if j.CancelRequested {
				continue
			}
			j.CancelRequested, j.CancelledAt = true, &now
			if j.FailureReason == "" {
				j.FailureReason = reason
			}
			if _, err := tx.NewUpdate().Model(j).Column("cancel_requested", "cancelled_at", "failure_reason").WherePK().Exec(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// cancelRun cancels every unfinished job of a run.
func (s *Server) cancelRun(ctx context.Context, tx bun.IDB, run *model.WorkflowRun, reason string) error {
	if run.Status == model.RunCompleted {
		return nil
	}
	run.CancelRequested, run.UpdatedAt = true, s.now()
	if reason != "" {
		run.StatusReason = reason
	}
	if _, err := tx.NewUpdate().Model(run).Column("cancel_requested", "status_reason", "updated_at").WherePK().Exec(ctx); err != nil {
		return err
	}
	jobReason := reason
	if jobReason == "" {
		jobReason = "The run was cancelled"
	}
	if err := s.cancelJobs(ctx, tx, tx.NewSelect().Model((*model.Job)(nil)).
		Where("run_id = ? AND attempt = ? AND status <> ?", run.ID, run.Attempt, model.JobCompleted), jobReason); err != nil {
		return err
	}
	if run.Status == model.RunWaiting {
		// It never started; complete it directly (its jobs are cancelled).
		run.Status = model.RunQueued
	}
	_, err := s.advanceRun(ctx, tx, run)
	return err
}

// rerunRun starts a new attempt. With failedOnly, jobs that passed keep
// their results (copied into the new attempt); with onlyJob, that job and
// everything depending on it run again.
func (s *Server) rerunRun(ctx context.Context, tx bun.IDB, run *model.WorkflowRun, failedOnly bool, onlyJob *model.Job) error {
	old, err := runJobs(ctx, tx, run)
	if err != nil {
		return err
	}
	if len(old) == 0 {
		return fmt.Errorf("run #%d has no jobs to re-run", run.Number)
	}
	defs := map[int64]jobDef{}
	for _, j := range old {
		defs[j.ID] = parseDef(j)
	}
	// Decide which job instances run again.
	again := map[int64]bool{}
	switch {
	case onlyJob != nil:
		again[onlyJob.ID] = true
	case failedOnly:
		for _, j := range old {
			if !conclusionOK(j, defs[j.ID]) && j.Conclusion != model.ConclusionSkipped {
				again[j.ID] = true
			}
		}
	default:
		for _, j := range old {
			again[j.ID] = true
		}
	}
	// Dependents of re-run jobs (transitively) also run again, as do
	// skipped jobs whose skip may have been caused by a failure.
	keysAgain := map[string]bool{}
	for _, j := range old {
		if again[j.ID] {
			keysAgain[j.Key] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, j := range old {
			if again[j.ID] {
				continue
			}
			for _, n := range jsonStrings(j.Needs) {
				if keysAgain[n] {
					again[j.ID], keysAgain[j.Key], changed = true, true, true
					break
				}
			}
		}
	}
	if len(again) == 0 {
		return fmt.Errorf("every job of run #%d passed; nothing to re-run", run.Number)
	}
	now := s.now()
	run.Attempt++
	run.Status, run.Conclusion, run.StatusReason, run.CancelRequested = model.RunQueued, "", "", false
	run.StartedAt, run.CompletedAt, run.UpdatedAt = nil, nil, now
	gh := jsonMap(run.GitHubContext)
	if gh != nil {
		gh["run_attempt"] = float64(run.Attempt)
		run.GitHubContext = mustJSON(gh)
	}
	if _, err := tx.NewUpdate().Model(run).Column("attempt", "status", "conclusion", "status_reason", "cancel_requested", "started_at", "completed_at", "updated_at", "github_context").
		WherePK().Exec(ctx); err != nil {
		return err
	}
	s.hooksRun(ctx, tx, run, "requested")
	for _, o := range old {
		j := *o
		j.ID, j.Attempt, j.CreatedAt, j.RunnerID, j.TokenHash, j.CancelRequested, j.HeartbeatAt, j.CancelledAt = 0, run.Attempt, now, nil, nil, false, nil, nil
		if again[o.ID] {
			j.Status, j.Conclusion, j.FailureReason, j.Outputs = model.JobPending, "", "", "{}"
			j.QueuedAt, j.StartedAt, j.CompletedAt, j.ReusedFromID = nil, nil, nil, nil
			j.LogLines, j.LogBytes, j.LogTruncated, j.LogsDeleted = 0, 0, false, false
		} else {
			src := o.ID
			if o.ReusedFromID != nil {
				src = *o.ReusedFromID
			}
			j.ReusedFromID = &src
		}
		if _, err := tx.NewInsert().Model(&j).Exec(ctx); err != nil {
			return err
		}
		if err := copySteps(ctx, tx, o, &j, again[o.ID]); err != nil {
			return err
		}
	}
	if run.ConcurrencyGroup != "" {
		busy, err := tx.NewSelect().Model((*model.WorkflowRun)(nil)).
			Where("repo_id = ? AND concurrency_group = ? AND id <> ? AND status IN (?)", run.RepoID, run.ConcurrencyGroup, run.ID,
				bun.In([]string{model.RunQueued, model.RunInProgress})).Exists(ctx)
		if err != nil {
			return err
		}
		if busy {
			run.Status = model.RunWaiting
			if _, err := tx.NewUpdate().Model(run).Column("status").WherePK().Exec(ctx); err != nil {
				return err
			}
		}
	}
	_, err = s.advanceRun(ctx, tx, run)
	return err
}

// copySteps gives a new job row its steps: fresh pending steps for a job
// that runs again, a copy of the old results for a reused job.
func copySteps(ctx context.Context, tx bun.IDB, from, to *model.Job, fresh bool) error {
	var steps []model.JobStep
	if err := tx.NewSelect().Model(&steps).Where("job_id = ?", from.ID).Order("number").Scan(ctx); err != nil {
		return err
	}
	for i := range steps {
		st := steps[i]
		st.ID, st.JobID = 0, to.ID
		if fresh {
			st.Status, st.Conclusion, st.StartedAt, st.CompletedAt = model.JobPending, "", nil, nil
		}
		if _, err := tx.NewInsert().Model(&st).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// insertSteps creates a new job's step rows: step 0 (set up) and one per
// workflow step.
func insertSteps(ctx context.Context, tx bun.IDB, j *model.Job, d jobDef) error {
	steps := []model.JobStep{{JobID: j.ID, Number: 0, Name: "Set up job", Status: model.JobPending}}
	for _, st := range d.Steps {
		steps = append(steps, model.JobStep{JobID: j.ID, Number: st.Number, Name: st.DisplayName(), Status: model.JobPending})
	}
	_, err := tx.NewInsert().Model(&steps).Exec(ctx)
	return err
}

// sortedKeys returns m's keys sorted.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
