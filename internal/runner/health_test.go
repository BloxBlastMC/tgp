package runner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/workflow"
)

// healthDocker fakes the docker CLI: inspect reports "starting" for the
// first two calls, then final; every call is recorded.
func healthDocker(t *testing.T, final string) (bin, dir string) {
	t.Helper()
	dir = t.TempDir()
	script := `#!/bin/sh
d="` + dir + `"
echo "$*" >> "$d/calls"
case "$1" in
  inspect)
    n=$(cat "$d/n" 2>/dev/null || echo 0); n=$((n+1)); echo $n > "$d/n"
    if [ $n -le 2 ]; then echo "running starting"; else echo "` + final + `"; fi ;;
  logs) echo "mysqld: ready? no" ;;
esac
exit 0
`
	bin = filepath.Join(dir, "docker")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

func TestServiceHealthcheck(t *testing.T) {
	old := healthPoll
	healthPoll = 10 * time.Millisecond
	t.Cleanup(func() { healthPoll = old })
	spec := &runnerapi.JobSpec{ID: 3, Image: "debian:bookworm", Services: []runnerapi.Service{{
		Name: "db", Image: "mysql:8.4",
		Healthcheck: &workflow.Healthcheck{Cmd: "mysqladmin ping -h 127.0.0.1", Interval: 1, Retries: 5, StartPeriod: 10},
	}}}

	bin, dir := healthDocker(t, "running healthy")
	var log bytes.Buffer
	d := &Docker{Binary: bin, RunnerID: 1}
	env, err := d.Prepare(context.Background(), spec, nil, &log)
	if err != nil {
		t.Fatalf("prepare: %v\n%s", err, log.String())
	}
	_ = env.Close(context.Background())
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if !strings.Contains(string(calls), "--health-cmd mysqladmin ping -h 127.0.0.1 --health-interval 1s --health-retries 5 --health-start-period 10s") {
		t.Fatalf("health flags missing:\n%s", calls)
	}
	// The job container starts only after the service is healthy.
	c := string(calls)
	if strings.Index(c, "--name tgp-job-3 ") < strings.LastIndex(c, "inspect") {
		t.Fatalf("job container started before the service was healthy:\n%s", c)
	}
	if !strings.Contains(log.String(), "Service db is healthy") {
		t.Fatalf("log: %s", log.String())
	}

	bin, _ = healthDocker(t, "running unhealthy")
	log.Reset()
	d = &Docker{Binary: bin, RunnerID: 1}
	env, err = d.Prepare(context.Background(), spec, nil, &log)
	if err == nil || !strings.Contains(err.Error(), "service db isn't healthy: its healthcheck failed") {
		t.Fatalf("unhealthy service: %v", err)
	}
	if !strings.Contains(log.String(), "mysqld: ready? no") {
		t.Fatalf("service logs not shown: %s", log.String())
	}
	_ = env.Close(context.Background())

	// A service without a healthcheck isn't waited for.
	bin, dir = healthDocker(t, "running healthy")
	plain := *spec
	plain.Services = []runnerapi.Service{{Name: "cache", Image: "redis:7"}}
	d = &Docker{Binary: bin, RunnerID: 1}
	env, err = d.Prepare(context.Background(), &plain, nil, &log)
	if err != nil {
		t.Fatal(err)
	}
	_ = env.Close(context.Background())
	calls, _ = os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains("\n"+string(calls), "\ninspect ") || strings.Contains(string(calls), "--health-cmd") {
		t.Fatalf("plain service was health-checked:\n%s", calls)
	}
}
