package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

func newRepoCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "repo",
		Aliases: []string{"repos"},
		Short:   "Create, list, view, clone, edit and delete repositories",
	}
	cmd.AddCommand(newRepoListCmd(a), newRepoViewCmd(a), newRepoCreateCmd(a), newRepoCloneCmd(a),
		newRepoEditCmd(a), newRepoDeleteCmd(a))
	return group(cmd)
}

func newRepoListCmd(a *app) *cobra.Command {
	var (
		search          string
		limit           int
		archived, noArc bool
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List repositories you can access, most recently pushed first",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			p := &apigen.ListReposParams{}
			if search != "" {
				p.Q = &search
			}
			r, err := c.ListReposWithResponse(cmd.Context(), p)
			if err != nil {
				return err
			}
			all, err := must(r.JSON200, "repo list")
			if err != nil {
				return err
			}
			repos := make([]apigen.Repo, 0, len(*all))
			for _, x := range *all {
				if (archived && !x.Archived) || (noArc && x.Archived) {
					continue
				}
				repos = append(repos, x)
				if limit > 0 && len(repos) == limit {
					break
				}
			}
			return a.print(cmd, repos, func(w io.Writer) error {
				if len(repos) == 0 {
					fmt.Fprintln(a.io.ErrOut, "No repositories.")
					return nil
				}
				rows := make([][]string, len(repos))
				for i, x := range repos {
					name := x.Name
					if x.Archived {
						name += a.io.Dim(" (archived)")
					}
					rows[i] = []string{name, string(x.Permission), ago(x.PushedAt), truncate(x.Description, 60)}
				}
				return printTable(w, a.io, []string{"name", "access", "pushed", "description"}, rows)
			})
		},
	}
	f := cmd.Flags()
	f.StringVarP(&search, "search", "q", "", "only names containing `text` (case-insensitive)")
	f.IntVarP(&limit, "limit", "L", 0, "show at most `n` repositories (0 = all)")
	f.BoolVar(&archived, "archived", false, "only archived repositories")
	f.BoolVar(&noArc, "no-archived", false, "hide archived repositories")
	cmd.MarkFlagsMutuallyExclusive("archived", "no-archived")
	return withOutput(cmd, "[]Repo")
}

func getRepo(cmd *cobra.Command, c *apiClient, name string) (*apigen.Repo, error) {
	r, err := c.GetRepoWithResponse(cmd.Context(), name)
	if err != nil {
		return nil, err
	}
	return must(r.JSON200, "repo")
}

func repoFields(a *app, x *apigen.Repo) [][2]string {
	state := "active"
	switch {
	case x.Archived:
		state = a.io.Yellow("archived (read-only)")
	case x.Empty:
		state = "empty (nothing pushed yet)"
	}
	desc := x.Description
	if desc == "" {
		desc = a.io.Dim("no description")
	}
	return [][2]string{
		{"name", a.io.Bold(x.Name)},
		{"description", desc},
		{"default branch", x.DefaultBranch},
		{"clone", x.CloneUrl},
		{"your access", string(x.Permission)},
		{"state", state},
		{"pushed", ago(x.PushedAt)},
		{"created", ago(&x.CreatedAt)},
	}
}

func newRepoViewCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   "view [NAME]",
		Short: "Show a repository",
		Long:  "Show a repository. Without NAME, uses -R or the current directory's git remote.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, name, err := a.resolveRepo(firstOr(args, repoFlag))
			if err != nil {
				return err
			}
			x, err := getRepo(cmd, c, name)
			if err != nil {
				return err
			}
			return a.print(cmd, x, func(w io.Writer) error { return printFields(w, a.io, repoFields(a, x)) })
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "Repo")
}

func firstOr(args []string, def string) string {
	if len(args) > 0 {
		return args[0]
	}
	return def
}

