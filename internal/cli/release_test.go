package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/runner"
)

func TestReleaseArtifactCacheCommands(t *testing.T) {
	e := newCLIEnv(t)
	e.admin.Do(http.MethodPost, "/repos", map[string]any{"name": "game", "auto_init": true}, nil)
	e.admin.ActivateUser("tom", "tester", "tom-password-1", true)
	tomTok := e.srv.Login("tom", "tom-password-1").Token("read", "write")
	e.login("admin", "correct horse battery")
	R := []string{"-R", "game"}
	with := func(args ...string) []string { return append(args, R...) }
	work := t.TempDir()
	jar := filepath.Join(work, "game.jar")
	_ = os.WriteFile(jar, []byte("jar-bytes"), 0o644)
	notes := filepath.Join(work, "NOTES.md")
	_ = os.WriteFile(notes, []byte("## Changes\n- faster\n"), 0o644)

	// Create a release with a file; view it; list.
	var rel map[string]any
	e.jsonOK(&rel, with("release", "create", "v1.0", jar+"#Server jar", "-t", "First", "-F", notes)...)
	assets := rel["assets"].([]any)
	if rel["tag"] != "v1.0" || rel["latest"] != true || len(assets) != 1 || assets[0].(map[string]any)["label"] != "Server jar" {
		t.Fatalf("create: %v", rel)
	}
	if r := e.ok("", with("release", "view")...); !strings.Contains(r.stdout, "First [Latest]") || !strings.Contains(r.stdout, "game.jar") || !strings.Contains(r.stdout, "## Changes") {
		t.Fatalf("view: %s", r.stdout)
	}
	e.jsonOK(&rel, with("release", "create", "v1.1", "--draft")...)
	var rels []map[string]any
	e.jsonOK(&rels, with("release", "list")...)
	if len(rels) != 2 || rels[0]["tag"] != "v1.1" {
		t.Fatalf("list: %v", rels)
	}
	e.as(tomTok, func() {
		e.jsonOK(&rels, with("release", "list")...)
		if len(rels) != 1 {
			t.Fatalf("tester sees drafts: %v", rels)
		}
		e.fail(ExitForbidden, "forbidden", with("release", "create", "v9", "--json")...)
	})

	// Upload (clobber), download, archive, publish, delete.
	_ = os.WriteFile(jar, []byte("jar-bytes-2"), 0o644)
	e.fail(ExitConflict, "conflict", with("release", "upload", "v1.0", jar, "--json")...)
	var up []map[string]any
	e.jsonOK(&up, with("release", "upload", "v1.0", jar, "--clobber")...)
	if len(up) != 1 || up[0]["size_bytes"] != float64(11) {
		t.Fatalf("upload: %v", up)
	}
	dl := filepath.Join(work, "dl")
	var got []map[string]any
	e.as(tomTok, func() {
		e.jsonOK(&got, with("release", "download", "v1.0", "-p", "*.jar", "-D", dl)...)
	})
	if b, _ := os.ReadFile(filepath.Join(dl, "game.jar")); len(got) != 1 || string(b) != "jar-bytes-2" {
		t.Fatalf("download: %v %q", got, b)
	}
	e.fail(ExitConflict, "conflict", with("release", "download", "v1.0", "-D", dl, "--json")...)
	e.fail(ExitNotFound, "not_found", with("release", "download", "v1.0", "-p", "*.exe", "-D", dl, "--json")...)
	e.jsonOK(&got, with("release", "download", "v1.0", "--archive", "zip", "-D", dl)...)
	if got[0]["name"] != "game-v1.0.zip" {
		t.Fatalf("archive: %v", got)
	}
	e.jsonOK(&rel, with("release", "edit", "v1.1", "--draft=false", "--prerelease")...)
	if rel["draft"] != false || rel["prerelease"] != true || rel["sha"] == nil {
		t.Fatalf("publish: %v", rel)
	}
	e.fail(ExitUsage, CodeInputRequired, with("release", "delete", "v1.1", "--json")...)
	e.jsonOK(nil, with("release", "delete-asset", "v1.0", "game.jar", "--yes")...)
	e.jsonOK(nil, with("release", "delete", "v1.1", "--yes", "--cleanup-tag")...)
	e.fail(ExitNotFound, "not_found", with("release", "view", "v1.1", "--json")...)

	// Artifacts and caches, made by a runner speaking the protocol.
	var tok map[string]any
	e.jsonOK(&tok, "admin", "runner", "token")
	ctx := context.Background()
	reg, err := runner.NewClient(e.srv.URL, "").Register(ctx, runnerapi.RegisterRequest{Token: tok["token"].(string), Name: "r1", Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	rc := runner.NewClient(e.srv.URL, reg.Token)
	e.ok("", "repo", "clone", "game")
	dir := filepath.Join(e.home, "game")
	t.Chdir(dir)
	_ = os.MkdirAll(filepath.Join(dir, ".tgp/workflows"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".tgp/workflows/ci.yml"), []byte("on: push\njobs:\n  build:\n    steps: [{run: make}]\n"), 0o644)
	e.git(dir, "add", ".")
	e.git(dir, "commit", "-qm", "CI")
	e.git(dir, "push", "-q", "origin", "main")
	job, err := rc.Fetch(ctx, runnerapi.FetchRequest{Capacity: 1})
	if err != nil || job == nil {
		t.Fatalf("fetch: %v %v", job, err)
	}
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("bin/app")
	_, _ = w.Write([]byte("app"))
	_ = zw.Close()
	if _, err := rc.UploadArtifact(ctx, job.ID, "dist", 0, false, bytes.NewReader(zbuf.Bytes()), int64(zbuf.Len())); err != nil {
		t.Fatal(err)
	}
	if _, err := rc.SaveCache(ctx, job.ID, "deps-1", "v1", strings.NewReader("tgz"), 3); err != nil {
		t.Fatal(err)
	}
	var arts []map[string]any
	e.jsonOK(&arts, "artifact", "list", "--run", "1")
	if len(arts) != 1 || arts[0]["name"] != "dist" {
		t.Fatalf("artifacts: %v", arts)
	}
	out := filepath.Join(work, "art")
	var da []map[string]any
	e.as(tomTok, func() {
		e.jsonOK(&da, "artifact", "download", "1", "-n", "dist", "-D", out)
	})
	if b, _ := os.ReadFile(filepath.Join(out, "bin/app")); string(b) != "app" || da[0]["files"] != float64(1) {
		t.Fatalf("artifact download: %v %q", da, b)
	}
	e.jsonOK(&da, "artifact", "download", "-D", out) // latest run, per-name directory
	if _, err := os.Stat(filepath.Join(out, "dist", "bin", "app")); err != nil {
		t.Fatalf("per-name dir: %v", err)
	}
	var caches map[string]any
	e.jsonOK(&caches, "cache", "list")
	if caches["total"] != float64(1) || caches["quota_bytes"] != float64(10240<<20) {
		t.Fatalf("caches: %v", caches)
	}
	e.as(tomTok, func() {
		e.fail(ExitForbidden, "forbidden", "cache", "delete", "--all", "--yes", "--json")
	})
	e.fail(ExitUsage, CodeUsage, "cache", "delete", "--json")
	var del map[string]any
	e.jsonOK(&del, "cache", "delete", "--key", "deps-1")
	if del["deleted"] != float64(1) {
		t.Fatalf("cache delete: %v", del)
	}
	e.jsonOK(nil, "artifact", "delete", strconv.FormatInt(int64(arts[0]["id"].(float64)), 10), "--yes")
}
