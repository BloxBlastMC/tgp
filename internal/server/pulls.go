package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/git"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/githook"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// Pull request event types (issue_events.type).
const (
	evClosed           = "closed"
	evReopened         = "reopened"
	evMerged           = "merged"
	evPushed           = "pushed"
	evBaseChanged      = "base_changed"
	evTitleChanged     = "title_changed"
	evReady            = "ready_for_review"
	evDraft            = "converted_to_draft"
	evReviewRequested  = "review_requested"
	evRequestRemoved   = "review_request_removed"
	evReviewDismissed  = "review_dismissed"
	evHeadDeleted      = "head_deleted"
	evHeadRestored     = "head_restored"
	evBranchDeleted    = "branch_deleted"
	evBranchUpdated    = "branch_updated"
	mergeMethodManual  = "manual"
	maxPullCommitsList = 250
)

// ---- users ----

// userCache batches user lookups while building responses.
type userCache struct {
	s *Server
	m map[int64]*model.User
}

func (s *Server) users() *userCache { return &userCache{s: s, m: map[int64]*model.User{}} }

func (c *userCache) load(ctx context.Context, ids ...int64) error {
	var missing []int64
	for _, id := range ids {
		if _, ok := c.m[id]; !ok && id != 0 {
			missing = append(missing, id)
			c.m[id] = nil
		}
	}
	if len(missing) == 0 {
		return nil
	}
	var us []model.User
	if err := c.s.db.NewSelect().Model(&us).Where("id IN (?)", bun.In(missing)).Scan(ctx); err != nil {
		return err
	}
	for i := range us {
		c.m[us[i].ID] = &us[i]
	}
	return nil
}

func (c *userCache) summary(ctx context.Context, id int64) apigen.UserSummary {
	_ = c.load(ctx, id)
	u := c.m[id]
	if u == nil {
		return apigen.UserSummary{Id: id, Username: "ghost", DisplayName: "Deleted user", Role: apigen.RoleTester}
	}
	return apigen.UserSummary{Id: u.ID, Username: u.Username, DisplayName: u.DisplayName, Role: apigen.Role(u.Role)}
}

func (c *userCache) summaryPtr(ctx context.Context, id *int64) *apigen.UserSummary {
	if id == nil {
		return nil
	}
	s := c.summary(ctx, *id)
	return &s
}

// identity is how u appears in commits the server makes.
func (s *Server) identity(ctx context.Context, u *model.User) git.Identity {
	name := u.DisplayName
	if name == "" {
		name = u.Username
	}
	email := u.Email
	if email == "" {
		host := "tgp.local"
		if org, err := s.loadOrg(ctx); err == nil && org.BaseURL != "" {
			if pu, err := url.Parse(org.BaseURL); err == nil && pu.Hostname() != "" {
				host = pu.Hostname()
			}
		}
		email = u.Username + "@users.noreply." + host
	}
	return git.Identity{Name: name, Email: email}
}

// ---- loading ----

// pullCtx is a loaded pull request with its repository.
type pullCtx struct {
	repo  *model.Repo
	perm  authz.Perm
	issue *model.Issue
	pr    *model.PullRequest
	g     *git.Repo
}

func (pc *pullCtx) merged() bool { return pc.pr.MergedAt != nil }
func (pc *pullCtx) open() bool   { return pc.issue.State == model.StateOpen }

func (pc *pullCtx) state() apigen.PullState {
	switch {
	case pc.merged():
		return apigen.PullStateMerged
	case pc.open():
		return apigen.PullStateOpen
	}
	return apigen.PullStateClosed
}

func (s *Server) loadPull(ctx context.Context, repoName string, number int64, need authz.Perm) (*pullCtx, error) {
	repo, perm, err := s.repoFor(ctx, repoName, need)
	if err != nil {
		return nil, err
	}
	return s.loadPullIn(ctx, s.db, repo, perm, number)
}

func (s *Server) loadPullIn(ctx context.Context, db bun.IDB, repo *model.Repo, perm authz.Perm, number int64) (*pullCtx, error) {
	issue := new(model.Issue)
	err := db.NewSelect().Model(issue).Where("repo_id = ? AND number = ?", repo.ID, number).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.NotFound("pull request #%d not found in %s", number, repo.Name)
	}
	if err != nil {
		return nil, err
	}
	if !issue.IsPull {
		return nil, apierr.NotFound("#%d is an issue, not a pull request", number).WithDetails(map[string]any{"is_issue": true})
	}
	pr := new(model.PullRequest)
	if err := db.NewSelect().Model(pr).Where("issue_id = ?", issue.ID).Scan(ctx); err != nil {
		return nil, err
	}
	return &pullCtx{repo: repo, perm: perm, issue: issue, pr: pr, g: s.repos.Open(repo)}, nil
}

