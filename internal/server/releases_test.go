package server

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

func (c *client) raw(method, path string) (*http.Response, []byte) {
	c.env.t.Helper()
	req, _ := http.NewRequest(method, c.env.ts.URL+"/api/v1"+path, nil)
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		c.env.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func tagNames(t *testing.T, c *client) string {
	t.Helper()
	var refs []apigen.Ref
	c.mustDo("GET", "/repos/game/tags", nil, 200).decode(t, &refs)
	var names []string
	for _, r := range refs {
		names = append(names, r.Name)
	}
	return strings.Join(names, ",")
}

func TestReleases(t *testing.T) {
	p := newPREnv(t)
	p.commit("main", ".tgp/workflows/tags.yml", "name: Tags\non:\n  push:\n    tags: ['v*']\njobs:\n  j:\n    steps: [{run: echo}]\n", "Tag CI")
	p.commit("main", ".tgp/workflows/release.yml", "name: Publish\non:\n  release:\njobs:\n  j:\n    steps: [{run: echo}]\n", "Release CI")
	p.commit("main", ".tgp/workflows/final.yml", "name: Final\non:\n  release:\n    types: [released]\njobs:\n  j:\n    steps: [{run: echo}]\n", "Final CI")
	runsBefore := len(p.runs())

	// Testers can't create releases; bad tags are rejected.
	p.tom.mustDo("POST", "/repos/game/releases", map[string]any{"tag": "v1.0"}, 403)
	if r := p.mia.do("POST", "/repos/game/releases", map[string]any{"tag": "bad..tag"}); r.code() != "validation_failed" {
		t.Fatalf("bad tag: %d %s", r.status, r.body)
	}
	if r := p.mia.do("POST", "/repos/game/releases", map[string]any{"tag": "v1.0", "target": "nope", "draft": true}); r.code() != "validation_failed" {
		t.Fatalf("bad target: %d %s", r.status, r.body)
	}

	// A draft creates no tag and is invisible to testers.
	var rel apigen.Release
	p.mia.mustDo("POST", "/repos/game/releases", map[string]any{"tag": "v1.0", "name": "First", "body": "Notes **bold**", "draft": true}, 201).decode(t, &rel)
	if !rel.Draft || rel.Sha != nil || rel.PublishedAt != nil || rel.Target != "main" || rel.Author == nil || rel.Author.Username != "mia" {
		t.Fatalf("draft: %+v", rel)
	}
	if tags := tagNames(t, p.admin); strings.Contains(tags, "v1.0") {
		t.Fatalf("draft created a tag: %s", tags)
	}
	var page apigen.ReleasePage
	p.tom.mustDo("GET", "/repos/game/releases", nil, 200).decode(t, &page)
	if page.Total != 0 {
		t.Fatalf("tester sees drafts: %+v", page)
	}
	p.tom.mustDo("GET", "/repos/game/releases/tags/v1.0", nil, 404)
	p.mia.mustDo("POST", "/repos/game/releases", map[string]any{"tag": "v1.0"}, 409)

	// Assets: upload, duplicate, overwrite, bad names.
	id := itoa(rel.Id)
	var asset apigen.ReleaseAsset
	p.mia.mustDo("POST", "/repos/game/releases/"+id+"/assets?name=game.zip", []byte("zipdata-1"), 201).decode(t, &asset)
	if asset.SizeBytes != 9 || asset.ContentType != "application/zip" || asset.Sha256 == "" {
		t.Fatalf("asset: %+v", asset)
	}
	p.mia.mustDo("POST", "/repos/game/releases/"+id+"/assets?name=game.zip", []byte("zipdata-2"), 409)
	p.mia.mustDo("POST", "/repos/game/releases/"+id+"/assets?name=game.zip&overwrite=true", []byte("zipdata-22"), 201).decode(t, &asset)
	if r := p.mia.do("POST", "/repos/game/releases/"+id+"/assets?name=a/b", []byte("x")); r.code() != "validation_failed" {
		t.Fatalf("bad name: %s", r.body)
	}
	p.tom.mustDo("POST", "/repos/game/releases/"+id+"/assets?name=x.txt", []byte("x"), 403)
	// Uploads bigger than the general 2 MiB API limit work; the asset limit applies.
	big := bytes.Repeat([]byte("a"), 3<<20)
	p.mia.mustDo("POST", "/repos/game/releases/"+id+"/assets?name=big.bin", big, 201)
	p.setCI(map[string]any{"release_asset_max_mb": 1})
	if r := p.mia.do("POST", "/repos/game/releases/"+id+"/assets?name=big2.bin", big); r.status != 413 || r.code() != "payload_too_large" {
		t.Fatalf("too large: %d %s", r.status, r.body)
	}

	// Publishing creates the tag and runs tag and release workflows.
	p.mia.mustDo("PATCH", "/repos/game/releases/"+id, map[string]any{"draft": false}, 200).decode(t, &rel)
	if rel.Draft || rel.Sha == nil || rel.PublishedAt == nil || !rel.Latest || !strings.Contains(rel.BodyHtml, "<strong>bold</strong>") || len(rel.Assets) != 2 {
		t.Fatalf("published: %+v", rel)
	}
	if tags := tagNames(t, p.admin); !strings.Contains(tags, "v1.0") {
		t.Fatalf("tags: %s", tags)
	}
	events := func() map[string]int {
		m := map[string]int{}
		for _, r := range p.runs()[:len(p.runs())-runsBefore] {
			m[r.WorkflowName+"/"+string(r.Event)]++
		}
		return m
	}
	if ev := events(); ev["Tags/push"] != 1 || ev["Publish/release"] != 1 || ev["Final/release"] != 1 || len(ev) != 3 {
		t.Fatalf("runs after publish: %v", ev)
	}
	if runs := p.runs(); runs[0].Title != "Release First" || runs[0].Ref != "refs/tags/v1.0" {
		t.Fatalf("release run: %+v", runs[0])
	}

	// A prerelease isn't the latest and doesn't trigger types: [released].
	var rc apigen.Release
	p.mia.mustDo("POST", "/repos/game/releases", map[string]any{"tag": "v2.0-rc1", "prerelease": true}, 201).decode(t, &rc)
	if rc.Latest {
		t.Fatal("prerelease is latest")
	}
	if ev := events(); ev["Publish/release"] != 2 || ev["Final/release"] != 1 || ev["Tags/push"] != 2 {
		t.Fatalf("runs after prerelease: %v", ev)
	}
	var latest apigen.Release
	p.tom.mustDo("GET", "/repos/game/releases/latest", nil, 200).decode(t, &latest)
	if latest.Tag != "v1.0" {
		t.Fatalf("latest: %s", latest.Tag)
	}
	p.tom.mustDo("GET", "/repos/game/releases", nil, 200).decode(t, &page)
	if page.Total != 2 || page.Releases[0].Tag != "v2.0-rc1" {
		t.Fatalf("list: %+v", page)
	}

	// Testers download assets; the count goes up.
	res, body := p.tom.raw("GET", fmt.Sprintf("/repos/game/releases/%d/assets/%d", rel.Id, asset.Id))
	if res.StatusCode != 200 || string(body) != "zipdata-22" || !strings.Contains(res.Header.Get("Content-Disposition"), `filename="game.zip"`) {
		t.Fatalf("download: %d %q %v", res.StatusCode, body, res.Header)
	}
	p.tom.mustDo("GET", "/repos/game/releases/tags/v1.0", nil, 200).decode(t, &rel)
	for _, a := range rel.Assets {
		if a.Name == "game.zip" && a.DownloadCount != 1 {
			t.Fatalf("download count: %+v", a)
		}
	}

	// Source archives.
	res, body = p.tom.raw("GET", "/repos/game/archive?ref=v1.0&format=zip")
	if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Disposition"), `game-v1.0.zip`) {
		t.Fatalf("archive: %d %v", res.StatusCode, res.Header)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range zr.File {
		found = found || f.Name == "game-v1.0/README.md"
	}
	if !found {
		t.Fatal("archive lacks game-v1.0/README.md")
	}
	if res, _ := p.tom.raw("GET", "/repos/game/archive?ref=v1.0"); res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Disposition"), "game-v1.0.tar.gz") {
		t.Fatalf("tar.gz: %v", res.Header)
	}
	p.tom.mustDo("GET", "/repos/game/archive?ref=nope", nil, 404)

	// Tags with slashes, and a release for a tag pushed with git.
	p.g.must(p.dir, "tag", "team/v3")
	p.g.must(p.dir, "push", "-q", "origin", "team/v3")
	var slash apigen.Release
	p.mia.mustDo("POST", "/repos/game/releases", map[string]any{"tag": "team/v3"}, 201).decode(t, &slash)
	head := strings.TrimSpace(p.g.must(p.dir, "rev-parse", "HEAD"))
	if slash.Sha == nil || *slash.Sha != head {
		t.Fatalf("existing tag sha: %v want %s", slash.Sha, head)
	}
	p.tom.mustDo("GET", "/repos/game/releases/tags/team%2Fv3", nil, 200)

	// Delete with the tag; testers can't delete.
	p.tom.mustDo("DELETE", "/repos/game/releases/"+id, nil, 403)
	p.mia.mustDo("DELETE", "/repos/game/releases/"+id+"?delete_tag=true", nil, 204)
	if tags := tagNames(t, p.admin); strings.Contains(tags, "v1.0,") || strings.HasSuffix(tags, "v1.0") {
		t.Fatalf("tag kept: %s", tags)
	}
	p.mia.mustDo("GET", "/repos/game/releases/"+id, nil, 404)
	p.mia.mustDo("DELETE", "/repos/game/releases/"+itoa(rc.Id), nil, 204)
	if tags := tagNames(t, p.admin); !strings.Contains(tags, "v2.0-rc1") {
		t.Fatalf("tag deleted without delete_tag: %s", tags)
	}
}
