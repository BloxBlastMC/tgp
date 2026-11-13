package server

import (
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

func issueEvents(t *testing.T, c *client, path string) []string {
	t.Helper()
	var tl []apigen.TimelineItem
	c.mustDo("GET", path, nil, 200).decode(t, &tl)
	var evs []string
	for _, it := range tl {
		if it.Event != nil {
			evs = append(evs, string(it.Event.Type))
		}
	}
	return evs
}

func labelNames(ls []apigen.Label) string {
	var n []string
	for _, l := range ls {
		n = append(n, l.Name)
	}
	return strings.Join(n, ",")
}

func TestIssuesLabelsMilestones(t *testing.T) {
	p := newPREnv(t)

	// New repositories get default labels.
	var labels []apigen.Label
	p.tom.mustDo("GET", "/repos/game/labels", nil, 200).decode(t, &labels)
	if len(labels) != 7 || labels[0].Name != "bug" {
		t.Fatalf("default labels: %s", labelNames(labels))
	}
	// Label management needs write access.
	if r := p.tom.do("POST", "/repos/game/labels", map[string]any{"name": "x", "color": "ffffff"}); r.status != 403 {
		t.Fatalf("tester created label: %d", r.status)
	}
	var crit apigen.Label
	p.mia.mustDo("POST", "/repos/game/labels", map[string]any{"name": "critical", "color": "#B60205", "description": "Drop everything"}, 201).decode(t, &crit)
	if crit.Color != "b60205" {
		t.Fatalf("color not normalised: %s", crit.Color)
	}
	for _, bad := range []map[string]any{{"name": "Critical", "color": "ffffff"}, {"name": "y", "color": "red"}, {"name": "a,b", "color": "ffffff"}} {
		if r := p.mia.do("POST", "/repos/game/labels", bad); r.status != 409 && r.status != 422 {
			t.Errorf("bad label %v: %d", bad, r.status)
		}
	}
	var ms apigen.Milestone
	if r := p.tom.do("POST", "/repos/game/milestones", map[string]any{"title": "v1"}); r.status != 403 {
		t.Fatalf("tester created milestone: %d", r.status)
	}
	p.mia.mustDo("POST", "/repos/game/milestones", map[string]any{"title": "v1.0", "description": "First release", "due_on": "2027-01-31"}, 201).decode(t, &ms)
	if ms.DueOn == nil || ms.DueOn.String() != "2027-01-31" || ms.State != "open" {
		t.Fatalf("milestone: %+v", ms)
	}

	// Testers file bugs with labels, assignees and a milestone.
	var is apigen.Issue
	p.tom.mustDo("POST", "/repos/game/issues", map[string]any{
		"title": "Crash on join", "body": "Steps in #2.", "labels": []string{"bug", "Critical"}, "assignees": []string{"mia"}, "milestone": ms.Id,
	}, 201).decode(t, &is)
	if is.Number != 1 || is.State != "open" || labelNames(is.Labels) != "bug,critical" || len(is.Assignees) != 1 ||
		is.Milestone == nil || is.Milestone.Title != "v1.0" || !is.CanEdit || !strings.Contains(is.BodyHtml, `href="/game/issues/2"`) {
		t.Fatalf("created: %+v", is)
	}
	if evs := issueEvents(t, p.tom, "/repos/game/issues/1/timeline"); len(evs) != 0 {
		t.Fatalf("creation recorded events: %v", evs)
	}
	for _, bad := range []map[string]any{
		{"title": "x", "labels": []string{"nope"}},
		{"title": "x", "assignees": []string{"ghost"}},
		{"title": "x", "milestone": 999},
		{"title": ""},
	} {
		if r := p.tom.do("POST", "/repos/game/issues", bad); r.code() != "validation_failed" {
			t.Errorf("bad issue %v: %d %s", bad, r.status, r.body)
		}
	}
	p.mia.mustDo("POST", "/repos/game/issues", map[string]any{"title": "Add shop", "labels": []string{"enhancement"}}, 201)

	// Numbers are shared with pull requests.
	p.commit("feat", "f.txt", "f\n", "f")
	var pr apigen.PullRequest
	p.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "Fix crash", "body": "Fixes #1", "head": "feat", "labels": []string{"bug"}, "assignees": []string{"mia"}}, 201).decode(t, &pr)
	if pr.Number != 3 || labelNames(pr.Labels) != "bug" || len(pr.Assignees) != 1 {
		t.Fatalf("PR meta: %d %s %v", pr.Number, labelNames(pr.Labels), pr.Assignees)
	}
	if r := p.mia.do("GET", "/repos/game/issues/3", nil); r.status != 404 || !strings.Contains(string(r.body), `"is_pull":true`) {
		t.Fatalf("issue lookup of PR: %d %s", r.status, r.body)
	}
	if r := p.mia.do("GET", "/repos/game/pulls/1", nil); r.status != 404 || !strings.Contains(string(r.body), `"is_issue":true`) {
		t.Fatalf("PR lookup of issue: %d %s", r.status, r.body)
	}
	var pp apigen.PullPage
	p.mia.mustDo("GET", "/repos/game/pulls?labels=bug", nil, 200).decode(t, &pp)
	if pp.Total != 1 || labelNames(pp.Pulls[0].Labels) != "bug" {
		t.Fatalf("PR label filter: %+v", pp)
	}

	// Filters.
	list := func(q string) []int64 {
		t.Helper()
		var page apigen.IssuePage
		p.mia.mustDo("GET", "/repos/game/issues"+q, nil, 200).decode(t, &page)
		var ns []int64
		for _, i := range page.Issues {
			ns = append(ns, i.Number)
		}
		if page.Total < len(ns) || (!strings.Contains(q, "per_page") && page.Total != len(ns)) {
			t.Fatalf("%s: total %d, %d items", q, page.Total, len(ns))
		}
		return ns
	}
	for q, want := range map[string]string{
		"":                         "2,1",
		"?labels=bug":              "1",
		"?labels=bug,critical":     "1",
		"?labels=bug,enhancement":  "",
		"?milestone=v1.0":          "1",
		"?milestone=none":          "2",
		"?milestone=*":             "1",
		"?assignee=mia":            "1",
		"?assignee=none":           "2",
		"?author=tom":              "1",
		"?q=SHOP":                  "2",
		"?state=closed":            "",
		"?sort=created&per_page=1": "2",
	} {
		var got []string
		for _, n := range list(q) {
			got = append(got, itoa(n))
		}
		if strings.Join(got, ",") != want {
			t.Errorf("issues%s = %v, want %s", q, got, want)
		}
	}
	var mine apigen.IssuePage
	p.mia.mustDo("GET", "/issues", nil, 200).decode(t, &mine)
	if mine.Total != 1 || mine.Issues[0].Repo != "game" {
		t.Fatalf("assigned to me: %+v", mine)
	}
	p.tom.mustDo("GET", "/issues?filter=created", nil, 200).decode(t, &mine)
	if mine.Total != 1 {
		t.Fatalf("created by me: %+v", mine)
	}

	// Triage: testers relabel, reassign and close; only authors/writers edit text.
	p.tom.mustDo("PATCH", "/repos/game/issues/2", map[string]any{"add_labels": []string{"question"}, "remove_labels": []string{"enhancement"}, "add_assignees": []string{"tom"}}, 200).decode(t, &is)
	if labelNames(is.Labels) != "question" || is.Assignees[0].Username != "tom" || is.CanEdit {
		t.Fatalf("triage: %s %v %v", labelNames(is.Labels), is.Assignees, is.CanEdit)
	}
	if r := p.tom.do("PATCH", "/repos/game/issues/2", map[string]any{"title": "Hijack"}); r.status != 403 {
		t.Fatalf("tester edited someone else's issue: %d", r.status)
	}
	p.tom.mustDo("PATCH", "/repos/game/issues/1", map[string]any{"title": "Crash on join (1.20)"}, 200)
	p.tom.mustDo("PATCH", "/repos/game/issues/2", map[string]any{"state": "closed", "state_reason": "not_planned", "milestone": ms.Id}, 200).decode(t, &is)
	if is.State != "closed" || deref(is.StateReason) != "not_planned" || is.ClosedBy.Username != "tom" {
		t.Fatalf("closed: %+v", is)
	}
	p.mia.mustDo("GET", "/repos/game/milestones/"+itoa(ms.Id), nil, 200).decode(t, &ms)
	if ms.OpenIssues != 1 || ms.ClosedIssues != 1 {
		t.Fatalf("milestone counts: %d/%d", ms.OpenIssues, ms.ClosedIssues)
	}
	is = apigen.Issue{} // decode doesn't clear fields the response omits
	p.tom.mustDo("PATCH", "/repos/game/issues/2", map[string]any{"state": "open", "milestone": 0, "labels": []string{}}, 200).decode(t, &is)
	if is.State != "open" || is.StateReason != nil || is.Milestone != nil || len(is.Labels) != 0 {
		t.Fatalf("reopened: %+v", is)
	}
	if evs := strings.Join(issueEvents(t, p.mia, "/repos/game/issues/2/timeline"), ","); evs != "unlabeled,labeled,assigned,closed,milestoned,reopened,unlabeled,demilestoned" {
		t.Fatalf("events: %s", evs)
	}

	// Comments.
	var c apigen.Comment
	p.tom.mustDo("POST", "/repos/game/issues/1/comments", map[string]any{"body": "Also on 1.19"}, 201).decode(t, &c)
	p.tom.mustDo("PATCH", "/repos/game/comments/"+itoa(c.Id), map[string]any{"body": "Also on 1.19 and 1.18"}, 200).decode(t, &c)
	if !c.Edited {
		t.Fatal("not marked edited")
	}
	if r := p.mia.do("PATCH", "/repos/game/comments/"+itoa(c.Id), map[string]any{"resolved": true}); r.code() != "validation_failed" {
		t.Fatalf("resolve issue comment: %d %s", r.status, r.body)
	}
	p.mia.mustDo("POST", "/repos/game/issues/1/comments", map[string]any{"body": "On it"}, 201)
	var cs []apigen.Comment
	p.mia.mustDo("GET", "/repos/game/issues/1/comments", nil, 200).decode(t, &cs)
	if len(cs) != 2 {
		t.Fatalf("comments: %d", len(cs))
	}
	p.tom.mustDo("DELETE", "/repos/game/comments/"+itoa(c.Id), nil, 204)
	p.mia.mustDo("GET", "/repos/game/issues/1", nil, 200).decode(t, &is)
	if is.Comments != 1 {
		t.Fatalf("comment count: %d", is.Comments)
	}

	// Merging "Fixes #1" into the default branch closes the issue.
	p.tom.mustDo("POST", "/repos/game/pulls/3/reviews", map[string]any{"state": "approve"}, 201)
	p.mia.mustDo("POST", "/repos/game/pulls/3/merge", map[string]any{"method": "squash"}, 200)
	p.mia.mustDo("GET", "/repos/game/issues/1", nil, 200).decode(t, &is)
	if is.State != "closed" || deref(is.StateReason) != "completed" || is.ClosedBy.Username != "mia" {
		t.Fatalf("closing keyword: %+v", is)
	}
	var tl []apigen.TimelineItem
	p.mia.mustDo("GET", "/repos/game/issues/1/timeline", nil, 200).decode(t, &tl)
	last := tl[len(tl)-1].Event
	if last == nil || last.Type != "closed" || last.Data["pull"] != float64(3) {
		t.Fatalf("close event: %+v", tl[len(tl)-1])
	}

	// A commit pushed to main with "closes #2" closes that issue.
	p.g.must(p.dir, "checkout", "-q", "main")
	p.g.must(p.dir, "pull", "-q", "origin", "main")
	writeFile(t, p.dir, "shop.txt", "shop\n")
	p.g.must(p.dir, "add", ".")
	p.g.must(p.dir, "commit", "-qm", "Shop\n\nCloses #2")
	p.g.must(p.dir, "push", "-q", "origin", "main")
	p.mia.mustDo("GET", "/repos/game/issues/2", nil, 200).decode(t, &is)
	if is.State != "closed" {
		t.Fatalf("push closing keyword: %s", is.State)
	}

	// Label rename and delete; milestone close and delete.
	p.mia.mustDo("PATCH", "/repos/game/labels/"+itoa(crit.Id), map[string]any{"name": "P0"}, 200)
	p.mia.mustDo("GET", "/repos/game/issues/1", nil, 200).decode(t, &is)
	if labelNames(is.Labels) != "bug,P0" {
		t.Fatalf("renamed label: %s", labelNames(is.Labels))
	}
	p.mia.mustDo("DELETE", "/repos/game/labels/"+itoa(crit.Id), nil, 204)
	p.mia.mustDo("GET", "/repos/game/issues/1", nil, 200).decode(t, &is)
	if labelNames(is.Labels) != "bug" {
		t.Fatalf("deleted label still attached: %s", labelNames(is.Labels))
	}
	msID := ms.Id
	ms = apigen.Milestone{}
	p.mia.mustDo("PATCH", "/repos/game/milestones/"+itoa(msID), map[string]any{"state": "closed", "clear_due_on": true}, 200).decode(t, &ms)
	if ms.State != "closed" || ms.DueOn != nil || ms.ClosedAt == nil {
		t.Fatalf("closed milestone: %+v", ms)
	}
	var mss []apigen.Milestone
	p.mia.mustDo("GET", "/repos/game/milestones", nil, 200).decode(t, &mss)
	if len(mss) != 0 {
		t.Fatalf("open milestones: %d", len(mss))
	}
	p.mia.mustDo("GET", "/repos/game/milestones?state=all", nil, 200).decode(t, &mss)
	if len(mss) != 1 {
		t.Fatalf("all milestones: %d", len(mss))
	}
	p.mia.mustDo("DELETE", "/repos/game/milestones/"+itoa(ms.Id), nil, 204)
	is = apigen.Issue{}
	p.mia.mustDo("GET", "/repos/game/issues/1", nil, 200).decode(t, &is)
	if is.Milestone != nil {
		t.Fatal("deleted milestone still attached")
	}
}

func TestClosingRefs(t *testing.T) {
	cases := map[string]string{
		"Fixes #12":                      "12",
		"fixed #1, closes #2 resolve #3": "1,2,3",
		"Closes: #4":                     "4",
		"see #5":                         "",
		"prefix#6 fixes#7":               "",
		"Fixes #8 fixes #8":              "8",
	}
	for in, want := range cases {
		var got []string
		for _, n := range closingRefs(in) {
			got = append(got, itoa(n))
		}
		if strings.Join(got, ",") != want {
			t.Errorf("closingRefs(%q) = %v, want %s", in, got, want)
		}
	}
}