func addEvent(ctx context.Context, db bun.IDB, issueID int64, actor *model.User, typ string, data map[string]any) error {
	ev := &model.IssueEvent{IssueID: issueID, Type: typ, Data: "{}", CreatedAt: time.Now().UTC()}
	if actor != nil {
		ev.ActorID = &actor.ID
	}
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		ev.Data = string(b)
	}
	_, err := db.NewInsert().Model(ev).Exec(ctx)
	return err
}

func touchIssue(ctx context.Context, db bun.IDB, issueID int64) error {
	_, err := db.NewUpdate().Model((*model.Issue)(nil)).Set("updated_at = ?", time.Now().UTC()).Where("id = ?", issueID).Exec(ctx)
	return err
}

// nextNumber allocates the next issue/pull request number of a repository.
func nextNumber(ctx context.Context, db bun.IDB, repoID int64) (int64, error) {
	var n int64
	err := db.NewUpdate().Model((*model.Repo)(nil)).Set("next_number = next_number + 1").
		Where("id = ?", repoID).Returning("next_number").Scan(ctx, &n)
	return n - 1, err
}

func branchRef(b string) string { return "refs/heads/" + b }
func pullRef(n int64) string    { return "refs/pull/" + strconv.FormatInt(n, 10) + "/head" }

// ---- mergeability ----

// mergeInfo is the live git state of an open pull request.
type mergeInfo struct {
	baseSHA   string // "" if the base branch is gone
	mergeBase string
	behind    int
	mergeable string // clean, conflict, unknown
	conflicts []string
}

// mergeState computes (and caches by base/head commit) whether the pull
// request merges cleanly.
func (s *Server) mergeState(ctx context.Context, pc *pullCtx) (*mergeInfo, error) {
	mi := &mergeInfo{mergeable: "unknown", conflicts: []string{}}
	if pc.merged() {
		mi.mergeBase = pc.pr.MergeBaseSHA
		return mi, nil
	}
	base, err := pc.g.RefCommit(ctx, branchRef(pc.pr.BaseBranch))
	if err != nil {
		return nil, err
	}
	mi.baseSHA = base
	if base == "" {
		return mi, nil
	}
	if mi.mergeBase, err = pc.g.MergeBase(ctx, base, pc.pr.HeadSHA); err != nil {
		return nil, err
	}
	if _, mi.behind, err = pc.g.AheadBehind(ctx, base, pc.pr.HeadSHA); err != nil {
		return nil, err
	}
	if !pc.open() || pc.pr.HeadDeleted {
		return mi, nil
	}
	pr := pc.pr
	if pr.MergeableBaseSHA == base && pr.MergeableHeadSHA == pr.HeadSHA && pr.Mergeable != "unknown" {
		mi.mergeable = pr.Mergeable
		_ = json.Unmarshal([]byte(pr.ConflictFiles), &mi.conflicts)
		if mi.conflicts == nil {
			mi.conflicts = []string{}
		}
		return mi, nil
	}
	_, merr := pc.g.MergeTree(ctx, base, pr.HeadSHA, "")
	var ce *git.ConflictError
	switch {
	case merr == nil:
		mi.mergeable = "clean"
	case errors.As(merr, &ce):
		mi.mergeable, mi.conflicts = "conflict", ce.Files
	default:
		return nil, merr
	}
	cf, _ := json.Marshal(mi.conflicts)
	pr.Mergeable, pr.MergeableBaseSHA, pr.MergeableHeadSHA, pr.ConflictFiles = mi.mergeable, base, pr.HeadSHA, string(cf)
	_, err = s.db.NewUpdate().Model(pr).Column("mergeable", "mergeable_base_sha", "mergeable_head_sha", "conflict_files").WherePK().Exec(ctx)
	return mi, err
}

// ---- reviews ----

type reviewSummary struct {
	decision         apigen.ReviewDecision
	approvals        int
	changesRequested []int64
	reviewers        []apigen.ReviewerState
}

