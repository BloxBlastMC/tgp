package server

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/git"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/githook"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// Payloads follow GitHub's shapes where tgp has the same concepts
// (action, repository, sender, organization, ref/before/after/commits),
// with tgp's own API objects for pull requests, issues, comments, reviews
// and releases.

func (s *Server) repoPayload(oi orgInfo, repo *model.Repo) map[string]any {
	html := strings.TrimRight(oi.base, "/") + "/" + repo.Name
	return map[string]any{
		"id": repo.ID, "name": repo.Name, "full_name": oi.owner + "/" + repo.Name, "private": true,
		"description": repo.Description, "default_branch": repo.DefaultBranch, "archived": repo.Archived,
		"html_url": html, "clone_url": html + ".git",
	}
}

func (s *Server) senderPayload(u *model.User) map[string]any {
	if u == nil {
		return map[string]any{"login": "tgp", "type": "Bot"}
	}
	return map[string]any{"id": u.ID, "login": u.Username, "name": u.DisplayName, "type": "User"}
}

// hasHooks reports whether any active webhook may want event from repo
// (a cheap check before building payloads).
func (s *Server) hasHooks(ctx context.Context, repoID int64, event string) bool {
	var hooks []*model.Webhook
	if err := s.db.NewSelect().Model(&hooks).Column("id", "events").Where("wh.active").
		Where("wh.repo_id = ? OR wh.repo_id IS NULL", repoID).Scan(ctx); err != nil {
		s.log.Warn("webhooks: list", "err", err)
		return false
	}
	for _, h := range hooks {
		if hookWants(h, event) {
			return true
		}
	}
	return false
}

// emit queues event for repo's webhooks. It never fails the caller.
func (s *Server) emit(ctx context.Context, db bun.IDB, repo *model.Repo, actor *model.User, event, action string, fields map[string]any) {
	if db == nil {
		db = s.db
	}
	ctx = context.WithoutCancel(ctx)
	oi := s.loadOrgInfoIn(ctx, db)
	payload := map[string]any{
		"repository":   s.repoPayload(oi, repo),
		"sender":       s.senderPayload(actor),
		"organization": map[string]any{"login": oi.owner},
	}
	if action != "" {
		payload["action"] = action
	}
	for k, v := range fields {
		payload[k] = v
	}
	if err := s.enqueue(ctx, db, repo.ID, event, action, payload); err != nil {
		s.log.Error("webhooks: queue", "repo", repo.Name, "event", event, "err", err)
	}
}

// loadOrgInfoIn is loadOrgInfo usable inside a transaction (SQLite has
// one connection, so the outer handle would deadlock).
func (s *Server) loadOrgInfoIn(ctx context.Context, db bun.IDB) orgInfo {
	if db == bun.IDB(s.db) {
		return s.loadOrgInfo(ctx)
	}
	var oi orgInfo
	var org model.OrgSettings
	if err := db.NewSelect().Model(&org).Limit(1).Scan(ctx); err == nil {
		oi.owner, oi.base = org.Name, org.BaseURL
	}
	if s.cfg.Server.BaseURL != "" {
		oi.base = s.cfg.Server.BaseURL
	}
	return oi
}

// actorCtx gives ctx a principal for actor when it has none (git hooks),
// so API conversions work.
func actorCtx(ctx context.Context, actor *model.User) context.Context {
	if principal(ctx) != nil || actor == nil {
		return ctx
	}
	return context.WithValue(ctx, principalKey, &Principal{User: actor, Method: MethodSession})
}

// ---- push, create, delete ----

