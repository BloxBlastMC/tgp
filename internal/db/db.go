// Package db opens the tgp database (SQLite or PostgreSQL) through bun and
// runs schema migrations.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver

	"git.taile67276.ts.net/BloxBlast/tgp/internal/config"
)

// DB wraps bun.DB with the driver name so callers can branch on dialect
// where SQL genuinely differs (e.g. SKIP LOCKED).
type DB struct {
	*bun.DB
	Driver string
}

// IsPostgres reports whether the database is PostgreSQL.
func (d *DB) IsPostgres() bool { return d.Driver == "postgres" }

// Open connects to the configured database and verifies the connection.
func Open(ctx context.Context, cfg config.DatabaseConfig) (*DB, error) {
	var (
		sqldb *sql.DB
		bdb   *bun.DB
	)
	switch cfg.Driver {
	case "sqlite":
		dsn, err := sqliteDSN(cfg.DSN)
		if err != nil {
			return nil, err
		}
		sqldb, err = sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("open sqlite: %w", err)
		}
		// SQLite allows a single writer. One connection avoids SQLITE_BUSY
		// churn; WAL keeps reads fast. Postgres is the scale-out option.
		sqldb.SetMaxOpenConns(1)
		bdb = bun.NewDB(sqldb, sqlitedialect.New())
	case "postgres":
		sqldb = sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(cfg.DSN)))
		maxOpen := cfg.MaxOpenConns
		if maxOpen <= 0 {
			maxOpen = 25
		}
		sqldb.SetMaxOpenConns(maxOpen)
		sqldb.SetMaxIdleConns(maxOpen)
		sqldb.SetConnMaxIdleTime(5 * time.Minute)
		bdb = bun.NewDB(sqldb, pgdialect.New())
	default:
		return nil, fmt.Errorf("unsupported database driver %q", cfg.Driver)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := bdb.PingContext(pingCtx); err != nil {
		_ = bdb.Close()
		return nil, fmt.Errorf("connect to %s database: %w", cfg.Driver, err)
	}
	return &DB{DB: bdb, Driver: cfg.Driver}, nil
}

// sqliteDSN turns a plain file path (or ":memory:") into a modernc DSN with
// the pragmas tgp relies on. A DSN that already starts with "file:" is used
// as given.
func sqliteDSN(path string) (string, error) {
	if strings.HasPrefix(path, "file:") {
		return path, nil
	}
	pragmas := "_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_txlock=immediate"
	if path == ":memory:" {
		return "file::memory:?" + pragmas, nil
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return "", fmt.Errorf("create database directory: %w", err)
		}
	}
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + pragmas + "&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", nil
}
