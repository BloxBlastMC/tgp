package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/db"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/version"
)

// repoSizeTTL is how long measured repository sizes are reused.
const repoSizeTTL = 5 * time.Minute

// Disk thresholds (fraction of the filesystem used).
const (
	diskWarnUsed = 0.90
	diskFailUsed = 0.95
)

// healthState caches repository sizes on disk, which take a directory walk
// per repository to measure.
type healthState struct {
	mu         sync.Mutex
	sizes      map[int64]int64
	measuredAt time.Time
}

func (a *api) GetServerHealth(ctx context.Context, req apigen.GetServerHealthRequestObject) (apigen.GetServerHealthResponseObject, error) {
	refresh := req.Params.Refresh != nil && *req.Params.Refresh
	h, err := a.s.buildHealth(ctx, refresh)
	if err != nil {
		return nil, err
	}
	return apigen.GetServerHealth200JSONResponse(*h), nil
}

// health builds the health report. Failures of individual subsystems are
// reported in the result, not returned as errors; only a database that
// can't answer the basic queries fails the request.
func (s *Server) buildHealth(ctx context.Context, refreshSizes bool) (*apigen.ServerHealth, error) {
	now := time.Now().UTC()
	h := &apigen.ServerHealth{
		Version: version.Version, Commit: version.Commit,
		StartedAt: s.started, UptimeSeconds: int64(now.Sub(s.started).Seconds()), GeneratedAt: now,
		Host: hostInfo(),
	}
	var checks []apigen.HealthCheck
	add := func(name string, st apigen.HealthStatus, format string, args ...any) {
		checks = append(checks, apigen.HealthCheck{Name: name, Status: st, Message: fmt.Sprintf(format, args...)})
	}

	// Database.
	h.Database = s.dbHealth(ctx)
	if !h.Database.Ok {
		add("database", "fail", "Database unreachable: %s", deref(h.Database.Error))
		h.Checks, h.Status = sortChecks(checks)
		return h, nil
	}
	add("database", "ok", "%s answered in %.1f ms", h.Database.Driver, h.Database.LatencyMs)
	if h.Database.PendingMigrations > 0 {
		add("migrations", "warn", "%d database migrations are pending; restart the server or run tgp-server migrate", h.Database.PendingMigrations)
	} else {
		add("migrations", "ok", "Schema is up to date (%s)", h.Database.SchemaVersion)
	}

	// Blob storage.
	st, err := s.storageHealth(ctx)
	if err != nil {
		return nil, err
	}
	h.Storage = st
	if st.Ok {
		add("storage", "ok", "%s storage is readable and writable", st.Type)
	} else {
		add("storage", "fail", "%s storage failed a write/read/delete probe: %s", st.Type, deref(st.Error))
	}

	// Disks.
	h.Disks = s.diskHealth()
	if len(h.Disks) == 0 {
		add("disk", "ok", "Disk usage isn't available on this platform")
	}
	for _, d := range h.Disks {
		used := float64(d.TotalBytes-d.FreeBytes) / float64(max(d.TotalBytes, 1))
		uses := make([]string, len(d.Uses))
		for i, u := range d.Uses {
			uses[i] = string(u)
		}
		switch {
		case used >= diskFailUsed:
			add("disk", "fail", "%s (%s) is %.0f%% full, %s free", d.Path, strings.Join(uses, ", "), used*100, humanBytes(d.FreeBytes))
		case used >= diskWarnUsed:
			add("disk", "warn", "%s (%s) is %.0f%% full, %s free", d.Path, strings.Join(uses, ", "), used*100, humanBytes(d.FreeBytes))
		default:
			add("disk", "ok", "%s (%s) is %.0f%% full, %s free", d.Path, strings.Join(uses, ", "), used*100, humanBytes(d.FreeBytes))
		}
	}

	// Master key.
	if p := s.cfg.Security.MasterKeyFile; p != "" {
		fi, err := os.Stat(p)
		switch {
		case err != nil:
			add("master_key", "warn", "Master key file %s can't be read: %v", p, err)
		case fi.Mode().Perm()&0o077 != 0:
			add("master_key", "warn", "Master key file %s is readable by other users (mode %04o); chmod 600 it", p, fi.Mode().Perm())
		default:
			add("master_key", "ok", "Master key file is private")
		}
	}

	if h.Repos, err = s.repoHealth(ctx, refreshSizes); err != nil {
		return nil, err
	}
	if h.Ci, err = s.ciHealth(ctx, now); err != nil {
		return nil, err
	}
	switch {
	case h.Ci.QueuedJobs > 0 && h.Ci.RunnersOnline == 0:
		add("runners", "fail", "%d jobs are queued but no runner is online", h.Ci.QueuedJobs)
	case h.Ci.RunnersOffline > 0:
		add("runners", "warn", "%d of %d enabled runners are offline", h.Ci.RunnersOffline, h.Ci.RunnersOffline+h.Ci.RunnersOnline)
	case h.Ci.RunnersOnline == 0:
		add("runners", "ok", "No runners registered")
	default:
		add("runners", "ok", "%d runners online, %d of %d slots busy", h.Ci.RunnersOnline, h.Ci.SlotsBusy, h.Ci.SlotsTotal)
	}
	if h.Ci.OldestQueuedAt != nil && now.Sub(*h.Ci.OldestQueuedAt) > 30*time.Minute {
		add("ci_queue", "warn", "The oldest queued job has waited %s", now.Sub(*h.Ci.OldestQueuedAt).Round(time.Minute))
	} else {
		add("ci_queue", "ok", "%d queued, %d running", h.Ci.QueuedJobs, h.Ci.RunningJobs)
	}

	if h.Webhooks, err = s.webhookHealth(ctx, now); err != nil {
		return nil, err
	}
	if h.Webhooks.OldestPendingAt != nil && now.Sub(*h.Webhooks.OldestPendingAt) > time.Hour {
		add("webhooks", "warn", "%d deliveries pending, the oldest for %s (%d retrying)", h.Webhooks.PendingDeliveries, now.Sub(*h.Webhooks.OldestPendingAt).Round(time.Minute), h.Webhooks.RetryingDeliveries)
	} else {
		add("webhooks", "ok", "%d deliveries pending, %d failed in the last 24 hours", h.Webhooks.PendingDeliveries, h.Webhooks.FailedDeliveries24h)
	}

	if h.Counts, err = s.countsHealth(ctx, now); err != nil {
		return nil, err
	}
	h.Checks, h.Status = sortChecks(checks)
	return h, nil
}

