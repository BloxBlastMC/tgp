package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/git"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/githook"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/markdown"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

const (
	maxTitle   = 256
	maxBody    = 256 << 10
	maxComment = 64 << 10
)

func markdownCtx(repo *model.Repo, ref string) markdown.RepoContext {
	return markdown.RepoContext{Repo: repo.Name, Ref: ref}
}

func renderMarkdown(src string, rc markdown.RepoContext) (string, error) {
	if src == "" {
		return "", nil
	}
	return markdown.Render([]byte(src), rc)
}

// loadActiveUserAny loads a user even if disabled (e.g. a PR author).
func (s *Server) loadActiveUserAny(ctx context.Context, id int64) (*model.User, error) {
	u := new(model.User)
	if err := s.db.NewSelect().Model(u).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Server) auditRecord(ctx context.Context, actor *model.User, action, targetType, target, ip string, details map[string]any) error {
	return audit.Record(ctx, s.db, audit.Entry{Actor: actor, Action: action, TargetType: targetType, Target: target, IP: ip, Details: details})
}

func validateTitle(t string) (string, error) {
	t = strings.TrimSpace(t)
	if t == "" {
		return "", apierr.Validation("title is required")
	}
	if len(t) > maxTitle {
		return "", apierr.Validation("title is too long (max %d characters)", maxTitle)
	}
	return t, nil
}

func validateBody(b string, max int) error {
	if len(b) > max {
		return apierr.Validation("text is too long (max %d KiB)", max>>10)
	}
	return nil
}

// ---- compare ----

func (a *api) CompareRefs(ctx context.Context, req apigen.CompareRefsRequestObject) (apigen.CompareRefsResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	g := a.s.repos.Open(repo)
	base, err := g.ResolveCommit(ctx, req.Params.Base)
	if err != nil {
		return nil, apierr.NotFound("base %q not found", req.Params.Base)
	}
	head, err := g.ResolveCommit(ctx, req.Params.Head)
	if err != nil {
		return nil, apierr.NotFound("head %q not found", req.Params.Head)
	}
	mb, err := g.MergeBase(ctx, base, head)
	if err != nil {
		return nil, err
	}
	if mb == "" {
		return nil, apierr.Conflict("%s and %s have no common history", req.Params.Base, req.Params.Head)
	}
	ahead, behind, err := g.AheadBehind(ctx, base, head)
	if err != nil {
		return nil, err
	}
	commits, err := g.LogRange(ctx, base, head, maxPullCommitsList)
	if err != nil {
		return nil, err
	}
	d, err := g.DiffMergeBase(ctx, base, head, git.DefaultDiffLimits)
	if err != nil {
		return nil, err
	}
	out := apigen.Comparison{Base: req.Params.Base, Head: req.Params.Head, BaseSha: base, HeadSha: head, MergeBase: mb,
		AheadBy: ahead, BehindBy: behind, Commits: make([]apigen.Commit, 0, len(commits)), Diff: toAPIDiff(d),
		Mergeable: apigen.ComparisonMergeableClean, ConflictFiles: []string{}}
	for i := range commits {
		out.Commits = append(out.Commits, toAPICommit(&commits[i]))
	}
	if _, err := g.MergeTree(ctx, base, head, ""); err != nil {
		var ce *git.ConflictError
		if !errors.As(err, &ce) {
			return nil, err
		}
		out.Mergeable, out.ConflictFiles = apigen.ComparisonMergeableConflict, ce.Files
	}
	var n int64
	err = a.s.db.NewSelect().TableExpr("pull_requests AS pr").ColumnExpr("i.number").Join("JOIN issues AS i ON i.id = pr.issue_id").
		Where("pr.repo_id = ? AND pr.head_branch = ? AND pr.base_branch = ? AND i.state = ?", repo.ID, req.Params.Head, req.Params.Base, model.StateOpen).
		Limit(1).Scan(ctx, &n)
	if err == nil && n > 0 {
		out.ExistingPull = &n
	}
	return apigen.CompareRefs200JSONResponse(out), nil
}

// ---- listing ----

type pullRow struct {
	issue model.Issue
	pr    model.PullRequest
}

func applyStateFilter(q *bun.SelectQuery, state string) *bun.SelectQuery {
	switch state {
	case "", "open":
		return q.Where("i.state = ?", model.StateOpen)
	case "closed":
		return q.Where("i.state = ? AND pr.merged_at IS NULL", model.StateClosed)
	case "merged":
		return q.Where("pr.merged_at IS NOT NULL")
	}
	return q
}

func pageParams(page *apigen.Page, perPage *apigen.PerPage) (limit, offset int) {
	limit, p := 30, 1
	if perPage != nil && *perPage > 0 {
		limit = min(*perPage, 100)
	}
	if page != nil && *page > 0 {
		p = *page
	}
	return limit, (p - 1) * limit
}

// listPullRows runs a filtered, paginated query over pull requests.
func (s *Server) listPullRows(ctx context.Context, q *bun.SelectQuery, limit, offset int) ([]pullRow, int, error) {
	total64, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	total := int(total64)
	var issues []model.Issue
	if err := q.Order("i.updated_at DESC", "i.id DESC").Limit(int64(limit)).Offset(int64(offset)).Scan(ctx, &issues); err != nil {
		return nil, 0, err
	}
	if len(issues) == 0 {
		return []pullRow{}, total, nil
	}
	ids := make([]int64, len(issues))
	for i := range issues {
		ids[i] = issues[i].ID
	}
	var prs []model.PullRequest
	if err := s.db.NewSelect().Model(&prs).Where("issue_id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return nil, 0, err
	}
	byID := map[int64]model.PullRequest{}
	for _, pr := range prs {
		byID[pr.IssueID] = pr
	}
	rows := make([]pullRow, len(issues))
	for i := range issues {
		rows[i] = pullRow{issue: issues[i], pr: byID[issues[i].ID]}
	}
	return rows, total, nil
}

