package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/api/apigen"
	"git.taile67276.ts.net/BloxBlast/tgp/internal/apierr"
)

func newAuthCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Log in, log out and manage git credentials",
	}
	cmd.AddCommand(newAuthLoginCmd(a), newAuthLogoutCmd(a), newAuthStatusCmd(a), newAuthTokenCmd(a),
		newAuthSetupGitCmd(a), newAuthGitCredentialCmd(a))
	return group(cmd)
}

// authStatus is the AuthStatus schema.
type authStatus struct {
	Host        string    `json:"host"`
	URL         string    `json:"url"`
	LoggedIn    bool      `json:"logged_in"`
	Default     bool      `json:"default,omitempty"`
	User        string    `json:"user,omitempty"`
	Role        string    `json:"role,omitempty"`
	TokenSource string    `json:"token_source"`
	TokenScopes []string  `json:"token_scopes,omitempty"`
	Error       *errorRef `json:"error,omitempty"`
}

type errorRef struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var totpRe = regexp.MustCompile(`^\d{6}$`)

func newAuthLoginCmd(a *app) *cobra.Command {
	var (
		withToken, passwordStdin, insecure, setupGit bool
		username, otp, scopes                        string
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to a tgp server",
		Long: `Log in to a tgp server and store a personal access token for it.

By default you log in with your username and password (and a 2FA code if
enabled); tgp then creates a token named "tgp CLI on <hostname>" and stores it
in the OS keyring (or, if no keyring is available, in hosts.toml in the config
directory with 0600 permissions). Users with a temporary password are asked
to choose a new one.

With --with-token, an existing token is read from stdin instead. This is the
way to log in from scripts and agents.

The default token scopes are read and write, plus admin for admins.`,
		Example: `  tgp auth login --host git.example.com
  echo "$TOKEN" | tgp auth login --host git.example.com --with-token
  tgp auth login --host http://localhost:3000 -u alice --password-stdin < pw.txt`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			hostIn := a.host
			if hostIn == "" {
				hostIn = os.Getenv("TGP_HOST")
			}
			if hostIn == "" {
				if err := a.io.requirePrompt("the server", "--host"); err != nil {
					return err
				}
				var err error
				if hostIn, err = a.io.Prompt("tgp server URL", ""); err != nil {
					return err
				}
			}
			base, key, err := normalizeHostURL(hostIn)
			if err != nil {
				return err
			}
			if err := checkServer(ctx, base); err != nil {
				return err
			}
			if insecureRemote(base) {
				fmt.Fprintf(a.io.ErrOut, "%s %s uses plain HTTP: your password and token are sent unencrypted.\n", a.io.Yellow("!"), base)
			}
			cfg, err := a.config()
			if err != nil {
				return err
			}
			var token string
			var tokenID int64
			if withToken {
				if a.io.CanPrompt() {
					token, err = a.io.PromptPassword("Paste a personal access token")
				} else {
					token, err = a.io.ReadLine()
				}
				if err != nil {
					return err
				}
				token = strings.TrimSpace(token)
				if token == "" {
					return usageErr("no token given on stdin")
				}
			} else {
				var scopeList []string
				if cmd.Flags().Changed("scopes") {
					scopeList = splitList(scopes)
				}
				token, tokenID, err = a.passwordLogin(ctx, base, username, passwordStdin, otp, scopeList)
				if err != nil {
					return err
				}
			}
			t := &target{Base: base, Key: key, Token: token}
			c, err := newAPIClient(t)
			if err != nil {
				return err
			}
			sess, err := getSession(ctx, c)
			if err != nil {
				return err
			}
			// Replace any previous login, revoking the token tgp created for it.
			if old := cfg.Hosts[key]; old != nil && old.TokenID != 0 && old.TokenID != tokenID {
				old.resolveToken(key)
				if old.token != "" && old.token != token {
					if oc, err := newAPIClient(&target{Base: old.URL, Key: key, Token: old.token}); err == nil {
						_, _ = oc.DeleteTokenWithResponse(ctx, old.TokenID)
					}
				}
			}
			source, err := cfg.storeLogin(key, &HostConfig{URL: base, User: sess.User.Username, TokenID: tokenID}, token, insecure)
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("setup-git") && a.io.CanPrompt() {
				setupGit, err = a.io.Confirm(fmt.Sprintf("Let git use tgp for %s credentials?", base), true)
				if err != nil {
					return err
				}
			}
			if setupGit {
				if _, err := configureGit([]string{base}); err != nil {
					return err
				}
			}
			st := statusFrom(key, base, source, sess)
			st.Default = cfg.DefaultHost == key
			return a.print(cmd, st, func(w io.Writer) error {
				fmt.Fprintf(w, "%s Logged in to %s as %s (%s)\n", a.io.SuccessIcon(), key, a.io.Bold(st.User), st.Role)
				where := "the OS keyring"
				if source == "file" {
					where = cfg.path
				}
				fmt.Fprintf(w, "%s Token stored in %s\n", a.io.SuccessIcon(), where)
				if setupGit {
					fmt.Fprintf(w, "%s git will use tgp for %s credentials\n", a.io.SuccessIcon(), base)
				}
				return nil
			})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&withToken, "with-token", false, "read a personal access token from stdin")
	f.StringVarP(&username, "username", "u", "", "username or email (prompted if omitted)")
	f.BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin")
	f.StringVar(&otp, "otp", "", "2FA code or recovery code (prompted if needed and omitted)")
	f.StringVar(&scopes, "scopes", "read,write", "comma-separated scopes for the created token: read, write, admin")
	f.BoolVar(&insecure, "insecure-storage", false, "store the token in hosts.toml instead of the OS keyring")
	f.BoolVar(&setupGit, "setup-git", false, "configure git to use tgp as the credential helper for this server (asked interactively)")
	cmd.MarkFlagsMutuallyExclusive("with-token", "password-stdin")
	cmd.MarkFlagsMutuallyExclusive("with-token", "username")
	return withOutput(cmd, "AuthStatus")
}