// summarizeReviews applies the review rules: each reviewer's latest verdict
// counts; dismissed verdicts don't; with dismiss_stale_approvals, approvals
// of an older head don't count; the author's reviews never count.
func summarizeReviews(ctx context.Context, uc *userCache, reviews []model.Review, requested []int64, authorID int64, head string, rule *model.BranchProtection) reviewSummary {
	verdict := map[int64]*model.Review{}
	last := map[int64]*model.Review{}
	var order []int64
	for i := range reviews {
		r := &reviews[i]
		if _, seen := last[r.AuthorID]; !seen {
			order = append(order, r.AuthorID)
		}
		last[r.AuthorID] = r
		if r.State != model.ReviewCommented {
			verdict[r.AuthorID] = r
		}
	}
	sum := reviewSummary{reviewers: []apigen.ReviewerState{}, changesRequested: []int64{}}
	req := map[int64]bool{}
	for _, id := range requested {
		req[id] = true
	}
	for _, uid := range order {
		r := last[uid]
		state := apigen.ReviewerStateStateCommented
		if v := verdict[uid]; v != nil {
			r = v
			switch {
			case v.Dismissed:
				state = apigen.ReviewerStateStateDismissed
			case v.State == model.ReviewApproved:
				state = apigen.ReviewerStateStateApproved
			default:
				state = apigen.ReviewerStateStateChangesRequested
			}
		}
		stale := r.CommitSHA != head
		if uid != authorID {
			if state == apigen.ReviewerStateStateApproved && !(stale && rule != nil && rule.DismissStaleApprovals) {
				sum.approvals++
			}
			if state == apigen.ReviewerStateStateChangesRequested {
				sum.changesRequested = append(sum.changesRequested, uid)
			}
		}
		if req[uid] {
			state = apigen.ReviewerStateStateRequested // re-requested after reviewing
		}
		sha := r.CommitSHA
		sum.reviewers = append(sum.reviewers, apigen.ReviewerState{User: uc.summary(ctx, uid), State: state, CommitSha: &sha, Stale: stale})
	}
	for _, id := range requested {
		if _, reviewed := last[id]; !reviewed {
			sum.reviewers = append(sum.reviewers, apigen.ReviewerState{User: uc.summary(ctx, id), State: apigen.ReviewerStateStateRequested})
		}
	}
	required := 0
	if rule != nil {
		required = rule.RequiredApprovals
	}
	switch {
	case len(sum.changesRequested) > 0:
		sum.decision = apigen.ReviewDecisionChangesRequested
	case required > 0 && sum.approvals >= required:
		sum.decision = apigen.ReviewDecisionApproved
	case required > 0:
		sum.decision = apigen.ReviewDecisionReviewRequired
	case sum.approvals > 0:
		sum.decision = apigen.ReviewDecisionApproved
	default:
		sum.decision = apigen.ReviewDecisionNone
	}
	return sum
}

func (s *Server) pullReviews(ctx context.Context, db bun.IDB, issueIDs ...int64) (map[int64][]model.Review, error) {
	out := map[int64][]model.Review{}
	if len(issueIDs) == 0 {
		return out, nil
	}
	var rs []model.Review
	if err := db.NewSelect().Model(&rs).Where("issue_id IN (?)", bun.In(issueIDs)).Order("created_at", "id").Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rs {
		out[r.IssueID] = append(out[r.IssueID], r)
	}
	return out, nil
}

func (s *Server) reviewRequests(ctx context.Context, db bun.IDB, issueIDs ...int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(issueIDs) == 0 {
		return out, nil
	}
	var rs []model.ReviewRequest
	if err := db.NewSelect().Model(&rs).Where("issue_id IN (?)", bun.In(issueIDs)).Order("id").Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rs {
		out[r.IssueID] = append(out[r.IssueID], r.UserID)
	}
	return out, nil
}

// ---- blockers ----

func blocker(code apigen.MergeBlockerCode, bypassable bool, format string, args ...any) apigen.MergeBlocker {
	return apigen.MergeBlocker{Code: code, Bypassable: bypassable, Message: fmt.Sprintf(format, args...)}
}

// mergeBlockers lists why the pull request can't be merged by someone with
// perm. Rule-based blockers are bypassable when the rule lets admins bypass.
func mergeBlockers(pc *pullCtx, mi *mergeInfo, rs reviewSummary, rule *model.BranchProtection, perm authz.Perm, checks apigen.CheckSummary) []apigen.MergeBlocker {
	out := []apigen.MergeBlocker{}
	switch {
	case pc.merged():
		return append(out, blocker(apigen.MergeBlockerCodeMerged, false, "already merged"))
	case !pc.open():
		return append(out, blocker(apigen.MergeBlockerCodeClosed, false, "the pull request is closed"))
	}
	if perm < authz.Write {
		out = append(out, blocker(apigen.MergeBlockerCodePermission, false, "merging needs write access; you have %s", perm))
	}
	if pc.pr.Draft {
		out = append(out, blocker(apigen.MergeBlockerCodeDraft, false, "the pull request is a draft; mark it ready for review first"))
	}
	if pc.pr.HeadDeleted {
		out = append(out, blocker(apigen.MergeBlockerCodeHeadMissing, false, "the head branch %s was deleted", pc.pr.HeadBranch))
	}
	if mi.baseSHA == "" {
		out = append(out, blocker(apigen.MergeBlockerCodeBaseMissing, false, "the base branch %s no longer exists", pc.pr.BaseBranch))
	}
	if mi.mergeable == "conflict" {
		out = append(out, blocker(apigen.MergeBlockerCodeConflict, false, "conflicts with %s in: %s", pc.pr.BaseBranch, strings.Join(mi.conflicts, ", ")))
	}
	if rule != nil {
		bp := rule.AdminsBypass && perm >= authz.Admin
		if rule.RequiredApprovals > 0 && rs.approvals < rule.RequiredApprovals {
			out = append(out, blocker(apigen.MergeBlockerCodeApprovals, bp, "%d of %d required approvals", rs.approvals, rule.RequiredApprovals))
		}
		if len(rs.changesRequested) > 0 {
			out = append(out, blocker(apigen.MergeBlockerCodeChangesRequested, bp, "changes were requested; they must be addressed and re-reviewed, or the review dismissed"))
		}
		if rule.RequireUpToDate && mi.behind > 0 {
			out = append(out, blocker(apigen.MergeBlockerCodeBehind, bp, "the head branch is %d commit(s) behind %s; update it first", mi.behind, pc.pr.BaseBranch))
		}
		if failing, pending := requiredChecksFailing(checks); len(failing) > 0 || len(pending) > 0 {
			var parts []string
			if len(failing) > 0 {
				parts = append(parts, "failing: "+strings.Join(failing, ", "))
			}
			if len(pending) > 0 {
				parts = append(parts, "waiting for: "+strings.Join(pending, ", "))
			}
			out = append(out, blocker(apigen.MergeBlockerCodeChecks, bp, "required checks haven't passed (%s)", strings.Join(parts, "; ")))
		}
	}
	return out
}

