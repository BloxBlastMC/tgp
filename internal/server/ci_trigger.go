package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/expr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/workflow"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/git"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/githook"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

func ignoreNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

// ciEvent is something that may start workflow runs.
type ciEvent struct {
	Name   string // push, pull_request, workflow_dispatch
	Ref    string // full ref the run is for
	SHA    string
	Before string // push: previous tip ("" or zeros for a new ref)
	Actor  *model.User

	// Pull requests.
	PullNumber int64
	PullTitle  string
	HeadBranch string
	BaseBranch string
	BaseSHA    string
	Action     string

	// workflow_dispatch.
	Inputs   map[string]string
	OnlyPath string
}

func isZeroSHA(s string) bool { return s == "" || strings.Trim(s, "0") == "" }

var skipCIRe = regexp.MustCompile(`(?i)\[(skip ci|ci skip|no ci|skip actions|actions skip)\]`)

// wfFile is a workflow file read from a commit.
type wfFile struct {
	Path string
	Data []byte
}

// workflowFiles reads .tgp/workflows/*.yml|yaml at commit.
func (s *Server) workflowFiles(ctx context.Context, g *git.Repo, commit string) ([]wfFile, error) {
	if t, err := g.ObjectType(ctx, commit, workflow.Dir); err != nil || t != "tree" {
		return nil, nil // no workflows directory
	}
	entries, err := g.Tree(ctx, commit, workflow.Dir)
	if err != nil {
		return nil, err
	}
	var out []wfFile
	for _, e := range entries {
		if e.Type != "file" || !(strings.HasSuffix(e.Name, ".yml") || strings.HasSuffix(e.Name, ".yaml")) {
			continue
		}
		b, err := g.ReadBlob(ctx, commit, e.Path, workflow.MaxFileBytes+1)
		if err != nil {
			return nil, err
		}
		out = append(out, wfFile{Path: e.Path, Data: b.Content})
	}
	return out, nil
}

// ciOnPush starts workflows for pushed refs. Errors are logged: CI must
// never make a push fail.
func (s *Server) ciOnPush(ctx context.Context, repo *model.Repo, actor *model.User, updates []githook.Update) {
	for _, u := range updates {
		if u.IsDelete() || !(strings.HasPrefix(u.Ref, "refs/heads/") || strings.HasPrefix(u.Ref, "refs/tags/")) {
			continue
		}
		ev := &ciEvent{Name: "push", Ref: u.Ref, SHA: u.New, Before: u.Old, Actor: actor}
		if _, err := s.triggerRuns(ctx, repo, ev); err != nil {
			s.log.Error("ci: push trigger failed", "repo", repo.Name, "ref", u.Ref, "err", err)
		}
	}
}

// ciOnPull starts pull_request workflows (action: opened, synchronize,
// reopened, ready_for_review).
func (s *Server) ciOnPull(ctx context.Context, repo *model.Repo, actor *model.User, pr *model.PullRequest, number int64, title, action string) {
	ev := &ciEvent{Name: "pull_request", Ref: pullRef(number), SHA: pr.HeadSHA, Actor: actor,
		PullNumber: number, PullTitle: title, HeadBranch: pr.HeadBranch, BaseBranch: pr.BaseBranch, Action: action}
	if sha, err := s.repos.Open(repo).ResolveCommit(ctx, pr.BaseBranch); err == nil {
		ev.BaseSHA = sha
	}
	if _, err := s.triggerRuns(ctx, repo, ev); err != nil {
		s.log.Error("ci: pull request trigger failed", "repo", repo.Name, "pull", number, "err", err)
	}
}

