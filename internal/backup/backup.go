// Package backup writes and restores tgp-server backups.
//
// A backup is a gzip-compressed tar archive:
//
//	manifest.json             format version, tgp version, schema migrations, contents
//	db/NNN-<table>.jsonl      one table per file: a header line, then one JSON array per row
//	master.key                the secrets master key (unless excluded)
//	repos/<name>.git/...      bare repositories, refs before objects
//	storage/<key>             blobs from local storage (logs, artifacts, caches, release assets)
//
// The database dump is logical and dialect-neutral, so a SQLite backup can
// be restored into PostgreSQL and the other way round. Values are converted
// to the target column's type when restoring.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/config"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/version"
)

// FormatVersion is the archive layout version. Restores refuse newer ones.
const FormatVersion = 1

// Manifest describes a backup. It is the archive's first entry.
type Manifest struct {
	Format     int       `json:"format"`
	Version    string    `json:"version"`
	Commit     string    `json:"commit"`
	CreatedAt  time.Time `json:"created_at"`
	Driver     string    `json:"driver"`
	Migrations []string  `json:"migrations"`
	// Tables in load order (parents before children).
	Tables      []string `json:"tables"`
	MasterKey   bool     `json:"master_key"`
	StorageType string   `json:"storage_type"`
	// Storage is true when blobs from local storage are included.
	Storage bool `json:"storage"`
	// Caches is false when CI caches were left out (--no-caches).
	Caches bool `json:"caches"`
}

// Stats summarises what a backup or restore handled.
type Stats struct {
	Tables    int   `json:"tables"`
	Rows      int64 `json:"rows"`
	Repos     int   `json:"repos"`
	RepoBytes int64 `json:"repo_bytes"`
	Blobs     int   `json:"blobs"`
	BlobBytes int64 `json:"blob_bytes"`
	MasterKey bool  `json:"master_key"`
}

// Options configure a backup.
type Options struct {
	Config *config.Config
	DB     *db.DB
	Log    *slog.Logger
	// NoMasterKey leaves the master key out. CI secrets and 2FA seeds then
	// only decrypt with the key the restoring server already has.
	NoMasterKey bool
	// NoStorage leaves out local blob storage.
	NoStorage bool
	// NoCaches leaves out CI caches (blobs and their database rows); jobs
	// rebuild them.
	NoCaches bool
}

// skipTables are never dumped: bun's migration bookkeeping is recreated by
// the restore's migrations.
var skipTables = map[string]bool{"bun_migrations": true, "bun_migration_locks": true}

