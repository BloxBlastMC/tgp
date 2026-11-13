package server

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// Issue event types (pull request ones are in pulls.go).
const (
	evLabeled      = "labeled"
	evUnlabeled    = "unlabeled"
	evMilestoned   = "milestoned"
	evDemilestoned = "demilestoned"
	evAssigned     = "assigned"
	evUnassigned   = "unassigned"
	maxAssignees   = 10
)

// ---- loading issues ----

type issueCtx struct {
	repo  *model.Repo
	perm  authz.Perm
	issue *model.Issue
}

func (s *Server) loadIssue(ctx context.Context, repoName string, number int64, need authz.Perm) (*issueCtx, error) {
	repo, perm, err := s.repoFor(ctx, repoName, need)
	if err != nil {
		return nil, err
	}
	issue := new(model.Issue)
	err = s.db.NewSelect().Model(issue).Where("repo_id = ? AND number = ?", repo.ID, number).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.NotFound("issue #%d not found in %s", number, repo.Name)
	}
	if err != nil {
		return nil, err
	}
	if issue.IsPull {
		return nil, apierr.NotFound("#%d is a pull request, not an issue", number).WithDetails(map[string]any{"is_pull": true})
	}
	return &issueCtx{repo: repo, perm: perm, issue: issue}, nil
}

// ---- labels, assignees, milestones attached to issues ----

func toAPILabel(l *model.Label) apigen.Label {
	return apigen.Label{Id: l.ID, Name: l.Name, Color: l.Color, Description: l.Description}
}

func toMilestoneRef(m *model.Milestone) *apigen.MilestoneRef {
	if m == nil {
		return nil
	}
	r := &apigen.MilestoneRef{Id: m.ID, Title: m.Title, State: apigen.MilestoneRefState(m.State)}
	if m.DueOn != nil {
		d := dateOf(*m.DueOn)
		r.DueOn = &d
	}
	return r
}

// issueMeta holds the labels, assignees and milestones of a set of issues.
type issueMeta struct {
	labels     map[int64][]apigen.Label
	assignees  map[int64][]int64
	milestones map[int64]*model.Milestone
}

func (s *Server) loadIssueMeta(ctx context.Context, issues []*model.Issue) (*issueMeta, error) {
	m := &issueMeta{labels: map[int64][]apigen.Label{}, assignees: map[int64][]int64{}, milestones: map[int64]*model.Milestone{}}
	if len(issues) == 0 {
		return m, nil
	}
	ids := make([]int64, len(issues))
	var msIDs []int64
	for i, is := range issues {
		ids[i] = is.ID
		if is.MilestoneID != nil {
			msIDs = append(msIDs, *is.MilestoneID)
		}
	}
	var rows []struct {
		IssueID int64 `bun:"issue_id"`
		model.Label
	}
	err := s.db.NewSelect().TableExpr("issue_labels AS il").ColumnExpr("il.issue_id, l.*").
		Join("JOIN labels AS l ON l.id = il.label_id").Where("il.issue_id IN (?)", bun.In(ids)).
		OrderExpr("l.lower_name").Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		m.labels[rows[i].IssueID] = append(m.labels[rows[i].IssueID], toAPILabel(&rows[i].Label))
	}
	var as []model.IssueAssignee
	if err := s.db.NewSelect().Model(&as).Where("issue_id IN (?)", bun.In(ids)).Order("user_id").Scan(ctx); err != nil {
		return nil, err
	}
	for _, a := range as {
		m.assignees[a.IssueID] = append(m.assignees[a.IssueID], a.UserID)
	}
	if len(msIDs) > 0 {
		var ms []model.Milestone
		if err := s.db.NewSelect().Model(&ms).Where("m.id IN (?)", bun.In(msIDs)).Scan(ctx); err != nil {
			return nil, err
		}
		for i := range ms {
			m.milestones[ms[i].ID] = &ms[i]
		}
	}
	return m, nil
}

func (m *issueMeta) labelsOf(id int64) []apigen.Label {
	if l := m.labels[id]; l != nil {
		return l
	}
	return []apigen.Label{}
}

func (m *issueMeta) assigneesOf(ctx context.Context, uc *userCache, id int64) []apigen.UserSummary {
	out := []apigen.UserSummary{}
	_ = uc.load(ctx, m.assignees[id]...)
	for _, u := range m.assignees[id] {
		out = append(out, uc.summary(ctx, u))
	}
	return out
}

func (m *issueMeta) milestoneOf(is *model.Issue) *apigen.MilestoneRef {
	if is.MilestoneID == nil {
		return nil
	}
	return toMilestoneRef(m.milestones[*is.MilestoneID])
}

// metaChange is a requested change to an issue's labels, assignees and
// milestone. nil fields mean "leave alone".
type metaChange struct {
	labels, addLabels, removeLabels          *[]string
	assignees, addAssignees, removeAssignees *[]string
	milestone                                *int64 // 0 clears
}

func (c metaChange) empty() bool {
	return c.labels == nil && c.addLabels == nil && c.removeLabels == nil && c.assignees == nil &&
		c.addAssignees == nil && c.removeAssignees == nil && c.milestone == nil
}

func (s *Server) labelsByName(ctx context.Context, db bun.IDB, repo *model.Repo, names []string) ([]model.Label, error) {
	var out []model.Label
	seen := map[int64]bool{}
	for _, n := range names {
		l := new(model.Label)
		err := db.NewSelect().Model(l).Where("repo_id = ? AND lower_name = ?", repo.ID, strings.ToLower(strings.TrimSpace(n))).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierr.Validation("label %q doesn't exist in %s", n, repo.Name)
		}
		if err != nil {
			return nil, err
		}
		if !seen[l.ID] {
			seen[l.ID] = true
			out = append(out, *l)
		}
	}
	return out, nil
}

