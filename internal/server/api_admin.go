package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/users"
)

func (a *api) AdminListUsers(ctx context.Context, _ apigen.AdminListUsersRequestObject) (apigen.AdminListUsersResponseObject, error) {
	var us []model.User
	if err := a.s.db.NewSelect().Model(&us).Order("username ASC").Scan(ctx); err != nil {
		return nil, err
	}
	out := make(apigen.AdminListUsers200JSONResponse, 0, len(us))
	for i := range us {
		out = append(out, toAPIUser(&us[i]))
	}
	return out, nil
}

func (a *api) AdminCreateUser(ctx context.Context, req apigen.AdminCreateUserRequestObject) (apigen.AdminCreateUserResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	actor := mustPrincipal(ctx).User
	b := req.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	var c *users.Created
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		c, err = users.Create(ctx, tx, users.CreateParams{
			Username:    b.Username,
			DisplayName: deref(b.DisplayName),
			Email:       deref(b.Email),
			Role:        model.Role(b.Role),
			AllRepos:    deref(b.AllRepos),
		})
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Actor: actor, Action: audit.UserCreate, TargetType: "user", Target: c.User.Username, IP: clientIP(r),
			Details: map[string]any{"role": c.User.Role, "all_repos": c.User.AllRepos}})
	})
	if err != nil {
		return nil, err
	}
	return apigen.AdminCreateUser201JSONResponse{
		User:                  toAPIUser(c.User),
		TempPassword:          c.TempPassword,
		TempPasswordExpiresAt: *c.User.TempPasswordExpiresAt,
	}, nil
}

func (a *api) AdminGetUser(ctx context.Context, req apigen.AdminGetUserRequestObject) (apigen.AdminGetUserResponseObject, error) {
	u, err := users.GetByUsername(ctx, a.s.db, req.Username)
	if err != nil {
		return nil, err
	}
	return apigen.AdminGetUser200JSONResponse(toAPIUser(u)), nil
}

func (a *api) AdminUpdateUser(ctx context.Context, req apigen.AdminUpdateUserRequestObject) (apigen.AdminUpdateUserResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	actor := mustPrincipal(ctx).User
	b := req.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	var u *model.User
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		if u, err = users.GetByUsername(ctx, tx, req.Username); err != nil {
			return err
		}
		changes := map[string]any{}
		if b.DisplayName != nil {
			dn := strings.TrimSpace(*b.DisplayName)
			if dn == "" {
				dn = u.Username
			}
			if len(dn) > 100 {
				return apierr.Validation("display name is too long (max 100)")
			}
			if dn != u.DisplayName {
				changes["display_name"] = dn
				u.DisplayName = dn
			}
		}
		if b.Email != nil {
			email, err := users.NormalizeEmail(*b.Email)
			if err != nil {
				return err
			}
			if err := users.EnsureEmailFree(ctx, tx, email, u.ID); err != nil {
				return err
			}
			if email != u.Email {
				changes["email"] = email
				u.Email = email
			}
		}
		if b.Role != nil {
			role := model.Role(*b.Role)
			if !role.Valid() {
				return apierr.Validation("role must be admin, maintainer or tester")
			}
			if role != u.Role {
				if u.ID == actor.ID {
					return apierr.Forbidden("you can't change your own role; ask another admin")
				}
				changes["role"] = role
				u.Role = role
			}
		}
		if b.AllRepos != nil && *b.AllRepos != u.AllRepos {
			changes["all_repos"] = *b.AllRepos
			u.AllRepos = *b.AllRepos
		}
		if u.Role == model.RoleAdmin {
			u.AllRepos = true
		}
		if b.Disabled != nil && *b.Disabled != u.Disabled {
			if u.ID == actor.ID {
				return apierr.Forbidden("you can't disable your own account")
			}
			changes["disabled"] = *b.Disabled
			u.Disabled = *b.Disabled
		}
		if len(changes) == 0 {
			return nil
		}
		// Never leave the org without an active admin.
		if u.Role != model.RoleAdmin || u.Disabled {
			n, err := users.CountActiveAdmins(ctx, tx, u.ID)
			if err != nil {
				return err
			}
			if n == 0 {
				return apierr.Conflict("this change would leave no active admin")
			}
		}
		u.UpdatedAt = time.Now().UTC()
		if _, err := tx.NewUpdate().Model(u).Column("display_name", "email", "role", "all_repos", "disabled", "updated_at").WherePK().Exec(ctx); err != nil {
			return err
		}
		if u.Disabled {
			if _, err := tx.NewDelete().Model((*model.Session)(nil)).Where("user_id = ?", u.ID).Exec(ctx); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{Actor: actor, Action: audit.UserUpdate, TargetType: "user", Target: u.Username, IP: clientIP(r), Details: changes})
	})
	if err != nil {
		return nil, err
	}
	return apigen.AdminUpdateUser200JSONResponse(toAPIUser(u)), nil
}

