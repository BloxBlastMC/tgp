package cli

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

// ---- transfers ----

// transfer sends a request for a file upload or download: no overall
// timeout (ctx still applies) and API errors become *Error. The caller
// closes the response body.
func (c *apiClient) transfer(ctx context.Context, method, path string, body io.Reader, size int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.t.Base+"/api/v1"+path, body)
	if err != nil {
		return nil, usageErr("invalid request: %v", err)
	}
	if body != nil {
		req.ContentLength = size
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	tr := *c.trans
	hc := *tr.hc
	hc.Timeout = 0
	tr.hc = &hc
	return tr.Do(req)
}

// downloadTo saves a response body to file atomically, refusing to
// replace an existing file unless clobber.
func downloadTo(resp *http.Response, file string, clobber bool) (int64, error) {
	defer resp.Body.Close()
	if _, err := os.Stat(file); err == nil && !clobber {
		return 0, &Error{Code: "conflict", Exit: ExitConflict, Message: file + " already exists", Hint: "use --clobber to overwrite it"}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".tgp-download-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, resp.Body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, err
	}
	return n, os.Rename(tmp.Name(), file)
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func (a *app) confirmOrYes(yes bool, question string) error {
	if yes {
		return nil
	}
	if !a.io.CanPrompt() || a.noInput {
		return a.io.requirePrompt("confirmation", "--yes")
	}
	ok, err := a.io.Confirm(question, false)
	if err != nil || !ok {
		return &Error{Code: CodeCancelled, Exit: ExitError, Message: "cancelled"}
	}
	return nil
}

// ---- tgp release ----

func newReleaseCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "release",
		Aliases: []string{"releases"},
		Short:   "Create, view and download releases",
		Long:    "Releases are selected by their tag.",
	}
	cmd.AddCommand(newReleaseListCmd(a), newReleaseViewCmd(a), newReleaseCreateCmd(a), newReleaseEditCmd(a),
		newReleaseUploadCmd(a), newReleaseDownloadCmd(a), newReleaseDeleteCmd(a), newReleaseDeleteAssetCmd(a))
	return group(cmd)
}

func releaseKind(r apigen.Release) string {
	switch {
	case r.Draft:
		return "Draft"
	case r.Prerelease:
		return "Pre-release"
	case r.Latest:
		return "Latest"
	}
	return ""
}

func newReleaseListCmd(a *app) *cobra.Command {
	var repoFlag string
	var limit int
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List releases",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			per := min(max(limit, 1), 100)
			r, err := c.ListReleasesWithResponse(cmd.Context(), repo, &apigen.ListReleasesParams{PerPage: &per})
			if err != nil {
				return err
			}
			page, err := must(r.JSON200, "releases")
			if err != nil {
				return err
			}
			return a.print(cmd, page.Releases, func(w io.Writer) error {
				if len(page.Releases) == 0 {
					_, err := fmt.Fprintln(w, "No releases.")
					return err
				}
				rows := make([][]string, len(page.Releases))
				for i, rel := range page.Releases {
					when := rel.PublishedAt
					if when == nil {
						when = &rel.CreatedAt
					}
					title := rel.Name
					if title == "" {
						title = rel.Tag
					}
					rows[i] = []string{truncate(title, 50), releaseKind(rel), rel.Tag, strconv.Itoa(len(rel.Assets)), ago(when)}
				}
				return printTable(w, a.io, []string{"title", "type", "tag", "assets", "published"}, rows)
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "maximum releases to list (max 100)")
	return withOutput(cmd, "[]Release")
}

// getRelease loads a release by tag, or the latest one if tag is "".
func getRelease(ctx context.Context, c *apiClient, repo, tag string) (*apigen.Release, error) {
	if tag == "" {
		r, err := c.GetLatestReleaseWithResponse(ctx, repo)
		if err != nil {
			return nil, err
		}
		return must(r.JSON200, "release")
	}
	r, err := c.GetReleaseByTagWithResponse(ctx, repo, tag)
	if err != nil {
		return nil, err
	}
	return must(r.JSON200, "release")
}

func (a *app) printRelease(w io.Writer, rel *apigen.Release, base, repo string) error {
	title := rel.Name
	if title == "" {
		title = rel.Tag
	}
	kind := releaseKind(*rel)
	if kind != "" {
		kind = " [" + kind + "]"
	}
	fmt.Fprintf(w, "%s%s\n", a.io.Bold(title), kind)
	fields := [][2]string{{"tag", rel.Tag}}
	if rel.Sha != nil {
		fields = append(fields, [2]string{"commit", shortSHA(*rel.Sha)})
	} else {
		fields = append(fields, [2]string{"target", rel.Target + " (tag created on publish)"})
	}
	if rel.Author != nil {
		fields = append(fields, [2]string{"author", rel.Author.Username})
	}
	if rel.PublishedAt != nil {
		fields = append(fields, [2]string{"published", rel.PublishedAt.Local().Format(time.DateTime)})
	}
	if err := printFields(w, a.io, fields); err != nil {
		return err
	}
	if strings.TrimSpace(rel.Body) != "" {
		fmt.Fprintf(w, "\n%s\n", strings.TrimRight(rel.Body, "\n"))
	}
	fmt.Fprintln(w)
	if len(rel.Assets) > 0 {
		rows := make([][]string, len(rel.Assets))
		for i, as := range rel.Assets {
			rows[i] = []string{as.Name, humanSize(as.SizeBytes), strconv.FormatInt(as.DownloadCount, 10)}
		}
		if err := printTable(w, a.io, []string{"asset", "size", "downloads"}, rows); err != nil {
			return err
		}
	}
	if rel.Sha != nil {
		q := url.Values{"ref": {rel.Tag}}
		fmt.Fprintf(w, "Source code: %s/api/v1/repos/%s/archive?%s (add &format=zip for a zip)\n", base, url.PathEscape(repo), q.Encode())
	}
	return nil
}

func newReleaseViewCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   "view [TAG]",
		Short: "Show a release (default: the latest)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			tag := ""
			if len(args) > 0 {
				tag = args[0]
			}
			rel, err := getRelease(cmd.Context(), c, repo, tag)
			if err != nil {
				return err
			}
			return a.print(cmd, rel, func(w io.Writer) error { return a.printRelease(w, rel, c.t.Base, repo) })
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "Release")
}

