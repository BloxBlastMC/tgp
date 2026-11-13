package server

import (
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

// prEnv: a repo "game" (README on main), an admin, maintainer "mia" and
// tester "tom" (both all-repos), and a clone owned by mia.
type prEnv struct {
	*testEnv
	admin, mia, tom *client
	g               *gitClient
	dir             string
	miaToken        string
}

func newPREnv(t *testing.T) *prEnv {
	e := newEnv(t)
	admin, _ := e.setup()
	admin.mustDo("POST", "/repos", map[string]any{"name": "game", "auto_init": true}, 201)
	mia := e.activeUser(admin, "mia", "maintainer", "mias-password-1")
	tom := e.activeUser(admin, "tom", "tester", "toms-password-1")
	for _, u := range []string{"mia", "tom"} {
		admin.mustDo("PUT", "/admin/users/"+u+"/repos", map[string]any{"all_repos": true, "repos": []string{}}, 200)
	}
	p := &prEnv{testEnv: e, admin: admin, mia: mia, tom: tom, g: e.gitClient()}
	p.miaToken = e.createToken(mia, "read", "write")
	p.dir = p.g.clone("game", p.miaToken)
	return p
}

// commit writes a file on branch (creating the branch from current HEAD if
// needed) and pushes it. Returns git's output.
func (p *prEnv) commit(branch, file, content, msg string) string {
	p.t.Helper()
	if _, err := p.g.run(p.dir, "checkout", "-q", branch); err != nil {
		p.g.must(p.dir, "checkout", "-q", "-b", branch)
	}
	writeFile(p.t, p.dir, file, content)
	p.g.must(p.dir, "add", ".")
	p.g.must(p.dir, "commit", "-qm", msg)
	return p.g.must(p.dir, "push", "origin", branch)
}

func (p *prEnv) pull(c *client, n int) apigen.PullRequest {
	p.t.Helper()
	var pr apigen.PullRequest
	c.mustDo("GET", "/repos/game/pulls/"+itoa(int64(n)), nil, 200).decode(p.t, &pr)
	return pr
}

func blockerCodes(pr apigen.PullRequest) string {
	var cs []string
	for _, b := range pr.Blockers {
		cs = append(cs, string(b.Code))
	}
	return strings.Join(cs, ",")
}

func TestPullRequestLifecycle(t *testing.T) {
	p := newPREnv(t)
	out := p.commit("feat", "game.txt", "line 1\nline 2\nline 3\n", "add game")
	if !strings.Contains(out, "open a pull request for feat") || !strings.Contains(out, "/game/compare/main...feat") {
		t.Fatalf("push hint missing:\n%s", out)
	}

	// Validation.
	for _, c := range []struct {
		body map[string]any
		code string
	}{
		{map[string]any{"title": "x", "head": "main"}, "validation_failed"},
		{map[string]any{"title": "x", "head": "nope"}, "validation_failed"},
		{map[string]any{"title": " ", "head": "feat"}, "validation_failed"},
		{map[string]any{"title": "x", "head": "feat", "reviewers": []string{"mia"}}, "validation_failed"},
		{map[string]any{"title": "x", "head": "feat", "reviewers": []string{"ghost"}}, "validation_failed"},
	} {
		if r := p.mia.do("POST", "/repos/game/pulls", c.body); r.code() != c.code {
			t.Errorf("%v: %d %s", c.body, r.status, r.body)
		}
	}
	// Testers can't open pull requests.
	if r := p.tom.do("POST", "/repos/game/pulls", map[string]any{"title": "x", "head": "feat"}); r.status != 403 {
		t.Fatalf("tester created PR: %d", r.status)
	}

	var pr apigen.PullRequest
	p.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "Add game", "body": "Adds **the game**.", "head": "feat", "reviewers": []string{"tom"}}, 201).decode(t, &pr)
	if pr.Number != 1 || pr.State != "open" || pr.Base.Branch != "main" || pr.Commits != 1 || pr.ChangedFiles != 1 || pr.Additions != 3 ||
		pr.Mergeable != "clean" || !pr.CanMerge || !strings.Contains(pr.BodyHtml, "<strong>the game</strong>") ||
		len(pr.RequestedReviewers) != 1 || pr.ReviewDecision != "none" {
		t.Fatalf("created PR: %+v", pr)
	}
	if r := p.mia.do("POST", "/repos/game/pulls", map[string]any{"title": "dup", "head": "feat"}); r.status != 409 {
		t.Fatalf("duplicate PR: %d %s", r.status, r.body)
	}
	// The tester sees it but can't merge it.
	if tp := p.pull(p.tom, 1); tp.CanMerge || blockerCodes(tp) != "permission" || tp.ViewerPermission != "triage" {
		t.Fatalf("tester view: %v %s", tp.CanMerge, blockerCodes(tp))
	}
	// Review requests show up for the reviewer.
	var mine apigen.PullPage
	p.tom.mustDo("GET", "/pulls?filter=review_requested", nil, 200).decode(t, &mine)
	if mine.Total != 1 || mine.Pulls[0].Repo != "game" || mine.Pulls[0].Number != 1 {
		t.Fatalf("review requested: %+v", mine)
	}
	// Pushing again to the branch reports the pull request.
	if out := p.commit("feat", "game.txt", "line 1\nline 2 changed\nline 3\n", "tweak"); !strings.Contains(out, "updated pull request #1") {
		t.Fatalf("push hint for existing PR missing:\n%s", out)
	}
	pr = p.pull(p.mia, 1)
	if pr.Commits != 2 {
		t.Fatalf("commits after push = %d", pr.Commits)
	}

	// Comments: conversation, inline (must be on a diff line), replies, resolve.
	p.tom.mustDo("POST", "/repos/game/pulls/1/comments", map[string]any{"body": "Looks fun"}, 201)
	var inline apigen.Comment
	p.tom.mustDo("POST", "/repos/game/pulls/1/comments", map[string]any{"body": "Typo?", "path": "game.txt", "line": 2}, 201).decode(t, &inline)
	if inline.DiffHunk == nil || !strings.Contains(*inline.DiffHunk, "+line 2 changed") || *inline.Side != "new" || deref(inline.Outdated) {
		t.Fatalf("inline comment: %+v", inline)
	}
	if r := p.tom.do("POST", "/repos/game/pulls/1/comments", map[string]any{"body": "x", "path": "game.txt", "line": 99}); r.code() != "validation_failed" {
		t.Fatalf("comment outside diff: %d %s", r.status, r.body)
	}
	if r := p.tom.do("POST", "/repos/game/pulls/1/comments", map[string]any{"body": "x", "path": "README.md", "line": 1}); r.code() != "validation_failed" {
		t.Fatalf("comment on unchanged file: %d %s", r.status, r.body)
	}
	var reply apigen.Comment
	p.mia.mustDo("POST", "/repos/game/pulls/1/comments", map[string]any{"body": "Fixed", "reply_to": inline.Id}, 201).decode(t, &reply)
	if deref(reply.ReplyTo) != inline.Id || deref(reply.Path) != "game.txt" {
		t.Fatalf("reply: %+v", reply)
	}
	var resolved apigen.Comment
	p.mia.mustDo("PATCH", "/repos/game/comments/"+itoa(inline.Id), map[string]any{"resolved": true}, 200).decode(t, &resolved)
	if !deref(resolved.Resolved) || resolved.ResolvedBy == nil || resolved.ResolvedBy.Username != "mia" {
		t.Fatalf("resolve: %+v", resolved)
	}
	if r := p.mia.do("PATCH", "/repos/game/comments/"+itoa(inline.Id), map[string]any{"body": "hijack"}); r.status != 403 {
		t.Fatalf("edited someone else's comment: %d", r.status)
	}

	// Reviews: the author can only comment; the tester requests changes.
	if r := p.mia.do("POST", "/repos/game/pulls/1/reviews", map[string]any{"state": "approve"}); r.status != 403 {
		t.Fatalf("self-approve: %d", r.status)
	}
	if r := p.tom.do("POST", "/repos/game/pulls/1/reviews", map[string]any{"state": "request_changes"}); r.code() != "validation_failed" {
		t.Fatalf("empty change request: %d %s", r.status, r.body)
	}
	if r := p.tom.do("POST", "/repos/game/pulls/1/reviews", map[string]any{"state": "approve", "commit_sha": strings.Repeat("a", 40)}); r.status != 409 {
		t.Fatalf("review of stale commit: %d", r.status)
	}
	var rev apigen.Review
	p.tom.mustDo("POST", "/repos/game/pulls/1/reviews", map[string]any{
		"state": "request_changes", "body": "Please add line 4",
		"comments": []map[string]any{{"path": "game.txt", "line": 3, "body": "after this"}},
	}, 201).decode(t, &rev)
	if rev.State != "changes_requested" || len(rev.Comments) != 1 || rev.Stale {
		t.Fatalf("review: %+v", rev)
	}
	pr = p.pull(p.mia, 1)
	if pr.ReviewDecision != "changes_requested" || len(pr.RequestedReviewers) != 0 || len(pr.Reviewers) != 1 || pr.Reviewers[0].State != "changes_requested" {
		t.Fatalf("after change request: %+v %+v", pr.ReviewDecision, pr.Reviewers)
	}
	// No protection rule: change requests don't block merging.
	if !pr.CanMerge {
		t.Fatalf("blocked without a rule: %s", blockerCodes(pr))
	}

	// Protect main: PRs only, 1 approval, stale approvals dismissed.
	var rule apigen.BranchProtection
	if r := p.mia.do("POST", "/repos/game/protections", map[string]any{"pattern": "main"}); r.status != 403 {
		t.Fatalf("maintainer created protection: %d", r.status)
	}
	p.admin.mustDo("POST", "/repos/game/protections", map[string]any{"pattern": "main"}, 201).decode(t, &rule)
	if !rule.RequirePullRequest || rule.RequiredApprovals != 1 || !rule.DismissStaleApprovals || !rule.BlockForcePush || rule.AllowDeletion {
		t.Fatalf("rule defaults: %+v", rule)
	}
	var branches []apigen.Ref
	p.mia.mustDo("GET", "/repos/game/branches", nil, 200).decode(t, &branches)
	for _, b := range branches {
		if (b.Name == "main") != deref(b.Protected) {
			t.Fatalf("branch %s protected = %v", b.Name, deref(b.Protected))
		}
	}
	// Direct pushes and deletion of main are rejected.
	p.g.must(p.dir, "checkout", "-q", "main")
	writeFile(t, p.dir, "direct.txt", "x")
	p.g.must(p.dir, "add", ".")
	p.g.must(p.dir, "commit", "-qm", "direct")
	if out, err := p.g.run(p.dir, "push", "origin", "main"); err == nil || !strings.Contains(out, "must go through a pull request") {
		t.Fatalf("direct push to protected main: %v\n%s", err, out)
	}
	p.g.must(p.dir, "reset", "-q", "--hard", "origin/main")
	if out, err := p.g.run(p.dir, "push", "origin", ":main"); err == nil {
		t.Fatalf("deleted protected main:\n%s", out)
	}

	pr = p.pull(p.mia, 1)
	if blockerCodes(pr) != "approvals,changes_requested" || pr.ReviewDecision != "changes_requested" {
		t.Fatalf("blockers with rule: %s %s", blockerCodes(pr), pr.ReviewDecision)
	}
	r := p.mia.do("POST", "/repos/game/pulls/1/merge", map[string]any{})
	if r.status != 409 || !strings.Contains(string(r.body), `"blockers"`) {
		t.Fatalf("merge despite blockers: %d %s", r.status, r.body)
	}
	// Maintainer can't bypass; tester can't dismiss.
	if r := p.mia.do("POST", "/repos/game/pulls/1/merge", map[string]any{"bypass_protection": true}); r.status != 403 {
		t.Fatalf("maintainer bypass: %d %s", r.status, r.body)
	}
	if r := p.tom.do("POST", "/repos/game/pulls/1/reviews/"+itoa(rev.Id)+"/dismiss", map[string]any{"message": "x"}); r.status != 403 {
		t.Fatalf("tester dismissed: %d", r.status)
	}

	// The tester approves: their latest verdict replaces the change request.
	p.tom.mustDo("POST", "/repos/game/pulls/1/reviews", map[string]any{"state": "approve", "commit_sha": pr.Head.Sha}, 201)
	pr = p.pull(p.mia, 1)
	if !pr.CanMerge || pr.ReviewDecision != "approved" {
		t.Fatalf("after approval: %s %s", blockerCodes(pr), pr.ReviewDecision)
	}
	// A new push makes the approval stale.
	p.commit("feat", "game.txt", "line 1\nline 2 changed\nline 3\nline 4\n", "line 4")
	pr = p.pull(p.mia, 1)
	if blockerCodes(pr) != "approvals" || !pr.Reviewers[0].Stale {
		t.Fatalf("after push: %s %+v", blockerCodes(pr), pr.Reviewers)
	}
	// The earlier inline comment on line 2 is now outdated.
	var comments []apigen.Comment
	p.mia.mustDo("GET", "/repos/game/pulls/1/comments", nil, 200).decode(t, &comments)
	if len(comments) != 4 || !deref(comments[1].Outdated) {
		t.Fatalf("comments: %d, outdated=%v", len(comments), comments[1].Outdated)
	}
	p.tom.mustDo("POST", "/repos/game/pulls/1/reviews", map[string]any{"state": "approve"}, 201)

	// Squash-merge and delete the branch.
	var merged apigen.PullRequest
	p.mia.mustDo("POST", "/repos/game/pulls/1/merge", map[string]any{"method": "squash", "delete_branch": true, "sha": p.pull(p.mia, 1).Head.Sha}, 200).decode(t, &merged)
	if merged.State != "merged" || deref(merged.MergeMethod) != "squash" || merged.MergedBy.Username != "mia" || merged.Head.Exists || merged.CanMerge {
		t.Fatalf("merged: %+v", merged)
	}
	p.g.must(p.dir, "fetch", "-q", "--prune", "origin")
	log := p.g.must(p.dir, "log", "--format=%s|%an|%cn", "origin/main", "-2")
	if !strings.HasPrefix(log, "Add game (#1)|mia|mia\n") {
		t.Fatalf("main log:\n%s", log)
	}
	if out, _ := p.g.run(p.dir, "rev-parse", "--verify", "-q", "origin/feat"); strings.TrimSpace(out) != "" {
		t.Fatal("feat wasn't deleted")
	}
	// Diff and commits of a merged PR still work.
	var diff apigen.Diff
	p.mia.mustDo("GET", "/repos/game/pulls/1/files", nil, 200).decode(t, &diff)
	if len(diff.Files) != 1 || diff.Files[0].NewPath != "game.txt" {
		t.Fatalf("merged diff: %+v", diff.Files)
	}
	var commits []apigen.Commit
	p.mia.mustDo("GET", "/repos/game/pulls/1/commits", nil, 200).decode(t, &commits)
	if len(commits) != 3 {
		t.Fatalf("merged commits = %d", len(commits))
	}

	// Timeline.
	var tl []apigen.TimelineItem
	p.mia.mustDo("GET", "/repos/game/pulls/1/timeline", nil, 200).decode(t, &tl)
	kinds := map[string]int{}
	var evs []string
	for _, it := range tl {
		kinds[string(it.Kind)]++
		if it.Event != nil {
			evs = append(evs, string(it.Event.Type))
		}
	}
	if kinds["comment"] != 1 || kinds["inline_comment"] != 1 || kinds["review"] != 3 || strings.Join(evs, ",") != "pushed,pushed,merged,branch_deleted" {
		t.Fatalf("timeline kinds %v events %v", kinds, evs)
	}
	var page apigen.PullPage
	p.mia.mustDo("GET", "/repos/game/pulls?state=merged", nil, 200).decode(t, &page)
	if page.Total != 1 || page.Pulls[0].State != "merged" || page.Pulls[0].ReviewDecision != "approved" {
		t.Fatalf("merged list: %+v", page)
	}
	p.mia.mustDo("GET", "/repos/game/pulls", nil, 200).decode(t, &page)
	if page.Total != 0 {
		t.Fatalf("open list: %+v", page)
	}
}

