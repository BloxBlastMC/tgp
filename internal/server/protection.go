package server

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// protectionRules loads a repository's rules.
func (s *Server) protectionRules(ctx context.Context, db bun.IDB, repoID int64) ([]model.BranchProtection, error) {
	var rules []model.BranchProtection
	err := db.NewSelect().Model(&rules).Where("repo_id = ?", repoID).Order("id").Scan(ctx)
	return rules, err
}

// matchRule picks the rule for branch: an exact pattern wins over globs,
// and among globs the longest (most specific) pattern wins.
func matchRule(rules []model.BranchProtection, branch string) *model.BranchProtection {
	var best *model.BranchProtection
	for i := range rules {
		r := &rules[i]
		if r.Pattern == branch {
			return r
		}
		if !strings.ContainsAny(r.Pattern, "*?[") {
			continue
		}
		if ok, _ := path.Match(r.Pattern, branch); ok && (best == nil || len(r.Pattern) > len(best.Pattern)) {
			best = r
		}
	}
	return best
}

func (s *Server) ruleFor(ctx context.Context, db bun.IDB, repoID int64, branch string) (*model.BranchProtection, error) {
	rules, err := s.protectionRules(ctx, db, repoID)
	if err != nil {
		return nil, err
	}
	return matchRule(rules, branch), nil
}

// checkProtectedPush returns a reason to reject a push to branch, or "".
func checkProtectedPush(rule *model.BranchProtection, branch string, isAdmin, isCreate, isDelete, forced bool) string {
	if rule == nil {
		return ""
	}
	bypass := isAdmin && rule.AdminsBypass
	switch {
	case isDelete:
		if !rule.AllowDeletion {
			return "branch " + branch + " is protected and can't be deleted"
		}
		return ""
	case isCreate:
		return "" // creating a branch that matches a rule is fine
	case forced && rule.BlockForcePush && !bypass:
		return "branch " + branch + " is protected: force pushes aren't allowed"
	case rule.RequirePullRequest && !bypass:
		return "branch " + branch + " is protected: changes must go through a pull request"
	}
	return ""
}

func toAPIProtection(r *model.BranchProtection) apigen.BranchProtection {
	return apigen.BranchProtection{
		Id: r.ID, Pattern: r.Pattern, RequirePullRequest: r.RequirePullRequest, RequiredApprovals: r.RequiredApprovals,
		DismissStaleApprovals: r.DismissStaleApprovals, RequireUpToDate: r.RequireUpToDate, BlockForcePush: r.BlockForcePush,
		AllowDeletion: r.AllowDeletion, AdminsBypass: r.AdminsBypass, RequiredChecks: ruleChecks(r), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func validatePattern(p string) error {
	if p == "" || len(p) > 255 || strings.HasPrefix(p, "-") || strings.HasPrefix(p, "refs/") {
		return apierr.Validation("pattern %q is invalid: use a branch name or a glob like release/*", p)
	}
	if _, err := path.Match(p, ""); err != nil {
		return apierr.Validation("pattern %q is not a valid glob: %v", p, err)
	}
	return nil
}

func validateApprovals(n int) error {
	if n < 0 || n > 10 {
		return apierr.Validation("required_approvals must be between 0 and 10")
	}
	return nil
}

func (a *api) ListProtections(ctx context.Context, req apigen.ListProtectionsRequestObject) (apigen.ListProtectionsResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	rules, err := a.s.protectionRules(ctx, a.s.db, repo.ID)
	if err != nil {
		return nil, err
	}
	out := make(apigen.ListProtections200JSONResponse, 0, len(rules))
	for i := range rules {
		out = append(out, toAPIProtection(&rules[i]))
	}
	return out, nil
}

func (s *Server) auditProtection(ctx context.Context, db bun.IDB, action string, repo *model.Repo, r *model.BranchProtection) error {
	_, hr := httpFrom(ctx)
	return audit.Record(ctx, db, audit.Entry{
		Actor: mustPrincipal(ctx).User, Action: action, TargetType: "repo", Target: repo.Name, IP: clientIP(hr),
		Details: map[string]any{"pattern": r.Pattern, "require_pull_request": r.RequirePullRequest,
			"required_approvals": r.RequiredApprovals, "admins_bypass": r.AdminsBypass, "allow_deletion": r.AllowDeletion,
			"required_checks": ruleChecks(r)},
	})
}

func (a *api) CreateProtection(ctx context.Context, req apigen.CreateProtectionRequestObject) (apigen.CreateProtectionResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Admin)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	b := req.Body
	pattern := strings.TrimSpace(b.Pattern)
	if err := validatePattern(pattern); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	rule := &model.BranchProtection{
		RepoID: repo.ID, Pattern: pattern,
		RequirePullRequest:    boolOr(b.RequirePullRequest, true),
		RequiredApprovals:     intOr(b.RequiredApprovals, 1),
		DismissStaleApprovals: boolOr(b.DismissStaleApprovals, true),
		RequireUpToDate:       boolOr(b.RequireUpToDate, false),
		BlockForcePush:        boolOr(b.BlockForcePush, true),
		AllowDeletion:         boolOr(b.AllowDeletion, false),
		AdminsBypass:          boolOr(b.AdminsBypass, false),
		RequiredChecks:        "[]",
		CreatedAt:             now, UpdatedAt: now,
	}
	if err := validateApprovals(rule.RequiredApprovals); err != nil {
		return nil, err
	}
	if b.RequiredChecks != nil {
		checks, err := cleanChecks(*b.RequiredChecks)
		if err != nil {
			return nil, err
		}
		rule.RequiredChecks = mustJSON(checks)
	}
	err = a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		exists, err := tx.NewSelect().Model((*model.BranchProtection)(nil)).Where("repo_id = ? AND pattern = ?", repo.ID, pattern).Exists(ctx)
		if err != nil {
			return err
		}
		if exists {
			return apierr.Conflict("a rule for %q already exists", pattern)
		}
		if _, err := tx.NewInsert().Model(rule).Exec(ctx); err != nil {
			return err
		}
		return a.s.auditProtection(ctx, tx, "protection.create", repo, rule)
	})
	if err != nil {
		return nil, err
	}
	return apigen.CreateProtection201JSONResponse(toAPIProtection(rule)), nil
}

