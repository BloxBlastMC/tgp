package migrations

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type orgSettingsV1 struct {
	bun.BaseModel `bun:"table:org_settings"`

	// ID is always 1: the platform hosts exactly one org.
	ID               int64      `bun:"id,pk"`
	Name             string     `bun:"name,notnull,default:''"`
	Description      string     `bun:"description,notnull,default:''"`
	LogoPath         string     `bun:"logo_path,notnull,default:''"`
	BaseURL          string     `bun:"base_url,notnull,default:''"`
	SetupCompletedAt *time.Time `bun:"setup_completed_at"`
	CreatedAt        time.Time  `bun:"created_at,notnull"`
	UpdatedAt        time.Time  `bun:"updated_at,notnull"`
}

type serverSettingsV1 struct {
	bun.BaseModel `bun:"table:server_settings"`

	// Key/value store for admin-editable settings (CI limits, retention...).
	Key       string    `bun:"key,pk"`
	Value     string    `bun:"value,notnull"`
	UpdatedAt time.Time `bun:"updated_at,notnull"`
}

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			if _, err := tx.NewCreateTable().Model((*orgSettingsV1)(nil)).Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.NewCreateTable().Model((*serverSettingsV1)(nil)).Exec(ctx); err != nil {
				return err
			}
			now := time.Now().UTC()
			_, err := tx.NewInsert().Model(&orgSettingsV1{ID: 1, CreatedAt: now, UpdatedAt: now}).Exec(ctx)
			return err
		})
	}, func(ctx context.Context, db *bun.DB) error {
		if _, err := db.NewDropTable().Model((*serverSettingsV1)(nil)).IfExists().Exec(ctx); err != nil {
			return err
		}
		_, err := db.NewDropTable().Model((*orgSettingsV1)(nil)).IfExists().Exec(ctx)
		return err
	})
}
