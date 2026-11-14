// Package runnercmd implements the tgp-runner command line.
package runnercmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/runner"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/version"
)

// defaultConfigPath is $TGP_RUNNER_CONFIG, /etc/tgp-runner/runner.toml for
// root, or ~/.config/tgp-runner/runner.toml.
func defaultConfigPath() string {
	if p := os.Getenv("TGP_RUNNER_CONFIG"); p != "" {
		return p
	}
	if os.Geteuid() == 0 {
		return "/etc/tgp-runner/runner.toml"
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "tgp-runner", "runner.toml")
	}
	return "runner.toml"
}

// Execute runs the CLI and returns the process exit code.
func Execute(args []string) int {
	var configPath string
	root := &cobra.Command{
		Use:   "tgp-runner",
		Short: "Run tgp CI jobs in Docker containers",
		Long: `tgp-runner connects to a tgp server, takes CI jobs and runs every job in
a Docker container (plus its service containers) with the CPU, memory and
process limits the server sets.

  1. An admin creates a registration token (Admin → Runners → Add runner).
  2. tgp-runner register --url https://git.example.com --token tgp_reg_...
  3. tgp-runner run`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Version,
	}
	root.SetVersionTemplate(version.String("tgp-runner") + "\n")
	root.PersistentFlags().StringVarP(&configPath, "config", "c", defaultConfigPath(), "config file")
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, _ []string) { fmt.Fprintln(cmd.OutOrStdout(), version.String("tgp-runner")) },
	})
	root.AddCommand(registerCmd(&configPath), runCmd(&configPath))
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func registerCmd(configPath *string) *cobra.Command {
	var (
		serverURL, token, name, labels, workDir, docker string
		capacity                                        int
		force                                           bool
	)
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register this runner with a tgp server and write its config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if serverURL == "" || token == "" {
				return errors.New("--url and --token are required (an admin creates tokens under Admin → Runners)")
			}
			u, err := url.Parse(serverURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("--url must be the server's address, like https://git.example.com")
			}
			if _, err := os.Stat(*configPath); err == nil && !force {
				return fmt.Errorf("%s already exists; use --force to replace it (the old registration stays until an admin removes it)", *configPath)
			}
			if name == "" {
				name, _ = os.Hostname()
			}
			lbls := runner.DefaultLabels()
			if labels != "" {
				lbls = nil
				for _, l := range strings.Split(labels, ",") {
					if l = strings.TrimSpace(l); l != "" {
						lbls = append(lbls, l)
					}
				}
			}
			c := runner.NewClient(serverURL, "")
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			resp, err := c.Register(ctx, runnerapi.RegisterRequest{
				Token: token, Name: name, Labels: lbls, Capacity: capacity,
				Version: version.Version, OS: runtime.GOOS, Arch: runtime.GOARCH,
			})
			if err != nil {
				return err
			}
			cfg := &runner.Config{URL: strings.TrimRight(serverURL, "/"), Token: resp.Token, ID: resp.ID, Name: resp.Name,
				Labels: resp.Labels, Capacity: capacity, WorkDir: workDir, Docker: docker, Pull: "missing"}
			if err := cfg.Save(*configPath); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Registered runner %q (id %d) with labels %s.\nConfig written to %s.\nStart it with: tgp-runner run --config %s\n",
				resp.Name, resp.ID, strings.Join(resp.Labels, ", "), *configPath, *configPath)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&serverURL, "url", "", "tgp server URL")
	f.StringVar(&token, "token", "", "registration token from Admin → Runners")
	f.StringVar(&name, "name", "", "runner name (default: hostname)")
	f.StringVar(&labels, "labels", "", "comma-separated labels for runs-on (default: docker,linux,<arch>)")
	f.IntVar(&capacity, "capacity", 1, "jobs to run at once")
	f.StringVar(&workDir, "work-dir", "", "directory for temporary checkouts (default: next to the config)")
	f.StringVar(&docker, "docker", "docker", "docker CLI binary")
	f.BoolVar(&force, "force", false, "overwrite an existing config")
	return cmd
}

func runCmd(configPath *string) *cobra.Command {
	var logFormat string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Take and run jobs until interrupted",
		Long: `Runs jobs until interrupted. The first interrupt (Ctrl-C or SIGTERM) stops
taking new jobs and waits for running ones; a second interrupt cancels them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var h slog.Handler = slog.NewTextHandler(os.Stderr, nil)
			if logFormat == "json" {
				h = slog.NewJSONHandler(os.Stderr, nil)
			}
			log := slog.New(h)
			cfg, err := runner.LoadConfig(*configPath)
			if err != nil {
				return err
			}
			d := &runner.Docker{Binary: cfg.Docker, RunnerID: cfg.ID, Pull: cfg.Pull}
			if err := d.Check(cmd.Context()); err != nil {
				return fmt.Errorf("can't reach Docker (every job runs in a container): %w", err)
			}
			ctx, stop := context.WithCancel(cmd.Context())
			jobCtx, cancelJobs := context.WithCancel(context.Background())
			defer cancelJobs()
			sigs := make(chan os.Signal, 2)
			signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigs
				log.Info("interrupt: finishing running jobs; interrupt again to cancel them")
				stop()
				<-sigs
				log.Info("interrupt: cancelling running jobs")
				cancelJobs()
			}()
			r := runner.New(*cfg, d, log, version.Version)
			return r.Run(ctx, jobCtx)
		},
	}
	cmd.Flags().StringVar(&logFormat, "log-format", "text", "text or json")
	return cmd
}
