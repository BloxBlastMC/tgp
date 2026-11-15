package runner

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
)

const hostWorkflow = `name: Build image
on: [push, pull_request]
jobs:
  image:
    container: none
    steps:
      - uses: checkout
      - run: |
          test -f README.md
          echo "token=$TGP_TOKEN"
          echo "registry=$TGP_REGISTRY image=$TGP_IMAGE"
          echo "ctx=${{ tgp.image }}"
          echo "home=$HOME"
          echo "docker-config=$DOCKER_CONFIG"
          case "$HOME" in "$TGP_WORKSPACE"/../home) echo home-ok;; esac
          touch "$HOME/leftover"
`

func TestHostJobTrustedRun(t *testing.T) {
	e := newCIEnv(t)
	work := t.TempDir()
	h := e.startRunnerCfg(Config{Name: "builder", Capacity: 2, AllowHostJobs: runnerapi.HostTrusted, WorkDir: work})
	e.push("main", "Image", map[string]string{".tgp/workflows/image.yml": hostWorkflow})
	r := e.waitRun(1, "completion", completed)
	id, c := e.jobByName(r, "image")
	log := e.logs(id)
	if c != "success" {
		t.Fatalf("host job: %s\n%s", c, log)
	}
	for _, want := range []string{"token=***", "registry=127.0.0.1", "/game\n", "ctx=127.0.0.1", "home-ok", "Running on the runner host", "trusted run: true"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "tgp_job_") {
		t.Fatalf("job token leaked:\n%s", log)
	}
	h.mu.Lock()
	spec := h.prepared[0]
	h.mu.Unlock()
	if !spec.Host || !spec.Trusted || spec.Image != "" {
		t.Fatalf("spec: host=%v trusted=%v image=%q", spec.Host, spec.Trusted, spec.Image)
	}
	// The job's directories are gone.
	if left, _ := filepath.Glob(filepath.Join(work, "host", "job-*")); len(left) > 0 {
		t.Fatalf("job directories left behind: %v", left)
	}
}

