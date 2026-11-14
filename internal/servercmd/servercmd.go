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
	"syscall"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/config"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/server"
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

	srv := server.New(server.Options{
		Config: cfg,
		DB:     d,
		Logger: log,
		Web:    server.SPAHandler(web.Dist()),
	})
	if err := srv.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	log.Info("stopped")
	return nil
}
