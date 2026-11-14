package server

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/workflow"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// ---- loading ----

func (s *Server) loadRun(ctx context.Context, repo *model.Repo, number int64) (*model.WorkflowRun, error) {
	run := new(model.WorkflowRun)
	err := s.db.NewSelect().Model(run).Where("repo_id = ? AND number = ?", repo.ID, number).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.NotFound("run #%d not found in %s", number, repo.Name)
	}
	return run, err
}

func (s *Server) loadJob(ctx context.Context, repo *model.Repo, id int64) (*model.Job, error) {
	j := new(model.Job)
	err := s.db.NewSelect().Model(j).Where("id = ? AND repo_id = ?", id, repo.ID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.NotFound("job %d not found in %s", id, repo.Name)
	}
	return j, err
}

// ---- conversions ----

func conclusionPtr(c string) *apigen.Conclusion {
	if c == "" {
		return nil
	}
	v := apigen.Conclusion(c)
	return &v
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *Server) toAPIRun(ctx context.Context, uc *userCache, repo *model.Repo, run *model.WorkflowRun) apigen.WorkflowRun {
	out := apigen.WorkflowRun{
		Id: run.ID, Number: run.Number, Repo: repo.Name, WorkflowPath: run.WorkflowPath, WorkflowName: run.WorkflowName,
		Title: run.Title, Event: apigen.RunEvent(run.Event), Ref: run.Ref, HeadSha: run.HeadSHA, PullNumber: run.PullNumber,
		Status: apigen.RunStatus(run.Status), Conclusion: conclusionPtr(run.Conclusion), StatusReason: strPtrOrNil(run.StatusReason),
		Attempt: run.Attempt, CreatedAt: run.CreatedAt, StartedAt: run.StartedAt, CompletedAt: run.CompletedAt,
		HeadBranch: strPtrOrNil(run.HeadBranch),
	}
	out.Actor = uc.summaryPtr(ctx, run.ActorID)
	return out
}

func (s *Server) jobSteps(ctx context.Context, jobIDs ...int64) (map[int64][]apigen.JobStep, error) {
	out := map[int64][]apigen.JobStep{}
	if len(jobIDs) == 0 {
		return out, nil
	}
	var steps []model.JobStep
	if err := s.db.NewSelect().Model(&steps).Where("job_id IN (?)", bun.In(jobIDs)).Order("job_id", "number").Scan(ctx); err != nil {
		return nil, err
	}
	for _, st := range steps {
		v := apigen.JobStep{Number: st.Number, Name: st.Name, Status: apigen.JobStepStatus(st.Status), StartedAt: st.StartedAt, CompletedAt: st.CompletedAt}
		if st.Conclusion != "" {
			c := apigen.JobStepConclusion(st.Conclusion)
			v.Conclusion = &c
		}
		out[st.JobID] = append(out[st.JobID], v)
	}
	return out, nil
}