// insecureRemote reports a plain-HTTP URL to a host other than loopback.
func insecureRemote(base string) bool {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return false
	}
	ip := net.ParseIP(h)
	return ip == nil || !ip.IsLoopback()
}

// checkServer verifies base is a tgp server that has been set up.
func checkServer(ctx context.Context, base string) error {
	c, err := newAPIClient(&target{Base: base})
	if err != nil {
		return err
	}
	r, err := c.GetMetaWithResponse(ctx)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Code != CodeNetwork {
			return &Error{Code: CodeNotTgp, Exit: ExitError, Message: fmt.Sprintf("%s doesn't look like a tgp server (%s)", base, e.Message)}
		}
		return err
	}
	if r.JSON200 == nil {
		return &Error{Code: CodeNotTgp, Exit: ExitError, Message: fmt.Sprintf("%s doesn't look like a tgp server", base)}
	}
	if r.JSON200.SetupRequired {
		return &Error{Code: apierr.CodeSetupRequired, Exit: ExitConflict,
			Message: base + " hasn't been set up yet", Hint: "open " + base + " in a browser to complete first-start setup"}
	}
	return nil
}

// passwordLogin logs in with a session, handles 2FA and a forced password
// change, creates a token and ends the session.
func (a *app) passwordLogin(ctx context.Context, base, username string, passwordStdin bool, otp string, scopes []string) (string, int64, error) {
	var err error
	if username == "" {
		if err := a.io.requirePrompt("a username", "--username (or use --with-token)"); err != nil {
			return "", 0, err
		}
		if username, err = a.io.Prompt("Username", ""); err != nil {
			return "", 0, err
		}
	}
	var password string
	if passwordStdin {
		password, err = a.io.ReadLine()
	} else {
		if err := a.io.requirePrompt("a password", "--password-stdin (or use --with-token)"); err != nil {
			return "", 0, err
		}
		password, err = a.io.PromptPassword("Password")
	}
	if err != nil {
		return "", 0, err
	}
	sc, err := newSessionClient(base)
	if err != nil {
		return "", 0, err
	}
	req := apigen.LoginRequest{Username: username, Password: password}
	setCode := func(code string) {
		code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
		if totpRe.MatchString(code) {
			req.TotpCode, req.RecoveryCode = &code, nil
		} else {
			req.RecoveryCode, req.TotpCode = &code, nil
		}
	}
	if otp != "" {
		setCode(otp)
	}
	var sess *apigen.Session
	for attempt := 0; ; attempt++ {
		r, err := sc.LoginWithResponse(ctx, req)
		var e *Error
		if errors.As(err, &e) && e.Code == apierr.CodeTOTPRequired && attempt == 0 && otp == "" {
			if err := a.io.requirePrompt("a 2FA code", "--otp"); err != nil {
				return "", 0, err
			}
			code, err := a.io.Prompt("2FA code (or a recovery code)", "")
			if err != nil {
				return "", 0, err
			}
			setCode(code)
			continue
		}
		if err != nil {
			return "", 0, err
		}
		if sess, err = must(r.JSON200, "login"); err != nil {
			return "", 0, err
		}
		break
	}
	defer func() { _, _ = sc.LogoutWithResponse(context.WithoutCancel(ctx)) }()

	if sess.MustChangePassword {
		if !a.io.CanPrompt() {
			return "", 0, &Error{Code: apierr.CodePasswordChange, Exit: ExitAuth,
				Message: "you have a temporary password that must be changed first",
				Hint:    "run `tgp auth login` in a terminal, or log in to the web UI, to choose a new password"}
		}
		fmt.Fprintln(a.io.ErrOut, "You're using a temporary password. Choose a new one (at least 10 characters).")
		for {
			p1, err := a.io.PromptPassword("New password")
			if err != nil {
				return "", 0, err
			}
			p2, err := a.io.PromptPassword("Repeat new password")
			if err != nil {
				return "", 0, err
			}
			if p1 != p2 {
				fmt.Fprintln(a.io.ErrOut, a.io.Red("X")+" The passwords don't match; try again.")
				continue
			}
			_, err = sc.ChangePasswordWithResponse(ctx, apigen.ChangePasswordRequest{CurrentPassword: password, NewPassword: p1})
			var e *Error
			if errors.As(err, &e) && e.Code == apierr.CodeValidation {
				fmt.Fprintln(a.io.ErrOut, a.io.Red("X")+" "+e.Message)
				continue
			}
			if err != nil {
				return "", 0, err
			}
			fmt.Fprintln(a.io.ErrOut, a.io.SuccessIcon()+" Password changed")
			break
		}
	}

	if scopes == nil {
		scopes = []string{"read", "write"}
		if sess.User.Role == apigen.RoleAdmin {
			scopes = append(scopes, "admin")
		}
	}
	ts := make([]apigen.TokenScope, len(scopes))
	for i, s := range scopes {
		ts[i] = apigen.TokenScope(s)
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown host"
	}
	r, err := sc.CreateTokenWithResponse(ctx, apigen.CreateTokenRequest{Name: "tgp CLI on " + hostname, Scopes: ts})
	if err != nil {
		return "", 0, err
	}
	created, err := must(r.JSON201, "token creation")
	if err != nil {
		return "", 0, err
	}
	return created.Token, created.AccessToken.Id, nil
}

