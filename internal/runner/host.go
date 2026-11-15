package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
)

// Host runs jobs directly on the runner machine (container: none), so
// steps can use the host's Docker daemon to build and push images. Each job
// gets its own directory tree (workspace, temp, home, Docker config) that
// is deleted afterwards, together with the containers, images, networks,
// volumes and buildx builders the job created.
type Host struct {
	// WorkDir is the parent of the per-job directories.
	WorkDir string
	// Docker is the docker CLI used for cleanup ("" means "docker"; if it
	// isn't installed, Docker cleanup is skipped).
	Docker string
	// Limits is how CPU, memory and process limits are enforced:
	// "systemd" (each step in a transient systemd scope), "systemd-user"
	// (the same through the user's systemd instance) or "" (not enforced).
	// Set by DetectLimits.
	Limits string

	mu  sync.Mutex
	seq int
}

// DetectLimits works out whether systemd-run can put steps in a scope with
// resource limits, and returns a message for the runner's log.
func (h *Host) DetectLimits(ctx context.Context) string {
	h.Limits = ""
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return "host jobs: systemd-run isn't installed; CPU, memory and process limits are not enforced (timeouts and log limits are)"
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return "host jobs: systemd isn't running; CPU, memory and process limits are not enforced (timeouts and log limits are)"
	}
	try := func(user bool) bool {
		args := []string{"--scope", "--quiet", "--collect", "-p", "CPUQuota=100%", "-p", "MemoryMax=64M", "-p", "TasksMax=16", "true"}
		if user {
			args = append([]string{"--user"}, args...)
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return exec.CommandContext(cctx, "systemd-run", args...).Run() == nil
	}
	switch {
	case os.Geteuid() == 0 && try(false):
		h.Limits = "systemd"
	case try(true):
		h.Limits = "systemd-user"
	default:
		return "host jobs: systemd-run can't create scopes with resource limits for this user (enable lingering or cgroup delegation); CPU, memory and process limits are not enforced"
	}
	return "host jobs: CPU, memory and process limits are enforced with systemd scopes (" + h.Limits + ")"
}

func (h *Host) docker() string {
	if h.Docker == "" {
		return "docker"
	}
	return h.Docker
}

