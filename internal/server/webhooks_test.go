package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/webhook"
)

type received struct {
	event, action, guid, sig string
	body                     []byte
	payload                  map[string]any
}

// receiver records webhook requests; paths ending in /fail answer 500,
// /reject 400.
type receiver struct {
	mu   sync.Mutex
	got  []received
	srv  *httptest.Server
	fail bool
}

func newReceiver(t *testing.T) *receiver {
	r := &receiver{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		var p map[string]any
		_ = json.Unmarshal(b, &p)
		action, _ := p["action"].(string)
		r.mu.Lock()
		r.got = append(r.got, received{req.Header.Get("X-Tgp-Event"), action, req.Header.Get("X-Tgp-Delivery"), req.Header.Get("X-Hub-Signature-256"), b, p})
		fail := r.fail
		r.mu.Unlock()
		switch {
		case strings.HasSuffix(req.URL.Path, "/reject"):
			w.WriteHeader(http.StatusBadRequest)
		case fail:
			w.WriteHeader(http.StatusInternalServerError)
		default:
			_, _ = w.Write([]byte("thanks"))
		}
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, g := range r.got {
		e := g.event
		if g.action != "" {
			e += "." + g.action
		}
		out = append(out, e)
	}
	return out
}

func (r *receiver) find(t *testing.T, ev string) received {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, g := range r.got {
		e := g.event
		if g.action != "" {
			e += "." + g.action
		}
		if e == ev {
			return g
		}
	}
	t.Fatalf("no %s delivery; got %v", ev, r.eventsLocked())
	return received{}
}

func (r *receiver) eventsLocked() []string {
	var out []string
	for _, g := range r.got {
		out = append(out, g.event+"."+g.action)
	}
	return out
}

func (r *receiver) reset() {
	r.mu.Lock()
	r.got = nil
	r.mu.Unlock()
}

func (p *prEnv) deliver() {
	p.t.Helper()
	if err := p.srv.DeliverWebhooks(p.t.Context()); err != nil {
		p.t.Fatal(err)
	}
}

func TestWebhooks(t *testing.T) {
	p := newPREnv(t)
	rcv := newReceiver(t)

	// Validation and permissions.
	if r := p.mia.do("POST", "/repos/game/webhooks", map[string]any{"url": "ftp://x"}); r.code() != "validation_failed" {
		t.Fatalf("bad url: %s", r.body)
	}
	if r := p.mia.do("POST", "/repos/game/webhooks", map[string]any{"url": rcv.srv.URL, "events": []string{"nope"}}); r.code() != "validation_failed" {
		t.Fatalf("bad event: %s", r.body)
	}
	p.tom.mustDo("POST", "/repos/game/webhooks", map[string]any{"url": rcv.srv.URL}, 403)

	// Creating sends a signed ping right away.
	var hook apigen.Webhook
	p.mia.mustDo("POST", "/repos/game/webhooks", map[string]any{"url": rcv.srv.URL + "/hook", "secret": "s3cret", "events": []string{"*"}}, 201).decode(t, &hook)
	if !hook.HasSecret || hook.Scope != "repo" || hook.LastDelivery == nil || hook.LastDelivery.Status != "success" || *hook.LastDelivery.ResponseStatus != 200 {
		t.Fatalf("hook: %+v", hook)
	}
	ping := rcv.find(t, "ping")
	if !webhook.Verify("s3cret", ping.body, ping.sig) || ping.payload["hook_id"] != float64(hook.Id) {
		t.Fatalf("ping: %s %s", ping.sig, ping.body)
	}
	p.tom.mustDo("GET", "/repos/game/webhooks", nil, 403)
	p.tom.mustDo("GET", fmt.Sprintf("/webhooks/%d", hook.Id), nil, 404)
	if strings.Contains(string(p.mia.mustDo("GET", fmt.Sprintf("/webhooks/%d", hook.Id), nil, 200).body), "s3cret") {
		t.Fatal("secret leaked")
	}
	rcv.reset()

	// Pushes: a new branch sends create + push, with commits.
	p.commit("feature", "a.txt", "1", "Add a")
	p.deliver()
	if ev := strings.Join(rcv.events(), ","); ev != "create,push" {
		t.Fatalf("branch push: %s", ev)
	}
	push := rcv.find(t, "push")
	if push.payload["ref"] != "refs/heads/feature" || push.payload["created"] != true || len(push.payload["commits"].([]any)) != 1 ||
		push.payload["repository"].(map[string]any)["name"] != "game" || push.payload["sender"].(map[string]any)["login"] != "mia" {
		t.Fatalf("push payload: %s", push.body)
	}
	rcv.reset()

	// Issues: opened, labeled + assigned + edited in one update, comment.
	p.tom.mustDo("POST", "/repos/game/issues", map[string]any{"title": "Crash"}, 201)
	p.mia.mustDo("PATCH", "/repos/game/issues/1", map[string]any{"add_labels": []string{"bug"}, "add_assignees": []string{"mia"}, "body": "Steps"}, 200)
	p.tom.mustDo("POST", "/repos/game/issues/1/comments", map[string]any{"body": "Still broken"}, 201)
	p.mia.mustDo("PATCH", "/repos/game/issues/1", map[string]any{"state": "closed"}, 200)
	p.deliver()
	got := rcv.events()
	sort.Strings(got[1:3]) // labels and assignees are applied in either order
	if ev := strings.Join(got, ","); ev != "issues.opened,issues.assigned,issues.labeled,issues.edited,issue_comment.created,issues.closed" {
		t.Fatalf("issue events: %s", ev)
	}
	if l := rcv.find(t, "issues.labeled"); l.payload["label"].(map[string]any)["name"] != "bug" || l.payload["issue"].(map[string]any)["number"] != float64(1) {
		t.Fatalf("labeled: %s", l.body)
	}
	rcv.reset()

	// Pull request: opened, merged (closed + push to main).
	p.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "Feature", "head": "feature", "base": "main"}, 201)
	p.mia.mustDo("POST", "/repos/game/pulls/2/merge", map[string]any{"method": "squash"}, 200)
	p.deliver()
	evs := rcv.events()
	if !contains(evs, "pull_request.opened") || !contains(evs, "pull_request.closed") || !contains(evs, "push") {
		t.Fatalf("pull events: %v", evs)
	}
	if pr := rcv.find(t, "pull_request.closed").payload["pull_request"].(map[string]any); pr["state"] != "merged" {
		t.Fatalf("merged pull: %v", pr["state"])
	}
	rcv.reset()

	// Releases and CI runs.
	p.g.must(p.dir, "checkout", "-q", "main")
	p.g.must(p.dir, "pull", "-q", "origin", "main")
	p.commit("main", ".tgp/workflows/ci.yml", quickWF, "Add CI")
	p.mia.mustDo("POST", "/repos/game/releases", map[string]any{"tag": "v1"}, 201)
	p.deliver()
	evs = rcv.events()
	for _, want := range []string{"release.created", "release.published", "release.released", "workflow_run.requested", "create"} {
		if !contains(evs, want) {
			t.Fatalf("missing %s in %v", want, evs)
		}
	}
	wr := rcv.find(t, "workflow_run.requested").payload["workflow_run"].(map[string]any)
	if wr["name"] != "CI" || wr["status"] == nil {
		t.Fatalf("workflow_run: %v", wr)
	}
	rcv.reset()

	// Event filters: only issues.
	p.mia.mustDo("PATCH", fmt.Sprintf("/webhooks/%d", hook.Id), map[string]any{"events": []string{"issues"}}, 200)
	p.commit("main", "b.txt", "1", "B")
	p.tom.mustDo("POST", "/repos/game/issues", map[string]any{"title": "Another"}, 201)
	p.deliver()
	if ev := strings.Join(rcv.events(), ","); ev != "issues.opened" {
		t.Fatalf("filtered: %s", ev)
	}
	rcv.reset()

	// Retries: 500s are retried with backoff, then succeed.
	rcv.mu.Lock()
	rcv.fail = true
	rcv.mu.Unlock()
	p.tom.mustDo("POST", "/repos/game/issues", map[string]any{"title": "Third"}, 201)
	p.deliver()
	var page apigen.WebhookDeliveryPage
	p.mia.mustDo("GET", fmt.Sprintf("/webhooks/%d/deliveries?per_page=1", hook.Id), nil, 200).decode(t, &page)
	d := page.Deliveries[0]
	if d.Status != "pending" || d.Attempts != 1 || d.NextAttemptAt == nil || *d.ResponseStatus != 500 {
		t.Fatalf("retrying: %+v", d)
	}
	rcv.mu.Lock()
	rcv.fail = false
	rcv.mu.Unlock()
	p.srv.ci.now = func() time.Time { return time.Now().Add(time.Minute) }
	p.deliver()
	p.srv.ci.now = nil
	p.mia.mustDo("GET", fmt.Sprintf("/webhooks/%d/deliveries?per_page=1", hook.Id), nil, 200).decode(t, &page)
	if d := page.Deliveries[0]; d.Status != "success" || d.Attempts != 2 {
		t.Fatalf("after retry: %+v", d)
	}

	// Detail and redelivery.
	var detail apigen.WebhookDeliveryDetail
	p.mia.mustDo("GET", fmt.Sprintf("/webhooks/%d/deliveries/%d", hook.Id, page.Deliveries[0].Id), nil, 200).decode(t, &detail)
	if detail.Request.Headers["X-Tgp-Event"] != "issues" || detail.Request.Payload["action"] != "opened" || detail.Response == nil || detail.Response.Body != "thanks" {
		t.Fatalf("detail: %+v", detail)
	}
	var again apigen.WebhookDelivery
	p.mia.mustDo("POST", fmt.Sprintf("/webhooks/%d/deliveries/%d/redeliver", hook.Id, detail.Id), nil, 201).decode(t, &again)
	p.deliver()
	if !again.Redelivery || again.Guid == detail.Guid {
		t.Fatalf("redelivery: %+v", again)
	}

	// 4xx answers aren't retried; link-local destinations are refused.
	var bad apigen.Webhook
	p.mia.mustDo("POST", "/repos/game/webhooks", map[string]any{"url": rcv.srv.URL + "/reject"}, 201).decode(t, &bad)
	if ld := bad.LastDelivery; ld == nil || ld.Status != "failed" || *ld.ResponseStatus != 400 {
		t.Fatalf("rejected ping: %+v", bad.LastDelivery)
	}
	var meta apigen.WebhookDelivery
	p.mia.mustDo("POST", "/repos/game/webhooks", map[string]any{"url": "http://169.254.169.254/latest"}, 201).decode(t, &bad)
	p.mia.mustDo("POST", fmt.Sprintf("/webhooks/%d/ping", bad.Id), nil, 200).decode(t, &meta)
	if meta.Status != "failed" || meta.Error == nil || !strings.Contains(*meta.Error, "not allowed") {
		t.Fatalf("metadata address: %+v", meta)
	}

	// Organisation webhooks: admins only, events from every repository.
	p.mia.mustDo("POST", "/admin/webhooks", map[string]any{"url": rcv.srv.URL}, 403)
	var org apigen.Webhook
	p.admin.mustDo("POST", "/admin/webhooks", map[string]any{"url": rcv.srv.URL + "/org", "events": []string{"repository_dummy"}}, 422)
	p.admin.mustDo("POST", "/admin/webhooks", map[string]any{"url": rcv.srv.URL + "/org", "events": []string{"issues"}}, 201).decode(t, &org)
	p.mia.mustDo("GET", fmt.Sprintf("/webhooks/%d", org.Id), nil, 404)
	p.admin.mustDo("POST", "/repos", map[string]any{"name": "tools", "auto_init": true}, 201)
	rcv.reset()
	p.admin.mustDo("POST", "/repos/tools/issues", map[string]any{"title": "Org-wide"}, 201)
	p.deliver()
	if ev := strings.Join(rcv.events(), ","); ev != "issues.opened" {
		t.Fatalf("org hook: %s", ev)
	}

	// Deleting removes the hook and its log.
	p.mia.mustDo("DELETE", fmt.Sprintf("/webhooks/%d", hook.Id), nil, 204)
	p.mia.mustDo("GET", fmt.Sprintf("/webhooks/%d/deliveries", hook.Id), nil, 404)
	var events []apigen.WebhookEventInfo
	p.tom.mustDo("GET", "/webhook-events", nil, 200).decode(t, &events)
	if len(events) < 9 {
		t.Fatalf("events: %d", len(events))
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