func (s *Server) assignableUsers(ctx context.Context, db bun.IDB, repo *model.Repo, names []string) ([]model.User, error) {
	var out []model.User
	seen := map[int64]bool{}
	for _, n := range names {
		u := new(model.User)
		err := db.NewSelect().Model(u).Where("username = ?", strings.ToLower(strings.TrimSpace(n))).Scan(ctx)
		if err != nil || u.Disabled {
			return nil, apierr.Validation("user %q doesn't exist", n)
		}
		perm, err := authz.RepoPerm(ctx, db, u, repo)
		if err != nil {
			return nil, err
		}
		if perm < authz.Triage {
			return nil, apierr.Validation("%s can't access %s, so they can't be assigned", u.Username, repo.Name)
		}
		if !seen[u.ID] {
			seen[u.ID] = true
			out = append(out, *u)
		}
	}
	return out, nil
}

// applyMeta changes an issue's (or pull request's) labels, assignees and
// milestone inside tx, recording an event for each change.
func (s *Server) applyMeta(ctx context.Context, tx bun.IDB, repo *model.Repo, issue *model.Issue, actor *model.User, c metaChange) error {
	if c.empty() {
		return nil
	}
	// Labels.
	if c.labels != nil || c.addLabels != nil || c.removeLabels != nil {
		var cur []model.Label
		if err := tx.NewSelect().Model(&cur).Where("l.id IN (SELECT label_id FROM issue_labels WHERE issue_id = ?)", issue.ID).Scan(ctx); err != nil {
			return err
		}
		want := map[int64]model.Label{}
		if c.labels != nil {
			ls, err := s.labelsByName(ctx, tx, repo, *c.labels)
			if err != nil {
				return err
			}
			for _, l := range ls {
				want[l.ID] = l
			}
		} else {
			for _, l := range cur {
				want[l.ID] = l
			}
		}
		if c.addLabels != nil {
			ls, err := s.labelsByName(ctx, tx, repo, *c.addLabels)
			if err != nil {
				return err
			}
			for _, l := range ls {
				want[l.ID] = l
			}
		}
		if c.removeLabels != nil {
			ls, err := s.labelsByName(ctx, tx, repo, *c.removeLabels)
			if err != nil {
				return err
			}
			for _, l := range ls {
				delete(want, l.ID)
			}
		}
		have := map[int64]bool{}
		for _, l := range cur {
			have[l.ID] = true
			if _, keep := want[l.ID]; !keep {
				if _, err := tx.NewDelete().Model((*model.IssueLabel)(nil)).Where("issue_id = ? AND label_id = ?", issue.ID, l.ID).Exec(ctx); err != nil {
					return err
				}
				if err := addEvent(ctx, tx, issue.ID, actor, evUnlabeled, map[string]any{"label": l.Name, "color": l.Color}); err != nil {
					return err
				}
			}
		}
		for _, id := range sortedKeysOf(want) {
			l := want[id]
			if have[l.ID] {
				continue
			}
			if _, err := tx.NewInsert().Model(&model.IssueLabel{IssueID: issue.ID, LabelID: l.ID}).Exec(ctx); err != nil {
				return err
			}
			if err := addEvent(ctx, tx, issue.ID, actor, evLabeled, map[string]any{"label": l.Name, "color": l.Color}); err != nil {
				return err
			}
		}
	}
	// Assignees.
	if c.assignees != nil || c.addAssignees != nil || c.removeAssignees != nil {
		var curIDs []int64
		if err := tx.NewSelect().Model((*model.IssueAssignee)(nil)).Column("user_id").Where("issue_id = ?", issue.ID).Scan(ctx, &curIDs); err != nil {
			return err
		}
		names := map[int64]string{}
		want := map[int64]bool{}
		if c.assignees != nil {
			us, err := s.assignableUsers(ctx, tx, repo, *c.assignees)
			if err != nil {
				return err
			}
			for _, u := range us {
				want[u.ID], names[u.ID] = true, u.Username
			}
		} else {
			for _, id := range curIDs {
				want[id] = true
			}
		}
		if c.addAssignees != nil {
			us, err := s.assignableUsers(ctx, tx, repo, *c.addAssignees)
			if err != nil {
				return err
			}
			for _, u := range us {
				want[u.ID], names[u.ID] = true, u.Username
			}
		}
		if c.removeAssignees != nil {
			for _, n := range *c.removeAssignees {
				u := new(model.User)
				if err := tx.NewSelect().Model(u).Where("username = ?", strings.ToLower(strings.TrimSpace(n))).Scan(ctx); err == nil {
					delete(want, u.ID)
				}
			}
		}
		if len(want) > maxAssignees {
			return apierr.Validation("at most %d assignees", maxAssignees)
		}
		uc := s.users()
		have := map[int64]bool{}
		for _, id := range curIDs {
			have[id] = true
			if !want[id] {
				if _, err := tx.NewDelete().Model((*model.IssueAssignee)(nil)).Where("issue_id = ? AND user_id = ?", issue.ID, id).Exec(ctx); err != nil {
					return err
				}
				if err := addEvent(ctx, tx, issue.ID, actor, evUnassigned, map[string]any{"assignee": uc.summary(ctx, id).Username}); err != nil {
					return err
				}
			}
		}
		for _, id := range sortedKeysOfBool(want) {
			if have[id] {
				continue
			}
			if _, err := tx.NewInsert().Model(&model.IssueAssignee{IssueID: issue.ID, UserID: id}).Exec(ctx); err != nil {
				return err
			}
			name := names[id]
			if name == "" {
				name = uc.summary(ctx, id).Username
			}
			if err := addEvent(ctx, tx, issue.ID, actor, evAssigned, map[string]any{"assignee": name}); err != nil {
				return err
			}
		}
	}
	// Milestone.
	if c.milestone != nil {
		var newID *int64
		var newMS *model.Milestone
		if *c.milestone != 0 {
			newMS = new(model.Milestone)
			err := tx.NewSelect().Model(newMS).Where("m.id = ? AND m.repo_id = ?", *c.milestone, repo.ID).Scan(ctx)
			if err != nil {
				return apierr.Validation("milestone %d doesn't exist in %s", *c.milestone, repo.Name)
			}
			newID = &newMS.ID
		}
		oldID := issue.MilestoneID
		if (oldID == nil) != (newID == nil) || (oldID != nil && *oldID != *newID) {
			if oldID != nil {
				old := new(model.Milestone)
				if err := tx.NewSelect().Model(old).Where("m.id = ?", *oldID).Scan(ctx); err == nil {
					if err := addEvent(ctx, tx, issue.ID, actor, evDemilestoned, map[string]any{"milestone": old.Title}); err != nil {
						return err
					}
				}
			}
			if newMS != nil {
				if err := addEvent(ctx, tx, issue.ID, actor, evMilestoned, map[string]any{"milestone": newMS.Title}); err != nil {
					return err
				}
			}
			issue.MilestoneID = newID
			if _, err := tx.NewUpdate().Model(issue).Column("milestone_id").WherePK().Exec(ctx); err != nil {
				return err
			}
		}
	}
	return touchIssue(ctx, tx, issue.ID)
}