// ---- responses ----

func (s *Server) toAPIPull(ctx context.Context, pc *pullCtx, uc *userCache) (apigen.PullRequest, error) {
	mi, err := s.mergeState(ctx, pc)
	if err != nil {
		return apigen.PullRequest{}, err
	}
	rule, err := s.ruleFor(ctx, s.db, pc.repo.ID, pc.pr.BaseBranch)
	if err != nil {
		return apigen.PullRequest{}, err
	}
	reviews, err := s.pullReviews(ctx, s.db, pc.issue.ID)
	if err != nil {
		return apigen.PullRequest{}, err
	}
	requested, err := s.reviewRequests(ctx, s.db, pc.issue.ID)
	if err != nil {
		return apigen.PullRequest{}, err
	}
	reqIDs := requested[pc.issue.ID]
	ids := append([]int64{pc.issue.AuthorID}, reqIDs...)
	for _, r := range reviews[pc.issue.ID] {
		ids = append(ids, r.AuthorID)
	}
	if pc.pr.MergedByID != nil {
		ids = append(ids, *pc.pr.MergedByID)
	}
	if err := uc.load(ctx, ids...); err != nil {
		return apigen.PullRequest{}, err
	}
	rs := summarizeReviews(ctx, uc, reviews[pc.issue.ID], reqIDs, pc.issue.AuthorID, pc.pr.HeadSHA, rule)
	checks, err := s.commitChecks(ctx, pc.repo, pc.pr.HeadSHA, ruleChecks(rule))
	if err != nil {
		return apigen.PullRequest{}, err
	}
	blockers := mergeBlockers(pc, mi, rs, rule, pc.perm, checks)

	out := apigen.PullRequest{
		Number: pc.issue.Number, Repo: pc.repo.Name, Title: pc.issue.Title, Body: pc.issue.Body,
		State: pc.state(), Draft: pc.pr.Draft, Author: uc.summary(ctx, pc.issue.AuthorID),
		Head:      apigen.PullBranch{Branch: pc.pr.HeadBranch, Sha: pc.pr.HeadSHA, Exists: !pc.pr.HeadDeleted},
		Base:      apigen.PullBranch{Branch: pc.pr.BaseBranch, Sha: mi.baseSHA, Exists: mi.baseSHA != ""},
		MergeBase: mi.mergeBase, Comments: pc.issue.CommentCount,
		Mergeable: apigen.PullRequestMergeable(mi.mergeable), ConflictFiles: mi.conflicts,
		ReviewDecision: rs.decision, Reviewers: rs.reviewers, RequestedReviewers: []apigen.UserSummary{},
		Blockers: blockers, CanMerge: len(blockers) == 0, Checks: checks, ViewerPermission: apigen.Permission(pc.perm.String()),
		CreatedAt: pc.issue.CreatedAt, UpdatedAt: pc.issue.UpdatedAt, ClosedAt: pc.issue.ClosedAt, MergedAt: pc.pr.MergedAt,
		MergedBy: uc.summaryPtr(ctx, pc.pr.MergedByID),
	}
	if !pc.merged() && mi.baseSHA != "" {
		out.BehindBy = &mi.behind
	}
	if pc.merged() {
		out.Base.Sha, out.Base.Exists = pc.pr.MergeCommitSHA, true
		if b, _ := pc.g.RefCommit(ctx, branchRef(pc.pr.BaseBranch)); b == "" {
			out.Base.Exists = false
		}
		out.MergeCommit, out.MergeMethod = &pc.pr.MergeCommitSHA, &pc.pr.MergeMethod
	}
	for _, id := range reqIDs {
		out.RequestedReviewers = append(out.RequestedReviewers, uc.summary(ctx, id))
	}
	meta, err := s.loadIssueMeta(ctx, []*model.Issue{pc.issue})
	if err != nil {
		return out, err
	}
	out.Labels, out.Assignees, out.Milestone = meta.labelsOf(pc.issue.ID), meta.assigneesOf(ctx, uc, pc.issue.ID), meta.milestoneOf(pc.issue)
	if rule != nil {
		p := toAPIProtection(rule)
		out.Protection = &p
	}
	if mi.mergeBase != "" {
		out.ChangedFiles, out.Additions, out.Deletions, err = pc.g.DiffStat(ctx, mi.mergeBase, pc.pr.HeadSHA)
		if err != nil {
			return out, err
		}
		if out.Commits, err = pc.g.CountCommits(ctx, mi.mergeBase, pc.pr.HeadSHA); err != nil {
			return out, err
		}
	}
	rc := markdownCtx(pc.repo, pc.pr.HeadSHA)
	if out.BodyHtml, err = renderMarkdown(pc.issue.Body, rc); err != nil {
		return out, err
	}
	return out, nil
}