func newRepoCreateCmd(a *app) *cobra.Command {
	var (
		desc, branch string
		initRepo     bool
		clone        bool
	)
	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a repository (admins)",
		Example: `  tgp repo create game-server -d "Paper plugins" --init
  tgp repo create website --clone`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			req := apigen.CreateRepoRequest{Name: args[0]}
			if desc != "" {
				req.Description = &desc
			}
			if branch != "" {
				req.DefaultBranch = &branch
			}
			if initRepo {
				req.AutoInit = &initRepo
			}
			r, err := c.CreateRepoWithResponse(cmd.Context(), req)
			if err != nil {
				return err
			}
			x, err := must(r.JSON201, "repo create")
			if err != nil {
				return err
			}
			if clone {
				// Human progress goes to stderr so --json output stays clean.
				if err := gitClone(a, c, x, "", nil); err != nil {
					return err
				}
			}
			return a.print(cmd, x, func(w io.Writer) error {
				fmt.Fprintf(w, "%s Created repository %s\n", a.io.SuccessIcon(), a.io.Bold(x.Name))
				fmt.Fprintf(w, "  %s\n", x.CloneUrl)
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.StringVarP(&desc, "description", "d", "", "description")
	f.StringVar(&branch, "default-branch", "", "default branch name (server default: main)")
	f.BoolVar(&initRepo, "init", false, "create an initial commit with a README")
	f.BoolVar(&clone, "clone", false, "clone the new repository into ./NAME")
	return withOutput(cmd, "Repo")
}

// gitClone runs `git clone`, storing tgp as the credential helper for the
// server in the new repository's config so later fetches and pushes work.
func gitClone(a *app, c *apiClient, x *apigen.Repo, dir string, extra []string) error {
	args := []string{"clone"}
	if c.t.Token != "" && os.Getenv("TGP_TOKEN") == "" {
		helper, err := helperCommand()
		if err != nil {
			return err
		}
		key := "credential." + c.t.Base + ".helper"
		args = append(args, "-c", key+"=", "-c", key+"="+helper)
	}
	args = append(args, extra...)
	args = append(args, "--", x.CloneUrl)
	if dir != "" {
		args = append(args, dir)
	}
	g := exec.Command("git", args...)
	g.Stdin = a.io.In
	g.Stdout = a.io.ErrOut
	g.Stderr = a.io.ErrOut
	if c.t.Token != "" && os.Getenv("TGP_TOKEN") != "" {
		// An env token isn't stored anywhere git's helper could read it
		// later, so pass it for this command only.
		g.Env = append(os.Environ(),
			"GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=credential."+c.t.Base+".helper", "GIT_CONFIG_VALUE_0=",
			"GIT_CONFIG_KEY_1=credential."+c.t.Base+".helper",
			"GIT_CONFIG_VALUE_1=!f() { echo username=tgp; echo \"password=$TGP_TOKEN\"; }; f",
		)
	}
	if err := g.Run(); err != nil {
		return errorf(CodeGit, ExitError, "git clone failed: %v", err)
	}
	return nil
}

func newRepoCloneCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clone NAME [DIRECTORY] [-- <git clone flags>...]",
		Short: "Clone a repository",
		Long: `Clone a repository over HTTPS. tgp is set as the credential helper in the
clone's git config, so later fetches and pushes use your tgp login. Flags after
-- are passed to git clone.`,
		Example: `  tgp repo clone game-server
  tgp repo clone game-server ~/src/gs -- --depth 1`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var extra []string
			if n := cmd.ArgsLenAtDash(); n >= 0 {
				extra = args[n:]
				args = args[:n]
			}
			if len(args) < 1 || len(args) > 2 {
				return usageErr("usage: tgp repo clone NAME [DIRECTORY] [-- <git clone flags>...]")
			}
			c, name, err := a.resolveRepo(args[0])
			if err != nil {
				return err
			}
			x, err := getRepo(cmd, c, name)
			if err != nil {
				return err
			}
			dir := ""
			if len(args) > 1 {
				dir = args[1]
			}
			if err := gitClone(a, c, x, dir, extra); err != nil {
				return err
			}
			if dir == "" {
				dir = x.Name
			}
			return a.print(cmd, map[string]string{"repo": x.Name, "directory": dir, "clone_url": x.CloneUrl},
				func(io.Writer) error { return nil })
		},
	}
	return withOutput(cmd, "Clone")
}

