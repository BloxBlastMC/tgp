package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/uptrace/bun"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/audit"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/auth"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/authz"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/workflow"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/registry"
)

// The container registry speaks the OCI Distribution API under /v2/.
// Clients authenticate with HTTP Basic (any username; a personal access
// token or a CI job token as the password) or a Bearer token. Pulling
// needs read access to the repository, pushing and deleting need write
// access (or a job token of a trusted run).

type regErr = registry.Error

func regErrorf(status int, code, format string, args ...any) *regErr {
	return registry.Errorf(status, code, format, args...)
}

var (
	errRegNameUnknown = func(name string) *regErr {
		return regErrorf(http.StatusNotFound, registry.CodeNameUnknown, "repository %s not found", name)
	}
	errRegBlobUnknown = func(d string) *regErr {
		return regErrorf(http.StatusNotFound, registry.CodeBlobUnknown, "blob %s not found", d)
	}
	errRegManifestUnknown = func(ref string) *regErr {
		return regErrorf(http.StatusNotFound, registry.CodeManifestUnknown, "manifest %s not found", ref)
	}
	errRegUploadUnknown = regErrorf(http.StatusNotFound, registry.CodeBlobUploadUnknown, "upload not found (it may have expired)")
)

// blobStorageKey is where a registry blob lives in storage.
func blobStorageKey(digest string) string {
	return "registry/blobs/sha256/" + strings.TrimPrefix(digest, "sha256:")
}

// uploadPartKey is where part n of an upload lives; parts sit under the
// repository's ID so deleting the repository removes them.
func uploadPartKey(repoID int64, id string, n int) string {
	return fmt.Sprintf("registry/uploads/%d/%s/%d", repoID, id, n)
}

// regTarget is the repository and image a request names.
type regTarget struct {
	name  string // the full repository name from the URL
	repo  *model.Repo
	image string // the part after <namespace>/<repo>, "" for the repo itself
}

