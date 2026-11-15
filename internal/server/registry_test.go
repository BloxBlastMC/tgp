package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/model"
)

type specT = runnerapi.JobSpec

// regEnv is a prEnv plus registry helpers.
type regEnv struct {
	*prEnv
	host string
}

func newRegEnv(t *testing.T) *regEnv {
	p := newPREnv(t)
	u, _ := url.Parse(p.ts.URL)
	return &regEnv{prEnv: p, host: u.Host}
}

func (e *regEnv) ref(t *testing.T, s string) name.Reference {
	t.Helper()
	r, err := name.ParseReference(e.host+"/"+s, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func basic(tok string) remote.Option {
	return remote.WithAuth(&authn.Basic{Username: "whoever", Password: tok})
}

func regCode(err error) string {
	var te *transport.Error
	if errors.As(err, &te) && len(te.Errors) > 0 {
		return string(te.Errors[0].Code)
	}
	if errors.As(err, &te) {
		return fmt.Sprintf("HTTP %d", te.StatusCode)
	}
	return ""
}

// raw sends a registry request.
func (e *regEnv) raw(method, path, tok string, body []byte, hdr map[string]string) (*http.Response, []byte) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.ts.URL+path, bytes.NewReader(body))
	if tok != "" {
		req.SetBasicAuth("x", tok)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func TestRegistryPushPullDelete(t *testing.T) {
	e := newRegEnv(t)
	tok := e.miaToken

	// Unauthenticated requests get a Basic challenge.
	res, _ := e.raw("GET", "/v2/", "", nil, nil)
	if res.StatusCode != 401 || !strings.Contains(res.Header.Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("challenge: %d %v", res.StatusCode, res.Header)
	}
	if res, _ := e.raw("GET", "/v2/", tok, nil, nil); res.StatusCode != 200 || res.Header.Get("Docker-Distribution-API-Version") != "registry/2.0" {
		t.Fatalf("ping: %d", res.StatusCode)
	}

	img, err := random.Image(2048, 3)
	if err != nil {
		t.Fatal(err)
	}
	ref := e.ref(t, "bloxblast/game/issues:g1a2b3c4d5e6f")
	if err := remote.Write(ref, img, basic(tok)); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := remote.Tag(ref.Context().Tag("master"), img, basic(tok)); err != nil {
		t.Fatalf("tag: %v", err)
	}
	// The repository itself can hold an image too (no name).
	if err := remote.Write(e.ref(t, "bloxblast/game:latest"), img, basic(tok)); err != nil {
		t.Fatalf("push to repo root: %v", err)
	}

	// Pull as the tester (read access) and compare digests.
	tomTok := e.createToken(e.tom, "read")
	got, err := remote.Image(ref, basic(tomTok))
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	want, _ := img.Digest()
	if d, _ := got.Digest(); d != want {
		t.Fatalf("digest %s != %s", d, want)
	}
	layers, _ := got.Layers()
	for _, l := range layers {
		rc, err := l.Compressed()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, rc); err != nil {
			t.Fatalf("layer: %v", err)
		}
		rc.Close()
	}
	tags, err := remote.List(ref.Context(), basic(tomTok))
	if err != nil || strings.Join(tags, ",") != "g1a2b3c4d5e6f,master" {
		t.Fatalf("tags: %v %v", tags, err)
	}
	// tags/list pagination.
	res, body := e.raw("GET", "/v2/bloxblast/game/issues/tags/list?n=1", tomTok, nil, nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"tags":["g1a2b3c4d5e6f"]`) || !strings.Contains(res.Header.Get("Link"), "last=g1a2b3c4d5e6f") {
		t.Fatalf("page 1: %s %v", body, res.Header)
	}
	if _, body := e.raw("GET", "/v2/bloxblast/game/issues/tags/list?n=1&last=g1a2b3c4d5e6f", tomTok, nil, nil); !strings.Contains(string(body), `"tags":["master"]`) {
		t.Fatalf("page 2: %s", body)
	}
	// Catalog lists the images the caller can see.
	if _, body := e.raw("GET", "/v2/_catalog", tomTok, nil, nil); !strings.Contains(string(body), `"bloxblast/game","bloxblast/game/issues"`) {
		t.Fatalf("catalog: %s", body)
	}

	// Testers can't push; read-only tokens can't either.
	if err := remote.Write(e.ref(t, "bloxblast/game/issues:tom"), img, basic(tomTok)); regCode(err) != "DENIED" {
		t.Fatalf("tester push: %v", err)
	}
	miaRead := e.createToken(e.mia, "read")
	if err := remote.Write(e.ref(t, "bloxblast/game/issues:ro"), img, basic(miaRead)); regCode(err) != "DENIED" {
		t.Fatalf("read-only token push: %v", err)
	}
	// Wrong namespace or unknown repo: NAME_UNKNOWN.
	if _, err := remote.Image(e.ref(t, "other/game/issues:master"), basic(tok)); regCode(err) != "NAME_UNKNOWN" && regCode(err) != "MANIFEST_UNKNOWN" {
		t.Fatalf("wrong namespace: %v", err)
	}
	if _, err := remote.Image(e.ref(t, "bloxblast/nope:master"), basic(tok)); regCode(err) != "NAME_UNKNOWN" {
		t.Fatalf("unknown repo: %v", err)
	}
	// Bad credentials.
	if _, err := remote.Image(ref, basic("tgp_pat_bogus")); err == nil {
		t.Fatal("bogus token pulled")
	}

	// Delete a tag, then the manifest.
	if err := remote.Delete(ref.Context().Tag("master"), basic(tok)); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	if tags, _ := remote.List(ref.Context(), basic(tok)); strings.Join(tags, ",") != "g1a2b3c4d5e6f" {
		t.Fatalf("tags after delete: %v", tags)
	}
	if err := remote.Delete(ref.Context().Digest(want.String()), basic(tok)); err != nil {
		t.Fatalf("delete manifest: %v", err)
	}
	if _, err := remote.Image(ref, basic(tok)); regCode(err) != "MANIFEST_UNKNOWN" {
		t.Fatalf("pull after delete: %v", err)
	}
}

func TestRegistryIndexAndChunkedUpload(t *testing.T) {
	e := newRegEnv(t)
	tok := e.miaToken
	idx, err := random.Index(1024, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	ref := e.ref(t, "bloxblast/game/multi:v1")
	if err := remote.WriteIndex(ref, idx, basic(tok)); err != nil {
		t.Fatalf("push index: %v", err)
	}
	got, err := remote.Index(ref, basic(tok))
	if err != nil {
		t.Fatal(err)
	}
	if a, b := mustDigest(t, got), mustDigest(t, idx); a != b {
		t.Fatalf("index digest %s != %s", a, b)
	}

	// A blob in two chunks, by hand.
	data := bytes.Repeat([]byte("tgp-registry-"), 1000)
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	res, _ := e.raw("POST", "/v2/bloxblast/game/chunks/blobs/uploads/", tok, nil, nil)
	if res.StatusCode != 202 {
		t.Fatalf("start: %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	half := len(data) / 2
	res, _ = e.raw("PATCH", loc, tok, data[:half], map[string]string{"Content-Range": fmt.Sprintf("0-%d", half-1)})
	if res.StatusCode != 202 || res.Header.Get("Range") != fmt.Sprintf("0-%d", half-1) {
		t.Fatalf("chunk 1: %d %v", res.StatusCode, res.Header)
	}
	// A chunk that doesn't continue the upload is refused.
	if res, _ := e.raw("PATCH", loc, tok, data[half:], map[string]string{"Content-Range": "5-10"}); res.StatusCode != 416 {
		t.Fatalf("bad range: %d", res.StatusCode)
	}
	res, _ = e.raw("PATCH", loc, tok, data[half:], map[string]string{"Content-Range": fmt.Sprintf("%d-%d", half, len(data)-1)})
	if res.StatusCode != 202 {
		t.Fatalf("chunk 2: %d", res.StatusCode)
	}
	if res, _ := e.raw("GET", loc, tok, nil, nil); res.StatusCode != 204 || res.Header.Get("Range") != fmt.Sprintf("0-%d", len(data)-1) {
		t.Fatalf("status: %d %v", res.StatusCode, res.Header)
	}
	res, body := e.raw("PUT", loc+"?digest="+digest, tok, nil, nil)
	if res.StatusCode != 201 || res.Header.Get("Docker-Content-Digest") != digest {
		t.Fatalf("finish: %d %s", res.StatusCode, body)
	}
	res, body = e.raw("GET", "/v2/bloxblast/game/chunks/blobs/"+digest, e.createToken(e.tom, "read"), nil, nil)
	if res.StatusCode != 200 || !bytes.Equal(body, data) {
		t.Fatalf("blob: %d %d bytes", res.StatusCode, len(body))
	}
	// A wrong digest is rejected and the upload discarded.
	res, _ = e.raw("POST", "/v2/bloxblast/game/chunks/blobs/uploads/?digest=sha256:"+strings.Repeat("0", 64), tok, []byte("other"), nil)
	if res.StatusCode != 400 {
		t.Fatalf("wrong digest: %d", res.StatusCode)
	}

	// Cross-repository mount into another tgp repository.
	e.admin.mustDo("POST", "/repos", map[string]any{"name": "finance", "auto_init": true}, 201)
	res, _ = e.raw("POST", "/v2/bloxblast/finance/blobs/uploads/?mount="+digest+"&from=bloxblast/game/chunks", tok, nil, nil)
	if res.StatusCode != 201 {
		t.Fatalf("mount: %d", res.StatusCode)
	}
	if res, _ := e.raw("HEAD", "/v2/bloxblast/finance/blobs/"+digest, tok, nil, nil); res.StatusCode != 200 || res.ContentLength != int64(len(data)) {
		t.Fatalf("mounted blob: %d %d", res.StatusCode, res.ContentLength)
	}
	// Blobs aren't visible from images that don't have them.
	if res, _ := e.raw("HEAD", "/v2/bloxblast/game/multi/blobs/"+digest, tok, nil, nil); res.StatusCode != 404 {
		t.Fatalf("unlinked blob: %d", res.StatusCode)
	}
	// A manifest referencing a blob the image lacks is refused.
	man := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` + strings.Repeat("1", 64) + `","size":2},"layers":[]}`
	res, body = e.raw("PUT", "/v2/bloxblast/game/chunks/manifests/x", tok, []byte(man), map[string]string{"Content-Type": "application/vnd.oci.image.manifest.v1+json"})
	if res.StatusCode != 400 || !strings.Contains(string(body), "MANIFEST_BLOB_UNKNOWN") {
		t.Fatalf("missing blob: %d %s", res.StatusCode, body)
	}
}

type digester interface{ Digest() (v1.Hash, error) }

func mustDigest(t *testing.T, d digester) string {
	t.Helper()
	h, err := d.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return h.String()
}

func TestRegistryQuotaAndRetention(t *testing.T) {
	e := newRegEnv(t)
	tok := e.miaToken
	ctx := context.Background()
	rs := defaultRegistrySettings()
	rs.QuotaMB = 1
	if err := e.srv.saveRegistrySettings(ctx, rs); err != nil {
		t.Fatal(err)
	}
	small, _ := random.Image(100<<10, 2)
	if err := remote.Write(e.ref(t, "bloxblast/game/app:one"), small, basic(tok)); err != nil {
		t.Fatalf("small push: %v", err)
	}
	big, _ := random.Image(600<<10, 2)
	if err := remote.Write(e.ref(t, "bloxblast/game/app:big"), big, basic(tok)); regCode(err) != "DENIED" {
		t.Fatalf("over quota: %v", err)
	}
	// Re-pushing blobs the repository already has doesn't count again.
	if err := remote.Write(e.ref(t, "bloxblast/game/other:one"), small, basic(tok)); err != nil {
		t.Fatalf("same blobs elsewhere in the repo: %v", err)
	}

	// Retention: untag "one", age everything, collect.
	if err := remote.Delete(e.ref(t, "bloxblast/game/app:one"), basic(tok)); err != nil {
		t.Fatal(err)
	}
	var blobsBefore int64
	blobsBefore, _ = e.db.NewSelect().Model((*model.RegistryBlob)(nil)).Count(ctx)
	e.srv.ci.now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	if err := e.srv.RunRegistryGC(ctx); err != nil {
		t.Fatal(err)
	}
	// app's manifest is gone; other:one still holds the same blobs.
	if _, err := remote.Image(e.ref(t, "bloxblast/game/app@"+mustDigest(t, small)), basic(tok)); regCode(err) != "MANIFEST_UNKNOWN" && regCode(err) != "NAME_UNKNOWN" {
		t.Fatalf("untagged manifest survived: %v", err)
	}
	if _, err := remote.Image(e.ref(t, "bloxblast/game/other:one"), basic(tok)); err != nil {
		t.Fatalf("tagged image collected: %v", err)
	}
	blobsAfter, _ := e.db.NewSelect().Model((*model.RegistryBlob)(nil)).Count(ctx)
	if blobsAfter == 0 || blobsAfter > blobsBefore {
		t.Fatalf("blobs %d -> %d", blobsBefore, blobsAfter)
	}
	// Deleting the last tag frees the blobs after another pass.
	if err := remote.Delete(e.ref(t, "bloxblast/game/other:one"), basic(tok)); err != nil {
		t.Fatal(err)
	}
	e.srv.ci.now = func() time.Time { return time.Now().Add(20 * 24 * time.Hour) }
	for i := 0; i < 2; i++ {
		if err := e.srv.RunRegistryGC(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := e.db.NewSelect().Model((*model.RegistryBlob)(nil)).Count(ctx); n != 0 {
		t.Fatalf("%d blobs left after collecting everything", n)
	}
	if n, _ := e.db.NewSelect().Model((*model.RegistryImage)(nil)).Count(ctx); n != 0 {
		t.Fatalf("%d empty images left", n)
	}

	// keep_last_tags.
	e.srv.ci.now = nil
	rs = defaultRegistrySettings()
	rs.KeepLastTags = 2
	_ = e.srv.saveRegistrySettings(ctx, rs)
	for i, tag := range []string{"g1", "g2", "g3", "master"} {
		img, _ := random.Image(64, 1)
		if err := remote.Write(e.ref(t, "bloxblast/game/app:"+tag), img, basic(tok)); err != nil {
			t.Fatal(err)
		}
		_ = i
		time.Sleep(10 * time.Millisecond)
	}
	if err := e.srv.RunRegistryGC(ctx); err != nil {
		t.Fatal(err)
	}
	tags, _ := remote.List(e.ref(t, "bloxblast/game/app:x").Context(), basic(tok))
	if strings.Join(tags, ",") != "g2,g3,master" {
		t.Fatalf("keep_last_tags: %v", tags)
	}
}

func TestRegistryJobTokens(t *testing.T) {
	e := newRegEnv(t)
	r := e.registerRunner(e.admin, "r1", 2)
	e.commit("main", ".tgp/workflows/ci.yml", "name: CI\non: [push, pull_request]\njobs:\n  test:\n    steps:\n      - run: make test\n", "CI")
	push := r.mustFetch()
	if !push.Trusted || push.Registry.Host != "git.example.com" || push.Registry.Path != "bloxblast/game" {
		t.Fatalf("push job: trusted=%v registry=%+v", push.Trusted, push.Registry)
	}
	trusted := push.Git.Token
	img, _ := random.Image(256, 1)
	if err := remote.Write(e.ref(t, "bloxblast/game/issues:g1"), img, basic(trusted)); err != nil {
		t.Fatalf("trusted job push: %v", err)
	}
	// The pusher is the run's actor, through the job.
	var m model.RegistryManifest
	if err := e.db.NewSelect().Model(&m).Limit(1).Scan(context.Background()); err != nil || m.PushedByJob == nil || *m.PushedByJob != push.ID || m.PushedByID == nil {
		t.Fatalf("pushed by: %+v %v", m, err)
	}
	// Job tokens can't touch other repositories.
	e.admin.mustDo("POST", "/repos", map[string]any{"name": "finance", "auto_init": true}, 201)
	if err := remote.Write(e.ref(t, "bloxblast/finance:x"), img, basic(trusted)); regCode(err) != "NAME_UNKNOWN" {
		t.Fatalf("other repo: %v", err)
	}
	// The API accepts the job token read-only, for its own repository.
	job := e.withToken(trusted)
	job.mustDo("GET", "/repos/game", nil, 200)
	job.mustDo("GET", "/repos/game/runs", nil, 200)
	job.mustDo("GET", "/auth/session", nil, 200)
	if res := job.do("GET", "/repos/finance", nil); res.status != 403 && res.status != 404 {
		t.Fatalf("other repo via API: %d", res.status)
	}
	if res := job.do("GET", "/repos", nil); res.status != 403 {
		t.Fatalf("repo list via API: %d %s", res.status, res.body)
	}
	if res := job.do("POST", "/repos/game/issues", map[string]any{"title": "x"}); res.status != 403 {
		t.Fatalf("write via API: %d", res.status)
	}
	r.complete(push.ID, "success", nil)
	// Once the job is done, the token is dead.
	if _, err := remote.Image(e.ref(t, "bloxblast/game/issues:g1"), basic(trusted)); err == nil {
		t.Fatal("finished job's token still works")
	}
	if res := job.do("GET", "/repos/game", nil); res.status != 401 {
		t.Fatalf("finished job via API: %d", res.status)
	}

	// A pull request run's token can pull but not push.
	e.commit("feature", "x.txt", "x", "Change")
	e.mia.mustDo("POST", "/repos/game/pulls", map[string]any{"title": "Feature", "head": "feature"}, 201)
	var pr *specT
	for i := 0; i < 5 && pr == nil; i++ {
		s := r.fetch()
		if s != nil && strings.HasPrefix(s.Env["GITHUB_EVENT_NAME"], "pull_request") {
			pr = s
		} else if s != nil {
			r.complete(s.ID, "success", nil)
		}
	}
	if pr == nil || pr.Trusted {
		t.Fatalf("pull request job: %+v", pr)
	}
	if _, err := remote.Image(e.ref(t, "bloxblast/game/issues:g1"), basic(pr.Git.Token)); err != nil {
		t.Fatalf("PR job pull: %v", err)
	}
	if err := remote.Write(e.ref(t, "bloxblast/game/issues:pr"), img, basic(pr.Git.Token)); regCode(err) != "DENIED" {
		t.Fatalf("PR job push: %v", err)
	}
}

func TestPackagesAPI(t *testing.T) {
	e := newRegEnv(t)
	tok := e.miaToken
	img, _ := random.Image(1024, 2)
	if err := remote.Write(e.ref(t, "bloxblast/game/issues:g1"), img, basic(tok)); err != nil {
		t.Fatal(err)
	}
	if err := remote.Tag(e.ref(t, "bloxblast/game/issues:master").(name.Tag), img, basic(tok)); err != nil {
		t.Fatal(err)
	}
	idx, _ := random.Index(256, 1, 2)
	if err := remote.WriteIndex(e.ref(t, "bloxblast/game/issues:multi"), idx, basic(tok)); err != nil {
		t.Fatal(err)
	}

	var list apigen.PackageList
	e.tom.mustDo("GET", "/repos/game/packages", nil, 200).decode(t, &list)
	if list.Registry != "git.example.com" || list.Namespace != "bloxblast" || len(list.Images) != 1 || list.QuotaBytes != 10240<<20 {
		t.Fatalf("list: %+v", list)
	}
	p := list.Images[0]
	if p.Name != "issues" || p.Path != "bloxblast/game/issues" || p.Tags != 3 || p.SizeBytes == 0 || list.UsageBytes < p.SizeBytes || p.LatestTag == nil || *p.LatestTag != "multi" {
		t.Fatalf("package: %+v", p)
	}
	var vs apigen.PackageVersionList
	e.tom.mustDo("GET", "/repos/game/packages/versions?name=issues", nil, 200).decode(t, &vs)
	// The index's two child manifests are folded under it.
	if len(vs.Versions) != 2 {
		t.Fatalf("versions: %+v", vs)
	}
	index, image := vs.Versions[0], vs.Versions[1]
	if index.Kind != apigen.PackageVersionKindIndex || strings.Join(index.Tags, ",") != "multi" || len(index.Platforms) != 0 && index.SizeBytes == 0 {
		t.Fatalf("index version: %+v", index)
	}
	if image.Kind != apigen.PackageVersionKindImage || strings.Join(image.Tags, ",") != "g1,master" || image.PushedBy == nil || image.PushedBy.Username != "mia" {
		t.Fatalf("image version: %+v", image)
	}

	// Testers can't delete; maintainers can.
	e.tom.mustDo("DELETE", "/repos/game/packages/tags?name=issues&tag=master", nil, 403)
	e.mia.mustDo("DELETE", "/repos/game/packages/tags?name=issues&tag=master", nil, 204)
	e.mia.mustDo("DELETE", "/repos/game/packages/tags?name=issues&tag=nope", nil, 404)
	e.mia.mustDo("DELETE", "/repos/game/packages/versions?name=issues&digest="+index.Digest, nil, 204)
	e.mia.mustDo("GET", "/repos/game/packages/versions?name=nope", nil, 404)
	if tags, _ := remote.List(e.ref(t, "bloxblast/game/issues:x").Context(), basic(tok)); strings.Join(tags, ",") != "g1" {
		t.Fatalf("tags after API deletes: %v", tags)
	}

	// Admin settings.
	e.mia.mustDo("GET", "/admin/registry/settings", nil, 403)
	var rs apigen.RegistrySettings
	e.admin.mustDo("PATCH", "/admin/registry/settings", map[string]any{"quota_mb": 500, "keep_last_tags": 10, "keep_last_tags_patterns": []string{"g*", "sha-*"}}, 200).decode(t, &rs)
	if rs.QuotaMb != 500 || rs.KeepLastTags != 10 || rs.UntaggedRetentionDays != 7 || len(rs.KeepLastTagsPatterns) != 2 {
		t.Fatalf("settings: %+v", rs)
	}
	if r := e.admin.do("PATCH", "/admin/registry/settings", map[string]any{"untagged_retention_days": 0}); r.status != 422 && r.status != 400 {
		t.Fatalf("invalid settings: %d %s", r.status, r.body)
	}
}
