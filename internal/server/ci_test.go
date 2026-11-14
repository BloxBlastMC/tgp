package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
)

func init() { fetchWait = 200 * time.Millisecond }

// ---- fake runner speaking the runner protocol ----

type fakeRunner struct {
	e     *testEnv
	id    int64
	token string
}

func (e *testEnv) registerRunner(admin *client, name string, capacity int, labels ...string) *fakeRunner {
	e.t.Helper()
	var tok apigen.RunnerRegistrationToken
	admin.mustDo("POST", "/admin/runners/registration-tokens", map[string]any{}, 201).decode(e.t, &tok)
	if !strings.Contains(tok.Command, "tgp-runner register --url https://git.example.com --token "+tok.Token) {
		e.t.Fatalf("command: %s", tok.Command)
	}
	r := &fakeRunner{e: e}
	var reg runnerapi.RegisterResponse
	res := r.post("/register", runnerapi.RegisterRequest{Token: tok.Token, Name: name, Labels: labels, Capacity: capacity})
	if res.status != 200 {
		e.t.Fatalf("register: %d %s", res.status, res.body)
	}
	res.decode(e.t, &reg)
	r.id, r.token = reg.ID, reg.Token
	return r
}

func (r *fakeRunner) post(path string, body any) resp {
	r.e.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", r.e.ts.URL+runnerapi.Prefix+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, body: data}
}

// fetch asks for a job; nil when none was assigned.
func (r *fakeRunner) fetch(running ...int64) *runnerapi.JobSpec {
	r.e.t.Helper()
	res := r.post("/fetch", runnerapi.FetchRequest{Running: running, Load: runnerapi.Load{CPUPercent: 10}})
	switch res.status {
	case 204:
		return nil
	case 200:
		var out runnerapi.FetchResponse
		res.decode(r.e.t, &out)
		return out.Job
	}
	r.e.t.Fatalf("fetch: %d %s", res.status, res.body)
	return nil
}

func (r *fakeRunner) mustFetch(running ...int64) *runnerapi.JobSpec {
	r.e.t.Helper()
	j := r.fetch(running...)
	if j == nil {
		r.e.t.Fatal("expected a job")
	}
	return j
}

func (r *fakeRunner) complete(id int64, conclusion string, outputs map[string]string) {
	r.e.t.Helper()
	if res := r.post(fmt.Sprintf("/jobs/%d/complete", id), runnerapi.CompleteRequest{Conclusion: conclusion, Outputs: outputs}); res.status != 204 {
		r.e.t.Fatalf("complete: %d %s", res.status, res.body)
	}
}

func (r *fakeRunner) heartbeat(running ...int64) runnerapi.HeartbeatResponse {
	r.e.t.Helper()
	var out runnerapi.HeartbeatResponse
	res := r.post("/heartbeat", runnerapi.HeartbeatRequest{Running: running})
	if res.status != 200 {
		r.e.t.Fatalf("heartbeat: %d %s", res.status, res.body)
	}
	res.decode(r.e.t, &out)
	return out
}

func (p *prEnv) run(n int64) apigen.WorkflowRunDetail {
	p.t.Helper()
	var r apigen.WorkflowRunDetail
	p.admin.mustDo("GET", "/repos/game/runs/"+itoa(n), nil, 200).decode(p.t, &r)
	return r
}

func runState(r apigen.WorkflowRunDetail) string {
	s := string(r.Status)
	if r.Conclusion != nil {
		s += "/" + string(*r.Conclusion)
	}
	return s
}

func (p *prEnv) runs() []apigen.WorkflowRun {
	p.t.Helper()
	var page apigen.RunPage
	p.admin.mustDo("GET", "/repos/game/runs?per_page=100", nil, 200).decode(p.t, &page)
	return page.Runs
}

func (p *prEnv) setCI(settings map[string]any) {
	p.t.Helper()
	p.admin.mustDo("PATCH", "/admin/ci/settings", settings, 200)
}

const quickWF = "name: CI\non: push\njobs:\n  test:\n    steps:\n      - run: make test\n"

// ---- tests ----