// toAPIJobs converts jobs with their steps, runners and wait reasons.
func (s *Server) toAPIJobs(ctx context.Context, run *model.WorkflowRun, jobs []*model.Job) ([]apigen.Job, error) {
	ids := make([]int64, len(jobs))
	runnerIDs := map[int64]bool{}
	for i, j := range jobs {
		ids[i] = j.ID
		if j.RunnerID != nil {
			runnerIDs[*j.RunnerID] = true
		}
	}
	steps, err := s.jobSteps(ctx, ids...)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	if len(runnerIDs) > 0 {
		var rs []model.Runner
		keys := make([]int64, 0, len(runnerIDs))
		for id := range runnerIDs {
			keys = append(keys, id)
		}
		if err := s.db.NewSelect().Model(&rs).Column("id", "name").Where("id IN (?)", bun.In(keys)).Scan(ctx); err != nil {
			return nil, err
		}
		for _, r := range rs {
			names[r.ID] = r.Name
		}
	}
	reasons, err := s.waitReasons(ctx, jobs)
	if err != nil {
		return nil, err
	}
	out := make([]apigen.Job, len(jobs))
	for i, j := range jobs {
		v := apigen.Job{
			Id: j.ID, RunNumber: run.Number, Attempt: j.Attempt, Key: j.Key, Name: j.Name, CheckName: j.CheckName,
			Needs: jsonStrings(j.Needs), Status: apigen.JobStatus(j.Status), Conclusion: conclusionPtr(j.Conclusion),
			FailureReason: strPtrOrNil(j.FailureReason), WaitReason: strPtrOrNil(reasons[j.ID]),
			Labels: jsonStrings(j.Labels), Image: j.Image,
			Resources:      apigen.JobResources{Cpus: float32(j.CPUs), MemoryBytes: j.MemoryBytes, Pids: j.Pids},
			TimeoutMinutes: j.TimeoutMinutes, Steps: steps[j.ID], CreatedAt: j.CreatedAt, QueuedAt: j.QueuedAt,
			StartedAt: j.StartedAt, CompletedAt: j.CompletedAt,
		}
		if v.Steps == nil {
			v.Steps = []apigen.JobStep{}
		}
		if m := jsonMap(j.Matrix); m != nil {
			v.Matrix = &m
		}
		if j.RunnerID != nil {
			v.Runner = &apigen.RunnerRef{Id: *j.RunnerID, Name: names[*j.RunnerID]}
		}
		out[i] = v
	}
	return out, nil
}

func (s *Server) jobView(ctx context.Context, repo *model.Repo, _ authz.Perm, j *model.Job) (apigen.Job, error) {
	run, err := loadRunByID(ctx, s.db, j.RunID)
	if err != nil {
		return apigen.Job{}, err
	}
	jobs, err := s.toAPIJobs(ctx, run, []*model.Job{j})
	if err != nil {
		return apigen.Job{}, err
	}
	return jobs[0], nil
}

func (s *Server) runDetail(ctx context.Context, repo *model.Repo, perm authz.Perm, run *model.WorkflowRun) (apigen.WorkflowRunDetail, error) {
	uc := s.users()
	base := s.toAPIRun(ctx, uc, repo, run)
	jobs, err := runJobs(ctx, s.db, run)
	if err != nil {
		return apigen.WorkflowRunDetail{}, err
	}
	apiJobs, err := s.toAPIJobs(ctx, run, jobs)
	if err != nil {
		return apigen.WorkflowRunDetail{}, err
	}
	inputs := jsonStringMap(run.Inputs)
	return apigen.WorkflowRunDetail{
		Id: base.Id, Number: base.Number, Repo: base.Repo, WorkflowPath: base.WorkflowPath, WorkflowName: base.WorkflowName,
		Title: base.Title, Event: base.Event, Ref: base.Ref, HeadBranch: base.HeadBranch, HeadSha: base.HeadSha,
		PullNumber: base.PullNumber, Actor: base.Actor, Status: base.Status, Conclusion: base.Conclusion,
		StatusReason: base.StatusReason, Attempt: base.Attempt, CreatedAt: base.CreatedAt, StartedAt: base.StartedAt,
		CompletedAt: base.CompletedAt, Jobs: apiJobs, Inputs: inputs,
		CanCancel: perm >= authz.Write && run.Status != model.RunCompleted,
		CanRerun:  perm >= authz.Triage && run.Status == model.RunCompleted && len(jobs) > 0 && !repo.Archived,
	}, nil
}

// ---- runs ----

// runFilters applies the shared list filters.
func (s *Server) runFilters(ctx context.Context, q *bun.SelectQuery, status string, actor *string) *bun.SelectQuery {
	switch st := status; st {
	case "":
	case "active":
		q = q.Where("wr.status <> ?", model.RunCompleted)
	case model.RunWaiting, model.RunQueued, model.RunInProgress, model.RunCompleted:
		q = q.Where("wr.status = ?", st)
	default:
		q = q.Where("wr.conclusion = ?", st)
	}
	if a := deref(actor); a != "" {
		if a == "@me" {
			q = q.Where("wr.actor_id = ?", mustPrincipal(ctx).User.ID)
		} else {
			q = q.Where("wr.actor_id = (SELECT id FROM users WHERE username = ?)", strings.ToLower(a))
		}
	}
	return q
}