func (a *api) AdminResetPassword(ctx context.Context, req apigen.AdminResetPasswordRequestObject) (apigen.AdminResetPasswordResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	actor := mustPrincipal(ctx).User
	var (
		u  *model.User
		pw string
	)
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		if u, err = users.GetByUsername(ctx, tx, req.Username); err != nil {
			return err
		}
		if u.ID == actor.ID {
			return apierr.Forbidden("use account settings to change your own password")
		}
		if pw, err = users.ResetTempPassword(ctx, tx, u); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Actor: actor, Action: audit.UserResetPassword, TargetType: "user", Target: u.Username, IP: clientIP(r)})
	})
	if err != nil {
		return nil, err
	}
	return apigen.AdminResetPassword200JSONResponse{User: toAPIUser(u), TempPassword: pw, TempPasswordExpiresAt: *u.TempPasswordExpiresAt}, nil
}

func (a *api) AdminResetTotp(ctx context.Context, req apigen.AdminResetTotpRequestObject) (apigen.AdminResetTotpResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	actor := mustPrincipal(ctx).User
	var u *model.User
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		if u, err = users.GetByUsername(ctx, tx, req.Username); err != nil {
			return err
		}
		if !u.TOTPEnabled {
			return apierr.Conflict("%s doesn't have two-factor authentication enabled", u.Username)
		}
		if err := clearTOTP(ctx, tx, u); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{Actor: actor, Action: audit.UserResetTOTP, TargetType: "user", Target: u.Username, IP: clientIP(r)})
	})
	if err != nil {
		return nil, err
	}
	return apigen.AdminResetTotp200JSONResponse(toAPIUser(u)), nil
}

