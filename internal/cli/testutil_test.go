package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/testserver"
)

// TestMain doubles as the tgp binary: git runs the credential helper
// (`<test binary> auth git-credential get`) and the server's hooks exec it.
func TestMain(m *testing.M) {
	testserver.RunHookIfRequested()
	if os.Getenv("TGP_TEST_CLI") == "1" {
		os.Exit(Execute(os.Args[1:]))
	}
	os.Exit(m.Run())
}

type cliEnv struct {
	t     *testing.T
	srv   *testserver.Server
	admin *testserver.Client
	home  string
}

// newCLIEnv starts a set-up server and isolates the CLI and git from the
// real user config. Tests using it can't run in parallel (they set env).
func newCLIEnv(t *testing.T) *cliEnv {
	t.Helper()
	srv := testserver.Start(t)
	admin := srv.Setup()
	home := t.TempDir()
	gitcfg := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(gitcfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{
		"HOME":                home,
		"XDG_CONFIG_HOME":     filepath.Join(home, ".config"),
		"TGP_CONFIG_DIR":      filepath.Join(home, "tgp"),
		"TGP_NO_KEYRING":      "1",
		"TGP_TEST_CLI":        "1",
		"TGP_TOKEN":           "",
		"TGP_HOST":            "",
		"TGP_NO_INPUT":        "",
		"TGP_DEBUG":           "",
		"GIT_CONFIG_GLOBAL":   gitcfg,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_AUTHOR_NAME":     "Test",
		"GIT_AUTHOR_EMAIL":    "test@example.com",
		"GIT_COMMITTER_NAME":  "Test",
		"GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		t.Setenv(k, v)
	}
	os.Unsetenv("TGP_TOKEN")
	os.Unsetenv("TGP_HOST")
	t.Chdir(home)
	return &cliEnv{t: t, srv: srv, admin: admin, home: home}
}

type result struct {
	stdout, stderr string
	code           int
}

// errCode returns the code of a JSON error on stderr.
func (r result) errCode() string {
	var e struct {
		Error struct{ Code string } `json:"error"`
	}
	_ = json.Unmarshal([]byte(r.stderr), &e)
	return e.Error.Code
}

func (e *cliEnv) exec(s *IOStreams, args ...string) result {
	var out, errOut bytes.Buffer
	s.Out, s.ErrOut = &out, &errOut
	code := run(args, s)
	return result{out.String(), errOut.String(), code}
}

// run runs the CLI non-interactively (like an agent or script).
func (e *cliEnv) run(stdin string, args ...string) result {
	return e.exec(&IOStreams{In: strings.NewReader(stdin)}, args...)
}

// runTTY runs the CLI as if in a terminal; stdin answers the prompts.
func (e *cliEnv) runTTY(stdin string, args ...string) result {
	return e.exec(&IOStreams{In: strings.NewReader(stdin), stdinTTY: true, stdoutTTY: true}, args...)
}

// ok runs a command and fails the test unless it exits 0.
func (e *cliEnv) ok(stdin string, args ...string) result {
	e.t.Helper()
	r := e.run(stdin, args...)
	if r.code != 0 {
		e.t.Fatalf("tgp %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), r.code, r.stdout, r.stderr)
	}
	return r
}

// fail runs a command and checks its exit code and JSON error code.
func (e *cliEnv) fail(wantExit int, wantCode string, args ...string) result {
	e.t.Helper()
	r := e.run("", args...)
	if r.code != wantExit || (wantCode != "" && r.errCode() != wantCode) {
		e.t.Fatalf("tgp %s: got exit %d code %q, want exit %d code %q\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), r.code, r.errCode(), wantExit, wantCode, r.stdout, r.stderr)
	}
	return r
}

// jsonOK runs a command with --json, validates the output against the
// command's schema and decodes it into out.
func (e *cliEnv) jsonOK(out any, args ...string) {
	e.t.Helper()
	args = append(args, "--json")
	r := e.ok("", args...)
	validateOutput(e.t, args, r.stdout)
	if out != nil {
		if err := json.Unmarshal([]byte(r.stdout), out); err != nil {
			e.t.Fatalf("decode %q: %v", r.stdout, err)
		}
	}
}

// validateOutput checks stdout against the schema of the command in args.
func validateOutput(t *testing.T, args []string, stdout string) {
	t.Helper()
	root := newRootCmd(&app{io: &IOStreams{}})
	cmd, _, err := root.Find(args)
	if err != nil {
		t.Fatal(err)
	}
	name, s, ok := outputSchema(cmd)
	if !ok {
		t.Fatalf("tgp %s has no output schema", strings.Join(args, " "))
	}
	var v any
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("tgp %s: invalid JSON %q: %v", strings.Join(args, " "), stdout, err)
	}
	if err := s.VisitJSON(v); err != nil {
		t.Fatalf("tgp %s: output doesn't match schema %s: %v\n%s", strings.Join(args, " "), name, err, stdout)
	}
}

// login logs the CLI in as username with a password.
func (e *cliEnv) login(username, password string) {
	e.t.Helper()
	e.ok(password+"\n", "auth", "login", "--host", e.srv.URL, "-u", username, "--password-stdin")
}

func (e *cliEnv) git(dir string, args ...string) string {
	e.t.Helper()
	out, err := e.gitErr(dir, args...)
	if err != nil {
		e.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (e *cliEnv) gitErr(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