// diffRange returns the commits a pull request's diff spans.
func (s *Server) diffRange(ctx context.Context, pc *pullCtx) (from, to string, err error) {
	if pc.merged() {
		return pc.pr.MergeBaseSHA, pc.pr.HeadSHA, nil
	}
	mi, err := s.mergeState(ctx, pc)
	if err != nil {
		return "", "", err
	}
	if mi.mergeBase == "" {
		if mi.baseSHA == "" {
			return "", "", apierr.Conflict("the base branch %s no longer exists", pc.pr.BaseBranch)
		}
		return "", "", apierr.Conflict("%s and %s have no common history", pc.pr.BaseBranch, pc.pr.HeadBranch)
	}
	return mi.mergeBase, pc.pr.HeadSHA, nil
}

// ---- reacting to ref updates ----

// syncPulls updates pull requests after branches change (pushes, merges,
// branch deletions): heads follow their branch, and an open pull request
// whose head was pushed into its base is marked merged.
func (s *Server) syncPulls(ctx context.Context, repo *model.Repo, actor *model.User, updates []githook.Update) error {
	g := s.repos.Open(repo)
	for _, u := range updates {
		if !strings.HasPrefix(u.Ref, "refs/heads/") {
			continue
		}
		branch := strings.TrimPrefix(u.Ref, "refs/heads/")
		if err := s.syncHeads(ctx, g, repo, actor, branch, u); err != nil {
			return err
		}
		if !u.IsDelete() {
			if err := s.syncBases(ctx, g, repo, actor, branch, u); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) syncHeads(ctx context.Context, g *git.Repo, repo *model.Repo, actor *model.User, branch string, u githook.Update) error {
	if u.IsDelete() {
		var rows []struct {
			model.PullRequest
			State string `bun:"state"`
		}
		err := s.db.NewSelect().TableExpr("pull_requests AS pr").ColumnExpr("pr.*, i.state").Join("JOIN issues AS i ON i.id = pr.issue_id").
			Where("pr.repo_id = ? AND pr.head_branch = ? AND NOT pr.head_deleted", repo.ID, branch).Scan(ctx, &rows)
		if err != nil {
			return err
		}
		for i := range rows {
			pr, open := &rows[i].PullRequest, rows[i].State == model.StateOpen
			err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
				if _, err := tx.NewUpdate().Model(pr).Set("head_deleted = TRUE").WherePK().Exec(ctx); err != nil {
					return err
				}
				if !open {
					return nil // closed and merged pull requests just note it
				}
				return addEvent(ctx, tx, pr.IssueID, actor, evHeadDeleted, map[string]any{"branch": branch})
			})
			if err != nil {
				return err
			}
		}
		return nil
	}
	var rows []struct {
		model.PullRequest
		Number int64 `bun:"number"`
	}
	err := s.db.NewSelect().TableExpr("pull_requests AS pr").ColumnExpr("pr.*, i.number").
		Join("JOIN issues AS i ON i.id = pr.issue_id").
		Where("pr.repo_id = ? AND pr.head_branch = ? AND i.state = ?", repo.ID, branch, model.StateOpen).Scan(ctx, &rows)
	if err != nil {
		return err
	}
	for i := range rows {
		pr := &rows[i].PullRequest
		if pr.HeadSHA == u.New && !pr.HeadDeleted {
			continue
		}
		if err := s.setHead(ctx, g, pr, rows[i].Number, actor, u.New, evPushed); err != nil {
			return err
		}
		var title string
		if err := s.db.NewSelect().Model((*model.Issue)(nil)).Column("title").Where("id = ?", pr.IssueID).Scan(ctx, &title); err != nil {
			return err
		}
		s.ciOnPull(context.WithoutCancel(ctx), repo, actor, pr, rows[i].Number, title, "synchronize")
	}
	return nil
}