// triggerRuns creates a run for every workflow the event matches.
func (s *Server) triggerRuns(ctx context.Context, repo *model.Repo, ev *ciEvent) ([]*model.WorkflowRun, error) {
	g := s.repos.Open(repo)
	commit, err := g.GetCommit(ctx, ev.SHA)
	if err != nil {
		return nil, err
	}
	if ev.Name != "workflow_dispatch" && skipCIRe.MatchString(commit.Subject+"\n"+commit.Body) {
		return nil, nil
	}
	files, err := s.workflowFiles(ctx, g, ev.SHA)
	if err != nil {
		return nil, err
	}
	var changed []string
	changedLoaded := false
	changedFn := func() ([]string, error) {
		if changedLoaded {
			return changed, nil
		}
		changedLoaded = true
		from := ev.Before
		switch {
		case ev.Name == "pull_request":
			from, _ = g.MergeBase(ctx, ev.BaseSHA, ev.SHA)
		case isZeroSHA(from):
			// New branch: compare with where it forked from the default branch.
			from = ""
			if base, err := g.ResolveCommit(ctx, repo.DefaultBranch); err == nil {
				from, _ = g.MergeBase(ctx, base, ev.SHA)
			}
		}
		if from == "" || from == ev.SHA {
			return nil, nil
		}
		changed, err = g.ChangedFiles(ctx, from, ev.SHA)
		return changed, err
	}
	title := commit.Subject
	if ev.PullTitle != "" {
		title = ev.PullTitle
	}
	var runs []*model.WorkflowRun
	for _, f := range files {
		if ev.OnlyPath != "" && f.Path != ev.OnlyPath {
			continue
		}
		w, perr := workflow.Parse(f.Path, f.Data)
		if perr != nil {
			if ev.Name == "workflow_dispatch" {
				return nil, perr
			}
			run, err := s.createInvalidRun(ctx, repo, ev, f, title, perr)
			if err != nil {
				return runs, err
			}
			runs = append(runs, run)
			continue
		}
		wev := &workflow.Event{Name: ev.Name, Ref: ev.Ref, BaseBranch: ev.BaseBranch, Action: ev.Action, Changed: changedFn}
		ok, err := w.Matches(wev)
		if err != nil {
			s.log.Warn("ci: changed files", "repo", repo.Name, "err", err)
			ok = true
		}
		if !ok {
			continue
		}
		run, err := s.createRun(ctx, repo, ev, f, w, title)
		if err != nil {
			return runs, err
		}
		runs = append(runs, run)
	}
	if len(runs) > 0 {
		s.ci.wake.notify()
		s.ci.logs.notify()
	}
	return runs, nil
}

// nextRunNumber allocates a repository's next run number.
func nextRunNumber(ctx context.Context, db bun.IDB, repoID int64) (int64, error) {
	var n int64
	err := db.NewUpdate().Model((*model.Repo)(nil)).Set("next_run_number = next_run_number + 1").
		Where("id = ?", repoID).Returning("next_run_number").Scan(ctx, &n)
	return n - 1, err
}

func workflowDisplayName(w *workflow.Workflow, p string) string {
	if w != nil && strings.TrimSpace(w.Name) != "" {
		return strings.TrimSpace(w.Name)
	}
	return path.Base(p)
}

// newRunRow builds the run row common to valid and invalid workflows.
func (s *Server) newRunRow(repo *model.Repo, ev *ciEvent, f wfFile, name, title string) *model.WorkflowRun {
	now := s.now()
	run := &model.WorkflowRun{
		RepoID: repo.ID, WorkflowPath: f.Path, WorkflowName: name, WorkflowYAML: string(f.Data),
		Event: ev.Name, Ref: ev.Ref, HeadSHA: ev.SHA, BeforeSHA: ev.Before, Title: title,
		Status: model.RunQueued, Attempt: 1, Inputs: mustJSON(map[string]string{}), GitHubContext: "{}",
		CreatedAt: now, UpdatedAt: now,
	}
	if ev.Inputs != nil {
		run.Inputs = mustJSON(ev.Inputs)
	}
	switch {
	case ev.HeadBranch != "":
		run.HeadBranch = ev.HeadBranch
	case strings.HasPrefix(ev.Ref, "refs/heads/"):
		run.HeadBranch = strings.TrimPrefix(ev.Ref, "refs/heads/")
	}
	if ev.PullNumber > 0 {
		n := ev.PullNumber
		run.PullNumber = &n
	}
	if ev.Actor != nil {
		id := ev.Actor.ID
		run.ActorID = &id
	}
	return run
}

