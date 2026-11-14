package runner

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
)

// TestDockerExecutor runs a real container. It needs a Docker daemon, so
// it only runs with TGP_DOCKER_TESTS=1.
func TestDockerExecutor(t *testing.T) {
	if os.Getenv("TGP_DOCKER_TESTS") != "1" {
		t.Skip("set TGP_DOCKER_TESTS=1 to run Docker tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	d := &Docker{RunnerID: 999999}
	if err := d.Check(ctx); err != nil {
		t.Fatal(err)
	}
	spec := &runnerapi.JobSpec{
		ID: time.Now().UnixNano() % 1e9, Image: "alpine:3.20",
		Resources: runnerapi.Resources{CPUs: 0.5, MemoryBytes: 64 << 20, Pids: 64},
		Services:  []runnerapi.Service{{Name: "cache", Image: "redis:7-alpine"}},
	}
	var log bytes.Buffer
	env, err := d.Prepare(ctx, spec, map[string]string{"FROM_CONTAINER": "yes"}, &log)
	if env != nil {
		defer env.Close(ctx)
	}
	if err != nil {
		t.Fatalf("prepare: %v\n%s", err, log.String())
	}
	run := func(script string, envs map[string]string) (int, string) {
		var out bytes.Buffer
		code, err := env.Exec(ctx, ExecOptions{Cmd: []string{"sh", "-c", script}, Env: envs, Dir: env.Workspace(), Output: &out})
		if err != nil {
			t.Fatalf("exec %q: %v", script, err)
		}
		return code, out.String()
	}
	if code, out := run(`echo "$SECRET $FROM_CONTAINER $(pwd)"`, map[string]string{"SECRET": "s3cret"}); code != 0 || strings.TrimSpace(out) != "s3cret yes /workspace" {
		t.Fatalf("env: %d %q", code, out)
	}
	if code, _ := run("exit 7", nil); code != 7 {
		t.Fatalf("exit code: %d", code)
	}
	// Limits are applied.
	if _, out := run("cat /sys/fs/cgroup/memory.max 2>/dev/null || cat /sys/fs/cgroup/memory/memory.limit_in_bytes", nil); strings.TrimSpace(out) != "67108864" {
		t.Errorf("memory limit: %q", out)
	}
	if _, out := run("cat /sys/fs/cgroup/pids.max 2>/dev/null || cat /sys/fs/cgroup/pids/pids.max", nil); strings.TrimSpace(out) != "64" {
		t.Errorf("pids limit: %q", out)
	}
	// Service containers are reachable by name.
	if code, out := run("for i in $(seq 1 20); do nc -z cache 6379 && echo up && exit 0; sleep 0.5; done; exit 1", nil); code != 0 {
		t.Errorf("service: %d %s", code, out)
	}
	// Files and copies.
	if err := env.WriteFile(ctx, env.TempDir()+"/f", []byte("data")); err != nil {
		t.Fatal(err)
	}
	if b, _ := env.ReadFile(ctx, env.TempDir()+"/f"); string(b) != "data" {
		t.Fatalf("read: %q", b)
	}
	host := t.TempDir()
	_ = os.WriteFile(host+"/README", []byte("hi"), 0o644)
	if err := env.CopyIn(ctx, host, env.Workspace()+"/sub"); err != nil {
		t.Fatal(err)
	}
	if _, out := run("cat sub/README", nil); out != "hi" {
		t.Fatalf("copy: %q", out)
	}
	// Cancelling stops a running command when the environment closes.
	cctx, ccancel := context.WithTimeout(ctx, time.Second)
	defer ccancel()
	start := time.Now()
	_, _ = env.Exec(cctx, ExecOptions{Cmd: []string{"sleep", "60"}, Output: &bytes.Buffer{}})
	if time.Since(start) > 15*time.Second {
		t.Fatal("cancel didn't stop the command")
	}
	if err := env.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
}