// setHead moves a pull request's head to sha and records why.
func (s *Server) setHead(ctx context.Context, g *git.Repo, pr *model.PullRequest, number int64, actor *model.User, sha, why string) error {
	before, wasDeleted := pr.HeadSHA, pr.HeadDeleted
	data := map[string]any{"before": before, "after": sha}
	if why == evPushed {
		forced := false
		if ok, err := g.IsAncestor(ctx, before, sha); err == nil && !ok {
			forced = true
		}
		data["forced"] = forced
		if !forced {
			n, _ := g.CountCommits(ctx, before, sha)
			data["commits"] = n
		}
	} else {
		data = map[string]any{"commit": sha}
	}
	if err := g.UpdateRef(ctx, pullRef(number), sha, ""); err != nil {
		return err
	}
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		pr.HeadSHA, pr.HeadDeleted = sha, false
		if _, err := tx.NewUpdate().Model(pr).Column("head_sha", "head_deleted").WherePK().Exec(ctx); err != nil {
			return err
		}
		if wasDeleted {
			if err := addEvent(ctx, tx, pr.IssueID, actor, evHeadRestored, map[string]any{"branch": pr.HeadBranch}); err != nil {
				return err
			}
		}
		if err := addEvent(ctx, tx, pr.IssueID, actor, why, data); err != nil {
			return err
		}
		return touchIssue(ctx, tx, pr.IssueID)
	})
}

func (s *Server) syncBases(ctx context.Context, g *git.Repo, repo *model.Repo, actor *model.User, branch string, u githook.Update) error {
	var rows []struct {
		model.PullRequest
		Number int64 `bun:"number"`
	}
	err := s.db.NewSelect().TableExpr("pull_requests AS pr").ColumnExpr("pr.*, i.number").
		Join("JOIN issues AS i ON i.id = pr.issue_id").
		Where("pr.repo_id = ? AND pr.base_branch = ? AND i.state = ? AND NOT pr.head_deleted", repo.ID, branch, model.StateOpen).Scan(ctx, &rows)
	if err != nil {
		return err
	}
	for i := range rows {
		pr := &rows[i].PullRequest
		if ok, err := g.IsAncestor(ctx, pr.HeadSHA, u.New); err != nil || !ok {
			continue
		}
		mb := pr.HeadSHA
		if !u.IsCreate() {
			if m, err := g.MergeBase(ctx, u.Old, pr.HeadSHA); err == nil && m != "" {
				mb = m
			}
		}
		if err := s.markMerged(ctx, s.db, pr, actor, u.New, mb, mergeMethodManual); err != nil {
			return err
		}
	}
	return nil
}

// markMerged records a merge and closes the pull request.
func (s *Server) markMerged(ctx context.Context, db bun.IDB, pr *model.PullRequest, actor *model.User, commit, mergeBase, method string) error {
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		now := time.Now().UTC()
		pr.MergedAt, pr.MergeCommitSHA, pr.MergeMethod, pr.MergeBaseSHA = &now, commit, method, mergeBase
		if actor != nil {
			pr.MergedByID = &actor.ID
		}
		if _, err := tx.NewUpdate().Model(pr).Column("merged_at", "merged_by_id", "merge_commit_sha", "merge_method", "merge_base_sha").WherePK().Exec(ctx); err != nil {
			return err
		}
		q := tx.NewUpdate().Model((*model.Issue)(nil)).Set("state = ?", model.StateClosed).Set("closed_at = ?", now).Set("updated_at = ?", now).Where("id = ?", pr.IssueID)
		if actor != nil {
			q = q.Set("closed_by_id = ?", actor.ID)
		}
		if _, err := q.Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*model.ReviewRequest)(nil)).Where("issue_id = ?", pr.IssueID).Exec(ctx); err != nil {
			return err
		}
		return addEvent(ctx, tx, pr.IssueID, actor, evMerged, map[string]any{"commit": commit, "method": method})
	})
}

// ---- merging ----

type mergeOptions struct {
	method       git.MergeMethod
	title        string
	message      string
	sha          string
	deleteBranch bool
	bypass       bool
}

