package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

// ---- tgp package ----

func newPackageCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "package",
		Aliases: []string{"packages", "image", "images"},
		Short:   "List and delete a repository's container images",
		Long: `Container images live in tgp's registry at
<host>/<namespace>/<repo>[/<name>]:<tag>. Push and pull them with docker,
logging in with a personal access token:

  docker login git.example.com -u <you> --password-stdin

Images are named by what follows <namespace>/<repo>/ ("issues" for
<host>/bloxblast/bloxblast/issues). The repository's own image is "-" in
versions and has no name in delete (":latest").`,
	}
	cmd.AddCommand(newPackageListCmd(a), newPackageVersionsCmd(a), newPackageDeleteCmd(a))
	return group(cmd)
}

func newPackageListCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the repository's images and its registry usage",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			r, err := c.ListPackagesWithResponse(cmd.Context(), repo)
			if err != nil {
				return err
			}
			l, err := must(r.JSON200, "packages")
			if err != nil {
				return err
			}
			return a.print(cmd, l, func(w io.Writer) error {
				host := l.Registry
				if host == "" {
					host = c.t.Key
				}
				quota := "no quota"
				if l.QuotaBytes > 0 {
					quota = "of " + humanSize(l.QuotaBytes)
				}
				fmt.Fprintf(w, "Registry usage: %s %s\n", humanSize(l.UsageBytes), quota)
				if len(l.Images) == 0 {
					_, err := fmt.Fprintf(w, "No images. Push one to %s/%s/<name>:<tag>\n", host, strings.ToLower(l.Namespace+"/"+repo))
					return err
				}
				rows := make([][]string, len(l.Images))
				for i, p := range l.Images {
					rows[i] = []string{host + "/" + p.Path, deref(p.LatestTag), strconv.Itoa(p.Tags), strconv.Itoa(p.Versions), humanSize(p.SizeBytes), ago(&p.UpdatedAt)}
				}
				return printTable(w, a.io, []string{"image", "latest tag", "tags", "versions", "size", "updated"}, rows)
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "PackageList")
}

// packageName maps the CLI's image name ("-" for the repository's own
// image) to the API's.
func packageName(s string) *string {
	if s == "-" {
		s = ""
	}
	return &s
}

func newPackageVersionsCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   "versions NAME",
		Short: "List an image's versions and their tags, newest first",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			r, err := c.ListPackageVersionsWithResponse(cmd.Context(), repo, &apigen.ListPackageVersionsParams{Name: packageName(args[0])})
			if err != nil {
				return err
			}
			l, err := must(r.JSON200, "versions")
			if err != nil {
				return err
			}
			return a.print(cmd, l.Versions, func(w io.Writer) error {
				if len(l.Versions) == 0 {
					_, err := fmt.Fprintln(w, "No versions.")
					return err
				}
				rows := make([][]string, len(l.Versions))
				for i, v := range l.Versions {
					by := ""
					if v.PushedBy != nil {
						by = v.PushedBy.Username
					}
					if v.PushedByJob != nil {
						by += fmt.Sprintf(" (CI job %d)", *v.PushedByJob)
					}
					kind := string(v.Kind)
					if len(v.Platforms) > 0 {
						kind += " " + strings.Join(v.Platforms, ",")
					}
					rows[i] = []string{v.Digest[:min(len(v.Digest), 19)], strings.Join(v.Tags, ", "), kind, humanSize(v.SizeBytes), strings.TrimSpace(by), ago(&v.CreatedAt)}
				}
				return printTable(w, a.io, []string{"digest", "tags", "kind", "size", "pushed by", "pushed"}, rows)
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "[]PackageVersion")
}