func TestCISupersedeQueueLimitAndSkip(t *testing.T) {
	p := newPREnv(t)
	p.commit("main", ".tgp/workflows/ci.yml", quickWF, "Add CI")
	if r := p.run(1); runState(r) != "queued" || r.Title != "Add CI" || r.Event != "push" || len(r.Jobs) != 1 {
		t.Fatalf("run 1: %s %+v", runState(r), r)
	}
	// A second push to a feature branch supersedes the first run there.
	p.commit("feature", "a.txt", "1", "Feature 1")
	p.commit("feature", "a.txt", "2", "Feature 2")
	if r := p.run(2); runState(r) != "completed/cancelled" || r.StatusReason == nil || *r.StatusReason != "Superseded by run #3" {
		t.Fatalf("run 2: %s %v", runState(r), r.StatusReason)
	}
	// The default branch isn't superseded.
	p.g.must(p.dir, "checkout", "-q", "main")
	p.commit("main", "b.txt", "1", "Main 2")
	if r := p.run(1); r.Status != "queued" {
		t.Fatalf("run 1 should stay queued: %s", runState(r))
	}
	// [skip ci] creates no run.
	p.commit("main", "c.txt", "1", "Docs only [skip ci]")
	if n := len(p.runs()); n != 4 {
		t.Fatalf("runs: %d", n)
	}
	// Queue limit 3: runs 1, 3, 4 are waiting.
	p.setCI(map[string]any{"max_queued_runs_per_repo": 3})
	p.commit("other", "d.txt", "1", "Other")
	r5 := p.run(5)
	if runState(r5) != "completed/cancelled" || r5.StatusReason == nil || !strings.Contains(*r5.StatusReason, "Rejected: 3 runs are already queued") {
		t.Fatalf("run 5: %s %v", runState(r5), r5.StatusReason)
	}
	// A new main run makes room by superseding the oldest main run.
	p.g.must(p.dir, "checkout", "-q", "main")
	p.commit("main", "e.txt", "1", "Main 3")
	if r := p.run(1); runState(r) != "completed/cancelled" || !strings.Contains(*r.StatusReason, "queue is full") {
		t.Fatalf("run 1: %s %v", runState(r), r.StatusReason)
	}
	if r := p.run(6); r.Status != "queued" {
		t.Fatalf("run 6: %s", runState(r))
	}
	// Filters.
	var page apigen.RunPage
	p.admin.mustDo("GET", "/repos/game/runs?branch=feature", nil, 200).decode(t, &page)
	if page.Total != 2 {
		t.Fatalf("branch filter: %d", page.Total)
	}
	p.admin.mustDo("GET", "/repos/game/runs?status=active", nil, 200).decode(t, &page)
	if page.Total != 3 {
		t.Fatalf("active filter: %d", page.Total)
	}
	p.admin.mustDo("GET", "/runs?status=cancelled", nil, 200).decode(t, &page)
	if page.Total != 3 || page.Runs[0].Repo != "game" {
		t.Fatalf("global list: %+v", page)
	}
}

func TestCIConcurrencyGroup(t *testing.T) {
	p := newPREnv(t)
	wf := "name: Deploy\non: push\nconcurrency: deploy-${{ github.ref_name }}\njobs:\n  deploy:\n    steps: [{run: ./deploy}]\n"
	p.commit("main", ".tgp/workflows/deploy.yml", wf, "Deploy 1")
	rn := p.registerRunner(p.admin, "r1", 4)
	j1 := rn.mustFetch()
	p.commit("main", "a", "1", "Deploy 2")
	p.commit("main", "a", "2", "Deploy 3")
	if r := p.run(2); runState(r) != "completed/cancelled" || !strings.Contains(*r.StatusReason, "Replaced by run #3") {
		t.Fatalf("run 2: %s %v", runState(r), r.StatusReason)
	}
	if r := p.run(3); r.Status != "waiting" {
		t.Fatalf("run 3: %s", runState(r))
	}
	if rn.fetch(j1.ID) != nil {
		t.Fatal("a waiting run's job was handed out")
	}
	rn.complete(j1.ID, "success", nil)
	if r := p.run(1); runState(r) != "completed/success" {
		t.Fatalf("run 1: %s", runState(r))
	}
	if r := p.run(3); r.Status != "queued" {
		t.Fatalf("run 3 released: %s", runState(r))
	}
	// cancel-in-progress cancels the running one.
	wf2 := "name: Deploy\non: push\nconcurrency:\n  group: deploy-${{ github.ref_name }}\n  cancel-in-progress: true\njobs:\n  deploy:\n    steps: [{run: ./deploy}]\n"
	j3 := rn.mustFetch()
	p.commit("main", ".tgp/workflows/deploy.yml", wf2, "Deploy 4")
	if hb := rn.heartbeat(j3.ID); len(hb.Cancel) != 1 || hb.Cancel[0] != j3.ID {
		t.Fatalf("heartbeat should cancel %d: %+v", j3.ID, hb)
	}
	rn.complete(j3.ID, "cancelled", nil)
	if r := p.run(3); runState(r) != "completed/cancelled" || !strings.Contains(*r.StatusReason, "Cancelled by run #4") {
		t.Fatalf("run 3: %s %v", runState(r), r.StatusReason)
	}
	if r := p.run(4); r.Status != "queued" {
		t.Fatalf("run 4: %s", runState(r))
	}
}

