package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/version"
)

// api implements apigen.StrictServerInterface.
type api struct {
	s *Server
}

var _ apigen.StrictServerInterface = (*api)(nil)

type orgSettings struct {
	bun.BaseModel `bun:"table:org_settings"`

	ID               int64      `bun:"id,pk"`
	Name             string     `bun:"name"`
	SetupCompletedAt *time.Time `bun:"setup_completed_at"`
}

func (a *api) GetMeta(ctx context.Context, _ apigen.GetMetaRequestObject) (apigen.GetMetaResponseObject, error) {
	var org orgSettings
	err := a.s.db.NewSelect().Model(&org).Where("id = 1").Scan(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	v := version.Get()
	return apigen.GetMeta200JSONResponse{
		Version:       v.Version,
		Commit:        v.Commit,
		SetupRequired: org.SetupCompletedAt == nil,
		OrgName:       org.Name,
	}, nil
}
