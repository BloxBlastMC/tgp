// Package servercmd implements the tgp-server command line.
package servercmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/config"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/git"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/githook"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/secretbox"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/server"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/storage"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/version"
	"git.taile67276.ts.net/BloxBlast/tgp/web"
)

type rootOpts struct {
	configPath string
}

// Execute runs the CLI and returns the process exit code.
func Execute(args []string) int {
	cmd := newRootCmd()
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	o := &rootOpts{}
	root := &cobra.Command{
		Use:           "tgp-server",
		Short:         "tgp git platform server",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Version,
		// Running with no subcommand serves.
		RunE: func(cmd *cobra.Command, _ []string) error { return runServe(cmd.Context(), o) },
	}
	root.SetVersionTemplate(version.String("tgp-server") + "\n")
	root.PersistentFlags().StringVarP(&o.configPath, "config", "c", os.Getenv("TGP_CONFIG"), "path to config file (env TGP_CONFIG)")

	root.AddCommand(
		&cobra.Command{
			Use:   "serve",
			Short: "Run the server (default)",
			Args:  cobra.NoArgs,
			RunE:  func(cmd *cobra.Command, _ []string) error { return runServe(cmd.Context(), o) },
		},
		newMigrateCmd(o),
		&cobra.Command{
			Use:    "hook <pre-receive|post-receive>",
			Short:  "Run a git hook (invoked by git during pushes)",
			Hidden: true,
			Args:   cobra.ExactArgs(1),
			Run: func(cmd *cobra.Command, args []string) {
				os.Exit(githook.Run(args[0], os.Stdin, os.Stderr))
			},
		},
		&cobra.Command{
			Use:   "version",
			Short: "Print version information",
			Args:  cobra.NoArgs,
			Run:   func(cmd *cobra.Command, _ []string) { fmt.Fprintln(cmd.OutOrStdout(), version.String("tgp-server")) },
		},
	)
	return root
}

func newMigrateCmd(o *rootOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply database migrations and exit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, log, err := load(o)
			if err != nil {
				return err
			}
			d, err := db.Open(cmd.Context(), cfg.Database)
			if err != nil {
				return err
			}
			defer d.Close()
			return db.Migrate(cmd.Context(), d, log)
		},
	}
	var asJSON bool
	status := &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := load(o)
			if err != nil {
				return err
			}
			d, err := db.Open(cmd.Context(), cfg.Database)
			if err != nil {
				return err
			}
			defer d.Close()
			st, err := db.Status(cmd.Context(), d)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(st)
			}
			for _, m := range st {
				mark := "pending"
				if m.Applied {
					mark = "applied"
				}
				fmt.Fprintf(out, "%-8s %s\n", mark, m.Name)
			}
			return nil
		},
	}
	status.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	cmd.AddCommand(status)
	return cmd
}

func load(o *rootOpts) (*config.Config, *slog.Logger, error) {
	cfg, err := config.Load(o.configPath)
	if err != nil {
		return nil, nil, err
	}
	return cfg, newLogger(cfg.Log), nil
}

func newLogger(c config.LogConfig) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(c.Level))
	opts := &slog.HandlerOptions{Level: lvl}
	if c.Format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func runServe(parent context.Context, o *rootOpts) error {
	cfg, log, err := load(o)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("starting tgp-server", "version", version.Version, "commit", version.Commit, "db", cfg.Database.Driver, "data_dir", cfg.DataDir)
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
	if err := db.Migrate(ctx, d, log); err != nil {
		return err
	}

	key, created, err := secretbox.LoadOrCreateKey(cfg.Security.MasterKeyFile)
	if err != nil {
		return err
	}
	if created {
		log.Warn("generated a new master key; back it up, it encrypts 2FA seeds and CI secrets", "path", cfg.Security.MasterKeyFile)
	}
	box, err := secretbox.New(key)
	if err != nil {
		return err
	}

	g, err := git.New(cfg.Repos.GitBinary, filepath.Join(cfg.DataDir, "git-home"))
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find own executable for git hooks: %w", err)
	}
	hooksDir := filepath.Join(cfg.DataDir, "hooks")
	if err := githook.WriteScripts(hooksDir, exe); err != nil {
		return fmt.Errorf("install git hooks: %w", err)
	}

	store, err := openStorage(ctx, cfg)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	log.Info("blob storage", "type", cfg.Storage.Type)
	srv := server.New(server.Options{
		Config:   cfg,
		DB:       d,
		Logger:   log,
		Web:      server.SPAHandler(web.Dist()),
		Box:      box,
		Git:      g,
		HooksDir: hooksDir,
		Storage:  store,
	})
	tok, err := srv.EnsureSetupToken(ctx)
	if err != nil {
		return err
	}
	if tok != "" {
		printSetupBanner(cfg, tok)
	}
	if err := srv.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	log.Info("stopped")
	return nil
}

// printSetupBanner shows the one-time setup token. It goes straight to
// stderr (not through the logger) so it stands out and isn't filtered by
// log level.
func printSetupBanner(cfg *config.Config, tok string) {
	url := cfg.Server.BaseURL
	if url == "" {
		url = "http://localhost" + cfg.Server.Listen
		if cfg.Server.Listen != "" && cfg.Server.Listen[0] != ':' {
			url = "http://" + cfg.Server.Listen
		}
	}
	fmt.Fprintf(os.Stderr, `
  ┌──────────────────────────────────────────────────────────────┐
  │  tgp first-start setup                                       │
  │                                                              │
  │  Open %-55s│
  │  and enter this setup token:                                 │
  │                                                              │
  │      %-56s│
  │                                                              │
  │  The token changes every time the server restarts until      │
  │  setup is complete.                                          │
  └──────────────────────────────────────────────────────────────┘

`, url+"/setup", tok)
}

// openStorage opens the configured blob store. S3 is checked at startup so
// a wrong bucket or key fails fast instead of on the first upload.
func openStorage(ctx context.Context, cfg *config.Config) (storage.Store, error) {
	switch cfg.Storage.Type {
	case "s3":
		c := cfg.Storage.S3
		st, err := storage.NewS3(storage.S3Config{Endpoint: c.Endpoint, Region: c.Region, Bucket: c.Bucket, Prefix: c.Prefix,
			AccessKey: c.AccessKey, SecretKey: c.SecretKey, PathStyle: c.UsePathStyle, Insecure: c.Insecure})
		if err != nil {
			return nil, err
		}
		if err := st.Check(ctx); err != nil {
			return nil, fmt.Errorf("can't reach bucket %q: %w", c.Bucket, err)
		}
		return st, nil
	default:
		return storage.NewLocal(cfg.Storage.Local.Path)
	}
}
