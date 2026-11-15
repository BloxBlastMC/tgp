package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

// Host jobs (container: none) and trusted runs: jobs remember whether they
// run on the runner host and whether their run is trusted (registry push
// with the job token), and runners report which host jobs they accept.
func init() {
	cols := []struct{ table, name, def string }{
		{"jobs", "host", "BOOLEAN NOT NULL DEFAULT FALSE"},
		{"jobs", "trusted", "BOOLEAN NOT NULL DEFAULT FALSE"},
		{"runners", "host_mode", "VARCHAR NOT NULL DEFAULT 'off'"},
	}
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			for _, c := range cols {
				if _, err := tx.NewAddColumn().Table(c.table).ColumnExpr(c.name + " " + c.def).Exec(ctx); err != nil {
					return err
				}
			}
			return nil
		})
	}, func(ctx context.Context, db *bun.DB) error {
		for _, c := range cols {
			if _, err := db.NewDropColumn().Table(c.table).Column(c.name).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}
