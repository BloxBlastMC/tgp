package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/config"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/storage"
)

// RestoreOptions configure a restore.
type RestoreOptions struct {
	Config *config.Config
	DB     *db.DB
	// Store receives the backup's blobs. It may differ from the backup's
	// storage type: a local-storage backup restores into S3 too.
	Store storage.Store
	Log   *slog.Logger
}

// ErrNotEmpty means the restore target already holds data.
var ErrNotEmpty = errors.New("restore target isn't empty")

// Restore loads a backup into an empty installation: a database without
// users or repositories and a repository directory without repositories.
// Afterwards it applies any migrations newer than the backup.
func Restore(ctx context.Context, r io.Reader, o RestoreOptions) (*Manifest, *Stats, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, nil, fmt.Errorf("not a tgp backup (expected .tar.gz): %w", err)
	}
	tr := tar.NewReader(gz)
	m, err := readManifest(tr)
	if err != nil {
		return nil, nil, err
	}
	o.Log.Info("restoring backup", "version", m.Version, "created_at", m.CreatedAt, "from", m.Driver, "to", o.DB.Driver)

	if err := prepareTarget(ctx, o, m); err != nil {
		return nil, nil, err
	}

	stats := &Stats{}
	var ld *loader
	finishDB := func() error {
		if ld == nil {
			return nil
		}
		err := ld.finish(ctx)
		ld = nil
		if err == nil {
			o.Log.Info("database restored", "tables", stats.Tables, "rows", stats.Rows)
		}
		return err
	}
	defer func() {
		if ld != nil {
			ld.abort()
		}
	}()
	repos := map[string]bool{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return m, stats, fmt.Errorf("read backup: %w", err)
		}
		switch {
		case strings.HasPrefix(h.Name, "db/"):
			if ld == nil {
				if ld, err = newLoader(ctx, o, m); err != nil {
					return m, stats, err
				}
			}
			n, err := ld.loadTable(ctx, tr)
			if err != nil {
				return m, stats, fmt.Errorf("%s: %w", h.Name, err)
			}
			stats.Tables++
			stats.Rows += n
			continue
		}
		if err := finishDB(); err != nil {
			return m, stats, err
		}
		switch {
		case h.Name == "master.key":
			if err := restoreKey(tr, o); err != nil {
				return m, stats, err
			}
			stats.MasterKey = true
		case strings.HasPrefix(h.Name, "repos/"):
			name, n, err := restoreRepoEntry(tr, h, o.Config.Repos.Root)
			if err != nil {
				return m, stats, err
			}
			repos[name] = true
			stats.RepoBytes += n
		case strings.HasPrefix(h.Name, "storage/"):
			if h.Typeflag != tar.TypeReg {
				continue
			}
			key := strings.TrimPrefix(h.Name, "storage/")
			if err := o.Store.Put(ctx, key, tr, h.Size); err != nil {
				return m, stats, fmt.Errorf("store blob %s: %w", key, err)
			}
			stats.Blobs++
			stats.BlobBytes += h.Size
		default:
			o.Log.Warn("ignoring unknown backup entry", "name", h.Name)
		}
	}
	if err := finishDB(); err != nil {
		return m, stats, err
	}
	stats.Repos = len(repos)
	o.Log.Info("repositories restored", "repos", stats.Repos, "bytes", stats.RepoBytes)
	if m.Storage {
		o.Log.Info("blobs restored", "blobs", stats.Blobs, "bytes", stats.BlobBytes)
	}
	if !m.MasterKey {
		if _, err := os.Stat(o.Config.Security.MasterKeyFile); err != nil {
			o.Log.Warn("the backup has no master key and none is configured; CI secrets and 2FA seeds can't be decrypted", "path", o.Config.Security.MasterKeyFile)
		}
	}
	if err := db.Migrate(ctx, o.DB, o.Log); err != nil {
		return m, stats, err
	}
	return m, stats, nil
}

// prepareTarget brings the target database to the backup's schema and
// checks nothing would be overwritten.
func prepareTarget(ctx context.Context, o RestoreOptions, m *Manifest) error {
	var applied []string
	err := o.DB.NewRaw("SELECT name FROM bun_migrations").Scan(ctx, &applied)
	if err == nil {
		for _, a := range applied {
			if !slices.Contains(m.Migrations, a) {
				return fmt.Errorf("%w: the database already has migration %s, which is newer than the backup; restore into a new, empty database", ErrNotEmpty, a)
			}
		}
	}
	if err := db.MigrateTo(ctx, o.DB, o.Log, m.Migrations); err != nil {
		return err
	}
	for _, t := range []string{"users", "repos"} {
		var n int
		if err := o.DB.NewRaw("SELECT COUNT(*) FROM "+quoteIdent(t)).Scan(ctx, &n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: the database has %d %s; restore into a new, empty database", ErrNotEmpty, n, t)
		}
	}
	entries, err := os.ReadDir(o.Config.Repos.Root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".git") {
			return fmt.Errorf("%w: %s already holds repositories (%s)", ErrNotEmpty, o.Config.Repos.Root, e.Name())
		}
	}
	return nil
}