func getSession(ctx context.Context, c *apiClient) (*apigen.Session, error) {
	r, err := c.GetSessionWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	return must(r.JSON200, "session")
}

func statusFrom(key, base, source string, s *apigen.Session) authStatus {
	st := authStatus{Host: key, URL: base, LoggedIn: true, TokenSource: source,
		User: s.User.Username, Role: string(s.User.Role)}
	if s.TokenScopes != nil {
		st.TokenScopes = *s.TokenScopes
	}
	return st
}

func newAuthLogoutCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out of a tgp server and revoke the CLI's token",
		Long: `Remove the stored login for a server (--host, or the default one). If the
token was created by ` + "`tgp auth login`" + `, it is also revoked on the server.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			key := cfg.DefaultHost
			if a.host != "" {
				if _, key, err = normalizeHostURL(a.host); err != nil {
					return err
				}
			} else if len(cfg.Hosts) > 1 {
				if err := a.io.requirePrompt("the server to log out of", "--host"); err != nil {
					return err
				}
				if key, err = a.io.Prompt("Log out of which server? ("+strings.Join(cfg.hostNames(), ", ")+")", cfg.DefaultHost); err != nil {
					return err
				}
			}
			h := cfg.Hosts[key]
			if h == nil {
				return &Error{Code: CodeNotLoggedIn, Exit: ExitAuth, Message: fmt.Sprintf("not logged in to %s", orNone(key))}
			}
			revoked := false
			if h.TokenID != 0 {
				h.resolveToken(key)
				if h.token != "" {
					if c, err := newAPIClient(&target{Base: h.URL, Key: key, Token: h.token}); err == nil {
						_, err := c.DeleteTokenWithResponse(cmd.Context(), h.TokenID)
						var e *Error
						revoked = err == nil || (errors.As(err, &e) && (e.Exit == ExitAuth || e.Exit == ExitNotFound))
					}
				}
			}
			if err := cfg.removeLogin(key); err != nil {
				return err
			}
			res := map[string]any{"host": key, "token_revoked": revoked}
			return a.print(cmd, res, func(w io.Writer) error {
				fmt.Fprintf(w, "%s Logged out of %s\n", a.io.SuccessIcon(), key)
				if h.TokenID != 0 && !revoked {
					fmt.Fprintln(w, a.io.Yellow("!")+" Couldn't revoke the token on the server; revoke it under Settings → Personal access tokens.")
				}
				if os.Getenv("TGP_TOKEN") != "" {
					fmt.Fprintln(w, a.io.Yellow("!")+" TGP_TOKEN is set in your environment and still applies.")
				}
				return nil
			})
		},
	}
	return withOutput(cmd, "Logout")
}

func orNone(s string) string {
	if s == "" {
		return "any server"
	}
	return s
}

func newAuthStatusCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show which servers you're logged in to, and check the tokens work",
		Long: `Check every stored login (or only --host) against its server. Exits with