// hooksOnPush sends push (and create/delete) events for ref updates.
func (s *Server) hooksOnPush(ctx context.Context, repo *model.Repo, actor *model.User, updates []githook.Update) {
	wantPush, wantCreate, wantDelete := s.hasHooks(ctx, repo.ID, "push"), s.hasHooks(ctx, repo.ID, "create"), s.hasHooks(ctx, repo.ID, "delete")
	if !wantPush && !wantCreate && !wantDelete {
		return
	}
	g := s.repos.Open(repo)
	oi := s.loadOrgInfo(ctx)
	html := strings.TrimRight(oi.base, "/") + "/" + repo.Name
	for _, u := range updates {
		var refType, name string
		switch {
		case strings.HasPrefix(u.Ref, "refs/heads/"):
			refType, name = "branch", strings.TrimPrefix(u.Ref, "refs/heads/")
		case strings.HasPrefix(u.Ref, "refs/tags/"):
			refType, name = "tag", strings.TrimPrefix(u.Ref, "refs/tags/")
		default:
			continue // pull request refs and other internal refs
		}
		if u.IsCreate() && wantCreate {
			s.emit(ctx, nil, repo, actor, "create", "", map[string]any{"ref": name, "ref_type": refType, "master_branch": repo.DefaultBranch})
		}
		if u.IsDelete() && wantDelete {
			s.emit(ctx, nil, repo, actor, "delete", "", map[string]any{"ref": name, "ref_type": refType})
		}
		if !wantPush {
			continue
		}
		commits := []map[string]any{}
		total := 0
		var head any
		if !u.IsDelete() {
			base := u.Old
			if u.IsCreate() {
				base = ""
				if refType == "branch" && name != repo.DefaultBranch {
					if def, err := g.ResolveCommit(ctx, repo.DefaultBranch); err == nil {
						base, _ = g.MergeBase(ctx, def, u.New)
					}
				}
			}
			if refType == "branch" && base != "" && !u.Forced {
				total, _ = g.CountCommits(ctx, base, u.New)
				if cs, err := g.LogRange(ctx, base, u.New, 20); err == nil {
					for i := len(cs) - 1; i >= 0; i-- { // oldest first, like GitHub
						commits = append(commits, commitPayload(html, cs[i]))
					}
				}
			}
			if c, err := g.GetCommit(ctx, u.New); err == nil {
				head = commitPayload(html, *c)
				if refType == "branch" && (base == "" || u.Forced) && len(commits) == 0 {
					commits, total = []map[string]any{commitPayload(html, *c)}, 1
				}
			}
		}
		compare := ""
		switch {
		case u.IsCreate() || u.IsDelete():
		default:
			compare = html + "/compare/" + url.PathEscape(shortRef(u.Old)) + "..." + url.PathEscape(shortRef(u.New))
		}
		pusher := map[string]any{"name": "tgp"}
		if actor != nil {
			pusher = map[string]any{"name": actor.Username, "email": actor.Email}
		}
		s.emit(ctx, nil, repo, actor, "push", "", map[string]any{
			"ref": u.Ref, "before": u.Old, "after": u.New, "created": u.IsCreate(), "deleted": u.IsDelete(), "forced": u.Forced,
			"base_ref": nil, "compare": compare, "commits": commits, "total_commits": total, "head_commit": head, "pusher": pusher,
		})
	}
}