func (s *Server) toPullSummaries(ctx context.Context, rows []pullRow) ([]apigen.PullSummary, error) {
	out := make([]apigen.PullSummary, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]int64, len(rows))
	repoIDs := map[int64]bool{}
	uc := s.users()
	var uids []int64
	for i, r := range rows {
		ids[i] = r.issue.ID
		repoIDs[r.issue.RepoID] = true
		uids = append(uids, r.issue.AuthorID)
	}
	reviews, err := s.pullReviews(ctx, s.db, ids...)
	if err != nil {
		return nil, err
	}
	requested, err := s.reviewRequests(ctx, s.db, ids...)
	if err != nil {
		return nil, err
	}
	for _, r := range requested {
		uids = append(uids, r...)
	}
	if err := uc.load(ctx, uids...); err != nil {
		return nil, err
	}
	var repos []model.Repo
	if err := s.db.NewSelect().Model(&repos).Where("r.id IN (?)", bun.List(sortedKeysInt(repoIDs))).Scan(ctx); err != nil {
		return nil, err
	}
	repoByID := map[int64]*model.Repo{}
	rulesByRepo := map[int64][]model.BranchProtection{}
	for i := range repos {
		repoByID[repos[i].ID] = &repos[i]
		if rulesByRepo[repos[i].ID], err = s.protectionRules(ctx, s.db, repos[i].ID); err != nil {
			return nil, err
		}
	}
	issuePtrs := make([]*model.Issue, len(rows))
	for i := range rows {
		issuePtrs[i] = &rows[i].issue
	}
	meta, err := s.loadIssueMeta(ctx, issuePtrs)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		pc := &pullCtx{issue: &r.issue, pr: &r.pr}
		rule := matchRule(rulesByRepo[r.issue.RepoID], r.pr.BaseBranch)
		rs := summarizeReviews(ctx, uc, reviews[r.issue.ID], requested[r.issue.ID], r.issue.AuthorID, r.pr.HeadSHA, rule)
		sum := apigen.PullSummary{
			Number: r.issue.Number, Repo: repoByID[r.issue.RepoID].Name, Title: r.issue.Title, State: pc.state(),
			Draft: r.pr.Draft, Author: uc.summary(ctx, r.issue.AuthorID), HeadBranch: r.pr.HeadBranch, BaseBranch: r.pr.BaseBranch,
			Comments: r.issue.CommentCount, ReviewDecision: rs.decision, RequestedReviewers: []apigen.UserSummary{},
			CreatedAt: r.issue.CreatedAt, UpdatedAt: r.issue.UpdatedAt, ClosedAt: r.issue.ClosedAt, MergedAt: r.pr.MergedAt,
		}
		for _, id := range requested[r.issue.ID] {
			sum.RequestedReviewers = append(sum.RequestedReviewers, uc.summary(ctx, id))
		}
		sum.Labels, sum.Assignees, sum.Milestone = meta.labelsOf(r.issue.ID), meta.assigneesOf(ctx, uc, r.issue.ID), meta.milestoneOf(&r.issue)
		out = append(out, sum)
	}
	return out, nil
}

func sortedKeysInt(m map[int64]bool) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (a *api) ListPulls(ctx context.Context, req apigen.ListPullsRequestObject) (apigen.ListPullsResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	p := req.Params
	q := a.s.db.NewSelect().Model((*model.Issue)(nil)).Join("JOIN pull_requests AS pr ON pr.issue_id = i.id").Where("i.repo_id = ?", repo.ID)
	q = applyStateFilter(q, string(deref(p.State)))
	if v := deref(p.Author); v != "" {
		q = q.Where("i.author_id = (SELECT id FROM users WHERE username = ?)", strings.ToLower(v))
	}
	if v := deref(p.Base); v != "" {
		q = q.Where("pr.base_branch = ?", v)
	}
	if v := deref(p.Head); v != "" {
		q = q.Where("pr.head_branch = ?", v)
	}
	if v := deref(p.Q); v != "" {
		q = q.Where("LOWER(i.title) LIKE ? ESCAPE '!'", "%"+escapeLike(strings.ToLower(v))+"%")
	}
	if v := deref(p.Labels); v != "" {
		q = labelFilter(q, repo.ID, v)
	}
	limit, offset := pageParams(p.Page, p.PerPage)
	rows, total, err := a.s.listPullRows(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	sums, err := a.s.toPullSummaries(ctx, rows)
	if err != nil {
		return nil, err
	}
	return apigen.ListPulls200JSONResponse{Pulls: sums, Total: total}, nil
}

func (a *api) ListMyPulls(ctx context.Context, req apigen.ListMyPullsRequestObject) (apigen.ListMyPullsResponseObject, error) {
	u := mustPrincipal(ctx).User
	p := req.Params
	q := a.s.db.NewSelect().Model((*model.Issue)(nil)).
		Join("JOIN pull_requests AS pr ON pr.issue_id = i.id").Join("JOIN repos AS r ON r.id = i.repo_id")
	q = authz.VisibleRepos(q, u)
	q = applyStateFilter(q, string(deref(p.State)))
	switch deref(p.Filter) {
	case "", apigen.ListMyPullsParamsFilterReviewRequested:
		q = q.Where("i.id IN (SELECT issue_id FROM review_requests WHERE user_id = ?)", u.ID)
	case apigen.ListMyPullsParamsFilterAuthored:
		q = q.Where("i.author_id = ?", u.ID)
	}
	limit, offset := pageParams(p.Page, p.PerPage)
	rows, total, err := a.s.listPullRows(ctx, q, limit, offset)
	if err != nil {
		return nil, err
	}
	sums, err := a.s.toPullSummaries(ctx, rows)
	if err != nil {
		return nil, err
	}
	return apigen.ListMyPulls200JSONResponse{Pulls: sums, Total: total}, nil
}

// ---- create / get / update ----

// reviewerIDs resolves usernames to users who can review in repo.
func (s *Server) reviewerIDs(ctx context.Context, db bun.IDB, repo *model.Repo, names []string, authorID int64) ([]int64, error) {
	var ids []int64
	seen := map[int64]bool{}
	for _, n := range names {
		u := new(model.User)
		err := db.NewSelect().Model(u).Where("username = ?", strings.ToLower(strings.TrimSpace(n))).Scan(ctx)
		if err != nil || u.Disabled {
			return nil, apierr.Validation("reviewer %q doesn't exist", n)
		}
		if u.ID == authorID {
			return nil, apierr.Validation("the author can't review their own pull request")
		}
		perm, err := authz.RepoPerm(ctx, db, u, repo)
		if err != nil {
			return nil, err
		}
		if perm < authz.Triage {
			return nil, apierr.Validation("%s can't access %s, so they can't review", u.Username, repo.Name)
		}
		if !seen[u.ID] {
			seen[u.ID] = true
			ids = append(ids, u.ID)
		}
	}
	return ids, nil
}

func (s *Server) existingOpenPull(ctx context.Context, db bun.IDB, repoID int64, head, base string, except int64) (int64, error) {
	var n int64
	err := db.NewSelect().TableExpr("pull_requests AS pr").ColumnExpr("i.number").Join("JOIN issues AS i ON i.id = pr.issue_id").
		Where("pr.repo_id = ? AND pr.head_branch = ? AND pr.base_branch = ? AND i.state = ? AND i.id <> ?", repoID, head, base, model.StateOpen, except).
		Limit(1).Scan(ctx, &n)
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		return 0, err
	}
	return n, nil
}

