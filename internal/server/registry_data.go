package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/workflow"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/registry"
)

const registrySettingsKey = "registry_settings"

// registrySettings are the admin-editable registry limits, stored as JSON
// in server_settings.
type registrySettings struct {
	// QuotaMB caps the blobs each repository's images hold (0 = no cap).
	QuotaMB int `json:"quota_mb"`
	// UntaggedRetentionDays: untagged manifests (not part of a tagged
	// index) older than this are deleted.
	UntaggedRetentionDays int `json:"untagged_retention_days"`
	// KeepLastTags > 0 keeps only the newest N tags matching
	// KeepLastTagsPatterns per image; older ones are deleted (their
	// manifests then age out as untagged).
	KeepLastTags         int      `json:"keep_last_tags"`
	KeepLastTagsPatterns []string `json:"keep_last_tags_patterns"`
}

func defaultRegistrySettings() registrySettings {
	return registrySettings{QuotaMB: 10240, UntaggedRetentionDays: 7, KeepLastTags: 0, KeepLastTagsPatterns: []string{"g*"}}
}

func (c registrySettings) validate() error {
	switch {
	case c.QuotaMB < 0:
		return apierr.Validation("quota_mb can't be negative")
	case c.UntaggedRetentionDays < 1:
		return apierr.Validation("untagged_retention_days must be at least 1")
	case c.KeepLastTags < 0:
		return apierr.Validation("keep_last_tags can't be negative")
	case len(c.KeepLastTagsPatterns) > 20:
		return apierr.Validation("at most 20 keep_last_tags_patterns")
	}
	for _, p := range c.KeepLastTagsPatterns {
		if strings.TrimSpace(p) == "" || len(p) > 128 {
			return apierr.Validation("keep_last_tags_patterns entries must be 1-128 characters")
		}
	}
	return nil
}

// regState caches the registry settings.
type regState struct {
	mu  sync.Mutex
	v   registrySettings
	at  time.Time
	set bool
}

func (s *Server) registrySettings(ctx context.Context) (registrySettings, error) {
	c := &s.reg
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.set && time.Since(c.at) < 5*time.Second {
		return c.v, nil
	}
	v := defaultRegistrySettings()
	var row model.ServerSetting
	err := s.db.NewSelect().Model(&row).Where("key = ?", registrySettingsKey).Scan(ctx)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return v, err
	default:
		if err := json.Unmarshal([]byte(row.Value), &v); err != nil {
			s.log.Warn("invalid stored registry settings; using defaults", "err", err)
			v = defaultRegistrySettings()
		}
	}
	c.v, c.at, c.set = v, time.Now(), true
	return v, nil
}

func (s *Server) saveRegistrySettings(ctx context.Context, v registrySettings) error {
	b, _ := json.Marshal(v)
	row := &model.ServerSetting{Key: registrySettingsKey, Value: string(b), UpdatedAt: time.Now().UTC()}
	if _, err := s.db.NewInsert().Model(row).On("CONFLICT (key) DO UPDATE").Set("value = EXCLUDED.value, updated_at = EXCLUDED.updated_at").Exec(ctx); err != nil {
		return err
	}
	s.reg.mu.Lock()
	s.reg.v, s.reg.at, s.reg.set = v, time.Now(), true
	s.reg.mu.Unlock()
	return nil
}

// regHost is the registry's host[:port] for display (docker pull lines),
// from the public base URL.
func (s *Server) regHost(ctx context.Context) string {
	return registryHost(s.publicBaseURL(ctx))
}

// hooksPackage queues a package webhook (published or deleted).
func (s *Server) hooksPackage(ctx context.Context, t *regTarget, m *model.RegistryManifest, tag, action string, actor *model.User) {
	if !s.hasHooks(ctx, t.repo.ID, "package") {
		return
	}
	oi := s.loadOrgInfo(ctx)
	path := registry.ImagePath(oi.owner, t.repo.Name, t.image)
	ref := path
	if h := registryHost(oi.base); h != "" {
		ref = h + "/" + path
	}
	pkg := map[string]any{
		"name": t.image, "path": path, "image": ref, "digest": m.Digest, "media_type": m.MediaType, "size": m.ImageSize,
		"html_url": strings.TrimRight(oi.base, "/") + "/" + t.repo.Name + "/packages",
	}
	if tag != "" {
		pkg["tag"] = tag
		pkg["image"] = ref + ":" + tag
	}
	s.emit(ctx, nil, t.repo, actor, "package", action, map[string]any{"package": pkg})
}