func sortedKeysOf(m map[int64]model.Label) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortedKeysOfBool(m map[int64]bool) []int64 {
	out := make([]int64, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ---- issue responses ----

func (s *Server) toIssueSummaries(ctx context.Context, issues []*model.Issue) ([]apigen.IssueSummary, error) {
	out := make([]apigen.IssueSummary, 0, len(issues))
	if len(issues) == 0 {
		return out, nil
	}
	meta, err := s.loadIssueMeta(ctx, issues)
	if err != nil {
		return nil, err
	}
	repoNames := map[int64]string{}
	uc := s.users()
	var ids []int64
	for _, is := range issues {
		ids = append(ids, is.AuthorID)
		ids = append(ids, meta.assignees[is.ID]...)
		if _, ok := repoNames[is.RepoID]; !ok {
			repoNames[is.RepoID] = ""
		}
	}
	if err := uc.load(ctx, ids...); err != nil {
		return nil, err
	}
	var repos []model.Repo
	if err := s.db.NewSelect().Model(&repos).Where("r.id IN (?)", bun.In(keysInt(repoNames))).Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range repos {
		repoNames[r.ID] = r.Name
	}
	for _, is := range issues {
		sum := apigen.IssueSummary{
			Number: is.Number, Repo: repoNames[is.RepoID], Title: is.Title, State: apigen.IssueSummaryState(is.State),
			Author: uc.summary(ctx, is.AuthorID), Labels: meta.labelsOf(is.ID), Assignees: meta.assigneesOf(ctx, uc, is.ID),
			Milestone: meta.milestoneOf(is), Comments: is.CommentCount, CreatedAt: is.CreatedAt, UpdatedAt: is.UpdatedAt, ClosedAt: is.ClosedAt,
		}
		if is.State == model.StateClosed && is.StateReason != "" {
			r := apigen.IssueStateReason(is.StateReason)
			sum.StateReason = &r
		}
		out = append(out, sum)
	}
	return out, nil
}

func keysInt(m map[int64]string) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func canEditIssue(ic *issueCtx, u *model.User) bool {
	return ic.perm >= authz.Write || (ic.perm >= authz.Triage && ic.issue.AuthorID == u.ID)
}

func (s *Server) toAPIIssue(ctx context.Context, ic *issueCtx) (apigen.Issue, error) {
	sums, err := s.toIssueSummaries(ctx, []*model.Issue{ic.issue})
	if err != nil {
		return apigen.Issue{}, err
	}
	sum := sums[0]
	html, err := renderMarkdown(ic.issue.Body, markdownCtx(ic.repo, ic.repo.DefaultBranch))
	if err != nil {
		return apigen.Issue{}, err
	}
	uc := s.users()
	return apigen.Issue{
		Number: sum.Number, Repo: sum.Repo, Title: sum.Title, State: apigen.IssueState(sum.State), StateReason: sum.StateReason,
		Author: sum.Author, Labels: sum.Labels, Assignees: sum.Assignees, Milestone: sum.Milestone, Comments: sum.Comments,
		CreatedAt: sum.CreatedAt, UpdatedAt: sum.UpdatedAt, ClosedAt: sum.ClosedAt,
		Body: ic.issue.Body, BodyHtml: html, ClosedBy: uc.summaryPtr(ctx, ic.issue.ClosedByID),
		ViewerPermission: apigen.Permission(ic.perm.String()), CanEdit: canEditIssue(ic, mustPrincipal(ctx).User),
	}, nil
}

// ---- issue filters ----

// labelFilter requires every comma-separated label.
func labelFilter(q *bun.SelectQuery, repoID int64, labels string) *bun.SelectQuery {
	for _, name := range strings.Split(labels, ",") {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			q = q.Where("i.id IN (SELECT il.issue_id FROM issue_labels AS il JOIN labels AS l ON l.id = il.label_id WHERE l.repo_id = ? AND l.lower_name = ?)", repoID, name)
		}
	}
	return q
}

func (s *Server) listIssueRows(ctx context.Context, q *bun.SelectQuery, order string, limit, offset int) ([]*model.Issue, int, error) {
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	var issues []*model.Issue
	if err := q.OrderExpr(order).Order("i.id DESC").Limit(int64(limit)).Offset(int64(offset)).Scan(ctx, &issues); err != nil {
		return nil, 0, err
	}
	return issues, int(total), nil
}

func issueStateFilter(q *bun.SelectQuery, state string) *bun.SelectQuery {
	switch state {
	case "", "open":
		return q.Where("i.state = ?", model.StateOpen)
	case "closed":
		return q.Where("i.state = ?", model.StateClosed)
	}
	return q
}

func (a *api) ListIssues(ctx context.Context, req apigen.ListIssuesRequestObject) (apigen.ListIssuesResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	p := req.Params
	q := a.s.db.NewSelect().Model((*model.Issue)(nil)).Where("i.repo_id = ? AND NOT i.is_pull", repo.ID)
	q = issueStateFilter(q, string(deref(p.State)))
	if v := deref(p.Labels); v != "" {
		q = labelFilter(q, repo.ID, v)
	}
	switch v := deref(p.Milestone); v {
	case "":
	case "none":
		q = q.Where("i.milestone_id IS NULL")
	case "*":
		q = q.Where("i.milestone_id IS NOT NULL")
	default:
		q = q.Where("i.milestone_id = (SELECT id FROM milestones WHERE repo_id = ? AND lower_title = ?)", repo.ID, strings.ToLower(v))
	}
	switch v := deref(p.Assignee); v {
	case "":
	case "none":
		q = q.Where("NOT EXISTS (SELECT 1 FROM issue_assignees AS ia WHERE ia.issue_id = i.id)")
	case "*":
		q = q.Where("EXISTS (SELECT 1 FROM issue_assignees AS ia WHERE ia.issue_id = i.id)")
	default:
		q = q.Where("i.id IN (SELECT issue_id FROM issue_assignees WHERE user_id = (SELECT id FROM users WHERE username = ?))", strings.ToLower(v))
	}
	if v := deref(p.Author); v != "" {
		q = q.Where("i.author_id = (SELECT id FROM users WHERE username = ?)", strings.ToLower(v))
	}
	if v := deref(p.Q); v != "" {
		q = q.Where("LOWER(i.title) LIKE ?", "%"+escapeLike(strings.ToLower(v))+"%")
	}
	order := "i.updated_at DESC"
	switch deref(p.Sort) {
	case apigen.ListIssuesParamsSortCreated:
		order = "i.created_at DESC"
	case apigen.ListIssuesParamsSortComments:
		order = "i.comment_count DESC"
	}
	limit, offset := pageParams(p.Page, p.PerPage)
	issues, total, err := a.s.listIssueRows(ctx, q, order, limit, offset)
	if err != nil {
		return nil, err
	}
	sums, err := a.s.toIssueSummaries(ctx, issues)
	if err != nil {
		return nil, err
	}
	return apigen.ListIssues200JSONResponse{Issues: sums, Total: total}, nil
}

func (a *api) ListMyIssues(ctx context.Context, req apigen.ListMyIssuesRequestObject) (apigen.ListMyIssuesResponseObject, error) {
	u := mustPrincipal(ctx).User
	p := req.Params
	q := a.s.db.NewSelect().Model((*model.Issue)(nil)).Join("JOIN repos AS r ON r.id = i.repo_id").Where("NOT i.is_pull")
	q = authz.VisibleRepos(q, u)
	q = issueStateFilter(q, string(deref(p.State)))
	switch deref(p.Filter) {
	case "", apigen.ListMyIssuesParamsFilterAssigned:
		q = q.Where("i.id IN (SELECT issue_id FROM issue_assignees WHERE user_id = ?)", u.ID)
	case apigen.ListMyIssuesParamsFilterCreated:
		q = q.Where("i.author_id = ?", u.ID)
	}
	limit, offset := pageParams(p.Page, p.PerPage)
	issues, total, err := a.s.listIssueRows(ctx, q, "i.updated_at DESC", limit, offset)
	if err != nil {
		return nil, err
	}
	sums, err := a.s.toIssueSummaries(ctx, issues)
	if err != nil {
		return nil, err
	}
	return apigen.ListMyIssues200JSONResponse{Issues: sums, Total: total}, nil
}

// ---- issue create / get / update ----

func (a *api) CreateIssue(ctx context.Context, req apigen.CreateIssueRequestObject) (apigen.CreateIssueResponseObject, error) {
	repo, perm, err := a.s.repoFor(ctx, req.Repo, authz.Triage)
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
	title, err := validateTitle(b.Title)
	if err != nil {
		return nil, err
	}
	body := deref(b.Body)
	if err := validateBody(body, maxBody); err != nil {
		return nil, err
	}
	user := mustPrincipal(ctx).User
	issue := &model.Issue{RepoID: repo.ID, Title: title, Body: body, AuthorID: user.ID, State: model.StateOpen}
	err = a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		n, err := nextNumber(ctx, tx, repo.ID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		issue.Number, issue.CreatedAt, issue.UpdatedAt = n, now, now
		if _, err := tx.NewInsert().Model(issue).Exec(ctx); err != nil {
			return err
		}
		return a.s.applyMeta(ctx, tx, repo, issue, user, metaChange{addLabels: b.Labels, addAssignees: b.Assignees, milestone: b.Milestone})
	})
	if err != nil {
		return nil, err
	}
	// Labels etc. set at creation aren't news: drop those events.
	if _, err := a.s.db.NewDelete().Model((*model.IssueEvent)(nil)).Where("issue_id = ?", issue.ID).Exec(ctx); err != nil {
		return nil, err
	}
	out, err := a.s.toAPIIssue(ctx, &issueCtx{repo: repo, perm: perm, issue: issue})
	if err != nil {
		return nil, err
	}
	return apigen.CreateIssue201JSONResponse(out), nil
}

