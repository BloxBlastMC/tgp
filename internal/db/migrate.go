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

// MigrateTo applies only the named migrations (which must all be known),
// bringing a database to the schema of an older tgp version. Restores use
// it to load a backup into the schema it was taken from before migrating
// to the current one.
func MigrateTo(ctx context.Context, d *DB, log *slog.Logger, names []string) error {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	sub := migrate.NewMigrations()
	for _, m := range migrations.Migrations.Sorted() {
		if want[m.Name] {
			sub.Add(migrate.Migration{Name: m.Name, Comment: m.Comment, Up: m.Up, Down: m.Down})
			delete(want, m.Name)
		}
	}
	for n := range want {
		return fmt.Errorf("unknown migration %q: the backup is from a newer tgp version", n)
	}
	m := migrate.NewMigrator(d.DB, sub, migrate.WithMarkAppliedOnSuccess(true))
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
	if _, err := m.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
