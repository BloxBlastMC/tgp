package runner

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/expr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
)

// jobRun is one job being executed.
type jobRun struct {
	r     *Runner
	spec  *runnerapi.JobSpec
	logs  *logStream
	mask  *masker
	env   Env
	shell string
	path  string // the container's default PATH

	cancelled atomic.Bool // the server asked us to stop
	steps     map[int]runnerapi.StepUpdate

	// Expression state.
	stepsCtx map[string]any
	baseEnv  map[string]string // workflow + job env
	addedEnv map[string]string // from $TGP_ENV files
	addPath  []string          // from $TGP_PATH files
	failed   bool

	home       string      // $HOME in the environment (lazily read)
	cacheSaves []cacheSave // caches to save after the steps
}

func (j *jobRun) printf(step int, format string, args ...any) {
	for _, line := range strings.Split(strings.TrimRight(fmt.Sprintf(format, args...), "\n"), "\n") {
		j.logs.Line(step, line)
	}
}

func (j *jobRun) writer(step int) *lineWriter { return &lineWriter{step: step, out: j.logs} }

// exprContext builds the expression context for the current point of the
// job.
func (j *jobRun) exprContext(ctx context.Context, env map[string]string) *expr.Context {
	c := map[string]any{}
	for k, v := range j.spec.Contexts {
		c[k] = v
	}
	secrets := map[string]any{}
	for k, v := range j.spec.Secrets {
		secrets[k] = v
	}
	envCtx := map[string]any{}
	for k, v := range env {
		envCtx[k] = v
	}
	status := "success"
	if j.failed {
		status = "failure"
	}
	if ctx.Err() != nil {
		status = "cancelled"
	}
	c["secrets"] = secrets
	c["env"] = envCtx
	c["steps"] = j.stepsCtx
	c["job"] = map[string]any{"status": status}
	ws, tmp := "", ""
	if j.env != nil {
		ws, tmp = j.env.Workspace(), j.env.TempDir()
	}
	c["runner"] = map[string]any{"os": "Linux", "arch": runtime.GOARCH, "name": j.r.cfg.Name, "temp": tmp, "workspace": ws}
	return &expr.Context{Contexts: c, HashFiles: func(p []string) string { return j.hashFiles(ctx, p) }, Status: func(fn string) bool {
		switch fn {
		case "success":
			return !j.failed && ctx.Err() == nil
		case "failure":
			return j.failed
		case "cancelled":
			return ctx.Err() != nil
		}
		return true
	}}
}

// evalEnv evaluates a map of env values in order of keys.
func (j *jobRun) evalEnv(ctx context.Context, m map[string]string, base map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	c := j.exprContext(ctx, base)
	for k, v := range m {
		s, err := expr.Interpolate(v, c)
		if err != nil {
			return nil, fmt.Errorf("env %s: %w", k, err)
		}
		out[k] = s
	}
	return out, nil
}

func (j *jobRun) report(ctx context.Context, u runnerapi.StepUpdate) {
	j.steps[u.Number] = u
	resp, err := j.r.client.Update(ctx, j.spec.ID, runnerapi.UpdateRequest{Steps: []runnerapi.StepUpdate{u}})
	var ae *APIError
	switch {
	case errors.As(err, &ae) && ae.Gone():
		j.cancelled.Store(true)
		j.r.cancelJob(j.spec.ID)
	case err != nil:
		j.r.log.Warn("step update failed", "job", j.spec.ID, "err", err)
	case resp.Cancel:
		j.cancelled.Store(true)
		j.r.cancelJob(j.spec.ID)
	}
}

func now() *time.Time { t := time.Now().UTC(); return &t }