// Create writes a backup to w.
func Create(ctx context.Context, w io.Writer, o Options) (*Manifest, *Stats, error) {
	src, cleanup, err := snapshot(ctx, o)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()

	m := &Manifest{Format: FormatVersion, Version: version.Version, Commit: version.Commit, CreatedAt: time.Now().UTC(),
		Driver: o.DB.Driver, MasterKey: !o.NoMasterKey, StorageType: o.Config.Storage.Type, Caches: !o.NoCaches}
	if m.StorageType == "" {
		m.StorageType = "local"
	}
	m.Storage = m.StorageType == "local" && !o.NoStorage
	if err := src.q.NewRaw("SELECT name FROM bun_migrations ORDER BY name").Scan(ctx, &m.Migrations); err != nil {
		return nil, nil, fmt.Errorf("read applied migrations: %w", err)
	}
	if len(m.Migrations) == 0 {
		return nil, nil, errors.New("the database has no schema yet; start the server once first")
	}
	tables, err := listTables(ctx, src.q, src.driver)
	if err != nil {
		return nil, nil, err
	}
	if m.Tables, err = loadOrder(ctx, src.q, src.driver, tables); err != nil {
		return nil, nil, err
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	stats := &Stats{}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := writeFile(tw, "manifest.json", mb, 0o600); err != nil {
		return nil, nil, err
	}

	for i, t := range m.Tables {
		n, err := dumpTable(ctx, src.q, src.driver, tw, i, t, o.NoCaches && t == "cache_entries")
		if err != nil {
			return nil, nil, fmt.Errorf("dump %s: %w", t, err)
		}
		stats.Tables++
		stats.Rows += n
	}
	o.Log.Info("database dumped", "tables", stats.Tables, "rows", stats.Rows)

	if m.MasterKey {
		key, err := os.ReadFile(o.Config.Security.MasterKeyFile)
		if err != nil {
			return nil, nil, fmt.Errorf("read master key (use --no-master-key to leave it out): %w", err)
		}
		if err := writeFile(tw, "master.key", key, 0o600); err != nil {
			return nil, nil, err
		}
		stats.MasterKey = true
	}

	var repoNames []string
	if err := src.q.NewRaw(`SELECT lower_name FROM repos ORDER BY lower_name`).Scan(ctx, &repoNames); err != nil {
		return nil, nil, fmt.Errorf("list repositories: %w", err)
	}
	for _, name := range repoNames {
		n, err := addRepo(tw, filepath.Join(o.Config.Repos.Root, name+".git"), "repos/"+name+".git")
		if errors.Is(err, fs.ErrNotExist) {
			o.Log.Warn("repository directory missing; deleted while backing up?", "repo", name)
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("repository %s: %w", name, err)
		}
		stats.Repos++
		stats.RepoBytes += n
	}
	o.Log.Info("repositories archived", "repos", stats.Repos, "bytes", stats.RepoBytes)

	if m.Storage {
		if err := addStorage(tw, localStoragePath(o.Config), o.NoCaches, stats); err != nil {
			return nil, nil, fmt.Errorf("storage: %w", err)
		}
		o.Log.Info("blob storage archived", "blobs", stats.Blobs, "bytes", stats.BlobBytes)
	}

	if err := tw.Close(); err != nil {
		return nil, nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, nil, err
	}
	return m, stats, nil
}

// source is a consistent view of the database to dump.
type source struct {
	q      bun.IDB
	driver string
}

// snapshot returns a consistent read view. For SQLite it copies the
// database with VACUUM INTO, which doesn't block the running server; for
// PostgreSQL it opens a repeatable-read, read-only transaction.
func snapshot(ctx context.Context, o Options) (*source, func(), error) {
	if !o.DB.IsPostgres() {
		dir := o.Config.DataDir
		if dsn := o.Config.Database.DSN; dsn != "" && !strings.HasPrefix(dsn, "file:") && dsn != ":memory:" {
			dir = filepath.Dir(dsn)
		}
		f, err := os.CreateTemp(dir, ".backup-*.db")
		if err != nil {
			return nil, nil, fmt.Errorf("temporary database copy: %w", err)
		}
		tmp := f.Name()
		f.Close()
		os.Remove(tmp) // VACUUM INTO wants a new file
		cleanup := func() {
			for _, sfx := range []string{"", "-wal", "-shm"} {
				os.Remove(tmp + sfx)
			}
		}
		if _, err := o.DB.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("snapshot database: %w", err)
		}
		snap, err := db.Open(ctx, config.DatabaseConfig{Driver: "sqlite", DSN: tmp})
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		return &source{q: snap.DB, driver: "sqlite"}, func() { snap.Close(); cleanup() }, nil
	}
	tx, err := o.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot database: %w", err)
	}
	return &source{q: tx, driver: "postgres"}, func() { _ = tx.Rollback() }, nil
}

func localStoragePath(cfg *config.Config) string {
	if p := cfg.Storage.Local.Path; p != "" {
		return p
	}
	return filepath.Join(cfg.DataDir, "storage")
}

// header is the first line of each table file.
type header struct {
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
}

// Typed JSON forms for values plain JSON would lose.
type (
	jsonBytes struct {
		B string `json:"$b"`
	}
	jsonTime struct {
		T string `json:"$t"`
	}
)

func dumpTable(ctx context.Context, q bun.IDB, driver string, tw *tar.Writer, i int, table string, headerOnly bool) (int64, error) {
	pk, err := primaryKey(ctx, q, driver, table)
	if err != nil {
		return 0, err
	}
	query := "SELECT * FROM " + quoteIdent(table)
	if len(pk) > 0 {
		cols := make([]string, len(pk))
		for i, c := range pk {
			cols[i] = quoteIdent(c)
		}
		query += " ORDER BY " + strings.Join(cols, ", ")
	}
	if headerOnly {
		query += " LIMIT 0"
	}
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	// Rows are buffered in a temp file: tar needs each entry's size first.
	tmp, err := os.CreateTemp("", "tgp-dump-*.jsonl")
	if err != nil {
		return 0, err
	}
	defer func() { tmp.Close(); os.Remove(tmp.Name()) }()
	enc := json.NewEncoder(tmp)
	if err := enc.Encode(header{Table: table, Columns: cols}); err != nil {
		return 0, err
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	out := make([]any, len(cols))
	var n int64
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		for i, v := range vals {
			switch v := v.(type) {
			case []byte:
				out[i] = jsonBytes{base64.StdEncoding.EncodeToString(v)}
			case time.Time:
				out[i] = jsonTime{v.UTC().Format(time.RFC3339Nano)}
			default:
				out[i] = v
			}
		}
		if err := enc.Encode(out); err != nil {
			return n, err
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return n, err
	}
	fi, err := tmp.Stat()
	if err != nil {
		return n, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return n, err
	}
	name := fmt.Sprintf("db/%03d-%s.jsonl", i, table)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: fi.Size(), ModTime: time.Now(), Typeflag: tar.TypeReg}); err != nil {
		return n, err
	}
	_, err = io.Copy(tw, tmp)
	return n, err
}