func (s *Server) mergePull(ctx context.Context, pc *pullCtx, user *model.User, o mergeOptions) error {
	if o.sha != "" && o.sha != pc.pr.HeadSHA {
		return apierr.Conflict("the head moved to %s since you looked; review the new commits and try again", short(pc.pr.HeadSHA))
	}
	if pc.open() && !pc.pr.HeadDeleted {
		tip, err := pc.g.RefCommit(ctx, branchRef(pc.pr.HeadBranch))
		if err != nil {
			return err
		}
		if tip != pc.pr.HeadSHA {
			return apierr.Conflict("the head branch changed while loading; try again")
		}
	}
	mi, err := s.mergeState(ctx, pc)
	if err != nil {
		return err
	}
	rule, err := s.ruleFor(ctx, s.db, pc.repo.ID, pc.pr.BaseBranch)
	if err != nil {
		return err
	}
	reviews, err := s.pullReviews(ctx, s.db, pc.issue.ID)
	if err != nil {
		return err
	}
	rs := summarizeReviews(ctx, s.users(), reviews[pc.issue.ID], nil, pc.issue.AuthorID, pc.pr.HeadSHA, rule)
	checks, err := s.commitChecks(ctx, pc.repo, pc.pr.HeadSHA, ruleChecks(rule))
	if err != nil {
		return err
	}
	all := mergeBlockers(pc, mi, rs, rule, pc.perm, checks)
	var remaining, bypassed []apigen.MergeBlocker
	for _, b := range all {
		if o.bypass && b.Bypassable {
			bypassed = append(bypassed, b)
		} else {
			remaining = append(remaining, b)
		}
	}
	if o.bypass && pc.perm < authz.Admin {
		return apierr.Forbidden("only admins can bypass branch protection")
	}
	if len(remaining) > 0 {
		msgs := make([]string, len(remaining))
		for i, b := range remaining {
			msgs[i] = b.Message
		}
		return apierr.Conflict("can't merge: %s", strings.Join(msgs, "; ")).WithDetails(map[string]any{"blockers": remaining})
	}

	author, err := s.loadActiveUserAny(ctx, pc.issue.AuthorID)
	if err != nil {
		return err
	}
	if o.method == "" {
		o.method = git.MethodMerge
	}
	msg := o.title
	switch o.method {
	case git.MethodMerge:
		if msg == "" {
			msg = fmt.Sprintf("Merge pull request #%d from %s\n\n%s", pc.issue.Number, pc.pr.HeadBranch, pc.issue.Title)
		}
	case git.MethodSquash:
		if msg == "" {
			msg = fmt.Sprintf("%s (#%d)", pc.issue.Title, pc.issue.Number)
		}
		if o.message == "" {
			commits, err := pc.g.LogRange(ctx, mi.baseSHA, pc.pr.HeadSHA, maxPullCommitsList)
			if err != nil {
				return err
			}
			var b strings.Builder
			for _, c := range commits {
				b.WriteString("* " + c.Subject + "\n")
			}
			o.message = b.String()
		}
	case git.MethodRebase:
	default:
		return apierr.Validation("unknown merge method %q: use merge, squash or rebase", o.method)
	}
	if o.message != "" && o.method != git.MethodRebase {
		msg = strings.TrimRight(msg, "\n") + "\n\n" + strings.TrimSpace(o.message)
	}
	newTip, err := pc.g.Merge(ctx, git.MergeRequest{
		Method: o.method, Base: mi.baseSHA, Head: pc.pr.HeadSHA, Message: msg + "\n",
		Author: s.identity(ctx, author), Committer: s.identity(ctx, user),
	})
	var ce *git.ConflictError
	switch {
	case errors.As(err, &ce):
		return apierr.Conflict("can't merge automatically: %v", ce).WithDetails(map[string]any{"conflict_files": ce.Files})
	case errors.Is(err, git.ErrMergeCommits):
		return apierr.Conflict("%v; use the merge or squash method", err)
	case err != nil:
		return err
	}
	if err := pc.g.UpdateRef(ctx, branchRef(pc.pr.BaseBranch), newTip, mi.baseSHA); err != nil {
		if errors.Is(err, git.ErrRefChanged) {
			return apierr.Conflict("%s changed while merging; try again", pc.pr.BaseBranch)
		}
		return err
	}
	if err := s.markMerged(ctx, s.db, pc.pr, user, newTip, mi.mergeBase, string(o.method)); err != nil {
		return err
	}
	if pc.pr.BaseBranch == pc.repo.DefaultBranch {
		text := pc.issue.Title + "\n" + pc.issue.Body
		if commits, err := pc.g.LogRange(ctx, mi.baseSHA, pc.pr.HeadSHA, maxPullCommitsList); err == nil {
			for _, c := range commits {
				text += "\n" + c.Subject + "\n" + c.Body
			}
		}
		if err := s.closeReferencedIssues(ctx, pc.repo, user, text, map[string]any{"pull": pc.issue.Number, "commit": newTip}); err != nil {
			return err
		}
	}
	if len(bypassed) > 0 {
		codes := make([]string, len(bypassed))
		for i, b := range bypassed {
			codes[i] = string(b.Code)
		}
		_, hr := httpFrom(ctx)
		if err := s.auditRecord(ctx, user, "pull.merge_bypass", "repo", fmt.Sprintf("%s#%d", pc.repo.Name, pc.issue.Number), clientIP(hr),
			map[string]any{"bypassed": codes}); err != nil {
			return err
		}
	}
	// Other pull requests into this base may now be merged too (e.g. stacked
	// branches) and the repository was pushed to.
	if err := s.afterServerPush(ctx, pc.repo, user, []githook.Update{{Ref: branchRef(pc.pr.BaseBranch), Old: mi.baseSHA, New: newTip}}); err != nil {
		return err
	}
	if o.deleteBranch {
		if err := s.deleteBranch(ctx, pc.repo, pc.perm, user, pc.pr.HeadBranch, pc.pr.HeadSHA); err == nil {
			_ = addEvent(ctx, s.db, pc.issue.ID, user, evBranchDeleted, map[string]any{"branch": pc.pr.HeadBranch})
		}
	}
	return nil
}

