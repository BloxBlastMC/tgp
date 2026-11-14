package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/scheduler"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

const ciSettingsKey = "ci_settings"

// ciSettings are the admin-editable CI limits, stored as JSON in
// server_settings. Zero limits mean "no limit".
type ciSettings struct {
	MaxConcurrentJobs         int     `json:"max_concurrent_jobs"`
	MaxJobsPerRepo            int     `json:"max_jobs_per_repo"`
	MaxQueuedRunsPerRepo      int     `json:"max_queued_runs_per_repo"`
	RunnerMaxCPUPercent       int     `json:"runner_max_cpu_percent"`
	RunnerOfflineAfterSeconds int     `json:"runner_offline_after_seconds"`
	DefaultImage              string  `json:"default_image"`
	DefaultCPUs               float64 `json:"default_cpus"`
	MaxCPUs                   float64 `json:"max_cpus"`
	DefaultMemoryMB           int     `json:"default_memory_mb"`
	MaxMemoryMB               int     `json:"max_memory_mb"`
	DefaultPids               int     `json:"default_pids"`
	MaxPids                   int     `json:"max_pids"`
	DefaultTimeoutMinutes     int     `json:"default_timeout_minutes"`
	MaxTimeoutMinutes         int     `json:"max_timeout_minutes"`
	MaxLogMB                  int     `json:"max_log_mb"`
	LogRetentionDays          int     `json:"log_retention_days"`
	CancelSuperseded          bool    `json:"cancel_superseded"`
	ArtifactMaxMB             int     `json:"artifact_max_mb"`
	ArtifactRetentionDays     int     `json:"artifact_retention_days"`
	CacheQuotaMB              int     `json:"cache_quota_mb"`
	CacheRetentionDays        int     `json:"cache_retention_days"`
	ReleaseAssetMaxMB         int     `json:"release_asset_max_mb"`
}

func defaultCISettings() ciSettings {
	return ciSettings{
		MaxConcurrentJobs:         0,
		MaxJobsPerRepo:            4,
		MaxQueuedRunsPerRepo:      50,
		RunnerMaxCPUPercent:       90,
		RunnerOfflineAfterSeconds: 60,
		DefaultImage:              "debian:bookworm",
		DefaultCPUs:               2,
		MaxCPUs:                   4,
		DefaultMemoryMB:           4096,
		MaxMemoryMB:               8192,
		DefaultPids:               4096,
		MaxPids:                   16384,
		DefaultTimeoutMinutes:     60,
		MaxTimeoutMinutes:         360,
		MaxLogMB:                  32,
		LogRetentionDays:          90,
		CancelSuperseded:          true,
		ArtifactMaxMB:             2048,
		ArtifactRetentionDays:     90,
		CacheQuotaMB:              10240,
		CacheRetentionDays:        7,
		ReleaseAssetMaxMB:         2048,
	}
}

func (c ciSettings) limits() scheduler.Limits {
	return scheduler.Limits{MaxConcurrent: c.MaxConcurrentJobs, MaxPerRepo: c.MaxJobsPerRepo, MaxCPUPercent: float64(c.RunnerMaxCPUPercent)}
}

func mb(n int) int64 { return int64(n) << 20 }

func (c ciSettings) offlineAfter() time.Duration {
	return time.Duration(max(c.RunnerOfflineAfterSeconds, 20)) * time.Second
}

func (c ciSettings) validate() error {
	switch {
	case c.DefaultCPUs > c.MaxCPUs:
		return apierr.Validation("default_cpus (%g) can't exceed max_cpus (%g)", c.DefaultCPUs, c.MaxCPUs)
	case c.DefaultMemoryMB > c.MaxMemoryMB:
		return apierr.Validation("default_memory_mb (%d) can't exceed max_memory_mb (%d)", c.DefaultMemoryMB, c.MaxMemoryMB)
	case c.DefaultPids > c.MaxPids:
		return apierr.Validation("default_pids (%d) can't exceed max_pids (%d)", c.DefaultPids, c.MaxPids)
	case c.DefaultTimeoutMinutes > c.MaxTimeoutMinutes:
		return apierr.Validation("default_timeout_minutes (%d) can't exceed max_timeout_minutes (%d)", c.DefaultTimeoutMinutes, c.MaxTimeoutMinutes)
	case c.DefaultImage == "":
		return apierr.Validation("default_image can't be empty")
	case c.ArtifactMaxMB < 1 || c.ReleaseAssetMaxMB < 1:
		return apierr.Validation("artifact_max_mb and release_asset_max_mb must be at least 1")
	case c.ArtifactRetentionDays < 1 || c.CacheRetentionDays < 1:
		return apierr.Validation("artifact_retention_days and cache_retention_days must be at least 1")
	case c.CacheQuotaMB < 0:
		return apierr.Validation("cache_quota_mb can't be negative")
	}
	return nil
}

// settingsCache keeps the CI settings for a few seconds; other server
// nodes see changes within that time.
type settingsCache struct {
	mu  sync.Mutex
	v   ciSettings
	at  time.Time
	set bool
}

