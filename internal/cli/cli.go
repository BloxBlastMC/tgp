// Package cli implements the tgp command line client.
//
// Conventions every command follows (documented in docs/cli.md):
//   - --json[=fields] prints the result as JSON matching `tgp schema <cmd>`;
//     --jq filters it.
//   - Errors go to stderr; with --json/--jq they are a single JSON object
//     {"error":{"code","message",...}}. Exit codes are listed in errors.go.
//   - Nothing prompts unless stdin and stdout are terminals and --no-input
//     isn't set; missing input is then an input_required error.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/version"
)

// Execute runs the CLI with the real terminal and returns the exit code.
func Execute(args []string) int {
	return run(args, systemIO())
}

func run(args []string, s *IOStreams) int {
	a := &app{io: s}
	root := newRootCmd(a)
	if cmd, _, err := root.Find(args); err == nil {
		args = rewriteJSONArgs(args, outputFields(cmd))
	}
	root.SetArgs(args)
	root.SetIn(s.In)
	root.SetOut(s.Out)
	root.SetErr(s.ErrOut)
	err := root.ExecuteContext(context.Background())
	if err == nil {
		return ExitOK
	}
	e := toError(err)
	asJSON := a.out.jsonMode()
	if !asJSON {
		// Flags may not have been parsed if parsing itself failed.
		for _, x := range args {
			if x == "--json" || strings.HasPrefix(x, "--json=") || x == "--jq" || strings.HasPrefix(x, "--jq=") {
				asJSON = true
			}
			if x == "--" {
				break
			}
		}
	}
	writeError(s.ErrOut, e, asJSON)
	if e.Code == CodeUsage && !asJSON && strings.Contains(e.Message, "unknown command") {
		fmt.Fprintln(s.ErrOut, "Run 'tgp --help' for usage.")
	}
	return e.Exit
}

// app is the state shared by all commands.
type app struct {
	io      *IOStreams
	out     output
	host    string // --host
	noInput bool

	cfg *Config
}

func (a *app) config() (*Config, error) {
	if a.cfg == nil {
		c, err := loadConfig()
		if err != nil {
			return nil, err
		}
		a.cfg = c
	}
	return a.cfg, nil
}