func TestCISchedulingLimitsAndLabels(t *testing.T) {
	p := newPREnv(t)
	p.setCI(map[string]any{"max_jobs_per_repo": 1})
	wf := `name: CI
on: push
jobs:
  a: {steps: [{run: a}]}
  b: {steps: [{run: b}]}
  gpu:
    runs-on: gpu
    resources: {cpus: 1, memory: 512m}
    timeout-minutes: 5
    steps: [{run: c}]
`
	p.commit("main", ".tgp/workflows/ci.yml", wf, "CI")
	rn := p.registerRunner(p.admin, "cpu", 5, "linux")
	j1 := rn.mustFetch()
	if j := rn.fetch(j1.ID); j != nil {
		t.Fatalf("repo limit ignored: got %s", j.Name)
	}
	r := p.run(1)
	for _, j := range r.Jobs {
		if j.Status == "queued" && (j.WaitReason == nil || !strings.Contains(*j.WaitReason, "limit 1")) {
			t.Fatalf("%s wait reason: %v", j.Name, j.WaitReason)
		}
	}
	rn.complete(j1.ID, "success", nil)
	j2 := rn.mustFetch()
	rn.complete(j2.ID, "success", nil)
	// Only the gpu job is left, and this runner lacks the label.
	if j := rn.fetch(); j != nil {
		t.Fatalf("label mismatch: got %s", j.Name)
	}
	gpu := p.registerRunner(p.admin, "gpu-box", 1, "linux", "GPU")
	j3 := gpu.mustFetch()
	if j3.JobKey != "gpu" || j3.Resources.CPUs != 1 || j3.Resources.MemoryBytes != 512<<20 || j3.TimeoutSeconds != 300 {
		t.Fatalf("gpu spec: %+v", j3)
	}
	if j3.Git.URL != "/game.git" || j3.Git.SHA != r.HeadSha || !strings.HasPrefix(j3.Git.Token, runnerapi.JobTokenPrefix) {
		t.Fatalf("git source: %+v", j3.Git)
	}
	if j3.Env["GITHUB_REPOSITORY"] != "game" || j3.Env["TGP_RUN_NUMBER"] != "1" || j3.Env["GITHUB_REF_NAME"] != "main" {
		t.Fatalf("env: %v", j3.Env)
	}
	// The admin view shows the busy runner and its job.
	var list apigen.RunnerList
	p.admin.mustDo("GET", "/admin/runners", nil, 200).decode(t, &list)
	if list.RunningJobs != 1 || len(list.Runners) != 2 {
		t.Fatalf("runners: %+v", list)
	}
	for _, rr := range list.Runners {
		if rr.Name == "gpu-box" && (rr.Busy != 1 || rr.Jobs[0].Name != "gpu" || rr.Status != apigen.Online) {
			t.Fatalf("gpu-box: %+v", rr)
		}
	}
	// Requests above the instance maximum fail the run up front.
	p.commit("main", ".tgp/workflows/big.yml", "on: push\njobs:\n  big:\n    resources: {cpus: 64}\n    steps: [{run: x}]\n", "Big")
	for _, run := range p.runs() {
		if run.WorkflowName == "big.yml" && (run.StatusReason == nil || !strings.Contains(*run.StatusReason, "asks for 64 CPUs; this instance allows at most 4")) {
			t.Fatalf("big: %+v", run.StatusReason)
		}
	}
	// Disabled runners get nothing; deleted runners are rejected.
	gpu.complete(j3.ID, "success", nil)
	var rinfo apigen.Runner
	p.admin.mustDo("PATCH", "/admin/runners/"+itoa(rn.id), map[string]any{"disabled": true, "name": "cpu-1"}, 200).decode(t, &rinfo)
	if rinfo.Status != apigen.Disabled || rinfo.Name != "cpu-1" {
		t.Fatalf("disable: %+v", rinfo)
	}
	p.commit("main", "x", "1", "More")
	if j := rn.fetch(); j != nil {
		t.Fatal("disabled runner got a job")
	}
	p.admin.mustDo("DELETE", "/admin/runners/"+itoa(rn.id), nil, 204)
	if res := rn.post("/fetch", runnerapi.FetchRequest{}); res.status != 401 {
		t.Fatalf("deleted runner: %d", res.status)
	}
}