var statusRank = map[apigen.HealthStatus]int{"ok": 0, "warn": 1, "fail": 2}

// sortChecks orders checks worst first (stable within a status) and
// returns the overall status.
func sortChecks(cs []apigen.HealthCheck) ([]apigen.HealthCheck, apigen.HealthStatus) {
	slices.SortStableFunc(cs, func(a, b apigen.HealthCheck) int { return statusRank[b.Status] - statusRank[a.Status] })
	if len(cs) == 0 {
		return []apigen.HealthCheck{}, "ok"
	}
	return cs, cs[0].Status
}

func hostInfo() apigen.HealthHost {
	hn, _ := os.Hostname()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return apigen.HealthHost{Hostname: hn, Os: runtime.GOOS, Arch: runtime.GOARCH, Cpus: runtime.NumCPU(),
		Goroutines: runtime.NumGoroutine(), MemoryBytes: int64(ms.Sys)}
}

func (s *Server) dbHealth(ctx context.Context) apigen.HealthDatabase {
	h := apigen.HealthDatabase{Driver: apigen.HealthDatabaseDriver(s.db.Driver)}
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	var one int
	if err := s.db.NewRaw("SELECT 1").Scan(pctx, &one); err != nil {
		msg := err.Error()
		h.Error = &msg
		return h
	}
	h.LatencyMs = float32(time.Since(start).Microseconds()) / 1000
	h.Ok = true
	var size int64
	var err error
	if s.db.IsPostgres() {
		err = s.db.NewRaw("SELECT pg_database_size(current_database())").Scan(ctx, &size)
	} else {
		err = s.db.NewRaw("SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()").Scan(ctx, &size)
	}
	if err == nil {
		h.SizeBytes = &size
	}
	if st, err := db.Status(ctx, s.db); err == nil {
		for _, m := range st {
			if m.Applied {
				h.SchemaVersion = m.Name
			} else {
				h.PendingMigrations++
			}
		}
	}
	return h
}