func (a *api) ListAuditLog(ctx context.Context, req apigen.ListAuditLogRequestObject) (apigen.ListAuditLogResponseObject, error) {
	p := req.Params
	limit := 50
	if p.Limit != nil {
		limit = max(1, min(*p.Limit, 200))
	}
	var rows []model.AuditEntry
	q := a.s.db.NewSelect().Model(&rows).Order("id DESC").Limit(int64(limit + 1))
	if p.Before != nil {
		q = q.Where("id < ?", *p.Before)
	}
	if p.Actor != nil && *p.Actor != "" {
		q = q.Where("actor_name = ?", users.NormalizeUsername(*p.Actor))
	}
	if p.Action != nil && *p.Action != "" {
		q = q.Where("action LIKE ?", escapeLike(*p.Action)+"%")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	out := apigen.AuditPage{Entries: []apigen.AuditEntry{}}
	if len(rows) > limit {
		rows = rows[:limit]
		next := rows[limit-1].ID
		out.NextBefore = &next
	}
	for _, e := range rows {
		details := map[string]any{}
		_ = json.Unmarshal([]byte(e.Details), &details)
		out.Entries = append(out.Entries, apigen.AuditEntry{
			Id: e.ID, Actor: e.ActorName, Action: e.Action, TargetType: e.TargetType, Target: e.Target,
			Ip: e.IP, Details: details, CreatedAt: e.CreatedAt,
		})
	}
	return apigen.ListAuditLog200JSONResponse(out), nil
}

// escapeLike strips the % wildcard from a user-supplied prefix filter. A
// literal "_" (which LIKE treats as any one character) only over-matches
// slightly, which is acceptable for an audit filter.
func escapeLike(s string) string { return strings.ReplaceAll(s, "%", "") }

// --- Org ---

func (a *api) GetOrg(ctx context.Context, _ apigen.GetOrgRequestObject) (apigen.GetOrgResponseObject, error) {
	org, err := a.s.loadOrg(ctx)
	if err != nil {
		return nil, err
	}
	logo, err := a.s.hasLogo(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.GetOrg200JSONResponse(toAPIOrg(org, logo)), nil
}

func (a *api) UpdateOrg(ctx context.Context, req apigen.UpdateOrgRequestObject) (apigen.UpdateOrgResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	actor := mustPrincipal(ctx).User
	b := req.Body
	if b == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	org, err := s.loadOrg(ctx)
	if err != nil {
		return nil, err
	}
	changes := map[string]any{}
	if b.Name != nil {
		n := strings.TrimSpace(*b.Name)
		if n == "" || len(n) > 100 {
			return nil, apierr.Validation("org name is required (max 100 characters)")
		}
		org.Name, changes["name"] = n, n
	}
	if b.Description != nil {
		d := strings.TrimSpace(*b.Description)
		if len(d) > 500 {
			return nil, apierr.Validation("description is too long (max 500)")
		}
		org.Description, changes["description"] = d, d
	}
	if b.BaseUrl != nil {
		u, err := normalizeBaseURL(*b.BaseUrl)
		if err != nil {
			return nil, err
		}
		org.BaseURL, changes["base_url"] = u, u
	}
	org.UpdatedAt = time.Now().UTC()
	if _, err := s.db.NewUpdate().Model(org).Column("name", "description", "base_url", "updated_at").Where("id = 1").Exec(ctx); err != nil {
		return nil, err
	}
	if err := audit.Record(ctx, s.db, audit.Entry{Actor: actor, Action: audit.OrgUpdate, TargetType: "org", Target: org.Name, IP: clientIP(r), Details: changes}); err != nil {
		return nil, err
	}
	logo, err := s.hasLogo(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.UpdateOrg200JSONResponse(toAPIOrg(org, logo)), nil
}

const maxLogoBytes = 512 << 10

var allowedLogoTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

func (a *api) UploadOrgLogo(ctx context.Context, req apigen.UploadOrgLogoRequestObject) (apigen.UploadOrgLogoResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	actor := mustPrincipal(ctx).User
	data, err := io.ReadAll(io.LimitReader(req.Body, maxLogoBytes+1))
	if err != nil {
		return nil, apierr.BadRequest("read logo: %v", err)
	}
	if len(data) == 0 {
		return nil, apierr.Validation("logo is empty")
	}
	if len(data) > maxLogoBytes {
		return nil, apierr.New(http.StatusRequestEntityTooLarge, apierr.CodePayloadTooLarge, "logo must be at most 512 KiB")
	}
	ct := http.DetectContentType(data)
	if !allowedLogoTypes[ct] {
		return nil, apierr.Validation("logo must be a PNG, JPEG, GIF or WebP image (got %s)", ct)
	}
	row := &model.OrgLogo{ID: 1, ContentType: ct, Data: data, UpdatedAt: time.Now().UTC()}
	if _, err := s.db.NewInsert().Model(row).On("CONFLICT (id) DO UPDATE").
		Set("content_type = EXCLUDED.content_type, data = EXCLUDED.data, updated_at = EXCLUDED.updated_at").Exec(ctx); err != nil {
		return nil, err
	}
	_ = audit.Record(ctx, s.db, audit.Entry{Actor: actor, Action: audit.OrgUpdate, TargetType: "org", Target: "logo", IP: clientIP(r), Details: map[string]any{"logo": ct, "bytes": len(data)}})
	org, err := s.loadOrg(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.UploadOrgLogo200JSONResponse(toAPIOrg(org, true)), nil
}

func (a *api) DeleteOrgLogo(ctx context.Context, _ apigen.DeleteOrgLogoRequestObject) (apigen.DeleteOrgLogoResponseObject, error) {
	s := a.s
	_, r := httpFrom(ctx)
	if _, err := s.db.NewDelete().Model((*model.OrgLogo)(nil)).Where("id = 1").Exec(ctx); err != nil {
		return nil, err
	}
	_ = audit.Record(ctx, s.db, audit.Entry{Actor: mustPrincipal(ctx).User, Action: audit.OrgUpdate, TargetType: "org", Target: "logo", IP: clientIP(r), Details: map[string]any{"logo": "removed"}})
	return apigen.DeleteOrgLogo204Response{}, nil
}

func (a *api) GetOrgLogo(ctx context.Context, _ apigen.GetOrgLogoRequestObject) (apigen.GetOrgLogoResponseObject, error) {
	w, _ := httpFrom(ctx)
	logo := new(model.OrgLogo)
	if err := a.s.db.NewSelect().Model(logo).Where("id = 1").Scan(ctx); err != nil {
		return nil, apierr.NotFound("no org logo")
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	return apigen.GetOrgLogo200ImageResponse{Body: bytes.NewReader(logo.Data), ContentType: logo.ContentType, ContentLength: int64(len(logo.Data))}, nil
}