func TestCIJanitor(t *testing.T) {
	p := newPREnv(t)
	clock := time.Now().UTC()
	p.srv.ci.now = func() time.Time { return clock }
	wf := "name: CI\non: push\njobs:\n  lost: {steps: [{run: a}]}\n  slow:\n    timeout-minutes: 1\n    steps: [{run: b}]\n  stuck:\n    runs-on: nowhere\n    steps: [{run: c}]\n"
	p.commit("main", ".tgp/workflows/ci.yml", wf, "CI")
	gone := p.registerRunner(p.admin, "gone", 1)
	rn := p.registerRunner(p.admin, "r1", 5)
	a, b := gone.mustFetch(), rn.mustFetch()
	lost, slow := a, b
	if a.JobKey == "slow" {
		t.Fatalf("unexpected order: %s first", a.JobKey)
	}
	ctx := t.Context()
	// 61s later: only the slow job's runner kept heartbeating.
	clock = clock.Add(61 * time.Second)
	rn.heartbeat(slow.ID)
	if err := p.srv.ciJanitor(ctx); err != nil {
		t.Fatal(err)
	}
	r := p.run(1)
	for _, j := range r.Jobs {
		switch j.Key {
		case "lost":
			if *j.Conclusion != "failure" || !strings.Contains(*j.FailureReason, "stopped responding") {
				t.Fatalf("lost: %v %v", j.Conclusion, j.FailureReason)
			}
		case "slow":
			if j.Status != "in_progress" {
				t.Fatalf("slow: %s", j.Status)
			}
		}
	}
	// Past its timeout the slow job is asked to stop; the runner reports
	// cancelled and the server records it as timed out.
	if hb := rn.heartbeat(slow.ID); len(hb.Cancel) != 1 {
		t.Fatalf("timeout cancel: %+v", hb)
	}
	rn.complete(slow.ID, "cancelled", nil)
	// The stuck job is cancelled after 24 hours in the queue.
	clock = clock.Add(25 * time.Hour)
	if err := p.srv.ciJanitor(ctx); err != nil {
		t.Fatal(err)
	}
	r = p.run(1)
	got := map[string]string{}
	for _, j := range r.Jobs {
		got[j.Key] = string(*j.Conclusion)
	}
	if got["slow"] != "timed_out" || got["stuck"] != "cancelled" || runState(r) != "completed/failure" {
		t.Fatalf("conclusions: %v run %s", got, runState(r))
	}
	if res := gone.post(fmt.Sprintf("/jobs/%d/logs", lost.ID), runnerapi.LogRequest{First: 1, Lines: []runnerapi.LogLine{{Text: "late"}}}); res.status != 409 {
		t.Fatalf("logs for a finished job: %d", res.status)
	}
	// A runner that comes back without the job it had makes the server
	// fail it; unknown jobs are reported back.
	p.commit("main", "z", "1", "Again")
	j := rn.mustFetch()
	for j.JobKey != "lost" {
		rn.complete(j.ID, "success", nil)
		j = rn.mustFetch()
	}
	clock = clock.Add(time.Minute)
	if hb := rn.heartbeat(999); len(hb.Unknown) != 1 || hb.Unknown[0] != 999 {
		t.Fatalf("unknown: %+v", hb)
	}
	var job apigen.Job
	p.admin.mustDo("GET", "/repos/game/jobs/"+itoa(j.ID), nil, 200).decode(t, &job)
	if job.Conclusion == nil || !strings.Contains(*job.FailureReason, "lost the job") {
		t.Fatalf("restarted runner: %+v", job)
	}
}

