package servercmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/backup"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
)

type backupResult struct {
	Path     string           `json:"path,omitempty"`
	Bytes    int64            `json:"bytes,omitempty"`
	Manifest *backup.Manifest `json:"manifest"`
	Stats    *backup.Stats    `json:"stats"`
}

func newBackupCmd(o *rootOpts) *cobra.Command {
	var (
		out                        string
		noKey, noStorage, noCaches bool
		asJSON                     bool
	)
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Write a backup of the database, repositories, blobs and master key",
		Long: `Write a backup archive (.tar.gz) holding the database, every repository,
blobs from local storage (CI logs, artifacts, caches, release assets) and
the master key. It runs while the server is serving: the database is read
from a consistent snapshot and repositories are copied refs-first, so the
backup is consistent apart from pushes landing during the copy.

The archive holds the master key, password hashes and encrypted secrets:
store it like the server's own data. It's created readable only by you.

Blobs in S3 storage aren't included; back the bucket up with your object
store's own tools (versioning or replication).

Restore with: tgp-server restore FILE`,
		Example: `  tgp-server backup -c /etc/tgp/config.toml -o /backups/tgp-$(date +%F).tar.gz
  tgp-server backup --no-caches -o - | ssh backup-host 'cat > tgp.tar.gz'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, log, err := load(o)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			d, err := db.Open(ctx, cfg.Database)
			if err != nil {
				return err
			}
			defer d.Close()

			if out == "" {
				out = "tgp-backup-" + time.Now().Format("20060102-150405") + ".tar.gz"
			}
			var w io.Writer
			var f *os.File
			tmp := ""
			if out == "-" {
				w = cmd.OutOrStdout()
			} else {
				if f, err = os.CreateTemp(filepath.Dir(out), "."+filepath.Base(out)+".partial-*"); err != nil {
					return err
				}
				tmp = f.Name()
				defer os.Remove(tmp) // no-op after the rename
				if err := f.Chmod(0o600); err != nil {
					f.Close()
					return err
				}
				w = f
			}
			cw := &countingWriter{w: w}
			m, st, err := backup.Create(ctx, cw, backup.Options{Config: cfg, DB: d, Log: log, NoMasterKey: noKey, NoStorage: noStorage, NoCaches: noCaches})
			if f != nil {
				if err == nil {
					err = f.Sync()
				}
				if cerr := f.Close(); err == nil {
					err = cerr
				}
				if err == nil {
					err = os.Rename(tmp, out)
				}
			}
			if err != nil {
				return fmt.Errorf("backup failed: %w", err)
			}
			res := backupResult{Manifest: m, Stats: st, Bytes: cw.n}
			if out != "-" {
				res.Path = out
			}
			if m.StorageType == "s3" {
				log.Warn("blobs are in S3 and aren't part of this backup; back up the bucket separately")
			}
			if asJSON && out != "-" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Backup written%s: %s, %d tables (%d rows), %d repositories, %d blobs%s\n",
				pathSuffix(res.Path), humanBytes(cw.n), st.Tables, st.Rows, st.Repos, st.Blobs, keyNote(st.MasterKey))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&out, "output", "o", "", "archive to write, or - for stdout (default tgp-backup-<time>.tar.gz)")
	f.BoolVar(&noKey, "no-master-key", false, "leave the master key out; you'll need the server's key file to decrypt CI secrets and 2FA seeds")
	f.BoolVar(&noStorage, "no-storage", false, "leave out local blob storage (CI logs, artifacts, caches, release assets)")
	f.BoolVar(&noCaches, "no-caches", false, "leave out CI caches; jobs rebuild them")
	f.BoolVar(&asJSON, "json", false, "print the result as JSON")
	return cmd
}

func newRestoreCmd(o *rootOpts) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "restore FILE",
		Short: "Restore a backup into a new, empty installation",
		Long: `Restore a backup written by tgp-server backup. Stop the server first.

The target must be empty: a new database (delete the SQLite file or create
a new PostgreSQL database) and a repository directory without repositories.
The backup may come from a different database: a SQLite backup restores
into PostgreSQL and the other way round. Blobs go to the configured
storage, local or S3.

A backup from an older tgp version is restored at its own schema and then
migrated. A backup from a newer version is refused.

The backup's master key is written to master_key_file; an existing,
different key is kept next to it as master.key.replaced-<time>.`,
		Example: `  systemctl stop tgp
  tgp-server restore -c /etc/tgp/config.toml /backups/tgp-2026-11-14.tar.gz
  systemctl start tgp`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, log, err := load(o)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			var r io.Reader = cmd.InOrStdin()
			if args[0] != "-" {
				f, err := os.Open(args[0])
				if err != nil {
					return err
				}
				defer f.Close()
				r = f
			}
			for _, dir := range []string{cfg.DataDir, cfg.Repos.Root} {
				if err := os.MkdirAll(dir, 0o750); err != nil {
					return fmt.Errorf("create %s: %w", dir, err)
				}
			}
			d, err := db.Open(ctx, cfg.Database)
			if err != nil {
				return err
			}
			defer d.Close()
			store, err := openStorage(ctx, cfg)
			if err != nil {
				return fmt.Errorf("storage: %w", err)
			}
			m, st, err := backup.Restore(ctx, r, backup.RestoreOptions{Config: cfg, DB: d, Store: store, Log: log})
			if err != nil {
				return fmt.Errorf("restore failed: %w", err)
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(backupResult{Manifest: m, Stats: st})
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Restored the backup from %s (tgp %s): %d tables (%d rows), %d repositories, %d blobs%s\n",
				m.CreatedAt.Local().Format("2006-01-02 15:04"), m.Version, st.Tables, st.Rows, st.Repos, st.Blobs, keyNote(st.MasterKey))
			if m.StorageType == "s3" {
				fmt.Fprintln(cmd.ErrOrStderr(), "The backup's blobs were in S3; point [storage.s3] at that bucket (or a restored copy).")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the result as JSON")
	return cmd
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func pathSuffix(p string) string {
	if p == "" {
		return ""
	}
	return " to " + p
}

func keyNote(included bool) string {
	if included {
		return ", master key"
	}
	return ", no master key"
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