func TestMergeMethodsConflictsAndUpdateBranch(t *testing.T) {
	p := newPREnv(t)
	p.commit("a", "README.md", "# changed by a\n", "a")
	p.g.must(p.dir, "checkout", "-q", "main")
	p.commit("b", "b.txt", "b1\n", "b1")
	p.commit("b", "b2.txt", "b2\n", "b2")
	p.g.must(p.dir, "checkout", "-q", "main")
	p.commit("c", "README.md", "# conflict\n", "conflicting readme")
	p.g.must(p.dir, "checkout", "-q", "main")
	for _, h := range []string{"a", "b", "c"} {
		p.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "PR " + h, "head": h}, 201)
	}

	// Plain merge commit.
	var m apigen.PullRequest
	p.mia.mustDo("POST", "/repos/game/pulls/1/merge", map[string]any{"method": "merge"}, 200).decode(t, &m)
	p.g.must(p.dir, "fetch", "-q", "origin")
	if parents := strings.Fields(p.g.must(p.dir, "log", "-1", "--format=%P", "origin/main")); len(parents) != 2 {
		t.Fatalf("merge commit parents: %v", parents)
	}
	if subj := strings.TrimSpace(p.g.must(p.dir, "log", "-1", "--format=%s", "origin/main")); subj != "Merge pull request #1 from a" {
		t.Fatalf("merge subject %q", subj)
	}

	// b is now behind main; require up-to-date.
	p.admin.mustDo("POST", "/repos/game/protections", map[string]any{"pattern": "main", "required_approvals": 0, "require_up_to_date": true}, 201)
	pr := p.pull(p.mia, 2)
	if blockerCodes(pr) != "behind" || deref(pr.BehindBy) != 2 {
		t.Fatalf("behind: %s %v", blockerCodes(pr), pr.BehindBy)
	}
	p.mia.mustDo("POST", "/repos/game/pulls/2/update-branch", map[string]any{"sha": pr.Head.Sha}, 200).decode(t, &pr)
	if !pr.CanMerge || deref(pr.BehindBy) != 0 {
		t.Fatalf("after update-branch: %s", blockerCodes(pr))
	}
	if r := p.mia.do("POST", "/repos/game/pulls/2/update-branch", nil); r.status != 409 {
		t.Fatalf("second update-branch: %d", r.status)
	}
	// Up to date with main, so rebase is a fast-forward to the head.
	head := pr.Head.Sha
	p.mia.mustDo("POST", "/repos/game/pulls/2/merge", map[string]any{"method": "rebase"}, 200).decode(t, &pr)
	if deref(pr.MergeCommit) != head {
		t.Fatalf("fast-forward rebase landed %s, want %s", deref(pr.MergeCommit), head)
	}

	// c conflicts.
	pr = p.pull(p.mia, 3)
	if pr.Mergeable != "conflict" || strings.Join(pr.ConflictFiles, ",") != "README.md" || !strings.Contains(blockerCodes(pr), "conflict") {
		t.Fatalf("conflict PR: %s %v %s", pr.Mergeable, pr.ConflictFiles, blockerCodes(pr))
	}
	if r := p.mia.do("POST", "/repos/game/pulls/3/update-branch", nil); r.status != 409 || !strings.Contains(string(r.body), "README.md") {
		t.Fatalf("update-branch with conflict: %d %s", r.status, r.body)
	}
	var cmp apigen.Comparison
	p.mia.mustDo("GET", "/repos/game/compare?base=main&head=c", nil, 200).decode(t, &cmp)
	if cmp.Mergeable != "conflict" || cmp.AheadBy != 1 || cmp.BehindBy != 5 || deref(cmp.ExistingPull) != 3 || len(cmp.Diff.Files) != 1 {
		t.Fatalf("compare: %+v", cmp)
	}
	// Close and reopen.
	p.mia.mustDo("PATCH", "/repos/game/pulls/3", map[string]any{"state": "closed"}, 200).decode(t, &pr)
	if pr.State != "closed" || blockerCodes(pr) != "closed" {
		t.Fatalf("closed: %s %s", pr.State, blockerCodes(pr))
	}
	p.mia.mustDo("PATCH", "/repos/game/pulls/3", map[string]any{"state": "open", "draft": true, "title": "Readme"}, 200).decode(t, &pr)
	if pr.State != "open" || !pr.Draft || pr.Title != "Readme" || !strings.Contains(blockerCodes(pr), "draft") {
		t.Fatalf("reopened draft: %+v %s", pr.State, blockerCodes(pr))
	}
}

