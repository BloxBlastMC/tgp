// Package authz decides what a user may do in a repository.
package authz

import (
	"context"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

// Perm is a user's permission level on a repository. Levels are ordered:
// each includes everything below it.
type Perm int

const (
	// None: the repository is invisible.
	None Perm = iota
	// Read: clone, browse code, history and CI results.
	Read
	// Triage (testers): Read plus issues, comments, PR review verdicts and
	// CI re-runs. No pushes, no merges.
	Triage
	// Write (maintainers): Triage plus push, merge, manage branches, labels
	// and CI settings.
	Write
	// Admin (admins): everything, including renaming, archiving, deleting
	// and managing access.
	Admin
)

func (p Perm) String() string {
	return [...]string{"none", "read", "triage", "write", "admin"}[p]
}

// RoleCeiling is the highest permission a role can have on a repository it
// has access to.
func RoleCeiling(r model.Role) Perm {
	switch r {
	case model.RoleAdmin:
		return Admin
	case model.RoleMaintainer:
		return Write
	case model.RoleTester:
		return Triage
	}
	return None
}

// RepoPerm returns u's permission on repo.
func RepoPerm(ctx context.Context, db bun.IDB, u *model.User, repo *model.Repo) (Perm, error) {
	if u == nil || u.Disabled {
		return None, nil
	}
	if u.Role == model.RoleAdmin {
		return Admin, nil
	}
	if !u.AllRepos {
		ok, err := db.NewSelect().Model((*model.RepoGrant)(nil)).
			Where("repo_id = ?", repo.ID).Where("user_id = ?", u.ID).Exists(ctx)
		if err != nil {
			return None, err
		}
		if !ok {
			return None, nil
		}
	}
	return RoleCeiling(u.Role), nil
}

// VisibleRepos restricts a repos query to those u can see.
func VisibleRepos(q *bun.SelectQuery, u *model.User) *bun.SelectQuery {
	if u.Role == model.RoleAdmin || u.AllRepos {
		return q
	}
	return q.Where("r.id IN (SELECT repo_id FROM repo_grants WHERE user_id = ?)", u.ID)
}
