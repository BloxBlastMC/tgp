package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
)

func newAdminCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administer users, runners, CI limits, the registry and the audit log (admins)",
		Long:  "Admin commands. They need an admin account and a token with the admin scope.",
	}
	user := &cobra.Command{
		Use:     "user",
		Aliases: []string{"users"},
		Short:   "Manage users",
	}
	group(user)
	user.AddCommand(newAdminUserListCmd(a), newAdminUserViewCmd(a), newAdminUserCreateCmd(a),
		newAdminUserEditCmd(a), newAdminUserResetPasswordCmd(a), newAdminUserReset2FACmd(a),
		newAdminUserAccessCmd(a))
	cmd.AddCommand(user, newAdminHealthCmd(a), newAdminRunnerCmd(a), newAdminCICmd(a), newAdminRegistryCmd(a), newAdminAuditCmd(a))
	return group(cmd)
}

func validRole(r string) (apigen.Role, error) {
	switch apigen.Role(r) {
	case apigen.RoleAdmin, apigen.RoleMaintainer, apigen.RoleTester:
		return apigen.Role(r), nil
	}
	return "", usageErr("invalid role %q: use admin, maintainer or tester", r)
}

func userState(a *app, u *apigen.User) string {
	switch {
	case u.Disabled:
		return a.io.Red("disabled")
	case u.MustChangePassword:
		return a.io.Yellow("temp password")
	}
	return "active"
}

func userAccess(u *apigen.User) string {
	if u.Role == apigen.RoleAdmin || u.AllRepos {
		return "all repos"
	}
	return "selected repos"
}

func newAdminUserListCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all users",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.AdminListUsersWithResponse(cmd.Context())
			if err != nil {
				return err
			}
			users, err := must(r.JSON200, "user list")
			if err != nil {
				return err
			}
			return a.print(cmd, users, func(w io.Writer) error {
				rows := make([][]string, len(*users))
				for i := range *users {
					u := &(*users)[i]
					twofa := "-"
					if u.TotpEnabled {
						twofa = "on"
					}
					rows[i] = []string{u.Username, u.DisplayName, string(u.Role), userAccess(u), twofa, userState(a, u), ago(u.LastLoginAt)}
				}
				return printTable(w, a.io, []string{"username", "name", "role", "access", "2fa", "state", "last login"}, rows)
			})
		},
	}
	return withOutput(cmd, "[]User")
}

func printUser(w io.Writer, a *app, u *apigen.User) error {
	twofa := "off"
	if u.TotpEnabled {
		twofa = "on"
	}
	return printFields(w, a.io, [][2]string{
		{"username", a.io.Bold(u.Username)},
		{"name", u.DisplayName},
		{"email", u.Email},
		{"role", string(u.Role)},
		{"access", userAccess(u)},
		{"2fa", twofa},
		{"state", userState(a, u)},
		{"last login", ago(u.LastLoginAt)},
		{"created", ago(&u.CreatedAt)},
	})
}

func newAdminUserViewCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view USERNAME",
		Short: "Show a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.AdminGetUserWithResponse(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			u, err := must(r.JSON200, "user")
			if err != nil {
				return err
			}
			return a.print(cmd, u, func(w io.Writer) error { return printUser(w, a, u) })
		},
	}
	return withOutput(cmd, "User")
}

func printTempPassword(w io.Writer, a *app, verb string, res *apigen.UserWithTempPassword) error {
	fmt.Fprintf(w, "%s %s %s (%s)\n", a.io.SuccessIcon(), verb, a.io.Bold(res.User.Username), res.User.Role)
	fmt.Fprintf(w, "  Temporary password: %s\n", a.io.Bold(res.TempPassword))
	fmt.Fprintf(w, "  It expires %s and is shown only now. They must choose a new password at first login.\n",
		res.TempPasswordExpiresAt.Local().Format(time.RFC1123))
	return nil
}

