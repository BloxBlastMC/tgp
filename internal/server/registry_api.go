package server

import (
	"context"
	"sort"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/registry"
)

// The REST side of the registry: list and delete images from the UI and
// CLI, and the admin settings.

func (a *api) ListPackages(ctx context.Context, req apigen.ListPackagesRequestObject) (apigen.ListPackagesResponseObject, error) {
	s := a.s
	repo, _, err := s.repoFor(ctx, req.Repo, authz.Read)
	if err != nil {
		return nil, err
	}
	rs, err := s.registrySettings(ctx)
	if err != nil {
		return nil, err
	}
	var imgs []*model.RegistryImage
	if err := s.db.NewSelect().Model(&imgs).Where("repo_id = ?", repo.ID).Order("name").Scan(ctx); err != nil {
		return nil, err
	}
	ns := s.regNamespace(ctx)
	out := apigen.PackageList{Registry: s.regHost(ctx), Namespace: ns, Images: []apigen.Package{}, QuotaBytes: mb(rs.QuotaMB)}
	if out.UsageBytes, err = s.regRepoUsage(ctx, repo.ID); err != nil {
		return nil, err
	}
	for _, img := range imgs {
		p := apigen.Package{Name: img.Name, Path: registry.ImagePath(ns, repo.Name, img.Name), UpdatedAt: img.UpdatedAt}
		n, err := s.db.NewSelect().Model((*model.RegistryTag)(nil)).Where("image_id = ?", img.ID).Count(ctx)
		if err != nil {
			return nil, err
		}
		p.Tags = int(n)
		v, err := s.db.NewSelect().Model((*model.RegistryManifest)(nil)).Where("image_id = ?", img.ID).Count(ctx)
		if err != nil {
			return nil, err
		}
		p.Versions = int(v)
		if p.Versions == 0 && p.Tags == 0 {
			continue // only uploads in progress or leftovers
		}
		var size *int64
		if err := s.db.NewSelect().Model((*model.RegistryBlob)(nil)).ColumnExpr("SUM(rb.size)").
			Where("rb.digest IN (SELECT digest FROM registry_image_blobs WHERE image_id = ?)", img.ID).Scan(ctx, &size); err != nil {
			return nil, err
		}
		if size != nil {
			p.SizeBytes = *size
		}
		var latest []string
		if err := s.db.NewSelect().Model((*model.RegistryTag)(nil)).Column("name").Where("image_id = ?", img.ID).
			Order("updated_at DESC", "id DESC").Limit(1).Scan(ctx, &latest); err != nil {
			return nil, err
		}
		if len(latest) > 0 {
			p.LatestTag = &latest[0]
		}
		out.Images = append(out.Images, p)
	}
	return apigen.ListPackages200JSONResponse(out), nil
}

// packageTarget loads a repository image for the API.
func (s *Server) packageTarget(ctx context.Context, repoName string, name *string, need authz.Perm) (*regTarget, *model.RegistryImage, error) {
	repo, _, err := s.repoFor(ctx, repoName, need)
	if err != nil {
		return nil, nil, err
	}
	t := &regTarget{repo: repo}
	if name != nil {
		t.image = *name
	}
	t.name = registry.ImagePath(s.regNamespace(ctx), repo.Name, t.image)
	img, err := s.regImage(ctx, s.db, t)
	if err != nil {
		return nil, nil, err
	}
	if img == nil {
		return nil, nil, apierr.NotFound("no image %q in %s", t.image, repo.Name)
	}
	return t, img, nil
}

func (a *api) ListPackageVersions(ctx context.Context, req apigen.ListPackageVersionsRequestObject) (apigen.ListPackageVersionsResponseObject, error) {
	s := a.s
	_, img, err := s.packageTarget(ctx, req.Repo, req.Params.Name, authz.Read)
	if err != nil {
		return nil, err
	}
	var mans []*model.RegistryManifest
	if err := s.db.NewSelect().Model(&mans).ExcludeColumn("content").Where("image_id = ?", img.ID).
		Order("created_at DESC", "id DESC").Limit(1000).Scan(ctx); err != nil {
		return nil, err
	}
	var tags []*model.RegistryTag
	if err := s.db.NewSelect().Model(&tags).Where("image_id = ?", img.ID).Order("name").Scan(ctx); err != nil {
		return nil, err
	}
	byManifest := map[int64][]string{}
	for _, t := range tags {
		byManifest[t.ManifestID] = append(byManifest[t.ManifestID], t.Name)
	}
	// Children of an index are listed under it, not separately, unless
	// they're tagged themselves.
	children := map[string]bool{}
	var refs []*model.RegistryManifestRef
	if err := s.db.NewSelect().Model(&refs).Where("kind = 'manifest'").
		Where("manifest_id IN (SELECT id FROM registry_manifests WHERE image_id = ?)", img.ID).Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range refs {
		children[r.Digest] = true
	}
	users := map[int64]*model.User{}
	out := apigen.PackageVersionList{Versions: []apigen.PackageVersion{}}
	for _, m := range mans {
		if children[m.Digest] && len(byManifest[m.ID]) == 0 {
			continue
		}
		v := apigen.PackageVersion{Digest: m.Digest, MediaType: m.MediaType, Tags: byManifest[m.ID], SizeBytes: m.ImageSize,
			Platforms: jsonStrings(m.Platforms), CreatedAt: m.CreatedAt, PushedByJob: m.PushedByJob}
		if v.Tags == nil {
			v.Tags = []string{}
		}
		sort.Strings(v.Tags)
		switch {
		case m.ArtifactType != "":
			v.Kind = apigen.PackageVersionKindArtifact
			at := m.ArtifactType
			v.ArtifactType = &at
		case registry.IsIndex(m.MediaType):
			v.Kind = apigen.PackageVersionKindIndex
		default:
			v.Kind = apigen.PackageVersionKindImage
		}
		if m.PushedByID != nil {
			u, ok := users[*m.PushedByID]
			if !ok {
				u = new(model.User)
				if err := s.db.NewSelect().Model(u).Where("id = ?", *m.PushedByID).Scan(ctx); err != nil {
					u = nil
				}
				users[*m.PushedByID] = u
			}
			if u != nil {
				v.PushedBy = &apigen.UserSummary{Id: u.ID, Username: u.Username, DisplayName: u.DisplayName, Role: apigen.Role(u.Role)}
			}
		}
		out.Versions = append(out.Versions, v)
	}
	return apigen.ListPackageVersions200JSONResponse(out), nil
}