code 3 if any login doesn't work.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			type entry struct{ base, key string }
			var hosts []entry
			if a.host != "" || os.Getenv("TGP_HOST") != "" {
				t, err := a.resolveHost()
				if err != nil {
					return err
				}
				hosts = append(hosts, entry{t.Base, t.Key})
			} else {
				for _, k := range cfg.hostNames() {
					hosts = append(hosts, entry{cfg.Hosts[k].URL, k})
				}
			}
			if len(hosts) == 0 {
				return &Error{Code: CodeNotLoggedIn, Exit: ExitAuth, Message: "not logged in to any tgp server",
					Hint: "run `tgp auth login --host <url>`"}
			}
			var out []authStatus
			failed := false
			for _, h := range hosts {
				t, err := a.targetFor(h.base, h.key)
				if err != nil {
					return err
				}
				st := authStatus{Host: h.key, URL: t.Base, TokenSource: orDefault(t.Source, "none"), User: t.User,
					Default: cfg.DefaultHost == h.key}
				if t.Token == "" {
					st.Error = &errorRef{Code: CodeNotLoggedIn, Message: "no token stored"}
				} else if c, err := newAPIClient(t); err != nil {
					return err
				} else if sess, err := getSession(cmd.Context(), c); err != nil {
					e := toError(err)
					st.Error = &errorRef{Code: e.Code, Message: e.Message}
				} else {
					s := statusFrom(h.key, t.Base, st.TokenSource, sess)
					s.Default = st.Default
					st = s
				}
				failed = failed || !st.LoggedIn
				out = append(out, st)
			}
			err = a.print(cmd, out, func(w io.Writer) error {
				for _, st := range out {
					def := ""
					if st.Default && len(out) > 1 {
						def = a.io.Dim(" (default)")
					}
					fmt.Fprintf(w, "%s%s\n", a.io.Bold(st.Host), def)
					if st.LoggedIn {
						scopes := ""
						if len(st.TokenScopes) > 0 {
							scopes = ", scopes: " + strings.Join(st.TokenScopes, ", ")
						}
						fmt.Fprintf(w, "  %s Logged in as %s (%s)\n", a.io.SuccessIcon(), st.User, st.Role)
						fmt.Fprintf(w, "  %s Token from %s%s\n", a.io.SuccessIcon(), st.TokenSource, scopes)
					} else {
						fmt.Fprintf(w, "  %s %s\n", a.io.Red("X"), st.Error.Message)
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			if failed {
				return &Error{Code: apierr.CodeUnauthenticated, Exit: ExitAuth, Message: "", silent: true}
			}
			return nil
		},
	}
	return withOutput(cmd, "[]AuthStatus")
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func newAuthTokenCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print the token for a server",
		Long:  "Print the stored token for the server (--host or the default), e.g. to pass it to another tool.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := a.resolveHost()
			if err != nil {
				return err
			}
			if t.Token == "" {
				return &Error{Code: CodeNotLoggedIn, Exit: ExitAuth, Message: "not logged in to " + t.Key}
			}
			_, err = fmt.Fprintln(a.io.Out, t.Token)
			return err
		},
	}
}