func TestRebaseMergeAndManualMerge(t *testing.T) {
	p := newPREnv(t)
	p.commit("r", "r1.txt", "1\n", "r1")
	p.commit("r", "r2.txt", "2\n", "r2")
	p.g.must(p.dir, "checkout", "-q", "main")
	p.commit("main", "m.txt", "m\n", "main moves")
	p.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "Rebase me", "head": "r"}, 201)
	p.mia.mustDo("POST", "/repos/game/pulls/1/merge", map[string]any{"method": "rebase"}, 200)
	p.g.must(p.dir, "fetch", "-q", "origin")
	if log := p.g.must(p.dir, "log", "--format=%s", "origin/main", "-4"); log != "r2\nr1\nmain moves\nInitial commit\n" {
		t.Fatalf("rebase log:\n%s", log)
	}

	// A pull request whose head is pushed to main directly is marked merged.
	p.g.must(p.dir, "checkout", "-q", "-B", "main", "origin/main")
	p.commit("x", "x.txt", "x\n", "x")
	p.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "X", "head": "x"}, 201)
	p.g.must(p.dir, "checkout", "-q", "main")
	p.g.must(p.dir, "merge", "-q", "--ff-only", "x")
	p.g.must(p.dir, "push", "-q", "origin", "main")
	pr := p.pull(p.mia, 2)
	if pr.State != "merged" || deref(pr.MergeMethod) != "manual" || pr.MergedBy == nil || pr.MergedBy.Username != "mia" {
		t.Fatalf("manual merge: %+v", pr)
	}
}