// execute runs the job and returns its conclusion.
func (j *jobRun) execute(ctx context.Context) (conclusion, reason string, outputs map[string]string) {
	spec := j.spec
	j.printf(runnerapi.StepSetup, "Runner: %s (tgp-runner %s, %s/%s)", j.r.cfg.Name, j.r.version, runtime.GOOS, runtime.GOARCH)
	j.printf(runnerapi.StepSetup, "Job: %s (run #%d, attempt %d) in %s", spec.Name, spec.RunNumber, spec.Attempt, spec.Repo)
	j.printf(runnerapi.StepSetup, "Limits: %g CPUs, %d MiB memory, %d processes, %d minute timeout",
		spec.Resources.CPUs, spec.Resources.MemoryBytes>>20, spec.Resources.Pids, spec.TimeoutSeconds/60)

	fail := func(step int, format string, args ...any) (string, string, map[string]string) {
		msg := fmt.Sprintf(format, args...)
		j.printf(step, "##[error]%s", msg)
		return runnerapi.ConclusionFailure, msg, nil
	}
	wfEnv, err := j.evalEnv(ctx, spec.WorkflowEnv, nil)
	if err != nil {
		j.report(ctx, runnerapi.StepUpdate{Number: 0, Status: runnerapi.StatusCompleted, Conclusion: runnerapi.ConclusionFailure, CompletedAt: now()})
		return fail(0, "workflow %v", err)
	}
	j.baseEnv, err = j.evalEnv(ctx, spec.JobEnv, wfEnv)
	if err != nil {
		j.report(ctx, runnerapi.StepUpdate{Number: 0, Status: runnerapi.StatusCompleted, Conclusion: runnerapi.ConclusionFailure, CompletedAt: now()})
		return fail(0, "job %v", err)
	}
	containerEnv, err := j.evalEnv(ctx, spec.ContainerEnv, nil)
	if err != nil {
		return fail(0, "container %v", err)
	}
	prep := *spec
	prep.Services = make([]runnerapi.Service, len(spec.Services))
	for i, svc := range spec.Services {
		e, err := j.evalEnv(ctx, svc.Env, nil)
		if err != nil {
			return fail(0, "service %s %v", svc.Name, err)
		}
		prep.Services[i] = runnerapi.Service{Name: svc.Name, Image: svc.Image, Env: e}
	}

	setupLog := j.writer(runnerapi.StepSetup)
	env, err := j.r.exec.Prepare(ctx, &prep, containerEnv, setupLog)
	setupLog.Flush()
	j.env = env
	if err != nil {
		j.report(ctx, runnerapi.StepUpdate{Number: 0, Status: runnerapi.StatusCompleted, Conclusion: runnerapi.ConclusionFailure, CompletedAt: now()})
		if ctx.Err() != nil {
			return runnerapi.ConclusionCancelled, "", nil
		}
		return fail(0, "Setting up the job failed: %v", err)
	}
	j.shell = j.detectShell(ctx)
	j.path = j.defaultPath(ctx)
	j.report(ctx, runnerapi.StepUpdate{Number: 0, Status: runnerapi.StatusCompleted, Conclusion: runnerapi.ConclusionSuccess, CompletedAt: now()})

	for _, st := range spec.Steps {
		j.runStep(ctx, st)
	}
	j.saveCaches(ctx)

	// Job outputs, evaluated after all steps.
	outputs = map[string]string{}
	oc := j.exprContext(ctx, j.mergedEnv(nil))
	for k, v := range spec.Outputs {
		s, err := expr.Interpolate(v, oc)
		if err != nil {
			j.printf(runnerapi.StepComplete, "##[warning]output %s: %v", k, err)
			continue
		}
		if masked := j.mask.mask(s); masked != s {
			j.printf(runnerapi.StepComplete, "##[warning]output %s contains a secret and was dropped", k)
			continue
		}
		outputs[k] = s
	}
	switch {
	case j.cancelled.Load():
		return runnerapi.ConclusionCancelled, "", outputs
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return runnerapi.ConclusionTimedOut, fmt.Sprintf("The job exceeded its %d minute timeout", spec.TimeoutSeconds/60), outputs
	case ctx.Err() != nil:
		return runnerapi.ConclusionCancelled, "", outputs
	case j.failed:
		return runnerapi.ConclusionFailure, "", outputs
	}
	return runnerapi.ConclusionSuccess, "", outputs
}

func (j *jobRun) mergedEnv(step map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range j.baseEnv {
		out[k] = v
	}
	for k, v := range j.addedEnv {
		out[k] = v
	}
	for k, v := range step {
		out[k] = v
	}
	return out
}