func (s *Server) loadRule(ctx context.Context, db bun.IDB, repo *model.Repo, id int64) (*model.BranchProtection, error) {
	rule := new(model.BranchProtection)
	err := db.NewSelect().Model(rule).Where("id = ? AND repo_id = ?", id, repo.ID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apierr.NotFound("protection rule %d not found", id)
	}
	return rule, err
}

func (a *api) UpdateProtection(ctx context.Context, req apigen.UpdateProtectionRequestObject) (apigen.UpdateProtectionResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Admin)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	var rule *model.BranchProtection
	err = a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if rule, err = a.s.loadRule(ctx, tx, repo, req.Id); err != nil {
			return err
		}
		b := req.Body
		if b.Pattern != nil {
			p := strings.TrimSpace(*b.Pattern)
			if err := validatePattern(p); err != nil {
				return err
			}
			if p != rule.Pattern {
				exists, err := tx.NewSelect().Model((*model.BranchProtection)(nil)).Where("repo_id = ? AND pattern = ?", repo.ID, p).Exists(ctx)
				if err != nil {
					return err
				}
				if exists {
					return apierr.Conflict("a rule for %q already exists", p)
				}
			}
			rule.Pattern = p
		}
		set := func(dst *bool, v *bool) {
			if v != nil {
				*dst = *v
			}
		}
		set(&rule.RequirePullRequest, b.RequirePullRequest)
		set(&rule.DismissStaleApprovals, b.DismissStaleApprovals)
		set(&rule.RequireUpToDate, b.RequireUpToDate)
		set(&rule.BlockForcePush, b.BlockForcePush)
		set(&rule.AllowDeletion, b.AllowDeletion)
		set(&rule.AdminsBypass, b.AdminsBypass)
		if b.RequiredApprovals != nil {
			if err := validateApprovals(*b.RequiredApprovals); err != nil {
				return err
			}
			rule.RequiredApprovals = *b.RequiredApprovals
		}
		if b.RequiredChecks != nil {
			checks, err := cleanChecks(*b.RequiredChecks)
			if err != nil {
				return err
			}
			rule.RequiredChecks = mustJSON(checks)
		}
		rule.UpdatedAt = time.Now().UTC()
		if _, err := tx.NewUpdate().Model(rule).WherePK().Exec(ctx); err != nil {
			return err
		}
		return a.s.auditProtection(ctx, tx, "protection.update", repo, rule)
	})
	if err != nil {
		return nil, err
	}
	return apigen.UpdateProtection200JSONResponse(toAPIProtection(rule)), nil
}

func (a *api) DeleteProtection(ctx context.Context, req apigen.DeleteProtectionRequestObject) (apigen.DeleteProtectionResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Admin)
	if err != nil {
		return nil, err
	}
	err = a.s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		rule, err := a.s.loadRule(ctx, tx, repo, req.Id)
		if err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model(rule).WherePK().Exec(ctx); err != nil {
			return err
		}
		return a.s.auditProtection(ctx, tx, "protection.delete", repo, rule)
	})
	if err != nil {
		return nil, err
	}
	return apigen.DeleteProtection204Response{}, nil
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// ruleChecks returns a rule's required checks (none without a rule).
func ruleChecks(r *model.BranchProtection) []string {
	if r == nil {
		return nil
	}
	return jsonStrings(r.RequiredChecks)
}

// cleanChecks validates required check names.
func cleanChecks(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		if len(c) > 255 {
			return nil, apierr.Validation("required check names are at most 255 characters").WithDetails(map[string]any{"field": "required_checks"})
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, nil
}
