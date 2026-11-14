package runner

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The host executor gives every job a fresh workspace path, so the cache
// lives at a fixed absolute path (in Docker, paths are stable).
const artifactsWorkflow = `name: Build
on: [push]
jobs:
  build:
    steps:
      - uses: checkout
      - run: |
          mkdir -p out/sub
          echo bin > out/app
          echo lib > out/sub/lib.txt
          echo hidden > out/.secret
          echo log > out/build.log
          test -f go.sum || echo 'module x' > go.sum
      - uses: actions/upload-artifact@v4
        with:
          name: dist
          path: |
            out/
            !out/**/*.log
      - uses: actions/upload-artifact@v4
        continue-on-error: true
        with:
          name: none
          path: nothing/*
          if-no-files-found: error
      - id: c
        uses: actions/cache@v4
        with:
          key: deps-${{ hashFiles('**/go.sum') }}
          restore-keys: deps-
          path: %[1]s
      - name: Fill the cache on a miss
        if: steps.c.outputs.cache-hit != 'true'
        run: mkdir -p %[1]s && echo cached > %[1]s/data.txt
      - name: Check a hit
        if: steps.c.outputs.cache-hit == 'true'
        run: test "$(cat %[1]s/data.txt)" = cached && echo "restored ok"
  test:
    needs: build
    steps:
      - uses: actions/download-artifact@v4
        with: { name: dist, path: got }
      - run: test "$(cat got/app)" = bin && test "$(cat got/sub/lib.txt)" = lib && test ! -e got/.secret && test ! -e got/build.log
      - uses: actions/download-artifact@v4
        with: { path: all }
      - run: test -f all/dist/app && echo "all ok"
      - uses: actions/download-artifact@v4
        with: { name: missing }
        continue-on-error: true
`

func TestRunnerArtifactsAndCache(t *testing.T) {
	e := newCIEnv(t)
	cacheDir := filepath.Join(t.TempDir(), "deps")
	e.startRunner("r1", 2)
	e.push("main", "Add build", map[string]string{".tgp/workflows/build.yml": fmt.Sprintf(artifactsWorkflow, cacheDir)})

	r := e.waitRun(1, "run 1", completed)
	buildID, _ := e.jobByName(r, "build")
	testID, _ := e.jobByName(r, "test")
	if r.conclusion() != "success" {
		t.Fatalf("run 1: %+v\nbuild:\n%s\ntest:\n%s", r, e.logs(buildID), e.logs(testID))
	}
	bl := e.logs(buildID)
	for _, want := range []string{"Uploading artifact dist: 2 files", "No files were found with the provided path", "Cache not found for key: deps-", "Saved cache deps-"} {
		if !strings.Contains(bl, want) {
			t.Errorf("build log lacks %q:\n%s", want, bl)
		}
	}
	tl := e.logs(testID)
	for _, want := range []string{"all ok", `artifact "missing" not found (available: dist)`} {
		if !strings.Contains(tl, want) {
			t.Errorf("test log lacks %q:\n%s", want, tl)
		}
	}

	// The API lists the artifact and serves its zip.
	var arts []struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		SizeBytes int64  `json:"size_bytes"`
		JobName   string `json:"job_name"`
	}
	e.admin.Do(http.MethodGet, "/repos/game/runs/1/artifacts", nil, &arts)
	if len(arts) != 1 || arts[0].Name != "dist" || arts[0].JobName != "build" {
		t.Fatalf("artifacts: %+v", arts)
	}
	resp := e.admin.Send(http.MethodGet, fmt.Sprintf("/repos/game/artifacts/%d/zip", arts[0].ID), nil, "")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Disposition"), `filename="dist.zip"`) {
		t.Fatalf("download: %d %v", resp.StatusCode, resp.Header)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "app,sub/lib.txt" {
		t.Fatalf("zip entries: %v", names)
	}

	var caches struct {
		Caches []struct {
			Key string `json:"key"`
			Ref string `json:"ref"`
		} `json:"caches"`
		TotalBytes int64 `json:"total_bytes"`
	}
	e.admin.Do(http.MethodGet, "/repos/game/caches", nil, &caches)
	if len(caches.Caches) != 1 || caches.Caches[0].Ref != "refs/heads/main" || caches.TotalBytes == 0 {
		t.Fatalf("caches: %+v", caches)
	}

	// Run 2 restores the cache from a clean slate.
	if err := os.RemoveAll(cacheDir); err != nil {
		t.Fatal(err)
	}
	e.push("main", "Again", map[string]string{"x.txt": "x"})
	r = e.waitRun(2, "run 2", completed)
	buildID, _ = e.jobByName(r, "build")
	bl = e.logs(buildID)
	if r.conclusion() != "success" || !strings.Contains(bl, "Restoring cache deps-") || !strings.Contains(bl, "restored ok") || strings.Contains(bl, "Saved cache") {
		t.Fatalf("run 2 %s:\n%s", r.conclusion(), bl)
	}

	// A feature branch reads main's cache but saves its own.
	e.push("feature", "Feature", map[string]string{"go.sum": "module y\n"})
	r = e.waitRun(3, "run 3", completed)
	buildID, _ = e.jobByName(r, "build")
	bl = e.logs(buildID)
	if !strings.Contains(bl, "saved from refs/heads/main") || !strings.Contains(bl, "Saved cache deps-") {
		t.Fatalf("run 3 %s:\n%s", r.conclusion(), bl)
	}
	e.admin.Do(http.MethodGet, "/repos/game/caches?ref=refs/heads/feature", nil, &caches)
	if len(caches.Caches) != 1 {
		t.Fatalf("feature caches: %+v", caches)
	}

	// Deleting an artifact; expired ones disappear.
	e.admin.Do(http.MethodDelete, fmt.Sprintf("/repos/game/artifacts/%d", arts[0].ID), nil, nil)
	e.admin.Do(http.MethodGet, "/repos/game/runs/1/artifacts", nil, &arts)
	if len(arts) != 0 {
		t.Fatalf("after delete: %+v", arts)
	}
}