func (j *jobRun) detectShell(ctx context.Context) string {
	code, err := j.env.Exec(ctx, ExecOptions{Cmd: []string{"sh", "-c", "command -v bash >/dev/null"}, Output: discard{}})
	if err == nil && code == 0 {
		return "bash"
	}
	return "sh"
}

func (j *jobRun) defaultPath(ctx context.Context) string {
	var out strings.Builder
	if code, err := j.env.Exec(ctx, ExecOptions{Cmd: []string{"sh", "-c", "printf %s \"$PATH\""}, Output: &out}); err == nil && code == 0 && out.Len() > 0 {
		return out.String()
	}
	return "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func (j *jobRun) runStep(ctx context.Context, st runnerapi.Step) {
	n := st.Number
	if ctx.Err() != nil {
		j.report(context.Background(), runnerapi.StepUpdate{Number: n, Status: runnerapi.StatusCompleted, Conclusion: runnerapi.ConclusionCancelled})
		return
	}
	c := j.exprContext(ctx, j.mergedEnv(nil))
	ok, err := expr.Condition(st.If, c)
	if err != nil {
		j.printf(n, "##[error]if: %v", err)
		j.failed = true
		j.report(ctx, runnerapi.StepUpdate{Number: n, Status: runnerapi.StatusCompleted, Conclusion: runnerapi.ConclusionFailure, StartedAt: now(), CompletedAt: now()})
		return
	}
	if !ok {
		j.report(ctx, runnerapi.StepUpdate{Number: n, Status: runnerapi.StatusCompleted, Conclusion: runnerapi.ConclusionSkipped})
		j.setStepCtx(st, "skipped", "skipped", nil)
		return
	}
	started := now()
	j.report(ctx, runnerapi.StepUpdate{Number: n, Status: runnerapi.StatusInProgress, StartedAt: started})
	sctx := ctx
	if st.TimeoutMinutes > 0 {
		var cancel context.CancelFunc
		sctx, cancel = context.WithTimeout(ctx, time.Duration(st.TimeoutMinutes)*time.Minute)
		defer cancel()
	}
	var outputs map[string]string
	if st.Uses != "" {
		outputs, err = j.builtin(sctx, st)
	} else {
		outputs, err = j.runScript(sctx, st)
	}
	outcome := runnerapi.ConclusionSuccess
	switch {
	case err == nil:
	case ctx.Err() != nil:
		outcome = runnerapi.ConclusionCancelled
		j.printf(n, "##[error]The step was stopped: the job was cancelled or timed out")
	case errors.Is(sctx.Err(), context.DeadlineExceeded):
		outcome = runnerapi.ConclusionFailure
		j.printf(n, "##[error]The step exceeded its %d minute timeout", st.TimeoutMinutes)
	default:
		outcome = runnerapi.ConclusionFailure
		j.printf(n, "##[error]%v", err)
	}
	conclusion := outcome
	if outcome == runnerapi.ConclusionFailure {
		if st.ContinueOnError {
			conclusion = runnerapi.ConclusionSuccess
			j.printf(n, "##[warning]continue-on-error is set; the job continues")
		} else {
			j.failed = true
		}
	}
	j.setStepCtx(st, outcome, conclusion, outputs)
	j.report(context.Background(), runnerapi.StepUpdate{Number: n, Status: runnerapi.StatusCompleted, Conclusion: outcome, StartedAt: started, CompletedAt: now()})
}

func (j *jobRun) setStepCtx(st runnerapi.Step, outcome, conclusion string, outputs map[string]string) {
	if st.ID == "" {
		return
	}
	o := map[string]any{}
	for k, v := range outputs {
		o[k] = v
	}
	j.stepsCtx[st.ID] = map[string]any{"outcome": outcome, "conclusion": conclusion, "outputs": o}
}

// shellCommand returns the command for a shell and the script's extension.
func shellCommand(shell, script string) ([]string, string) {
	switch shell {
	case "bash":
		return []string{"bash", "--noprofile", "--norc", "-eo", "pipefail", script}, ".sh"
	case "sh":
		return []string{"sh", "-e", script}, ".sh"
	case "python", "python3":
		return []string{shell, script}, ".py"
	case "pwsh":
		return []string{"pwsh", "-command", ". '" + script + "'"}, ".ps1"
	case "node":
		return []string{"node", script}, ".js"
	}
	fields := strings.Fields(shell)
	for i, f := range fields {
		fields[i] = strings.ReplaceAll(f, "{0}", script)
	}
	return fields, ""
}