func (a *api) ListRuns(ctx context.Context, req apigen.ListRunsRequestObject) (apigen.ListRunsResponseObject, error) {
	s := a.s
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	p := req.Params
	q := s.db.NewSelect().Model((*model.WorkflowRun)(nil)).Where("wr.repo_id = ?", repo.ID)
	q = s.runFilters(ctx, q, string(deref(p.Status)), p.Actor)
	if v := deref(p.Workflow); v != "" {
		if !strings.Contains(v, "/") {
			v = workflow.Dir + "/" + v
		}
		q = q.Where("wr.workflow_path = ?", v)
	}
	if v := deref(p.Branch); v != "" {
		q = q.Where("wr.head_branch = ?", v)
	}
	if v := deref(p.Event); v != "" {
		q = q.Where("wr.event = ?", string(v))
	}
	if p.Pull != nil {
		q = q.Where("wr.pull_number = ?", *p.Pull)
	}
	if v := strings.ToLower(deref(p.Sha)); v != "" {
		q = q.Where("wr.head_sha LIKE ?", escapeLike(v)+"%")
	}
	limit, offset := pageParams(p.Page, p.PerPage)
	var runs []*model.WorkflowRun
	total, err := q.Order("wr.id DESC").Limit(int64(limit)).Offset(int64(offset)).ScanAndCount(ctx, &runs)
	if err != nil {
		return nil, err
	}
	uc := s.users()
	out := apigen.RunPage{Runs: make([]apigen.WorkflowRun, len(runs)), Total: int(total)}
	for i, r := range runs {
		out.Runs[i] = s.toAPIRun(ctx, uc, repo, r)
	}
	return apigen.ListRuns200JSONResponse(out), nil
}

func (a *api) ListAllRuns(ctx context.Context, req apigen.ListAllRunsRequestObject) (apigen.ListAllRunsResponseObject, error) {
	s := a.s
	user := mustPrincipal(ctx).User
	p := req.Params
	q := s.db.NewSelect().Model((*model.WorkflowRun)(nil))
	if user.Role != model.RoleAdmin && !user.AllRepos {
		q = q.Where("wr.repo_id IN (SELECT repo_id FROM repo_grants WHERE user_id = ?)", user.ID)
	}
	q = s.runFilters(ctx, q, string(deref(p.Status)), p.Actor)
	limit, offset := pageParams(p.Page, p.PerPage)
	var runs []*model.WorkflowRun
	total, err := q.Order("wr.id DESC").Limit(int64(limit)).Offset(int64(offset)).ScanAndCount(ctx, &runs)
	if err != nil {
		return nil, err
	}
	repoIDs := map[int64]bool{}
	for _, r := range runs {
		repoIDs[r.RepoID] = true
	}
	repos := map[int64]*model.Repo{}
	if len(repoIDs) > 0 {
		ids := make([]int64, 0, len(repoIDs))
		for id := range repoIDs {
			ids = append(ids, id)
		}
		var rs []*model.Repo
		if err := s.db.NewSelect().Model(&rs).Where("r.id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
			return nil, err
		}
		for _, r := range rs {
			repos[r.ID] = r
		}
	}
	uc := s.users()
	out := apigen.RunPage{Runs: make([]apigen.WorkflowRun, 0, len(runs)), Total: int(total)}
	for _, r := range runs {
		if repo := repos[r.RepoID]; repo != nil {
			out.Runs = append(out.Runs, s.toAPIRun(ctx, uc, repo, r))
		}
	}
	return apigen.ListAllRuns200JSONResponse(out), nil
}

func (a *api) GetRun(ctx context.Context, req apigen.GetRunRequestObject) (apigen.GetRunResponseObject, error) {
	repo, perm, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	run, err := a.s.loadRun(ctx, repo, req.Number)
	if err != nil {
		return nil, err
	}
	d, err := a.s.runDetail(ctx, repo, perm, run)
	if err != nil {
		return nil, err
	}
	return apigen.GetRun200JSONResponse(d), nil
}