func (a *api) GetIssue(ctx context.Context, req apigen.GetIssueRequestObject) (apigen.GetIssueResponseObject, error) {
	ic, err := a.s.loadIssue(ctx, req.Repo, req.Number, authz.Read)
	if err != nil {
		return nil, err
	}
	out, err := a.s.toAPIIssue(ctx, ic)
	if err != nil {
		return nil, err
	}
	return apigen.GetIssue200JSONResponse(out), nil
}

func (a *api) UpdateIssue(ctx context.Context, req apigen.UpdateIssueRequestObject) (apigen.UpdateIssueResponseObject, error) {
	ic, err := a.s.loadIssue(ctx, req.Repo, req.Number, authz.Triage)
	if err != nil {
		return nil, err
	}
	if ic.repo.Archived {
		return nil, apierr.Forbidden("%s is archived", ic.repo.Name)
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	b := req.Body
	user := mustPrincipal(ctx).User
	if (b.Title != nil || b.Body != nil) && !canEditIssue(ic, user) {
		return nil, apierr.Forbidden("only the author or someone with write access can edit the title and description")
	}
	err = a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		issue := ic.issue
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
		reason := string(deref(b.StateReason))
		if reason != "" && reason != "completed" && reason != "not_planned" {
			return apierr.Validation("state_reason must be completed or not_planned")
		}
		if b.State != nil {
			switch {
			case *b.State == apigen.UpdateIssueStateClosed && issue.State == model.StateOpen:
				if reason == "" {
					reason = "completed"
				}
				if err := closeIssue(ctx, tx, issue, user, reason, nil); err != nil {
					return err
				}
			case *b.State == apigen.UpdateIssueStateOpen && issue.State == model.StateClosed:
				issue.State, issue.ClosedAt, issue.ClosedByID, issue.StateReason = model.StateOpen, nil, nil, ""
				if err := addEvent(ctx, tx, issue.ID, user, evReopened, nil); err != nil {
					return err
				}
			}
		} else if reason != "" && issue.State == model.StateClosed {
			issue.StateReason = reason
		}
		issue.UpdatedAt = time.Now().UTC()
		if _, err := tx.NewUpdate().Model(issue).Column("title", "body", "state", "state_reason", "closed_at", "closed_by_id", "updated_at").WherePK().Exec(ctx); err != nil {
			return err
		}
		return a.s.applyMeta(ctx, tx, ic.repo, issue, user, metaChange{
			labels: b.Labels, addLabels: b.AddLabels, removeLabels: b.RemoveLabels,
			assignees: b.Assignees, addAssignees: b.AddAssignees, removeAssignees: b.RemoveAssignees, milestone: b.Milestone,
		})
	})
	if err != nil {
		return nil, err
	}
	out, err := a.s.toAPIIssue(ctx, ic)
	if err != nil {
		return nil, err
	}
	return apigen.UpdateIssue200JSONResponse(out), nil
}

