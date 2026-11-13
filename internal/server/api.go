package server

import (
	"context"
	"database/sql"
	"errors"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/version"
)

// api implements apigen.StrictServerInterface. Handlers are spread across
// api_*.go files by area.
type api struct {
	s *Server
}

var _ apigen.StrictServerInterface = (*api)(nil)

func (s *Server) loadOrg(ctx context.Context) (*model.OrgSettings, error) {
	org := new(model.OrgSettings)
	err := s.db.NewSelect().Model(org).Where("id = 1").Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return org, nil
	}
	return org, err
}

func (s *Server) hasLogo(ctx context.Context) (bool, error) {
	return s.db.NewSelect().Model((*model.OrgLogo)(nil)).Where("id = 1").Exists(ctx)
}

// setupComplete reports whether first-start setup has finished.
func (s *Server) setupComplete(ctx context.Context) (bool, error) {
	if s.setupDone.Load() {
		return true, nil
	}
	org, err := s.loadOrg(ctx)
	if err != nil {
		return false, err
	}
	if org.SetupCompletedAt != nil {
		s.setupDone.Store(true)
		return true, nil
	}
	return false, nil
}

func (a *api) GetMeta(ctx context.Context, _ apigen.GetMetaRequestObject) (apigen.GetMetaResponseObject, error) {
	org, err := a.s.loadOrg(ctx)
	if err != nil {
		return nil, err
	}
	logo, err := a.s.hasLogo(ctx)
	if err != nil {
		return nil, err
	}
	v := version.Get()
	return apigen.GetMeta200JSONResponse{
		Version:       v.Version,
		Commit:        v.Commit,
		SetupRequired: org.SetupCompletedAt == nil,
		OrgName:       org.Name,
		HasLogo:       logo,
	}, nil
}

func toAPIUser(u *model.User) apigen.User {
	return apigen.User{
		Id:                    u.ID,
		Username:              u.Username,
		DisplayName:           u.DisplayName,
		Email:                 u.Email,
		Role:                  apigen.Role(u.Role),
		AllRepos:              u.AllRepos,
		MustChangePassword:    u.MustChangePassword,
		TempPasswordExpiresAt: u.TempPasswordExpiresAt,
		TotpEnabled:           u.TOTPEnabled,
		Disabled:              u.Disabled,
		LastLoginAt:           u.LastLoginAt,
		CreatedAt:             u.CreatedAt,
	}
}

func toAPIOrg(org *model.OrgSettings, hasLogo bool) apigen.Org {
	return apigen.Org{Name: org.Name, Description: org.Description, BaseUrl: org.BaseURL, HasLogo: hasLogo}
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
