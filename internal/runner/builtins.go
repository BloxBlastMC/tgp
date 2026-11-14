package runner

import (
	"archive/tar"
	"archive/zip"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
)

// homeDir is $HOME in the job environment.
func (j *jobRun) homeDir(ctx context.Context) string {
	if j.home != "" {
		return j.home
	}
	var out strings.Builder
	if code, err := j.env.Exec(ctx, ExecOptions{Cmd: []string{"sh", "-c", `printf %s "$HOME"`}, Output: &out}); err == nil && code == 0 && out.Len() > 0 {
		j.home = out.String()
	} else {
		j.home = "/root"
	}
	return j.home
}

// resolveDir makes a path input absolute (relative to the workspace).
func (j *jobRun) resolveDir(ctx context.Context, p string) string {
	switch {
	case p == "":
		return j.env.Workspace()
	case p == "~" || strings.HasPrefix(p, "~/"):
		return path.Clean(j.homeDir(ctx) + p[1:])
	case path.IsAbs(p):
		return path.Clean(p)
	}
	return path.Join(j.env.Workspace(), p)
}

func boolInput(with map[string]string, name string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(with[name])) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	}
	return false, fmt.Errorf("%s must be true or false, got %q", name, with[name])
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// tempFile creates a temporary file in the runner's work directory.
func (j *jobRun) tempFile(pattern string) (*os.File, error) {
	if err := os.MkdirAll(j.r.cfg.WorkDir, 0o700); err != nil {
		return nil, err
	}
	return os.CreateTemp(j.r.cfg.WorkDir, fmt.Sprintf("job-%d-%s", j.spec.ID, pattern))
}

// transferCtx bounds an upload or download that the server may refuse
// late (e.g. a size limit); the step's own context still applies.
func transferCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 6*time.Hour)
}

// hashFiles implements the hashFiles() expression function like GitHub:
// the SHA-256 of the SHA-256s of the matching workspace files, in path
// order; "" if none match or the job has no environment yet.
func (j *jobRun) hashFiles(ctx context.Context, patterns []string) string {
	if j.env == nil {
		return ""
	}
	ps, err := parsePaths(strings.Join(patterns, "\n"), j.env.Workspace(), j.homeDir(ctx))
	if err != nil {
		return ""
	}
	sums := map[string][]byte{}
	if _, err := collect(ctx, j.env, ps, true, func(full string, hdr *tar.Header, r io.Reader) error {
		if hdr.Typeflag != tar.TypeReg {
			return nil
		}
		h := sha256.New()
		if _, err := io.Copy(h, r); err != nil {
			return err
		}
		sums[full] = h.Sum(nil)
		return nil
	}); err != nil || len(sums) == 0 {
		return ""
	}
	names := make([]string, 0, len(sums))
	for k := range sums {
		names = append(names, k)
	}
	sort.Strings(names)
	all := sha256.New()
	for _, k := range names {
		all.Write(sums[k])
	}
	return hex.EncodeToString(all.Sum(nil))
}

// ---- upload-artifact ----