// createInvalidRun records a failed run for a workflow file that doesn't
// parse, so the author sees the error where they look for CI results.
func (s *Server) createInvalidRun(ctx context.Context, repo *model.Repo, ev *ciEvent, f wfFile, title string, perr error) (*model.WorkflowRun, error) {
	run := s.newRunRow(repo, ev, f, path.Base(f.Path), title)
	now := s.now()
	run.Status, run.Conclusion, run.CompletedAt = model.RunCompleted, model.ConclusionFailure, &now
	msg := perr.Error()
	if es, ok := perr.(workflow.Errors); ok && len(es) > 0 {
		msg = es[0].Error()
		if len(es) > 1 {
			msg += fmt.Sprintf(" (and %d more)", len(es)-1)
		}
	}
	run.StatusReason = "Invalid workflow file: " + msg
	return run, s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		n, err := nextRunNumber(ctx, tx, repo.ID)
		if err != nil {
			return err
		}
		run.Number = n
		_, err = tx.NewInsert().Model(run).Exec(ctx)
		return err
	})
}

// orgInfo is the organisation name and public base URL, loaded outside
// transactions (SQLite has a single connection).
type orgInfo struct{ owner, base string }

func (s *Server) loadOrgInfo(ctx context.Context) orgInfo {
	var oi orgInfo
	if org, err := s.loadOrg(ctx); err == nil && org != nil {
		oi.owner = org.Name
	}
	oi.base = s.publicBaseURL(ctx)
	return oi
}

// githubContext builds the github (alias tgp) expression context.
func githubContext(oi orgInfo, repo *model.Repo, run *model.WorkflowRun, ev *ciEvent) map[string]any {
	refName, refType := run.Ref, ""
	switch {
	case strings.HasPrefix(run.Ref, "refs/heads/"):
		refName, refType = strings.TrimPrefix(run.Ref, "refs/heads/"), "branch"
	case strings.HasPrefix(run.Ref, "refs/tags/"):
		refName, refType = strings.TrimPrefix(run.Ref, "refs/tags/"), "tag"
	case run.PullNumber != nil:
		refName = strconv.FormatInt(*run.PullNumber, 10) + "/merge"
	}
	base := oi.base
	actor := ""
	if ev.Actor != nil {
		actor = ev.Actor.Username
	}
	event := map[string]any{}
	switch ev.Name {
	case "push":
		event["before"], event["after"] = ev.Before, ev.SHA
		event["created"], event["deleted"] = isZeroSHA(ev.Before), false
	case "pull_request":
		event["action"] = ev.Action
		event["number"] = float64(ev.PullNumber)
		event["pull_request"] = map[string]any{
			"number": float64(ev.PullNumber), "title": ev.PullTitle,
			"head": map[string]any{"ref": ev.HeadBranch, "sha": ev.SHA},
			"base": map[string]any{"ref": ev.BaseBranch, "sha": ev.BaseSHA},
		}
	case "workflow_dispatch":
		inputs := map[string]any{}
		for k, v := range ev.Inputs {
			inputs[k] = v
		}
		event["inputs"] = inputs
	}
	owner := oi.owner
	gh := map[string]any{
		"event_name": ev.Name, "event": event,
		"ref": run.Ref, "ref_name": refName, "ref_type": refType, "sha": run.HeadSHA,
		"repository": repo.Name, "repository_owner": owner,
		"run_id": float64(run.ID), "run_number": float64(run.Number), "run_attempt": float64(run.Attempt),
		"actor": actor, "triggering_actor": actor,
		"workflow": run.WorkflowName, "workflow_ref": run.WorkflowPath,
		"head_ref": "", "base_ref": "",
		"server_url": base, "api_url": base + "/api/v1",
		"default_branch": repo.DefaultBranch,
	}
	if ev.Name == "pull_request" {
		gh["head_ref"], gh["base_ref"] = ev.HeadBranch, ev.BaseBranch
	}
	return gh
}

// publicBaseURL is the configured base URL (config, then setup), or "".
func (s *Server) publicBaseURL(ctx context.Context) string {
	base := s.cfg.Server.BaseURL
	if base == "" {
		if org, err := s.loadOrg(ctx); err == nil {
			base = org.BaseURL
		}
	}
	return strings.TrimRight(base, "/")
}