func newRepoEditCmd(a *app) *cobra.Command {
	var (
		repoFlag, desc, branch, rename string
		archive, unarchive             bool
	)
	cmd := &cobra.Command{
		Use:   "edit [NAME]",
		Short: "Change a repository's settings",
		Long: `Change a repository's description or default branch (maintainers), or
rename or archive it (admins). Archived repositories are read-only.`,
		Example: `  tgp repo edit -d "New description"
  tgp repo edit game-server --default-branch develop
  tgp repo edit old-site --archive`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := apigen.UpdateRepoRequest{}
			f := cmd.Flags()
			if f.Changed("description") {
				req.Description = &desc
			}
			if branch != "" {
				req.DefaultBranch = &branch
			}
			if rename != "" {
				req.Name = &rename
			}
			if archive || unarchive {
				req.Archived = &archive
			}
			if req == (apigen.UpdateRepoRequest{}) {
				return usageErr("nothing to change: pass --description, --default-branch, --rename, --archive or --unarchive")
			}
			c, name, err := a.resolveRepo(firstOr(args, repoFlag))
			if err != nil {
				return err
			}
			r, err := c.UpdateRepoWithResponse(cmd.Context(), name, req)
			if err != nil {
				return err
			}
			x, err := must(r.JSON200, "repo edit")
			if err != nil {
				return err
			}
			return a.print(cmd, x, func(w io.Writer) error {
				fmt.Fprintf(w, "%s Updated %s\n", a.io.SuccessIcon(), a.io.Bold(x.Name))
				if rename != "" {
					fmt.Fprintf(w, "  New clone URL: %s (update your remotes: git remote set-url origin %s)\n", x.CloneUrl, x.CloneUrl)
				}
				return nil
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.StringVarP(&desc, "description", "d", "", "new description (\"\" clears it)")
	f.StringVar(&branch, "default-branch", "", "new default branch (must exist)")
	f.StringVar(&rename, "rename", "", "new `name` (admins; the old clone URL stops working)")
	f.BoolVar(&archive, "archive", false, "archive: make read-only (admins)")
	f.BoolVar(&unarchive, "unarchive", false, "unarchive (admins)")
	cmd.MarkFlagsMutuallyExclusive("archive", "unarchive")
	return withOutput(cmd, "Repo")
}

func newRepoDeleteCmd(a *app) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Permanently delete a repository (admins)",
		Long: `Permanently delete a repository and all its data. This can't be undone.
Asks you to type the name to confirm; pass --yes to skip that (required when
not running in a terminal).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, name, err := a.resolveRepo(args[0])
			if err != nil {
				return err
			}
			x, err := getRepo(cmd, c, name)
			if err != nil {
				return err
			}
			if !yes {
				if err := a.io.requirePrompt("confirmation", "--yes"); err != nil {
					return err
				}
				fmt.Fprintf(a.io.ErrOut, "%s This permanently deletes %s, including all branches, tags and history.\n", a.io.Red("!"), a.io.Bold(x.Name))
				ans, err := a.io.Prompt("Type the repository name to confirm", "")
				if err != nil {
					return err
				}
				if !strings.EqualFold(ans, x.Name) {
					return errorf(CodeCancelled, ExitError, "cancelled: the name didn't match")
				}
			}
			if _, err := c.DeleteRepoWithResponse(cmd.Context(), x.Name); err != nil {
				return err
			}
			return a.print(cmd, map[string]any{"name": x.Name, "deleted": true}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Deleted %s\n", a.io.SuccessIcon(), x.Name)
				return err
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	return withOutput(cmd, "Deleted")
}
