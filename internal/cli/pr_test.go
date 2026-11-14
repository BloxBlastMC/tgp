package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// as runs fn with TGP_TOKEN set to tok (another user), restoring after.
func (e *cliEnv) as(tok string, fn func()) {
	e.t.Helper()
	old, had := os.LookupEnv("TGP_TOKEN")
	os.Setenv("TGP_TOKEN", tok)
	defer func() {
		if had {
			os.Setenv("TGP_TOKEN", old)
		} else {
			os.Unsetenv("TGP_TOKEN")
		}
	}()
	fn()
}

func TestPullRequestWorkflow(t *testing.T) {
	e := newCLIEnv(t)
	e.admin.Do(http.MethodPost, "/repos", map[string]any{"name": "game", "auto_init": true}, nil)
	e.admin.ActivateUser("mia", "maintainer", "mia-password-1", true)
	e.admin.ActivateUser("tom", "tester", "tom-password-1", true)
	tomTok := e.srv.Login("tom", "tom-password-1").Token("read", "write")
	adminTok := e.admin.Token("read", "write", "admin")
	e.login("mia", "mia-password-1")

	e.ok("", "repo", "clone", "game")
	dir := filepath.Join(e.home, "game")
	t.Chdir(dir)
	e.git(dir, "checkout", "-q", "-b", "shop")
	if err := os.WriteFile(filepath.Join(dir, "shop.txt"), []byte("sword 10\nshield 15\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.git(dir, "add", ".")
	e.git(dir, "commit", "-qm", "Add shop prices")
	e.git(dir, "push", "-q", "-u", "origin", "shop")

	// Create from the current branch, title/body from the commit.
	var pr map[string]any
	e.jsonOK(&pr, "pr", "create", "--fill", "--reviewer", "tom")
	if pr["number"] != float64(1) || pr["title"] != "Add shop prices" || pr["head"].(map[string]any)["branch"] != "shop" {
		t.Fatalf("created: %v", pr)
	}
	e.fail(ExitConflict, "conflict", "pr", "create", "--fill", "--json")

	var list []map[string]any
	e.jsonOK(&list, "pr", "list")
	if len(list) != 1 || list[0]["review_decision"] != "none" {
		t.Fatalf("list: %v", list)
	}
	// View resolves the PR from the current branch.
	var view map[string]any
	e.jsonOK(&view, "pr", "view")
	if view["number"] != float64(1) || view["can_merge"] != true || view["reviews"] == nil || view["comment_list"] == nil {
		t.Fatalf("view: %v", view)
	}
	if r := e.ok("", "pr", "view", "1", "--json=number,mergeable,comment_list"); !strings.HasPrefix(strings.TrimSpace(r.stdout), `{"comment_list":[],"mergeable":"clean","number":1}`) {
		t.Fatalf("view fields: %s", r.stdout)
	}
	if r := e.ok("", "pr", "diff", "1"); !strings.Contains(r.stdout, "+++ b/shop.txt") || !strings.Contains(r.stdout, "+sword 10") {
		t.Fatalf("diff: %q", r.stdout)
	}
	if r := e.ok("", "pr", "diff", "--name-only"); r.stdout != "shop.txt\n" {
		t.Fatalf("diff --name-only: %q", r.stdout)
	}
	e.jsonOK(nil, "pr", "diff", "1")
	e.fail(ExitNotFound, "not_found", "pr", "view", "no-such-branch", "--json")
	e.fail(ExitNotFound, "not_found", "pr", "view", "99", "--json")

	// The tester reviews: inline comment, then request changes with an inline note.
	e.as(tomTok, func() {
		var st map[string][]map[string]any
		e.jsonOK(&st, "pr", "status")
		if len(st["review_requested"]) != 1 {
			t.Fatalf("status: %v", st)
		}
		var c map[string]any
		e.jsonOK(&c, "pr", "comment", "1", "--file", "shop.txt:2", "-b", "Too cheap?")
		if c["path"] != "shop.txt" || c["line"] != float64(2) {
			t.Fatalf("inline comment: %v", c)
		}
		e.fail(ExitConflict, "validation_failed", "pr", "comment", "1", "--file", "shop.txt:50", "-b", "x", "--json")
		var rv map[string]any
		e.jsonOK(&rv, "pr", "review", "1", "--request-changes", "-b", "Prices need work", "--inline", "shop.txt:1=Sword should be 12")
		if rv["state"] != "changes_requested" || len(rv["comments"].([]any)) != 1 {
			t.Fatalf("review: %v", rv)
		}
		e.fail(ExitUsage, CodeUsage, "pr", "review", "1", "--approve", "--comment", "--json")
		e.fail(ExitForbidden, "forbidden", "pr", "merge", "1", "--json")
	})

	// Admin protects main.
	e.as(adminTok, func() {
		var rule map[string]any
		e.jsonOK(&rule, "protection", "set", "main", "--approvals", "1")
		if rule["required_approvals"] != float64(1) || rule["require_pull_request"] != true {
			t.Fatalf("rule: %v", rule)
		}
		e.jsonOK(&rule, "protection", "set", "main", "--up-to-date")
		if rule["require_up_to_date"] != true || rule["required_approvals"] != float64(1) {
			t.Fatalf("rule update kept other fields? %v", rule)
		}
		e.jsonOK(nil, "protection", "list")
	})
	e.fail(ExitForbidden, "forbidden", "protection", "set", "dev", "--json")

	// Merging is blocked; the JSON error lists the blockers.
	r := e.fail(ExitConflict, "conflict", "pr", "merge", "1", "--squash", "--json")
	var errOut struct {
		Error struct {
			Details struct {
				Blockers []struct{ Code string } `json:"blockers"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stderr), &errOut); err != nil || len(errOut.Error.Details.Blockers) != 2 {
		t.Fatalf("blockers in error: %s", r.stderr)
	}

	// The author replies, fixes and pushes; the tester approves.
	var comments []map[string]any
	e.jsonOK(&comments, "pr", "comments", "1", "--unresolved")
	if len(comments) != 2 {
		t.Fatalf("unresolved threads: %v", comments)
	}
	first := int64(comments[0]["id"].(float64))
	e.ok("", "pr", "comment", "--reply-to", strconv.FormatInt(first, 10), "-b", "Raised to 12")
	if err := os.WriteFile(filepath.Join(dir, "shop.txt"), []byte("sword 12\nshield 15\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.git(dir, "commit", "-qam", "Raise sword price")
	e.git(dir, "push", "-q")
	if r := e.ok("", "pr", "view", "--comments"); !strings.Contains(r.stdout, "Raised to 12") || !strings.Contains(r.stdout, "blocked:") {
		t.Fatalf("human view: %s", r.stdout)
	}
	e.as(tomTok, func() {
		e.jsonOK(nil, "pr", "review", "shop", "--approve")
	})

	// Edit, draft round trip, then merge with squash and delete the branch.
	e.jsonOK(&pr, "pr", "edit", "1", "--title", "Shop prices", "--remove-reviewer", "tom")
	if pr["title"] != "Shop prices" {
		t.Fatalf("edit: %v", pr)
	}
	e.jsonOK(&pr, "pr", "ready", "1", "--undo")
	if pr["draft"] != true {
		t.Fatal("not a draft")
	}
	e.jsonOK(&pr, "pr", "ready", "1")
	e.jsonOK(&pr, "pr", "merge", "1", "--squash", "--delete-branch", "--sync")
	if pr["state"] != "merged" || pr["merge_method"] != "squash" {
		t.Fatalf("merged: %v", pr)
	}
	if b := strings.TrimSpace(e.git(dir, "rev-parse", "--abbrev-ref", "HEAD")); b != "main" {
		t.Fatalf("--sync left us on %s", b)
	}
	if log := e.git(dir, "log", "-1", "--format=%s"); strings.TrimSpace(log) != "Shop prices (#1)" {
		t.Fatalf("local main after sync: %q", log)
	}
	e.jsonOK(&list, "pr", "list", "--state", "merged")
	if len(list) != 1 {
		t.Fatalf("merged list: %v", list)
	}

	// Second PR: close/reopen, checkout, update-branch.
	e.git(dir, "checkout", "-q", "-b", "quests", "HEAD~1")
	if err := os.WriteFile(filepath.Join(dir, "quests.txt"), []byte("slay dragon\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.git(dir, "add", ".")
	e.git(dir, "commit", "-qm", "Quests")
	e.git(dir, "push", "-q", "origin", "quests")
	e.jsonOK(&pr, "pr", "create", "--title", "Quests", "--body", "Adds quests", "--draft")
	if pr["draft"] != true || pr["number"] != float64(2) {
		t.Fatalf("draft PR: %v", pr)
	}
	e.jsonOK(&pr, "pr", "close", "2", "--comment", "Not yet")
	if pr["state"] != "closed" {
		t.Fatal("not closed")
	}
	e.jsonOK(&pr, "pr", "reopen", "2")
	e.git(dir, "checkout", "-q", "main")
	var co map[string]any
	e.jsonOK(&co, "pr", "checkout", "2")
	if co["branch"] != "quests" || strings.TrimSpace(e.git(dir, "rev-parse", "--abbrev-ref", "HEAD")) != "quests" {
		t.Fatalf("checkout: %v", co)
	}
	e.jsonOK(&pr, "pr", "update-branch", "2")
	if pr["behind_by"] != float64(0) {
		t.Fatalf("update-branch: %v", pr["behind_by"])
	}
	e.fail(ExitConflict, "conflict", "pr", "update-branch", "2", "--json")
	e.as(adminTok, func() {
		e.jsonOK(nil, "protection", "delete", "main")
		e.fail(ExitNotFound, "not_found", "protection", "delete", "main", "--json")
	})
}