// loader loads tables inside one transaction. On SQLite, foreign key
// checks are off while loading and verified at the end; on PostgreSQL the
// load order keeps every reference valid.
type loader struct {
	o      RestoreOptions
	conn   *bun.Conn
	tx     bun.Tx
	tables []string
}

func newLoader(ctx context.Context, o RestoreOptions, m *Manifest) (*loader, error) {
	ld := &loader{o: o}
	var err error
	if ld.tables, err = listTables(ctx, o.DB.DB, o.DB.Driver); err != nil {
		return nil, err
	}
	if ld.tables, err = loadOrder(ctx, o.DB.DB, o.DB.Driver, ld.tables); err != nil {
		return nil, err
	}
	if !o.DB.IsPostgres() {
		c, err := o.DB.Conn(ctx)
		if err != nil {
			return nil, err
		}
		if _, err := c.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
			c.Close()
			return nil, err
		}
		ld.conn = &c
		if ld.tx, err = c.BeginTx(ctx, nil); err != nil {
			ld.closeConn()
			return nil, err
		}
	} else if ld.tx, err = o.DB.BeginTx(ctx, nil); err != nil {
		return nil, err
	}
	// Remove rows the migrations seeded (e.g. the org settings row); the
	// backup has its own.
	for i := len(ld.tables) - 1; i >= 0; i-- {
		if _, err := ld.tx.ExecContext(ctx, "DELETE FROM "+quoteIdent(ld.tables[i])); err != nil {
			ld.abort()
			return nil, fmt.Errorf("clear %s: %w", ld.tables[i], err)
		}
	}
	return ld, nil
}

// Batches are flushed at this many rows or bytes of values.
const (
	batchRows  = 200
	batchBytes = 4 << 20
)

func (ld *loader) loadTable(ctx context.Context, r io.Reader) (int64, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var h header
	if err := dec.Decode(&h); err != nil {
		return 0, fmt.Errorf("read table header: %w", err)
	}
	if !slices.Contains(ld.tables, h.Table) {
		return 0, fmt.Errorf("table %s doesn't exist in this database", h.Table)
	}
	kinds, err := columnKinds(ctx, ld.tx, ld.o.DB.Driver, h.Table)
	if err != nil {
		return 0, err
	}
	cols := make([]string, len(h.Columns))
	ks := make([]kind, len(h.Columns))
	for i, c := range h.Columns {
		k, ok := kinds[c]
		if !ok {
			return 0, fmt.Errorf("column %s.%s doesn't exist in this database", h.Table, c)
		}
		cols[i], ks[i] = quoteIdent(c), k
	}
	prefix := "INSERT INTO " + quoteIdent(h.Table) + " (" + strings.Join(cols, ", ") + ") VALUES "
	placeholder := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")"

	var (
		args  []any
		nrows int
		size  int
		total int64
	)
	flush := func() error {
		if nrows == 0 {
			return nil
		}
		q := prefix + strings.TrimSuffix(strings.Repeat(placeholder+", ", nrows), ", ")
		if _, err := ld.tx.NewRaw(q, args...).Exec(ctx); err != nil {
			return fmt.Errorf("insert into %s: %w", h.Table, err)
		}
		args, nrows, size = args[:0], 0, 0
		return nil
	}
	for {
		var row []any
		if err := dec.Decode(&row); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return total, fmt.Errorf("read row %d: %w", total+1, err)
		}
		if len(row) != len(cols) {
			return total, fmt.Errorf("row %d has %d values, want %d", total+1, len(row), len(cols))
		}
		for i, v := range row {
			cv, n, err := convert(v, ks[i])
			if err != nil {
				return total, fmt.Errorf("row %d, column %s: %w", total+1, h.Columns[i], err)
			}
			args = append(args, cv)
			size += n
		}
		nrows++
		total++
		if nrows >= batchRows || size >= batchBytes {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	return total, flush()
}

// finish commits the load, checks references (SQLite) and moves id
// sequences past the restored rows (PostgreSQL).
func (ld *loader) finish(ctx context.Context) error {
	if ld.o.DB.IsPostgres() {
		for _, t := range ld.tables {
			kinds, err := columnKinds(ctx, ld.tx, "postgres", t)
			if err != nil {
				ld.abort()
				return err
			}
			if _, ok := kinds["id"]; !ok {
				continue
			}
			q := fmt.Sprintf(`SELECT setval(pg_get_serial_sequence(?, 'id'), COALESCE((SELECT MAX(id) FROM %s), 0) + 1, false)`, quoteIdent(t))
			if _, err := ld.tx.NewRaw(q, quoteIdent(t)).Exec(ctx); err != nil {
				ld.abort()
				return fmt.Errorf("reset id sequence of %s: %w", t, err)
			}
		}
		return ld.tx.Commit()
	}
	var bad []struct {
		Table  string `bun:"table"`
		Parent string `bun:"parent"`
	}
	if err := ld.tx.NewRaw(`SELECT "table", parent FROM pragma_foreign_key_check`).Scan(ctx, &bad); err != nil {
		ld.abort()
		return fmt.Errorf("check references: %w", err)
	}
	if len(bad) > 0 {
		ld.abort()
		return fmt.Errorf("the backup has %d rows with broken references (first: %s → %s)", len(bad), bad[0].Table, bad[0].Parent)
	}
	if err := ld.tx.Commit(); err != nil {
		ld.closeConn()
		return err
	}
	_, err := ld.conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	ld.closeConn()
	return err
}

func (ld *loader) abort() {
	_ = ld.tx.Rollback()
	ld.closeConn()
}

func (ld *loader) closeConn() {
	if ld.conn != nil {
		// Restore the pragma before the connection goes back to the pool.
		_, _ = ld.conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")
		_ = ld.conn.Close()
		ld.conn = nil
	}
}

// convert turns a dumped JSON value into a Go value for a column of kind
// k. It also returns a rough size for batching.
func convert(v any, k kind) (any, int, error) {
	if v == nil {
		return nil, 4, nil
	}
	var (
		b     []byte
		t     *time.Time
		isBin bool
	)
	if obj, ok := v.(map[string]any); ok {
		switch {
		case obj["$b"] != nil:
			s, _ := obj["$b"].(string)
			d, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return nil, 0, err
			}
			b, isBin = d, true
		case obj["$t"] != nil:
			s, _ := obj["$t"].(string)
			tm, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				return nil, 0, err
			}
			t = &tm
		default:
			return nil, 0, fmt.Errorf("unexpected object %v", obj)
		}
	}
	switch k {
	case kindBool:
		switch x := v.(type) {
		case bool:
			return x, 1, nil
		case json.Number:
			return x.String() != "0", 1, nil
		case string:
			return x == "1" || strings.EqualFold(x, "true") || strings.EqualFold(x, "t"), 1, nil
		}
	case kindInt:
		switch x := v.(type) {
		case json.Number:
			n, err := x.Int64()
			return n, 8, err
		case bool:
			if x {
				return int64(1), 8, nil
			}
			return int64(0), 8, nil
		case string:
			n, err := strconv.ParseInt(x, 10, 64)
			return n, 8, err
		}
	case kindFloat:
		switch x := v.(type) {
		case json.Number:
			f, err := x.Float64()
			return f, 8, err
		case string:
			f, err := strconv.ParseFloat(x, 64)
			return f, 8, err
		}
	case kindTime:
		if t != nil {
			return *t, 32, nil
		}
		if s, ok := v.(string); ok {
			tm, err := parseTime(s)
			return tm, 32, err
		}
	case kindBytes:
		if isBin {
			return b, len(b) * 2, nil
		}
		if s, ok := v.(string); ok {
			return []byte(s), len(s) * 2, nil
		}
	default: // text
		switch x := v.(type) {
		case string:
			return x, len(x), nil
		case json.Number:
			return x.String(), len(x), nil
		case bool:
			return strconv.FormatBool(x), 5, nil
		}
		if isBin {
			return string(b), len(b), nil
		}
		if t != nil {
			return t.Format(time.RFC3339Nano), 32, nil
		}
	}
	return nil, 0, fmt.Errorf("can't store %T %v in a %s column", v, v, kindNames[k])
}

