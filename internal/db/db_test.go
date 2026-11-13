package db

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/config"
)

// testDatabases returns the database configs to run against: always SQLite,
// plus Postgres when TGP_TEST_POSTGRES_DSN is set.
func testDatabases(t *testing.T) map[string]config.DatabaseConfig {
	dbs := map[string]config.DatabaseConfig{
		"sqlite": {Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "sub", "tgp.db")},
	}
	if dsn := os.Getenv("TGP_TEST_POSTGRES_DSN"); dsn != "" {
		dbs["postgres"] = config.DatabaseConfig{Driver: "postgres", DSN: dsn}
	}
	return dbs
}

func TestMigrateIdempotent(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for name, cfg := range testDatabases(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			d, err := Open(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if err := Migrate(ctx, d, log); err != nil {
				t.Fatal(err)
			}
			if err := Migrate(ctx, d, log); err != nil {
				t.Fatalf("second migrate: %v", err)
			}
			st, err := Status(ctx, d)
			if err != nil {
				t.Fatal(err)
			}
			if len(st) == 0 {
				t.Fatal("no migrations registered")
			}
			for _, m := range st {
				if !m.Applied {
					t.Errorf("migration %s not applied", m.Name)
				}
			}
			var n int
			if err := d.NewSelect().Table("org_settings").ColumnExpr("count(*)").Scan(ctx, &n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("org_settings rows = %d, want 1", n)
			}
		})
	}
}

func TestSQLiteForeignKeysEnabled(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, config.DatabaseConfig{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "x.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var fk int
	if err := d.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatal("foreign keys not enabled")
	}
}
