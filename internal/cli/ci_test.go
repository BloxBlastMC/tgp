package cli

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/runner"
)

func TestCICommands(t *testing.T) {
	e := newCLIEnv(t)
	e.admin.Do(http.MethodPost, "/repos", map[string]any{"name": "game", "auto_init": true}, nil)
	e.admin.ActivateUser("tom", "tester", "tom-password-1", true)
	tomTok := e.srv.Login("tom", "tom-password-1").Token("read", "write")
	e.login("admin", "correct horse battery")
	R := []string{"-R", "game"}
	with := func(args ...string) []string { return append(args, R...) }

	// Runner token and registration.
	var tok map[string]any
	e.jsonOK(&tok, "admin", "runner", "token", "--expires-in", "2")
	ctx := context.Background()
	reg, err := runner.NewClient(e.srv.URL, "").Register(ctx, runnerapi.RegisterRequest{Token: tok["token"].(string), Name: "r1", Labels: []string{"linux"}, Capacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	rc := runner.NewClient(e.srv.URL, reg.Token)
	var runners map[string]any
	e.jsonOK(&runners, "admin", "runner", "list")
	if len(runners["runners"].([]any)) != 1 {
		t.Fatalf("runners: %v", runners)
	}

	// Secrets: value from stdin, never shown.
	if r := e.ok("s3cret-value\n", with("secret", "set", "deploy_key")...); !strings.Contains(r.stdout, "Set repo secret DEPLOY_KEY") {
		t.Fatalf("secret set: %s", r.stdout)
	}
	var secrets []map[string]any
	e.jsonOK(&secrets, with("secret", "list")...)
	if len(secrets) != 1 || secrets[0]["name"] != "DEPLOY_KEY" {
		t.Fatalf("secrets: %v", secrets)
	}
	e.fail(ExitUsage, CodeUsage, with("secret", "set", "X", "--json")...) // empty stdin

	// Push workflows.
	e.ok("", "repo", "clone", "game")
	dir := filepath.Join(e.home, "game")
	t.Chdir(dir)
	_ = os.MkdirAll(filepath.Join(dir, ".tgp/workflows"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".tgp/workflows/ci.yml"), []byte("name: CI\non: [push, pull_request]\njobs:\n  test:\n    steps:\n      - run: make test\n      - run: make lint\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".tgp/workflows/deploy.yml"), []byte("name: Deploy\non:\n  workflow_dispatch:\n    inputs:\n      env: {type: choice, options: [staging, prod], required: true}\njobs:\n  deploy:\n    steps: [{run: deploy}]\n"), 0o644)
	e.git(dir, "add", ".")
	e.git(dir, "commit", "-qm", "Add CI")
	e.git(dir, "push", "-q", "origin", "main")

	var wfs []map[string]any
	e.jsonOK(&wfs, "workflow", "list")
	if len(wfs) != 2 || wfs[1]["name"] != "Deploy" {
		t.Fatalf("workflows: %v", wfs)
	}
	var runs []map[string]any
	e.jsonOK(&runs, "run", "list")
	if len(runs) != 1 || runs[0]["status"] != "queued" {
		t.Fatalf("runs: %v", runs)
	}

	// The runner takes the job, logs and fails the second step.
	job, err := rc.Fetch(ctx, runnerapi.FetchRequest{Capacity: 2})
	if err != nil || job == nil {
		t.Fatalf("fetch: %v %v", job, err)
	}
	now := time.Now()
	lines := []runnerapi.LogLine{{Step: 0, T: now, Text: "setting up"}, {Step: 1, T: now, Text: "tests ok"}, {Step: 2, T: now, Text: "lint: bad.go:3: unused"}, {Step: 2, T: now, Text: "exit 1"}}
	if _, err := rc.Logs(ctx, job.ID, runnerapi.LogRequest{First: 1, Lines: lines}); err != nil {
		t.Fatal(err)
	}
	done := func(n int, c string) runnerapi.StepUpdate {
		return runnerapi.StepUpdate{Number: n, Status: "completed", Conclusion: c, StartedAt: &now, CompletedAt: &now}
	}
	if err := rc.Complete(ctx, job.ID, runnerapi.CompleteRequest{Conclusion: "failure", Steps: []runnerapi.StepUpdate{done(0, "success"), done(1, "success"), done(2, "failure")}}); err != nil {
		t.Fatal(err)
	}

	var run map[string]any
	e.jsonOK(&run, "run", "view")
	if run["number"] != float64(1) || run["conclusion"] != "failure" {
		t.Fatalf("run view: %v", run)
	}
	if r := e.ok("", "run", "view", "1"); !strings.Contains(r.stdout, "tgp run logs 1 --failed") || !strings.Contains(r.stdout, "Run make lint") {
		t.Fatalf("human view: %s", r.stdout)
	}
	var logs []map[string]any
	e.jsonOK(&logs, "run", "logs", "1", "--failed")
	if len(logs) != 2 || logs[0]["text"] != "lint: bad.go:3: unused" || logs[0]["step_name"] != "Run make lint" {
		t.Fatalf("failed logs: %v", logs)
	}
	e.jsonOK(&logs, "run", "logs", "1", "--tail", "1")
	if len(logs) != 1 || logs[0]["text"] != "exit 1" {
		t.Fatalf("tail: %v", logs)
	}
	if r := e.run("", "run", "watch", "1", "--exit-status"); r.code != ExitError || !strings.Contains(r.stdout, "failure") {
		t.Fatalf("watch: %d %s %s", r.code, r.stdout, r.stderr)
	}

	// A tester re-runs the failed job; a tester can't cancel.
	e.as(tomTok, func() {
		e.jsonOK(&run, "run", "rerun", "1", "--failed")
		if run["attempt"] != float64(2) {
			t.Fatalf("rerun: %v", run)
		}
		e.fail(ExitForbidden, "forbidden", "run", "cancel", "1", "--json")
		e.fail(ExitForbidden, "forbidden", "workflow", "run", "deploy.yml", "-f", "env=prod", "--json")
	})
	e.jsonOK(&run, "run", "cancel", "1")
	e.jsonOK(&run, "run", "view", "1")
	if run["conclusion"] != "cancelled" {
		t.Fatalf("cancelled: %v", run)
	}
	e.fail(ExitConflict, "conflict", "run", "cancel", "1", "--json")

	// Manual runs.
	e.fail(ExitConflict, "validation_failed", "workflow", "run", "deploy.yml", "-f", "env=dev", "--json")
	e.jsonOK(&run, "workflow", "run", "deploy.yml", "-f", "env=prod")
	if run["event"] != "workflow_dispatch" || run["inputs"].(map[string]any)["env"] != "prod" {
		t.Fatalf("dispatch: %v", run)
	}

	// Pull request checks: pending (exit 8), then failing (exit 1).
	e.git(dir, "checkout", "-q", "-b", "fix")
	_ = os.WriteFile(filepath.Join(dir, "fix.txt"), []byte("x\n"), 0o644)
	e.git(dir, "add", ".")
	e.git(dir, "commit", "-qm", "Fix lint")
	e.git(dir, "push", "-q", "-u", "origin", "fix")
	e.jsonOK(nil, "pr", "create", "--fill")
	r := e.run("", "pr", "checks", "--json")
	if r.code != ExitPending || !strings.Contains(r.stdout, `"state":"pending"`) {
		t.Fatalf("pending checks: %d %s %s", r.code, r.stdout, r.stderr)
	}
	validateOutput(t, []string{"pr", "checks", "--json"}, r.stdout)
	for range 3 { // the manual deploy run, the branch push run and the pull request run
		j, err := rc.Fetch(ctx, runnerapi.FetchRequest{Capacity: 2})
		if err != nil || j == nil {
			t.Fatalf("fetch: %v %v", j, err)
		}
		if err := rc.Complete(ctx, j.ID, runnerapi.CompleteRequest{Conclusion: "failure"}); err != nil {
			t.Fatal(err)
		}
	}
	if r := e.run("", "pr", "checks"); r.code != ExitError || !strings.Contains(r.stdout, "failure") {
		t.Fatalf("failing checks: %d %s %s", r.code, r.stdout, r.stderr)
	}

	// Admin CI settings and runner management.
	var cs map[string]any
	e.jsonOK(&cs, "admin", "ci", "set", "max_jobs_per_repo=2", "default_cpus=1.5", "cancel_superseded=false")
	if cs["max_jobs_per_repo"] != float64(2) || cs["default_cpus"] != 1.5 || cs["cancel_superseded"] != false {
		t.Fatalf("settings: %v", cs)
	}
	e.fail(ExitUsage, CodeUsage, "admin", "ci", "set", "nope=1", "--json")
	e.fail(ExitConflict, "validation_failed", "admin", "ci", "set", "default_cpus=99", "--json")
	var rn map[string]any
	e.jsonOK(&rn, "admin", "runner", "edit", "1", "--disable", "--name", "builder")
	if rn["status"] != "disabled" || rn["name"] != "builder" {
		t.Fatalf("runner edit: %v", rn)
	}
	e.as(tomTok, func() {
		e.fail(ExitForbidden, "forbidden", "admin", "runner", "list", "--json")
	})
	e.jsonOK(nil, "admin", "runner", "delete", "1", "--yes")
	if _, err := rc.Fetch(ctx, runnerapi.FetchRequest{}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("deleted runner: %v", err)
	}
	e.jsonOK(nil, with("secret", "delete", "deploy_key")...)
}