func TestHostJobPullRequestsNeedAllRunner(t *testing.T) {
	e := newCIEnv(t)
	trusted := e.startRunnerCfg(Config{Name: "trusted", Capacity: 1, AllowHostJobs: runnerapi.HostTrusted})
	e.push("main", "Image", map[string]string{".tgp/workflows/image.yml": hostWorkflow})
	e.waitRun(1, "push run", completed)
	e.push("feature", "Change", map[string]string{"x.txt": "x"})
	e.admin.Do(http.MethodPost, "/repos/game/pulls", map[string]any{"title": "Feature", "head": "feature"}, nil)
	// The pull request run waits: the trusted runner refuses untrusted
	// host jobs (so does the push run of the feature branch).
	var prRun *run
	deadline := time.Now().Add(20 * time.Second)
	for prRun == nil {
		var list struct {
			Runs []struct {
				Number int64  `json:"number"`
				Event  string `json:"event"`
			} `json:"runs"`
		}
		e.admin.Do(http.MethodGet, "/repos/game/runs", nil, &list)
		for _, rr := range list.Runs {
			if rr.Event == "pull_request" {
				prRun = e.getRun(rr.Number)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no pull request run: %+v", list)
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(1500 * time.Millisecond) // a few fetches by the trusted runner
	prRun = e.getRun(prRun.Number)
	if prRun.Status != "queued" || prRun.Jobs[0].WaitReason == nil || !strings.Contains(*prRun.Jobs[0].WaitReason, "pull requests") {
		t.Fatalf("PR host job should wait for an 'all' runner: %+v %v", prRun.Status, prRun.Jobs[0].WaitReason)
	}
	trusted.mu.Lock()
	if n := len(trusted.prepared); n != 1 {
		t.Fatalf("trusted runner ran %d jobs, want 1 (the push run)", n)
	}
	trusted.mu.Unlock()

	all := e.startRunnerCfg(Config{Name: "throwaway", Capacity: 1, AllowHostJobs: runnerapi.HostAll})
	r := e.waitRun(prRun.Number, "PR run completion", completed)
	id, c := e.jobByName(r, "image")
	if c != "success" {
		t.Fatalf("PR host job: %s\n%s", c, e.logs(id))
	}
	if l := e.logs(id); !strings.Contains(l, "trusted run: false") {
		t.Fatalf("PR run should be untrusted:\n%s", l)
	}
	// The throwaway runner also ran the push to the feature branch (not
	// the default branch, so untrusted too).
	all.mu.Lock()
	defer all.mu.Unlock()
	for _, s := range all.prepared {
		if s.Trusted {
			t.Fatalf("throwaway runner got a trusted job: %s", s.Name)
		}
	}
}

func TestHostJobNotOnPlainRunner(t *testing.T) {
	e := newCIEnv(t)
	plain := e.startRunner("plain", 1)
	e.push("main", "Image", map[string]string{".tgp/workflows/image.yml": hostWorkflow})
	time.Sleep(1500 * time.Millisecond)
	r := e.getRun(1)
	if r.Status != "queued" || r.Jobs[0].WaitReason == nil || !strings.Contains(*r.Jobs[0].WaitReason, "allow_host_jobs") {
		t.Fatalf("host job on a plain runner: %+v %v", r.Status, r.Jobs[0].WaitReason)
	}
	plain.mu.Lock()
	defer plain.mu.Unlock()
	if len(plain.prepared) != 0 {
		t.Fatalf("plain runner took a host job")
	}
}

func TestHostJobCancelKillsBackgroundProcesses(t *testing.T) {
	e := newCIEnv(t)
	e.startRunnerCfg(Config{Name: "builder", Capacity: 1, AllowHostJobs: runnerapi.HostTrusted})
	pidFile := filepath.Join(t.TempDir(), "pid")
	wf := "on: push\njobs:\n  slow:\n    container: none\n    steps:\n      - run: |\n          sleep 300 &\n          echo $! > " + pidFile + "\n          echo started\n          wait\n"
	e.push("main", "Slow", map[string]string{".tgp/workflows/slow.yml": wf})
	e.waitRun(1, "start", func(r *run) bool {
		_, err := os.Stat(pidFile)
		return err == nil && r.Status == "in_progress"
	})
	time.Sleep(200 * time.Millisecond)
	b, _ := os.ReadFile(pidFile)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("pid file: %q", b)
	}
	e.admin.Do(http.MethodPost, "/repos/game/runs/1/cancel", nil, nil)
	r := e.waitRun(1, "cancellation", completed)
	if r.conclusion() != "cancelled" {
		t.Fatalf("cancel: %s", r.conclusion())
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("background process %d survived the cancellation", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRunnerLogCap(t *testing.T) {
	e := newCIEnv(t)
	e.admin.Do(http.MethodPatch, "/admin/ci/settings", map[string]any{"max_log_mb": 1}, nil)
	e.startRunnerCfg(Config{Name: "builder", Capacity: 1, AllowHostJobs: runnerapi.HostTrusted})
	wf := "on: push\njobs:\n  noisy:\n    container: none\n    steps:\n      - run: |\n          i=0; while [ $i -lt 30000 ]; do echo \"line $i padding padding padding padding padding\"; i=$((i+1)); done\n"
	e.push("main", "Noisy", map[string]string{".tgp/workflows/noisy.yml": wf})
	r := e.waitRun(1, "completion", completed)
	id, c := e.jobByName(r, "noisy")
	if c != "success" {
		t.Fatalf("noisy: %s", c)
	}
	if l := e.logs(id); !strings.Contains(l, "limit") || strings.Contains(l, "line 29999") {
		t.Fatalf("log wasn't capped (%d bytes)", len(l))
	}
}

// fakeDocker writes a docker CLI stand-in that answers listing commands
// from files in dir and records every invocation in dir/calls.
func fakeDocker(t *testing.T, dir string) string {
	t.Helper()
	script := `#!/bin/sh
d="` + dir + `"
echo "$*" >> "$d/calls"
case "$*" in
  "ps -aq --no-trunc") cat "$d/containers" 2>/dev/null ;;
  "image ls -aq --no-trunc") cat "$d/images" 2>/dev/null ;;
  "image ls --format {{.Repository}}:{{.Tag}}") cat "$d/tags" 2>/dev/null ;;
  "network ls -q --no-trunc") cat "$d/networks" 2>/dev/null ;;
  "volume ls -q") cat "$d/volumes" 2>/dev/null ;;
  "buildx ls --format {{.Name}}") cat "$d/builders" 2>/dev/null ;;
esac
exit 0
`
	p := filepath.Join(dir, "docker")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHostDockerCleanup(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, lines ...string) {
		_ = os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	}
	write("containers", "c-old")
	write("images", "sha256:old")
	write("tags", "debian:bookworm")
	write("networks", "n-bridge")
	write("volumes", "v-old")
	h := &Host{WorkDir: t.TempDir(), Docker: fakeDocker(t, dir)}
	env, err := h.Prepare(context.Background(), &runnerapi.JobSpec{ID: 7, Host: true}, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	he := env.(*hostEnv)
	if he.snap == nil {
		t.Fatal("no docker snapshot")
	}
	// The job builds an image, starts a container, makes a builder, ...
	write("containers", "c-old", "c-new")
	write("images", "sha256:old", "sha256:new")
	write("tags", "debian:bookworm", "git.example.com/org/game/issues:g123")
	write("networks", "n-bridge", "n-new")
	write("volumes", "v-old", "v-new")
	write("builders", "default", "tgp-mine*")
	if code, err := env.Exec(context.Background(), ExecOptions{Cmd: []string{"sh", "-c", `test "$DOCKER_CONFIG" = "` + he.dockerConfig() + `" && touch "$HOME/x"`}, Output: io.Discard}); err != nil || code != 0 {
		t.Fatalf("exec: %d %v", code, err)
	}
	if err := env.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "calls"))
	calls := strings.Split(strings.TrimSpace(string(b)), "\n")
	want := []string{
		"buildx rm -f tgp-mine",
		"rm -f -v c-new",
		"network rm n-new",
		"volume rm -f v-new",
		"image rm git.example.com/org/game/issues:g123",
		"image rm -f sha256:new",
		"image prune -f",
		"builder prune -f",
	}
	got := map[string]bool{}
	for _, c := range calls {
		got[c] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing docker call %q; calls:\n%s", w, strings.Join(calls, "\n"))
		}
	}
	for _, c := range calls {
		for _, old := range []string{"c-old", "sha256:old", "debian:bookworm", "n-bridge", "v-old", "rm -f default"} {
			if strings.Contains(c, old) && (strings.HasPrefix(c, "rm") || strings.Contains(c, " rm")) {
				t.Errorf("cleanup touched pre-existing %s: %q", old, c)
			}
		}
	}
	if _, err := os.Stat(he.root); !os.IsNotExist(err) {
		t.Fatalf("job root still exists: %v", err)
	}
}

func TestMuxRoutes(t *testing.T) {
	m := &Mux{}
	if _, err := m.Prepare(context.Background(), &runnerapi.JobSpec{Host: true}, nil, io.Discard); err == nil || !strings.Contains(err.Error(), "allow_host_jobs") {
		t.Fatalf("host job without host executor: %v", err)
	}
	if _, err := m.Prepare(context.Background(), &runnerapi.JobSpec{}, nil, io.Discard); err == nil || !strings.Contains(err.Error(), "docker_jobs") {
		t.Fatalf("container job without docker: %v", err)
	}
}

func TestConfigHostMode(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) string {
		p := filepath.Join(dir, "runner.toml")
		_ = os.WriteFile(p, []byte("url = 'https://x'\ntoken = 't'\nlabels = ['linux']\n"+s), 0o600)
		return p
	}
	c, err := LoadConfig(write(""))
	if err != nil || c.HostMode() != runnerapi.HostOff || !c.RunsDockerJobs() || strings.Join(c.EffectiveLabels(), ",") != "linux" {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	c, err = LoadConfig(write("allow_host_jobs = 'trusted'\ndocker_jobs = false\n"))
	if err != nil || c.HostMode() != runnerapi.HostTrusted || c.RunsDockerJobs() {
		t.Fatalf("host-only: %+v %v", c, err)
	}
	labels := c.EffectiveLabels()
	sort.Strings(labels)
	if strings.Join(labels, ",") != "host,linux" {
		t.Fatalf("labels: %v", labels)
	}
	if _, err := LoadConfig(write("allow_host_jobs = 'yes'\n")); err == nil {
		t.Fatal("bad allow_host_jobs accepted")
	}
	if _, err := LoadConfig(write("docker_jobs = false\n")); err == nil {
		t.Fatal("a runner that runs nothing was accepted")
	}
}
