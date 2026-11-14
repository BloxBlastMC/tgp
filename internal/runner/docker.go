package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
)

// Executor prepares an isolated environment for a job.
type Executor interface {
	// Prepare pulls images and starts the job's containers. Progress goes
	// to log. The returned Env must be closed even if a later step fails.
	Prepare(ctx context.Context, spec *runnerapi.JobSpec, env map[string]string, log io.Writer) (Env, error)
	// Cleanup removes leftovers of jobs from an earlier run of this runner
	// (e.g. after a crash).
	Cleanup(ctx context.Context) error
}

// ExecOptions describes a command to run in the job environment.
type ExecOptions struct {
	Cmd    []string
	Env    map[string]string
	Dir    string // absolute path inside the environment
	Stdin  io.Reader
	Output io.Writer // stdout and stderr, interleaved
}

// Env is a running job environment.
type Env interface {
	Exec(ctx context.Context, o ExecOptions) (exitCode int, err error)
	WriteFile(ctx context.Context, path string, data []byte) error
	ReadFile(ctx context.Context, path string) ([]byte, error)
	// CopyIn copies the contents of a host directory to dir in the
	// environment (created if needed).
	CopyIn(ctx context.Context, hostDir, dir string) error
	// Workspace is the checkout directory; TempDir holds tgp's files.
	Workspace() string
	TempDir() string
	Close(ctx context.Context) error
}

// Docker runs each job in a container (plus service containers) on a
// private network, with the workspace on a volume shared by all steps.
type Docker struct {
	// Binary is the docker CLI (default "docker").
	Binary string
	// RunnerID labels containers so leftovers can be found.
	RunnerID int64
	// Pull is "missing" (default: pull only absent images, and :latest
	// always) or "always".
	Pull string
}

const (
	dockerWorkspace = "/workspace"
	dockerTemp      = "/tmp/tgp"
	labelRunner     = "dev.tgp.runner"
	labelJob        = "dev.tgp.job"
)

func (d *Docker) bin() string {
	if d.Binary == "" {
		return "docker"
	}
	return d.Binary
}

// cmd builds a docker command. Values in env are passed through the
// docker CLI's environment (-e NAME), never on the command line, so
// secrets don't show up in the host's process list.
func (d *Docker) cmd(ctx context.Context, env map[string]string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, d.bin(), args...)
	c.Env = os.Environ()
	for k, v := range env {
		c.Env = append(c.Env, k+"="+v)
	}
	c.WaitDelay = 5 * time.Second
	return c
}

func envFlags(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, 2*len(keys))
	for _, k := range keys {
		out = append(out, "-e", k)
	}
	return out
}

func (d *Docker) run(ctx context.Context, log io.Writer, env map[string]string, args ...string) error {
	c := d.cmd(ctx, env, args...)
	var buf bytes.Buffer
	if log != nil {
		c.Stdout, c.Stderr = io.MultiWriter(log, &buf), io.MultiWriter(log, &buf)
	} else {
		c.Stdout, c.Stderr = &buf, &buf
	}
	if err := c.Run(); err != nil {
		return fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(lastLines(buf.String(), 5)))
	}
	return nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (d *Docker) pull(ctx context.Context, image string, log io.Writer) error {
	always := d.Pull == "always" || strings.HasSuffix(image, ":latest") || !strings.Contains(lastSegment(image), ":")
	if !always {
		if err := d.run(ctx, nil, nil, "image", "inspect", "--format", "{{.Id}}", image); err == nil {
			fmt.Fprintf(log, "Using image %s (already present)\n", image)
			return nil
		}
	}
	fmt.Fprintf(log, "Pulling %s\n", image)
	return d.run(ctx, log, nil, "pull", "--quiet", image)
}

// lastSegment is the image name after the registry/path, where a tag lives.
func lastSegment(image string) string {
	if i := strings.IndexByte(image, '@'); i >= 0 {
		return image[:i] + ":digest"
	}
	if i := strings.LastIndexByte(image, '/'); i >= 0 {
		return image[i+1:]
	}
	return image
}

func (d *Docker) labels(job int64) []string {
	return []string{"--label", labelRunner + "=" + strconv.FormatInt(d.RunnerID, 10), "--label", labelJob + "=" + strconv.FormatInt(job, 10)}
}

type dockerEnv struct {
	d          *Docker
	job        int64
	name       string
	network    string
	volume     string
	containers []string
}