func TestAdminBypassAndBranchDeletion(t *testing.T) {
	p := newPREnv(t)
	p.commit("f", "f.txt", "f\n", "f")
	p.admin.mustDo("POST", "/repos/game/protections", map[string]any{"pattern": "main", "admins_bypass": true}, 201)
	p.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "F", "head": "f"}, 201)

	// Admin view: the approval blocker is bypassable.
	pr := p.pull(p.admin, 1)
	if blockerCodes(pr) != "approvals" || !pr.Blockers[0].Bypassable {
		t.Fatalf("admin blockers: %+v", pr.Blockers)
	}
	if mp := p.pull(p.mia, 1); mp.Blockers[0].Bypassable {
		t.Fatal("bypassable for a maintainer")
	}
	// Branch deletion rules.
	if r := p.mia.do("POST", "/repos/game/branches/delete", map[string]any{"name": "main"}); r.status != 409 {
		t.Fatalf("delete default: %d", r.status)
	}
	if r := p.mia.do("POST", "/repos/game/branches/delete", map[string]any{"name": "f"}); r.status != 409 || !strings.Contains(string(r.body), "#1") {
		t.Fatalf("delete PR head: %d %s", r.status, r.body)
	}
	if r := p.tom.do("POST", "/repos/game/branches/delete", map[string]any{"name": "f"}); r.status != 403 {
		t.Fatalf("tester delete: %d", r.status)
	}

	if r := p.admin.do("POST", "/repos/game/pulls/1/merge", nil); r.status != 409 {
		t.Fatalf("admin merge without bypass: %d", r.status)
	}
	p.admin.mustDo("POST", "/repos/game/pulls/1/merge", map[string]any{"bypass_protection": true}, 200)
	var audit struct {
		Entries []struct{ Action, Target string }
	}
	p.admin.mustDo("GET", "/admin/audit-log?action=pull.", nil, 200).decode(t, &audit)
	if len(audit.Entries) != 1 || audit.Entries[0].Target != "game#1" {
		t.Fatalf("bypass audit: %+v", audit.Entries)
	}
	// Admins with bypass can push directly; maintainers can't.
	p.g.must(p.dir, "checkout", "-q", "main")
	p.g.must(p.dir, "pull", "-q", "origin", "main")
	writeFile(t, p.dir, "y.txt", "y")
	p.g.must(p.dir, "add", ".")
	p.g.must(p.dir, "commit", "-qm", "y")
	if _, err := p.g.run(p.dir, "push", "origin", "main"); err == nil {
		t.Fatal("maintainer pushed to protected main")
	}
	adminDir := p.g.clone("game", p.createToken(p.admin, "read", "write"))
	writeFile(t, adminDir, "z.txt", "z")
	p.g.must(adminDir, "add", ".")
	p.g.must(adminDir, "commit", "-qm", "z")
	p.g.must(adminDir, "push", "-q", "origin", "main")

	// Now f is merged; deleting it works, and the PR shows it gone.
	p.mia.mustDo("POST", "/repos/game/branches/delete", map[string]any{"name": "f"}, 204)
	if pr := p.pull(p.mia, 1); pr.Head.Exists {
		t.Fatal("head still exists")
	}
	if r := p.mia.do("POST", "/repos/game/branches/delete", map[string]any{"name": "f"}); r.status != 404 {
		t.Fatalf("delete twice: %d", r.status)
	}
}