// hostEnvAllow are the runner's environment variables passed to host jobs.
// Everything else (including anything the runner's service manager set) is
// dropped so the job starts from a predictable environment.
var hostEnvAllow = []string{"PATH", "LANG", "LC_ALL", "TZ", "USER", "LOGNAME", "SHELL", "DOCKER_HOST", "DOCKER_CONTEXT", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"}

func (h *Host) Prepare(ctx context.Context, spec *runnerapi.JobSpec, containerEnv map[string]string, log io.Writer) (Env, error) {
	root := filepath.Join(h.WorkDir, "host", "job-"+strconv.FormatInt(spec.ID, 10))
	_ = os.RemoveAll(root) // a leftover from a crashed attempt
	e := &hostEnv{h: h, job: spec.ID, root: root, res: spec.Resources}
	for _, d := range []string{e.Workspace(), e.TempDir(), e.home(), e.dockerConfig()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return e, err
		}
	}
	// Keep Docker CLI plugins (buildx, compose) installed for the runner's
	// user working with the per-job Docker config.
	if home, err := os.UserHomeDir(); err == nil {
		if fi, err := os.Stat(filepath.Join(home, ".docker", "cli-plugins")); err == nil && fi.IsDir() {
			_ = os.Symlink(filepath.Join(home, ".docker", "cli-plugins"), filepath.Join(e.dockerConfig(), "cli-plugins"))
		}
	}
	e.base = map[string]string{}
	for _, k := range hostEnvAllow {
		if v, ok := os.LookupEnv(k); ok {
			e.base[k] = v
		}
	}
	if e.base["PATH"] == "" {
		e.base["PATH"] = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	e.base["HOME"] = e.home()
	e.base["TMPDIR"] = e.TempDir()
	e.base["DOCKER_CONFIG"] = e.dockerConfig()
	for k, v := range containerEnv {
		e.base[k] = v
	}
	if _, err := exec.LookPath(h.docker()); err == nil {
		e.snap = e.dockerSnapshot(ctx)
		if e.snap == nil {
			fmt.Fprintf(log, "Docker isn't reachable from this runner; Docker cleanup is skipped for this job\n")
		}
	}
	limits := "not enforced (no systemd scopes)"
	if h.Limits != "" {
		limits = "enforced per step with systemd scopes"
	}
	fmt.Fprintf(log, "Running on the runner host in %s (CPU, memory and process limits: %s)\n", root, limits)
	return e, nil
}

// Cleanup removes job directories left by an earlier run of the runner.
func (h *Host) Cleanup(context.Context) error {
	dirs, _ := filepath.Glob(filepath.Join(h.WorkDir, "host", "job-*"))
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
	return nil
}

type hostEnv struct {
	h    *Host
	job  int64
	root string
	res  runnerapi.Resources
	base map[string]string
	snap *dockerSnap
}

func (e *hostEnv) Workspace() string    { return filepath.Join(e.root, "workspace") }
func (e *hostEnv) TempDir() string      { return filepath.Join(e.root, "tmp") }
func (e *hostEnv) home() string         { return filepath.Join(e.root, "home") }
func (e *hostEnv) dockerConfig() string { return filepath.Join(e.root, "docker") }

// unitPrefix names the systemd scopes of this job's steps.
func (e *hostEnv) unitPrefix() string { return fmt.Sprintf("tgp-job-%d-", e.job) }

func (e *hostEnv) Exec(ctx context.Context, o ExecOptions) (int, error) {
	cmd := o.Cmd
	if e.h.Limits != "" {
		e.h.mu.Lock()
		e.h.seq++
		unit := e.unitPrefix() + strconv.Itoa(e.h.seq)
		e.h.mu.Unlock()
		args := []string{"--scope", "--quiet", "--collect", "--unit=" + unit}
		if e.h.Limits == "systemd-user" {
			args = append([]string{"--user"}, args...)
		}
		if e.res.CPUs > 0 {
			args = append(args, "-p", fmt.Sprintf("CPUQuota=%d%%", int(e.res.CPUs*100)))
		}
		if e.res.MemoryBytes > 0 {
			args = append(args, "-p", "MemoryMax="+strconv.FormatInt(e.res.MemoryBytes, 10), "-p", "MemorySwapMax=0")
		}
		if e.res.Pids > 0 {
			args = append(args, "-p", "TasksMax="+strconv.Itoa(e.res.Pids))
		}
		cmd = append(append([]string{"systemd-run"}, args...), cmd...)
	}
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	c.Dir = o.Dir
	if c.Dir != "" {
		_ = os.MkdirAll(c.Dir, 0o755)
	}
	env := map[string]string{}
	for k, v := range e.base {
		env[k] = v
	}
	for k, v := range o.Env {
		env[k] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c.Env = append(c.Env, k+"="+env[k])
	}
	c.Stdin, c.Stdout, c.Stderr = o.Stdin, o.Output, o.Output
	if c.Stdin == nil {
		c.Stdin = strings.NewReader("")
	}
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		e.killScopes()
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	c.WaitDelay = 2 * time.Second
	err := c.Run()
	if ee, ok := err.(*exec.ExitError); ok && ctx.Err() == nil {
		return ee.ExitCode(), nil
	}
	if err != nil {
		if errors.Is(err, exec.ErrWaitDelay) && ctx.Err() == nil {
			// The step exited but left a process holding its output open;
			// the output was cut off but the step's exit status stands.
			return 0, nil
		}
		return -1, err
	}
	return 0, nil
}

// killScopes kills every process in the job's systemd scopes, including
// ones that left the step's process group.
func (e *hostEnv) killScopes() {
	if e.h.Limits == "" {
		return
	}
	args := []string{"kill", "--signal=SIGKILL", e.unitPrefix() + "*"}
	if e.h.Limits == "systemd-user" {
		args = append([]string{"--user"}, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "systemctl", args...).Run()
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

// Close kills what's left of the job's processes, removes the Docker
// objects the job created and deletes the job's directories.
func (e *hostEnv) Close(context.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	e.killScopes()
	var errs []string
	if e.snap != nil {
		errs = append(errs, e.dockerCleanup(ctx)...)
	}
	if err := removeAll(e.root); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("cleanup: %s", strings.Join(errs, "; "))
	}
	return nil
}

// removeAll deletes dir, making read-only directories writable first (Go's
// module cache, for one, is read-only).
func removeAll(dir string) error {
	if err := os.RemoveAll(dir); err == nil {
		return nil
	}
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && fi.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(dir)
}

// dockerSnap is the Docker state when the job started.
type dockerSnap struct {
	containers, images, tags, networks, volumes map[string]bool
}

func (e *hostEnv) dockerLines(ctx context.Context, args ...string) ([]string, error) {
	c := exec.CommandContext(ctx, e.h.docker(), args...)
	c.Env = append(os.Environ(), "DOCKER_CONFIG="+e.dockerConfig())
	var out, stderr bytes.Buffer
	c.Stdout, c.Stderr = &out, &stderr
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("docker %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	var lines []string
	for _, l := range strings.Split(out.String(), "\n") {
		if l = strings.TrimSpace(l); l != "" && l != "<none>:<none>" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

func (e *hostEnv) dockerState(ctx context.Context) (*dockerSnap, error) {
	s := &dockerSnap{}
	for _, q := range []struct {
		dst  *map[string]bool
		args []string
	}{
		{&s.containers, []string{"ps", "-aq", "--no-trunc"}},
		{&s.images, []string{"image", "ls", "-aq", "--no-trunc"}},
		{&s.tags, []string{"image", "ls", "--format", "{{.Repository}}:{{.Tag}}"}},
		{&s.networks, []string{"network", "ls", "-q", "--no-trunc"}},
		{&s.volumes, []string{"volume", "ls", "-q"}},
	} {
		lines, err := e.dockerLines(ctx, q.args...)
		if err != nil {
			return nil, err
		}
		*q.dst = map[string]bool{}
		for _, l := range lines {
			(*q.dst)[l] = true
		}
	}
	return s, nil
}

func (e *hostEnv) dockerSnapshot(ctx context.Context) *dockerSnap {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := e.dockerState(cctx)
	if err != nil {
		return nil
	}
	return s
}

func newItems(now, before map[string]bool) []string {
	var out []string
	for k := range now {
		if !before[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// dockerCleanup removes the buildx builders, containers, networks,
// volumes, images and tags that appeared while the job ran. Host jobs run
// alone on their runner, so everything new belongs to this job.
func (e *hostEnv) dockerCleanup(ctx context.Context) []string {
	var errs []string
	run := func(args ...string) {
		if _, err := e.dockerLines(ctx, args...); err != nil {
			errs = append(errs, err.Error())
		}
	}
	// Builders created with docker buildx create live in the job's Docker
	// config; removing them also removes their build cache and containers.
	if names, err := e.dockerLines(ctx, "buildx", "ls", "--format", "{{.Name}}"); err == nil {
		for _, n := range names {
			n = strings.TrimSuffix(strings.Fields(n)[0], "*")
			if n != "default" && !strings.HasPrefix(n, "desktop-") && n != "NAME" {
				run("buildx", "rm", "-f", n)
			}
		}
	}
	now, err := e.dockerState(ctx)
	if err != nil {
		return append(errs, err.Error())
	}
	if ids := newItems(now.containers, e.snap.containers); len(ids) > 0 {
		run(append([]string{"rm", "-f", "-v"}, ids...)...)
	}
	for _, n := range newItems(now.networks, e.snap.networks) {
		run("network", "rm", n)
	}
	if vs := newItems(now.volumes, e.snap.volumes); len(vs) > 0 {
		run(append([]string{"volume", "rm", "-f"}, vs...)...)
	}
	// New tags first (they may point at images that existed before), then
	// new image IDs, then images the job left dangling.
	for _, t := range newItems(now.tags, e.snap.tags) {
		if !strings.HasSuffix(t, ":<none>") {
			run("image", "rm", t)
		}
	}
	if ids := newItems(now.images, e.snap.images); len(ids) > 0 {
		_, _ = e.dockerLines(ctx, append([]string{"image", "rm", "-f"}, ids...)...)
	}
	_, _ = e.dockerLines(ctx, "image", "prune", "-f")
	// Build cache no longer referenced by any image.
	_, _ = e.dockerLines(ctx, "builder", "prune", "-f")
	return errs
}

// Mux sends host jobs to Host and other jobs to Docker. Either may be nil
// when the runner doesn't accept that kind of job.
type Mux struct {
	Host   Executor
	Docker Executor
}

func (m *Mux) Prepare(ctx context.Context, spec *runnerapi.JobSpec, env map[string]string, log io.Writer) (Env, error) {
	if spec.Host {
		if m.Host == nil {
			return nil, errors.New("this runner doesn't run host jobs (container: none); set allow_host_jobs in its runner.toml")
		}
		return m.Host.Prepare(ctx, spec, env, log)
	}
	if m.Docker == nil {
		return nil, errors.New("this runner only runs host jobs (docker_jobs = false in its runner.toml)")
	}
	return m.Docker.Prepare(ctx, spec, env, log)
}

func (m *Mux) Cleanup(ctx context.Context) error {
	var errs []error
	for _, e := range []Executor{m.Host, m.Docker} {
		if e != nil {
			if err := e.Cleanup(ctx); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