func TestCINeedsOutputsFailFastAndRerun(t *testing.T) {
	p := newPREnv(t)
	wf := `name: CI
on: push
jobs:
  build:
    outputs: {v: "${{ steps.x.outputs.v }}"}
    steps: [{id: x, run: build}]
  test:
    needs: build
    strategy:
      matrix: {n: [1, 2, 3]}
    steps: [{run: "test ${{ needs.build.outputs.v }}"}]
  report:
    needs: test
    if: always()
    steps: [{run: report}]
`
	p.commit("main", ".tgp/workflows/ci.yml", wf, "CI")
	rn := p.registerRunner(p.admin, "r1", 10)
	b := rn.mustFetch()
	if rn.fetch(b.ID) != nil {
		t.Fatal("test started before build finished")
	}
	rn.complete(b.ID, "success", map[string]string{"v": "1.2.3"})
	t1 := rn.mustFetch()
	needs := t1.Contexts["needs"].(map[string]any)["build"].(map[string]any)
	if needs["result"] != "success" || needs["outputs"].(map[string]any)["v"] != "1.2.3" {
		t.Fatalf("needs: %v", needs)
	}
	t2 := rn.mustFetch(t1.ID)
	// One matrix job fails: fail-fast cancels its siblings.
	rn.complete(t1.ID, "failure", nil)
	if hb := rn.heartbeat(t2.ID); len(hb.Cancel) != 1 || hb.Cancel[0] != t2.ID {
		t.Fatalf("fail-fast cancel: %+v", hb)
	}
	rn.complete(t2.ID, "cancelled", nil)
	rep := rn.mustFetch()
	if rep.JobKey != "report" {
		t.Fatalf("always() job: %s", rep.JobKey)
	}
	rn.complete(rep.ID, "success", nil)
	r := p.run(1)
	if runState(r) != "completed/failure" {
		t.Fatalf("run: %s", runState(r))
	}
	// A tester can re-run failed jobs; build is reused with its outputs.
	tester := p.tom
	var rr apigen.WorkflowRunDetail
	tester.mustDo("POST", "/repos/game/runs/1/rerun", map[string]any{"failed_only": true}, 200).decode(t, &rr)
	if rr.Attempt != 2 || rr.Status != "queued" {
		t.Fatalf("rerun: %s attempt %d", runState(rr), rr.Attempt)
	}
	for _, j := range rr.Jobs {
		if j.Key == "build" && (j.Conclusion == nil || *j.Conclusion != "success") {
			t.Fatalf("build should be reused: %+v", j)
		}
	}
	nt := rn.mustFetch()
	if nt.JobKey != "test" || nt.Contexts["needs"].(map[string]any)["build"].(map[string]any)["outputs"].(map[string]any)["v"] != "1.2.3" {
		t.Fatalf("rerun needs: %v", nt.Contexts["needs"])
	}
	// Testers can't cancel; maintainers can.
	tester.mustDo("POST", "/repos/game/runs/1/cancel", nil, 403)
	p.mia.mustDo("POST", "/repos/game/runs/1/cancel", nil, 200)
	rn.complete(nt.ID, "cancelled", nil)
	if r := p.run(1); runState(r) != "completed/cancelled" || !r.CanRerun || r.CanCancel {
		t.Fatalf("cancelled: %s rerun=%v cancel=%v", runState(r), r.CanRerun, r.CanCancel)
	}
	p.mia.mustDo("POST", "/repos/game/runs/1/cancel", nil, 409)
}

func TestCIPullRequestChecks(t *testing.T) {
	p := newPREnv(t)
	wf := "name: CI\non:\n  pull_request:\n    branches: [main]\njobs:\n  test: {steps: [{run: t}]}\n  lint: {steps: [{run: l}]}\n"
	p.commit("main", ".tgp/workflows/ci.yml", wf, "CI")
	if n := len(p.runs()); n != 0 {
		t.Fatalf("push shouldn't run a pull_request workflow: %d", n)
	}
	p.admin.mustDo("POST", "/repos/game/protections", map[string]any{"pattern": "main", "required_approvals": 0, "required_checks": []string{"CI / test", "CI / test", " "}}, 201)
	p.commit("feature", "f.txt", "1", "Feature")
	p.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "Feature", "head": "feature"}, 201)
	r := p.run(1)
	if r.Event != "pull_request" || r.Ref != "refs/pull/1/head" || r.PullNumber == nil || *r.PullNumber != 1 || r.Title != "Feature" {
		t.Fatalf("PR run: %+v", r)
	}
	pr := p.pull(p.mia, 1)
	if pr.Checks.State != "pending" || pr.Checks.Total != 2 || blockerCodes(pr) != "checks" {
		t.Fatalf("pending checks: %+v blockers %s", pr.Checks, blockerCodes(pr))
	}
	if pr.Protection == nil || len(pr.Protection.RequiredChecks) != 1 {
		t.Fatalf("protection checks: %+v", pr.Protection)
	}
	rn := p.registerRunner(p.admin, "r1", 2)
	a, b := rn.mustFetch(), rn.mustFetch()
	test, lint := a, b
	if a.JobKey != "test" {
		test, lint = b, a
	}
	if test.Contexts["github"].(map[string]any)["head_ref"] != "feature" || test.Env["GITHUB_EVENT_NAME"] != "pull_request" {
		t.Fatalf("PR context: %v", test.Contexts["github"])
	}
	rn.complete(lint.ID, "failure", nil) // not required: doesn't block
	rn.complete(test.ID, "failure", nil)
	pr = p.pull(p.mia, 1)
	if pr.Checks.State != "failure" || !strings.Contains(pr.Blockers[0].Message, "failing: CI / test") {
		t.Fatalf("failed check: %+v %+v", pr.Checks, pr.Blockers)
	}
	p.mia.mustDo("POST", "/repos/game/pulls/1/merge", map[string]any{}, 409)
	// A new push to the PR runs again (pull_request synchronize).
	p.commit("feature", "f.txt", "2", "Fix")
	t2 := rn.mustFetch()
	l2 := rn.mustFetch(t2.ID)
	rn.complete(t2.ID, "success", nil)
	rn.complete(l2.ID, "failure", nil)
	pr = p.pull(p.mia, 1)
	if len(pr.Blockers) != 0 || pr.Checks.State != "failure" || pr.Checks.Succeeded != 1 {
		t.Fatalf("after fix: %+v blockers %s", pr.Checks, blockerCodes(pr))
	}
	p.mia.mustDo("POST", "/repos/game/pulls/1/merge", map[string]any{}, 200)
	// The commit checks endpoint and missing required checks.
	var cs apigen.CheckSummary
	p.admin.mustDo("GET", "/repos/game/commits/main/checks", nil, 200).decode(t, &cs)
	if cs.State != "none" {
		t.Fatalf("main has no runs: %+v", cs)
	}
}

