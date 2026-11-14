package backup_test

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/backup"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/config"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/storage"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/testserver"
)

// TestCrossDialect moves an installation SQLite → PostgreSQL → SQLite
// through backups. It runs when TGP_TEST_POSTGRES_DSN points at a
// disposable database; it works in a schema of its own and drops it.
func TestCrossDialect(t *testing.T) {
	dsn := os.Getenv("TGP_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TGP_TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	schema := "tgp_backup_" + strings.ToLower(auth.RandomToken(6))
	adminDB, err := db.Open(ctx, config.DatabaseConfig{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = adminDB.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}

	src := testserver.Start(t)
	head := populate(t, src)
	var b1 bytes.Buffer
	if _, _, err := backup.Create(ctx, &b1, backup.Options{Config: src.Config, DB: src.DB, Log: quiet}); err != nil {
		t.Fatal(err)
	}

	// SQLite backup → PostgreSQL.
	pgDir := t.TempDir()
	pgCfg := testserver.Config(pgDir)
	pgCfg.Database = config.DatabaseConfig{Driver: "postgres", DSN: dsn + sep + "search_path=" + schema}
	pg, err := db.Open(ctx, pgCfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	pgStore, _ := storage.NewLocal(filepath.Join(pgDir, "storage"))
	if _, st, err := backup.Restore(ctx, bytes.NewReader(b1.Bytes()), backup.RestoreOptions{Config: pgCfg, DB: pg, Store: pgStore, Log: quiet}); err != nil {
		t.Fatalf("restore into postgres: %v", err)
	} else if st.Repos != 2 {
		t.Fatalf("stats %+v", st)
	}

	// PostgreSQL backup → SQLite, then serve it.
	var b2 bytes.Buffer
	m, _, err := backup.Create(ctx, &b2, backup.Options{Config: pgCfg, DB: pg, Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	if m.Driver != "postgres" {
		t.Fatal(m.Driver)
	}
	dir := t.TempDir()
	cfg := testserver.Config(dir)
	d, err := db.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := storage.NewLocal(filepath.Join(dir, "storage"))
	if _, _, err := backup.Restore(ctx, bytes.NewReader(b2.Bytes()), backup.RestoreOptions{Config: cfg, DB: d, Store: store, Log: quiet}); err != nil {
		t.Fatalf("restore into sqlite: %v", err)
	}
	d.Close()
	s := testserver.StartIn(t, dir)
	admin := s.Login("admin", testserver.AdminPassword)
	var issue struct{ Title string }
	admin.Do(http.MethodGet, "/repos/Game/issues/1", nil, &issue)
	if issue.Title != "Crash on start" {
		t.Fatalf("issue: %+v", issue)
	}
	cl := t.TempDir()
	git(t, cl, "clone", "-q", cloneURL(s.URL, admin.Token("read"), "game"), "c")
	if got := git(t, filepath.Join(cl, "c"), "rev-parse", "HEAD"); got != head {
		t.Fatalf("head %s, want %s", got, head)
	}
}
