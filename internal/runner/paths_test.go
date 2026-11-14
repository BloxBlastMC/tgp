package runner

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathPatterns(t *testing.T) {
	ps, err := parsePaths("dist/\n# comment\n!dist/**/*.map\n~/.m2/repository\n/abs/*.log\n", "/ws", "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ps.includes, ",") != "/ws/dist,/home/u/.m2/repository,/abs/*.log" || strings.Join(ps.excludes, ",") != "/ws/dist/**/*.map" {
		t.Fatalf("%+v", ps)
	}
	if _, err := parsePaths("!only/excludes", "/ws", "/h"); err == nil {
		t.Fatal("only exclusions accepted")
	}
	for _, c := range []struct{ pattern, root string }{
		{"/ws/dist", "/ws/dist"}, {"/ws/dist/**/*.js", "/ws/dist"}, {"/ws/*.txt", "/ws"}, {"/*", "/"}, {"/ws/a[0-9]/b", "/ws"},
	} {
		if r := staticRoot(c.pattern); r != c.root {
			t.Errorf("staticRoot(%s) = %s, want %s", c.pattern, r, c.root)
		}
	}
	for _, c := range []struct {
		pattern, path string
		want          bool
	}{
		{"/ws/dist", "/ws/dist/a/b.js", true},
		{"/ws/dist", "/ws/distx/a", false},
		{"/ws/dist/**/*.js", "/ws/dist/a.js", true},
		{"/ws/dist/**/*.js", "/ws/dist/x/y/a.js", true},
		{"/ws/dist/**/*.js", "/ws/dist/a.map", false},
		{"/ws/*.txt", "/ws/a.txt", true},
		{"/ws/*.txt", "/ws/sub/a.txt", false},
		{"/ws/a?.txt", "/ws/ab.txt", true},
		{"/ws/a?.txt", "/ws/a/.txt", false},
		{"/ws/[!b]*", "/ws/abc", true},
		{"/ws/[!b]*", "/ws/bcd", false},
		{"/ws/build/*", "/ws/build/libs/x.jar", true}, // a matching directory includes its contents
	} {
		if got := newMatcher(c.pattern).match(c.path); got != c.want {
			t.Errorf("match(%s, %s) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestCollectAndCommonRoot(t *testing.T) {
	ws := t.TempDir()
	for name, content := range map[string]string{
		"dist/app": "a", "dist/sub/lib.txt": "l", "dist/.hidden": "h", "dist/x.map": "m", "README": "r",
	} {
		p := filepath.Join(ws, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(content), 0o644)
	}
	env := &hostEnv{root: filepath.Dir(ws)}
	ps, _ := parsePaths("dist\n!**/*.map\nREADME\nmissing/", ws, "/h")
	var got []string
	n, err := collect(t.Context(), env, ps, false, func(full string, hdr *tar.Header, _ io.Reader) error {
		if hdr.Typeflag == tar.TypeReg {
			got = append(got, strings.TrimPrefix(full, ws+"/"))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || strings.Join(got, ",") != "README,dist/app,dist/sub/lib.txt" {
		t.Fatalf("collected %d: %v", n, got)
	}
	if r := commonRoot(t.Context(), env, ps); r != ws {
		t.Fatalf("common root %s", r)
	}
	one, _ := parsePaths("dist/app", ws, "/h")
	if r := commonRoot(t.Context(), env, one); r != filepath.Join(ws, "dist") {
		t.Fatalf("file root %s", r)
	}
}

func TestUntarRejectsEscapes(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "../../evil", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	dir := t.TempDir()
	// "../../evil" is cleaned to "evil" inside dir.
	if err := Untar(&buf, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); err != nil {
		t.Fatalf("not extracted inside: %v", err)
	}
}

func TestCacheVersion(t *testing.T) {
	if cacheVersion("a\nb\n") != cacheVersion("  a\n\nb") || cacheVersion("a") == cacheVersion("b") {
		t.Fatal("cache versions")
	}
}