// closeIssue closes issue (the caller saves it) and records why.
func closeIssue(ctx context.Context, tx bun.IDB, issue *model.Issue, actor *model.User, reason string, extra map[string]any) error {
	now := time.Now().UTC()
	issue.State, issue.ClosedAt, issue.StateReason = model.StateClosed, &now, reason
	issue.ClosedByID = nil
	if actor != nil {
		issue.ClosedByID = &actor.ID
	}
	data := map[string]any{"reason": reason}
	for k, v := range extra {
		data[k] = v
	}
	return addEvent(ctx, tx, issue.ID, actor, evClosed, data)
}

// ---- issue comments and timeline ----

func (a *api) ListIssueComments(ctx context.Context, req apigen.ListIssueCommentsRequestObject) (apigen.ListIssueCommentsResponseObject, error) {
	ic, err := a.s.loadIssue(ctx, req.Repo, req.Number, authz.Read)
	if err != nil {
		return nil, err
	}
	cs, err := a.s.loadComments(ctx, ic.issue.ID)
	if err != nil {
		return nil, err
	}
	v := a.s.issueCommentView(ic)
	out := make(apigen.ListIssueComments200JSONResponse, 0, len(cs))
	for i := range cs {
		c, err := v.toAPI(ctx, &cs[i])
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// issueCommentView renders comments of an issue (no diff anchoring).
func (s *Server) issueCommentView(ic *issueCtx) *commentView {
	return s.commentView(&pullCtx{repo: ic.repo, perm: ic.perm, issue: ic.issue}, s.users())
}

func (a *api) CreateIssueComment(ctx context.Context, req apigen.CreateIssueCommentRequestObject) (apigen.CreateIssueCommentResponseObject, error) {
	ic, err := a.s.loadIssue(ctx, req.Repo, req.Number, authz.Triage)
	if err != nil {
		return nil, err
	}
	if ic.repo.Archived {
		return nil, apierr.Forbidden("%s is archived", ic.repo.Name)
	}
	if req.Body == nil || strings.TrimSpace(req.Body.Body) == "" {
		return nil, apierr.Validation("comment body is required")
	}
	body := strings.TrimSpace(req.Body.Body)
	if err := validateBody(body, maxComment); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	c := &model.Comment{IssueID: ic.issue.ID, AuthorID: mustPrincipal(ctx).User.ID, Body: body, CreatedAt: now, UpdatedAt: now}
	if err := a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return a.s.insertComments(ctx, tx, ic.issue.ID, c)
	}); err != nil {
		return nil, err
	}
	out, err := a.s.issueCommentView(ic).toAPI(ctx, c)
	if err != nil {
		return nil, err
	}
	return apigen.CreateIssueComment201JSONResponse(out), nil
}

