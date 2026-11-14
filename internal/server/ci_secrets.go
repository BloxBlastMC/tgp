package server

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

const maxSecretsPerScope = 200

var secretNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,99}$`)

func normalizeSecretName(name string) (string, error) {
	n := strings.ToUpper(strings.TrimSpace(name))
	switch {
	case !secretNameRe.MatchString(n):
		return "", apierr.Validation("secret names use letters, digits and _, don't start with a digit, and are at most 100 characters").WithDetails(map[string]any{"field": "name"})
	case strings.HasPrefix(n, "TGP_") || strings.HasPrefix(n, "GITHUB_"):
		return "", apierr.Validation("secret names can't start with TGP_ or GITHUB_ (reserved)").WithDetails(map[string]any{"field": "name"})
	}
	return n, nil
}

// secretAAD binds a sealed value to its scope and name, so a value can't
// be moved to another secret in the database.
func secretAAD(repoID *int64, name string) string {
	scope := "org"
	if repoID != nil {
		scope = "repo:" + strconv.FormatInt(*repoID, 10)
	}
	return "secret:" + scope + ":" + name
}

func scopeWhere(q *bun.SelectQuery, repoID *int64) *bun.SelectQuery {
	if repoID == nil {
		return q.Where("sec.repo_id IS NULL")
	}
	return q.Where("sec.repo_id = ?", *repoID)
}

func (s *Server) listSecrets(ctx context.Context, repoID *int64) ([]apigen.Secret, error) {
	var rows []*model.Secret
	if err := scopeWhere(s.db.NewSelect().Model(&rows), repoID).Order("sec.name").Scan(ctx); err != nil {
		return nil, err
	}
	uc := s.users()
	scope := apigen.SecretScopeOrg
	if repoID != nil {
		scope = apigen.SecretScopeRepo
	}
	out := make([]apigen.Secret, len(rows))
	for i, r := range rows {
		out[i] = apigen.Secret{Name: r.Name, Scope: scope, UpdatedAt: r.UpdatedAt, UpdatedBy: uc.summaryPtr(ctx, r.UpdatedByID)}
	}
	return out, nil
}

func (s *Server) setSecret(ctx context.Context, repoID *int64, target, rawName, value string) (apigen.Secret, error) {
	name, err := normalizeSecretName(rawName)
	if err != nil {
		return apigen.Secret{}, err
	}
	if value == "" {
		return apigen.Secret{}, apierr.Validation("the secret value is empty").WithDetails(map[string]any{"field": "value"})
	}
	user := mustPrincipal(ctx).User
	now := s.now()
	sealed := s.box.Seal([]byte(value), secretAAD(repoID, name))
	created := false
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		row := new(model.Secret)
		err := scopeWhere(tx.NewSelect().Model(row), repoID).Where("sec.name = ?", name).Scan(ctx)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			n, err := scopeWhere(tx.NewSelect().Model((*model.Secret)(nil)), repoID).Count(ctx)
			if err != nil {
				return err
			}
			if n >= maxSecretsPerScope {
				return apierr.Validation("at most %d secrets per scope", maxSecretsPerScope)
			}
			row = &model.Secret{RepoID: repoID, Name: name, Value: sealed, CreatedAt: now, UpdatedAt: now, UpdatedByID: &user.ID}
			created = true
			if _, err := tx.NewInsert().Model(row).Exec(ctx); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			row.Value, row.UpdatedAt, row.UpdatedByID = sealed, now, &user.ID
			if _, err := tx.NewUpdate().Model(row).Column("value", "updated_at", "updated_by_id").WherePK().Exec(ctx); err != nil {
				return err
			}
		}
		return s.record(ctx, tx, audit.SecretSet, "secret", target+"/"+name, map[string]any{"created": created})
	})
	if err != nil {
		return apigen.Secret{}, err
	}
	scope := apigen.SecretScopeOrg
	if repoID != nil {
		scope = apigen.SecretScopeRepo
	}
	return apigen.Secret{Name: name, Scope: scope, UpdatedAt: now, UpdatedBy: s.users().summaryPtr(ctx, &user.ID)}, nil
}

func (s *Server) deleteSecret(ctx context.Context, repoID *int64, target, rawName string) error {
	name := strings.ToUpper(strings.TrimSpace(rawName))
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		q := tx.NewDelete().Model((*model.Secret)(nil)).Where("name = ?", name)
		if repoID == nil {
			q = q.Where("repo_id IS NULL")
		} else {
			q = q.Where("repo_id = ?", *repoID)
		}
		res, err := q.Exec(ctx)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return apierr.NotFound("secret %s not found", name)
		}
		return s.record(ctx, tx, audit.SecretDelete, "secret", target+"/"+name, nil)
	})
}

// jobSecrets decrypts the secrets a job references: organisation secrets,
// overridden by repository secrets of the same name.
func (s *Server) jobSecrets(ctx context.Context, db bun.IDB, repoID int64, names []string) (map[string]string, error) {
	out := map[string]string{}
	if len(names) == 0 {
		return out, nil
	}
	q := db.NewSelect().Model((*model.Secret)(nil)).Where("(sec.repo_id IS NULL OR sec.repo_id = ?)", repoID)
	if !(len(names) == 1 && names[0] == "*") {
		q = q.Where("sec.name IN (?)", bun.In(names))
	}
	var rows []*model.Secret
	if err := q.Order("sec.repo_id").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	// Org rows (NULL repo_id) first, then repo rows override them; ordering
	// of NULLs differs between databases, so apply in two passes.
	for _, pass := range []bool{false, true} {
		for _, r := range rows {
			if (r.RepoID != nil) != pass {
				continue
			}
			v, err := s.box.Open(r.Value, secretAAD(r.RepoID, r.Name))
			if err != nil {
				s.log.Error("ci: can't decrypt secret (was the master key changed?)", "name", r.Name, "err", err)
				continue
			}
			out[r.Name] = string(v)
		}
	}
	return out, nil
}

func (a *api) ListRepoSecrets(ctx context.Context, req apigen.ListRepoSecretsRequestObject) (apigen.ListRepoSecretsResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	out, err := a.s.listSecrets(ctx, &repo.ID)
	if err != nil {
		return nil, err
	}
	return apigen.ListRepoSecrets200JSONResponse(out), nil
}

func (a *api) SetRepoSecret(ctx context.Context, req apigen.SetRepoSecretRequestObject) (apigen.SetRepoSecretResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	out, err := a.s.setSecret(ctx, &repo.ID, repo.Name, req.Name, req.Body.Value)
	if err != nil {
		return nil, err
	}
	return apigen.SetRepoSecret200JSONResponse(out), nil
}

func (a *api) DeleteRepoSecret(ctx context.Context, req apigen.DeleteRepoSecretRequestObject) (apigen.DeleteRepoSecretResponseObject, error) {
	repo, _, err := a.s.repoFor(ctx, req.Repo, authz.Write)
	if err != nil {
		return nil, err
	}
	if err := a.s.deleteSecret(ctx, &repo.ID, repo.Name, req.Name); err != nil {
		return nil, err
	}
	return apigen.DeleteRepoSecret204Response{}, nil
}

func (a *api) ListOrgSecrets(ctx context.Context, _ apigen.ListOrgSecretsRequestObject) (apigen.ListOrgSecretsResponseObject, error) {
	out, err := a.s.listSecrets(ctx, nil)
	if err != nil {
		return nil, err
	}
	return apigen.ListOrgSecrets200JSONResponse(out), nil
}

func (a *api) SetOrgSecret(ctx context.Context, req apigen.SetOrgSecretRequestObject) (apigen.SetOrgSecretResponseObject, error) {
	out, err := a.s.setSecret(ctx, nil, "org", req.Name, req.Body.Value)
	if err != nil {
		return nil, err
	}
	return apigen.SetOrgSecret200JSONResponse(out), nil
}

func (a *api) DeleteOrgSecret(ctx context.Context, req apigen.DeleteOrgSecretRequestObject) (apigen.DeleteOrgSecretResponseObject, error) {
	if err := a.s.deleteSecret(ctx, nil, "org", req.Name); err != nil {
		return nil, err
	}
	return apigen.DeleteOrgSecret204Response{}, nil
}