// afterServerPush runs the post-push bookkeeping for ref updates the server
// made itself (merges, branch updates, deletions), which skip git hooks.
func (s *Server) afterServerPush(ctx context.Context, repo *model.Repo, user *model.User, updates []githook.Update) error {
	now := time.Now().UTC()
	repo.PushedAt = &now
	if _, err := s.db.NewUpdate().Model(repo).Column("pushed_at").WherePK().Exec(ctx); err != nil {
		return err
	}
	if err := s.syncPulls(ctx, repo, user, updates); err != nil {
		return err
	}
	s.ciOnPush(context.WithoutCancel(ctx), repo, user, updates)
	return nil
}

// deleteBranch deletes a branch if the rules allow it.
func (s *Server) deleteBranch(ctx context.Context, repo *model.Repo, perm authz.Perm, user *model.User, branch, sha string) error {
	if perm < authz.Write {
		return apierr.Forbidden("deleting branches needs write access")
	}
	if repo.Archived {
		return apierr.Forbidden("%s is archived", repo.Name)
	}
	if branch == repo.DefaultBranch {
		return apierr.Conflict("%s is the default branch and can't be deleted", branch)
	}
	g := s.repos.Open(repo)
	tip, err := g.RefCommit(ctx, branchRef(branch))
	if err != nil {
		return err
	}
	if tip == "" {
		return apierr.NotFound("branch %q not found", branch)
	}
	if sha != "" && tip != sha {
		return apierr.Conflict("branch %s moved to %s; not deleting", branch, short(tip))
	}
	rule, err := s.ruleFor(ctx, s.db, repo.ID, branch)
	if err != nil {
		return err
	}
	if msg := checkProtectedPush(rule, branch, user.IsAdmin(), false, true, false); msg != "" {
		return apierr.Forbidden("%s", msg)
	}
	var open []int64
	err = s.db.NewSelect().TableExpr("pull_requests AS pr").ColumnExpr("i.number").Join("JOIN issues AS i ON i.id = pr.issue_id").
		Where("pr.repo_id = ? AND i.state = ? AND (pr.head_branch = ? OR pr.base_branch = ?)", repo.ID, model.StateOpen, branch, branch).
		Scan(ctx, &open)
	if err != nil {
		return err
	}
	if len(open) > 0 {
		nums := make([]string, len(open))
		for i, n := range open {
			nums[i] = "#" + strconv.FormatInt(n, 10)
		}
		return apierr.Conflict("branch %s is used by open pull request(s) %s", branch, strings.Join(nums, ", "))
	}
	if err := g.DeleteRef(ctx, branchRef(branch), tip); err != nil {
		if errors.Is(err, git.ErrRefChanged) {
			return apierr.Conflict("branch %s changed; not deleting", branch)
		}
		return err
	}
	return s.syncPulls(ctx, repo, user, []githook.Update{{Ref: branchRef(branch), Old: tip, New: git.ZeroID}})
}

func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// sortTimeline orders items by time, keeping insertion order for ties.
func sortTimeline(items []apigen.TimelineItem) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
}

// pushHints tells the pusher where to open or find a pull request for each
// pushed branch (shown in their terminal by git).
func (s *Server) pushHints(ctx context.Context, repo *model.Repo, updates []githook.Update) []string {
	var msgs []string
	// Hooks have no HTTP request to derive the public URL from, so hints
	// need a configured base URL.
	root := s.cfg.Server.BaseURL
	if root == "" {
		if org, err := s.loadOrg(ctx); err == nil {
			root = org.BaseURL
		}
	}
	if root == "" {
		return nil
	}
	base := strings.TrimRight(root, "/") + "/" + repo.Name
	for _, u := range updates {
		if !strings.HasPrefix(u.Ref, "refs/heads/") || u.IsDelete() {
			continue
		}
		branch := strings.TrimPrefix(u.Ref, "refs/heads/")
		if branch == repo.DefaultBranch {
			continue
		}
		var n int64
		err := s.db.NewSelect().TableExpr("pull_requests AS pr").ColumnExpr("i.number").Join("JOIN issues AS i ON i.id = pr.issue_id").
			Where("pr.repo_id = ? AND pr.head_branch = ? AND i.state = ?", repo.ID, branch, model.StateOpen).Limit(1).Scan(ctx, &n)
		if err == nil && n > 0 {
			msgs = append(msgs, fmt.Sprintf("tgp: updated pull request #%d: %s/pulls/%d", n, base, n))
		} else if u.IsCreate() {
			msgs = append(msgs, fmt.Sprintf("tgp: open a pull request for %s: %s/compare/%s...%s", branch, base, repo.DefaultBranch, url.PathEscape(branch)))
		}
	}
	return msgs
}