// storageHealth probes the blob store with a tiny write, read and delete,
// and sums what tgp keeps in it from the database.
func (s *Server) storageHealth(ctx context.Context) (apigen.HealthStorage, error) {
	h := apigen.HealthStorage{Type: apigen.HealthStorageType(s.cfg.Storage.Type)}
	if h.Type == "" {
		h.Type = "local"
	}
	if err := probeStore(ctx, s); err != nil {
		msg := err.Error()
		h.Error = &msg
	} else {
		h.Ok = true
	}
	var err error
	if h.LogsBytes, err = sumInt(ctx, s.db.NewSelect().Model((*model.Job)(nil)).ColumnExpr("COALESCE(SUM(log_bytes), 0)").Where("logs_deleted = ?", false)); err != nil {
		return h, err
	}
	sumCount := func(m any) (int64, int, error) {
		var r struct {
			Bytes int64 `bun:"bytes"`
			N     int   `bun:"n"`
		}
		err := s.db.NewSelect().Model(m).ColumnExpr("COALESCE(SUM(size), 0) AS bytes, COUNT(*) AS n").Scan(ctx, &r)
		return r.Bytes, r.N, err
	}
	if h.ArtifactsBytes, h.ArtifactsCount, err = sumCount((*model.Artifact)(nil)); err != nil {
		return h, err
	}
	if h.CachesBytes, h.CachesCount, err = sumCount((*model.CacheEntry)(nil)); err != nil {
		return h, err
	}
	if h.ReleaseAssetsBytes, h.ReleaseAssetsCount, err = sumCount((*model.ReleaseAsset)(nil)); err != nil {
		return h, err
	}
	h.TotalBytes = h.LogsBytes + h.ArtifactsBytes + h.CachesBytes + h.ReleaseAssetsBytes
	return h, nil
}

func count(ctx context.Context, q *bun.SelectQuery) (int, error) {
	n, err := q.Count(ctx)
	return int(n), err
}

type scanner interface {
	Scan(ctx context.Context, dest ...any) error
}

func sumInt(ctx context.Context, q scanner) (int64, error) {
	var n int64
	err := q.Scan(ctx, &n)
	return n, err
}

func probeStore(ctx context.Context, s *Server) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	key := "health/" + auth.RandomToken(8)
	want := []byte("tgp health probe")
	if err := s.store.Put(ctx, key, bytes.NewReader(want), int64(len(want))); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	defer func() { _ = s.store.Delete(context.WithoutCancel(ctx), key) }()
	rc, err := s.store.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("read back different content")
	}
	if err := s.store.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}

// diskHealth reports each filesystem holding tgp data once.
func (s *Server) diskHealth() []apigen.HealthDisk {
	type place struct {
		path string
		use  apigen.HealthDiskUses
	}
	places := []place{{s.cfg.DataDir, "data"}, {s.cfg.Repos.Root, "repos"}}
	if s.cfg.Storage.Type != "s3" {
		p := s.cfg.Storage.Local.Path
		if p == "" {
			p = filepath.Join(s.cfg.DataDir, "storage")
		}
		places = append(places, place{p, "storage"})
	}
	if s.db.Driver == "sqlite" && s.cfg.Database.DSN != "" && s.cfg.Database.DSN != ":memory:" && !strings.HasPrefix(s.cfg.Database.DSN, "file:") {
		places = append(places, place{filepath.Dir(s.cfg.Database.DSN), "database"})
	}
	var out []apigen.HealthDisk
	byDev := map[uint64]int{}
	for _, p := range places {
		if p.path == "" {
			continue
		}
		abs, err := filepath.Abs(p.path)
		if err != nil {
			continue
		}
		u, ok := diskUsage(abs)
		if !ok {
			continue
		}
		if i, seen := byDev[u.dev]; seen {
			if !slices.Contains(out[i].Uses, p.use) {
				out[i].Uses = append(out[i].Uses, p.use)
			}
			continue
		}
		byDev[u.dev] = len(out)
		out = append(out, apigen.HealthDisk{Path: abs, Uses: []apigen.HealthDiskUses{p.use},
			TotalBytes: u.total, FreeBytes: u.free, UsedBytes: u.total - u.free})
	}
	if out == nil {
		out = []apigen.HealthDisk{}
	}
	return out
}

