package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/testserver"
)

func TestMain(m *testing.M) {
	testserver.RunHookIfRequested()
	heartbeatEvery = 300 * time.Millisecond
	os.Exit(m.Run())
}

// hostExec runs jobs directly on the host in temp directories. It stands
// in for Docker in tests (this environment has no Docker daemon); the
// Docker executor itself is covered by TestDockerExecutor when
// TGP_DOCKER_TESTS=1.
type hostExec struct {
	mu       sync.Mutex
	prepared []*runnerapi.JobSpec
	fail     error
}

func (h *hostExec) Cleanup(context.Context) error { return nil }

func (h *hostExec) Prepare(_ context.Context, spec *runnerapi.JobSpec, _ map[string]string, log io.Writer) (Env, error) {
	h.mu.Lock()
	h.prepared = append(h.prepared, spec)
	h.mu.Unlock()
	if h.fail != nil {
		return nil, h.fail
	}
	root, err := os.MkdirTemp("", "tgp-hostexec-")
	if err != nil {
		return nil, err
	}
	e := &hostEnv{root: root}
	for _, d := range []string{e.Workspace(), e.TempDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	fmt.Fprintf(log, "Host executor ready (image %s ignored)\n", spec.Image)
	return e, nil
}

type hostEnv struct{ root string }

func (e *hostEnv) Workspace() string { return filepath.Join(e.root, "workspace") }
func (e *hostEnv) TempDir() string   { return filepath.Join(e.root, "tmp") }

func (e *hostEnv) Exec(ctx context.Context, o ExecOptions) (int, error) {
	c := exec.CommandContext(ctx, o.Cmd[0], o.Cmd[1:]...)
	c.Dir = o.Dir
	if c.Dir != "" {
		_ = os.MkdirAll(c.Dir, 0o755)
	}
	c.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + e.root}
	for k, v := range o.Env {
		c.Env = append(c.Env, k+"="+v)
	}
	c.Stdin, c.Stdout, c.Stderr = o.Stdin, o.Output, o.Output
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = time.Second
	err := c.Run()
	if ee, ok := err.(*exec.ExitError); ok && ctx.Err() == nil {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

func (e *hostEnv) WriteFile(_ context.Context, path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (e *hostEnv) ReadFile(_ context.Context, path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}

func (e *hostEnv) CopyIn(ctx context.Context, hostDir, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, "cp", "-a", hostDir+"/.", dir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cp: %v: %s", err, out)
	}
	return nil
}

func (e *hostEnv) CopyOut(_ context.Context, p string) (io.ReadCloser, error) { return TarPath(p) }

func (e *hostEnv) CopyInTar(_ context.Context, dir string, r io.Reader) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return Untar(r, dir)
}

func (e *hostEnv) Close(context.Context) error { return os.RemoveAll(e.root) }

// ---- helpers ----

type ciEnv struct {
	t     *testing.T
	srv   *testserver.Server
	admin *testserver.Client
	token string // admin PAT
}

func newCIEnv(t *testing.T) *ciEnv {
	srv := testserver.Start(t)
	admin := srv.Setup()
	tok := admin.Token("read", "write", "admin")
	admin.Do(http.MethodPost, "/repos", map[string]any{"name": "game", "auto_init": true}, nil)
	return &ciEnv{t: t, srv: srv, admin: srv.WithToken(tok), token: tok}
}

// push commits files to branch (creating it from main if needed).
func (e *ciEnv) push(branch, msg string, files map[string]string) string {
	e.t.Helper()
	dir := e.t.TempDir()
	u, _ := url.Parse(e.srv.URL)
	u.User = url.UserPassword("admin", e.token)
	u.Path = "/game.git"
	run := func(args ...string) string {
		e.t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := c.CombinedOutput()
		if err != nil {
			e.t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("clone", "-q", u.String(), ".")
	if branch != "main" {
		run("checkout", "-q", "-B", branch)
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			e.t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-qm", msg)
	run("push", "-q", "origin", "HEAD:refs/heads/"+branch)
	return run("rev-parse", "HEAD")
}

// startRunner registers a runner and runs it until the test ends.
func (e *ciEnv) startRunner(name string, capacity int, labels ...string) *hostExec {
	e.t.Helper()
	var tok struct {
		Token string `json:"token"`
	}
	e.admin.Do(http.MethodPost, "/admin/runners/registration-tokens", map[string]any{}, &tok)
	if len(labels) == 0 {
		labels = DefaultLabels()
	}
	reg, err := NewClient(e.srv.URL, "").Register(context.Background(), runnerapi.RegisterRequest{Token: tok.Token, Name: name, Labels: labels, Capacity: capacity})
	if err != nil {
		e.t.Fatal(err)
	}
	h := &hostExec{}
	cfg := Config{URL: e.srv.URL, Token: reg.Token, ID: reg.ID, Name: name, Labels: labels, Capacity: capacity, WorkDir: e.t.TempDir()}
	r := New(cfg, h, slog.New(slog.NewTextHandler(testLogOut(), nil)), "test")
	r.loadFn = func() runnerapi.Load { return runnerapi.Load{CPUPercent: 5, MemoryPercent: 20} }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := r.Run(ctx, ctx); err != nil {
			e.t.Errorf("runner: %v", err)
		}
	}()
	e.t.Cleanup(func() {
		cancel()
		<-done
	})
	return h
}

type run struct {
	Number       int64   `json:"number"`
	Status       string  `json:"status"`
	Conclusion   *string `json:"conclusion"`
	StatusReason *string `json:"status_reason"`
	Attempt      int     `json:"attempt"`
	CanRerun     bool    `json:"can_rerun"`
	Jobs         []struct {
		ID            int64   `json:"id"`
		Key           string  `json:"key"`
		Name          string  `json:"name"`
		Status        string  `json:"status"`
		Conclusion    *string `json:"conclusion"`
		FailureReason *string `json:"failure_reason"`
		WaitReason    *string `json:"wait_reason"`
		Steps         []struct {
			Number     int     `json:"number"`
			Name       string  `json:"name"`
			Status     string  `json:"status"`
			Conclusion *string `json:"conclusion"`
		} `json:"steps"`
	} `json:"jobs"`
}

func (r *run) conclusion() string {
	if r.Conclusion == nil {
		return ""
	}
	return *r.Conclusion
}

func (e *ciEnv) getRun(n int64) *run {
	e.t.Helper()
	var r run
	e.admin.Do(http.MethodGet, fmt.Sprintf("/repos/game/runs/%d", n), nil, &r)
	return &r
}

// waitRun polls until the run satisfies cond.
func (e *ciEnv) waitRun(n int64, what string, cond func(*run) bool) *run {
	e.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		r := e.getRun(n)
		if cond(r) {
			return r
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("run #%d: timed out waiting for %s; last state: %+v", n, what, r)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func completed(r *run) bool { return r.Status == "completed" }

func (e *ciEnv) logs(jobID int64) string {
	e.t.Helper()
	var out struct {
		Lines []struct {
			Step int    `json:"step"`
			Text string `json:"text"`
		} `json:"lines"`
		Complete bool `json:"complete"`
	}
	e.admin.Do(http.MethodGet, fmt.Sprintf("/repos/game/jobs/%d/logs", jobID), nil, &out)
	var b strings.Builder
	for _, l := range out.Lines {
		fmt.Fprintf(&b, "%d| %s\n", l.Step, l.Text)
	}
	return b.String()
}

func (e *ciEnv) jobByName(r *run, name string) (int64, string) {
	for _, j := range r.Jobs {
		if j.Name == name {
			c := ""
			if j.Conclusion != nil {
				c = *j.Conclusion
			}
			return j.ID, c
		}
	}
	e.t.Fatalf("no job %q in %+v", name, r.Jobs)
	return 0, ""
}

const pipeline = `name: CI
on: [push]
env:
  GREETING: hello
jobs:
  build:
    strategy:
      matrix:
        n: [1, 2]
    outputs:
      v: ${{ steps.ver.outputs.version }}
    steps:
      - uses: actions/checkout@v4
      - name: Look at the checkout
        run: test -f README.md && echo "checked out $(git rev-parse --abbrev-ref HEAD)"
      - id: ver
        run: echo "version=1.${{ matrix.n }}" >> "$TGP_OUTPUT"
      - run: echo "secret is ${{ secrets.DEPLOY_KEY }}"
      - run: |
          echo "EXTRA=from-env-file" >> "$GITHUB_ENV"
          mkdir -p "$RUNNER_TEMP/bin" && printf '#!/bin/sh\necho tool-ok\n' > "$RUNNER_TEMP/bin/mytool" && chmod +x "$RUNNER_TEMP/bin/mytool"
          echo "$RUNNER_TEMP/bin" >> "$TGP_PATH"
      - run: test "$EXTRA" = from-env-file && echo "$GREETING $EXTRA" && mytool
      - name: Allowed to fail
        run: exit 3
        continue-on-error: true
      - name: Only on failure
        if: failure()
        run: echo never
  test:
    needs: build
    steps:
      - run: echo "got ${{ needs.build.outputs.v }} result ${{ needs.build.result }}"
  never:
    if: github.ref == 'refs/heads/nope'
    steps:
      - run: echo no
`

func TestRunnerEndToEnd(t *testing.T) {
	e := newCIEnv(t)
	e.admin.Do(http.MethodPut, "/repos/game/secrets/deploy_key", map[string]any{"value": "s3cr3t-value"}, nil)

	// Pushed before any runner exists: the run waits with a reason.
	e.push("main", "Add CI", map[string]string{".tgp/workflows/ci.yml": pipeline})
	r := e.getRun(1)
	if r.Status != "queued" || len(r.Jobs) != 4 {
		t.Fatalf("queued run: %+v", r)
	}
	if w := r.Jobs[0].WaitReason; w == nil || !strings.Contains(*w, "no runner") {
		t.Fatalf("wait reason: %v", w)
	}

	h := e.startRunner("r1", 2)
	r = e.waitRun(1, "completion", completed)
	if r.conclusion() != "success" {
		t.Fatalf("run: %s %+v", r.conclusion(), r)
	}
	for _, name := range []string{"build (1)", "build (2)", "test"} {
		id, c := e.jobByName(r, name)
		if c != "success" {
			t.Fatalf("%s: %s\n%s", name, c, e.logs(id))
		}
	}
	if _, c := e.jobByName(r, "never"); c != "skipped" {
		t.Fatalf("never: %s", c)
	}
	id, _ := e.jobByName(r, "build (1)")
	log := e.logs(id)
	for _, want := range []string{"checked out main", "secret is ***", "hello from-env-file", "tool-ok", "exit code 3", "continue-on-error"} {
		if !strings.Contains(log, want) {
			t.Errorf("build log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "s3cr3t-value") {
		t.Fatalf("secret leaked:\n%s", log)
	}
	for _, j := range r.Jobs {
		if j.Name != "build (1)" {
			continue
		}
		for _, st := range j.Steps {
			want := "success"
			switch st.Name {
			case "Allowed to fail":
				want = "failure"
			case "Only on failure":
				want = "skipped"
			}
			if st.Conclusion == nil || *st.Conclusion != want {
				t.Errorf("step %q: %v, want %s", st.Name, st.Conclusion, want)
			}
		}
	}
	testID, _ := e.jobByName(r, "test")
	if l := e.logs(testID); !strings.Contains(l, "got 1.") || !strings.Contains(l, "result success") {
		t.Fatalf("needs outputs:\n%s", l)
	}
	// The runner received the secret only for the job that uses it.
	h.mu.Lock()
	for _, s := range h.prepared {
		_, has := s.Secrets["DEPLOY_KEY"]
		if has != strings.HasPrefix(s.Name, "build") {
			t.Errorf("job %s secrets: %v", s.Name, s.Secrets)
		}
		if s.Resources.CPUs != 2 || s.Resources.MemoryBytes != 4096<<20 || s.TimeoutSeconds != 3600 {
			t.Errorf("job %s defaults: %+v timeout %d", s.Name, s.Resources, s.TimeoutSeconds)
		}
	}
	h.mu.Unlock()

	// The commit's checks.
	var checks struct {
		State string `json:"state"`
		Total int    `json:"total"`
	}
	e.admin.Do(http.MethodGet, "/repos/game/commits/main/checks", nil, &checks)
	if checks.State != "success" || checks.Total != 4 {
		t.Fatalf("checks: %+v", checks)
	}
}

func TestRunnerFailureRerunAndCancel(t *testing.T) {
	e := newCIEnv(t)
	e.startRunner("r1", 1)

	flaky := `name: Flaky
on: push
jobs:
  first:
    steps:
      - run: echo first
  flaky:
    needs: first
    steps:
      - run: |
          if [ -f /tmp/tgp-flaky-$TGP_RUN_ID ]; then echo second try; else touch /tmp/tgp-flaky-$TGP_RUN_ID; echo boom; exit 1; fi
  after:
    needs: flaky
    steps:
      - run: echo after
`
	e.push("main", "Flaky", map[string]string{".tgp/workflows/flaky.yml": flaky})
	r := e.waitRun(1, "completion", completed)
	defer os.Remove("/tmp/tgp-flaky-" + fmt.Sprint(r.Number))
	if r.conclusion() != "failure" {
		t.Fatalf("expected failure: %+v", r)
	}
	firstID, _ := e.jobByName(r, "first")
	if _, c := e.jobByName(r, "after"); c != "skipped" {
		t.Fatalf("after: %s", c)
	}
	// Re-run failed jobs: first is reused, flaky and after run again.
	var rr run
	e.admin.Do(http.MethodPost, "/repos/game/runs/1/rerun", map[string]any{"failed_only": true}, &rr)
	if rr.Attempt != 2 {
		t.Fatalf("attempt: %+v", rr)
	}
	r = e.waitRun(1, "rerun completion", completed)
	if r.conclusion() != "success" {
		t.Fatalf("rerun: %+v", r)
	}
	newFirst, c := e.jobByName(r, "first")
	if c != "success" || newFirst == firstID {
		t.Fatalf("first reused: %d %s", newFirst, c)
	}
	if l := e.logs(newFirst); !strings.Contains(l, "first") {
		t.Fatalf("reused job's logs should be the original's:\n%s", l)
	}
	// Re-running a finished, passing run's failed jobs is a conflict.
	if st, _ := e.admin.Raw(http.MethodPost, "/repos/game/runs/1/rerun", map[string]any{"failed_only": true}); st != http.StatusConflict {
		t.Fatalf("nothing to rerun: %d", st)
	}

	// Cancelling a running job stops it on the runner.
	slow := "name: Slow\non: push\njobs:\n  slow:\n    steps:\n      - run: echo started; sleep 60\n"
	e.push("main", "Slow", map[string]string{".tgp/workflows/slow.yml": slow, ".tgp/workflows/flaky.yml": "on: workflow_dispatch\njobs:\n  x:\n    steps: [{run: 'true'}]\n"})
	e.waitRun(2, "start", func(r *run) bool {
		return r.Status == "in_progress" && len(r.Jobs) > 0 && len(r.Jobs[0].Steps) > 1 && r.Jobs[0].Steps[1].Status == "in_progress"
	})
	start := time.Now()
	e.admin.Do(http.MethodPost, "/repos/game/runs/2/cancel", nil, nil)
	r = e.waitRun(2, "cancellation", completed)
	if r.conclusion() != "cancelled" || time.Since(start) > 15*time.Second {
		t.Fatalf("cancel: %s after %s", r.conclusion(), time.Since(start))
	}
	if fr := r.Jobs[0].FailureReason; fr == nil || !strings.Contains(*fr, "Cancelled by admin") {
		t.Fatalf("cancel reason: %v", fr)
	}
}

func TestRunnerLabelsAndSetupFailure(t *testing.T) {
	e := newCIEnv(t)
	wf := `on: push
jobs:
  gpu:
    runs-on: [gpu]
    steps: [{run: nvidia-smi}]
  plain:
    container: {image: "alpine:3.20"}
    steps: [{run: echo hi}]
`
	e.push("main", "Labels", map[string]string{".tgp/workflows/l.yml": wf})
	h := e.startRunner("cpu-only", 1, "linux")
	h.fail = fmt.Errorf("pull access denied for alpine:3.20")
	r := e.waitRun(1, "plain job done", func(r *run) bool {
		for _, j := range r.Jobs {
			if j.Key == "plain" && j.Status == "completed" {
				return true
			}
		}
		return false
	})
	plainID, c := e.jobByName(r, "plain")
	if c != "failure" {
		t.Fatalf("plain: %s", c)
	}
	if l := e.logs(plainID); !strings.Contains(l, "pull access denied") {
		t.Fatalf("setup failure log:\n%s", l)
	}
	for _, j := range r.Jobs {
		if j.Key == "gpu" && (j.Status != "queued" || j.WaitReason == nil || !strings.Contains(*j.WaitReason, "no runner has labels gpu")) {
			t.Fatalf("gpu job: %+v %v", j.Status, j.WaitReason)
		}
	}
}

func testLogOut() io.Writer {
	if os.Getenv("TGP_TEST_RUNNER_LOG") != "" {
		return os.Stderr
	}
	return io.Discard
}