func (a *api) CreatePull(ctx context.Context, req apigen.CreatePullRequestObject) (apigen.CreatePullResponseObject, error) {
	repo, perm, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	if repo.Archived {
		return nil, apierr.Forbidden("%s is archived", repo.Name)
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	b := req.Body
	user := mustPrincipal(ctx).User
	title, err := validateTitle(b.Title)
	if err != nil {
		return nil, err
	}
	body := deref(b.Body)
	if err := validateBody(body, maxBody); err != nil {
		return nil, err
	}
	base := deref(b.Base)
	if base == "" {
		base = repo.DefaultBranch
	}
	if b.Head == base {
		return nil, apierr.Validation("head and base are the same branch")
	}
	g := a.s.repos.Open(repo)
	headSHA, err := g.RefCommit(ctx, branchRef(b.Head))
	if err != nil || headSHA == "" || !git.ValidRefName(b.Head) {
		return nil, apierr.Validation("head branch %q doesn't exist", b.Head)
	}
	baseSHA, err := g.RefCommit(ctx, branchRef(base))
	if err != nil || baseSHA == "" || !git.ValidRefName(base) {
		return nil, apierr.Validation("base branch %q doesn't exist", base)
	}
	if mb, err := g.MergeBase(ctx, baseSHA, headSHA); err != nil {
		return nil, err
	} else if mb == "" {
		return nil, apierr.Validation("%s and %s have no common history", base, b.Head)
	}
	if ahead, _, err := g.AheadBehind(ctx, baseSHA, headSHA); err != nil {
		return nil, err
	} else if ahead == 0 {
		return nil, apierr.Validation("%s has no commits that aren't already in %s", b.Head, base)
	}
	var number int64
	var created *model.PullRequest
	err = a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if n, err := a.s.existingOpenPull(ctx, tx, repo.ID, b.Head, base, 0); err != nil {
			return err
		} else if n > 0 {
			return apierr.Conflict("pull request #%d from %s into %s is already open", n, b.Head, base).WithDetails(map[string]any{"number": n})
		}
		var reviewers []int64
		if b.Reviewers != nil {
			if reviewers, err = a.s.reviewerIDs(ctx, tx, repo, *b.Reviewers, user.ID); err != nil {
				return err
			}
		}
		if number, err = nextNumber(ctx, tx, repo.ID); err != nil {
			return err
		}
		now := time.Now().UTC()
		issue := &model.Issue{RepoID: repo.ID, Number: number, IsPull: true, Title: title, Body: body, AuthorID: user.ID,
			State: model.StateOpen, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.NewInsert().Model(issue).Exec(ctx); err != nil {
			return err
		}
		pr := &model.PullRequest{IssueID: issue.ID, RepoID: repo.ID, HeadBranch: b.Head, BaseBranch: base, HeadSHA: headSHA,
			Draft: deref(b.Draft), Mergeable: "unknown", ConflictFiles: "[]"}
		created = pr
		if _, err := tx.NewInsert().Model(pr).Exec(ctx); err != nil {
			return err
		}
		for _, id := range reviewers {
			if _, err := tx.NewInsert().Model(&model.ReviewRequest{IssueID: issue.ID, UserID: id, CreatedAt: now}).Exec(ctx); err != nil {
				return err
			}
		}
		if err := a.s.applyMeta(ctx, tx, repo, issue, user, metaChange{addLabels: b.Labels, addAssignees: b.Assignees, milestone: b.Milestone}); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*model.IssueEvent)(nil)).Where("issue_id = ?", issue.ID).Exec(ctx); err != nil {
			return err // metadata set at creation isn't news
		}
		return g.UpdateRef(ctx, pullRef(number), headSHA, "")
	})
	if err != nil {
		return nil, err
	}
	a.s.ciOnPull(context.WithoutCancel(ctx), repo, user, created, number, title, "opened")
	pc, err := a.s.loadPullIn(ctx, a.s.db, repo, perm, number)
	if err != nil {
		return nil, err
	}
	out, err := a.s.toAPIPull(ctx, pc, a.s.users())
	if err != nil {
		return nil, err
	}
	a.s.hooksOpened(ctx, repo, user, number, out, true)
	// Auto-labels come after "opened", as with a labeler workflow.
	mark := a.s.eventMark(ctx)
	a.s.labelPull(context.WithoutCancel(ctx), repo, created, number)
	a.s.hooksFromEvents(context.WithoutCancel(ctx), repo, user, mark)
	if pc, err := a.s.loadPullIn(ctx, a.s.db, repo, perm, number); err == nil {
		if o, err := a.s.toAPIPull(ctx, pc, a.s.users()); err == nil {
			out = o
		}
	}
	return apigen.CreatePull201JSONResponse(out), nil
}

func (a *api) GetPull(ctx context.Context, req apigen.GetPullRequestObject) (apigen.GetPullResponseObject, error) {
	pc, err := a.s.loadPull(ctx, req.Repo, req.Number, authz.Read)
	if err != nil {
		return nil, err
	}
	out, err := a.s.toAPIPull(ctx, pc, a.s.users())
	if err != nil {
		return nil, err
	}
	return apigen.GetPull200JSONResponse(out), nil
}

// canEditPull: the author or anyone with write access.
func canEditPull(pc *pullCtx, u *model.User) bool {
	return pc.perm >= authz.Write || (pc.perm >= authz.Triage && pc.issue.AuthorID == u.ID)
}