// assetSpec is a file to upload: path[#label].
type assetSpec struct{ path, name, label string }

func parseAssetSpecs(args []string) ([]assetSpec, error) {
	var out []assetSpec
	for _, a := range args {
		p, label, _ := strings.Cut(a, "#")
		st, err := os.Stat(p)
		if err != nil {
			return nil, usageErr("can't read %s: %v", p, err)
		}
		if st.IsDir() {
			return nil, usageErr("%s is a directory; upload files (an archive of it, for example)", p)
		}
		out = append(out, assetSpec{path: p, name: filepath.Base(p), label: label})
	}
	return out, nil
}

func (a *app) uploadAssets(ctx context.Context, c *apiClient, repo string, rel *apigen.Release, specs []assetSpec, clobber bool) ([]apigen.ReleaseAsset, error) {
	var out []apigen.ReleaseAsset
	for _, s := range specs {
		f, err := os.Open(s.path)
		if err != nil {
			return out, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return out, err
		}
		q := url.Values{"name": {s.name}}
		if s.label != "" {
			q.Set("label", s.label)
		}
		if clobber {
			q.Set("overwrite", "true")
		}
		a.io.Progressf("Uploading %s (%s)", s.name, humanSize(st.Size()))
		resp, err := c.transfer(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/releases/%d/assets?%s", url.PathEscape(repo), rel.Id, q.Encode()), f, st.Size())
		f.Close()
		if err != nil {
			return out, err
		}
		var as apigen.ReleaseAsset
		err = decodeJSONBody(resp, &as)
		if err != nil {
			return out, err
		}
		out = append(out, as)
	}
	return out, nil
}

func newReleaseCreateCmd(a *app) *cobra.Command {
	var (
		repoFlag, title, notes, notesFile, target string
		draft, prerelease                         bool
	)
	cmd := &cobra.Command{
		Use:   "create TAG [FILE[#LABEL]...]",
		Short: "Create a release and upload files to it",
		Long: `Create a release for TAG. If the tag doesn't exist it's created at --target
(default: the default branch) when the release is published; drafts don't
create tags. Files are uploaded as release assets.`,
		Example: `  tgp release create v1.2.0 dist/game.jar --notes-file CHANGELOG.md
  tgp release create v2.0.0-rc1 --prerelease --target release/2.0
  tgp release create v1.3.0 --draft -t "Spring update"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specs, err := parseAssetSpecs(args[1:])
			if err != nil {
				return err
			}
			body, err := a.readBody(notes, notesFile)
			if err != nil {
				return err
			}
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			req := apigen.CreateRelease{Tag: args[0], Draft: &draft, Prerelease: &prerelease}
			if title != "" {
				req.Name = &title
			}
			if body != "" {
				req.Body = &body
			}
			if target != "" {
				req.Target = &target
			}
			r, err := c.CreateReleaseWithResponse(cmd.Context(), repo, req)
			if err != nil {
				return err
			}
			rel, err := must(r.JSON201, "release")
			if err != nil {
				return err
			}
			if _, err := a.uploadAssets(cmd.Context(), c, repo, rel, specs, false); err != nil {
				return err
			}
			if len(specs) > 0 {
				if rel, err = getRelease(cmd.Context(), c, repo, rel.Tag); err != nil {
					return err
				}
			}
			return a.print(cmd, rel, func(w io.Writer) error {
				what := "Published"
				if rel.Draft {
					what = "Created draft"
				}
				_, err := fmt.Fprintf(w, "%s %s release %s with %d file(s)\n", a.io.SuccessIcon(), what, rel.Tag, len(rel.Assets))
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&title, "title", "t", "", "release title (default: the tag)")
	f.StringVarP(&notes, "notes", "n", "", "release notes (Markdown)")
	f.StringVarP(&notesFile, "notes-file", "F", "", "read the notes from a `file` (- for stdin)")
	f.StringVar(&target, "target", "", "branch or commit for a new tag (default: the default branch)")
	f.BoolVarP(&draft, "draft", "d", false, "save as a draft (not visible to testers, no tag yet)")
	f.BoolVarP(&prerelease, "prerelease", "p", false, "mark as a pre-release")
	return withOutput(cmd, "Release")
}

func newReleaseEditCmd(a *app) *cobra.Command {
	var (
		repoFlag, title, notes, notesFile, target, tag string
		draft, prerelease                              bool
	)
	cmd := &cobra.Command{
		Use:   "edit TAG",
		Short: "Edit or publish a release",
		Example: `  tgp release edit v1.3.0 --draft=false     # publish a draft
  tgp release edit v1.2.0 --notes-file NOTES.md`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			rel, err := getRelease(cmd.Context(), c, repo, args[0])
			if err != nil {
				return err
			}
			f := cmd.Flags()
			var req apigen.UpdateRelease
			if f.Changed("title") {
				req.Name = &title
			}
			if f.Changed("notes") || f.Changed("notes-file") {
				body, err := a.readBody(notes, notesFile)
				if err != nil {
					return err
				}
				req.Body = &body
			}
			if f.Changed("target") {
				req.Target = &target
			}
			if f.Changed("tag") {
				req.Tag = &tag
			}
			if f.Changed("draft") {
				req.Draft = &draft
			}
			if f.Changed("prerelease") {
				req.Prerelease = &prerelease
			}
			r, err := c.UpdateReleaseWithResponse(cmd.Context(), repo, rel.Id, req)
			if err != nil {
				return err
			}
			out, err := must(r.JSON200, "release")
			if err != nil {
				return err
			}
			return a.print(cmd, out, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Updated release %s\n", a.io.SuccessIcon(), out.Tag)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&title, "title", "t", "", "new title")
	f.StringVarP(&notes, "notes", "n", "", "new notes")
	f.StringVarP(&notesFile, "notes-file", "F", "", "read new notes from a `file` (- for stdin)")
	f.StringVar(&target, "target", "", "branch or commit for the tag (if it doesn't exist yet)")
	f.StringVar(&tag, "tag", "", "new tag name")
	f.BoolVar(&draft, "draft", false, "make it a draft (--draft=false publishes it)")
	f.BoolVar(&prerelease, "prerelease", false, "mark as a pre-release (--prerelease=false to unmark)")
	return withOutput(cmd, "Release")
}

func newReleaseUploadCmd(a *app) *cobra.Command {
	var repoFlag string
	var clobber bool
	cmd := &cobra.Command{
		Use:     "upload TAG FILE[#LABEL]...",
		Short:   "Upload files to a release",
		Example: `  tgp release upload v1.2.0 build/libs/*.jar --clobber`,
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			specs, err := parseAssetSpecs(args[1:])
			if err != nil {
				return err
			}
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			rel, err := getRelease(cmd.Context(), c, repo, args[0])
			if err != nil {
				return err
			}
			assets, err := a.uploadAssets(cmd.Context(), c, repo, rel, specs, clobber)
			if err != nil {
				return err
			}
			return a.print(cmd, assets, func(w io.Writer) error {
				for _, as := range assets {
					fmt.Fprintf(w, "%s Uploaded %s (%s)\n", a.io.SuccessIcon(), as.Name, humanSize(as.SizeBytes))
				}
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVar(&clobber, "clobber", false, "replace files with the same name")
	return withOutput(cmd, "[]ReleaseAsset")
}

func newReleaseDownloadCmd(a *app) *cobra.Command {
	var (
		repoFlag, dir, archive string
		patterns               []string
		clobber                bool
	)
	cmd := &cobra.Command{
		Use:   "download [TAG]",
		Short: "Download a release's files (default: the latest release)",
		Example: `  tgp release download v1.2.0 -p '*.jar' -D build
  tgp release download --archive zip`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if archive != "" && archive != "zip" && archive != "tar.gz" {
				return usageErr("--archive must be zip or tar.gz")
			}
			for _, p := range patterns {
				if _, err := path.Match(p, ""); err != nil {
					return usageErr("invalid pattern %q", p)
				}
			}
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			tag := ""
			if len(args) > 0 {
				tag = args[0]
			}
			rel, err := getRelease(cmd.Context(), c, repo, tag)
			if err != nil {
				return err
			}
			var got []map[string]any
			save := func(urlPath, name string) error {
				resp, err := c.transfer(cmd.Context(), http.MethodGet, urlPath, nil, 0)
				if err != nil {
					return err
				}
				file := filepath.Join(dir, name)
				a.io.Progressf("Downloading %s", name)
				n, err := downloadTo(resp, file, clobber)
				if err != nil {
					return err
				}
				got = append(got, map[string]any{"name": name, "path": file, "size_bytes": n})
				return nil
			}
			if archive != "" {
				label := strings.ReplaceAll(rel.Tag, "/", "-")
				q := url.Values{"ref": {rel.Tag}, "format": {archive}}
				if err := save(fmt.Sprintf("/repos/%s/archive?%s", url.PathEscape(repo), q.Encode()), repo+"-"+label+"."+archive); err != nil {
					return err
				}
			} else {
				for _, as := range rel.Assets {
					ok := len(patterns) == 0
					for _, p := range patterns {
						if m, _ := path.Match(p, as.Name); m {
							ok = true
						}
					}
					if !ok {
						continue
					}
					if err := save(fmt.Sprintf("/repos/%s/releases/%d/assets/%d", url.PathEscape(repo), rel.Id, as.Id), as.Name); err != nil {
						return err
					}
				}
				if len(got) == 0 {
					return &Error{Code: "not_found", Exit: ExitNotFound, Message: fmt.Sprintf("release %s has no matching files", rel.Tag),
						Hint: "tgp release view " + rel.Tag + " lists them; --archive zip downloads the source code"}
				}
			}
			if got == nil {
				got = []map[string]any{}
			}
			return a.print(cmd, got, func(w io.Writer) error {
				for _, g := range got {
					fmt.Fprintf(w, "%s %s (%s)\n", a.io.SuccessIcon(), g["path"], humanSize(g["size_bytes"].(int64)))
				}
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&dir, "dir", "D", ".", "directory to save files in")
	f.StringArrayVarP(&patterns, "pattern", "p", nil, "only files matching this glob (repeatable)")
	f.StringVarP(&archive, "archive", "A", "", "download the source code instead: zip or tar.gz")
	f.BoolVar(&clobber, "clobber", false, "overwrite existing files")
	return withOutput(cmd, "[]DownloadedFile")
}

func newReleaseDeleteCmd(a *app) *cobra.Command {
	var repoFlag string
	var yes, cleanupTag bool
	cmd := &cobra.Command{
		Use:   "delete TAG",
		Short: "Delete a release and its files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			rel, err := getRelease(cmd.Context(), c, repo, args[0])
			if err != nil {
				return err
			}
			q := fmt.Sprintf("Delete release %s and its %d file(s)?", rel.Tag, len(rel.Assets))
			if cleanupTag {
				q = fmt.Sprintf("Delete release %s, its %d file(s) and the tag?", rel.Tag, len(rel.Assets))
			}
			if err := a.confirmOrYes(yes, q); err != nil {
				return err
			}
			if _, err := c.DeleteReleaseWithResponse(cmd.Context(), repo, rel.Id, &apigen.DeleteReleaseParams{DeleteTag: &cleanupTag}); err != nil {
				return err
			}
			return a.print(cmd, map[string]any{"name": rel.Tag, "deleted": true}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Deleted release %s\n", a.io.SuccessIcon(), rel.Tag)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	cmd.Flags().BoolVar(&cleanupTag, "cleanup-tag", false, "also delete the git tag")
	return withOutput(cmd, "Deleted")
}

func newReleaseDeleteAssetCmd(a *app) *cobra.Command {
	var repoFlag string
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete-asset TAG NAME",
		Short: "Delete a file from a release",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			rel, err := getRelease(cmd.Context(), c, repo, args[0])
			if err != nil {
				return err
			}
			var id int64
			for _, as := range rel.Assets {
				if as.Name == args[1] {
					id = as.Id
				}
			}
			if id == 0 {
				return &Error{Code: "not_found", Exit: ExitNotFound, Message: fmt.Sprintf("release %s has no file named %s", rel.Tag, args[1])}
			}
			if err := a.confirmOrYes(yes, fmt.Sprintf("Delete %s from release %s?", args[1], rel.Tag)); err != nil {
				return err
			}
			if _, err := c.DeleteReleaseAssetWithResponse(cmd.Context(), repo, rel.Id, id); err != nil {
				return err
			}
			return a.print(cmd, map[string]any{"name": args[1], "deleted": true}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Deleted %s from %s\n", a.io.SuccessIcon(), args[1], rel.Tag)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	return withOutput(cmd, "Deleted")
}

// ---- tgp artifact ----

func newArtifactCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "artifact",
		Aliases: []string{"artifacts"},
		Short:   "List and download CI artifacts",
	}
	cmd.AddCommand(newArtifactListCmd(a), newArtifactDownloadCmd(a), newArtifactDeleteCmd(a))
	return group(cmd)
}

func newArtifactListCmd(a *app) *cobra.Command {
	var repoFlag, name string
	var run, limit int
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List artifacts (of one run with --run)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			var arts []apigen.Artifact
			if run > 0 {
				r, err := c.ListRunArtifactsWithResponse(cmd.Context(), repo, int64(run))
				if err != nil {
					return err
				}
				l, err := must(r.JSON200, "artifacts")
				if err != nil {
					return err
				}
				for _, x := range *l {
					if name == "" || x.Name == name {
						arts = append(arts, x)
					}
				}
			} else {
				per := min(max(limit, 1), 100)
				p := &apigen.ListArtifactsParams{PerPage: &per}
				if name != "" {
					p.Name = &name
				}
				r, err := c.ListArtifactsWithResponse(cmd.Context(), repo, p)
				if err != nil {
					return err
				}
				page, err := must(r.JSON200, "artifacts")
				if err != nil {
					return err
				}
				arts = page.Artifacts
			}
			if arts == nil {
				arts = []apigen.Artifact{}
			}
			return a.print(cmd, arts, func(w io.Writer) error {
				if len(arts) == 0 {
					_, err := fmt.Fprintln(w, "No artifacts.")
					return err
				}
				rows := make([][]string, len(arts))
				for i, x := range arts {
					rows[i] = []string{strconv.FormatInt(x.Id, 10), x.Name, "#" + strconv.FormatInt(x.RunNumber, 10), x.JobName,
						humanSize(x.SizeBytes), "in " + strings.TrimSuffix(ago(&x.ExpiresAt), " ago")}
				}
				return printTable(w, a.io, []string{"id", "name", "run", "job", "size", "expires"}, rows)
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.IntVar(&run, "run", 0, "only this run's artifacts")
	f.StringVarP(&name, "name", "n", "", "only artifacts with this name")
	f.IntVarP(&limit, "limit", "L", 30, "maximum artifacts to list (max 100)")
	return withOutput(cmd, "[]Artifact")
}

func newArtifactDownloadCmd(a *app) *cobra.Command {
	var repoFlag, dir string
	var names []string
	cmd := &cobra.Command{
		Use:   "download [RUN]",
		Short: "Download and unpack a run's artifacts",
		Long: `Download artifacts of a run (default: the latest run of the current branch)
and unpack them. With one --name, files go straight into --dir; otherwise
each artifact goes into its own directory named after it.`,
		Example: `  tgp artifact download 42 -n dist -D build
  tgp artifact download          # every artifact of the latest run`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, n, err := a.resolveRun(cmd.Context(), repoFlag, args)
			if err != nil {
				return err
			}
			r, err := c.ListRunArtifactsWithResponse(cmd.Context(), repo, n)
			if err != nil {
				return err
			}
			all, err := must(r.JSON200, "artifacts")
			if err != nil {
				return err
			}
			var pick []apigen.Artifact
			for _, x := range *all {
				ok := len(names) == 0
				for _, want := range names {
					ok = ok || x.Name == want
				}
				if ok {
					pick = append(pick, x)
				}
			}
			if len(pick) == 0 {
				return &Error{Code: "not_found", Exit: ExitNotFound, Message: fmt.Sprintf("run #%d has no matching artifacts", n),
					Hint: fmt.Sprintf("tgp artifact list --run %d shows them", n)}
			}
			var out []map[string]any
			for _, x := range pick {
				to := dir
				if len(names) != 1 {
					to = filepath.Join(dir, x.Name)
				}
				a.io.Progressf("Downloading %s (%s)", x.Name, humanSize(x.SizeBytes))
				files, err := a.downloadArtifact(cmd.Context(), c, repo, x, to)
				if err != nil {
					return err
				}
				out = append(out, map[string]any{"name": x.Name, "directory": to, "files": files})
			}
			return a.print(cmd, out, func(w io.Writer) error {
				for _, o := range out {
					fmt.Fprintf(w, "%s %s → %s (%d files)\n", a.io.SuccessIcon(), o["name"], o["directory"], o["files"])
				}
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().StringArrayVarP(&names, "name", "n", nil, "only this artifact (repeatable)")
	cmd.Flags().StringVarP(&dir, "dir", "D", ".", "directory to unpack into")
	return withOutput(cmd, "[]DownloadedArtifact")
}

// downloadArtifact fetches an artifact's zip and unpacks it into dir.
func (a *app) downloadArtifact(ctx context.Context, c *apiClient, repo string, x apigen.Artifact, dir string) (int, error) {
	resp, err := c.transfer(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/artifacts/%d/zip", url.PathEscape(repo), x.Id), nil, 0)
	if err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp("", "tgp-artifact-*.zip")
	if err != nil {
		resp.Body.Close()
		return 0, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	size, err := io.Copy(tmp, resp.Body)
	resp.Body.Close()
	if err != nil {
		return 0, err
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		return 0, fmt.Errorf("artifact %s isn't a valid zip: %w", x.Name, err)
	}
	return unzip(zr, dir)
}

// unzip extracts regular files and directories; entries can't escape dir.
func unzip(zr *zip.Reader, dir string) (int, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, zf := range zr.File {
		name := path.Clean("/" + strings.ReplaceAll(zf.Name, "\\", "/"))
		if name == "/" {
			continue
		}
		dst := filepath.Join(root, filepath.FromSlash(name))
		if !strings.HasPrefix(dst, root+string(filepath.Separator)) {
			return n, fmt.Errorf("zip entry %q escapes the destination", zf.Name)
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return n, err
			}
			continue
		}
		if !zf.Mode().IsRegular() {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return n, err
		}
		mode := zf.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		rc, err := zf.Open()
		if err != nil {
			return n, err
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return n, err
		}
		_, err = io.Copy(f, rc)
		rc.Close()
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func newArtifactDeleteCmd(a *app) *cobra.Command {
	var repoFlag string
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete ID",
		Short: "Delete an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return usageErr("invalid artifact id %q (tgp artifact list shows ids)", args[0])
			}
			if err := a.confirmOrYes(yes, fmt.Sprintf("Delete artifact %d?", id)); err != nil {
				return err
			}
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			if _, err := c.DeleteArtifactWithResponse(cmd.Context(), repo, id); err != nil {
				return err
			}
			return a.print(cmd, map[string]any{"name": args[0], "deleted": true}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Deleted artifact %d\n", a.io.SuccessIcon(), id)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	return withOutput(cmd, "Deleted")
}

// ---- tgp cache ----

func newCacheCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cache",
		Aliases: []string{"caches"},
		Short:   "List and delete CI caches",
	}
	cmd.AddCommand(newCacheListCmd(a), newCacheDeleteCmd(a))
	return group(cmd)
}

func newCacheListCmd(a *app) *cobra.Command {
	var repoFlag, key, ref, sort string
	var limit int
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List caches with the repository's usage",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if sort != "last_used" && sort != "created" && sort != "size" {
				return usageErr("--sort must be last_used, created or size")
			}
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			per := min(max(limit, 1), 100)
			st := apigen.ListCachesParamsSort(sort)
			p := &apigen.ListCachesParams{PerPage: &per, Sort: &st}
			if key != "" {
				p.Key = &key
			}
			if ref != "" {
				p.Ref = &ref
			}
			r, err := c.ListCachesWithResponse(cmd.Context(), repo, p)
			if err != nil {
				return err
			}
			page, err := must(r.JSON200, "caches")
			if err != nil {
				return err
			}
			return a.print(cmd, page, func(w io.Writer) error {
				quota := "no quota"
				if page.QuotaBytes > 0 {
					quota = "of " + humanSize(page.QuotaBytes)
				}
				fmt.Fprintf(w, "%d caches, %s used %s\n", page.Total, humanSize(page.TotalBytes), quota)
				if len(page.Caches) == 0 {
					return nil
				}
				rows := make([][]string, len(page.Caches))
				for i, e := range page.Caches {
					rows[i] = []string{strconv.FormatInt(e.Id, 10), truncate(e.Key, 60), strings.TrimPrefix(e.Ref, "refs/heads/"),
						humanSize(e.SizeBytes), ago(&e.LastUsedAt)}
				}
				return printTable(w, a.io, []string{"id", "key", "ref", "size", "last used"}, rows)
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&key, "key", "k", "", "only keys starting with this")
	f.StringVar(&ref, "ref", "", "only caches saved from this ref (refs/heads/main)")
	f.StringVarP(&sort, "sort", "S", "last_used", "last_used, created or size")
	f.IntVarP(&limit, "limit", "L", 30, "maximum caches to list (max 100)")
	return withOutput(cmd, "CachePage")
}

func newCacheDeleteCmd(a *app) *cobra.Command {
	var repoFlag, key, ref string
	var all, yes bool
	cmd := &cobra.Command{
		Use:   "delete [ID]",
		Short: "Delete caches by id, by key, or all of them",
		Example: `  tgp cache delete 17
  tgp cache delete --key deps-linux-abc123
  tgp cache delete --all --yes`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n := 0
			if len(args) > 0 {
				n++
			}
			if key != "" {
				n++
			}
			if all {
				n++
			}
			if n != 1 {
				return usageErr("give exactly one of ID, --key or --all")
			}
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			deleted := 0
			if len(args) > 0 {
				id, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					return usageErr("invalid cache id %q (tgp cache list shows ids)", args[0])
				}
				if _, err := c.DeleteCacheWithResponse(cmd.Context(), repo, id); err != nil {
					return err
				}
				deleted = 1
			} else {
				if all {
					if err := a.confirmOrYes(yes, "Delete every cache of "+repo+"?"); err != nil {
						return err
					}
				}
				p := &apigen.DeleteCachesParams{}
				if key != "" {
					p.Key = &key
				}
				if ref != "" {
					p.Ref = &ref
				}
				r, err := c.DeleteCachesWithResponse(cmd.Context(), repo, p)
				if err != nil {
					return err
				}
				d, err := must(r.JSON200, "result")
				if err != nil {
					return err
				}
				deleted = d.Deleted
			}
			return a.print(cmd, apigen.DeletedCount{Deleted: deleted}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Deleted %d cache(s)\n", a.io.SuccessIcon(), deleted)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&key, "key", "k", "", "delete caches with exactly this key")
	f.StringVar(&ref, "ref", "", "with --key or --all: only caches of this ref")
	f.BoolVar(&all, "all", false, "delete every cache")
	f.BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	return withOutput(cmd, "DeletedCount")
}

// decodeJSONBody decodes a successful response's JSON body and closes it.
func decodeJSONBody(resp *http.Response, v any) error {
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(v); err != nil {
		return &Error{Code: CodeBadResponse, Exit: ExitError, Message: fmt.Sprintf("invalid response from the server: %v", err)}
	}
	return nil
}