func (j *jobRun) uploadArtifact(ctx context.Context, n int, with map[string]string) error {
	name := strings.TrimSpace(with["name"])
	if name == "" {
		name = "artifact"
	}
	ps, err := parsePaths(with["path"], j.env.Workspace(), j.homeDir(ctx))
	if err != nil {
		return err
	}
	ifNone := strings.ToLower(strings.TrimSpace(with["if-no-files-found"]))
	switch ifNone {
	case "":
		ifNone = "warn"
	case "warn", "error", "ignore":
	default:
		return fmt.Errorf("if-no-files-found must be warn, error or ignore, got %q", with["if-no-files-found"])
	}
	retention := 0
	if v := strings.TrimSpace(with["retention-days"]); v != "" && v != "0" {
		if retention, err = strconv.Atoi(v); err != nil || retention < 1 {
			return fmt.Errorf("retention-days must be a number of days ≥ 1, got %q", v)
		}
	}
	overwrite, err := boolInput(with, "overwrite")
	if err != nil {
		return err
	}
	hidden, err := boolInput(with, "include-hidden-files")
	if err != nil {
		return err
	}
	level := flate.DefaultCompression
	if v := strings.TrimSpace(with["compression-level"]); v != "" {
		if level, err = strconv.Atoi(v); err != nil || level < 0 || level > 9 {
			return fmt.Errorf("compression-level must be 0-9, got %q", v)
		}
	}

	root := commonRoot(ctx, j.env, ps)
	f, err := j.tempFile("artifact-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	zw := zip.NewWriter(f)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) { return flate.NewWriter(w, level) })
	skipped := 0
	files, err := collect(ctx, j.env, ps, hidden, func(full string, hdr *tar.Header, r io.Reader) error {
		if hdr.Typeflag != tar.TypeReg {
			if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
				skipped++
			}
			return nil
		}
		rel := strings.TrimPrefix(full, strings.TrimSuffix(root, "/")+"/")
		if rel == full {
			rel = path.Base(full)
		}
		fh := &zip.FileHeader{Name: rel, Method: zip.Deflate, Modified: hdr.ModTime}
		if level == 0 {
			fh.Method = zip.Store
		}
		fh.SetMode(os.FileMode(hdr.Mode) & os.ModePerm)
		w, err := zw.CreateHeader(fh)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, r)
		return err
	})
	if err != nil {
		return fmt.Errorf("collecting files: %w", err)
	}
	if skipped > 0 {
		j.printf(n, "##[warning]Skipped %d symbolic or hard links (artifacts only hold regular files)", skipped)
	}
	if files == 0 {
		msg := fmt.Sprintf("No files were found with the provided path: %s. No artifact will be uploaded.", strings.Join(ps.includes, ", "))
		switch ifNone {
		case "error":
			return errors.New(msg)
		case "warn":
			j.printf(n, "##[warning]%s", msg)
		default:
			j.printf(n, "%s", msg)
		}
		return nil
	}
	if err := zw.Close(); err != nil {
		return err
	}
	size, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	j.printf(n, "Uploading artifact %s: %d files, %s (relative to %s)", name, files, humanBytes(size), root)
	tctx, cancel := transferCtx(ctx)
	defer cancel()
	a, err := j.r.client.UploadArtifact(tctx, j.spec.ID, name, retention, overwrite, f, size)
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status < 500 {
			return fmt.Errorf("upload refused: %s", ae.Message)
		}
		return fmt.Errorf("upload failed: %w", err)
	}
	j.printf(n, "Artifact %s uploaded (id %d, sha256 %s)", a.Name, a.ID, a.SHA256)
	return nil
}

// ---- download-artifact ----

func (j *jobRun) downloadArtifact(ctx context.Context, n int, with map[string]string) error {
	name, pattern := strings.TrimSpace(with["name"]), strings.TrimSpace(with["pattern"])
	merge, err := boolInput(with, "merge-multiple")
	if err != nil {
		return err
	}
	dest := j.resolveDir(ctx, strings.TrimSpace(with["path"]))
	arts, err := j.r.client.ListArtifacts(ctx, j.spec.ID)
	if err != nil {
		return fmt.Errorf("listing artifacts: %w", err)
	}
	var pick []runnerapi.Artifact
	for _, a := range arts {
		switch {
		case name != "":
			if a.Name == name {
				pick = append(pick, a)
			}
		case pattern != "":
			if ok, _ := path.Match(pattern, a.Name); ok {
				pick = append(pick, a)
			}
		default:
			pick = append(pick, a)
		}
	}
	if name != "" && len(pick) == 0 {
		var names []string
		for _, a := range arts {
			names = append(names, a.Name)
		}
		have := "this run has no artifacts yet"
		if len(names) > 0 {
			have = "available: " + strings.Join(names, ", ")
		}
		return fmt.Errorf("artifact %q not found (%s); the job that uploads it must finish first, so list it in needs", name, have)
	}
	if len(pick) == 0 {
		j.printf(n, "No artifacts to download")
		return nil
	}
	for _, a := range pick {
		to := dest
		if name == "" && !merge {
			to = path.Join(dest, a.Name)
		}
		j.printf(n, "Downloading artifact %s (%s) to %s", a.Name, humanBytes(a.Size), to)
		if err := j.fetchArtifact(ctx, a, to); err != nil {
			return fmt.Errorf("artifact %s: %w", a.Name, err)
		}
	}
	return nil
}