// createRun inserts a run with its jobs and lets admission rules and the
// state machine take it from there.
func (s *Server) createRun(ctx context.Context, repo *model.Repo, ev *ciEvent, f wfFile, w *workflow.Workflow, title string) (*model.WorkflowRun, error) {
	settings, err := s.ciSettings(ctx)
	if err != nil {
		return nil, err
	}
	run := s.newRunRow(repo, ev, f, workflowDisplayName(w, f.Path), title)
	oi := s.loadOrgInfo(ctx)
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		n, err := nextRunNumber(ctx, tx, repo.ID)
		if err != nil {
			return err
		}
		run.Number = n
		if _, err := tx.NewInsert().Model(run).Exec(ctx); err != nil {
			return err
		}
		gh := githubContext(oi, repo, run, ev)
		run.GitHubContext = mustJSON(gh)
		inputs := map[string]any{}
		for k, v := range jsonStringMap(run.Inputs) {
			inputs[k] = v
		}
		ec := &expr.Context{Contexts: map[string]any{"github": gh, "tgp": gh, "inputs": inputs, "vars": map[string]any{}}}
		if w.Concurrency != nil {
			g, err := expr.Interpolate(w.Concurrency.Group, ec)
			if err != nil {
				return s.failRun(ctx, tx, run, "Invalid concurrency group: "+err.Error())
			}
			run.ConcurrencyGroup, run.CancelInProgress = strings.TrimSpace(g), w.Concurrency.CancelInProgress
		}
		if _, err := tx.NewUpdate().Model(run).Column("github_context", "concurrency_group", "cancel_in_progress").WherePK().Exec(ctx); err != nil {
			return err
		}
		jobs, err := buildJobs(run, w, settings, ec)
		if err != nil {
			return s.failRun(ctx, tx, run, "Invalid workflow: "+err.Error())
		}
		for _, j := range jobs {
			if _, err := tx.NewInsert().Model(j.job).Exec(ctx); err != nil {
				return err
			}
			if err := insertSteps(ctx, tx, j.job, j.def); err != nil {
				return err
			}
		}
		if err := s.admitRun(ctx, tx, repo, run, ev, settings); err != nil {
			return err
		}
		_, err = s.advanceRun(ctx, tx, run)
		return err
	})
	return run, err
}

func (s *Server) failRun(ctx context.Context, tx bun.IDB, run *model.WorkflowRun, reason string) error {
	now := s.now()
	run.Status, run.Conclusion, run.StatusReason, run.CompletedAt, run.UpdatedAt = model.RunCompleted, model.ConclusionFailure, reason, &now, now
	_, err := tx.NewUpdate().Model(run).Column("status", "conclusion", "status_reason", "completed_at", "updated_at").WherePK().Exec(ctx)
	return err
}

type builtJob struct {
	job *model.Job
	def jobDef
}