func (a *api) GetIssueTimeline(ctx context.Context, req apigen.GetIssueTimelineRequestObject) (apigen.GetIssueTimelineResponseObject, error) {
	ic, err := a.s.loadIssue(ctx, req.Repo, req.Number, authz.Read)
	if err != nil {
		return nil, err
	}
	items, err := a.s.timeline(ctx, &pullCtx{repo: ic.repo, perm: ic.perm, issue: ic.issue})
	if err != nil {
		return nil, err
	}
	return apigen.GetIssueTimeline200JSONResponse(items), nil
}

// ---- closing keywords ----

var closingRe = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s*:?\s+#([0-9]{1,9})\b`)

// closingRefs returns the issue numbers a text says it closes ("Fixes #12").
func closingRefs(text string) []int64 {
	var out []int64
	seen := map[int64]bool{}
	for _, m := range closingRe.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.ParseInt(m[1], 10, 64)
		if n > 0 && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// closeReferencedIssues closes open issues that texts reference with a
// closing keyword. extra is recorded on each close event (pull, commit).
func (s *Server) closeReferencedIssues(ctx context.Context, repo *model.Repo, actor *model.User, text string, extra map[string]any) error {
	nums := closingRefs(text)
	if len(nums) == 0 {
		return nil
	}
	var issues []*model.Issue
	if err := s.db.NewSelect().Model(&issues).Where("repo_id = ? AND number IN (?) AND NOT is_pull AND state = ?", repo.ID, bun.In(nums), model.StateOpen).Scan(ctx); err != nil {
		return err
	}
	for _, is := range issues {
		err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			if err := closeIssue(ctx, tx, is, actor, "completed", extra); err != nil {
				return err
			}
			is.UpdatedAt = time.Now().UTC()
			_, err := tx.NewUpdate().Model(is).Column("state", "state_reason", "closed_at", "closed_by_id", "updated_at").WherePK().Exec(ctx)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// closeIssuesFromPush closes issues named by closing keywords in commits
// pushed to the default branch.
func (s *Server) closeIssuesFromPush(ctx context.Context, repo *model.Repo, actor *model.User, old, new string) error {
	g := s.repos.Open(repo)
	commits, err := g.LogRange(ctx, old, new, 200)
	if err != nil {
		return err
	}
	for _, c := range commits {
		if err := s.closeReferencedIssues(ctx, repo, actor, c.Subject+"\n"+c.Body, map[string]any{"commit": c.SHA}); err != nil {
			return err
		}
	}
	return nil
}

// ---- labels ----

var colorRe = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)

func normalizeLabel(name, color string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 50 || strings.Contains(name, ",") {
		return "", "", apierr.Validation("label name must be 1-50 characters without commas")
	}
	color = strings.TrimPrefix(strings.TrimSpace(color), "#")
	if !colorRe.MatchString(color) {
		return "", "", apierr.Validation("color must be six hex digits, e.g. d73a4a")
	}
	return name, strings.ToLower(color), nil
}

func (a *api) ListLabels(ctx context.Context, req apigen.ListLabelsRequestObject) (apigen.ListLabelsResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	var ls []model.Label
	if err := a.s.db.NewSelect().Model(&ls).Where("repo_id = ?", repo.ID).Order("lower_name").Scan(ctx); err != nil {
		return nil, err
	}
	var counts []struct {
		LabelID int64 `bun:"label_id"`
		N       int   `bun:"n"`
	}
	err = a.s.db.NewSelect().TableExpr("issue_labels AS il").ColumnExpr("il.label_id, COUNT(*) AS n").
		Join("JOIN issues AS i ON i.id = il.issue_id").Where("i.repo_id = ? AND i.state = ?", repo.ID, model.StateOpen).
		GroupExpr("il.label_id").Scan(ctx, &counts)
	if err != nil {
		return nil, err
	}
	byID := map[int64]int{}
	for _, c := range counts {
		byID[c.LabelID] = c.N
	}
	out := make(apigen.ListLabels200JSONResponse, 0, len(ls))
	for i := range ls {
		l := toAPILabel(&ls[i])
		n := byID[ls[i].ID]
		l.OpenIssues = &n
		out = append(out, l)
	}
	return out, nil
}

func (a *api) CreateLabel(ctx context.Context, req apigen.CreateLabelRequestObject) (apigen.CreateLabelResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	name, color, err := normalizeLabel(req.Body.Name, req.Body.Color)
	if err != nil {
		return nil, err
	}
	l := &model.Label{RepoID: repo.ID, Name: name, LowerName: strings.ToLower(name), Color: color,
		Description: strings.TrimSpace(deref(req.Body.Description)), CreatedAt: time.Now().UTC()}
	if len(l.Description) > 200 {
		return nil, apierr.Validation("description is too long (max 200)")
	}
	if exists, err := a.s.db.NewSelect().Model((*model.Label)(nil)).Where("repo_id = ? AND lower_name = ?", repo.ID, l.LowerName).Exists(ctx); err != nil {
		return nil, err
	} else if exists {
		return nil, apierr.Conflict("label %q already exists", name)
	}
	if _, err := a.s.db.NewInsert().Model(l).Exec(ctx); err != nil {
		return nil, err
	}
	return apigen.CreateLabel201JSONResponse(toAPILabel(l)), nil
}

func (s *Server) loadLabel(ctx context.Context, repo *model.Repo, id int64) (*model.Label, error) {
	l := new(model.Label)
	err := s.db.NewSelect().Model(l).Where("l.id = ? AND l.repo_id = ?", id, repo.ID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.NotFound("label %d not found", id)
	}
	return l, err
}

func (a *api) UpdateLabel(ctx context.Context, req apigen.UpdateLabelRequestObject) (apigen.UpdateLabelResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	l, err := a.s.loadLabel(ctx, repo, req.Id)
	if err != nil {
		return nil, err
	}
	name, color := l.Name, l.Color
	if req.Body.Name != nil {
		name = *req.Body.Name
	}
	if req.Body.Color != nil {
		color = *req.Body.Color
	}
	if name, color, err = normalizeLabel(name, color); err != nil {
		return nil, err
	}
	if !strings.EqualFold(name, l.Name) {
		if exists, err := a.s.db.NewSelect().Model((*model.Label)(nil)).Where("repo_id = ? AND lower_name = ?", repo.ID, strings.ToLower(name)).Exists(ctx); err != nil {
			return nil, err
		} else if exists {
			return nil, apierr.Conflict("label %q already exists", name)
		}
	}
	l.Name, l.LowerName, l.Color = name, strings.ToLower(name), color
	if req.Body.Description != nil {
		l.Description = strings.TrimSpace(*req.Body.Description)
	}
	if _, err := a.s.db.NewUpdate().Model(l).Column("name", "lower_name", "color", "description").WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	return apigen.UpdateLabel200JSONResponse(toAPILabel(l)), nil
}

func (a *api) DeleteLabel(ctx context.Context, req apigen.DeleteLabelRequestObject) (apigen.DeleteLabelResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	l, err := a.s.loadLabel(ctx, repo, req.Id)
	if err != nil {
		return nil, err
	}
	err = a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().Model((*model.IssueLabel)(nil)).Where("label_id = ?", l.ID).Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewDelete().Model(l).WherePK().Exec(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.DeleteLabel204Response{}, nil
}

// ---- milestones ----

func dateOf(t time.Time) openapi_types.Date {
	return openapi_types.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)}
}

type msCounts struct{ open, closed int }

func (s *Server) milestoneCounts(ctx context.Context, ids []int64) (map[int64]msCounts, error) {
	out := map[int64]msCounts{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		MilestoneID int64  `bun:"milestone_id"`
		State       string `bun:"state"`
		N           int    `bun:"n"`
	}
	err := s.db.NewSelect().TableExpr("issues AS i").ColumnExpr("i.milestone_id, i.state, COUNT(*) AS n").
		Where("i.milestone_id IN (?)", bun.In(ids)).GroupExpr("i.milestone_id, i.state").Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		c := out[r.MilestoneID]
		if r.State == model.StateOpen {
			c.open += r.N
		} else {
			c.closed += r.N
		}
		out[r.MilestoneID] = c
	}
	return out, nil
}

