package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/workflow"
)

// Config is the runner's configuration file (written by register).
type Config struct {
	// URL is the tgp server, e.g. https://git.example.com.
	URL string `toml:"url"`
	// Token is the runner's credential (secret).
	Token string `toml:"token"`
	ID    int64  `toml:"id"`
	Name  string `toml:"name"`
	// Labels are matched against jobs' runs-on.
	Labels []string `toml:"labels"`
	// Capacity is how many jobs run at once.
	Capacity int `toml:"capacity"`
	// WorkDir holds temporary checkouts.
	WorkDir string `toml:"work_dir"`
	// Docker is the docker CLI binary.
	Docker string `toml:"docker"`
	// Pull is "missing" or "always".
	Pull string `toml:"pull"`
	// AllowHostJobs says which host jobs (container: none) this runner
	// runs directly on its machine: "off" (default), "trusted" (pushes to
	// the default branch, manual dispatches and releases) or "all" (pull
	// requests too; use a throwaway machine). Host jobs can use the host's
	// Docker daemon and run alone on the runner.
	AllowHostJobs string `toml:"allow_host_jobs,omitempty"`
	// DockerJobs = false makes a runner that only runs host jobs (no
	// Docker daemon needed). Default true.
	DockerJobs *bool `toml:"docker_jobs,omitempty"`
}

// HostMode is the effective allow_host_jobs value.
func (c *Config) HostMode() string {
	if c.AllowHostJobs == "" {
		return runnerapi.HostOff
	}
	return c.AllowHostJobs
}

// RunsDockerJobs reports whether the runner runs container jobs.
func (c *Config) RunsDockerJobs() bool { return c.DockerJobs == nil || *c.DockerJobs }

// EffectiveLabels are the labels reported to the server: the configured
// ones plus "host" when the runner accepts host jobs.
func (c *Config) EffectiveLabels() []string {
	out := append([]string{}, c.Labels...)
	if c.HostMode() != runnerapi.HostOff {
		for _, l := range out {
			if strings.EqualFold(l, workflow.HostLabel) {
				return out
			}
		}
		out = append(out, workflow.HostLabel)
	}
	return out
}

// DefaultLabels are given to runners registered without --labels.
func DefaultLabels() []string { return []string{"docker", "linux", runtime.GOARCH} }

// LoadConfig reads a runner config file.
func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s doesn't exist; register the runner first: tgp-runner register --url URL --token TOKEN", path)
		}
		return nil, err
	}
	var c Config
	if err := toml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.URL == "" || c.Token == "" {
		return nil, fmt.Errorf("%s: url and token are required", path)
	}
	if c.Capacity < 1 {
		c.Capacity = 1
	}
	if c.WorkDir == "" {
		c.WorkDir = filepath.Join(filepath.Dir(path), "work")
	}
	if !runnerapi.ValidHostMode(c.HostMode()) {
		return nil, fmt.Errorf(`%s: allow_host_jobs must be "off", "trusted" or "all", not %q`, path, c.AllowHostJobs)
	}
	if !c.RunsDockerJobs() && c.HostMode() == runnerapi.HostOff {
		return nil, fmt.Errorf("%s: docker_jobs = false needs allow_host_jobs, or the runner can't run anything", path)
	}
	return &c, nil
}

// Save writes the config with owner-only permissions (it holds the token).
func (c *Config) Save(path string) error {
	b, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Runner fetches and runs jobs.
type Runner struct {
	cfg     Config
	client  *Client
	exec    Executor
	log     *slog.Logger
	version string

	mu      sync.Mutex
	running map[int64]context.CancelFunc
	host    map[int64]bool // running host jobs
	slot    chan struct{} // signalled when a job finishes
	wg      sync.WaitGroup
	// loadFn reports host load (replaceable in tests).
	loadFn func() runnerapi.Load
}

// New creates a runner.
func New(cfg Config, exec Executor, log *slog.Logger, version string) *Runner {
	if cfg.Capacity < 1 {
		cfg.Capacity = 1
	}
	return &Runner{
		cfg: cfg, client: NewClient(cfg.URL, cfg.Token), exec: exec, log: log, version: version,
		running: map[int64]context.CancelFunc{}, host: map[int64]bool{}, slot: make(chan struct{}, 1), loadFn: HostLoad,
	}
}

func (r *Runner) runningIDs() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]int64, 0, len(r.running))
	for id := range r.running {
		ids = append(ids, id)
	}
	return ids
}