func newAdminUserCreateCmd(a *app) *cobra.Command {
	var (
		role, name, email, repos string
		allRepos                 bool
	)
	cmd := &cobra.Command{
		Use:   "create USERNAME --role ROLE",
		Short: "Create a user with a temporary password",
		Long: `Create a user. A temporary password is generated and printed once; the
user must change it at first login. Maintainers and testers get access to the
repositories in --repos, or to every repository with --all-repos.`,
		Example: `  tgp admin user create alice --role maintainer --all-repos
  tgp admin user create bob --role tester --repos game-server,website --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rl, err := validRole(role)
			if err != nil {
				return err
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			req := apigen.CreateUserRequest{Username: args[0], Role: rl, AllRepos: &allRepos}
			if name != "" {
				req.DisplayName = &name
			}
			if email != "" {
				req.Email = &email
			}
			r, err := c.AdminCreateUserWithResponse(cmd.Context(), req)
			if err != nil {
				return err
			}
			res, err := must(r.JSON201, "user create")
			if err != nil {
				return err
			}
			if list := splitList(repos); len(list) > 0 {
				if _, err := c.AdminSetUserReposWithResponse(cmd.Context(), res.User.Username,
					apigen.AdminSetUserReposJSONRequestBody{AllRepos: allRepos, Repos: list}); err != nil {
					e := toError(err)
					e.Message = fmt.Sprintf("created %s (temporary password: %s) but couldn't grant repositories: %s",
						res.User.Username, res.TempPassword, e.Message)
					return e
				}
			}
			return a.print(cmd, res, func(w io.Writer) error { return printTempPassword(w, a, "Created", res) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&role, "role", "", "admin, maintainer or tester (required)")
	f.StringVar(&name, "name", "", "display name")
	f.StringVar(&email, "email", "", "email address")
	f.BoolVar(&allRepos, "all-repos", false, "access to every repository, including future ones")
	f.StringVar(&repos, "repos", "", "comma-separated repositories to grant access to")
	_ = cmd.MarkFlagRequired("role")
	return withOutput(cmd, "UserWithTempPassword")
}

func newAdminUserEditCmd(a *app) *cobra.Command {
	var (
		role, name, email  string
		disable, enable    bool
		allRepos, selected bool
	)
	cmd := &cobra.Command{
		Use:   "edit USERNAME",
		Short: "Change a user's role, profile or disabled state",
		Long: `Change a user. Disabling signs them out everywhere and stops their tokens
working. To change which repositories they can access, use ` + "`tgp admin user access`" + `.`,
		Example: `  tgp admin user edit alice --role admin
  tgp admin user edit bob --disable`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := apigen.UpdateUserRequest{}
			if role != "" {
				rl, err := validRole(role)
				if err != nil {
					return err
				}
				req.Role = &rl
			}
			if cmd.Flags().Changed("name") {
				req.DisplayName = &name
			}
			if cmd.Flags().Changed("email") {
				req.Email = &email
			}
			if disable || enable {
				req.Disabled = &disable
			}
			if allRepos || selected {
				req.AllRepos = &allRepos
			}
			if req == (apigen.UpdateUserRequest{}) {
				return usageErr("nothing to change: pass --role, --name, --email, --disable or --enable")
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.AdminUpdateUserWithResponse(cmd.Context(), args[0], req)
			if err != nil {
				return err
			}
			u, err := must(r.JSON200, "user edit")
			if err != nil {
				return err
			}
			return a.print(cmd, u, func(w io.Writer) error {
				fmt.Fprintf(w, "%s Updated %s\n", a.io.SuccessIcon(), a.io.Bold(u.Username))
				return printUser(w, a, u)
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&role, "role", "", "new role: admin, maintainer or tester")
	f.StringVar(&name, "name", "", "display name")
	f.StringVar(&email, "email", "", "email address")
	f.BoolVar(&disable, "disable", false, "disable the account")
	f.BoolVar(&enable, "enable", false, "re-enable the account")
	f.BoolVar(&allRepos, "all-repos", false, "give access to every repository")
	f.BoolVar(&selected, "selected-repos", false, "limit access to granted repositories")
	cmd.MarkFlagsMutuallyExclusive("disable", "enable")
	cmd.MarkFlagsMutuallyExclusive("all-repos", "selected-repos")
	return withOutput(cmd, "User")
}

func newAdminUserResetPasswordCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset-password USERNAME",
		Short: "Issue a new temporary password",
		Long:  "Issue a new temporary password. The user is signed out everywhere and must choose a new password at next login.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.AdminResetPasswordWithResponse(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			res, err := must(r.JSON200, "password reset")
			if err != nil {
				return err
			}
			return a.print(cmd, res, func(w io.Writer) error { return printTempPassword(w, a, "Reset the password of", res) })
		},
	}
	return withOutput(cmd, "UserWithTempPassword")
}

func newAdminUserReset2FACmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset-2fa USERNAME",
		Short: "Turn off a user's 2FA (e.g. lost device)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.AdminResetTotpWithResponse(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			u, err := must(r.JSON200, "2FA reset")
			if err != nil {
				return err
			}
			return a.print(cmd, u, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s Turned off 2FA for %s\n", a.io.SuccessIcon(), u.Username)
				return err
			})
		},
	}
	return withOutput(cmd, "User")
}

func newAdminUserAccessCmd(a *app) *cobra.Command {
	var (
		all, selected      bool
		grant, revoke, set string
	)
	cmd := &cobra.Command{
		Use:   "access USERNAME",
		Short: "Show or change which repositories a user can access",
		Long: `Without flags, show a user's repository access. With flags, change it:
--all / --selected switch between every repository and only granted ones;
--grant and --revoke add or remove repositories; --set replaces the list.
Granted repositories are kept while --all is on, so switching back restores them.`,
		Example: `  tgp admin user access bob
  tgp admin user access bob --grant game-server,website
  tgp admin user access bob --revoke website
  tgp admin user access alice --all`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			r, err := c.AdminGetUserReposWithResponse(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			cur, err := must(r.JSON200, "user access")
			if err != nil {
				return err
			}
			f := cmd.Flags()
			if f.Changed("all") || f.Changed("selected") || f.Changed("grant") || f.Changed("revoke") || f.Changed("set") {
				body := apigen.AdminSetUserReposJSONRequestBody{AllRepos: cur.AllRepos, Repos: cur.Repos}
				if all || selected {
					body.AllRepos = all
				}
				if f.Changed("set") {
					body.Repos = splitList(set)
				}
				for _, g := range splitList(grant) {
					if !slices.ContainsFunc(body.Repos, func(x string) bool { return strings.EqualFold(x, g) }) {
						body.Repos = append(body.Repos, g)
					}
				}
				for _, rv := range splitList(revoke) {
					body.Repos = slices.DeleteFunc(body.Repos, func(x string) bool { return strings.EqualFold(x, rv) })
				}
				if body.Repos == nil {
					body.Repos = []string{}
				}
				pr, err := c.AdminSetUserReposWithResponse(cmd.Context(), args[0], body)
				if err != nil {
					return err
				}
				if cur, err = must(pr.JSON200, "user access"); err != nil {
					return err
				}
			}
			return a.print(cmd, cur, func(w io.Writer) error {
				if cur.AllRepos {
					fmt.Fprintf(w, "%s can access %s.\n", args[0], a.io.Bold("all repositories"))
					if len(cur.Repos) > 0 {
						fmt.Fprintf(w, "%s\n", a.io.Dim("Kept for when all-repos access is turned off: "+strings.Join(cur.Repos, ", ")))
					}
					return nil
				}
				if len(cur.Repos) == 0 {
					fmt.Fprintf(w, "%s can't access any repository.\n", args[0])
					return nil
				}
				fmt.Fprintf(w, "%s can access:\n", args[0])
				for _, x := range cur.Repos {
					fmt.Fprintf(w, "  %s\n", x)
				}
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&all, "all", false, "access to every repository")
	f.BoolVar(&selected, "selected", false, "access only to granted repositories")
	f.StringVar(&grant, "grant", "", "comma-separated repositories to add")
	f.StringVar(&revoke, "revoke", "", "comma-separated repositories to remove")
	f.StringVar(&set, "set", "", "replace the granted repositories with this comma-separated list")
	cmd.MarkFlagsMutuallyExclusive("all", "selected")
	cmd.MarkFlagsMutuallyExclusive("set", "grant")
	cmd.MarkFlagsMutuallyExclusive("set", "revoke")
	return withOutput(cmd, "UserRepoAccess")
}

func newAdminAuditCmd(a *app) *cobra.Command {
	var (
		limit         int
		before        int64
		actor, action string
	)
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Show the security audit log, newest first",
		Example: `  tgp admin audit --actor alice
  tgp admin audit --action auth.login --limit 100
  tgp admin audit --json --jq '.entries[] | select(.action == "user.create")'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			p := &apigen.ListAuditLogParams{Limit: &limit}
			if before > 0 {
				p.Before = &before
			}
			if actor != "" {
				p.Actor = &actor
			}
			if action != "" {
				p.Action = &action
			}
			r, err := c.ListAuditLogWithResponse(cmd.Context(), p)
			if err != nil {
				return err
			}
			page, err := must(r.JSON200, "audit log")
			if err != nil {
				return err
			}
			return a.print(cmd, page, func(w io.Writer) error {
				rows := make([][]string, len(page.Entries))
				for i, e := range page.Entries {
					target := e.Target
					if e.TargetType != "" && target != "" {
						target = e.TargetType + ":" + target
					}
					rows[i] = []string{e.CreatedAt.Local().Format("2006-01-02 15:04:05"), orDefault(e.Actor, "-"), e.Action, target, e.Ip}
				}
				if err := printTable(w, a.io, []string{"time", "actor", "action", "target", "ip"}, rows); err != nil {
					return err
				}
				if page.NextBefore != nil {
					fmt.Fprintln(a.io.ErrOut, a.io.Dim(fmt.Sprintf("More: tgp admin audit --before %d", *page.NextBefore)))
				}
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.IntVarP(&limit, "limit", "L", 50, "entries to show (1-200)")
	f.Int64Var(&before, "before", 0, "show entries older than this `id` (for paging)")
	f.StringVar(&actor, "actor", "", "only actions by this username")
	f.StringVar(&action, "action", "", "only actions starting with this, e.g. user. or auth.login")
	return withOutput(cmd, "AuditPage")
}