func (s *Server) ciSettings(ctx context.Context) (ciSettings, error) {
	c := &s.ci.settings
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.set && time.Since(c.at) < 5*time.Second {
		return c.v, nil
	}
	v := defaultCISettings()
	var row model.ServerSetting
	err := s.db.NewSelect().Model(&row).Where("key = ?", ciSettingsKey).Scan(ctx)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return v, err
	default:
		if err := json.Unmarshal([]byte(row.Value), &v); err != nil {
			s.log.Warn("invalid stored CI settings; using defaults", "err", err)
			v = defaultCISettings()
		}
	}
	c.v, c.at, c.set = v, time.Now(), true
	return v, nil
}

func toAPICISettings(c ciSettings) apigen.CISettings {
	return apigen.CISettings{
		MaxConcurrentJobs: c.MaxConcurrentJobs, MaxJobsPerRepo: c.MaxJobsPerRepo, MaxQueuedRunsPerRepo: c.MaxQueuedRunsPerRepo,
		RunnerMaxCpuPercent: c.RunnerMaxCPUPercent, RunnerOfflineAfterSeconds: c.RunnerOfflineAfterSeconds,
		DefaultImage: c.DefaultImage, DefaultCpus: float32(c.DefaultCPUs), MaxCpus: float32(c.MaxCPUs),
		DefaultMemoryMb: c.DefaultMemoryMB, MaxMemoryMb: c.MaxMemoryMB, DefaultPids: c.DefaultPids, MaxPids: c.MaxPids,
		DefaultTimeoutMinutes: c.DefaultTimeoutMinutes, MaxTimeoutMinutes: c.MaxTimeoutMinutes,
		MaxLogMb: c.MaxLogMB, LogRetentionDays: c.LogRetentionDays, CancelSuperseded: c.CancelSuperseded,
		ArtifactMaxMb: c.ArtifactMaxMB, ArtifactRetentionDays: c.ArtifactRetentionDays, CacheQuotaMb: c.CacheQuotaMB,
		CacheRetentionDays: c.CacheRetentionDays, ReleaseAssetMaxMb: c.ReleaseAssetMaxMB,
	}
}

func (a *api) GetCISettings(ctx context.Context, _ apigen.GetCISettingsRequestObject) (apigen.GetCISettingsResponseObject, error) {
	c, err := a.s.ciSettings(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.GetCISettings200JSONResponse(toAPICISettings(c)), nil
}

func (a *api) UpdateCISettings(ctx context.Context, req apigen.UpdateCISettingsRequestObject) (apigen.UpdateCISettingsResponseObject, error) {
	s := a.s
	cur, err := s.ciSettings(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Body
	c := cur
	setInt := func(dst *int, v *int) {
		if v != nil {
			*dst = *v
		}
	}
	setInt(&c.MaxConcurrentJobs, p.MaxConcurrentJobs)
	setInt(&c.MaxJobsPerRepo, p.MaxJobsPerRepo)
	setInt(&c.MaxQueuedRunsPerRepo, p.MaxQueuedRunsPerRepo)
	setInt(&c.RunnerMaxCPUPercent, p.RunnerMaxCpuPercent)
	setInt(&c.RunnerOfflineAfterSeconds, p.RunnerOfflineAfterSeconds)
	setInt(&c.DefaultMemoryMB, p.DefaultMemoryMb)
	setInt(&c.MaxMemoryMB, p.MaxMemoryMb)
	setInt(&c.DefaultPids, p.DefaultPids)
	setInt(&c.MaxPids, p.MaxPids)
	setInt(&c.DefaultTimeoutMinutes, p.DefaultTimeoutMinutes)
	setInt(&c.MaxTimeoutMinutes, p.MaxTimeoutMinutes)
	setInt(&c.MaxLogMB, p.MaxLogMb)
	setInt(&c.LogRetentionDays, p.LogRetentionDays)
	setInt(&c.ArtifactMaxMB, p.ArtifactMaxMb)
	setInt(&c.ArtifactRetentionDays, p.ArtifactRetentionDays)
	setInt(&c.CacheQuotaMB, p.CacheQuotaMb)
	setInt(&c.CacheRetentionDays, p.CacheRetentionDays)
	setInt(&c.ReleaseAssetMaxMB, p.ReleaseAssetMaxMb)
	if p.DefaultImage != nil {
		c.DefaultImage = *p.DefaultImage
	}
	if p.DefaultCpus != nil {
		c.DefaultCPUs = float64(*p.DefaultCpus)
	}
	if p.MaxCpus != nil {
		c.MaxCPUs = float64(*p.MaxCpus)
	}
	if p.CancelSuperseded != nil {
		c.CancelSuperseded = *p.CancelSuperseded
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(c)
	row := &model.ServerSetting{Key: ciSettingsKey, Value: string(b), UpdatedAt: time.Now().UTC()}
	if _, err := s.db.NewInsert().Model(row).On("CONFLICT (key) DO UPDATE").Set("value = EXCLUDED.value, updated_at = EXCLUDED.updated_at").Exec(ctx); err != nil {
		return nil, err
	}
	s.ci.settings.mu.Lock()
	s.ci.settings.v, s.ci.settings.at, s.ci.settings.set = c, time.Now(), true
	s.ci.settings.mu.Unlock()
	if err := s.record(ctx, s.db, audit.CISettingsUpdate, "ci", "settings", map[string]any{"before": cur, "after": c}); err != nil {
		return nil, err
	}
	s.ci.wake.notify() // raised limits may let queued jobs start
	return apigen.UpdateCISettings200JSONResponse(toAPICISettings(c)), nil
}
