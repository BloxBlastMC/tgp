package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIssueLabelMilestoneWorkflow(t *testing.T) {
	e := newCLIEnv(t)
	e.admin.Do(http.MethodPost, "/repos", map[string]any{"name": "game", "auto_init": true}, nil)
	e.admin.ActivateUser("mia", "maintainer", "mia-password-1", true)
	e.admin.ActivateUser("tom", "tester", "tom-password-1", true)
	tomTok := e.srv.Login("tom", "tom-password-1").Token("read", "write")
	e.login("mia", "mia-password-1")
	R := []string{"-R", "game"}
	with := func(args ...string) []string { return append(args, R...) }

	// Labels and milestones (maintainer).
	var labels []map[string]any
	e.jsonOK(&labels, with("label", "list")...)
	if len(labels) != 7 {
		t.Fatalf("default labels: %d", len(labels))
	}
	var l map[string]any
	e.jsonOK(&l, with("label", "create", "P1", "--color", "#B60205", "-d", "This week")...)
	if l["color"] != "b60205" {
		t.Fatalf("label: %v", l)
	}
	e.jsonOK(&l, with("label", "edit", "p1", "--name", "P0")...)
	if l["name"] != "P0" {
		t.Fatalf("renamed: %v", l)
	}
	e.fail(ExitNotFound, "not_found", with("label", "edit", "nope", "--name", "x", "--json")...)
	var ms map[string]any
	e.jsonOK(&ms, with("milestone", "create", "v1.0", "--due", "2027-01-31", "-d", "First release")...)
	if ms["due_on"] != "2027-01-31" {
		t.Fatalf("milestone: %v", ms)
	}
	e.fail(ExitUsage, CodeUsage, with("milestone", "create", "bad", "--due", "31/01/2027", "--json")...)

	// A tester files a bug; a maintainer triages it.
	var is map[string]any
	e.as(tomTok, func() {
		e.jsonOK(&is, with("issue", "create", "-t", "Crash on join", "-b", "Happens every time", "-l", "bug")...)
		if is["number"] != float64(1) {
			t.Fatalf("issue: %v", is)
		}
		e.fail(ExitForbidden, "forbidden", with("label", "create", "x", "--json")...)
	})
	e.jsonOK(&is, with("issue", "edit", "1", "--add-label", "P0", "--add-assignee", "mia", "--milestone", "v1.0")...)
	if len(is["labels"].([]any)) != 2 || len(is["assignees"].([]any)) != 1 || is["milestone"].(map[string]any)["title"] != "v1.0" {
		t.Fatalf("triaged: %v", is)
	}
	e.fail(ExitConflict, "validation_failed", with("issue", "edit", "1", "--milestone", "v9", "--json")...)
	e.fail(ExitConflict, "validation_failed", with("issue", "edit", "1", "--add-label", "nope", "--json")...)
	e.fail(ExitUsage, CodeUsage, with("issue", "edit", "1", "--json")...)
	e.jsonOK(nil, with("issue", "create", "-t", "Shop", "-l", "enhancement")...)

	var list []map[string]any
	e.jsonOK(&list, with("issue", "list", "--label", "bug")...)
	if len(list) != 1 || list[0]["number"] != float64(1) {
		t.Fatalf("label filter: %v", list)
	}
	e.jsonOK(&list, with("issue", "list", "--assignee", "none")...)
	if len(list) != 1 || list[0]["number"] != float64(2) {
		t.Fatalf("assignee filter: %v", list)
	}
	e.jsonOK(&list, with("issue", "list", "--milestone", "v1.0")...)
	if len(list) != 1 {
		t.Fatalf("milestone filter: %v", list)
	}
	var st map[string][]map[string]any
	e.jsonOK(&st, "issue", "status")
	if len(st["assigned"]) != 1 || len(st["created"]) != 1 {
		t.Fatalf("status: %v", st)
	}

	// Comments, view.
	e.as(tomTok, func() {
		e.jsonOK(nil, with("issue", "comment", "1", "-b", "Also on 1.19")...)
	})
	var view map[string]any
	e.jsonOK(&view, with("issue", "view", "1")...)
	if len(view["comment_list"].([]any)) != 1 || view["can_edit"] != true {
		t.Fatalf("view: %v", view)
	}
	if r := e.ok("", with("issue", "view", "#1", "--comments")...); !strings.Contains(r.stdout, "Also on 1.19") || !strings.Contains(r.stdout, "milestone:") {
		t.Fatalf("human view: %s", r.stdout)
	}

	// A PR that fixes it, labelled and in the milestone; merging closes the issue.
	e.ok("", "repo", "clone", "game")
	dir := filepath.Join(e.home, "game")
	t.Chdir(dir)
	e.git(dir, "checkout", "-q", "-b", "fix")
	_ = os.WriteFile(filepath.Join(dir, "fix.txt"), []byte("fixed\n"), 0o644)
	e.git(dir, "add", ".")
	e.git(dir, "commit", "-qm", "Fix crash on join\n\nFixes #1")
	e.git(dir, "push", "-q", "-u", "origin", "fix")
	var pr map[string]any
	e.jsonOK(&pr, "pr", "create", "--fill", "-l", "bug", "-m", "v1.0")
	if pr["number"] != float64(3) || len(pr["labels"].([]any)) != 1 || pr["milestone"] == nil {
		t.Fatalf("PR meta: %v", pr)
	}
	e.jsonOK(&pr, "pr", "edit", "--add-label", "P0", "--remove-label", "bug", "--milestone", "none")
	if pr["labels"].([]any)[0].(map[string]any)["name"] != "P0" || pr["milestone"] != nil {
		t.Fatalf("PR edit meta: %v", pr)
	}
	e.jsonOK(&list, "pr", "list", "--label", "P0")
	if len(list) != 1 {
		t.Fatalf("pr label filter: %v", list)
	}
	r := e.fail(ExitNotFound, "not_found", "issue", "view", "3", "--json")
	if !strings.Contains(r.stderr, "tgp pr view 3") {
		t.Fatalf("PR-number hint: %s", r.stderr)
	}
	e.jsonOK(nil, "pr", "merge", "3", "--squash")
	e.jsonOK(&is, with("issue", "view", "1")...)
	if is["state"] != "closed" || is["state_reason"] != "completed" {
		t.Fatalf("closing keyword: %v", is["state"])
	}

	// Close as not planned, reopen.
	e.jsonOK(&is, with("issue", "close", "2", "--not-planned", "-c", "Out of scope")...)
	if is["state_reason"] != "not_planned" || is["comments"] != float64(1) {
		t.Fatalf("not planned: %v", is)
	}
	e.jsonOK(&is, with("issue", "reopen", "2")...)
	if is["state"] != "open" {
		t.Fatal("not reopened")
	}

	// Milestone progress, close, delete; label delete.
	e.jsonOK(&ms, with("milestone", "view", "v1.0")...)
	if ms["closed_issues"] != float64(1) || ms["open_issues"] != float64(0) {
		t.Fatalf("progress: %v", ms)
	}
	e.jsonOK(&ms, with("milestone", "close", "v1.0")...)
	if ms["state"] != "closed" {
		t.Fatal("milestone not closed")
	}
	var mss []map[string]any
	e.jsonOK(&mss, with("milestone", "list")...)
	if len(mss) != 0 {
		t.Fatalf("open milestones: %v", mss)
	}
	e.jsonOK(&ms, with("milestone", "edit", "v1.0", "--due", "none", "--title", "1.0")...)
	if ms["title"] != "1.0" || ms["due_on"] != nil {
		t.Fatalf("milestone edit: %v", ms)
	}
	e.jsonOK(nil, with("milestone", "delete", "1.0")...)
	e.jsonOK(nil, with("label", "delete", "P0")...)
	if r := e.ok("", with("label", "list")...); strings.Contains(r.stdout, "P0") {
		t.Fatal("label not deleted")
	}
}