func TestCIDispatchSecretsAndWorkflows(t *testing.T) {
	p := newPREnv(t)
	wf := `name: Deploy
on:
  workflow_dispatch:
    inputs:
      env: {type: choice, options: [staging, prod], required: true}
      dry: {type: boolean, default: "true"}
jobs:
  deploy:
    steps:
      - run: deploy ${{ inputs.env }} ${{ secrets.TOKEN }}
`
	p.commit("main", ".tgp/workflows/deploy.yml", wf, "Deploy wf")
	p.commit("main", ".tgp/workflows/broken.yml", "on: push\njobs:\n  x:\n    stepz: []\n", "Broken wf")
	// The broken file fails a run on push, with the error.
	runs := p.runs()
	if len(runs) != 1 || runs[0].Conclusion == nil || *runs[0].Conclusion != "failure" || !strings.Contains(*runs[0].StatusReason, `unknown key "stepz"`) {
		t.Fatalf("broken run: %+v", runs)
	}
	var wfs []apigen.Workflow
	p.tom.mustDo("GET", "/repos/game/workflows", nil, 200).decode(t, &wfs)
	if len(wfs) != 2 || wfs[0].Valid || wfs[0].Errors[0].Line == nil || !wfs[1].Valid || len(wfs[1].DispatchInputs) != 2 {
		t.Fatalf("workflows: %+v", wfs)
	}

	// Secrets: org and repo scope, names normalised, values never returned.
	p.admin.mustDo("PUT", "/admin/secrets/token", map[string]any{"value": "org-value"}, 200)
	p.admin.mustDo("PUT", "/admin/secrets/SHARED", map[string]any{"value": "shared"}, 200)
	var sec apigen.Secret
	p.mia.mustDo("PUT", "/repos/game/secrets/Token", map[string]any{"value": "repo-value"}, 200).decode(t, &sec)
	if sec.Name != "TOKEN" || sec.Scope != "repo" || sec.UpdatedBy == nil || sec.UpdatedBy.Username != "mia" {
		t.Fatalf("secret: %+v", sec)
	}
	for _, bad := range []string{"1X", "TGP_X", "GITHUB_TOKEN", "a-b"} {
		if r := p.mia.do("PUT", "/repos/game/secrets/"+bad, map[string]any{"value": "v"}); r.status != 422 {
			t.Errorf("secret name %q: %d", bad, r.status)
		}
	}
	r := p.mia.mustDo("GET", "/repos/game/secrets", nil, 200)
	if strings.Contains(string(r.body), "repo-value") {
		t.Fatal("secret value returned")
	}
	p.tom.mustDo("GET", "/repos/game/secrets", nil, 403)
	p.mia.mustDo("GET", "/admin/secrets", nil, 403)

	// Dispatch: testers can't; inputs are validated.
	p.tom.mustDo("POST", "/repos/game/workflows/deploy.yml/dispatch", map[string]any{"inputs": map[string]string{"env": "prod"}}, 403)
	for _, in := range []map[string]string{{}, {"env": "dev"}, {"env": "prod", "dry": "maybe"}, {"env": "prod", "nope": "1"}} {
		if r := p.mia.do("POST", "/repos/game/workflows/deploy.yml/dispatch", map[string]any{"inputs": in}); r.status != 422 {
			t.Errorf("inputs %v: %d %s", in, r.status, r.body)
		}
	}
	p.mia.mustDo("POST", "/repos/game/workflows/broken.yml/dispatch", map[string]any{}, 422)
	p.mia.mustDo("POST", "/repos/game/workflows/missing.yml/dispatch", map[string]any{}, 404)
	var run apigen.WorkflowRunDetail
	p.mia.mustDo("POST", "/repos/game/workflows/deploy.yml/dispatch", map[string]any{"ref": "main", "inputs": map[string]string{"env": "prod"}}, 201).decode(t, &run)
	if run.Event != "workflow_dispatch" || run.Inputs["env"] != "prod" || run.Inputs["dry"] != "true" || run.Actor.Username != "mia" {
		t.Fatalf("dispatched: %+v", run)
	}
	rn := p.registerRunner(p.admin, "r1", 1)
	j := rn.mustFetch()
	// Only referenced secrets are sent, repo values override org values.
	if len(j.Secrets) != 1 || j.Secrets["TOKEN"] != "repo-value" {
		t.Fatalf("secrets: %v", j.Secrets)
	}
	if j.Contexts["inputs"].(map[string]any)["env"] != "prod" {
		t.Fatalf("inputs: %v", j.Contexts["inputs"])
	}

	// The job token clones this repository, read-only, while the job runs.
	g := p.g
	dir := t.TempDir()
	cloneURL := strings.Replace(g.url("game", ""), "http://", "http://"+runnerapi.JobTokenUser+":"+j.Git.Token+"@", 1)
	g.must(dir, "clone", "-q", cloneURL, "c")
	g.must(dir+"/c", "fetch", "-q", "--depth=1", "origin", j.Git.SHA)
	if out, err := g.run(dir+"/c", "push", "origin", "HEAD:refs/heads/evil"); err == nil {
		t.Fatalf("job token pushed: %s", out)
	}
	p.admin.mustDo("POST", "/repos", map[string]any{"name": "other", "auto_init": true}, 201)
	if _, err := g.run(dir, "clone", "-q", strings.Replace(cloneURL, "/game.git", "/other.git", 1), "o"); err == nil {
		t.Fatal("job token cloned another repo")
	}
	rn.complete(j.ID, "success", nil)
	if _, err := g.run(dir, "clone", "-q", cloneURL, "after"); err == nil {
		t.Fatal("job token works after the job")
	}
	p.mia.mustDo("DELETE", "/repos/game/secrets/token", nil, 204)
	p.mia.mustDo("DELETE", "/repos/game/secrets/token", nil, 404)
}