func (d *Docker) Prepare(ctx context.Context, spec *runnerapi.JobSpec, containerEnv map[string]string, log io.Writer) (Env, error) {
	base := fmt.Sprintf("tgp-job-%d", spec.ID)
	e := &dockerEnv{d: d, job: spec.ID, name: base, network: base, volume: base + "-workspace"}
	images := []string{spec.Image}
	for _, s := range spec.Services {
		images = append(images, s.Image)
	}
	for _, img := range images {
		if err := d.pull(ctx, img, log); err != nil {
			return e, fmt.Errorf("can't pull %s: %w", img, err)
		}
	}
	if err := d.run(ctx, nil, nil, append([]string{"network", "create"}, append(d.labels(spec.ID), e.network)...)...); err != nil {
		return e, err
	}
	if err := d.run(ctx, nil, nil, append([]string{"volume", "create"}, append(d.labels(spec.ID), e.volume)...)...); err != nil {
		return e, err
	}
	limits := []string{"--security-opt", "no-new-privileges"}
	if r := spec.Resources; r.CPUs > 0 {
		limits = append(limits, "--cpus", strconv.FormatFloat(r.CPUs, 'f', -1, 64))
	}
	if r := spec.Resources; r.MemoryBytes > 0 {
		m := strconv.FormatInt(r.MemoryBytes, 10)
		limits = append(limits, "--memory", m, "--memory-swap", m)
	}
	if r := spec.Resources; r.Pids > 0 {
		limits = append(limits, "--pids-limit", strconv.Itoa(r.Pids))
	}
	for _, svc := range spec.Services {
		name := base + "-" + svc.Name
		fmt.Fprintf(log, "Starting service %s (%s)\n", svc.Name, svc.Image)
		args := append([]string{"run", "-d", "--name", name, "--network", e.network, "--network-alias", svc.Name}, d.labels(spec.ID)...)
		args = append(args, limits...)
		args = append(args, envFlags(svc.Env)...)
		args = append(args, svc.Image)
		if err := d.run(ctx, nil, svc.Env, args...); err != nil {
			return e, fmt.Errorf("can't start service %s: %w", svc.Name, err)
		}
		e.containers = append(e.containers, name)
	}
	fmt.Fprintf(log, "Starting job container (%s; %g CPUs, %d MiB memory, %d processes)\n",
		spec.Image, spec.Resources.CPUs, spec.Resources.MemoryBytes>>20, spec.Resources.Pids)
	args := append([]string{"run", "-d", "--name", e.name, "--network", e.network, "--hostname", "tgp-job"}, d.labels(spec.ID)...)
	args = append(args, limits...)
	args = append(args, "-v", e.volume+":"+dockerWorkspace, "-w", dockerWorkspace)
	args = append(args, envFlags(containerEnv)...)
	args = append(args, "--entrypoint", "tail", spec.Image, "-f", "/dev/null")
	if err := d.run(ctx, nil, containerEnv, args...); err != nil {
		return e, fmt.Errorf("can't start the job container: %w (the image needs tail and sh)", err)
	}
	e.containers = append(e.containers, e.name)
	if err := d.run(ctx, nil, nil, "exec", e.name, "mkdir", "-p", dockerTemp); err != nil {
		return e, err
	}
	return e, nil
}

func (e *dockerEnv) Workspace() string { return dockerWorkspace }
func (e *dockerEnv) TempDir() string   { return dockerTemp }

func (e *dockerEnv) Exec(ctx context.Context, o ExecOptions) (int, error) {
	args := []string{"exec", "-i"}
	if o.Dir != "" {
		args = append(args, "-w", o.Dir)
	}
	args = append(args, envFlags(o.Env)...)
	args = append(args, e.name)
	args = append(args, o.Cmd...)
	c := e.d.cmd(ctx, o.Env, args...)
	c.Stdin = o.Stdin
	if c.Stdin == nil {
		c.Stdin = strings.NewReader("")
	}
	c.Stdout, c.Stderr = o.Output, o.Output
	err := c.Run()
	if ee, ok := err.(*exec.ExitError); ok && ctx.Err() == nil {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

func (e *dockerEnv) WriteFile(ctx context.Context, path string, data []byte) error {
	c := e.d.cmd(ctx, nil, "exec", "-i", e.name, "sh", "-c", `mkdir -p "$(dirname "$1")" && cat > "$1"`, "sh", path)
	c.Stdin = bytes.NewReader(data)
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("write %s: %w: %s", path, err, out)
	}
	return nil
}

func (e *dockerEnv) ReadFile(ctx context.Context, path string) ([]byte, error) {
	c := e.d.cmd(ctx, nil, "exec", e.name, "sh", "-c", `cat "$1" 2>/dev/null || true`, "sh", path)
	var out bytes.Buffer
	c.Stdout = &out
	if err := c.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (e *dockerEnv) CopyIn(ctx context.Context, hostDir, dir string) error {
	if err := e.d.run(ctx, nil, nil, "exec", e.name, "mkdir", "-p", dir); err != nil {
		return err
	}
	return e.d.run(ctx, nil, nil, "cp", hostDir+"/.", e.name+":"+dir)
}

// Close removes the containers, network and volume. It uses a fresh
// context: cleanup must happen even when the job was cancelled.
func (e *dockerEnv) Close(context.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var errs []string
	if len(e.containers) > 0 {
		if err := e.d.run(ctx, nil, nil, append([]string{"rm", "-f", "-v"}, e.containers...)...); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if err := e.d.run(ctx, nil, nil, "network", "rm", e.network); err != nil && !strings.Contains(err.Error(), "not found") {
		errs = append(errs, err.Error())
	}
	if err := e.d.run(ctx, nil, nil, "volume", "rm", "-f", e.volume); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("cleanup: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Cleanup removes containers, networks and volumes this runner left behind.
func (d *Docker) Cleanup(ctx context.Context) error {
	filter := "label=" + labelRunner + "=" + strconv.FormatInt(d.RunnerID, 10)
	list := func(args ...string) []string {
		c := d.cmd(ctx, nil, args...)
		out, err := c.Output()
		if err != nil {
			return nil
		}
		return strings.Fields(string(out))
	}
	if ids := list("ps", "-aq", "--filter", filter); len(ids) > 0 {
		_ = d.run(ctx, nil, nil, append([]string{"rm", "-f", "-v"}, ids...)...)
	}
	for _, n := range list("network", "ls", "-q", "--filter", filter) {
		_ = d.run(ctx, nil, nil, "network", "rm", n)
	}
	for _, v := range list("volume", "ls", "-q", "--filter", filter) {
		_ = d.run(ctx, nil, nil, "volume", "rm", "-f", v)
	}
	return nil
}

// Check verifies the docker CLI can reach a daemon.
func (d *Docker) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return d.run(ctx, nil, nil, "version", "--format", "{{.Server.Version}}")
}