// addRepo archives a bare repository. Refs go first and objects last: git
// writes objects before the refs pointing at them, so every ref in the
// copy finds its objects even if a push lands meanwhile. Object files that
// appear while copying (a repack by git gc) are picked up afterwards, so a
// pack replacing loose objects we missed is never lost.
func addRepo(tw *tar.Writer, dir, prefix string) (int64, error) {
	if _, err := os.Stat(dir); err != nil {
		return 0, err
	}
	refs, objects, err := listRepo(dir)
	if err != nil {
		return 0, err
	}
	// Directories git expects even when empty.
	for _, d := range []string{"objects", "refs", "refs/heads", "refs/tags"} {
		if err := tw.WriteHeader(&tar.Header{Name: prefix + "/" + d + "/", Mode: 0o750, Typeflag: tar.TypeDir, ModTime: time.Now()}); err != nil {
			return 0, err
		}
	}
	var total int64
	added := map[string]bool{}
	copyAll := func(rels []string) error {
		for _, rel := range rels {
			if added[rel] {
				continue
			}
			n, err := addFile(tw, filepath.Join(dir, filepath.FromSlash(rel)), prefix+"/"+rel)
			if errors.Is(err, fs.ErrNotExist) {
				continue // e.g. a loose object packed by gc meanwhile
			}
			if err != nil {
				return err
			}
			added[rel] = true
			total += n
		}
		return nil
	}
	if err := copyAll(append(refs, objects...)); err != nil {
		return total, err
	}
	for range 3 {
		_, objects, err := listRepo(dir)
		if err != nil {
			return total, err
		}
		fresh := slices.DeleteFunc(objects, func(rel string) bool { return added[rel] })
		if len(fresh) == 0 {
			break
		}
		if err := copyAll(fresh); err != nil {
			return total, err
		}
	}
	return total, nil
}

// listRepo lists a repository's files (relative, slash-separated): refs
// and everything else first, then objects. In-flight writes are skipped.
func listRepo(dir string) (refs, objects []string, err error) {
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		base := path.Base(rel)
		switch {
		case strings.HasSuffix(base, ".lock"), strings.HasPrefix(base, "tmp_"), strings.HasPrefix(rel, "objects/incoming-"):
		case strings.HasPrefix(rel, "objects/"):
			objects = append(objects, rel)
		default:
			refs = append(refs, rel)
		}
		return nil
	})
	return refs, objects, err
}

// addStorage archives local blob storage, skipping health probes, files
// being written and (optionally) CI caches.
func addStorage(tw *tar.Writer, root string, noCaches bool, stats *Stats) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(root, p)
		key := filepath.ToSlash(rel)
		if d.IsDir() {
			if key == "health" || (noCaches && key == "caches") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".tmp-") {
			return nil
		}
		n, err := addFile(tw, p, "storage/"+key)
		if errors.Is(err, fs.ErrNotExist) {
			return nil // deleted by retention while we walked
		}
		if err != nil {
			return err
		}
		stats.Blobs++
		stats.BlobBytes += n
		return nil
	})
}

// addFile copies a file into the archive, sized from the open file so a
// file replaced meanwhile is still copied consistently.
func addFile(tw *tar.Writer, src, name string) (int64, error) {
	f, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	mode := int64(0o640)
	if fi.Mode().Perm()&0o100 != 0 {
		mode = 0o750
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: fi.Size(), ModTime: fi.ModTime(), Typeflag: tar.TypeReg}); err != nil {
		return 0, err
	}
	return io.CopyN(tw, f, fi.Size())
}

func writeFile(tw *tar.Writer, name string, data []byte, mode int64) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), ModTime: time.Now(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// ReadManifest reads only the manifest of a backup.
func ReadManifest(r io.Reader) (*Manifest, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("not a tgp backup (expected .tar.gz): %w", err)
	}
	return readManifest(tar.NewReader(gz))
}

func readManifest(tr *tar.Reader) (*Manifest, error) {
	h, err := tr.Next()
	if err != nil {
		return nil, fmt.Errorf("not a tgp backup: %w", err)
	}
	if h.Name != "manifest.json" {
		return nil, fmt.Errorf("not a tgp backup: first entry is %q, not manifest.json", h.Name)
	}
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(tr, 1<<20)).Decode(&m); err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if m.Format < 1 || m.Format > FormatVersion {
		return nil, fmt.Errorf("backup format %d isn't supported by this tgp version (supports up to %d)", m.Format, FormatVersion)
	}
	return &m, nil
}
