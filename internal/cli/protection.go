package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

func newProtectionCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "protection",
		Aliases: []string{"protect"},
		Short:   "Manage branch protection rules",
		Long: `List and change branch protection rules. Anyone can list them; changing
them needs an admin. A rule's pattern is a branch name or a glob (release/*).`,
	}
	cmd.AddCommand(newProtectionListCmd(a), newProtectionSetCmd(a), newProtectionDeleteCmd(a))
	return group(cmd)
}

func listRules(ctx context.Context, c *apiClient, repo string) ([]apigen.BranchProtection, error) {
	r, err := c.ListProtectionsWithResponse(ctx, repo)
	if err != nil {
		return nil, err
	}
	rules, err := must(r.JSON200, "protection rules")
	if err != nil {
		return nil, err
	}
	return *rules, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func newProtectionListCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List branch protection rules",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			rules, err := listRules(cmd.Context(), c, repo)
			if err != nil {
				return err
			}
			return a.print(cmd, rules, func(w io.Writer) error {
				if len(rules) == 0 {
					fmt.Fprintln(a.io.ErrOut, "No protection rules.")
					return nil
				}
				rows := make([][]string, len(rules))
				for i, r := range rules {
					rows[i] = []string{r.Pattern, yesNo(r.RequirePullRequest), strconv.Itoa(r.RequiredApprovals), yesNo(r.DismissStaleApprovals),
						yesNo(r.RequireUpToDate), yesNo(r.BlockForcePush), yesNo(r.AllowDeletion), yesNo(r.AdminsBypass)}
				}
				return printTable(w, a.io, []string{"pattern", "pr only", "approvals", "stale dismissed", "up to date", "no force", "deletable", "admin bypass"}, rows)
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "[]BranchProtection")
}

func newProtectionSetCmd(a *app) *cobra.Command {
	var (
		repoFlag                                                          string
		approvals                                                         int
		requirePR, dismissStale, upToDate, blockForce, allowDelete, admin bool
	)
	cmd := &cobra.Command{
		Use:   "set PATTERN",
		Short: "Create or change the rule for a branch pattern (admins)",
		Long: `Create the rule for PATTERN, or change it if it exists (only the flags you
pass change). New rules default to: pull requests required, 1 approval, stale
approvals dismissed, force pushes blocked, deletion blocked, no admin bypass.`,
		Example: `  tgp protection set main
  tgp protection set main --approvals 2 --up-to-date
  tgp protection set "release/*" --approvals 0 --allow-deletion=false
  tgp protection set main --admin-bypass`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			rules, err := listRules(cmd.Context(), c, repo)
			if err != nil {
				return err
			}
			f := cmd.Flags()
			pick := func(name string, v bool) *bool {
				if f.Changed(name) {
					return &v
				}
				return nil
			}
			var ap *int
			if f.Changed("approvals") {
				ap = &approvals
			}
			var out *apigen.BranchProtection
			created := false
			for _, r := range rules {
				if r.Pattern != args[0] {
					continue
				}
				resp, err := c.UpdateProtectionWithResponse(cmd.Context(), repo, r.Id, apigen.BranchProtectionPatch{
					RequirePullRequest: pick("require-pr", requirePR), RequiredApprovals: ap,
					DismissStaleApprovals: pick("dismiss-stale", dismissStale), RequireUpToDate: pick("up-to-date", upToDate),
					BlockForcePush: pick("block-force-push", blockForce), AllowDeletion: pick("allow-deletion", allowDelete),
					AdminsBypass: pick("admin-bypass", admin),
				})
				if err != nil {
					return err
				}
				if out, err = must(resp.JSON200, "protection rule"); err != nil {
					return err
				}
			}
			if out == nil {
				resp, err := c.CreateProtectionWithResponse(cmd.Context(), repo, apigen.BranchProtectionInput{
					Pattern: args[0], RequirePullRequest: pick("require-pr", requirePR), RequiredApprovals: ap,
					DismissStaleApprovals: pick("dismiss-stale", dismissStale), RequireUpToDate: pick("up-to-date", upToDate),
					BlockForcePush: pick("block-force-push", blockForce), AllowDeletion: pick("allow-deletion", allowDelete),
					AdminsBypass: pick("admin-bypass", admin),
				})
				if err != nil {
					return err
				}
				if out, err = must(resp.JSON201, "protection rule"); err != nil {
					return err
				}
				created = true
			}
			return a.print(cmd, out, func(w io.Writer) error {
				verb := "Updated"
				if created {
					verb = "Created"
				}
				fmt.Fprintf(w, "%s %s protection for %s\n", a.io.SuccessIcon(), verb, a.io.Bold(out.Pattern))
				return printFields(w, a.io, [][2]string{
					{"pull request required", yesNo(out.RequirePullRequest)},
					{"approvals", strconv.Itoa(out.RequiredApprovals)},
					{"stale approvals dismissed", yesNo(out.DismissStaleApprovals)},
					{"must be up to date", yesNo(out.RequireUpToDate)},
					{"force push blocked", yesNo(out.BlockForcePush)},
					{"deletion allowed", yesNo(out.AllowDeletion)},
					{"admins may bypass", yesNo(out.AdminsBypass)},
				})
			})
		},
	}
	addRepoFlag(cmd, &repoFlag)
	f := cmd.Flags()
	f.IntVar(&approvals, "approvals", 1, "approvals required to merge (0-10)")
	f.BoolVar(&requirePR, "require-pr", true, "changes must go through pull requests (no direct pushes)")
	f.BoolVar(&dismissStale, "dismiss-stale", true, "approvals of an older head don't count")
	f.BoolVar(&upToDate, "up-to-date", false, "the head must contain the latest base commit to merge")
	f.BoolVar(&blockForce, "block-force-push", true, "reject force pushes")
	f.BoolVar(&allowDelete, "allow-deletion", false, "allow deleting matching branches")
	f.BoolVar(&admin, "admin-bypass", false, "admins may push directly and merge despite unmet requirements")
	return withOutput(cmd, "BranchProtection")
}

func newProtectionDeleteCmd(a *app) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   "delete PATTERN",
		Short: "Remove the rule for a branch pattern (admins)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, repo, err := a.resolveRepo(repoFlag)
			if err != nil {
				return err
			}
			rules, err := listRules(cmd.Context(), c, repo)
			if err != nil {
				return err
			}
			for _, r := range rules {
				if r.Pattern == args[0] {
					if _, err := c.DeleteProtectionWithResponse(cmd.Context(), repo, r.Id); err != nil {
						return err
					}
					return a.print(cmd, map[string]any{"name": r.Pattern, "deleted": true}, func(w io.Writer) error {
						_, err := fmt.Fprintf(w, "%s Removed protection for %s\n", a.io.SuccessIcon(), r.Pattern)
						return err
					})
				}
			}
			return &Error{Code: "not_found", Exit: ExitNotFound, Message: fmt.Sprintf("no protection rule for %q in %s", args[0], repo)}
		},
	}
	addRepoFlag(cmd, &repoFlag)
	return withOutput(cmd, "Deleted")
}