type diskInfo struct {
	dev         uint64
	total, free int64 // free: available to unprivileged users
}

// repoHealth lists every repository with its git size on disk and the blob
// storage its CI and releases use.
func (s *Server) repoHealth(ctx context.Context, refresh bool) (apigen.HealthRepos, error) {
	var repos []*model.Repo
	if err := s.db.NewSelect().Model(&repos).Order("id").Scan(ctx); err != nil {
		return apigen.HealthRepos{}, err
	}
	sizes, measured := s.repoSizes(repos, refresh)
	type usage struct {
		RepoID int64 `bun:"repo_id"`
		Bytes  int64 `bun:"bytes"`
	}
	perRepo := func(m any, expr string, where ...string) (map[int64]int64, error) {
		var rows []usage
		q := s.db.NewSelect().Model(m).ColumnExpr("repo_id, COALESCE(SUM(" + expr + "), 0) AS bytes").Group("repo_id")
		for _, w := range where {
			q = q.Where(w, false)
		}
		if err := q.Scan(ctx, &rows); err != nil {
			return nil, err
		}
		out := make(map[int64]int64, len(rows))
		for _, r := range rows {
			out[r.RepoID] = r.Bytes
		}
		return out, nil
	}
	logs, err := perRepo((*model.Job)(nil), "log_bytes", "logs_deleted = ?")
	if err != nil {
		return apigen.HealthRepos{}, err
	}
	arts, err := perRepo((*model.Artifact)(nil), "size")
	if err != nil {
		return apigen.HealthRepos{}, err
	}
	caches, err := perRepo((*model.CacheEntry)(nil), "size")
	if err != nil {
		return apigen.HealthRepos{}, err
	}
	assets, err := perRepo((*model.ReleaseAsset)(nil), "size")
	if err != nil {
		return apigen.HealthRepos{}, err
	}
	h := apigen.HealthRepos{Count: len(repos), MeasuredAt: measured, Items: make([]apigen.RepoUsage, 0, len(repos))}
	for _, r := range repos {
		u := apigen.RepoUsage{Name: r.Name, Archived: r.Archived, PushedAt: r.PushedAt, GitBytes: sizes[r.ID],
			LogsBytes: logs[r.ID], ArtifactsBytes: arts[r.ID], CachesBytes: caches[r.ID], ReleaseAssetsBytes: assets[r.ID]}
		u.TotalBytes = u.GitBytes + u.LogsBytes + u.ArtifactsBytes + u.CachesBytes + u.ReleaseAssetsBytes
		h.GitBytes += u.GitBytes
		h.Items = append(h.Items, u)
	}
	slices.SortStableFunc(h.Items, func(a, b apigen.RepoUsage) int {
		switch {
		case a.TotalBytes > b.TotalBytes:
			return -1
		case a.TotalBytes < b.TotalBytes:
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return h, nil
}

// repoSizes returns the on-disk size of each repository, measuring again
// when the cache is older than repoSizeTTL, misses a repository or refresh
// is set.
func (s *Server) repoSizes(repos []*model.Repo, refresh bool) (map[int64]int64, time.Time) {
	hs := &s.healthCache
	hs.mu.Lock()
	defer hs.mu.Unlock()
	stale := refresh || hs.sizes == nil || time.Since(hs.measuredAt) > repoSizeTTL
	for _, r := range repos {
		if _, ok := hs.sizes[r.ID]; !ok {
			stale = true
			break
		}
	}
	if !stale {
		return hs.sizes, hs.measuredAt
	}
	sizes := make(map[int64]int64, len(repos))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, r := range repos {
		wg.Add(1)
		sem <- struct{}{}
		go func(r *model.Repo) {
			defer func() { <-sem; wg.Done() }()
			n := dirSize(s.repos.Path(r))
			mu.Lock()
			sizes[r.ID] = n
			mu.Unlock()
		}(r)
	}
	wg.Wait()
	hs.sizes, hs.measuredAt = sizes, time.Now().UTC()
	return hs.sizes, hs.measuredAt
}

// dirSize sums the sizes of the regular files under dir.
func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

func (s *Server) ciHealth(ctx context.Context, now time.Time) (apigen.HealthCI, error) {
	var h apigen.HealthCI
	var err error
	if h.QueuedJobs, err = count(ctx, s.db.NewSelect().Model((*model.Job)(nil)).Where("status = ?", model.JobQueued)); err != nil {
		return h, err
	}
	q := h.QueuedJobs
	if h.RunningJobs, err = count(ctx, s.db.NewSelect().Model((*model.Job)(nil)).Where("status = ?", model.JobInProgress)); err != nil {
		return h, err
	}
	if q > 0 {
		var oldest []time.Time
		if err := s.db.NewSelect().Model((*model.Job)(nil)).Column("queued_at").Where("status = ? AND queued_at IS NOT NULL", model.JobQueued).
			Order("queued_at").Limit(1).Scan(ctx, &oldest); err != nil {
			return h, err
		}
		if len(oldest) > 0 {
			t := oldest[0].UTC()
			h.OldestQueuedAt = &t
		}
	}
	cs, err := s.ciSettings(ctx)
	if err != nil {
		return h, err
	}
	var runners []*model.Runner
	if err := s.db.NewSelect().Model(&runners).Scan(ctx); err != nil {
		return h, err
	}
	for _, r := range runners {
		switch {
		case r.Disabled:
			h.RunnersDisabled++
		case runnerOnline(r, now, cs):
			h.RunnersOnline++
			h.SlotsTotal += r.Capacity
		default:
			h.RunnersOffline++
		}
	}
	h.SlotsBusy = h.RunningJobs
	return h, nil
}

func (s *Server) webhookHealth(ctx context.Context, now time.Time) (apigen.HealthWebhooks, error) {
	var h apigen.HealthWebhooks
	var err error
	if h.Hooks, err = count(ctx, s.db.NewSelect().Model((*model.Webhook)(nil))); err != nil {
		return h, err
	}
	if h.ActiveHooks, err = count(ctx, s.db.NewSelect().Model((*model.Webhook)(nil)).Where("active = ?", true)); err != nil {
		return h, err
	}
	pending := func() *bun.SelectQuery {
		return s.db.NewSelect().Model((*model.WebhookDelivery)(nil)).Where("status = ?", model.DeliveryPending)
	}
	if h.PendingDeliveries, err = count(ctx, pending()); err != nil {
		return h, err
	}
	if h.RetryingDeliveries, err = count(ctx, pending().Where("attempts > 0")); err != nil {
		return h, err
	}
	if h.FailedDeliveries24h, err = count(ctx, s.db.NewSelect().Model((*model.WebhookDelivery)(nil)).
		Where("status = ? AND created_at >= ?", model.DeliveryFailed, now.Add(-24*time.Hour))); err != nil {
		return h, err
	}
	if h.PendingDeliveries > 0 {
		var oldest []time.Time
		if err := pending().Column("created_at").Order("created_at").Limit(1).Scan(ctx, &oldest); err != nil {
			return h, err
		}
		if len(oldest) > 0 {
			t := oldest[0].UTC()
			h.OldestPendingAt = &t
		}
	}
	return h, nil
}

func (s *Server) countsHealth(ctx context.Context, now time.Time) (apigen.HealthCounts, error) {
	var h apigen.HealthCounts
	var err error
	if h.Users, err = count(ctx, s.db.NewSelect().Model((*model.User)(nil))); err != nil {
		return h, err
	}
	if h.DisabledUsers, err = count(ctx, s.db.NewSelect().Model((*model.User)(nil)).Where("disabled = ?", true)); err != nil {
		return h, err
	}
	if h.Sessions, err = count(ctx, s.db.NewSelect().Model((*model.Session)(nil)).Where("expires_at > ?", now)); err != nil {
		return h, err
	}
	if h.Repos, err = count(ctx, s.db.NewSelect().Model((*model.Repo)(nil))); err != nil {
		return h, err
	}
	if h.OpenPulls, err = count(ctx, s.db.NewSelect().Model((*model.Issue)(nil)).Where("is_pull = ? AND state = ?", true, model.StateOpen)); err != nil {
		return h, err
	}
	if h.OpenIssues, err = count(ctx, s.db.NewSelect().Model((*model.Issue)(nil)).Where("is_pull = ? AND state = ?", false, model.StateOpen)); err != nil {
		return h, err
	}
	return h, nil
}

// humanBytes formats n with a binary unit, e.g. "1.5 GiB".
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