func (a *api) CancelRun(ctx context.Context, req apigen.CancelRunRequestObject) (apigen.CancelRunResponseObject, error) {
	s := a.s
	repo, perm, err := s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	run, err := s.loadRun(ctx, repo, req.Number)
	if err != nil {
		return nil, err
	}
	if run.Status == model.RunCompleted {
		return nil, apierr.Conflict("run #%d already finished (%s)", run.Number, run.Conclusion)
	}
	user := mustPrincipal(ctx).User
	if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := tx.NewSelect().Model(run).WherePK().Scan(ctx); err != nil {
			return err
		}
		return s.cancelRun(ctx, tx, run, "Cancelled by "+user.Username)
	}); err != nil {
		return nil, err
	}
	s.ci.wake.notify()
	s.ci.logs.notify()
	d, err := s.runDetail(ctx, repo, perm, run)
	if err != nil {
		return nil, err
	}
	return apigen.CancelRun200JSONResponse(d), nil
}

func (s *Server) rerun(ctx context.Context, repo *model.Repo, run *model.WorkflowRun, failedOnly bool, job *model.Job) error {
	if repo.Archived {
		return apierr.Forbidden("%s is archived", repo.Name)
	}
	if run.Status != model.RunCompleted {
		return apierr.Conflict("run #%d is still running; cancel it or wait for it to finish", run.Number)
	}
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := tx.NewSelect().Model(run).WherePK().Scan(ctx); err != nil {
			return err
		}
		if run.Status != model.RunCompleted {
			return apierr.Conflict("run #%d is already running again", run.Number)
		}
		if err := s.rerunRun(ctx, tx, run, failedOnly, job); err != nil {
			var ae *apierr.Error
			if errors.As(err, &ae) {
				return err
			}
			if strings.Contains(err.Error(), "nothing to re-run") || strings.Contains(err.Error(), "no jobs") {
				return apierr.Conflict("%s", err.Error())
			}
			return err
		}
		return nil
	})
	if err == nil {
		s.ci.wake.notify()
		s.ci.logs.notify()
	}
	return err
}

func (a *api) RerunRun(ctx context.Context, req apigen.RerunRunRequestObject) (apigen.RerunRunResponseObject, error) {
	s := a.s
	repo, perm, err := s.repoFor(ctx, req.Repo, authz.Triage)
	if err != nil {
		return nil, err
	}
	run, err := s.loadRun(ctx, repo, req.Number)
	if err != nil {
		return nil, err
	}
	failedOnly := req.Body != nil && deref(req.Body.FailedOnly)
	if err := s.rerun(ctx, repo, run, failedOnly, nil); err != nil {
		return nil, err
	}
	d, err := s.runDetail(ctx, repo, perm, run)
	if err != nil {
		return nil, err
	}
	return apigen.RerunRun200JSONResponse(d), nil
}

// ---- jobs ----

func (a *api) GetJob(ctx context.Context, req apigen.GetJobRequestObject) (apigen.GetJobResponseObject, error) {
	repo, perm, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	j, err := a.s.loadJob(ctx, repo, req.Id)
	if err != nil {
		return nil, err
	}
	v, err := a.s.jobView(ctx, repo, perm, j)
	if err != nil {
		return nil, err
	}
	return apigen.GetJob200JSONResponse(v), nil
}

func (a *api) RerunJob(ctx context.Context, req apigen.RerunJobRequestObject) (apigen.RerunJobResponseObject, error) {
	s := a.s
	repo, perm, err := s.repoFor(ctx, req.Repo, authz.Triage)
	if err != nil {
		return nil, err
	}
	j, err := s.loadJob(ctx, repo, req.Id)
	if err != nil {
		return nil, err
	}
	run, err := loadRunByID(ctx, s.db, j.RunID)
	if err != nil {
		return nil, err
	}
	if j.Attempt != run.Attempt {
		return nil, apierr.Conflict("job %d belongs to an older attempt of run #%d; re-run a job of the latest attempt", j.ID, run.Number)
	}
	if err := s.rerun(ctx, repo, run, false, j); err != nil {
		return nil, err
	}
	d, err := s.runDetail(ctx, repo, perm, run)
	if err != nil {
		return nil, err
	}
	return apigen.RerunJob200JSONResponse(d), nil
}

