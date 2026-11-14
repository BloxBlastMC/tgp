package cli

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

func newAdminHealthCmd(a *app) *cobra.Command {
	var (
		refresh bool
		repos   int
	)
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Show server health, disk use and queue lengths",
		Long: `Show the server's health checks, disks, blob storage, CI and webhook
queues, and the storage each repository uses.

Exits 1 when a check fails (status "fail") and 0 otherwise, so it works
in monitoring scripts. Warnings don't change the exit code.`,
		Example: `  tgp admin health
  tgp admin health --repos 0          # all repositories
  tgp admin health --json --jq '.checks[] | select(.status != "ok")'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.GetServerHealthWithResponse(cmd.Context(), &apigen.GetServerHealthParams{Refresh: &refresh})
			if err != nil {
				return err
			}
			h, err := must(r.JSON200, "health")
			if err != nil {
				return err
			}
			if err := a.print(cmd, h, func(w io.Writer) error { return printHealth(a, w, h, repos) }); err != nil {
				return err
			}
			if h.Status == "fail" {
				return &Error{Code: "unhealthy", Exit: ExitError, Message: "a health check failed", silent: !a.out.jsonMode()}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "measure repository sizes on disk now instead of using figures up to 5 minutes old")
	cmd.Flags().IntVar(&repos, "repos", 10, "largest repositories to list (0 = all)")
	return withOutput(cmd, "ServerHealth")
}

func healthMark(a *app, s apigen.HealthStatus) string {
	switch s {
	case "fail":
		return a.io.Red("✗ fail")
	case "warn":
		return a.io.Yellow("! warn")
	}
	return a.io.Green("✓ ok")
}

func printHealth(a *app, w io.Writer, h *apigen.ServerHealth, maxRepos int) error {
	fmt.Fprintf(w, "Status: %s   tgp %s (%s), up %s\n\n", healthMark(a, h.Status), h.Version, h.Commit, (time.Duration(h.UptimeSeconds) * time.Second).String())
	rows := make([][]string, len(h.Checks))
	for i, c := range h.Checks {
		rows[i] = []string{healthMark(a, c.Status), c.Name, c.Message}
	}
	if err := printTable(w, a.io, []string{"status", "check", "detail"}, rows); err != nil {
		return err
	}

	fmt.Fprintln(w)
	db := h.Database
	dbSize := "?"
	if db.SizeBytes != nil {
		dbSize = humanSize(*db.SizeBytes)
	}
	ago := func(t *time.Time) string {
		if t == nil {
			return "-"
		}
		return time.Since(*t).Round(time.Second).String()
	}
	if err := printFields(w, a.io, [][2]string{
		{"database", fmt.Sprintf("%s, %s, %.1f ms, schema %s", db.Driver, dbSize, db.LatencyMs, db.SchemaVersion)},
		{"storage", fmt.Sprintf("%s: logs %s, artifacts %s (%d), caches %s (%d), release assets %s (%d)", h.Storage.Type,
			humanSize(h.Storage.LogsBytes), humanSize(h.Storage.ArtifactsBytes), h.Storage.ArtifactsCount,
			humanSize(h.Storage.CachesBytes), h.Storage.CachesCount, humanSize(h.Storage.ReleaseAssetsBytes), h.Storage.ReleaseAssetsCount)},
		{"ci", fmt.Sprintf("%d queued (oldest %s), %d running; runners %d online, %d offline, %d disabled; slots %d/%d busy",
			h.Ci.QueuedJobs, ago(h.Ci.OldestQueuedAt), h.Ci.RunningJobs, h.Ci.RunnersOnline, h.Ci.RunnersOffline, h.Ci.RunnersDisabled, h.Ci.SlotsBusy, h.Ci.SlotsTotal)},
		{"webhooks", fmt.Sprintf("%d hooks (%d active); %d pending (oldest %s), %d retrying, %d failed in 24h",
			h.Webhooks.Hooks, h.Webhooks.ActiveHooks, h.Webhooks.PendingDeliveries, ago(h.Webhooks.OldestPendingAt), h.Webhooks.RetryingDeliveries, h.Webhooks.FailedDeliveries24h)},
		{"counts", fmt.Sprintf("%d users (%d disabled), %d sessions, %d repos, %d open PRs, %d open issues",
			h.Counts.Users, h.Counts.DisabledUsers, h.Counts.Sessions, h.Counts.Repos, h.Counts.OpenPulls, h.Counts.OpenIssues)},
		{"host", fmt.Sprintf("%s %s/%s, %d CPUs, %d goroutines, %s memory", h.Host.Hostname, h.Host.Os, h.Host.Arch, h.Host.Cpus, h.Host.Goroutines, humanSize(h.Host.MemoryBytes))},
	}); err != nil {
		return err
	}

	if len(h.Disks) > 0 {
		fmt.Fprintln(w)
		rows = make([][]string, len(h.Disks))
		for i, d := range h.Disks {
			uses := ""
			for j, u := range d.Uses {
				if j > 0 {
					uses += ","
				}
				uses += string(u)
			}
			pct := 0.0
			if d.TotalBytes > 0 {
				pct = float64(d.TotalBytes-d.FreeBytes) / float64(d.TotalBytes) * 100
			}
			rows[i] = []string{d.Path, uses, fmt.Sprintf("%.0f%%", pct), humanSize(d.FreeBytes), humanSize(d.TotalBytes)}
		}
		if err := printTable(w, a.io, []string{"disk", "holds", "used", "free", "size"}, rows); err != nil {
			return err
		}
	}

	items := h.Repos.Items
	if maxRepos > 0 && len(items) > maxRepos {
		items = items[:maxRepos]
	}
	if len(items) > 0 {
		fmt.Fprintln(w)
		rows = make([][]string, len(items))
		for i, r := range items {
			rows[i] = []string{r.Name, humanSize(r.GitBytes), humanSize(r.LogsBytes), humanSize(r.ArtifactsBytes), humanSize(r.CachesBytes), humanSize(r.ReleaseAssetsBytes), humanSize(r.TotalBytes)}
		}
		if err := printTable(w, a.io, []string{"repo", "git", "logs", "artifacts", "caches", "releases", "total"}, rows); err != nil {
			return err
		}
		if len(items) < len(h.Repos.Items) {
			fmt.Fprintln(a.io.ErrOut, a.io.Dim(strconv.Itoa(len(h.Repos.Items)-len(items))+" more repositories; use --repos 0 to list all"))
		}
	}
	return nil
}