// busy reports whether the runner can't take another job: every slot is
// taken, or a host job is running (host jobs run alone).
func (r *Runner) busy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.running) >= r.cfg.Capacity || len(r.host) > 0
}

func (r *Runner) cancelJob(id int64) {
	r.mu.Lock()
	cancel := r.running[id]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ErrUnregistered means the server doesn't know this runner any more.
var ErrUnregistered = errors.New("the server rejected this runner's token (it was removed); register it again")

// Run fetches jobs until ctx ends, then waits for running jobs to finish.
// Cancelling jobCtx stops running jobs too (e.g. on a second interrupt).
func (r *Runner) Run(ctx, jobCtx context.Context) error {
	if err := os.MkdirAll(r.cfg.WorkDir, 0o700); err != nil {
		return err
	}
	if err := r.exec.Cleanup(ctx); err != nil {
		r.log.Warn("cleaning up leftovers failed", "err", err)
	}
	r.log.Info("runner started", "name", r.cfg.Name, "server", r.cfg.URL, "labels", strings.Join(r.cfg.EffectiveLabels(), ","),
		"capacity", r.cfg.Capacity, "host_jobs", r.cfg.HostMode(), "docker_jobs", r.cfg.RunsDockerJobs())
	hbCtx, stopHB := context.WithCancel(context.Background())
	hbDone := make(chan error, 1)
	go func() { hbDone <- r.heartbeats(hbCtx) }()
	defer func() {
		r.wg.Wait()
		stopHB()
		<-hbDone
	}()

	backoff := time.Second
	for ctx.Err() == nil {
		if r.busy() {
			select {
			case <-ctx.Done():
			case <-r.slot:
			case <-time.After(5 * time.Second):
			}
			continue
		}
		spec, err := r.client.Fetch(ctx, runnerapi.FetchRequest{
			Capacity: r.cfg.Capacity, Running: r.runningIDs(), Labels: r.cfg.EffectiveLabels(), Load: r.loadFn(), Version: r.version,
			HostMode: r.cfg.HostMode(),
		})
		var ae *APIError
		switch {
		case ctx.Err() != nil:
			continue
		case errors.As(err, &ae) && ae.Status == http.StatusUnauthorized:
			return ErrUnregistered
		case err != nil:
			r.log.Warn("fetching jobs failed; retrying", "err", err, "in", backoff)
			select {
			case <-ctx.Done():
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		if spec != nil {
			r.start(jobCtx, spec)
		}
	}
	if n := len(r.runningIDs()); n > 0 {
		r.log.Info("stopping: waiting for running jobs to finish (interrupt again to cancel them)", "jobs", n)
	}
	return nil
}

func (r *Runner) start(parent context.Context, spec *runnerapi.JobSpec) {
	ctx, cancel := context.WithCancel(parent)
	r.mu.Lock()
	r.running[spec.ID] = cancel
	if spec.Host {
		r.host[spec.ID] = true
	}
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			cancel()
			r.mu.Lock()
			delete(r.running, spec.ID)
			delete(r.host, spec.ID)
			r.mu.Unlock()
			select {
			case r.slot <- struct{}{}:
			default:
			}
		}()
		r.runJob(ctx, spec)
	}()
}

// runJob executes one job and reports its result.
func (r *Runner) runJob(ctx context.Context, spec *runnerapi.JobSpec) {
	log := r.log.With("job", spec.ID, "repo", spec.Repo, "run", spec.RunNumber)
	log.Info("job started", "name", spec.Name)
	m := &masker{}
	for _, v := range spec.Secrets {
		m.add(v)
	}
	m.add(spec.Git.Token)
	r.addJobEnv(spec)
	j := &jobRun{r: r, spec: spec, mask: m, steps: map[int]runnerapi.StepUpdate{}, stepsCtx: map[string]any{}}
	j.logs = newLogStream(r.client, spec.ID, m, func() {
		j.cancelled.Store(true)
		r.cancelJob(spec.ID)
	})
	j.logs.maxBytes = spec.MaxLogBytes
	timeout := time.Duration(spec.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 6 * time.Hour
	}
	jctx, cancel := context.WithTimeout(ctx, timeout)
	conclusion, reason, outputs := j.execute(jctx)
	if ctx.Err() != nil && !j.cancelled.Load() && !errors.Is(jctx.Err(), context.DeadlineExceeded) {
		// The runner itself is shutting down.
		conclusion, reason = runnerapi.ConclusionCancelled, "The runner was stopped"
	}
	cancel()
	if j.env != nil {
		if spec.Host {
			j.printf(runnerapi.StepComplete, "Cleaning up the job's files, processes and Docker objects")
		} else {
			j.printf(runnerapi.StepComplete, "Cleaning up the job's containers")
		}
		if err := j.env.Close(context.Background()); err != nil {
			j.printf(runnerapi.StepComplete, "##[warning]%v", err)
		}
	}
	j.printf(runnerapi.StepComplete, "Job finished: %s", conclusion)
	j.logs.Close()
	steps := make([]runnerapi.StepUpdate, 0, len(j.steps))
	for _, u := range j.steps {
		steps = append(steps, u)
	}
	cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer ccancel()
	err := retry(cctx, func() error {
		return r.client.Complete(cctx, spec.ID, runnerapi.CompleteRequest{Conclusion: conclusion, FailureReason: reason, Outputs: outputs, Steps: steps})
	})
	var ae *APIError
	if err != nil && !(errors.As(err, &ae) && ae.Gone()) {
		log.Error("reporting the job result failed", "err", err)
	}
	log.Info("job finished", "conclusion", conclusion)
}

// addJobEnv gives steps the job token and the repository's registry:
// TGP_TOKEN, TGP_REGISTRY and TGP_IMAGE, also available as tgp.token,
// tgp.registry and tgp.image (and the github.* aliases) in expressions.
func (r *Runner) addJobEnv(spec *runnerapi.JobSpec) {
	host := spec.Registry.Host
	if host == "" {
		if u, err := url.Parse(r.cfg.URL); err == nil {
			host = strings.ToLower(u.Host)
		}
	}
	image := host + "/" + spec.Registry.Path
	env := map[string]string{}
	for k, v := range spec.Env {
		env[k] = v
	}
	env["TGP_TOKEN"], env["TGP_REGISTRY"], env["TGP_IMAGE"] = spec.Git.Token, host, image
	spec.Env = env
	ctxs := map[string]any{}
	for k, v := range spec.Contexts {
		ctxs[k] = v
	}
	for _, name := range []string{"github", "tgp"} {
		m := map[string]any{}
		if old, ok := ctxs[name].(map[string]any); ok {
			for k, v := range old {
				m[k] = v
			}
		}
		m["token"], m["registry"], m["image"] = spec.Git.Token, host, image
		ctxs[name] = m
	}
	spec.Contexts = ctxs
}

// heartbeats reports liveness and load, and stops jobs the server cancels.
func (r *Runner) heartbeats(ctx context.Context) error {
	t := time.NewTicker(heartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		resp, err := r.client.Heartbeat(ctx, runnerapi.HeartbeatRequest{Capacity: r.cfg.Capacity, Running: r.runningIDs(), Load: r.loadFn()})
		if err != nil {
			if ctx.Err() == nil {
				r.log.Warn("heartbeat failed", "err", err)
			}
			continue
		}
		for _, id := range append(resp.Cancel, resp.Unknown...) {
			r.log.Info("stopping job at the server's request", "job", id)
			r.cancelJob(id)
		}
	}
}

// heartbeatEvery is the heartbeat period (tests shorten it).
var heartbeatEvery = runnerapi.HeartbeatInterval

// HostLoad reports CPU load (1-minute load average per core) and memory
// use on Linux; elsewhere it reports zero.
func HostLoad() runnerapi.Load {
	var l runnerapi.Load
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if v, err := strconv.ParseFloat(f[0], 64); err == nil {
				l.CPUPercent = v / float64(runtime.NumCPU()) * 100
			}
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		var total, avail float64
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			v, _ := strconv.ParseFloat(f[1], 64)
			switch f[0] {
			case "MemTotal:":
				total = v
			case "MemAvailable:":
				avail = v
			}
		}
		if total > 0 {
			l.MemoryPercent = (1 - avail/total) * 100
		}
	}
	return l
}