// buildJobs expands a workflow's jobs (matrix included) into job rows,
// applying the instance defaults and limits.
func buildJobs(run *model.WorkflowRun, w *workflow.Workflow, cs ciSettings, ec *expr.Context) ([]builtJob, error) {
	var out []builtJob
	now := run.CreatedAt
	total := 0
	for _, key := range w.JobOrder {
		wj := w.Jobs[key]
		insts, err := wj.Instances(ec)
		if err != nil {
			return nil, err
		}
		total += len(insts)
		if total > workflow.MaxMatrixJobs {
			return nil, fmt.Errorf("the workflow expands to more than %d jobs", workflow.MaxMatrixJobs)
		}
		secrets := referencedSecrets(jobStrings(w, wj))
		for idx, inst := range insts {
			mc := workflow.WithContext(ec, "matrix", inst.Matrix)
			if inst.Matrix == nil {
				mc = workflow.WithContext(ec, "matrix", map[string]any{})
			}
			strategy := map[string]any{"fail-fast": true, "job-index": float64(idx), "job-total": float64(len(insts)), "max-parallel": float64(len(insts))}
			if wj.Strategy != nil {
				strategy["fail-fast"] = wj.Strategy.FailFast
				if wj.Strategy.MaxParallel > 0 {
					strategy["max-parallel"] = float64(wj.Strategy.MaxParallel)
				}
			}
			mc = workflow.WithContext(mc, "strategy", strategy)
			labels := make([]string, 0, len(wj.RunsOn))
			for _, l := range wj.RunsOn {
				v, err := expr.Interpolate(l, mc)
				if err != nil {
					return nil, fmt.Errorf("jobs.%s.runs-on: %w", key, err)
				}
				if v = strings.TrimSpace(v); v != "" {
					labels = append(labels, v)
				}
			}
			image := cs.DefaultImage
			var containerEnv map[string]string
			if wj.Container != nil {
				v, err := expr.Interpolate(wj.Container.Image, mc)
				if err != nil {
					return nil, fmt.Errorf("jobs.%s.container.image: %w", key, err)
				}
				if image = strings.TrimSpace(v); image == "" {
					return nil, fmt.Errorf("jobs.%s.container.image is empty", key)
				}
				containerEnv = wj.Container.Env
			}
			var services []runnerapi.Service
			for _, name := range sortedKeys(wj.Services) {
				svc := wj.Services[name]
				img, err := expr.Interpolate(svc.Image, mc)
				if err != nil {
					return nil, fmt.Errorf("jobs.%s.services.%s.image: %w", key, name, err)
				}
				services = append(services, runnerapi.Service{Name: name, Image: strings.TrimSpace(img), Env: svc.Env})
			}
			cpus, mem, pids := wj.Resources.CPUs, wj.Resources.MemoryBytes, wj.Resources.Pids
			if cpus == 0 {
				cpus = cs.DefaultCPUs
			}
			if mem == 0 {
				mem = int64(cs.DefaultMemoryMB) << 20
			}
			if pids == 0 {
				pids = cs.DefaultPids
			}
			switch {
			case cpus > cs.MaxCPUs:
				return nil, fmt.Errorf("jobs.%s asks for %g CPUs; this instance allows at most %g", key, cpus, cs.MaxCPUs)
			case mem > int64(cs.MaxMemoryMB)<<20:
				return nil, fmt.Errorf("jobs.%s asks for %d MiB of memory; this instance allows at most %d MiB", key, mem>>20, cs.MaxMemoryMB)
			case pids > cs.MaxPids:
				return nil, fmt.Errorf("jobs.%s asks for %d processes; this instance allows at most %d", key, pids, cs.MaxPids)
			}
			timeout := wj.TimeoutMinutes
			if timeout == 0 {
				timeout = cs.DefaultTimeoutMinutes
			}
			if timeout > cs.MaxTimeoutMinutes {
				return nil, fmt.Errorf("jobs.%s has timeout-minutes %d; this instance allows at most %d", key, timeout, cs.MaxTimeoutMinutes)
			}
			defaults := w.Defaults
			if wj.Defaults.Shell != "" {
				defaults.Shell = wj.Defaults.Shell
			}
			if wj.Defaults.WorkingDirectory != "" {
				defaults.WorkingDirectory = wj.Defaults.WorkingDirectory
			}
			steps := make([]runnerapi.Step, len(wj.Steps))
			for i, st := range wj.Steps {
				steps[i] = runnerapi.Step{Number: i + 1, Step: st}
			}
			def := jobDef{
				Steps: steps, Services: services, ContainerEnv: containerEnv, WorkflowEnv: w.Env, JobEnv: wj.Env,
				Defaults: defaults, Outputs: wj.Outputs, If: wj.If, ContinueOnError: wj.ContinueOnError,
				FailFast: wj.Strategy != nil && wj.Strategy.FailFast, Strategy: strategy, Secrets: secrets,
			}
			maxParallel := 0
			if wj.Strategy != nil {
				maxParallel = wj.Strategy.MaxParallel
			}
			needs := wj.Needs
			if needs == nil {
				needs = []string{}
			}
			j := &model.Job{
				RunID: run.ID, RepoID: run.RepoID, Attempt: run.Attempt, Key: key, Name: inst.Name,
				CheckName: run.WorkflowName + " / " + inst.Name,
				Needs:     mustJSON(needs), Labels: mustJSON(labels), Image: image,
				CPUs: cpus, MemoryBytes: mem, Pids: pids, TimeoutMinutes: timeout, MaxParallel: maxParallel,
				Definition: mustJSON(def), Status: model.JobPending, Outputs: "{}", CreatedAt: now,
			}
			if inst.Matrix != nil {
				j.Matrix = mustJSON(inst.Matrix)
			}
			out = append(out, builtJob{job: j, def: def})
		}
	}
	return out, nil
}

