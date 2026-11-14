package server

import (
	"strings"
	"testing"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

func TestServerHealth(t *testing.T) {
	p := newPREnv(t)
	p.admin.mustDo("POST", "/repos", map[string]any{"name": "empty"}, 201)
	p.commit("main", "big.bin", strings.Repeat("tgp health ", 20000), "Big file")

	// Admins only.
	p.mia.mustDo("GET", "/admin/health", nil, 403)
	p.tom.mustDo("GET", "/admin/health", nil, 403)

	var h apigen.ServerHealth
	p.admin.mustDo("GET", "/admin/health", nil, 200).decode(t, &h)
	if h.Database.Driver != "sqlite" || !h.Database.Ok || h.Database.PendingMigrations != 0 || h.Database.SchemaVersion == "" || h.Database.SizeBytes == nil || *h.Database.SizeBytes <= 0 {
		t.Fatalf("database: %+v", h.Database)
	}
	if !h.Storage.Ok || h.Storage.Type != "local" {
		t.Fatalf("storage: %+v", h.Storage)
	}
	if len(h.Disks) == 0 || h.Disks[0].TotalBytes <= 0 || len(h.Disks[0].Uses) == 0 {
		t.Fatalf("disks: %+v", h.Disks)
	}
	// Repos: both listed, the one with the big file first.
	if h.Repos.Count != 2 || len(h.Repos.Items) != 2 || h.Repos.Items[0].Name != "game" || h.Repos.Items[0].GitBytes <= 0 ||
		h.Repos.Items[0].TotalBytes < h.Repos.Items[1].TotalBytes || h.Repos.GitBytes < h.Repos.Items[0].GitBytes {
		t.Fatalf("repos: %+v", h.Repos)
	}
	if h.Counts.Users != 3 || h.Counts.Repos != 2 || h.Counts.Sessions < 1 {
		t.Fatalf("counts: %+v", h.Counts)
	}
	if h.Ci.QueuedJobs != 0 || h.Ci.RunnersOnline != 0 || h.Webhooks.PendingDeliveries != 0 {
		t.Fatalf("ci/webhooks: %+v %+v", h.Ci, h.Webhooks)
	}
	if h.Status == "" || len(h.Checks) < 5 || h.Checks[0].Status != h.Status {
		t.Fatalf("checks: %s %+v", h.Status, h.Checks)
	}
	names := map[string]bool{}
	for _, c := range h.Checks {
		names[c.Name] = true
	}
	for _, n := range []string{"database", "migrations", "storage", "runners", "ci_queue", "webhooks"} {
		if !names[n] {
			t.Errorf("missing check %q in %+v", n, h.Checks)
		}
	}

	// Sizes are cached; a new repository is measured right away, and
	// refresh=true measures everything again.
	measured := h.Repos.MeasuredAt
	time.Sleep(10 * time.Millisecond)
	p.admin.mustDo("GET", "/admin/health", nil, 200).decode(t, &h)
	if !h.Repos.MeasuredAt.Equal(measured) {
		t.Fatalf("sizes re-measured without refresh")
	}
	p.admin.mustDo("POST", "/repos", map[string]any{"name": "third", "auto_init": true}, 201)
	p.admin.mustDo("GET", "/admin/health", nil, 200).decode(t, &h)
	if h.Repos.Count != 3 || h.Repos.MeasuredAt.Equal(measured) {
		t.Fatalf("new repo not measured: %+v", h.Repos)
	}
	measured = h.Repos.MeasuredAt
	time.Sleep(10 * time.Millisecond)
	p.admin.mustDo("GET", "/admin/health?refresh=true", nil, 200).decode(t, &h)
	if !h.Repos.MeasuredAt.After(measured) {
		t.Fatalf("refresh didn't re-measure")
	}

	// Queued jobs with no runner fail the runners check.
	p.commit("main", ".tgp/workflows/ci.yml", "name: CI\non: push\njobs:\n  j:\n    steps: [{run: echo}]\n", "CI")
	p.admin.mustDo("GET", "/admin/health", nil, 200).decode(t, &h)
	runners := false
	for _, c := range h.Checks {
		runners = runners || (c.Name == "runners" && c.Status == "fail")
	}
	if h.Ci.QueuedJobs != 1 || h.Ci.OldestQueuedAt == nil || h.Status != "fail" || !runners {
		t.Fatalf("queued without runners: %s %+v %+v", h.Status, h.Ci, h.Checks)
	}
}

func TestSortChecks(t *testing.T) {
	cs, st := sortChecks([]apigen.HealthCheck{{Name: "a", Status: "ok"}, {Name: "b", Status: "warn"}, {Name: "c", Status: "fail"}, {Name: "d", Status: "warn"}})
	var got []string
	for _, c := range cs {
		got = append(got, c.Name)
	}
	if st != "fail" || strings.Join(got, "") != "cbda" {
		t.Fatalf("%s %v", st, got)
	}
	if _, st := sortChecks(nil); st != "ok" {
		t.Fatal(st)
	}
	if humanBytes(512) != "512 B" || humanBytes(1536) != "1.5 KiB" || humanBytes(3<<30) != "3.0 GiB" {
		t.Fatal(humanBytes(1536), humanBytes(3<<30))
	}
}
