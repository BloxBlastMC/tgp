package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
)

// send makes a raw runner API request.
func (r *fakeRunner) send(method, path string, body []byte) resp {
	r.e.t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, r.e.ts.URL+runnerapi.Prefix+path, rd)
	req.Header.Set("Authorization", "Bearer "+r.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, body: data}
}

func (r *fakeRunner) lookup(job int64, key string, restore ...string) runnerapi.CacheLookupResponse {
	r.e.t.Helper()
	var out runnerapi.CacheLookupResponse
	res := r.post(fmt.Sprintf("/jobs/%d/caches/lookup", job), runnerapi.CacheLookupRequest{Key: key, RestoreKeys: restore, Version: "v1"})
	if res.status != 200 {
		r.e.t.Fatalf("lookup: %d %s", res.status, res.body)
	}
	res.decode(r.e.t, &out)
	return out
}

func TestCIStorageLimitsAndScopes(t *testing.T) {
	p := newPREnv(t)
	rn := p.registerRunner(p.admin, "r1", 4)
	p.commit("main", ".tgp/workflows/ci.yml", quickWF, "Add CI")
	main := rn.mustFetch()

	// Caches: save, duplicate, lookup by exact key and prefix.
	put := func(job int64, key string, size int) resp {
		return rn.send("PUT", fmt.Sprintf("/jobs/%d/caches?key=%s&version=v1", job, key), bytes.Repeat([]byte("c"), size))
	}
	if r := put(main.ID, "deps-1", 400<<10); r.status != 201 {
		t.Fatalf("save: %d %s", r.status, r.body)
	}
	if r := put(main.ID, "deps-1", 10); r.status != 409 {
		t.Fatalf("duplicate: %d %s", r.status, r.body)
	}
	if l := rn.lookup(main.ID, "deps-1"); l.Entry == nil || !l.Exact {
		t.Fatalf("exact: %+v", l)
	}
	if l := rn.lookup(main.ID, "deps-2", "nope-", "deps-"); l.Entry == nil || l.Exact || l.Entry.Key != "deps-1" {
		t.Fatalf("prefix: %+v", l)
	}
	if l := rn.lookup(main.ID, "other"); l.Entry != nil {
		t.Fatalf("miss: %+v", l)
	}
	if r := rn.send("GET", fmt.Sprintf("/jobs/%d/caches/%d", main.ID, rn.lookup(main.ID, "deps-1").Entry.ID), nil); r.status != 200 || len(r.body) != 400<<10 {
		t.Fatalf("download: %d %d", r.status, len(r.body))
	}

	// A feature branch reads main's caches but writes its own; main
	// doesn't see the feature's.
	p.commit("feature", "a.txt", "1", "Feature")
	feat := rn.mustFetch(main.ID)
	if l := rn.lookup(feat.ID, "deps-1"); l.Entry == nil || l.Entry.Ref != "refs/heads/main" {
		t.Fatalf("fallback: %+v", l)
	}
	if r := put(feat.ID, "deps-1", 300<<10); r.status != 201 {
		t.Fatalf("feature save: %d %s", r.status, r.body)
	}
	if l := rn.lookup(feat.ID, "deps-1"); l.Entry.Ref != "refs/heads/feature" {
		t.Fatalf("own scope first: %+v", l)
	}
	if l := rn.lookup(main.ID, "deps-1"); l.Entry.Ref != "refs/heads/main" {
		t.Fatalf("main sees feature: %+v", l)
	}

	// Quota: 1 MiB. Saving 500 KiB evicts the least recently used cache.
	p.setCI(map[string]any{"cache_quota_mb": 1})
	time.Sleep(10 * time.Millisecond)
	rn.lookup(main.ID, "deps-1") // main's cache was used most recently
	if r := put(feat.ID, "deps-2", 500<<10); r.status != 201 {
		t.Fatalf("save 3: %d %s", r.status, r.body)
	}
	var caches apigen.CachePage
	p.tom.mustDo("GET", "/repos/game/caches?sort=size", nil, 200).decode(t, &caches)
	if caches.Total != 2 || caches.QuotaBytes != 1<<20 || caches.TotalBytes != 900<<10 {
		t.Fatalf("after quota: %+v", caches)
	}
	for _, c := range caches.Caches {
		if c.Ref == "refs/heads/feature" && c.Key == "deps-1" {
			t.Fatalf("LRU cache not evicted: %+v", caches)
		}
	}
	if r := put(feat.ID, "huge", 2<<20); r.status != 413 {
		t.Fatalf("over quota: %d %s", r.status, r.body)
	}

	// Testers can't delete caches; maintainers can, by key.
	p.tom.mustDo("DELETE", "/repos/game/caches?key=deps-1", nil, 403)
	var del apigen.DeletedCount
	p.mia.mustDo("DELETE", "/repos/game/caches?key=deps-1", nil, 200).decode(t, &del)
	if del.Deleted != 1 {
		t.Fatalf("deleted: %+v", del)
	}

	// Artifacts: names, duplicates, overwrite, size limit, cross-run access.
	art := func(job int64, q string, size int) resp {
		return rn.send("PUT", fmt.Sprintf("/jobs/%d/artifacts?%s", job, q), bytes.Repeat([]byte("z"), size))
	}
	if r := art(main.ID, "name=a/b", 1); r.code() != "validation_failed" {
		t.Fatalf("bad name: %s", r.body)
	}
	if r := art(main.ID, "name=dist&retention_days=3", 100); r.status != 201 {
		t.Fatalf("upload: %d %s", r.status, r.body)
	}
	if r := art(main.ID, "name=dist", 100); r.status != 409 {
		t.Fatalf("duplicate: %d %s", r.status, r.body)
	}
	if r := art(main.ID, "name=dist&overwrite=1&retention_days=3", 120); r.status != 201 {
		t.Fatalf("overwrite: %d %s", r.status, r.body)
	}
	p.setCI(map[string]any{"artifact_max_mb": 1})
	if r := art(main.ID, "name=big", 1<<20+1); r.status != 413 {
		t.Fatalf("too big: %d %s", r.status, r.body)
	}
	var arts []apigen.Artifact
	p.tom.mustDo("GET", "/repos/game/runs/1/artifacts", nil, 200).decode(t, &arts)
	if len(arts) != 1 || arts[0].SizeBytes != 120 || arts[0].ExpiresAt.Sub(arts[0].CreatedAt) != 72*time.Hour {
		t.Fatalf("artifacts: %+v", arts)
	}
	var list runnerapi.ArtifactList
	rn.send("GET", fmt.Sprintf("/jobs/%d/artifacts", feat.ID), nil).decode(t, &list)
	if len(list.Artifacts) != 0 {
		t.Fatalf("other run sees artifacts: %+v", list)
	}
	if r := rn.send("GET", fmt.Sprintf("/jobs/%d/artifacts/%d", feat.ID, arts[0].Id), nil); r.status != 404 {
		t.Fatalf("cross-run download: %d", r.status)
	}
	p.tom.mustDo("DELETE", fmt.Sprintf("/repos/game/artifacts/%d", arts[0].Id), nil, 403)

	// Once the job finishes, its token can't upload any more.
	rn.complete(main.ID, "success", nil)
	if r := art(main.ID, "name=late", 1); r.status != 409 {
		t.Fatalf("after completion: %d %s", r.status, r.body)
	}

	// Retention: expired artifacts are deleted by the janitor.
	p.srv.ci.now = func() time.Time { return time.Now().Add(4 * 24 * time.Hour) }
	defer func() { p.srv.ci.now = nil }()
	if err := p.srv.RunCIJanitor(t.Context()); err != nil {
		t.Fatal(err)
	}
	var page apigen.ArtifactPage
	p.admin.mustDo("GET", "/repos/game/artifacts", nil, 200).decode(t, &page)
	if page.Total != 0 {
		t.Fatalf("not expired: %+v", page)
	}
}