func (a *api) GetJobLogs(ctx context.Context, req apigen.GetJobLogsRequestObject) (apigen.GetJobLogsResponseObject, error) {
	s := a.s
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	j, err := s.loadJob(ctx, repo, req.Id)
	if err != nil {
		return nil, err
	}
	src := j
	if j.ReusedFromID != nil {
		src = new(model.Job)
		if err := s.db.NewSelect().Model(src).Where("id = ?", *j.ReusedFromID).Scan(ctx); err != nil {
			return nil, err
		}
	}
	limit := 5000
	if req.Params.Limit != nil {
		limit = min(max(*req.Params.Limit, 1), 20000)
	}
	after := int64(deref(req.Params.After))
	lines, next, err := s.readLogs(ctx, j, after, limit, req.Params.Step)
	if err != nil {
		return nil, err
	}
	complete := logsComplete(j, src, next)
	if req.Params.Step != nil && len(lines) < limit && j.Status == model.JobCompleted {
		complete, next = true, max(next, src.LogLines)
	}
	return apigen.GetJobLogs200JSONResponse{Lines: lines, Next: int(next), Complete: complete, Truncated: src.LogTruncated}, nil
}

// ---- workflows ----

func (a *api) ListWorkflows(ctx context.Context, req apigen.ListWorkflowsRequestObject) (apigen.ListWorkflowsResponseObject, error) {
	s := a.s
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	g := s.repos.Open(repo)
	ref := deref(req.Params.Ref)
	if ref == "" {
		ref = repo.DefaultBranch
	}
	out := []apigen.Workflow{}
	sha, err := g.ResolveCommit(ctx, ref)
	if err != nil {
		if empty, _ := g.IsEmpty(ctx); empty || ref == repo.DefaultBranch {
			return apigen.ListWorkflows200JSONResponse(out), nil
		}
		return nil, apierr.NotFound("ref %q not found", ref)
	}
	files, err := s.workflowFiles(ctx, g, sha)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		out = append(out, describeWorkflow(f))
	}
	return apigen.ListWorkflows200JSONResponse(out), nil
}

func describeWorkflow(f wfFile) apigen.Workflow {
	v := apigen.Workflow{
		Path: f.Path, File: path.Base(f.Path), Name: path.Base(f.Path), Valid: true,
		Errors: []apigen.WorkflowError{}, Triggers: []apigen.RunEvent{}, DispatchInputs: []apigen.DispatchInput{}, Jobs: []string{},
	}
	w, err := workflow.Parse(f.Path, f.Data)
	if err != nil {
		v.Valid = false
		var es workflow.Errors
		if errors.As(err, &es) {
			for _, e := range es {
				we := apigen.WorkflowError{Message: e.Msg}
				if e.Line > 0 {
					line, col := e.Line, e.Col
					we.Line, we.Column = &line, &col
				}
				v.Errors = append(v.Errors, we)
			}
		} else {
			v.Errors = append(v.Errors, apigen.WorkflowError{Message: err.Error()})
		}
		return v
	}
	v.Name = workflowDisplayName(w, f.Path)
	v.Jobs = w.JobOrder
	if w.On.Push != nil {
		v.Triggers = append(v.Triggers, apigen.RunEventPush)
	}
	if w.On.PullRequest != nil {
		v.Triggers = append(v.Triggers, apigen.RunEventPullRequest)
	}
	if d := w.On.Dispatch; d != nil {
		v.Triggers = append(v.Triggers, apigen.RunEventWorkflowDispatch)
		for _, name := range d.InputOrder {
			in := d.Inputs[name]
			di := apigen.DispatchInput{Name: name, Description: in.Description, Required: in.Required, Type: apigen.DispatchInputType(in.Type)}
			if in.Default != "" {
				def := in.Default
				di.Default = &def
			}
			if in.Options != nil {
				opts := in.Options
				di.Options = &opts
			}
			v.DispatchInputs = append(v.DispatchInputs, di)
		}
	}
	return v
}