func (a *api) DeletePackageVersion(ctx context.Context, req apigen.DeletePackageVersionRequestObject) (apigen.DeletePackageVersionResponseObject, error) {
	s := a.s
	t, img, err := s.packageTarget(ctx, req.Repo, req.Params.Name, authz.Write)
	if err != nil {
		return nil, err
	}
	if t.repo.Archived {
		return nil, apierr.Forbidden("the repository is archived")
	}
	m, err := s.regFindManifest(ctx, img, req.Params.Digest)
	if err != nil {
		return nil, err
	}
	if m == nil || !registry.ValidDigest(req.Params.Digest) {
		return nil, apierr.NotFound("no version %s in %s", req.Params.Digest, t.name)
	}
	if err := s.deleteManifest(ctx, t, m); err != nil {
		return nil, err
	}
	if err := s.record(ctx, s.db, audit.PackageDelete, "package", t.name+"@"+m.Digest, map[string]any{"repo": t.repo.Name, "image": t.image, "digest": m.Digest}); err != nil {
		return nil, err
	}
	return apigen.DeletePackageVersion204Response{}, nil
}

func (a *api) DeletePackageTag(ctx context.Context, req apigen.DeletePackageTagRequestObject) (apigen.DeletePackageTagResponseObject, error) {
	s := a.s
	t, img, err := s.packageTarget(ctx, req.Repo, req.Params.Name, authz.Write)
	if err != nil {
		return nil, err
	}
	if t.repo.Archived {
		return nil, apierr.Forbidden("the repository is archived")
	}
	m, err := s.regFindManifest(ctx, img, req.Params.Tag)
	if err != nil {
		return nil, err
	}
	if m == nil || registry.ValidDigest(req.Params.Tag) {
		return nil, apierr.NotFound("no tag %q in %s", req.Params.Tag, t.name)
	}
	if _, err := s.db.NewDelete().Model((*model.RegistryTag)(nil)).Where("image_id = ? AND name = ?", img.ID, req.Params.Tag).Exec(ctx); err != nil {
		return nil, err
	}
	if err := s.record(ctx, s.db, audit.PackageDelete, "package", t.name+":"+req.Params.Tag, map[string]any{"repo": t.repo.Name, "image": t.image, "tag": req.Params.Tag}); err != nil {
		return nil, err
	}
	s.hooksPackage(ctx, t, m, req.Params.Tag, "deleted", mustPrincipal(ctx).User)
	return apigen.DeletePackageTag204Response{}, nil
}

func toAPIRegistrySettings(c registrySettings) apigen.RegistrySettings {
	p := c.KeepLastTagsPatterns
	if p == nil {
		p = []string{}
	}
	return apigen.RegistrySettings{QuotaMb: c.QuotaMB, UntaggedRetentionDays: c.UntaggedRetentionDays, KeepLastTags: c.KeepLastTags, KeepLastTagsPatterns: p}
}

func (a *api) GetRegistrySettings(ctx context.Context, _ apigen.GetRegistrySettingsRequestObject) (apigen.GetRegistrySettingsResponseObject, error) {
	c, err := a.s.registrySettings(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.GetRegistrySettings200JSONResponse(toAPIRegistrySettings(c)), nil
}

func (a *api) UpdateRegistrySettings(ctx context.Context, req apigen.UpdateRegistrySettingsRequestObject) (apigen.UpdateRegistrySettingsResponseObject, error) {
	s := a.s
	cur, err := s.registrySettings(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Body
	if p == nil {
		return nil, apierr.BadRequest("missing request body")
	}
	c := cur
	if p.QuotaMb != nil {
		c.QuotaMB = *p.QuotaMb
	}
	if p.UntaggedRetentionDays != nil {
		c.UntaggedRetentionDays = *p.UntaggedRetentionDays
	}
	if p.KeepLastTags != nil {
		c.KeepLastTags = *p.KeepLastTags
	}
	if p.KeepLastTagsPatterns != nil {
		c.KeepLastTagsPatterns = *p.KeepLastTagsPatterns
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	if err := s.saveRegistrySettings(ctx, c); err != nil {
		return nil, err
	}
	if err := s.record(ctx, s.db, audit.RegistrySettings, "registry", "settings", map[string]any{"before": cur, "after": c}); err != nil {
		return nil, err
	}
	return apigen.UpdateRegistrySettings200JSONResponse(toAPIRegistrySettings(c)), nil
}