// ---- git integration ----

func helperCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if p, err := exec.LookPath("tgp"); err == nil {
		if same(p, exe) {
			exe = "tgp"
		}
	}
	if strings.ContainsAny(exe, " \t'\"\\") {
		exe = "'" + strings.ReplaceAll(exe, "'", `'\''`) + "'"
	}
	return "!" + exe + " auth git-credential", nil
}

func same(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}

// configureGit makes tgp the credential helper for each base URL in the
// global git config. An empty helper entry first resets helpers inherited
// from broader config so stale credentials aren't tried first.
func configureGit(bases []string) (string, error) {
	helper, err := helperCommand()
	if err != nil {
		return "", err
	}
	for _, base := range bases {
		key := "credential." + base + ".helper"
		for _, args := range [][]string{
			{"config", "--global", "--replace-all", key, ""},
			{"config", "--global", "--add", key, helper},
		} {
			if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
				return "", errorf(CodeGit, ExitError, "git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			}
		}
	}
	return helper, nil
}

func newAuthSetupGitCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup-git",
		Short: "Make git use tgp for HTTPS credentials",
		Long: `Configure git (globally) to get credentials for your tgp servers from
` + "`tgp auth git-credential`" + `, so clone, fetch and push work without pasting tokens.
Applies to every logged-in server, or only --host.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			var bases []string
			if a.host != "" {
				t, err := a.resolveHost()
				if err != nil {
					return err
				}
				bases = []string{t.Base}
			} else {
				for _, k := range cfg.hostNames() {
					bases = append(bases, cfg.Hosts[k].URL)
				}
			}
			if len(bases) == 0 {
				return &Error{Code: CodeNotLoggedIn, Exit: ExitAuth, Message: "not logged in to any tgp server",
					Hint: "run `tgp auth login --host <url>` first"}
			}
			helper, err := configureGit(bases)
			if err != nil {
				return err
			}
			return a.print(cmd, map[string]any{"urls": bases, "helper": helper}, func(w io.Writer) error {
				for _, b := range bases {
					fmt.Fprintf(w, "%s git will use tgp for %s credentials\n", a.io.SuccessIcon(), b)
				}
				return nil
			})
		},
	}
	return withOutput(cmd, "GitSetup")
}

func newAuthGitCredentialCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:    "git-credential <get|store|erase>",
		Short:  "git credential helper (used by git, not run directly)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "get" {
				// tgp owns the tokens; git's store/erase requests are ignored.
				_, _ = io.Copy(io.Discard, a.io.In)
				return nil
			}
			attrs := map[string]string{}
			for {
				line, err := a.io.ReadLine()
				if err != nil || line == "" {
					break
				}
				if k, v, ok := strings.Cut(line, "="); ok {
					attrs[k] = v
				}
			}
			proto, host := attrs["protocol"], strings.ToLower(attrs["host"])
			if (proto != "https" && proto != "http") || host == "" {
				return nil
			}
			cfg, err := a.config()
			if err != nil {
				return err
			}
			var base string
			if h := cfg.Hosts[host]; h != nil {
				base = h.URL
			} else if env := os.Getenv("TGP_HOST"); env != "" {
				if b, k, err := normalizeHostURL(env); err == nil && k == host {
					base = b
				}
			}
			if base == "" || !strings.HasPrefix(base, proto+"://") {
				return nil
			}
			t, err := a.targetFor(base, host)
			if err != nil || t.Token == "" {
				return nil
			}
			user := orDefault(t.User, "tgp")
			fmt.Fprintf(a.io.Out, "protocol=%s\nhost=%s\nusername=%s\npassword=%s\n", proto, attrs["host"], user, t.Token)
			return nil
		},
	}
}