func TestHeadDeletedAndBaseChange(t *testing.T) {
	p := newPREnv(t)
	p.commit("dev", "d.txt", "d\n", "dev")
	p.commit("feat", "f.txt", "f\n", "feat on dev")
	var pr apigen.PullRequest
	p.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "Feat", "head": "feat", "base": "dev"}, 201).decode(t, &pr)
	if pr.Commits != 1 {
		t.Fatalf("stacked commits = %d", pr.Commits)
	}
	p.mia.mustDo("PATCH", "/repos/game/pulls/1", map[string]any{"base": "main"}, 200).decode(t, &pr)
	if pr.Base.Branch != "main" || pr.Commits != 2 {
		t.Fatalf("after base change: %s %d", pr.Base.Branch, pr.Commits)
	}
	// Deleting the head by push: the PR can't be merged or reopened.
	p.g.must(p.dir, "push", "-q", "origin", ":feat")
	pr = p.pull(p.mia, 1)
	if pr.Head.Exists || !strings.Contains(blockerCodes(pr), "head_missing") {
		t.Fatalf("head deleted: %v %s", pr.Head.Exists, blockerCodes(pr))
	}
	p.mia.mustDo("PATCH", "/repos/game/pulls/1", map[string]any{"state": "closed"}, 200)
	if r := p.mia.do("PATCH", "/repos/game/pulls/1", map[string]any{"state": "open"}); r.status != 409 {
		t.Fatalf("reopen without head: %d", r.status)
	}
	// Pushing the branch again restores it.
	p.g.must(p.dir, "push", "-q", "origin", "feat")
	p.mia.mustDo("PATCH", "/repos/game/pulls/1", map[string]any{"state": "open"}, 200).decode(t, &pr)
	if !pr.Head.Exists || pr.State != "open" {
		t.Fatalf("reopened: %+v", pr.Head)
	}
	var tl []apigen.TimelineItem
	p.mia.mustDo("GET", "/repos/game/pulls/1/timeline", nil, 200).decode(t, &tl)
	var evs []string
	for _, it := range tl {
		if it.Event != nil {
			evs = append(evs, string(it.Event.Type))
		}
	}
	if strings.Join(evs, ",") != "base_changed,head_deleted,closed,reopened" {
		t.Fatalf("events: %v", evs)
	}
}

// Without a base URL, pushes still work (no hints) instead of failing the hook.
func TestPushWithoutBaseURL(t *testing.T) {
	p := newPREnv(t)
	p.admin.mustDo("PATCH", "/admin/org", map[string]any{"base_url": ""}, 200)
	out := p.commit("nohint", "n.txt", "n\n", "n")
	if strings.Contains(out, "HTTP 500") || strings.Contains(out, "open a pull request") {
		t.Fatalf("push output:\n%s", out)
	}
}