// admitRun applies the queue rules to a new run: superseding older runs of
// the same branch or pull request, concurrency groups and the per-repo
// queue limit.
func (s *Server) admitRun(ctx context.Context, tx bun.IDB, repo *model.Repo, run *model.WorkflowRun, ev *ciEvent, cs ciSettings) error {
	active := []string{model.RunWaiting, model.RunQueued, model.RunInProgress}
	cancelAll := func(q *bun.SelectQuery, reason string) error {
		var olds []*model.WorkflowRun
		if err := q.Scan(ctx, &olds); err != nil {
			return err
		}
		for _, o := range olds {
			if err := s.cancelRun(ctx, tx, o, reason); err != nil {
				return err
			}
		}
		return nil
	}
	// 1. A newer push to a branch (not the default branch or tags) or pull
	// request supersedes older runs of the same workflow.
	if cs.CancelSuperseded {
		q := tx.NewSelect().Model((*model.WorkflowRun)(nil)).
			Where("repo_id = ? AND workflow_path = ? AND id <> ? AND status IN (?)", run.RepoID, run.WorkflowPath, run.ID, bun.In(active))
		apply := false
		switch {
		case ev.Name == "pull_request":
			q, apply = q.Where("event = ? AND pull_number = ?", ev.Name, ev.PullNumber), true
		case ev.Name == "push" && strings.HasPrefix(ev.Ref, "refs/heads/") && ev.Ref != branchRef(repo.DefaultBranch):
			q, apply = q.Where("event = ? AND ref = ?", ev.Name, ev.Ref), true
		}
		if apply {
			if err := cancelAll(q, fmt.Sprintf("Superseded by run #%d", run.Number)); err != nil {
				return err
			}
		}
	}
	// 2. Concurrency group: one running and one waiting run per group.
	if run.ConcurrencyGroup != "" {
		q := tx.NewSelect().Model((*model.WorkflowRun)(nil)).
			Where("repo_id = ? AND concurrency_group = ? AND id <> ?", run.RepoID, run.ConcurrencyGroup, run.ID)
		if run.CancelInProgress {
			q = q.Where("status IN (?)", bun.In(active))
			if err := cancelAll(q, fmt.Sprintf("Cancelled by run #%d (concurrency group %s)", run.Number, run.ConcurrencyGroup)); err != nil {
				return err
			}
		} else {
			if err := cancelAll(q.Clone().Where("status = ?", model.RunWaiting),
				fmt.Sprintf("Replaced by run #%d (concurrency group %s)", run.Number, run.ConcurrencyGroup)); err != nil {
				return err
			}
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
	}
	// 3. Queue limit on runs that haven't started.
	if cs.MaxQueuedRunsPerRepo > 0 {
		count := func() (int, error) {
			n, err := tx.NewSelect().Model((*model.WorkflowRun)(nil)).
				Where("repo_id = ? AND id <> ? AND started_at IS NULL AND status IN (?)", run.RepoID, run.ID,
					bun.In([]string{model.RunWaiting, model.RunQueued})).Count(ctx)
			return int(n), err
		}
		n, err := count()
		if err != nil {
			return err
		}
		if n >= cs.MaxQueuedRunsPerRepo {
			// Make room by superseding the oldest unstarted runs for the
			// same workflow and ref.
			var olds []*model.WorkflowRun
			if err := tx.NewSelect().Model(&olds).
				Where("repo_id = ? AND id <> ? AND workflow_path = ? AND ref = ? AND started_at IS NULL AND status IN (?)",
					run.RepoID, run.ID, run.WorkflowPath, run.Ref, bun.In([]string{model.RunWaiting, model.RunQueued})).
				Order("id").Limit(int64(n - cs.MaxQueuedRunsPerRepo + 1)).Scan(ctx); err != nil {
				return err
			}
			for _, o := range olds {
				if err := s.cancelRun(ctx, tx, o, fmt.Sprintf("Superseded by run #%d: the repository's queue is full (limit %d)", run.Number, cs.MaxQueuedRunsPerRepo)); err != nil {
					return err
				}
			}
			if n, err = count(); err != nil {
				return err
			}
		}
		if n >= cs.MaxQueuedRunsPerRepo {
			return s.cancelRun(ctx, tx, run, fmt.Sprintf("Rejected: %d runs are already queued for this repository (limit %d)", n, cs.MaxQueuedRunsPerRepo))
		}
	}
	return nil
}