func (a *api) UpdatePull(ctx context.Context, req apigen.UpdatePullRequestObject) (apigen.UpdatePullResponseObject, error) {
	pc, err := a.s.loadPull(ctx, req.Repo, req.Number, authz.Triage)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	b := req.Body
	user := mustPrincipal(ctx).User
	editing := b.Title != nil || b.Body != nil || b.Base != nil || b.State != nil || b.Draft != nil
	ctx = hookScope(ctx)
	mark := a.s.eventMark(ctx)
	oldBody := pc.issue.Body
	if editing && !canEditPull(pc, user) {
		return nil, apierr.Forbidden("only the author or someone with write access can change this pull request")
	}
	if pc.repo.Archived {
		return nil, apierr.Forbidden("%s is archived", pc.repo.Name)
	}
	if pc.merged() && (b.Base != nil || b.State != nil || b.Draft != nil) {
		return nil, apierr.Conflict("pull request #%d is already merged", pc.issue.Number)
	}
	ciAction := ""
	err = a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		issue, pr := pc.issue, pc.pr
		now := time.Now().UTC()
		if b.Title != nil {
			t, err := validateTitle(*b.Title)
			if err != nil {
				return err
			}
			if t != issue.Title {
				if err := addEvent(ctx, tx, issue.ID, user, evTitleChanged, map[string]any{"from": issue.Title, "to": t}); err != nil {
					return err
				}
				issue.Title = t
			}
		}
		if b.Body != nil {
			if err := validateBody(*b.Body, maxBody); err != nil {
				return err
			}
			issue.Body = *b.Body
		}
		if b.Base != nil && *b.Base != pr.BaseBranch {
			nb := *b.Base
			if nb == pr.HeadBranch {
				return apierr.Validation("head and base are the same branch")
			}
			if sha, err := pc.g.RefCommit(ctx, branchRef(nb)); err != nil || sha == "" || !git.ValidRefName(nb) {
				return apierr.Validation("base branch %q doesn't exist", nb)
			}
			if n, err := a.s.existingOpenPull(ctx, tx, pc.repo.ID, pr.HeadBranch, nb, issue.ID); err != nil {
				return err
			} else if n > 0 && issue.State == model.StateOpen {
				return apierr.Conflict("pull request #%d from %s into %s is already open", n, pr.HeadBranch, nb)
			}
			if err := addEvent(ctx, tx, issue.ID, user, evBaseChanged, map[string]any{"from": pr.BaseBranch, "to": nb}); err != nil {
				return err
			}
			pr.BaseBranch, pr.Mergeable = nb, "unknown"
		}
		if b.Draft != nil && *b.Draft != pr.Draft {
			ev := evReady
			if *b.Draft {
				ev = evDraft
			}
			if err := addEvent(ctx, tx, issue.ID, user, ev, nil); err != nil {
				return err
			}
			if !*b.Draft && issue.State == model.StateOpen {
				ciAction = "ready_for_review"
			}
			pr.Draft = *b.Draft
		}
		if b.State != nil {
			switch {
			case *b.State == apigen.UpdatePullRequestStateClosed && issue.State == model.StateOpen:
				issue.State, issue.ClosedAt, issue.ClosedByID = model.StateClosed, &now, &user.ID
				if err := addEvent(ctx, tx, issue.ID, user, evClosed, nil); err != nil {
					return err
				}
			case *b.State == apigen.UpdatePullRequestStateOpen && issue.State == model.StateClosed:
				tip, err := pc.g.RefCommit(ctx, branchRef(pr.HeadBranch))
				if err != nil {
					return err
				}
				if tip == "" {
					return apierr.Conflict("can't reopen: the head branch %s no longer exists", pr.HeadBranch)
				}
				if n, err := a.s.existingOpenPull(ctx, tx, pc.repo.ID, pr.HeadBranch, pr.BaseBranch, issue.ID); err != nil {
					return err
				} else if n > 0 {
					return apierr.Conflict("can't reopen: pull request #%d from %s into %s is already open", n, pr.HeadBranch, pr.BaseBranch)
				}
				issue.State, issue.ClosedAt, issue.ClosedByID = model.StateOpen, nil, nil
				if tip != pr.HeadSHA {
					if err := pc.g.UpdateRef(ctx, pullRef(issue.Number), tip, ""); err != nil {
						return err
					}
				}
				pr.HeadSHA, pr.HeadDeleted = tip, false
				if err := addEvent(ctx, tx, issue.ID, user, evReopened, nil); err != nil {
					return err
				}
				ciAction = "reopened"
			}
		}
		if b.Reviewers != nil {
			if err := a.s.setReviewers(ctx, tx, pc, user, *b.Reviewers); err != nil {
				return err
			}
		}
		if err := a.s.applyMeta(ctx, tx, pc.repo, issue, user, metaChange{
			labels: b.Labels, addLabels: b.AddLabels, removeLabels: b.RemoveLabels,
			assignees: b.Assignees, addAssignees: b.AddAssignees, removeAssignees: b.RemoveAssignees, milestone: b.Milestone,
		}); err != nil {
			return err
		}
		issue.UpdatedAt = now
		if _, err := tx.NewUpdate().Model(issue).Column("title", "body", "state", "closed_at", "closed_by_id", "updated_at").WherePK().Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewUpdate().Model(pr).Column("base_branch", "draft", "head_sha", "head_deleted", "mergeable").WherePK().Exec(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	if ciAction != "" {
		a.s.ciOnPull(context.WithoutCancel(ctx), pc.repo, user, pc.pr, pc.issue.Number, pc.issue.Title, ciAction)
	}
	out, err := a.s.toAPIPull(ctx, pc, a.s.users())
	if err != nil {
		return nil, err
	}
	var edited []int64
	if pc.issue.Body != oldBody {
		edited = append(edited, pc.issue.ID)
	}
	a.s.hooksFromEvents(ctx, pc.repo, user, mark, edited...)
	return apigen.UpdatePull200JSONResponse(out), nil
}

func (s *Server) setReviewers(ctx context.Context, tx bun.Tx, pc *pullCtx, user *model.User, names []string) error {
	want, err := s.reviewerIDs(ctx, tx, pc.repo, names, pc.issue.AuthorID)
	if err != nil {
		return err
	}
	cur, err := s.reviewRequests(ctx, tx, pc.issue.ID)
	if err != nil {
		return err
	}
	have := map[int64]bool{}
	for _, id := range cur[pc.issue.ID] {
		have[id] = true
	}
	wantSet := map[int64]bool{}
	uc := s.users()
	for _, id := range want {
		wantSet[id] = true
		if !have[id] {
			if _, err := tx.NewInsert().Model(&model.ReviewRequest{IssueID: pc.issue.ID, UserID: id, CreatedAt: time.Now().UTC()}).Exec(ctx); err != nil {
				return err
			}
			if err := addEvent(ctx, tx, pc.issue.ID, user, evReviewRequested, map[string]any{"reviewer": uc.summary(ctx, id).Username}); err != nil {
				return err
			}
		}
	}
	for id := range have {
		if !wantSet[id] {
			if _, err := tx.NewDelete().Model((*model.ReviewRequest)(nil)).Where("issue_id = ? AND user_id = ?", pc.issue.ID, id).Exec(ctx); err != nil {
				return err
			}
			if err := addEvent(ctx, tx, pc.issue.ID, user, evRequestRemoved, map[string]any{"reviewer": uc.summary(ctx, id).Username}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- commits, diff ----

func (a *api) ListPullCommits(ctx context.Context, req apigen.ListPullCommitsRequestObject) (apigen.ListPullCommitsResponseObject, error) {
	pc, err := a.s.loadPull(ctx, req.Repo, req.Number, authz.Read)
	if err != nil {
		return nil, err
	}
	from, to, err := a.s.diffRange(ctx, pc)
	if err != nil {
		return nil, err
	}
	cs, err := pc.g.LogRange(ctx, from, to, maxPullCommitsList)
	if err != nil {
		return nil, err
	}
	out := make(apigen.ListPullCommits200JSONResponse, 0, len(cs))
	for i := range cs {
		out = append(out, toAPICommit(&cs[i]))
	}
	return out, nil
}

func (a *api) GetPullDiff(ctx context.Context, req apigen.GetPullDiffRequestObject) (apigen.GetPullDiffResponseObject, error) {
	pc, err := a.s.loadPull(ctx, req.Repo, req.Number, authz.Read)
	if err != nil {
		return nil, err
	}
	from, to, err := a.s.diffRange(ctx, pc)
	if err != nil {
		return nil, err
	}
	d, err := pc.g.Diff(ctx, from, to, git.DefaultDiffLimits)
	if err != nil {
		return nil, err
	}
	return apigen.GetPullDiff200JSONResponse(toAPIDiff(d)), nil
}

// ---- comments ----

// commentView converts comments, working out which inline comments are
// outdated (their file changed since).
type commentView struct {
	s     *Server
	pc    *pullCtx
	uc    *userCache
	cache map[string]bool
	head  string // pull request head ("" for issues)
	ref   string // ref relative links resolve against
}

func (s *Server) commentView(pc *pullCtx, uc *userCache) *commentView {
	v := &commentView{s: s, pc: pc, uc: uc, cache: map[string]bool{}, ref: pc.repo.DefaultBranch}
	if pc.pr != nil {
		v.head, v.ref = pc.pr.HeadSHA, pc.pr.HeadSHA
	}
	return v
}

func (v *commentView) outdated(ctx context.Context, c *model.Comment) bool {
	if c.Path == "" || v.head == "" || c.CommitSHA == v.head {
		return false
	}
	key := c.CommitSHA + "\x00" + c.Path
	if o, ok := v.cache[key]; ok {
		return o
	}
	changed, err := v.pc.g.PathChanged(ctx, c.CommitSHA, v.head, c.Path)
	o := err != nil || changed
	v.cache[key] = o
	return o
}

func (v *commentView) toAPI(ctx context.Context, c *model.Comment) (apigen.Comment, error) {
	html, err := renderMarkdown(c.Body, markdownCtx(v.pc.repo, v.ref))
	if err != nil {
		return apigen.Comment{}, err
	}
	out := apigen.Comment{Id: c.ID, Author: v.uc.summary(ctx, c.AuthorID), Body: c.Body, BodyHtml: html,
		ReviewId: c.ReviewID, ReplyTo: c.ReplyToID, Edited: c.Edited, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if c.Path != "" {
		side := apigen.CommentSide(c.Side)
		outdated := v.outdated(ctx, c)
		out.Path, out.Line, out.Side, out.CommitSha, out.DiffHunk, out.Outdated = &c.Path, &c.Line, &side, &c.CommitSHA, &c.DiffHunk, &outdated
		if c.ReplyToID == nil {
			out.Resolved = &c.Resolved
			out.ResolvedBy = v.uc.summaryPtr(ctx, c.ResolvedByID)
		}
	}
	return out, nil
}

func (s *Server) loadComments(ctx context.Context, issueID int64) ([]model.Comment, error) {
	var cs []model.Comment
	err := s.db.NewSelect().Model(&cs).Where("issue_id = ?", issueID).Order("created_at", "id").Scan(ctx)
	return cs, err
}

func (a *api) ListPullComments(ctx context.Context, req apigen.ListPullCommentsRequestObject) (apigen.ListPullCommentsResponseObject, error) {
	pc, err := a.s.loadPull(ctx, req.Repo, req.Number, authz.Read)
	if err != nil {
		return nil, err
	}
	cs, err := a.s.loadComments(ctx, pc.issue.ID)
	if err != nil {
		return nil, err
	}
	uc := a.s.users()
	ids := make([]int64, 0, len(cs))
	for _, c := range cs {
		ids = append(ids, c.AuthorID)
	}
	if err := uc.load(ctx, ids...); err != nil {
		return nil, err
	}
	v := a.s.commentView(pc, uc)
	out := make(apigen.ListPullComments200JSONResponse, 0, len(cs))
	for i := range cs {
		c, err := v.toAPI(ctx, &cs[i])
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// anchorLine finds a line in the diff and returns the diff lines leading up
// to it (at most 6), for display next to the comment.
func anchorLine(d *git.Diff, path, side string, line int) (string, bool) {
	for _, f := range d.Files {
		if (side == "new" && f.NewPath != path) || (side == "old" && f.OldPath != path) {
			continue
		}
		for _, h := range f.Hunks {
			for i, l := range h.Lines {
				n := l.NewLine
				if side == "old" {
					n = l.OldLine
				}
				if n != line || (side == "new" && l.Type == "del") || (side == "old" && l.Type == "add") {
					continue
				}
				start := max(0, i-5)
				var b strings.Builder
				b.WriteString(h.Header + "\n")
				for _, x := range h.Lines[start : i+1] {
					prefix := " "
					switch x.Type {
					case "add":
						prefix = "+"
					case "del":
						prefix = "-"
					}
					b.WriteString(prefix + x.Content + "\n")
				}
				return strings.TrimRight(b.String(), "\n"), true
			}
		}
	}
	return "", false
}

type inlineInput struct {
	path, side, body string
	line             int
}

// buildInline validates an inline comment against the current diff.
func (s *Server) buildInline(ctx context.Context, pc *pullCtx, d **git.Diff, in inlineInput) (*model.Comment, error) {
	p, ok := git.CleanPath(in.path)
	if !ok || p == "" {
		return nil, apierr.Validation("invalid path %q", in.path)
	}
	if in.side == "" {
		in.side = "new"
	}
	if in.side != "new" && in.side != "old" {
		return nil, apierr.Validation("side must be new or old")
	}
	if *d == nil {
		from, to, err := s.diffRange(ctx, pc)
		if err != nil {
			return nil, err
		}
		if *d, err = pc.g.Diff(ctx, from, to, git.DefaultDiffLimits); err != nil {
			return nil, err
		}
	}
	hunk, ok := anchorLine(*d, p, in.side, in.line)
	if !ok {
		return nil, apierr.Validation("line %d (%s side) of %s isn't part of the diff; inline comments must be on changed or context lines", in.line, in.side, p)
	}
	return &model.Comment{IssueID: pc.issue.ID, Path: p, Line: in.line, Side: in.side, CommitSHA: pc.pr.HeadSHA, DiffHunk: hunk, Body: in.body}, nil
}

func (s *Server) insertComments(ctx context.Context, tx bun.IDB, issueID int64, cs ...*model.Comment) error {
	for _, c := range cs {
		if _, err := tx.NewInsert().Model(c).Exec(ctx); err != nil {
			return err
		}
	}
	if len(cs) == 0 {
		return nil
	}
	_, err := tx.NewUpdate().Model((*model.Issue)(nil)).Set("comment_count = comment_count + ?", len(cs)).
		Set("updated_at = ?", time.Now().UTC()).Where("id = ?", issueID).Exec(ctx)
	return err
}

func (a *api) CreatePullComment(ctx context.Context, req apigen.CreatePullCommentRequestObject) (apigen.CreatePullCommentResponseObject, error) {
	pc, err := a.s.loadPull(ctx, req.Repo, req.Number, authz.Triage)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	if pc.repo.Archived {
		return nil, apierr.Forbidden("%s is archived", pc.repo.Name)
	}
	b := req.Body
	body := strings.TrimSpace(b.Body)
	if body == "" {
		return nil, apierr.Validation("comment body is required")
	}
	if err := validateBody(body, maxComment); err != nil {
		return nil, err
	}
	user := mustPrincipal(ctx).User
	now := time.Now().UTC()
	var c *model.Comment
	switch {
	case b.ReplyTo != nil:
		root := new(model.Comment)
		if err := a.s.db.NewSelect().Model(root).Where("id = ? AND issue_id = ?", *b.ReplyTo, pc.issue.ID).Scan(ctx); err != nil {
			return nil, apierr.NotFound("comment %d not found on this pull request", *b.ReplyTo)
		}
		if root.ReplyToID != nil {
			root.ID = *root.ReplyToID // replies always attach to the thread's first comment
			if err := a.s.db.NewSelect().Model(root).WherePK().Scan(ctx); err != nil {
				return nil, err
			}
		}
		if root.Path == "" {
			return nil, apierr.Validation("only inline comments have threads; post a normal comment instead")
		}
		c = &model.Comment{IssueID: pc.issue.ID, ReplyToID: &root.ID, Path: root.Path, Line: root.Line, Side: root.Side,
			CommitSHA: root.CommitSHA, DiffHunk: root.DiffHunk}
	case b.Path != nil && *b.Path != "":
		if b.Line == nil {
			return nil, apierr.Validation("line is required for inline comments")
		}
		var d *git.Diff
		if c, err = a.s.buildInline(ctx, pc, &d, inlineInput{path: *b.Path, line: *b.Line, side: string(deref(b.Side))}); err != nil {
			return nil, err
		}
	default:
		c = &model.Comment{IssueID: pc.issue.ID}
	}
	c.AuthorID, c.Body, c.CreatedAt, c.UpdatedAt = user.ID, body, now, now
	if err := a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return a.s.insertComments(ctx, tx, pc.issue.ID, c)
	}); err != nil {
		return nil, err
	}
	out, err := a.s.commentView(pc, a.s.users()).toAPI(ctx, c)
	if err != nil {
		return nil, err
	}
	a.s.hooksComment(ctx, pc.repo, user, pc.issue, "created", out)
	return apigen.CreatePullComment201JSONResponse(out), nil
}

// commentFor loads a comment of a repository along with its issue or pull
// request (pr is nil for issues).
func (s *Server) commentFor(ctx context.Context, repoName string, id int64, need authz.Perm) (*model.Comment, *pullCtx, error) {
	repo, perm, err := s.repoFor(ctx, repoName, need)
	if err != nil {
		return nil, nil, err
	}
	c := new(model.Comment)
	err = s.db.NewSelect().Model(c).Where("id = ? AND issue_id IN (SELECT id FROM issues WHERE repo_id = ?)", id, repo.ID).Scan(ctx)
	if err != nil {
		return nil, nil, apierr.NotFound("comment %d not found", id)
	}
	issue := new(model.Issue)
	if err := s.db.NewSelect().Model(issue).Where("id = ?", c.IssueID).Scan(ctx); err != nil {
		return nil, nil, err
	}
	if !issue.IsPull {
		return c, &pullCtx{repo: repo, perm: perm, issue: issue, g: s.repos.Open(repo)}, nil
	}
	pc, err := s.loadPullIn(ctx, s.db, repo, perm, issue.Number)
	if err != nil {
		return nil, nil, err
	}
	return c, pc, nil
}

func (a *api) UpdateComment(ctx context.Context, req apigen.UpdateCommentRequestObject) (apigen.UpdateCommentResponseObject, error) {
	c, pc, err := a.s.commentFor(ctx, req.Repo, req.Id, authz.Triage)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	user := mustPrincipal(ctx).User
	cols := []string{}
	if req.Body.Body != nil {
		if c.AuthorID != user.ID {
			return nil, apierr.Forbidden("only the author can edit a comment")
		}
		body := strings.TrimSpace(*req.Body.Body)
		if body == "" {
			return nil, apierr.Validation("comment body is required")
		}
		if err := validateBody(body, maxComment); err != nil {
			return nil, err
		}
		if body != c.Body {
			c.Body, c.Edited, c.UpdatedAt = body, true, time.Now().UTC()
			cols = append(cols, "body", "edited", "updated_at")
		}
	}
	if req.Body.Resolved != nil {
		if c.Path == "" || c.ReplyToID != nil {
			return nil, apierr.Validation("only the first comment of an inline thread can be resolved")
		}
		c.Resolved = *req.Body.Resolved
		c.ResolvedByID = nil
		if c.Resolved {
			c.ResolvedByID = &user.ID
		}
		cols = append(cols, "resolved", "resolved_by_id")
	}
	if len(cols) > 0 {
		if _, err := a.s.db.NewUpdate().Model(c).Column(cols...).WherePK().Exec(ctx); err != nil {
			return nil, err
		}
	}
	out, err := a.s.commentView(pc, a.s.users()).toAPI(ctx, c)
	if err != nil {
		return nil, err
	}
	if req.Body.Body != nil {
		a.s.hooksComment(ctx, pc.repo, mustPrincipal(ctx).User, pc.issue, "edited", out)
	}
	return apigen.UpdateComment200JSONResponse(out), nil
}

func (a *api) DeleteComment(ctx context.Context, req apigen.DeleteCommentRequestObject) (apigen.DeleteCommentResponseObject, error) {
	c, pc, err := a.s.commentFor(ctx, req.Repo, req.Id, authz.Triage)
	if err != nil {
		return nil, err
	}
	user := mustPrincipal(ctx).User
	if c.AuthorID != user.ID && pc.perm < authz.Admin {
		return nil, apierr.Forbidden("only the author or an admin can delete a comment")
	}
	gone, gerr := a.s.commentView(pc, a.s.users()).toAPI(ctx, c)
	err = a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Deleting a thread's first comment deletes its replies (FK cascade).
		n, err := tx.NewSelect().Model((*model.Comment)(nil)).Where("id = ? OR reply_to_id = ?", c.ID, c.ID).Count(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*model.Comment)(nil)).Where("reply_to_id = ?", c.ID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model(c).WherePK().Exec(ctx); err != nil {
			return err
		}
		_, err = tx.NewUpdate().Model((*model.Issue)(nil)).Set("comment_count = comment_count - ?", n).Where("id = ?", c.IssueID).Exec(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	if gerr == nil {
		a.s.hooksComment(ctx, pc.repo, user, pc.issue, "deleted", gone)
	}
	return apigen.DeleteComment204Response{}, nil
}

// ---- reviews ----

func (s *Server) toAPIReview(ctx context.Context, v *commentView, r *model.Review, comments []model.Comment) (apigen.Review, error) {
	html, err := renderMarkdown(r.Body, markdownCtx(v.pc.repo, v.ref))
	if err != nil {
		return apigen.Review{}, err
	}
	out := apigen.Review{Id: r.ID, Author: v.uc.summary(ctx, r.AuthorID), State: apigen.ReviewState(r.State), Body: r.Body, BodyHtml: html,
		CommitSha: r.CommitSHA, Stale: r.CommitSHA != v.head, Dismissed: r.Dismissed, DismissedBy: v.uc.summaryPtr(ctx, r.DismissedByID),
		Comments: []apigen.Comment{}, CreatedAt: r.CreatedAt}
	if r.DismissMessage != "" {
		out.DismissMessage = &r.DismissMessage
	}
	for i := range comments {
		if comments[i].ReviewID != nil && *comments[i].ReviewID == r.ID && comments[i].ReplyToID == nil {
			c, err := v.toAPI(ctx, &comments[i])
			if err != nil {
				return out, err
			}
			out.Comments = append(out.Comments, c)
		}
	}
	return out, nil
}

func (a *api) ListReviews(ctx context.Context, req apigen.ListReviewsRequestObject) (apigen.ListReviewsResponseObject, error) {
	pc, err := a.s.loadPull(ctx, req.Repo, req.Number, authz.Read)
	if err != nil {
		return nil, err
	}
	reviews, err := a.s.pullReviews(ctx, a.s.db, pc.issue.ID)
	if err != nil {
		return nil, err
	}
	comments, err := a.s.loadComments(ctx, pc.issue.ID)
	if err != nil {
		return nil, err
	}
	v := a.s.commentView(pc, a.s.users())
	out := make(apigen.ListReviews200JSONResponse, 0)
	for _, r := range reviews[pc.issue.ID] {
		ar, err := a.s.toAPIReview(ctx, v, &r, comments)
		if err != nil {
			return nil, err
		}
		out = append(out, ar)
	}
	return out, nil
}

func (a *api) CreateReview(ctx context.Context, req apigen.CreateReviewRequestObject) (apigen.CreateReviewResponseObject, error) {
	pc, err := a.s.loadPull(ctx, req.Repo, req.Number, authz.Triage)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	b := req.Body
	user := mustPrincipal(ctx).User
	if !pc.open() {
		return nil, apierr.Conflict("pull request #%d is %s", pc.issue.Number, pc.state())
	}
	if pc.repo.Archived {
		return nil, apierr.Forbidden("%s is archived", pc.repo.Name)
	}
	var state string
	switch b.State {
	case apigen.CreateReviewStateApprove:
		state = model.ReviewApproved
	case apigen.CreateReviewStateRequestChanges:
		state = model.ReviewChangesRequested
	case apigen.CreateReviewStateComment:
		state = model.ReviewCommented
	default:
		return nil, apierr.Validation("state must be approve, request_changes or comment")
	}
	if state != model.ReviewCommented && pc.issue.AuthorID == user.ID {
		return nil, apierr.Forbidden("you can't approve or request changes on your own pull request; leave a comment instead")
	}
	if sha := deref(b.CommitSha); sha != "" && sha != pc.pr.HeadSHA {
		return nil, apierr.Conflict("the head moved to %s since you reviewed %s; review the new commits", short(pc.pr.HeadSHA), short(sha))
	}
	body := strings.TrimSpace(deref(b.Body))
	if err := validateBody(body, maxBody); err != nil {
		return nil, err
	}
	var inputs []apigen.InlineCommentInput
	if b.Comments != nil {
		inputs = *b.Comments
	}
	if body == "" && len(inputs) == 0 && state != model.ReviewApproved {
		return nil, apierr.Validation("say what needs changing: add a body or inline comments")
	}
	if len(inputs) > 200 {
		return nil, apierr.Validation("too many inline comments in one review (max 200)")
	}
	now := time.Now().UTC()
	var d *git.Diff
	var comments []*model.Comment
	for _, in := range inputs {
		text := strings.TrimSpace(in.Body)
		if text == "" {
			return nil, apierr.Validation("inline comment on %s:%d has no text", in.Path, in.Line)
		}
		if err := validateBody(text, maxComment); err != nil {
			return nil, err
		}
		side := ""
		if in.Side != nil {
			side = string(*in.Side)
		}
		c, err := a.s.buildInline(ctx, pc, &d, inlineInput{path: in.Path, line: in.Line, side: side, body: text})
		if err != nil {
			return nil, err
		}
		c.AuthorID, c.CreatedAt, c.UpdatedAt = user.ID, now, now
		comments = append(comments, c)
	}
	review := &model.Review{IssueID: pc.issue.ID, AuthorID: user.ID, State: state, Body: body, CommitSHA: pc.pr.HeadSHA, CreatedAt: now}
	err = a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(review).Exec(ctx); err != nil {
			return err
		}
		for _, c := range comments {
			c.ReviewID = &review.ID
		}
		if err := a.s.insertComments(ctx, tx, pc.issue.ID, comments...); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*model.ReviewRequest)(nil)).Where("issue_id = ? AND user_id = ?", pc.issue.ID, user.ID).Exec(ctx); err != nil {
			return err
		}
		return touchIssue(ctx, tx, pc.issue.ID)
	})
	if err != nil {
		return nil, err
	}
	all, err := a.s.loadComments(ctx, pc.issue.ID)
	if err != nil {
		return nil, err
	}
	out, err := a.s.toAPIReview(ctx, a.s.commentView(pc, a.s.users()), review, all)
	if err != nil {
		return nil, err
	}
	a.s.hooksReview(ctx, pc, "submitted", out)
	return apigen.CreateReview201JSONResponse(out), nil
}

func (a *api) DismissReview(ctx context.Context, req apigen.DismissReviewRequestObject) (apigen.DismissReviewResponseObject, error) {
	pc, err := a.s.loadPull(ctx, req.Repo, req.Number, authz.Write)
	if err != nil {
		return nil, err
	}
	if req.Body == nil || strings.TrimSpace(req.Body.Message) == "" {
		return nil, apierr.Validation("a message explaining the dismissal is required")
	}
	r := new(model.Review)
	if err := a.s.db.NewSelect().Model(r).Where("id = ? AND issue_id = ?", req.Id, pc.issue.ID).Scan(ctx); err != nil {
		return nil, apierr.NotFound("review %d not found on this pull request", req.Id)
	}
	if r.State == model.ReviewCommented {
		return nil, apierr.Validation("only approvals and change requests can be dismissed")
	}
	if r.Dismissed {
		return nil, apierr.Conflict("review %d is already dismissed", r.ID)
	}
	user := mustPrincipal(ctx).User
	now := time.Now().UTC()
	msg := strings.TrimSpace(req.Body.Message)
	err = a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		r.Dismissed, r.DismissedByID, r.DismissMessage, r.DismissedAt = true, &user.ID, msg, &now
		if _, err := tx.NewUpdate().Model(r).Column("dismissed", "dismissed_by_id", "dismiss_message", "dismissed_at").WherePK().Exec(ctx); err != nil {
			return err
		}
		reviewer := a.s.users().summary(ctx, r.AuthorID).Username
		return addEvent(ctx, tx, pc.issue.ID, user, evReviewDismissed, map[string]any{"reviewer": reviewer, "message": msg, "review_id": r.ID})
	})
	if err != nil {
		return nil, err
	}
	all, err := a.s.loadComments(ctx, pc.issue.ID)
	if err != nil {
		return nil, err
	}
	out, err := a.s.toAPIReview(ctx, a.s.commentView(pc, a.s.users()), r, all)
	if err != nil {
		return nil, err
	}
	a.s.hooksReview(ctx, pc, "dismissed", out)
	return apigen.DismissReview200JSONResponse(out), nil
}

// ---- timeline ----

func (a *api) GetPullTimeline(ctx context.Context, req apigen.GetPullTimelineRequestObject) (apigen.GetPullTimelineResponseObject, error) {
	pc, err := a.s.loadPull(ctx, req.Repo, req.Number, authz.Read)
	if err != nil {
		return nil, err
	}
	items, err := a.s.timeline(ctx, pc)
	if err != nil {
		return nil, err
	}
	return apigen.GetPullTimeline200JSONResponse(items), nil
}

// timeline builds the conversation of an issue or pull request (pc.pr nil
// for issues): top-level comments, reviews with their inline comments,
// standalone inline threads and events.
func (s *Server) timeline(ctx context.Context, pc *pullCtx) ([]apigen.TimelineItem, error) {
	comments, err := s.loadComments(ctx, pc.issue.ID)
	if err != nil {
		return nil, err
	}
	reviews, err := s.pullReviews(ctx, s.db, pc.issue.ID)
	if err != nil {
		return nil, err
	}
	var events []model.IssueEvent
	if err := s.db.NewSelect().Model(&events).Where("issue_id = ?", pc.issue.ID).Order("created_at", "id").Scan(ctx); err != nil {
		return nil, err
	}
	uc := s.users()
	var ids []int64
	for _, c := range comments {
		ids = append(ids, c.AuthorID)
	}
	for _, r := range reviews[pc.issue.ID] {
		ids = append(ids, r.AuthorID)
	}
	for _, e := range events {
		if e.ActorID != nil {
			ids = append(ids, *e.ActorID)
		}
	}
	if err := uc.load(ctx, ids...); err != nil {
		return nil, err
	}
	v := s.commentView(pc, uc)
	items := make([]apigen.TimelineItem, 0, len(comments)+len(events))
	for i := range comments {
		c := &comments[i]
		if c.ReviewID != nil || c.ReplyToID != nil {
			continue
		}
		ac, err := v.toAPI(ctx, c)
		if err != nil {
			return nil, err
		}
		kind := apigen.TimelineItemKindComment
		if c.Path != "" {
			kind = apigen.TimelineItemKindInlineComment
		}
		items = append(items, apigen.TimelineItem{Kind: kind, CreatedAt: c.CreatedAt, Comment: &ac})
	}
	for _, r := range reviews[pc.issue.ID] {
		ar, err := s.toAPIReview(ctx, v, &r, comments)
		if err != nil {
			return nil, err
		}
		items = append(items, apigen.TimelineItem{Kind: apigen.TimelineItemKindReview, CreatedAt: r.CreatedAt, Review: &ar})
	}
	for _, e := range events {
		data := map[string]any{}
		_ = json.Unmarshal([]byte(e.Data), &data)
		items = append(items, apigen.TimelineItem{Kind: apigen.TimelineItemKindEvent, CreatedAt: e.CreatedAt, Event: &apigen.PullEvent{
			Id: e.ID, Type: apigen.PullEventType(e.Type), Actor: uc.summaryPtr(ctx, e.ActorID), Data: data, CreatedAt: e.CreatedAt}})
	}
	sortTimeline(items)
	return items, nil
}

// ---- merge, update branch, delete branch ----

func (a *api) MergePull(ctx context.Context, req apigen.MergePullRequestObject) (apigen.MergePullResponseObject, error) {
	ctx = hookScope(ctx)
	mark := a.s.eventMark(ctx)
	pc, err := a.s.loadPull(ctx, req.Repo, req.Number, authz.Read)
	if err != nil {
		return nil, err
	}
	if pc.repo.Archived {
		return nil, apierr.Forbidden("%s is archived", pc.repo.Name)
	}
	var b apigen.MergePullRequest
	if req.Body != nil {
		b = *req.Body
	}
	if pc.perm < authz.Write {
		return nil, apierr.Forbidden("merging needs write access to %s; you have %s", pc.repo.Name, pc.perm)
	}
	o := mergeOptions{title: strings.TrimSpace(deref(b.Title)), message: deref(b.Message), sha: deref(b.Sha),
		deleteBranch: deref(b.DeleteBranch), bypass: deref(b.BypassProtection)}
	if b.Method != nil {
		o.method = git.MergeMethod(*b.Method)
	}
	if err := a.s.mergePull(ctx, pc, mustPrincipal(ctx).User, o); err != nil {
		return nil, err
	}
	pc, err = a.s.loadPullIn(ctx, a.s.db, pc.repo, pc.perm, pc.issue.Number)
	if err != nil {
		return nil, err
	}
	out, err := a.s.toAPIPull(ctx, pc, a.s.users())
	if err != nil {
		return nil, err
	}
	a.s.hooksFromEvents(ctx, pc.repo, mustPrincipal(ctx).User, mark)
	return apigen.MergePull200JSONResponse(out), nil
}

func (a *api) UpdatePullBranch(ctx context.Context, req apigen.UpdatePullBranchRequestObject) (apigen.UpdatePullBranchResponseObject, error) {
	pc, err := a.s.loadPull(ctx, req.Repo, req.Number, authz.Write)
	if err != nil {
		return nil, err
	}
	user := mustPrincipal(ctx).User
	if !pc.open() || pc.pr.HeadDeleted {
		return nil, apierr.Conflict("pull request #%d is %s", pc.issue.Number, pc.state())
	}
	if pc.repo.Archived {
		return nil, apierr.Forbidden("%s is archived", pc.repo.Name)
	}
	if req.Body != nil && deref(req.Body.Sha) != "" && *req.Body.Sha != pc.pr.HeadSHA {
		return nil, apierr.Conflict("the head moved to %s; not updating", short(pc.pr.HeadSHA))
	}
	mi, err := a.s.mergeState(ctx, pc)
	if err != nil {
		return nil, err
	}
	if mi.baseSHA == "" {
		return nil, apierr.Conflict("the base branch %s no longer exists", pc.pr.BaseBranch)
	}
	if mi.behind == 0 {
		return nil, apierr.Conflict("the head branch already contains the latest %s", pc.pr.BaseBranch)
	}
	rule, err := a.s.ruleFor(ctx, a.s.db, pc.repo.ID, pc.pr.HeadBranch)
	if err != nil {
		return nil, err
	}
	if msg := checkProtectedPush(rule, pc.pr.HeadBranch, user.IsAdmin(), false, false, false); msg != "" {
		return nil, apierr.Forbidden("%s", msg)
	}
	tree, err := pc.g.MergeTree(ctx, pc.pr.HeadSHA, mi.baseSHA, "")
	if err != nil {
		var ce *git.ConflictError
		if errors.As(err, &ce) {
			return nil, apierr.Conflict("%s conflicts with the head branch; update it locally: %v", pc.pr.BaseBranch, ce).WithDetails(map[string]any{"conflict_files": ce.Files})
		}
		return nil, err
	}
	id := a.s.identity(ctx, user)
	commit, err := pc.g.CommitTree(ctx, tree, []string{pc.pr.HeadSHA, mi.baseSHA},
		fmt.Sprintf("Merge branch '%s' into %s\n", pc.pr.BaseBranch, pc.pr.HeadBranch), id, id)
	if err != nil {
		return nil, err
	}
	if err := pc.g.UpdateRef(ctx, branchRef(pc.pr.HeadBranch), commit, pc.pr.HeadSHA); err != nil {
		if errors.Is(err, git.ErrRefChanged) {
			return nil, apierr.Conflict("%s changed while updating; try again", pc.pr.HeadBranch)
		}
		return nil, err
	}
	old := pc.pr.HeadSHA
	if err := a.s.setHead(ctx, pc.g, pc.pr, pc.issue.Number, user, commit, evBranchUpdated); err != nil {
		return nil, err
	}
	// Other pull requests from the same branch, and pushed_at.
	if err := a.s.afterServerPush(ctx, pc.repo, user, []githook.Update{{Ref: branchRef(pc.pr.HeadBranch), Old: old, New: commit}}); err != nil {
		return nil, err
	}
	pc, err = a.s.loadPullIn(ctx, a.s.db, pc.repo, pc.perm, pc.issue.Number)
	if err != nil {
		return nil, err
	}
	out, err := a.s.toAPIPull(ctx, pc, a.s.users())
	if err != nil {
		return nil, err
	}
	return apigen.UpdatePullBranch200JSONResponse(out), nil
}

func (a *api) DeleteBranch(ctx context.Context, req apigen.DeleteBranchRequestObject) (apigen.DeleteBranchResponseObject, error) {
	repo, perm, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	if req.Body == nil || req.Body.Name == "" {
		return nil, apierr.Validation("name is required")
	}
	if !git.ValidRefName(req.Body.Name) {
		return nil, apierr.NotFound("branch %q not found", req.Body.Name)
	}
	if err := a.s.deleteBranch(ctx, repo, perm, mustPrincipal(ctx).User, req.Body.Name, deref(req.Body.Sha)); err != nil {
		return nil, err
	}
	return apigen.DeleteBranch204Response{}, nil
}
