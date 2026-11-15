package server

import (
	"context"
	"sort"
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

func pullLabels(t *testing.T, p *prEnv, n int) string {
	t.Helper()
	pr := p.pull(p.admin, n)
	var names []string
	for _, l := range pr.Labels {
		names = append(names, l.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func TestPullLabeler(t *testing.T) {
	p := newPREnv(t)
	for _, l := range []string{"issues", "finance", "docs", "ci"} {
		p.mia.mustDo("POST", "/repos/game/labels", map[string]any{"name": l, "color": "ededed"}, 201)
	}
	p.admin.mustDo("POST", "/repos/game/webhooks", map[string]any{"url": "http://127.0.0.1:1/hook", "events": []string{"pull_request"}}, 201)
	cfg := `issues:
  - changed-files:
      - any-glob-to-any-file: ['services/issues/**']
finance:
  - changed-files:
      - any-glob-to-any-file: 'services/finance/**'
docs: '**/*.md'
missing-label: 'services/**'
`
	// The configuration lives in .github/labeler.yml (the GitHub location).
	p.commit("main", ".github/labeler.yml", cfg, "Labeler")

	p.commit("feature", "services/issues/main.go", "package main", "Issues change")
	var pr apigen.PullRequest
	p.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "Issues", "head": "feature"}, 201).decode(t, &pr)
	if got := pullLabels(t, p, 1); got != "issues" {
		t.Fatalf("labels on open: %q", got)
	}
	// The response already carries the labels.
	if len(pr.Labels) != 1 || pr.Labels[0].Name != "issues" {
		t.Fatalf("create response labels: %+v", pr.Labels)
	}
	// A push adds labels for the new files.
	p.commit("feature", "docs/finance.md", "# Finance", "Docs")
	p.commit("feature", "services/finance/x.go", "package x", "Finance")
	if got := pullLabels(t, p, 1); got != "docs,finance,issues" {
		t.Fatalf("labels after push: %q", got)
	}
	// Labels are only added: removing one by hand sticks until it matches anew.
	p.mia.mustDo("PATCH", "/repos/game/pulls/1", map[string]any{"remove_labels": []string{"docs"}}, 200)
	if got := pullLabels(t, p, 1); got != "finance,issues" {
		t.Fatalf("labels after removal: %q", got)
	}
	p.commit("feature", "services/issues/more.go", "package main", "More")
	if got := pullLabels(t, p, 1); got != "docs,finance,issues" {
		// docs/finance.md still matches, so docs comes back on the next push.
		t.Fatalf("labels after second push: %q", got)
	}

	// The timeline shows tgp (no actor) adding them; webhooks went out.
	var evs []model.IssueEvent
	if err := p.db.NewSelect().Model(&evs).Where("type = ?", evLabeled).Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.ActorID != nil {
			t.Fatalf("labeled event with an actor: %+v", e)
		}
	}
	n, _ := p.db.NewSelect().Model((*model.WebhookDelivery)(nil)).Where("event = 'pull_request' AND action = 'labeled'").Count(context.Background())
	if n < 3 {
		t.Fatalf("labeled webhooks: %d", n)
	}

	// The rules come from the base branch: a PR can't label itself with
	// a changed configuration.
	p.g.must(p.dir, "checkout", "-q", "main")
	p.commit("sneaky", ".github/labeler.yml", "ci: '**'\n", "Change rules")
	p.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "Sneaky", "head": "sneaky"}, 201)
	if got := pullLabels(t, p, 2); got != "" {
		t.Fatalf("PR changed its own labeler rules: %q", got)
	}
	// .tgp/labeler.yml takes precedence over .github/labeler.yml.
	p.g.must(p.dir, "checkout", "-q", "main")
	p.commit("main", ".tgp/labeler.yml", "ci: ['.tgp/**', '.github/**']\n", "tgp labeler")
	p.commit("sneaky", "x.txt", "x", "Touch")
	p.g.must(p.dir, "merge", "-q", "main", "-m", "merge main")
	p.g.must(p.dir, "push", "-q", "origin", "sneaky")
	if got := pullLabels(t, p, 2); got != "ci" {
		t.Fatalf(".tgp/labeler.yml: %q", got)
	}
	// An invalid configuration is ignored (logged), never an error.
	p.g.must(p.dir, "checkout", "-q", "main")
	p.commit("main", ".tgp/labeler.yml", "ci:\n  - nonsense: x\n", "Break labeler")
	p.commit("other", "services/issues/z.go", "package main", "Z")
	p.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "Other", "head": "other"}, 201)
	if got := pullLabels(t, p, 3); got != "" {
		t.Fatalf("invalid config applied labels: %q", got)
	}
}