func (j *jobRun) workDir(dir string) string {
	if dir == "" {
		dir = j.spec.Defaults.WorkingDirectory
	}
	if dir == "" {
		return j.env.Workspace()
	}
	if path.IsAbs(dir) {
		return path.Clean(dir)
	}
	return path.Join(j.env.Workspace(), dir)
}

func (j *jobRun) runScript(ctx context.Context, st runnerapi.Step) (map[string]string, error) {
	n := st.Number
	stepEnv, err := j.evalEnv(ctx, st.Env, j.mergedEnv(nil))
	if err != nil {
		return nil, err
	}
	script, err := expr.Interpolate(st.Run, j.exprContext(ctx, stepEnv))
	if err != nil {
		return nil, err
	}
	dir, err := expr.Interpolate(st.WorkingDirectory, j.exprContext(ctx, stepEnv))
	if err != nil {
		return nil, err
	}
	shell := st.Shell
	if shell == "" {
		shell = j.spec.Defaults.Shell
	}
	if shell == "" {
		shell = j.shell
	}
	tmp := j.env.TempDir()
	scriptPath := fmt.Sprintf("%s/step-%d", tmp, n)
	cmd, ext := shellCommand(shell, scriptPath)
	if ext != "" {
		scriptPath += ext
		cmd, _ = shellCommand(shell, scriptPath)
	}
	files := map[string]string{
		"OUTPUT": fmt.Sprintf("%s/output-%d", tmp, n),
		"ENV":    fmt.Sprintf("%s/env-%d", tmp, n),
		"PATH":   fmt.Sprintf("%s/path-%d", tmp, n),
	}
	for _, f := range files {
		if err := j.env.WriteFile(ctx, f, nil); err != nil {
			return nil, err
		}
	}
	if err := j.env.WriteFile(ctx, scriptPath, []byte(script)); err != nil {
		return nil, err
	}
	env := map[string]string{}
	for k, v := range j.spec.Env {
		env[k] = v
	}
	for k, v := range stepEnv {
		env[k] = v
	}
	for name, f := range files {
		env["TGP_"+name], env["GITHUB_"+name] = f, f
	}
	env["TGP_WORKSPACE"], env["GITHUB_WORKSPACE"] = j.env.Workspace(), j.env.Workspace()
	env["RUNNER_TEMP"], env["RUNNER_OS"], env["RUNNER_NAME"] = tmp, "Linux", j.r.cfg.Name
	env["GITHUB_ACTION"] = strconv.Itoa(n)
	if len(j.addPath) > 0 {
		env["PATH"] = strings.Join(j.addPath, ":") + ":" + j.path
	}
	// Show the script, folded, like GitHub does.
	j.printf(n, "::group::%s", firstLine(script))
	for _, l := range strings.Split(strings.TrimRight(script, "\n"), "\n") {
		j.logs.Line(n, l)
	}
	j.printf(n, "shell: %s", strings.Join(cmd, " "))
	j.printf(n, "::endgroup::")
	out := j.writer(n)
	code, err := j.env.Exec(ctx, ExecOptions{Cmd: cmd, Env: env, Dir: j.workDir(dir), Output: out})
	out.Flush()
	if err != nil {
		return nil, err
	}
	readCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	outputs := map[string]string{}
	if b, err := j.env.ReadFile(readCtx, files["OUTPUT"]); err == nil {
		for k, v := range parseEnvFile(string(b)) {
			outputs[k] = v
		}
	}
	if b, err := j.env.ReadFile(readCtx, files["ENV"]); err == nil {
		for k, v := range parseEnvFile(string(b)) {
			if j.addedEnv == nil {
				j.addedEnv = map[string]string{}
			}
			j.addedEnv[k] = v
		}
	}
	if b, err := j.env.ReadFile(readCtx, files["PATH"]); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				j.addPath = append([]string{l}, j.addPath...)
			}
		}
	}
	if code != 0 {
		return outputs, fmt.Errorf("Process completed with exit code %d", code)
	}
	return outputs, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return "Run " + s
}