func (s *Server) toAPIMilestones(ctx context.Context, repo *model.Repo, ms []model.Milestone) ([]apigen.Milestone, error) {
	ids := make([]int64, len(ms))
	for i := range ms {
		ids[i] = ms[i].ID
	}
	counts, err := s.milestoneCounts(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]apigen.Milestone, 0, len(ms))
	for i := range ms {
		m := &ms[i]
		html, err := renderMarkdown(m.Description, markdownCtx(repo, repo.DefaultBranch))
		if err != nil {
			return nil, err
		}
		am := apigen.Milestone{Id: m.ID, Title: m.Title, Description: m.Description, DescriptionHtml: html,
			State: apigen.MilestoneState(m.State), OpenIssues: counts[m.ID].open, ClosedIssues: counts[m.ID].closed,
			ClosedAt: m.ClosedAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
		if m.DueOn != nil {
			d := dateOf(*m.DueOn)
			am.DueOn = &d
		}
		out = append(out, am)
	}
	return out, nil
}

func (a *api) ListMilestones(ctx context.Context, req apigen.ListMilestonesRequestObject) (apigen.ListMilestonesResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	q := a.s.db.NewSelect().Model((*model.Milestone)(nil)).Where("m.repo_id = ?", repo.ID)
	switch deref(req.Params.State) {
	case "", apigen.ListMilestonesParamsStateOpen:
		q = q.Where("m.state = ?", model.StateOpen)
	case apigen.ListMilestonesParamsStateClosed:
		q = q.Where("m.state = ?", model.StateClosed)
	}
	var ms []model.Milestone
	// Open first, then by due date (undated last), then title.
	if err := q.OrderExpr("CASE WHEN m.state = 'open' THEN 0 ELSE 1 END, CASE WHEN m.due_on IS NULL THEN 1 ELSE 0 END, m.due_on, m.lower_title").Scan(ctx, &ms); err != nil {
		return nil, err
	}
	out, err := a.s.toAPIMilestones(ctx, repo, ms)
	if err != nil {
		return nil, err
	}
	return apigen.ListMilestones200JSONResponse(out), nil
}