// handleRegistry serves /v2/.
func (s *Server) handleRegistry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	route, ok := registry.ParseRoute(r.URL.Path)
	if !ok {
		regErrorf(http.StatusNotFound, registry.CodeUnsupported, "no registry endpoint at %s", r.URL.Path).Write(w, r)
		return
	}
	ctx := r.Context()
	if done, err := s.setupComplete(ctx); err != nil || !done {
		regErrorf(http.StatusServiceUnavailable, registry.CodeUnsupported, "this server hasn't been set up yet").Write(w, r)
		return
	}
	p, rerr := s.registryAuth(r)
	if rerr != nil {
		if rerr.Status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Basic realm="tgp"`)
		}
		rerr.Write(w, r)
		return
	}
	ctx = context.WithValue(ctx, principalKey, p)
	ctx = context.WithValue(ctx, httpKey, httpPair{w: w, r: r})
	r = r.WithContext(ctx)

	if route.Kind == registry.RouteBase {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
		return
	}
	if route.Kind == registry.RouteCatalog {
		s.regCatalog(w, r, p)
		return
	}
	t, rerr := s.regResolve(ctx, route.Name)
	if rerr != nil {
		rerr.Write(w, r)
		return
	}
	push := !(r.Method == http.MethodGet || r.Method == http.MethodHead)
	if rerr := s.regAccess(ctx, p, t, push); rerr != nil {
		rerr.Write(w, r)
		return
	}
	var err *regErr
	switch route.Kind {
	case registry.RouteBlob:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			err = s.regGetBlob(w, r, t, route.Ref)
		case http.MethodDelete:
			err = s.regDeleteBlob(w, r, t, route.Ref)
		default:
			err = regMethod(r)
		}
	case registry.RouteUploads:
		if r.Method != http.MethodPost {
			err = regMethod(r)
			break
		}
		err = s.regStartUpload(w, r, p, t)
	case registry.RouteUpload:
		switch r.Method {
		case http.MethodGet:
			err = s.regUploadStatus(w, r, t, route.Ref)
		case http.MethodPatch:
			err = s.regPatchUpload(w, r, t, route.Ref)
		case http.MethodPut:
			err = s.regFinishUpload(w, r, t, route.Ref)
		case http.MethodDelete:
			err = s.regCancelUpload(w, r, t, route.Ref)
		default:
			err = regMethod(r)
		}
	case registry.RouteManifest:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			err = s.regGetManifest(w, r, t, route.Ref)
		case http.MethodPut:
			err = s.regPutManifest(w, r, p, t, route.Ref)
		case http.MethodDelete:
			err = s.regDeleteManifest(w, r, p, t, route.Ref)
		default:
			err = regMethod(r)
		}
	case registry.RouteTags:
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			err = regMethod(r)
			break
		}
		err = s.regListTags(w, r, t)
	case registry.RouteReferrers:
		err = regErrorf(http.StatusNotFound, registry.CodeUnsupported, "the referrers API isn't supported")
	}
	if err != nil {
		err.Write(w, r)
	}
}

func regMethod(r *http.Request) *regErr {
	return regErrorf(http.StatusMethodNotAllowed, registry.CodeUnsupported, "%s isn't supported here", r.Method)
}

// registryAuth authenticates a registry request.
func (s *Server) registryAuth(r *http.Request) (*Principal, *regErr) {
	ctx := r.Context()
	token := bearerToken(r)
	if token == "" {
		if _, pw, ok := r.BasicAuth(); ok {
			token = pw
		}
	}
	if token == "" {
		return nil, regErrorf(http.StatusUnauthorized, registry.CodeUnauthorized, "authentication required: log in with a personal access token (docker login)")
	}
	ip := clientIP(r)
	if blocked, _ := s.ipLimiter.Blocked(ip); blocked {
		return nil, regErrorf(http.StatusTooManyRequests, registry.CodeTooManyRequests, "too many failed logins from this address; try again later")
	}
	p, err := s.principalFromToken(ctx, token)
	if err != nil {
		var ae *apierr.Error
		if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
			s.ipLimiter.Fail(ip)
			return nil, regErrorf(http.StatusUnauthorized, registry.CodeUnauthorized, "invalid or expired token")
		}
		s.log.Error("registry: authenticate", "err", err)
		return nil, regErrorf(http.StatusInternalServerError, registry.CodeUnsupported, "internal error")
	}
	if p.User.MustChangePassword {
		return nil, regErrorf(http.StatusForbidden, registry.CodeDenied, "change your temporary password in the web UI first")
	}
	return p, nil
}

// regNamespace is the first path component of image names.
func (s *Server) regNamespace(ctx context.Context) string {
	org, _ := s.loadOrg(ctx)
	name := ""
	if org != nil {
		name = org.Name
	}
	return registry.Namespace(name)
}

// regResolve maps a repository name to a tgp repository and image name.
func (s *Server) regResolve(ctx context.Context, name string) (*regTarget, *regErr) {
	if !registry.ValidName(name) {
		return nil, regErrorf(http.StatusBadRequest, registry.CodeNameInvalid, "invalid repository name %q", name)
	}
	parts := strings.SplitN(name, "/", 3)
	if len(parts) < 2 || parts[0] != s.regNamespace(ctx) {
		return nil, errRegNameUnknown(name)
	}
	repo, err := s.repos.Get(ctx, s.db, parts[1])
	if err != nil {
		return nil, errRegNameUnknown(name)
	}
	t := &regTarget{name: name, repo: repo}
	if len(parts) == 3 {
		t.image = parts[2]
	}
	return t, nil
}

// regAccess checks the caller may pull from (or push to) t.
func (s *Server) regAccess(ctx context.Context, p *Principal, t *regTarget, push bool) *regErr {
	perm, err := authz.RepoPerm(ctx, s.db, p.User, t.repo)
	if err != nil || perm < authz.Read {
		return errRegNameUnknown(t.name)
	}
	if !p.HasScope("read") {
		return regErrorf(http.StatusForbidden, registry.CodeDenied, "this token lacks the read scope")
	}
	if !push {
		return nil
	}
	if t.repo.Archived {
		return regErrorf(http.StatusForbidden, registry.CodeDenied, "the repository is archived")
	}
	if p.Method == MethodJob {
		return jobCanPush(p.Job)
	}
	if perm < authz.Write {
		return regErrorf(http.StatusForbidden, registry.CodeDenied, "pushing needs write access to %s", t.repo.Name)
	}
	if !p.HasScope("write") {
		return regErrorf(http.StatusForbidden, registry.CodeDenied, "this token lacks the write scope")
	}
	return nil
}

// jobCanPush: job tokens publish only from trusted runs (pushes to the
// default branch, manual dispatches, releases), unless the workflow set
// permissions: packages: read or none.
func jobCanPush(j *model.Job) *regErr {
	if !j.Trusted {
		return regErrorf(http.StatusForbidden, registry.CodeDenied,
			"this CI job can't push: only runs for pushes to the default branch, manual dispatches and releases can publish images")
	}
	switch parseDef(j).Packages {
	case workflow.PackagesRead, workflow.PackagesNone:
		return regErrorf(http.StatusForbidden, registry.CodeDenied, "this job's permissions don't allow pushing packages (packages: write)")
	}
	return nil
}

func (s *Server) regImage(ctx context.Context, db bun.IDB, t *regTarget) (*model.RegistryImage, error) {
	img := new(model.RegistryImage)
	err := db.NewSelect().Model(img).Where("repo_id = ? AND name = ?", t.repo.ID, t.image).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return img, err
}

// regEnsureImage returns t's image, creating it.
func (s *Server) regEnsureImage(ctx context.Context, t *regTarget) (*model.RegistryImage, error) {
	if img, err := s.regImage(ctx, s.db, t); img != nil || err != nil {
		return img, err
	}
	now := s.now()
	img := &model.RegistryImage{RepoID: t.repo.ID, Name: t.image, CreatedAt: now, UpdatedAt: now}
	if _, err := s.db.NewInsert().Model(img).On("CONFLICT (repo_id, name) DO NOTHING").Exec(ctx); err != nil {
		return nil, err
	}
	return s.regImage(ctx, s.db, t)
}

func regInternal(s *Server, what string, err error) *regErr {
	s.log.Error("registry: "+what, "err", err)
	return regErrorf(http.StatusInternalServerError, registry.CodeUnsupported, "internal error (%s)", what)
}

// ---- blobs ----

func (s *Server) regGetBlob(w http.ResponseWriter, r *http.Request, t *regTarget, digest string) *regErr {
	ctx := r.Context()
	if !registry.ValidDigest(digest) {
		return regErrorf(http.StatusBadRequest, registry.CodeDigestInvalid, "invalid digest %q (sha256 only)", digest)
	}
	img, err := s.regImage(ctx, s.db, t)
	if err != nil {
		return regInternal(s, "load image", err)
	}
	if img == nil {
		return errRegBlobUnknown(digest)
	}
	b := new(model.RegistryBlob)
	err = s.db.NewSelect().Model(b).Where("rb.digest = ?", digest).
		Where("EXISTS (SELECT 1 FROM registry_image_blobs ib WHERE ib.image_id = ? AND ib.digest = rb.digest)", img.ID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return errRegBlobUnknown(digest)
	} else if err != nil {
		return regInternal(s, "load blob", err)
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Length", strconv.FormatInt(b.Size, 10))
	h.Set("Docker-Content-Digest", digest)
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return nil
	}
	rc, err := s.store.Get(ctx, blobStorageKey(digest))
	if err != nil {
		return regInternal(s, "open blob", err)
	}
	defer rc.Close()
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
	return nil
}

// regDeleteBlob unlinks a blob from the image; the garbage collector
// frees it once nothing uses it.
func (s *Server) regDeleteBlob(w http.ResponseWriter, r *http.Request, t *regTarget, digest string) *regErr {
	ctx := r.Context()
	img, err := s.regImage(ctx, s.db, t)
	if err != nil {
		return regInternal(s, "load image", err)
	}
	if img == nil || !registry.ValidDigest(digest) {
		return errRegBlobUnknown(digest)
	}
	res, err := s.db.NewDelete().Model((*model.RegistryImageBlob)(nil)).Where("image_id = ? AND digest = ?", img.ID, digest).Exec(ctx)
	if err != nil {
		return regInternal(s, "unlink blob", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errRegBlobUnknown(digest)
	}
	w.WriteHeader(http.StatusAccepted)
	return nil
}

// regLinkBlob makes an existing blob available in img, checking the quota
// when the blob is new to the repository.
func (s *Server) regLinkBlob(ctx context.Context, t *regTarget, img *model.RegistryImage, digest string, size int64) *regErr {
	linked, err := s.db.NewSelect().Model((*model.RegistryImageBlob)(nil)).Where("image_id = ? AND digest = ?", img.ID, digest).Exists(ctx)
	if err != nil {
		return regInternal(s, "check blob", err)
	}
	if linked {
		return nil
	}
	if rerr := s.regCheckQuota(ctx, t.repo, digest, size); rerr != nil {
		return rerr
	}
	link := &model.RegistryImageBlob{ImageID: img.ID, Digest: digest, CreatedAt: s.now()}
	if _, err := s.db.NewInsert().Model(link).On("CONFLICT (image_id, digest) DO NOTHING").Exec(ctx); err != nil {
		return regInternal(s, "link blob", err)
	}
	return nil
}

// regCheckQuota fails if adding a blob of size would take the repository
// over its quota. Blobs the repository already has don't count again.
func (s *Server) regCheckQuota(ctx context.Context, repo *model.Repo, digest string, size int64) *regErr {
	rs, err := s.registrySettings(ctx)
	if err != nil {
		return regInternal(s, "settings", err)
	}
	if rs.QuotaMB <= 0 {
		return nil
	}
	used, err := s.regRepoUsage(ctx, repo.ID)
	if err != nil {
		return regInternal(s, "usage", err)
	}
	has, err := s.db.NewSelect().Model((*model.RegistryImageBlob)(nil)).Join("JOIN registry_images AS i ON i.id = rib.image_id").
		Where("i.repo_id = ? AND rib.digest = ?", repo.ID, digest).Exists(ctx)
	if err != nil {
		return regInternal(s, "usage", err)
	}
	if has {
		return nil
	}
	if used+size > mb(rs.QuotaMB) {
		return regErrorf(http.StatusForbidden, registry.CodeDenied,
			"the repository's registry quota is full (%d MiB used of %d MiB; this blob needs %d MiB): delete old images or ask an admin to raise registry quota_mb",
			used>>20, rs.QuotaMB, (size+(1<<20)-1)>>20)
	}
	return nil
}

// regCheckManifestQuota fails if the blobs the repository's manifests use,
// plus blobs, exceed the quota.
func (s *Server) regCheckManifestQuota(ctx context.Context, repo *model.Repo, blobs []string) *regErr {
	rs, err := s.registrySettings(ctx)
	if err != nil {
		return regInternal(s, "settings", err)
	}
	if rs.QuotaMB <= 0 {
		return nil
	}
	q := s.db.NewSelect().Model((*model.RegistryBlob)(nil)).ColumnExpr("SUM(rb.size)").
		Where(`rb.digest IN (SELECT r.digest FROM registry_manifest_refs r JOIN registry_manifests m ON m.id = r.manifest_id
			JOIN registry_images i ON i.id = m.image_id WHERE i.repo_id = ?)`, repo.ID)
	if len(blobs) > 0 {
		q = q.WhereOr("rb.digest IN (?)", bun.List(blobs))
	}
	var used sql.NullInt64
	if err := q.Scan(ctx, &used); err != nil {
		return regInternal(s, "usage", err)
	}
	if used.Int64 > mb(rs.QuotaMB) {
		return regErrorf(http.StatusForbidden, registry.CodeDenied,
			"this image would take the repository's registry storage to %d MiB, over its %d MiB quota: delete old images or ask an admin to raise registry quota_mb",
			(used.Int64+(1<<20)-1)>>20, rs.QuotaMB)
	}
	return nil
}

// regRepoUsage is the size of the distinct blobs a repository's images
// hold.
func (s *Server) regRepoUsage(ctx context.Context, repoID int64) (int64, error) {
	var used sql.NullInt64
	err := s.db.NewSelect().Model((*model.RegistryBlob)(nil)).ColumnExpr("SUM(rb.size)").
		Where("rb.digest IN (SELECT ib.digest FROM registry_image_blobs ib JOIN registry_images i ON i.id = ib.image_id WHERE i.repo_id = ?)", repoID).
		Scan(ctx, &used)
	return used.Int64, err
}

// ---- uploads ----

func (s *Server) uploadURL(t *regTarget, id string) string {
	return "/v2/" + t.name + "/blobs/uploads/" + id
}

func setUploadHeaders(w http.ResponseWriter, loc, id string, size int64) {
	h := w.Header()
	h.Set("Location", loc)
	h.Set("Docker-Upload-UUID", id)
	end := size - 1
	if end < 0 {
		end = 0
	}
	h.Set("Range", "0-"+strconv.FormatInt(end, 10))
	h.Set("Content-Length", "0")
}

func (s *Server) blobCreated(w http.ResponseWriter, t *regTarget, digest string) {
	h := w.Header()
	h.Set("Location", "/v2/"+t.name+"/blobs/"+digest)
	h.Set("Docker-Content-Digest", digest)
	h.Set("Content-Length", "0")
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) regStartUpload(w http.ResponseWriter, r *http.Request, p *Principal, t *regTarget) *regErr {
	ctx := r.Context()
	img, err := s.regEnsureImage(ctx, t)
	if err != nil {
		return regInternal(s, "create image", err)
	}
	q := r.URL.Query()
	// Cross-repository mount: link a blob the caller can read elsewhere.
	if d, from := q.Get("mount"), q.Get("from"); d != "" && from != "" && registry.ValidDigest(d) {
		if ok := s.regTryMount(ctx, p, t, img, d, from); ok == nil {
			s.blobCreated(w, t, d)
			return nil
		} else if ok.Code == registry.CodeDenied {
			return ok
		}
		// Not mountable: fall through to a normal upload, as the spec says.
	}
	now := s.now()
	up := &model.RegistryUpload{ID: auth.RandomToken(16), ImageID: img.ID, CreatedAt: now, UpdatedAt: now}
	if _, err := s.db.NewInsert().Model(up).Exec(ctx); err != nil {
		return regInternal(s, "create upload", err)
	}
	if d := q.Get("digest"); d != "" {
		// Monolithic upload: the body is the whole blob.
		return s.regCompleteUpload(w, r, t, up, d)
	}
	setUploadHeaders(w, s.uploadURL(t, up.ID), up.ID, 0)
	w.WriteHeader(http.StatusAccepted)
	return nil
}

// regTryMount links digest from the image named from into img. It
// returns nil on success, DENIED when over quota, and another error when
// the blob can't be mounted (the client then uploads it).
func (s *Server) regTryMount(ctx context.Context, p *Principal, t *regTarget, img *model.RegistryImage, digest, from string) *regErr {
	src, rerr := s.regResolve(ctx, from)
	if rerr != nil {
		return rerr
	}
	if rerr := s.regAccess(ctx, p, src, false); rerr != nil {
		return rerr
	}
	srcImg, err := s.regImage(ctx, s.db, src)
	if err != nil || srcImg == nil {
		return errRegBlobUnknown(digest)
	}
	b := new(model.RegistryBlob)
	err = s.db.NewSelect().Model(b).Where("rb.digest = ?", digest).
		Where("EXISTS (SELECT 1 FROM registry_image_blobs ib WHERE ib.image_id = ? AND ib.digest = rb.digest)", srcImg.ID).Scan(ctx)
	if err != nil {
		return errRegBlobUnknown(digest)
	}
	return s.regLinkBlob(ctx, t, img, digest, b.Size)
}

func (s *Server) regLoadUpload(ctx context.Context, t *regTarget, id string) (*model.RegistryUpload, *regErr) {
	img, err := s.regImage(ctx, s.db, t)
	if err != nil {
		return nil, regInternal(s, "load image", err)
	}
	if img == nil {
		return nil, errRegUploadUnknown
	}
	up := new(model.RegistryUpload)
	if err := s.db.NewSelect().Model(up).Where("id = ? AND image_id = ?", id, img.ID).Scan(ctx); err != nil {
		return nil, errRegUploadUnknown
	}
	return up, nil
}

func (s *Server) regUploadStatus(w http.ResponseWriter, r *http.Request, t *regTarget, id string) *regErr {
	up, rerr := s.regLoadUpload(r.Context(), t, id)
	if rerr != nil {
		return rerr
	}
	setUploadHeaders(w, s.uploadURL(t, up.ID), up.ID, up.Size)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// regHash restores an upload's SHA-256 state.
func regHash(up *model.RegistryUpload) (hash.Hash, error) {
	h := sha256.New()
	if len(up.HashState) > 0 {
		if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(up.HashState); err != nil {
			return nil, err
		}
	}
	return h, nil
}

// regAppend stores the request body as the upload's next part.
func (s *Server) regAppend(r *http.Request, t *regTarget, up *model.RegistryUpload) *regErr {
	ctx := r.Context()
	if cr := r.Header.Get("Content-Range"); cr != "" {
		var start, end int64
		if _, err := fmt.Sscanf(strings.TrimPrefix(cr, "bytes="), "%d-%d", &start, &end); err != nil || start != up.Size || end < start {
			return regErrorf(http.StatusRequestedRangeNotSatisfiable, registry.CodeRangeInvalid, "Content-Range %q doesn't continue the upload at byte %d", cr, up.Size)
		}
	}
	h, err := regHash(up)
	if err != nil {
		return regInternal(s, "restore hash", err)
	}
	rs, err := s.registrySettings(ctx)
	if err != nil {
		return regInternal(s, "settings", err)
	}
	max := int64(1) << 62
	if rs.QuotaMB > 0 {
		max = mb(rs.QuotaMB) - up.Size
	}
	hr := &hashingReader{r: r.Body, h: h, max: max}
	key := uploadPartKey(t.repo.ID, up.ID, up.Parts)
	declared := r.ContentLength
	if err := s.store.Put(ctx, key, hr, declared); err != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), key)
		if hr.n > max {
			return regErrorf(http.StatusForbidden, registry.CodeDenied, "the blob is larger than the repository's registry quota")
		}
		return regErrorf(http.StatusBadRequest, registry.CodeBlobUploadInvalid, "storing the chunk failed: %v", err)
	}
	if hr.n == 0 {
		_ = s.store.Delete(ctx, key)
		return nil
	}
	state, err := h.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		return regInternal(s, "save hash", err)
	}
	res, err := s.db.NewUpdate().Model((*model.RegistryUpload)(nil)).
		Set("size = ?, parts = ?, hash_state = ?, updated_at = ?", up.Size+hr.n, up.Parts+1, state, s.now()).
		Where("id = ? AND size = ? AND parts = ?", up.ID, up.Size, up.Parts).Exec(ctx)
	if err != nil {
		return regInternal(s, "update upload", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = s.store.Delete(ctx, key)
		return regErrorf(http.StatusConflict, registry.CodeBlobUploadInvalid, "the upload changed concurrently")
	}
	up.Size, up.Parts, up.HashState = up.Size+hr.n, up.Parts+1, state
	return nil
}

func (s *Server) regPatchUpload(w http.ResponseWriter, r *http.Request, t *regTarget, id string) *regErr {
	up, rerr := s.regLoadUpload(r.Context(), t, id)
	if rerr != nil {
		return rerr
	}
	if rerr := s.regAppend(r, t, up); rerr != nil {
		return rerr
	}
	setUploadHeaders(w, s.uploadURL(t, up.ID), up.ID, up.Size)
	w.WriteHeader(http.StatusAccepted)
	return nil
}

func (s *Server) regFinishUpload(w http.ResponseWriter, r *http.Request, t *regTarget, id string) *regErr {
	up, rerr := s.regLoadUpload(r.Context(), t, id)
	if rerr != nil {
		return rerr
	}
	return s.regCompleteUpload(w, r, t, up, r.URL.Query().Get("digest"))
}

// regCompleteUpload appends the request body, checks the digest, moves the
// parts into the content-addressed blob and links it to the image.
func (s *Server) regCompleteUpload(w http.ResponseWriter, r *http.Request, t *regTarget, up *model.RegistryUpload, digest string) *regErr {
	ctx := r.Context()
	if !registry.ValidDigest(digest) {
		return regErrorf(http.StatusBadRequest, registry.CodeDigestInvalid, "invalid or missing digest %q (sha256 only)", digest)
	}
	if rerr := s.regAppend(r, t, up); rerr != nil {
		return rerr
	}
	h, err := regHash(up)
	if err != nil {
		return regInternal(s, "restore hash", err)
	}
	got := "sha256:" + hex.EncodeToString(h.Sum(nil))
	if got != digest {
		s.regDropUpload(context.WithoutCancel(ctx), t.repo.ID, up)
		return regErrorf(http.StatusBadRequest, registry.CodeDigestInvalid, "the uploaded content's digest is %s, not %s", got, digest)
	}
	img := &model.RegistryImage{ID: up.ImageID, RepoID: t.repo.ID}
	if rerr := s.regCheckQuota(ctx, t.repo, digest, up.Size); rerr != nil {
		s.regDropUpload(context.WithoutCancel(ctx), t.repo.ID, up)
		return rerr
	}
	// Link first: the garbage collector never deletes a linked blob (see
	// registryGC).
	link := &model.RegistryImageBlob{ImageID: img.ID, Digest: digest, CreatedAt: s.now()}
	if _, err := s.db.NewInsert().Model(link).On("CONFLICT (image_id, digest) DO NOTHING").Exec(ctx); err != nil {
		return regInternal(s, "link blob", err)
	}
	exists, err := s.db.NewSelect().Model((*model.RegistryBlob)(nil)).Where("digest = ?", digest).Exists(ctx)
	if err != nil {
		return regInternal(s, "check blob", err)
	}
	if !exists {
		if err := s.store.Put(ctx, blobStorageKey(digest), &partsReader{ctx: ctx, s: s, repoID: t.repo.ID, up: up}, up.Size); err != nil {
			return regInternal(s, "store blob", err)
		}
		b := &model.RegistryBlob{Digest: digest, Size: up.Size, CreatedAt: s.now()}
		if _, err := s.db.NewInsert().Model(b).On("CONFLICT (digest) DO NOTHING").Exec(ctx); err != nil {
			return regInternal(s, "record blob", err)
		}
	}
	s.regDropUpload(context.WithoutCancel(ctx), t.repo.ID, up)
	s.blobCreated(w, t, digest)
	return nil
}

// partsReader reads an upload's parts in order.
type partsReader struct {
	ctx    context.Context
	s      *Server
	repoID int64
	up     *model.RegistryUpload
	n      int
	cur    io.ReadCloser
}

func (p *partsReader) Read(b []byte) (int, error) {
	for {
		if p.cur == nil {
			if p.n >= p.up.Parts {
				return 0, io.EOF
			}
			rc, err := p.s.store.Get(p.ctx, uploadPartKey(p.repoID, p.up.ID, p.n))
			if err != nil {
				return 0, err
			}
			p.cur, p.n = rc, p.n+1
		}
		n, err := p.cur.Read(b)
		if err == io.EOF {
			_ = p.cur.Close()
			p.cur = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (s *Server) regDropUpload(ctx context.Context, repoID int64, up *model.RegistryUpload) {
	for i := 0; i < up.Parts; i++ {
		_ = s.store.Delete(ctx, uploadPartKey(repoID, up.ID, i))
	}
	_, _ = s.db.NewDelete().Model((*model.RegistryUpload)(nil)).Where("id = ?", up.ID).Exec(ctx)
}

func (s *Server) regCancelUpload(w http.ResponseWriter, r *http.Request, t *regTarget, id string) *regErr {
	up, rerr := s.regLoadUpload(r.Context(), t, id)
	if rerr != nil {
		return rerr
	}
	s.regDropUpload(r.Context(), t.repo.ID, up)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// ---- manifests ----

func (s *Server) regFindManifest(ctx context.Context, img *model.RegistryImage, ref string) (*model.RegistryManifest, error) {
	m := new(model.RegistryManifest)
	q := s.db.NewSelect().Model(m).Where("rm.image_id = ?", img.ID)
	if registry.ValidDigest(ref) {
		q = q.Where("rm.digest = ?", ref)
	} else {
		q = q.Where("rm.id = (SELECT manifest_id FROM registry_tags WHERE image_id = ? AND name = ?)", img.ID, ref)
	}
	err := q.Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

func (s *Server) regGetManifest(w http.ResponseWriter, r *http.Request, t *regTarget, ref string) *regErr {
	ctx := r.Context()
	img, err := s.regImage(ctx, s.db, t)
	if err != nil {
		return regInternal(s, "load image", err)
	}
	if img == nil {
		return errRegManifestUnknown(ref)
	}
	m, err := s.regFindManifest(ctx, img, ref)
	if err != nil {
		return regInternal(s, "load manifest", err)
	}
	if m == nil {
		return errRegManifestUnknown(ref)
	}
	h := w.Header()
	h.Set("Content-Type", m.MediaType)
	h.Set("Content-Length", strconv.Itoa(len(m.Content)))
	h.Set("Docker-Content-Digest", m.Digest)
	h.Set("ETag", `"`+m.Digest+`"`)
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(m.Content)
	}
	return nil
}

// pushedBy is who a push is attributed to: the user, or for a job token the
// run's actor (and the job).
func (s *Server) pushedBy(ctx context.Context, p *Principal) (user *int64, job *int64) {
	if p.Method != MethodJob {
		id := p.User.ID
		return &id, nil
	}
	jid := p.Job.ID
	run, err := loadRunByID(ctx, s.db, p.Job.RunID)
	if err == nil && run.ActorID != nil {
		return run.ActorID, &jid
	}
	return nil, &jid
}

func (s *Server) regPutManifest(w http.ResponseWriter, r *http.Request, p *Principal, t *regTarget, ref string) *regErr {
	ctx := r.Context()
	isDigest := registry.ValidDigest(ref)
	if !isDigest && !registry.ValidTag(ref) {
		return regErrorf(http.StatusBadRequest, registry.CodeManifestInvalid, "invalid tag %q", ref)
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, registry.MaxManifestBytes+1))
	if err != nil {
		return regErrorf(http.StatusBadRequest, registry.CodeManifestInvalid, "reading the manifest failed: %v", err)
	}
	if len(body) > registry.MaxManifestBytes {
		return regErrorf(http.StatusRequestEntityTooLarge, registry.CodeSizeInvalid, "manifests are limited to %d MiB", registry.MaxManifestBytes>>20)
	}
	man, mediaType, err := registry.ParseManifest(r.Header.Get("Content-Type"), body)
	if err != nil {
		return regErrorf(http.StatusBadRequest, registry.CodeManifestInvalid, "%v", err)
	}
	sum := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if isDigest && ref != digest {
		return regErrorf(http.StatusBadRequest, registry.CodeDigestInvalid, "the manifest's digest is %s, not %s", digest, ref)
	}
	img, err := s.regEnsureImage(ctx, t)
	if err != nil {
		return regInternal(s, "create image", err)
	}
	// Everything the manifest references must already be in the image.
	refs := map[string]string{}
	var imageSize int64
	platforms := []string{}
	if registry.IsIndex(mediaType) {
		for _, d := range man.Manifests {
			child, err := s.regFindManifest(ctx, img, d.Digest)
			if err != nil {
				return regInternal(s, "load manifest", err)
			}
			if child == nil {
				return regErrorf(http.StatusBadRequest, registry.CodeManifestBlobUnknown, "the index lists manifest %s, which isn't in %s; push it first", d.Digest, t.name)
			}
			refs[d.Digest] = "manifest"
			imageSize += child.ImageSize
			if ps := d.Platform.String(); ps != "" {
				platforms = append(platforms, ps)
			}
		}
	} else {
		for _, d := range man.Blobs() {
			ok, err := s.db.NewSelect().Model((*model.RegistryImageBlob)(nil)).Where("image_id = ? AND digest = ?", img.ID, d).Exists(ctx)
			if err != nil {
				return regInternal(s, "check blob", err)
			}
			if !ok {
				return regErrorf(http.StatusBadRequest, registry.CodeManifestBlobUnknown, "blob %s isn't in %s; push it first", d, t.name)
			}
			refs[d] = "blob"
		}
		imageSize = man.Size()
		// Uploads check the quota one blob at a time and can race; the
		// manifest makes the image real, so check what it adds in full.
		if rerr := s.regCheckManifestQuota(ctx, t.repo, man.Blobs()); rerr != nil {
			return rerr
		}
	}
	user, job := s.pushedBy(ctx, p)
	now := s.now()
	created := false
	var m *model.RegistryManifest
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		m = new(model.RegistryManifest)
		err := tx.NewSelect().Model(m).Where("image_id = ? AND digest = ?", img.ID, digest).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			created = true
			artifact := man.ArtifactType
			if artifact == "" && man.Config != nil && !strings.HasPrefix(man.Config.MediaType, "application/vnd.oci.image.config") &&
				!strings.HasPrefix(man.Config.MediaType, "application/vnd.docker.container.image") {
				artifact = man.Config.MediaType
			}
			m = &model.RegistryManifest{ImageID: img.ID, Digest: digest, MediaType: mediaType, ArtifactType: artifact,
				Size: int64(len(body)), ImageSize: imageSize, Platforms: mustJSON(platforms), Content: body,
				PushedByID: user, PushedByJob: job, CreatedAt: now}
			if _, err := tx.NewInsert().Model(m).Exec(ctx); err != nil {
				return err
			}
			for d, kind := range refs {
				if _, err := tx.NewInsert().Model(&model.RegistryManifestRef{ManifestID: m.ID, Digest: d, Kind: kind}).
					On("CONFLICT (manifest_id, digest) DO NOTHING").Exec(ctx); err != nil {
					return err
				}
			}
		} else if err != nil {
			return err
		}
		if !isDigest {
			tag := &model.RegistryTag{ImageID: img.ID, Name: ref, ManifestID: m.ID, CreatedAt: now, UpdatedAt: now}
			if _, err := tx.NewInsert().Model(tag).On("CONFLICT (image_id, name) DO UPDATE").
				Set("manifest_id = EXCLUDED.manifest_id, updated_at = EXCLUDED.updated_at").Exec(ctx); err != nil {
				return err
			}
		}
		_, err = tx.NewUpdate().Model((*model.RegistryImage)(nil)).Set("updated_at = ?", now).Where("id = ?", img.ID).Exec(ctx)
		return err
	})
	if err != nil {
		return regInternal(s, "store manifest", err)
	}
	if !isDigest || created {
		tag := ""
		if !isDigest {
			tag = ref
		}
		s.hooksPackage(ctx, t, m, tag, "published", userOrNil(p))
	}
	h := w.Header()
	h.Set("Location", "/v2/"+t.name+"/manifests/"+digest)
	h.Set("Docker-Content-Digest", digest)
	h.Set("Content-Length", "0")
	w.WriteHeader(http.StatusCreated)
	return nil
}

// userOrNil is the acting user for webhooks (nil, the bot, for job tokens).
func userOrNil(p *Principal) *model.User {
	if p == nil || p.Method == MethodJob {
		return nil
	}
	return p.User
}

func (s *Server) regDeleteManifest(w http.ResponseWriter, r *http.Request, p *Principal, t *regTarget, ref string) *regErr {
	ctx := r.Context()
	img, err := s.regImage(ctx, s.db, t)
	if err != nil {
		return regInternal(s, "load image", err)
	}
	if img == nil {
		return errRegManifestUnknown(ref)
	}
	if registry.ValidDigest(ref) {
		m, err := s.regFindManifest(ctx, img, ref)
		if err != nil {
			return regInternal(s, "load manifest", err)
		}
		if m == nil {
			return errRegManifestUnknown(ref)
		}
		if err := s.deleteManifest(ctx, t, m); err != nil {
			return regInternal(s, "delete manifest", err)
		}
		_ = s.record(ctx, s.db, audit.PackageDelete, "package", t.name+"@"+ref, map[string]any{"repo": t.repo.Name, "image": t.image, "digest": ref})
	} else {
		m, err := s.regFindManifest(ctx, img, ref)
		if err != nil {
			return regInternal(s, "load manifest", err)
		}
		if m == nil {
			return errRegManifestUnknown(ref)
		}
		if _, err := s.db.NewDelete().Model((*model.RegistryTag)(nil)).Where("image_id = ? AND name = ?", img.ID, ref).Exec(ctx); err != nil {
			return regInternal(s, "delete tag", err)
		}
		_ = s.record(ctx, s.db, audit.PackageDelete, "package", t.name+":"+ref, map[string]any{"repo": t.repo.Name, "image": t.image, "tag": ref})
		s.hooksPackage(ctx, t, m, ref, "deleted", userOrNil(p))
	}
	w.WriteHeader(http.StatusAccepted)
	return nil
}

// deleteManifest removes a manifest and its tags. Blobs stay until the
// garbage collector finds them unused.
func (s *Server) deleteManifest(ctx context.Context, t *regTarget, m *model.RegistryManifest) error {
	if _, err := s.db.NewDelete().Model((*model.RegistryManifest)(nil)).Where("id = ?", m.ID).Exec(ctx); err != nil {
		return err
	}
	s.hooksPackage(ctx, t, m, "", "deleted", userOrNil(principal(ctx)))
	return nil
}

// ---- tags and catalog ----

// pageParams reads n and last; n is capped at 1000.
func regPageParams(r *http.Request) (int, string, *regErr) {
	q := r.URL.Query()
	n := 1000
	if v := q.Get("n"); v != "" {
		i, err := strconv.Atoi(v)
		if err != nil || i < 0 {
			return 0, "", regErrorf(http.StatusBadRequest, registry.CodeUnsupported, "n must be a non-negative number")
		}
		n = min(i, 1000)
	}
	return n, q.Get("last"), nil
}

func writePage(w http.ResponseWriter, r *http.Request, base string, items []string, n int, last string, body func([]string) any) {
	sort.Strings(items)
	start := 0
	if last != "" {
		start = sort.SearchStrings(items, last)
		if start < len(items) && items[start] == last {
			start++
		}
	}
	end := min(start+n, len(items))
	page := items[start:end]
	if page == nil {
		page = []string{}
	}
	if end < len(items) && len(page) > 0 {
		w.Header().Set("Link", fmt.Sprintf(`<%s?n=%d&last=%s>; rel="next"`, base, n, url.QueryEscape(page[len(page)-1])))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(body(page))
	}
}

func (s *Server) regListTags(w http.ResponseWriter, r *http.Request, t *regTarget) *regErr {
	ctx := r.Context()
	n, last, rerr := regPageParams(r)
	if rerr != nil {
		return rerr
	}
	img, err := s.regImage(ctx, s.db, t)
	if err != nil {
		return regInternal(s, "load image", err)
	}
	if img == nil {
		return errRegNameUnknown(t.name)
	}
	var tags []string
	if err := s.db.NewSelect().Model((*model.RegistryTag)(nil)).Column("name").Where("image_id = ?", img.ID).Scan(ctx, &tags); err != nil {
		return regInternal(s, "list tags", err)
	}
	writePage(w, r, "/v2/"+t.name+"/tags/list", tags, n, last, func(page []string) any {
		return map[string]any{"name": t.name, "tags": page}
	})
	return nil
}

func (s *Server) regCatalog(w http.ResponseWriter, r *http.Request, p *Principal) {
	ctx := r.Context()
	n, last, rerr := regPageParams(r)
	if rerr != nil {
		rerr.Write(w, r)
		return
	}
	var rows []struct {
		Repo  string `bun:"repo"`
		Image string `bun:"image"`
	}
	q := s.db.NewSelect().TableExpr("registry_images AS ri").Join("JOIN repos AS r ON r.id = ri.repo_id").
		ColumnExpr("r.name AS repo, ri.name AS image")
	q = authz.VisibleRepos(q, p.User)
	if err := q.Scan(ctx, &rows); err != nil {
		regInternal(s, "catalog", err).Write(w, r)
		return
	}
	ns := s.regNamespace(ctx)
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, registry.ImagePath(ns, row.Repo, row.Image))
	}
	writePage(w, r, "/v2/_catalog", names, n, last, func(page []string) any {
		return map[string]any{"repositories": page}
	})
}