func (a *api) DispatchWorkflow(ctx context.Context, req apigen.DispatchWorkflowRequestObject) (apigen.DispatchWorkflowResponseObject, error) {
	s := a.s
	repo, perm, err := s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	if repo.Archived {
		return nil, apierr.Forbidden("%s is archived", repo.Name)
	}
	var body apigen.DispatchRequest
	if req.Body != nil {
		body = *req.Body
	}
	ref := deref(body.Ref)
	if ref == "" {
		ref = repo.DefaultBranch
	}
	g := s.repos.Open(repo)
	fullRef := ""
	switch {
	case strings.HasPrefix(ref, "refs/"):
		fullRef = ref
	default:
		if _, err := g.RefCommit(ctx, branchRef(ref)); err == nil {
			fullRef = branchRef(ref)
		} else if _, err := g.RefCommit(ctx, "refs/tags/"+ref); err == nil {
			fullRef = "refs/tags/" + ref
		}
	}
	if fullRef == "" {
		return nil, apierr.Validation("ref %q is not a branch or tag of %s", ref, repo.Name)
	}
	sha, err := g.RefCommit(ctx, fullRef)
	if err != nil {
		return nil, apierr.Validation("ref %q not found", ref)
	}
	file := req.Workflow
	p := file
	if !strings.Contains(p, "/") {
		p = workflow.Dir + "/" + p
	}
	files, err := s.workflowFiles(ctx, g, sha)
	if err != nil {
		return nil, err
	}
	var wf *wfFile
	for i := range files {
		if files[i].Path == p {
			wf = &files[i]
		}
	}
	if wf == nil {
		return nil, apierr.NotFound("workflow %s not found at %s", p, ref)
	}
	w, perr := workflow.Parse(wf.Path, wf.Data)
	if perr != nil {
		return nil, apierr.Validation("%s is invalid: %v", p, perr)
	}
	if w.On.Dispatch == nil {
		return nil, apierr.Validation("%s has no workflow_dispatch trigger, so it can't be run manually", p)
	}
	inputs, err := checkInputs(w.On.Dispatch, deref(body.Inputs))
	if err != nil {
		return nil, err
	}
	ev := &ciEvent{Name: "workflow_dispatch", Ref: fullRef, SHA: sha, Actor: mustPrincipal(ctx).User, Inputs: inputs, OnlyPath: p}
	runs, err := s.triggerRuns(ctx, repo, ev)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, apierr.Conflict("the workflow didn't start")
	}
	d, err := s.runDetail(ctx, repo, perm, runs[0])
	if err != nil {
		return nil, err
	}
	return apigen.DispatchWorkflow201JSONResponse(d), nil
}

// checkInputs validates dispatch inputs and fills defaults.
func checkInputs(d *workflow.DispatchTrigger, given map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for k := range given {
		if _, ok := d.Inputs[k]; !ok {
			return nil, apierr.Validation("unknown input %q (inputs: %s)", k, strings.Join(d.InputOrder, ", ")).WithDetails(map[string]any{"field": "inputs." + k})
		}
	}
	for _, name := range d.InputOrder {
		in := d.Inputs[name]
		v, ok := given[name]
		if !ok {
			v = in.Default
		}
		if v == "" && in.Type == "boolean" {
			v = "false"
		}
		if v == "" && in.Required {
			return nil, apierr.Validation("input %q is required", name).WithDetails(map[string]any{"field": "inputs." + name})
		}
		switch in.Type {
		case "boolean":
			if v != "true" && v != "false" {
				return nil, apierr.Validation("input %q must be true or false", name)
			}
		case "number":
			if v != "" {
				if _, err := strconv.ParseFloat(v, 64); err != nil {
					return nil, apierr.Validation("input %q must be a number", name)
				}
			}
		case "choice":
			ok := false
			for _, o := range in.Options {
				ok = ok || o == v
			}
			if !ok && !(v == "" && !in.Required) {
				return nil, apierr.Validation("input %q must be one of: %s", name, strings.Join(in.Options, ", "))
			}
		}
		out[name] = v
	}
	return out, nil
}