func newRootCmd(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "tgp",
		Short: "Work with tgp repositories, pull requests, issues and CI from the command line",
		Long: `Work with a tgp server from the command line.

Every command that prints data accepts --json (all fields), --json=a,b
(selected fields) and --jq <expr>. ` + "`tgp schema <command>`" + ` prints the JSON
Schema of a command's output. Errors are JSON on stderr when --json or --jq
is given, and exit codes are stable:

  0 ok, 1 error, 2 usage, 3 authentication, 4 forbidden, 5 not found,
  6 conflict/validation, 7 network/unavailable (retry may help)

Environment: TGP_HOST, TGP_TOKEN (skip the stored login), TGP_CONFIG_DIR,
TGP_NO_KEYRING=1, TGP_DEBUG=1 (log HTTP requests), NO_COLOR.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Version,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			a.out.pretty = a.io.IsStdoutTTY()
			if a.noInput || truthy(os.Getenv("TGP_NO_INPUT")) {
				a.io.noInput = true
			}
			return nil
		},
	}
	root.SetVersionTemplate(version.String("tgp") + "\n")
	pf := root.PersistentFlags()
	pf.StringVar(&a.out.json, "json", "", "print JSON; optionally only the given comma-separated `fields`")
	pf.Lookup("json").NoOptDefVal = "*"
	pf.StringVar(&a.out.jq, "jq", "", "filter JSON output with a jq `expression` (implies --json)")
	pf.StringVar(&a.host, "host", "", "tgp server to use (`host` or URL); default from the repo's git remote or your login")
	pf.BoolVar(&a.noInput, "no-input", false, "never prompt; fail with input_required instead")

	root.AddCommand(
		newAuthCmd(a),
		newRepoCmd(a),
		newPRCmd(a),
		newIssueCmd(a),
		newLabelCmd(a),
		newMilestoneCmd(a),
		newProtectionCmd(a),
		newRunCmd(a),
		newWorkflowCmd(a),
		newSecretCmd(a),
		newArtifactCmd(a),
		newCacheCmd(a),
		newReleaseCmd(a),
		newPackageCmd(a),
		newWebhookCmd(a),
		newAdminCmd(a),
		newAPICmd(a),
		newSchemaCmd(a),
		newVersionCmd(a),
	)

	// Help for commands with JSON output lists their fields.
	defHelp := root.HelpFunc()
	root.SetHelpFunc(func(c *cobra.Command, args []string) {
		if fields := outputFields(c); len(fields) > 0 && !strings.Contains(c.Long, "JSON fields:") {
			long := c.Long
			if long == "" {
				long = c.Short
			}
			c.Long = fmt.Sprintf("%s\n\nJSON fields: %s\nSchema: tgp schema %s", long, strings.Join(fields, ", "), commandName(c))
		}
		defHelp(c, args)
	})
	return root
}

// group makes a command that only holds subcommands reject unknown ones
// (cobra would otherwise print help and exit 0).
func group(cmd *cobra.Command) *cobra.Command {
	cmd.Args = cobra.ArbitraryArgs
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if len(args) > 0 {
			return &Error{Code: CodeUsage, Exit: ExitUsage,
				Message: fmt.Sprintf("unknown command %q for %q", args[0], c.CommandPath()),
				Hint:    fmt.Sprintf("run '%s --help' for the available commands", c.CommandPath())}
		}
		return c.Help()
	}
	return cmd
}

func truthy(s string) bool { return s != "" && s != "0" && strings.ToLower(s) != "false" }

func newVersionCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.print(cmd, version.Get(), func(w io.Writer) error {
				_, err := fmt.Fprintln(w, version.String("tgp"))
				return err
			})
		},
	}
	return withOutput(cmd, "Version")
}

// ---- host and repository resolution ----

// knownHosts maps host keys to base URLs from the config and TGP_HOST.
func (a *app) knownHosts() map[string]string {
	hosts := map[string]string{}
	if cfg, err := a.config(); err == nil {
		for k, h := range cfg.Hosts {
			hosts[k] = h.URL
		}
	}
	for _, in := range []string{os.Getenv("TGP_HOST"), a.host} {
		if in != "" {
			if base, key, err := normalizeHostURL(in); err == nil {
				hosts[key] = base
			}
		}
	}
	return hosts
}

// targetFor builds a target for a host key/URL with stored or env credentials.
func (a *app) targetFor(base, key string) (*target, error) {
	cfg, err := a.config()
	if err != nil {
		return nil, err
	}
	t := &target{Base: base, Key: key}
	if h := cfg.Hosts[key]; h != nil {
		if t.Base == "" {
			t.Base = h.URL
		}
		h.resolveToken(key)
		t.User, t.Token, t.Source = h.User, h.token, h.tokenSource
	}
	if tok := os.Getenv("TGP_TOKEN"); tok != "" {
		t.Token, t.Source, t.User = tok, "env", ""
	}
	return t, nil
}

// resolveHost picks the server: --host, TGP_HOST, the cwd's git remote, the
// default login, or the only login.
func (a *app) resolveHost() (*target, error) {
	for _, in := range []string{a.host, os.Getenv("TGP_HOST")} {
		if in != "" {
			base, key, err := normalizeHostURL(in)
			if err != nil {
				return nil, err
			}
			if cfg, _ := a.config(); cfg != nil && cfg.Hosts[key] != nil && !strings.Contains(in, "://") {
				base = cfg.Hosts[key].URL // keep the stored scheme (e.g. http for local servers)
			}
			return a.targetFor(base, key)
		}
	}
	if key, _, ok := a.repoFromGit(); ok {
		return a.targetFor(a.knownHosts()[key], key)
	}
	cfg, err := a.config()
	if err != nil {
		return nil, err
	}
	if cfg.DefaultHost != "" && cfg.Hosts[cfg.DefaultHost] != nil {
		return a.targetFor("", cfg.DefaultHost)
	}
	if names := cfg.hostNames(); len(names) == 1 {
		return a.targetFor("", names[0])
	}
	return nil, &Error{Code: CodeNotLoggedIn, Exit: ExitAuth,
		Message: "no tgp server configured",
		Hint:    "run `tgp auth login --host <url>`, or set TGP_HOST and TGP_TOKEN"}
}

// client resolves the host and returns an authenticated API client.
func (a *app) client() (*apiClient, error) {
	t, err := a.resolveHost()
	if err != nil {
		return nil, err
	}
	return a.clientFor(t)
}

func (a *app) clientFor(t *target) (*apiClient, error) {
	if t.Token == "" {
		return nil, &Error{Code: CodeNotLoggedIn, Exit: ExitAuth,
			Message: fmt.Sprintf("not logged in to %s", t.Key),
			Hint:    fmt.Sprintf("run `tgp auth login --host %s`, or set TGP_TOKEN", t.Base)}
	}
	return newAPIClient(t)
}

// repoFromGit finds a remote of the git repository in the working directory
// that points at a known tgp host. Prefers "origin", then "upstream".
func (a *app) repoFromGit() (hostKey, repo string, ok bool) {
	out, err := exec.Command("git", "config", "--get-regexp", `^remote\..*\.url$`).Output()
	if err != nil {
		return "", "", false
	}
	hosts := a.knownHosts()
	type match struct{ remote, key, repo string }
	var matches []match
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		k, v, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		remote := strings.TrimSuffix(strings.TrimPrefix(k, "remote."), ".url")
		key, path, ok := splitRemote(v)
		if !ok {
			continue
		}
		base, known := hosts[key]
		if !known {
			continue
		}
		// Strip the server's own path prefix (when tgp is served under a subpath).
		if _, bpath, ok := splitRemote(base); ok && bpath != "" {
			if !strings.HasPrefix(path, bpath+"/") {
				continue
			}
			path = strings.TrimPrefix(path, bpath+"/")
		}
		name := strings.TrimSuffix(path, ".git")
		if name == "" || strings.Contains(name, "/") {
			continue
		}
		matches = append(matches, match{remote, key, name})
	}
	for _, pref := range []string{"origin", "upstream"} {
		for _, m := range matches {
			if m.remote == pref {
				return m.key, m.repo, true
			}
		}
	}
	if len(matches) > 0 {
		return matches[0].key, matches[0].repo, true
	}
	return "", "", false
}

// resolveRepo determines the repository from an argument or -R value
// ("name", "host/name" or a URL), else from the cwd's git remote.
func (a *app) resolveRepo(arg string) (*apiClient, string, error) {
	if arg != "" {
		if strings.Contains(arg, "/") {
			in := arg
			if !strings.Contains(in, "://") {
				in = "https://" + in
			}
			key, path, ok := splitRemote(in)
			if !ok || path == "" {
				return nil, "", usageErr("invalid repository %q: use NAME, HOST/NAME or a clone URL", arg)
			}
			name := strings.TrimSuffix(path[strings.LastIndex(path, "/")+1:], ".git")
			base := a.knownHosts()[key]
			if base == "" {
				base = strings.TrimSuffix(in, "/"+path)
				base = strings.TrimSuffix(base, "/")
			}
			t, err := a.targetFor(base, key)
			if err != nil {
				return nil, "", err
			}
			c, err := a.clientFor(t)
			return c, name, err
		}
		c, err := a.client()
		return c, strings.TrimSuffix(arg, ".git"), err
	}
	if key, name, ok := a.repoFromGit(); ok && (a.host == "" || hostKeyOf(a.host) == key) {
		t, err := a.targetFor(a.knownHosts()[key], key)
		if err != nil {
			return nil, "", err
		}
		c, err := a.clientFor(t)
		return c, name, err
	}
	return nil, "", &Error{Code: CodeNoRepo, Exit: ExitUsage,
		Message: "no repository given",
		Hint:    "pass the repository name (or -R NAME), or run inside a clone of a tgp repository"}
}

func hostKeyOf(in string) string {
	_, key, _ := normalizeHostURL(in)
	return key
}

// addRepoFlag adds -R/--repo to a command.
func addRepoFlag(cmd *cobra.Command, dst *string) {
	cmd.Flags().StringVarP(dst, "repo", "R", "", "repository (`NAME`, HOST/NAME or URL); default from the current directory's git remote")
}