// registryGC applies retention and frees storage: stale uploads, surplus
// tags, old untagged manifests, blob links nothing uses and blobs no image
// links any more. Blob links and blobs get a grace period so pushes in
// progress (layers first, manifest last) aren't collected.
func (s *Server) registryGC(ctx context.Context) error {
	rs, err := s.registrySettings(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	// 1. Uploads nobody finished within a day.
	var stale []struct {
		model.RegistryUpload
		RepoID int64 `bun:"repo_id"`
	}
	if err := s.db.NewSelect().TableExpr("registry_uploads AS ru").Join("JOIN registry_images AS ri ON ri.id = ru.image_id").
		ColumnExpr("ru.*, ri.repo_id").Where("ru.updated_at < ?", now.Add(-24*time.Hour)).Limit(500).Scan(ctx, &stale); err != nil {
		return fmt.Errorf("stale uploads: %w", err)
	}
	for i := range stale {
		s.regDropUpload(ctx, stale[i].RepoID, &stale[i].RegistryUpload)
	}
	// 2. keep_last_tags.
	if rs.KeepLastTags > 0 && len(rs.KeepLastTagsPatterns) > 0 {
		var tags []*model.RegistryTag
		if err := s.db.NewSelect().Model(&tags).Order("image_id", "updated_at DESC", "id DESC").Scan(ctx); err != nil {
			return err
		}
		seen := map[int64]int{}
		var drop []int64
		for _, t := range tags {
			match := false
			for _, p := range rs.KeepLastTagsPatterns {
				if workflow.MatchGlob(p, t.Name) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
			seen[t.ImageID]++
			if seen[t.ImageID] > rs.KeepLastTags {
				drop = append(drop, t.ID)
			}
		}
		for len(drop) > 0 {
			n := min(len(drop), 500)
			if _, err := s.db.NewDelete().Model((*model.RegistryTag)(nil)).Where("id IN (?)", bun.In(drop[:n])).Exec(ctx); err != nil {
				return err
			}
			drop = drop[n:]
		}
	}
	// 3. Untagged manifests past retention that no index in their image
	// lists. Deleting an index can orphan its children, hence the loop.
	cutoff := now.Add(-time.Duration(rs.UntaggedRetentionDays) * 24 * time.Hour)
	for i := 0; i < 5; i++ {
		res, err := s.db.NewDelete().Model((*model.RegistryManifest)(nil)).
			Where("created_at < ?", cutoff).
			Where("NOT EXISTS (SELECT 1 FROM registry_tags t WHERE t.manifest_id = rm.id)").
			Where(`NOT EXISTS (SELECT 1 FROM registry_manifest_refs r JOIN registry_manifests p ON p.id = r.manifest_id
				WHERE p.image_id = rm.image_id AND r.digest = rm.digest AND r.kind = 'manifest')`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("untagged manifests: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			break
		}
	}
	// 4. Blob links no manifest of the image uses (abandoned pushes,
	// deleted manifests), after a day.
	if _, err := s.db.NewDelete().Model((*model.RegistryImageBlob)(nil)).
		Where("created_at < ?", now.Add(-24*time.Hour)).
		Where(`NOT EXISTS (SELECT 1 FROM registry_manifest_refs r JOIN registry_manifests m ON m.id = r.manifest_id
			WHERE m.image_id = rib.image_id AND r.digest = rib.digest)`).Exec(ctx); err != nil {
		return fmt.Errorf("blob links: %w", err)
	}
	// 5. Blobs nothing links or references, after an hour.
	var orphans []string
	if err := s.db.NewSelect().Model((*model.RegistryBlob)(nil)).Column("digest").
		Where("created_at < ?", now.Add(-time.Hour)).
		Where("NOT EXISTS (SELECT 1 FROM registry_image_blobs ib WHERE ib.digest = rb.digest)").
		Where("NOT EXISTS (SELECT 1 FROM registry_manifest_refs r WHERE r.digest = rb.digest)").
		Limit(1000).Scan(ctx, &orphans); err != nil {
		return fmt.Errorf("orphan blobs: %w", err)
	}
	for _, d := range orphans {
		// The row goes first, conditionally: an upload of the same blob
		// links it before checking for the row, so either it sees no row
		// and stores the blob again, or this delete matches nothing.
		res, err := s.db.NewDelete().Model((*model.RegistryBlob)(nil)).Where("digest = ?", d).
			Where("NOT EXISTS (SELECT 1 FROM registry_image_blobs ib WHERE ib.digest = rb.digest)").
			Where("NOT EXISTS (SELECT 1 FROM registry_manifest_refs r WHERE r.digest = rb.digest)").Exec(ctx)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		if err := s.store.Delete(ctx, blobStorageKey(d)); err != nil {
			s.log.Warn("registry: delete blob", "digest", d, "err", err)
		}
	}
	// 6. Images left empty.
	if _, err := s.db.NewDelete().Model((*model.RegistryImage)(nil)).
		Where("updated_at < ?", now.Add(-24*time.Hour)).
		Where("NOT EXISTS (SELECT 1 FROM registry_manifests m WHERE m.image_id = ri.id)").
		Where("NOT EXISTS (SELECT 1 FROM registry_image_blobs ib WHERE ib.image_id = ri.id)").
		Where("NOT EXISTS (SELECT 1 FROM registry_uploads u WHERE u.image_id = ri.id)").Exec(ctx); err != nil {
		return fmt.Errorf("empty images: %w", err)
	}
	return nil
}

// RunRegistryGC runs one garbage collection pass (tests).
func (s *Server) RunRegistryGC(ctx context.Context) error { return s.registryGC(ctx) }