// ---- checks ----

// commitChecks summarises the latest attempt of every job that ran on sha.
func (s *Server) commitChecks(ctx context.Context, repo *model.Repo, sha string, required []string) (apigen.CheckSummary, error) {
	out := apigen.CheckSummary{State: apigen.CheckSummaryStateNone, Checks: []apigen.Check{}, MissingRequired: []string{}}
	var runs []*model.WorkflowRun
	if sha != "" {
		if err := s.db.NewSelect().Model(&runs).Where("repo_id = ? AND head_sha = ?", repo.ID, sha).Order("id DESC").Scan(ctx); err != nil {
			return out, err
		}
	}
	req := map[string]bool{}
	for _, r := range required {
		req[r] = true
	}
	seen := map[string]bool{}
	for _, run := range runs {
		jobs, err := runJobs(ctx, s.db, run)
		if err != nil {
			return out, err
		}
		for _, j := range jobs {
			if seen[j.CheckName] {
				continue // a newer run already reported this check
			}
			seen[j.CheckName] = true
			out.Checks = append(out.Checks, apigen.Check{
				Name: j.CheckName, Workflow: run.WorkflowName, JobName: j.Name, RunNumber: run.Number, JobId: j.ID,
				Status: apigen.JobStatus(j.Status), Conclusion: conclusionPtr(j.Conclusion), Required: req[j.CheckName],
				StartedAt: j.StartedAt, CompletedAt: j.CompletedAt,
			})
		}
	}
	sort.SliceStable(out.Checks, func(i, k int) bool { return out.Checks[i].Name < out.Checks[k].Name })
	for _, r := range required {
		if !seen[r] {
			out.MissingRequired = append(out.MissingRequired, r)
		}
	}
	out.Total = len(out.Checks)
	for _, c := range out.Checks {
		switch {
		case c.Status != apigen.JobStatusCompleted:
			out.Pending++
		case c.Conclusion != nil && *c.Conclusion == apigen.ConclusionSuccess:
			out.Succeeded++
		case c.Conclusion != nil && *c.Conclusion == apigen.ConclusionSkipped:
			out.Skipped++
		default:
			out.Failed++
		}
	}
	switch {
	case out.Failed > 0:
		out.State = apigen.CheckSummaryStateFailure
	case out.Pending > 0 || len(out.MissingRequired) > 0:
		out.State = apigen.CheckSummaryStatePending
	case out.Total > 0:
		out.State = apigen.CheckSummaryStateSuccess
	}
	return out, nil
}

func (a *api) GetCommitChecks(ctx context.Context, req apigen.GetCommitChecksRequestObject) (apigen.GetCommitChecksResponseObject, error) {
	s := a.s
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	sha, err := s.repos.Open(repo).ResolveCommit(ctx, req.Sha)
	if err != nil {
		return nil, apierr.NotFound("commit %q not found", req.Sha)
	}
	out, err := s.commitChecks(ctx, repo, sha, nil)
	if err != nil {
		return nil, err
	}
	return apigen.GetCommitChecks200JSONResponse(out), nil
}

// requiredChecksFailing lists required checks that haven't passed.
func requiredChecksFailing(cs apigen.CheckSummary) (failing, pending []string) {
	for _, c := range cs.Checks {
		if !c.Required {
			continue
		}
		switch {
		case c.Status != apigen.JobStatusCompleted:
			pending = append(pending, c.Name)
		case c.Conclusion != nil && (*c.Conclusion == apigen.ConclusionSuccess || *c.Conclusion == apigen.ConclusionSkipped):
		default:
			failing = append(failing, c.Name)
		}
	}
	pending = append(pending, cs.MissingRequired...)
	return failing, pending
}
