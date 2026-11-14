package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsDeriveFromDataDir(t *testing.T) {
	t.Setenv("TGP_DATA_DIR", "/srv/tgp")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.DSN != "/srv/tgp/tgp.db" {
		t.Errorf("DSN = %q", cfg.Database.DSN)
	}
	if cfg.Repos.Root != "/srv/tgp/repos" {
		t.Errorf("repos root = %q", cfg.Repos.Root)
	}
	if cfg.Storage.Local.Path != "/srv/tgp/storage" {
		t.Errorf("storage path = %q", cfg.Storage.Local.Path)
	}
	if cfg.Security.MasterKeyFile != "/srv/tgp/master.key" {
		t.Errorf("master key = %q", cfg.Security.MasterKeyFile)
	}
}

func TestLoadFileAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tgp.toml")
	err := os.WriteFile(p, []byte(`
data_dir = "/var/lib/tgp"
[server]
listen = ":8080"
base_url = "https://git.example.com/"
shutdown_timeout = "5s"
[database]
driver = "postgres"
dsn = "postgres://localhost/tgp"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TGP_LISTEN", ":9999")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != ":9999" {
		t.Errorf("env override not applied: %q", cfg.Server.Listen)
	}
	if cfg.Server.BaseURL != "https://git.example.com" {
		t.Errorf("base url not trimmed: %q", cfg.Server.BaseURL)
	}
	if cfg.Server.ShutdownTimeout.Seconds() != 5 {
		t.Errorf("shutdown timeout = %v", cfg.Server.ShutdownTimeout)
	}
	if !cfg.UseSecureCookies() {
		t.Error("https base url should imply secure cookies")
	}
}

func TestUnknownKeysRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tgp.toml")
	if err := os.WriteFile(p, []byte("[server]\nlisten_addr = \":1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "listen_addr") {
		t.Fatalf("expected unknown key error, got %v", err)
	}
}

func TestValidation(t *testing.T) {
	cases := map[string]map[string]string{
		"bad driver":   {"TGP_DB_DRIVER": "mysql"},
		"pg no dsn":    {"TGP_DB_DRIVER": "postgres"},
		"bad base url": {"TGP_BASE_URL": "git.example.com"},
		"s3 no bucket": {"TGP_STORAGE_TYPE": "s3"},
		"bad level":    {"TGP_LOG_LEVEL": "loud"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			for k, v := range env {
				t.Setenv(k, v)
			}
			if _, err := Load(""); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Fatal("expected error for missing file")
	}
}
