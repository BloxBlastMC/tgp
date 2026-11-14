package migrations

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type reposV1 struct {
	bun.BaseModel `bun:"table:repos"`

	ID            int64      `bun:"id,pk,autoincrement"`
	Name          string     `bun:"name,notnull"`
	LowerName     string     `bun:"lower_name,notnull,unique"`
	Description   string     `bun:"description,notnull,default:''"`
	DefaultBranch string     `bun:"default_branch,notnull"`
	Archived      bool       `bun:"archived,notnull,default:false"`
	CreatedByID   *int64     `bun:"created_by_id"`
	CreatedAt     time.Time  `bun:"created_at,notnull"`
	UpdatedAt     time.Time  `bun:"updated_at,notnull"`
	PushedAt      *time.Time `bun:"pushed_at"`
}

type repoGrantsV1 struct {
	bun.BaseModel `bun:"table:repo_grants"`

	ID        int64     `bun:"id,pk,autoincrement"`
	RepoID    int64     `bun:"repo_id,notnull,unique:repo_user"`
	UserID    int64     `bun:"user_id,notnull,unique:repo_user"`
	CreatedAt time.Time `bun:"created_at,notnull"`
}

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			if _, err := tx.NewCreateTable().Model((*reposV1)(nil)).
				ForeignKey(`("created_by_id") REFERENCES "users" ("id") ON DELETE SET NULL`).Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.NewCreateTable().Model((*repoGrantsV1)(nil)).
				ForeignKey(`("repo_id") REFERENCES "repos" ("id") ON DELETE CASCADE`).
				ForeignKey(`("user_id") REFERENCES "users" ("id") ON DELETE CASCADE`).Exec(ctx); err != nil {
				return err
			}
			_, err := tx.NewCreateIndex().Model((*repoGrantsV1)(nil)).Index("repo_grants_user_id_idx").Column("user_id").Exec(ctx)
			return err
		})
	}, func(ctx context.Context, db *bun.DB) error {
		if _, err := db.NewDropTable().Model((*repoGrantsV1)(nil)).IfExists().Exec(ctx); err != nil {
			return err
		}
		_, err := db.NewDropTable().Model((*reposV1)(nil)).IfExists().Exec(ctx)
		return err
	})
}