func shortRef(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func commitPayload(html string, c git.Commit) map[string]any {
	msg := c.Subject
	if c.Body != "" {
		msg += "\n\n" + c.Body
	}
	return map[string]any{
		"id": c.SHA, "message": msg, "timestamp": c.Committer.Date.Format(time.RFC3339), "url": html + "/commit/" + c.SHA,
		"author":    map[string]any{"name": c.Author.Name, "email": c.Author.Email},
		"committer": map[string]any{"name": c.Committer.Name, "email": c.Committer.Email},
	}
}

// ---- issues and pull requests (from timeline events) ----

type hookSeenKey struct{}

// hookScope makes ctx remember which timeline events were already sent as
// webhooks, so nested helpers and their callers don't send them twice.
func hookScope(ctx context.Context) context.Context {
	if ctx.Value(hookSeenKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, hookSeenKey{}, &sync.Map{})
}

// eventMark is the newest timeline event id; events a request adds come
// after it.
func (s *Server) eventMark(ctx context.Context) int64 {
	var id int64
	_ = s.db.NewSelect().Model((*model.IssueEvent)(nil)).ColumnExpr("COALESCE(MAX(id), 0)").Scan(ctx, &id)
	return id
}

// hookActions maps timeline event types to webhook actions.
var hookActions = map[string]string{
	evClosed: "closed", evReopened: "reopened", evMerged: "closed", evTitleChanged: "edited", evBaseChanged: "edited",
	evLabeled: "labeled", evUnlabeled: "unlabeled", evAssigned: "assigned", evUnassigned: "unassigned",
	evMilestoned: "milestoned", evDemilestoned: "demilestoned", evReady: "ready_for_review", evDraft: "converted_to_draft",
	evReviewRequested: "review_requested", evRequestRemoved: "review_request_removed", evPushed: "synchronize",
}

// hooksFromEvents sends issues and pull_request events for the timeline
// events actor added to repo after since. edited lists issue ids whose
// description changed (which leaves no timeline event).
func (s *Server) hooksFromEvents(ctx context.Context, repo *model.Repo, actor *model.User, since int64, edited ...int64) {
	if !s.hasHooks(ctx, repo.ID, "issues") && !s.hasHooks(ctx, repo.ID, "pull_request") {
		return
	}
	ctx = actorCtx(ctx, actor)
	seen, _ := ctx.Value(hookSeenKey{}).(*sync.Map)
	var evs []model.IssueEvent
	q := s.db.NewSelect().Model(&evs).Join("JOIN issues AS iss ON iss.id = issue_event.issue_id").
		Where("issue_event.id > ? AND iss.repo_id = ?", since, repo.ID).OrderExpr("issue_event.id")
	if actor != nil {
		q = q.Where("issue_event.actor_id = ? OR issue_event.actor_id IS NULL", actor.ID)
	}
	if err := q.Scan(ctx); err != nil {
		s.log.Warn("webhooks: timeline events", "err", err)
		return
	}
	type item struct {
		action string
		data   map[string]any
	}
	byIssue := map[int64][]item{}
	var order []int64
	add := func(id int64, it item) {
		if _, ok := byIssue[id]; !ok {
			order = append(order, id)
		}
		byIssue[id] = append(byIssue[id], it)
	}
	for _, ev := range evs {
		action, ok := hookActions[ev.Type]
		if !ok {
			continue
		}
		if seen != nil {
			if _, dup := seen.LoadOrStore(ev.ID, true); dup {
				continue
			}
		}
		add(ev.IssueID, item{action, jsonMap(ev.Data)})
	}
	for _, id := range edited {
		add(id, item{"edited", nil})
	}
	for _, issueID := range order {
		items := byIssue[issueID]
		issue := new(model.Issue)
		if err := s.db.NewSelect().Model(issue).Where("id = ?", issueID).Scan(ctx); err != nil {
			continue
		}
		perm, _ := authz.RepoPerm(ctx, s.db, actor, repo)
		event, key := "issues", "issue"
		var obj any
		if issue.IsPull {
			pc, err := s.loadPullIn(ctx, s.db, repo, perm, issue.Number)
			if err != nil {
				continue
			}
			v, err := s.toAPIPull(ctx, pc, s.users())
			if err != nil {
				s.log.Warn("webhooks: pull payload", "err", err)
				continue
			}
			event, key, obj = "pull_request", "pull_request", v
		} else {
			v, err := s.toAPIIssue(ctx, &issueCtx{repo: repo, perm: perm, issue: issue})
			if err != nil {
				s.log.Warn("webhooks: issue payload", "err", err)
				continue
			}
			obj = v
		}
		sentEdit, sentClose := false, false
		for _, it := range items {
			if it.action == "edited" {
				if sentEdit {
					continue
				}
				sentEdit = true
			}
			if it.action == "closed" {
				if sentClose {
					continue
				}
				sentClose = true
			}
			fields := map[string]any{key: obj, "number": issue.Number}
			switch it.action {
			case "labeled", "unlabeled":
				fields["label"] = map[string]any{"name": it.data["label"], "color": it.data["color"]}
			case "assigned", "unassigned":
				fields["assignee"] = map[string]any{"login": it.data["assignee"]}
			case "milestoned", "demilestoned":
				fields["milestone"] = map[string]any{"title": it.data["milestone"]}
			case "review_requested", "review_request_removed":
				fields["requested_reviewer"] = map[string]any{"login": it.data["reviewer"]}
			case "edited":
				if from, ok := it.data["from"]; ok {
					if _, base := it.data["to"]; base && issue.IsPull && it.data["from"] != nil && hookIsBaseChange(it.data) {
						fields["changes"] = map[string]any{"base": map[string]any{"ref": map[string]any{"from": from}}}
					} else {
						fields["changes"] = map[string]any{"title": map[string]any{"from": from}}
					}
				}
			case "synchronize":
				if b, ok := it.data["before"]; ok {
					fields["before"] = b
				}
				if a, ok := it.data["after"]; ok {
					fields["after"] = a
				}
			}
			s.emit(ctx, nil, repo, actor, event, it.action, fields)
		}
	}
}

// hookIsBaseChange tells base_changed data from title_changed data (both
// have from/to): base changes name branches, which can't contain spaces...
// so record it explicitly instead.
func hookIsBaseChange(data map[string]any) bool { _, ok := data["base"]; return ok }

// hooksOpened sends issues/pull_request "opened".
func (s *Server) hooksOpened(ctx context.Context, repo *model.Repo, actor *model.User, number int64, obj any, pull bool) {
	event, key := "issues", "issue"
	if pull {
		event, key = "pull_request", "pull_request"
	}
	if s.hasHooks(ctx, repo.ID, event) {
		s.emit(ctx, nil, repo, actor, event, "opened", map[string]any{key: obj, "number": number})
	}
}

// hooksComment sends issue_comment or pull_request_review_comment.
func (s *Server) hooksComment(ctx context.Context, repo *model.Repo, actor *model.User, issue *model.Issue, action string, c apigen.Comment) {
	event := "issue_comment"
	if c.Path != nil && *c.Path != "" {
		event = "pull_request_review_comment"
	}
	if !s.hasHooks(ctx, repo.ID, event) {
		return
	}
	oi := s.loadOrgInfo(ctx)
	kind := "issues"
	if issue.IsPull {
		kind = "pulls"
	}
	is := map[string]any{"number": issue.Number, "title": issue.Title, "state": issue.State, "pull_request": issue.IsPull,
		"html_url": strings.TrimRight(oi.base, "/") + "/" + repo.Name + "/" + kind + "/" + itoa64(issue.Number)}
	key := "issue"
	if event == "pull_request_review_comment" {
		key = "pull_request"
	}
	s.emit(ctx, nil, repo, actor, event, action, map[string]any{key: is, "comment": c})
}

func itoa64(n int64) string { b, _ := json.Marshal(n); return string(b) }

// ---- releases and CI ----

func (s *Server) hooksRelease(ctx context.Context, repo *model.Repo, actor *model.User, rel apigen.Release, actions ...string) {
	if !s.hasHooks(ctx, repo.ID, "release") {
		return
	}
	for _, a := range actions {
		s.emit(ctx, nil, repo, actor, "release", a, map[string]any{"release": rel})
	}
}

// runPayload describes a workflow run for workflow_run events.
func runPayload(oi orgInfo, repo *model.Repo, run *model.WorkflowRun) map[string]any {
	html := strings.TrimRight(oi.base, "/") + "/" + repo.Name + "/ci/runs/" + itoa64(run.Number)
	p := map[string]any{
		"id": run.ID, "run_number": run.Number, "run_attempt": run.Attempt, "name": run.WorkflowName, "path": run.WorkflowPath,
		"display_title": run.Title, "event": run.Event, "status": run.Status, "conclusion": nil, "head_branch": run.HeadBranch,
		"head_sha": run.HeadSHA, "ref": run.Ref, "html_url": html, "created_at": run.CreatedAt, "run_started_at": run.StartedAt,
		"updated_at": run.UpdatedAt,
	}
	if run.Conclusion != "" {
		p["conclusion"] = run.Conclusion
	}
	if run.PullNumber != nil {
		p["pull_number"] = *run.PullNumber
	}
	return p
}

// hooksRun queues workflow_run events (db may be the transaction that
// changed the run).
func (s *Server) hooksRun(ctx context.Context, db bun.IDB, run *model.WorkflowRun, action string) {
	repo := new(model.Repo)
	if err := db.NewSelect().Model(repo).Where("id = ?", run.RepoID).Scan(ctx); err != nil {
		return
	}
	var actor *model.User
	if run.ActorID != nil {
		u := new(model.User)
		if err := db.NewSelect().Model(u).Where("id = ?", *run.ActorID).Scan(ctx); err == nil {
			actor = u
		}
	}
	oi := s.loadOrgInfoIn(ctx, db)
	s.emit(ctx, db, repo, actor, "workflow_run", action, map[string]any{"workflow_run": runPayload(oi, repo, run)})
}

// hooksReview sends pull_request_review.
func (s *Server) hooksReview(ctx context.Context, pc *pullCtx, action string, r apigen.Review) {
	if !s.hasHooks(ctx, pc.repo.ID, "pull_request_review") {
		return
	}
	pr, err := s.toAPIPull(ctx, pc, s.users())
	if err != nil {
		s.log.Warn("webhooks: pull payload", "err", err)
		return
	}
	s.emit(ctx, nil, pc.repo, mustPrincipal(ctx).User, "pull_request_review", action, map[string]any{"review": r, "pull_request": pr})
}