func (j *jobRun) fetchArtifact(ctx context.Context, a runnerapi.Artifact, to string) error {
	f, err := j.tempFile("download-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	tctx, cancel := transferCtx(ctx)
	defer cancel()
	body, err := j.r.client.DownloadArtifact(tctx, j.spec.ID, a.ID)
	if err != nil {
		return err
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, h), body)
	body.Close()
	if err != nil {
		return err
	}
	if sum := hex.EncodeToString(h.Sum(nil)); a.SHA256 != "" && sum != a.SHA256 {
		return fmt.Errorf("checksum mismatch (got %s, want %s)", sum, a.SHA256)
	}
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(zipToTar(zr, pw)) }()
	err = j.env.CopyInTar(ctx, to, pr)
	pr.CloseWithError(err)
	return err
}

// zipToTar rewrites a zip as a tar stream.
func zipToTar(zr *zip.Reader, w io.Writer) error {
	tw := tar.NewWriter(w)
	for _, zf := range zr.File {
		name := path.Clean("/" + zf.Name)[1:]
		if name == "" || strings.HasPrefix(zf.Name, "/") || strings.Contains(zf.Name, "\\") {
			continue
		}
		mode := int64(zf.Mode() & os.ModePerm)
		if zf.FileInfo().IsDir() {
			if err := tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: max(mode, 0o755), ModTime: zf.Modified}); err != nil {
				return err
			}
			continue
		}
		if mode == 0 {
			mode = 0o644
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(zf.UncompressedSize64), ModTime: zf.Modified}); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return tw.Close()
}

// ---- cache ----

// cacheSave is a cache to save after the job's steps (when it missed).
type cacheSave struct {
	step    int
	key     string
	version string
	paths   pathSet
}

