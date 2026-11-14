package db

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/uptrace/bun/migrate"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/db/migrations"
)

// Migrate applies all pending migrations. It takes the bun migration lock so
// several server nodes starting at once don't race.
func Migrate(ctx context.Context, d *DB, log *slog.Logger) error {
	m := migrate.NewMigrator(d.DB, migrations.Migrations, migrate.WithMarkAppliedOnSuccess(true))
	if err := m.Init(ctx); err != nil {
		return fmt.Errorf("init migrations: %w", err)
	}
	if err := m.Lock(ctx); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		if err := m.Unlock(ctx); err != nil {
			log.Warn("unlock migrations", "err", err)
		}
	}()
	group, err := m.Migrate(ctx)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if group.IsZero() {
		log.Debug("database schema up to date")
		return nil
	}
	log.Info("applied database migrations", "group", group.ID, "migrations", group.Migrations.String())
	return nil
}

// MigrationStatus describes one migration and whether it has been applied.
type MigrationStatus struct {
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
	Applied bool   `json:"applied"`
}

// Status lists all known migrations with their applied state.
func Status(ctx context.Context, d *DB) ([]MigrationStatus, error) {
	m := migrate.NewMigrator(d.DB, migrations.Migrations)
	if err := m.Init(ctx); err != nil {
		return nil, err
	}
	ms, err := m.MigrationsWithStatus(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MigrationStatus, 0, len(ms))
	for _, mg := range ms {
		out = append(out, MigrationStatus{Name: mg.Name, Comment: mg.Comment, Applied: mg.IsApplied()})
	}
	return out, nil
}
