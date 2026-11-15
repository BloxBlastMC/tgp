package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.taile67276.ts.net/BloxBlast/tgp/internal/ci/runnerapi"
)

func TestRegistryOverride(t *testing.T) {
	spec := &runnerapi.JobSpec{Registry: runnerapi.Registry{Host: "git.example.com", Path: "acme/app"}}
	r := &Runner{cfg: Config{URL: "http://tgp:3000"}}
	r.addJobEnv(spec)
	if spec.Env["TGP_REGISTRY"] != "git.example.com" || spec.Env["TGP_IMAGE"] != "git.example.com/acme/app" {
		t.Fatalf("default: %v", spec.Env)
	}
	spec = &runnerapi.JobSpec{Registry: runnerapi.Registry{Host: "git.example.com", Path: "acme/app"}}
	r.cfg.Registry = "tgp.internal:8443"
	r.addJobEnv(spec)
	if spec.Env["TGP_REGISTRY"] != "tgp.internal:8443" || spec.Env["TGP_IMAGE"] != "tgp.internal:8443/acme/app" {
		t.Fatalf("override: %v", spec.Env)
	}
}

func TestRegistryConfigValidation(t *testing.T) {
	for v, ok := range map[string]bool{
		"": true, "Git.Internal": true, "git.internal:8443": true, "10.0.0.5:3000": true,
		"https://git.internal": false, "git.internal/v2": false, "u@git.internal": false, ":443": false,
	} {
		p := filepath.Join(t.TempDir(), "runner.toml")
		toml := "url = \"http://tgp:3000\"\ntoken = \"x\"\nregistry = \"" + v + "\"\n"
		if err := os.WriteFile(p, []byte(toml), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := LoadConfig(p)
		if (err == nil) != ok {
			t.Errorf("%q: err = %v", v, err)
		}
		if err == nil && c.Registry != strings.ToLower(v) {
			t.Errorf("%q: got %q", v, c.Registry)
		}
	}
}

func TestLoginHint(t *testing.T) {
	for out, want := range map[string]string{
		`Error response from daemon: Get "https://bloxgit.ts.net/v2/": dial tcp: lookup bloxgit.ts.net on 100.100.100.100:53: no such host`: "can't resolve",
		`dial tcp 10.0.0.5:443: connect: connection refused`:                                                   "can't connect",
		`tls: failed to verify certificate: x509: certificate signed by unknown authority`:                     "TLS certificate",
		`exec: "docker": executable file not found in $PATH`:                                                   "no docker CLI",
		`Error response from daemon: ... unauthorized: authentication required`:                                "rejected the job token",
		`Cannot connect to the Docker daemon at unix:///var/run/docker.sock`:                                   "can't reach a Docker daemon",
		`permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock`: "can't reach a Docker daemon",
		`something else`: "see the output above",
	} {
		if got := loginHint("reg", out); !strings.Contains(got, want) {
			t.Errorf("%q: %q", out, got)
		}
	}
}