func TestCILogsAndStream(t *testing.T) {
	p := newPREnv(t)
	p.setCI(map[string]any{"max_log_mb": 1})
	p.commit("main", ".tgp/workflows/ci.yml", quickWF, "CI")
	rn := p.registerRunner(p.admin, "r1", 1)
	j := rn.mustFetch()
	lines := func(step int, texts ...string) []runnerapi.LogLine {
		var out []runnerapi.LogLine
		for _, s := range texts {
			out = append(out, runnerapi.LogLine{Step: step, T: time.Now(), Text: s})
		}
		return out
	}
	path := fmt.Sprintf("/jobs/%d/logs", j.ID)
	var lr runnerapi.LogResponse
	rn.post(path, runnerapi.LogRequest{First: 1, Lines: lines(0, "setup a", "setup b")}).decode(t, &lr)
	if lr.Next != 3 {
		t.Fatalf("next: %+v", lr)
	}
	// A retried upload overlapping what the server has is deduplicated.
	rn.post(path, runnerapi.LogRequest{First: 2, Lines: lines(1, "setup b", "step 1 \x1b[31mred\x1b[0m", "multi\nline")}).decode(t, &lr)
	if lr.Next != 5 {
		t.Fatalf("next after overlap: %+v", lr)
	}
	if res := rn.post(path, runnerapi.LogRequest{First: 9, Lines: lines(1, "gap")}); res.status != 409 || !strings.Contains(string(res.body), `"next":5`) {
		t.Fatalf("gap: %d %s", res.status, res.body)
	}
	var logs apigen.JobLogs
	p.tom.mustDo("GET", fmt.Sprintf("/repos/game/jobs/%d/logs", j.ID), nil, 200).decode(t, &logs)
	if len(logs.Lines) != 4 || logs.Lines[3].Text != "multi line" || logs.Lines[2].Step != 1 || logs.Complete {
		t.Fatalf("logs: %+v", logs)
	}
	p.tom.mustDo("GET", fmt.Sprintf("/repos/game/jobs/%d/logs?after=2&step=1", j.ID), nil, 200).decode(t, &logs)
	if len(logs.Lines) != 2 || logs.Lines[0].N != 3 || logs.Next != 4 {
		t.Fatalf("after+step: %+v", logs)
	}
	// The size cap truncates with a notice.
	big := strings.Repeat("x", 60<<10)
	var many []string
	for i := 0; i < 20; i++ {
		many = append(many, big)
	}
	rn.post(path, runnerapi.LogRequest{First: 5, Lines: lines(1, many...)}).decode(t, &lr)
	if !lr.Truncated {
		t.Fatalf("not truncated: %+v", lr)
	}
	rn.post(path, runnerapi.LogRequest{First: 25, Lines: lines(1, "dropped")}).decode(t, &lr)
	if !lr.Truncated {
		t.Fatal("later lines should be dropped")
	}
	// Step updates and completion.
	now := time.Now()
	if res := rn.post(fmt.Sprintf("/jobs/%d/update", j.ID), runnerapi.UpdateRequest{Steps: []runnerapi.StepUpdate{
		{Number: 0, Status: "completed", Conclusion: "success", CompletedAt: &now},
		{Number: 1, Status: "in_progress", StartedAt: &now},
	}}); res.status != 200 {
		t.Fatalf("update: %d %s", res.status, res.body)
	}
	rn.complete(j.ID, "success", nil)
	var job apigen.Job
	p.admin.mustDo("GET", fmt.Sprintf("/repos/game/jobs/%d", j.ID), nil, 200).decode(t, &job)
	if job.Steps[0].Name != "Set up job" || *job.Steps[1].Conclusion != "success" || job.Steps[1].Name != "Run make test" {
		t.Fatalf("steps: %+v", job.Steps)
	}
	p.admin.mustDo("GET", fmt.Sprintf("/repos/game/jobs/%d/logs?limit=20000", j.ID), nil, 200).decode(t, &logs)
	last := logs.Lines[len(logs.Lines)-1].Text
	if !logs.Complete || !logs.Truncated || !strings.Contains(last, "1 MB limit") {
		t.Fatalf("truncated logs: complete=%v truncated=%v last=%q", logs.Complete, logs.Truncated, last)
	}

	// The stream sends every line, then "end", then closes.
	req, _ := http.NewRequest("GET", p.ts.URL+fmt.Sprintf("/api/v1/repos/game/jobs/%d/logs/stream?after=3", j.ID), nil)
	req.Header.Set("Authorization", "Bearer "+p.miaToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	events := map[string]int{}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		if ev, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
			events[ev]++
		}
	}
	if events["line"] != len(logs.Lines)-3 || events["end"] != 1 {
		t.Fatalf("events: %v (lines %d)", events, len(logs.Lines))
	}
	// Streams need repo access.
	p.admin.mustDo("POST", "/repos", map[string]any{"name": "secret-repo"}, 201)
	outsider := p.activeUser(p.admin, "olly", "tester", "ollys-password-1")
	r := outsider.do("GET", "/repos/secret-repo/runs", nil)
	if r.status != 404 {
		t.Fatalf("outsider: %d", r.status)
	}
}