var kindNames = map[kind]string{kindText: "text", kindInt: "integer", kindFloat: "number", kindBool: "boolean", kindTime: "timestamp", kindBytes: "binary"}

func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised time %q", s)
}

// restoreKey installs the backup's master key, keeping a differing
// existing key next to it.
func restoreKey(r io.Reader, o RestoreOptions) error {
	key, err := io.ReadAll(io.LimitReader(r, 4096))
	if err != nil {
		return err
	}
	p := o.Config.Security.MasterKeyFile
	if old, err := os.ReadFile(p); err == nil {
		if bytes.Equal(old, key) {
			return nil
		}
		keep := p + ".replaced-" + time.Now().UTC().Format("20060102-150405")
		if err := os.Rename(p, keep); err != nil {
			return err
		}
		o.Log.Warn("replaced the existing master key with the backup's; the old one was kept", "path", keep)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	return os.WriteFile(p, key, 0o600)
}

// restoreRepoEntry writes one repository file or directory. It returns
// the repository's directory name and the bytes written.
func restoreRepoEntry(r io.Reader, h *tar.Header, root string) (string, int64, error) {
	rel := strings.TrimPrefix(h.Name, "repos/")
	repo, _, _ := strings.Cut(rel, "/")
	if !strings.HasSuffix(repo, ".git") || !filepath.IsLocal(filepath.FromSlash(strings.TrimSuffix(rel, "/"))) {
		return "", 0, fmt.Errorf("unsafe path in backup: %q", h.Name)
	}
	dst := filepath.Join(root, filepath.FromSlash(rel))
	switch h.Typeflag {
	case tar.TypeDir:
		return repo, 0, os.MkdirAll(dst, 0o750)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return "", 0, err
		}
		mode := os.FileMode(0o640)
		if h.Mode&0o100 != 0 {
			mode = 0o750
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return "", 0, err
		}
		n, err := io.Copy(f, r)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return repo, n, err
	}
	return repo, 0, nil
}