// parseEnvFile reads NAME=value lines and NAME<<DELIM heredocs.
func parseEnvFile(s string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if i := strings.Index(line, "<<"); i > 0 && (strings.IndexByte(line, '=') < 0 || strings.IndexByte(line, '=') > i) {
			name, delim := line[:i], line[i+2:]
			var val []string
			for sc.Scan() {
				if sc.Text() == delim {
					break
				}
				val = append(val, sc.Text())
			}
			out[name] = strings.Join(val, "\n")
			continue
		}
		if i := strings.IndexByte(line, '='); i > 0 {
			out[line[:i]] = line[i+1:]
		}
	}
	return out
}

// builtin runs a uses: step and returns its outputs.
func (j *jobRun) builtin(ctx context.Context, st runnerapi.Step) (map[string]string, error) {
	with := map[string]string{}
	c := j.exprContext(ctx, j.mergedEnv(nil))
	for k, v := range st.With {
		s, err := expr.Interpolate(v, c)
		if err != nil {
			return nil, fmt.Errorf("with.%s: %w", k, err)
		}
		with[k] = s
	}
	switch st.Uses {
	case "checkout":
		return nil, j.checkout(ctx, st.Number, with)
	case "upload-artifact":
		return nil, j.uploadArtifact(ctx, st.Number, with)
	case "download-artifact":
		return nil, j.downloadArtifact(ctx, st.Number, with)
	case "cache":
		return j.cache(ctx, st.Number, with)
	}
	return nil, fmt.Errorf("unknown built-in step %q (this runner may be older than the server)", st.Uses)
}

// checkout clones the repository on the runner host with the job token and
// copies it into the workspace, so job images don't need git.
func (j *jobRun) checkout(ctx context.Context, n int, with map[string]string) error {
	g := j.spec.Git
	depth := 1
	if v := with["fetch-depth"]; v != "" {
		d, err := strconv.Atoi(v)
		if err != nil || d < 0 {
			return fmt.Errorf("fetch-depth must be a number ≥ 0, got %q", v)
		}
		depth = d
	}
	target, ref := g.SHA, g.Ref
	if v := with["ref"]; v != "" {
		target, ref = v, ""
	}
	dir, err := os.MkdirTemp(j.r.cfg.WorkDir, fmt.Sprintf("checkout-%d-", j.spec.ID))
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	auth := base64.StdEncoding.EncodeToString([]byte(g.Username + ":" + g.Token))
	env := append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Basic "+auth,
	)
	out := j.writer(n)
	defer out.Flush()
	git := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		cmd.Env, cmd.Stdout, cmd.Stderr = env, out, out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git %s: %w", args[0], err)
		}
		return nil
	}
	url := strings.TrimRight(j.r.cfg.URL, "/") + g.URL
	j.printf(n, "Checking out %s at %s", j.spec.Repo, short(target))
	if err := git("init", "-q"); err != nil {
		return err
	}
	if err := git("remote", "add", "origin", url); err != nil {
		return err
	}
	fetch := []string{"fetch", "--no-tags", "--prune", "--no-recurse-submodules"}
	if depth > 0 {
		fetch = append(fetch, "--depth="+strconv.Itoa(depth))
	}
	if depth == 0 {
		if err := git(append(fetch, "origin", "+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*")...); err != nil {
			return err
		}
	}
	if err := git(append(fetch, "origin", target)...); err != nil {
		return err
	}
	if err := git("checkout", "-q", "--force", "FETCH_HEAD"); err != nil {
		return err
	}
	if b, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
		if err := git("checkout", "-q", "-B", b); err != nil {
			return err
		}
	}
	dest := j.env.Workspace()
	if p := with["path"]; p != "" {
		clean := path.Clean("/" + p)
		dest = path.Join(dest, clean)
	}
	// Leave no credentials in the copied repository.
	_ = os.Remove(filepath.Join(dir, ".git", "FETCH_HEAD"))
	if err := j.env.CopyIn(ctx, dir, dest); err != nil {
		return err
	}
	j.printf(n, "Checked out %s into %s", short(g.SHA), dest)
	return nil
}

func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}