func TestRunnerRegistrationTokens(t *testing.T) {
	p := newPREnv(t)
	var tok apigen.RunnerRegistrationToken
	p.admin.mustDo("POST", "/admin/runners/registration-tokens", map[string]any{"expires_in_hours": 1}, 201).decode(t, &tok)
	r := &fakeRunner{e: p.testEnv}
	if res := r.post("/register", runnerapi.RegisterRequest{Token: tok.Token, Name: "a", Capacity: 2}); res.status != 200 {
		t.Fatalf("register: %d %s", res.status, res.body)
	}
	if res := r.post("/register", runnerapi.RegisterRequest{Token: tok.Token, Name: "b"}); res.status != 401 || !strings.Contains(string(res.body), "already used") {
		t.Fatalf("reuse: %d %s", res.status, res.body)
	}
	p.admin.mustDo("POST", "/admin/runners/registration-tokens", map[string]any{}, 201).decode(t, &tok)
	p.srv.ci.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if res := r.post("/register", runnerapi.RegisterRequest{Token: tok.Token, Name: "c"}); res.status != 401 || !strings.Contains(string(res.body), "expired") {
		t.Fatalf("expired: %d %s", res.status, res.body)
	}
	p.srv.ci.now = nil
	p.mia.mustDo("POST", "/admin/runners/registration-tokens", map[string]any{}, 403)
	p.mia.mustDo("GET", "/admin/runners", nil, 403)
	p.mia.mustDo("GET", "/admin/ci/settings", nil, 403)
	if r := p.admin.do("PATCH", "/admin/ci/settings", map[string]any{"default_cpus": 8}); r.status != 422 {
		t.Fatalf("default above max: %d", r.status)
	}
	var cs apigen.CISettings
	p.admin.mustDo("PATCH", "/admin/ci/settings", map[string]any{"max_cpus": 16, "default_cpus": 8, "default_image": "node:22"}, 200).decode(t, &cs)
	if cs.DefaultCpus != 8 || cs.DefaultImage != "node:22" || cs.MaxJobsPerRepo != 4 {
		t.Fatalf("settings: %+v", cs)
	}
	var audit struct {
		Entries []struct{ Action string } `json:"entries"`
	}
	p.admin.mustDo("GET", "/admin/audit-log?per_page=50", nil, 200).decode(t, &audit)
	seen := map[string]bool{}
	for _, e := range audit.Entries {
		seen[e.Action] = true
	}
	for _, a := range []string{"runner.token_create", "runner.register", "ci.settings_update"} {
		if !seen[a] {
			t.Errorf("audit lacks %s", a)
		}
	}
}