func newPackageDeleteCmd(a *app) *cobra.Command {
	var repoFlag string
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete NAME:TAG | NAME@DIGEST",
		Short: "Delete a tag, or a version and all its tags (write access)",
		Long: `Deleting a tag leaves the version; untagged versions are removed after
the registry's retention period. Deleting a version (by digest) removes
it and every tag pointing at it. Storage is freed by garbage collection.`,
		Example: `  tgp package delete issues:g1a2b3c4d5e6f
  tgp package delete issues@sha256:4f3c…
  tgp package delete :latest --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[0]
			var name, tag, digest string
			if i := strings.Index(ref, "@"); i >= 0 {
				name, digest = ref[:i], ref[i+1:]
			} else if i := strings.LastIndex(ref, ":"); i >= 0 {
				name, tag = ref[:i], ref[i+1:]
			} else {
				return usageErr("say what to delete: NAME:TAG or NAME@DIGEST (leave out NAME for the repository's own image)")
			}
			if name == "" {
				name = "-"
			}
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			what := fmt.Sprintf("tag %s of %s", tag, name)
			if digest != "" {
				what = fmt.Sprintf("version %s of %s and its tags", digest, name)
			}
			if err := a.confirmOrYes(yes, "Delete "+what+"?"); err != nil {
				return err
			}
			if digest != "" {
				if _, err := c.DeletePackageVersionWithResponse(cmd.Context(), repo, &apigen.DeletePackageVersionParams{Name: packageName(name), Digest: digest}); err != nil {
					return err
				}
			} else if _, err := c.DeletePackageTagWithResponse(cmd.Context(), repo, &apigen.DeletePackageTagParams{Name: packageName(name), Tag: tag}); err != nil {
				return err
			}
			return a.print(cmd, map[string]any{"name": ref, "deleted": true}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Deleted %s\n", a.io.SuccessIcon(), what)
				return err
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	return withOutput(cmd, "Deleted")
}

// ---- tgp admin registry ----

var registrySettingKeys = map[string]string{
	"quota_mb": "int", "untagged_retention_days": "int", "keep_last_tags": "int", "keep_last_tags_patterns": "list",
}

func newAdminRegistryCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "View and change container registry quota and retention",
	}
	group(cmd)
	view := &cobra.Command{
		Use:   "view",
		Short: "Show registry settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.GetRegistrySettingsWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			s, err := must(r.JSON200, "settings")
			if err != nil {
				return err
			}
			return a.print(cmd, s, func(w io.Writer) error { return printRegistrySettings(a, w, s) })
		},
	}
	set := &cobra.Command{
		Use:   "set KEY=VALUE...",
		Short: "Change registry settings",
		Long: "Keys: " + strings.Join(sortedKeys(registrySettingKeys), ", ") + `.
quota_mb is per repository (0 = no quota). keep_last_tags keeps the newest N
tags matching keep_last_tags_patterns (comma-separated globs) per image.`,
		Example: `  tgp admin registry set quota_mb=20480 untagged_retention_days=14
  tgp admin registry set keep_last_tags=20 keep_last_tags_patterns=g*,sha-*`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			for _, kv := range args {
				k, v, ok := strings.Cut(kv, "=")
				typ, known := registrySettingKeys[k]
				if !ok || !known {
					return usageErr("unknown setting %q; keys: %s", kv, strings.Join(sortedKeys(registrySettingKeys), ", "))
				}
				if typ == "list" {
					var l []string
					for _, p := range strings.Split(v, ",") {
						if p = strings.TrimSpace(p); p != "" {
							l = append(l, p)
						}
					}
					if l == nil {
						l = []string{}
					}
					body[k] = l
					continue
				}
				n, err := strconv.Atoi(v)
				if err != nil {
					return usageErr("%s must be a whole number", k)
				}
				body[k] = n
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			var patch apigen.RegistrySettingsPatch
			if err := remarshal(body, &patch); err != nil {
				return err
			}
			r, err := c.UpdateRegistrySettingsWithResponse(cmd.Context(), patch)
			if err != nil {
				return err
			}
			s, err := must(r.JSON200, "settings")
			if err != nil {
				return err
			}
			return a.print(cmd, s, func(w io.Writer) error { return printRegistrySettings(a, w, s) })
		},
	}
	cmd.AddCommand(withOutput(view, "RegistrySettings"), withOutput(set, "RegistrySettings"))
	return cmd
}

func printRegistrySettings(a *app, w io.Writer, s *apigen.RegistrySettings) error {
	quota := "no quota"
	if s.QuotaMb > 0 {
		quota = strconv.Itoa(s.QuotaMb)
	}
	keep := "keep all"
	if s.KeepLastTags > 0 {
		keep = strconv.Itoa(s.KeepLastTags)
	}
	return printFields(w, a.io, [][2]string{
		{"quota_mb (per repo)", quota},
		{"untagged_retention_days", strconv.Itoa(s.UntaggedRetentionDays)},
		{"keep_last_tags", keep},
		{"keep_last_tags_patterns", strings.Join(s.KeepLastTagsPatterns, ", ")},
	})
}