func validateMilestoneTitle(t string) (string, error) {
	t = strings.TrimSpace(t)
	if t == "" || len(t) > 100 {
		return "", apierr.Validation("milestone title must be 1-100 characters")
	}
	if strings.EqualFold(t, "none") || t == "*" {
		return "", apierr.Validation("%q is reserved", t)
	}
	return t, nil
}

func (a *api) CreateMilestone(ctx context.Context, req apigen.CreateMilestoneRequestObject) (apigen.CreateMilestoneResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	title, err := validateMilestoneTitle(req.Body.Title)
	if err != nil {
		return nil, err
	}
	desc := deref(req.Body.Description)
	if err := validateBody(desc, maxComment); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	m := &model.Milestone{RepoID: repo.ID, Title: title, LowerTitle: strings.ToLower(title), Description: desc, State: model.StateOpen, CreatedAt: now, UpdatedAt: now}
	if req.Body.DueOn != nil {
		t := req.Body.DueOn.Time
		m.DueOn = &t
	}
	if exists, err := a.s.db.NewSelect().Model((*model.Milestone)(nil)).Where("repo_id = ? AND lower_title = ?", repo.ID, m.LowerTitle).Exists(ctx); err != nil {
		return nil, err
	} else if exists {
		return nil, apierr.Conflict("milestone %q already exists", title)
	}
	if _, err := a.s.db.NewInsert().Model(m).Exec(ctx); err != nil {
		return nil, err
	}
	out, err := a.s.toAPIMilestones(ctx, repo, []model.Milestone{*m})
	if err != nil {
		return nil, err
	}
	return apigen.CreateMilestone201JSONResponse(out[0]), nil
}

func (s *Server) loadMilestone(ctx context.Context, repo *model.Repo, id int64) (*model.Milestone, error) {
	m := new(model.Milestone)
	err := s.db.NewSelect().Model(m).Where("m.id = ? AND m.repo_id = ?", id, repo.ID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.NotFound("milestone %d not found", id)
	}
	return m, err
}

func (a *api) GetMilestone(ctx context.Context, req apigen.GetMilestoneRequestObject) (apigen.GetMilestoneResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	m, err := a.s.loadMilestone(ctx, repo, req.Id)
	if err != nil {
		return nil, err
	}
	out, err := a.s.toAPIMilestones(ctx, repo, []model.Milestone{*m})
	if err != nil {
		return nil, err
	}
	return apigen.GetMilestone200JSONResponse(out[0]), nil
}

func (a *api) UpdateMilestone(ctx context.Context, req apigen.UpdateMilestoneRequestObject) (apigen.UpdateMilestoneResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	m, err := a.s.loadMilestone(ctx, repo, req.Id)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b.Title != nil {
		t, err := validateMilestoneTitle(*b.Title)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(t, m.Title) {
			if exists, err := a.s.db.NewSelect().Model((*model.Milestone)(nil)).Where("repo_id = ? AND lower_title = ?", repo.ID, strings.ToLower(t)).Exists(ctx); err != nil {
				return nil, err
			} else if exists {
				return nil, apierr.Conflict("milestone %q already exists", t)
			}
		}
		m.Title, m.LowerTitle = t, strings.ToLower(t)
	}
	if b.Description != nil {
		if err := validateBody(*b.Description, maxComment); err != nil {
			return nil, err
		}
		m.Description = *b.Description
	}
	if deref(b.ClearDueOn) {
		m.DueOn = nil
	} else if b.DueOn != nil {
		t := b.DueOn.Time
		m.DueOn = &t
	}
	now := time.Now().UTC()
	if b.State != nil {
		switch string(*b.State) {
		case model.StateClosed:
			if m.State != model.StateClosed {
				m.State, m.ClosedAt = model.StateClosed, &now
			}
		case model.StateOpen:
			m.State, m.ClosedAt = model.StateOpen, nil
		default:
			return nil, apierr.Validation("state must be open or closed")
		}
	}
	m.UpdatedAt = now
	if _, err := a.s.db.NewUpdate().Model(m).Column("title", "lower_title", "description", "due_on", "state", "closed_at", "updated_at").WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	out, err := a.s.toAPIMilestones(ctx, repo, []model.Milestone{*m})
	if err != nil {
		return nil, err
	}
	return apigen.UpdateMilestone200JSONResponse(out[0]), nil
}

func (a *api) DeleteMilestone(ctx context.Context, req apigen.DeleteMilestoneRequestObject) (apigen.DeleteMilestoneResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	m, err := a.s.loadMilestone(ctx, repo, req.Id)
	if err != nil {
		return nil, err
	}
	err = a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewUpdate().Model((*model.Issue)(nil)).Set("milestone_id = NULL").Where("milestone_id = ?", m.ID).Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewDelete().Model(m).WherePK().Exec(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return apigen.DeleteMilestone204Response{}, nil
}