// cacheVersion ties a cache to its paths, so the same key with different
// paths doesn't restore the wrong files.
func cacheVersion(pathInput string) string {
	var lines []string
	for _, l := range strings.Split(pathInput, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	h := sha256.Sum256([]byte(strings.Join(lines, "\n") + "|tar.gz|tgp-cache-1"))
	return hex.EncodeToString(h[:16])
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func (j *jobRun) cache(ctx context.Context, n int, with map[string]string) (map[string]string, error) {
	key := strings.TrimSpace(with["key"])
	if key == "" {
		return nil, errors.New("key is required")
	}
	ps, err := parsePaths(with["path"], j.env.Workspace(), j.homeDir(ctx))
	if err != nil {
		return nil, err
	}
	failMiss, err := boolInput(with, "fail-on-cache-miss")
	if err != nil {
		return nil, err
	}
	lookupOnly, err := boolInput(with, "lookup-only")
	if err != nil {
		return nil, err
	}
	version := cacheVersion(with["path"])
	outputs := map[string]string{"cache-hit": "false", "cache-primary-key": key, "cache-matched-key": ""}
	res, err := j.r.client.LookupCache(ctx, j.spec.ID, runnerapi.CacheLookupRequest{Key: key, RestoreKeys: lines(with["restore-keys"]), Version: version})
	if err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status < 500 {
			return nil, fmt.Errorf("cache lookup refused: %s", ae.Message)
		}
		// The cache is an optimisation: don't fail the job.
		j.printf(n, "##[warning]Cache service unavailable, continuing without the cache: %v", err)
		return outputs, nil
	}
	exact := res.Entry != nil && res.Exact
	if !exact && !lookupOnly {
		j.cacheSaves = append(j.cacheSaves, cacheSave{step: n, key: key, version: version, paths: ps})
	}
	if res.Entry == nil {
		if failMiss {
			return outputs, fmt.Errorf("no cache found for key %s", key)
		}
		j.printf(n, "Cache not found for key: %s", key)
		return outputs, nil
	}
	outputs["cache-matched-key"] = res.Entry.Key
	outputs["cache-hit"] = strconv.FormatBool(exact)
	if lookupOnly {
		j.printf(n, "Cache found for key %s (lookup only, not restored)", res.Entry.Key)
		return outputs, nil
	}
	j.printf(n, "Restoring cache %s (%s, saved from %s)", res.Entry.Key, humanBytes(res.Entry.Size), res.Entry.Ref)
	start := time.Now()
	if err := j.restoreCache(ctx, res.Entry.ID); err != nil {
		if failMiss {
			return outputs, fmt.Errorf("restoring the cache failed: %w", err)
		}
		j.printf(n, "##[warning]Restoring the cache failed, continuing without it: %v", err)
		outputs["cache-hit"], outputs["cache-matched-key"] = "false", ""
		return outputs, nil
	}
	j.printf(n, "Cache restored in %s", time.Since(start).Round(100*time.Millisecond))
	return outputs, nil
}

func (j *jobRun) restoreCache(ctx context.Context, id int64) error {
	tctx, cancel := transferCtx(ctx)
	defer cancel()
	body, err := j.r.client.DownloadCache(tctx, j.spec.ID, id)
	if err != nil {
		return err
	}
	defer body.Close()
	zr, err := gzip.NewReader(body)
	if err != nil {
		return err
	}
	return j.env.CopyInTar(ctx, "/", zr)
}

// saveCaches runs after the steps: it saves caches that missed, if the
// job succeeded so far.
func (j *jobRun) saveCaches(ctx context.Context) {
	if len(j.cacheSaves) == 0 {
		return
	}
	step := runnerapi.StepComplete
	if j.failed || ctx.Err() != nil {
		j.printf(step, "Not saving %d cache(s): the job didn't succeed", len(j.cacheSaves))
		return
	}
	for _, cs := range j.cacheSaves {
		j.printf(step, "::group::Post cache %s", cs.key)
		if err := j.saveCache(ctx, cs); err != nil {
			j.printf(step, "##[warning]Saving cache %s failed: %v", cs.key, err)
		}
		j.printf(step, "::endgroup::")
	}
}

func (j *jobRun) saveCache(ctx context.Context, cs cacheSave) error {
	step := runnerapi.StepComplete
	f, err := j.tempFile("cache-*.tgz")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	files, err := collect(ctx, j.env, cs.paths, true, func(full string, hdr *tar.Header, r io.Reader) error {
		h := *hdr
		h.Name = strings.TrimPrefix(full, "/")
		if hdr.Typeflag == tar.TypeDir {
			h.Name += "/"
		}
		if hdr.Typeflag == tar.TypeLink { // collect made Linkname absolute
			h.Linkname = strings.TrimPrefix(hdr.Linkname, "/")
		}
		if err := tw.WriteHeader(&h); err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg {
			_, err := io.Copy(tw, r)
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("collecting files: %w", err)
	}
	if files == 0 {
		j.printf(step, "No files found in the cache paths; nothing to save")
		return nil
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	size, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	tctx, cancel := transferCtx(ctx)
	defer cancel()
	_, err = j.r.client.SaveCache(tctx, j.spec.ID, cs.key, cs.version, f, size)
	var ae *APIError
	switch {
	case errors.As(err, &ae) && ae.Status == http.StatusConflict:
		j.printf(step, "Cache %s already exists (another job saved it first)", cs.key)
		return nil
	case errors.As(err, &ae):
		return errors.New(ae.Message)
	case err != nil:
		return err
	}
	j.printf(step, "Saved cache %s: %d files, %s", cs.key, files, humanBytes(size))
	return nil
}
