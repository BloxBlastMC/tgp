package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWebhookCommands(t *testing.T) {
	e := newCLIEnv(t)
	e.admin.Do(http.MethodPost, "/repos", map[string]any{"name": "game", "auto_init": true}, nil)
	e.admin.ActivateUser("mia", "maintainer", "mia-password-1", true)
	e.admin.ActivateUser("tom", "tester", "tom-password-1", true)
	tomTok := e.srv.Login("tom", "tom-password-1").Token("read", "write")
	var hits atomic.Int32
	var failing atomic.Bool
	rcv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		hits.Add(1)
		if failing.Load() {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer rcv.Close()
	e.login("mia", "mia-password-1")
	R := []string{"-R", "game"}
	with := func(args ...string) []string { return append(args, R...) }

	// Create with a secret from stdin; the ping succeeds.
	r := e.ok("s3cret\n", with("webhook", "create", rcv.URL, "-e", "push,issues", "-e", "release", "--secret-stdin", "--json")...)
	var h map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &h); err != nil {
		t.Fatal(err)
	}
	validateOutput(t, []string{"webhook", "create"}, r.stdout)
	if h["has_secret"] != true || strings.Join(toStrings(h["events"]), ",") != "push,issues,release" || h["last_delivery"].(map[string]any)["status"] != "success" {
		t.Fatalf("create: %v", h)
	}
	id := strconv.Itoa(int(h["id"].(float64)))
	e.fail(ExitUsage, CodeUsage, with("webhook", "create", rcv.URL, "--secret-stdin", "--json")...) // empty stdin
	e.fail(ExitConflict, "validation_failed", with("webhook", "create", rcv.URL, "-e", "nope", "--json")...)

	var hooks []map[string]any
	e.jsonOK(&hooks, with("webhook", "list")...)
	if len(hooks) != 1 {
		t.Fatalf("list: %v", hooks)
	}
	if out := e.ok("", "webhook", "view", id).stdout; !strings.Contains(out, "push, issues, release") || !strings.Contains(out, "ping") {
		t.Fatalf("view: %s", out)
	}

	// Ping, deliveries, delivery detail, redeliver.
	e.jsonOK(nil, "webhook", "ping", id)
	var ds []map[string]any
	e.jsonOK(&ds, "webhook", "deliveries", id)
	if len(ds) != 2 || ds[0]["event"] != "ping" {
		t.Fatalf("deliveries: %v", ds)
	}
	did := strconv.Itoa(int(ds[0]["id"].(float64)))
	var d map[string]any
	e.jsonOK(&d, "webhook", "delivery", id, did)
	if d["request"].(map[string]any)["headers"].(map[string]any)["X-Tgp-Event"] != "ping" {
		t.Fatalf("delivery: %v", d)
	}
	e.jsonOK(&d, "webhook", "redeliver", id, did)
	if d["redelivery"] != true || d["status"] != "pending" {
		t.Fatalf("redeliver: %v", d)
	}

	// A failing ping exits 1.
	failing.Store(true)
	if r := e.run("", "webhook", "ping", id); r.code != ExitError || !strings.Contains(r.stdout, "404") {
		t.Fatalf("failing ping: %d %s %s", r.code, r.stdout, r.stderr)
	}

	// Edit; testers and non-admins are kept out.
	e.jsonOK(&h, "webhook", "edit", id, "--active=false", "--remove-secret", "-e", "*")
	if h["active"] != false || h["has_secret"] != false || toStrings(h["events"])[0] != "*" {
		t.Fatalf("edit: %v", h)
	}
	e.as(tomTok, func() {
		e.fail(ExitForbidden, "forbidden", with("webhook", "list", "--json")...)
		e.fail(ExitNotFound, "not_found", "webhook", "view", id, "--json")
	})
	e.fail(ExitForbidden, "forbidden", "webhook", "list", "--org", "--json")
	var evs []map[string]any
	e.jsonOK(&evs, "webhook", "events")
	if len(evs) < 9 {
		t.Fatalf("events: %v", evs)
	}
	e.fail(ExitUsage, CodeInputRequired, "webhook", "delete", id, "--json")
	e.jsonOK(nil, "webhook", "delete", id, "--yes")
	if hits.Load() < 3 {
		t.Fatalf("receiver hits: %d", hits.Load())
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}
