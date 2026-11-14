// Package config loads tgp-server configuration from a TOML file, with
// environment variable overrides (TGP_*) applied on top.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Config is the full server configuration.
type Config struct {
	// DataDir is the root for everything tgp stores on disk unless a more
	// specific path is configured.
	DataDir string `toml:"data_dir"`

	Server   ServerConfig   `toml:"server"`
	Database DatabaseConfig `toml:"database"`
	Repos    ReposConfig    `toml:"repos"`
	Storage  StorageConfig  `toml:"storage"`
	Security SecurityConfig `toml:"security"`
	Webhooks WebhooksConfig `toml:"webhooks"`
	Log      LogConfig      `toml:"log"`
}

// WebhooksConfig controls outgoing webhook deliveries.
type WebhooksConfig struct {
	// BlockPrivateNetworks refuses deliveries to loopback and private
	// addresses (RFC 1918, ULA, 100.64/10). Default true; set false for
	// integrations on internal networks. Link-local addresses (cloud
	// metadata services) are always refused.
	BlockPrivateNetworks *bool `toml:"block_private_networks"`
	// Timeout per delivery attempt. Default 10s.
	Timeout Duration `toml:"timeout"`
	// Workers is how many deliveries run at once per server. Default 4.
	Workers int `toml:"workers"`
}

type ServerConfig struct {
	// Listen is the HTTP listen address, e.g. ":3000".
	Listen string `toml:"listen"`
	// BaseURL is the public URL users reach the server on. It can also be
	// set in the setup wizard; the config value wins when both are present.
	BaseURL string `toml:"base_url"`
	// TrustProxy makes the server honour X-Forwarded-For / X-Forwarded-Proto
	// from a reverse proxy.
	TrustProxy      bool     `toml:"trust_proxy"`
	ShutdownTimeout Duration `toml:"shutdown_timeout"`
}

type DatabaseConfig struct {
	// Driver is "sqlite" or "postgres".
	Driver string `toml:"driver"`
	// DSN is the connection string. For sqlite it is a file path; for
	// postgres a URL like postgres://user:pass@host:5432/tgp?sslmode=disable.
	DSN          string `toml:"dsn"`
	MaxOpenConns int    `toml:"max_open_conns"`
}

type ReposConfig struct {
	// Root is the directory holding bare repositories.
	Root string `toml:"root"`
	// GitBinary is the git executable to use.
	GitBinary string `toml:"git_binary"`
	// MaxConcurrentGit caps simultaneous clone/fetch/push operations;
	// extra requests wait up to 30s, then get 503. 0 = 2 × CPU cores.
	MaxConcurrentGit int `toml:"max_concurrent_git"`
	// MaxPushBytes caps the size of a single push. Default 2 GiB.
	MaxPushBytes int64 `toml:"max_push_bytes"`
}

type StorageConfig struct {
	// Type is "local" or "s3".
	Type  string             `toml:"type"`
	Local LocalStorageConfig `toml:"local"`
	S3    S3StorageConfig    `toml:"s3"`
}

type LocalStorageConfig struct {
	Path string `toml:"path"`
}

type S3StorageConfig struct {
	Endpoint     string `toml:"endpoint"`
	Region       string `toml:"region"`
	Bucket       string `toml:"bucket"`
	Prefix       string `toml:"prefix"`
	AccessKey    string `toml:"access_key"`
	SecretKey    string `toml:"secret_key"`
	UsePathStyle bool   `toml:"use_path_style"`
	Insecure     bool   `toml:"insecure"`
}

type SecurityConfig struct {
	// MasterKeyFile holds the 32-byte key used to encrypt CI secrets and
	// TOTP seeds at rest. It is generated on first start if missing.
	MasterKeyFile string `toml:"master_key_file"`
	// SecureCookies forces the Secure flag on cookies. Defaults to true when
	// the base URL is https.
	SecureCookies *bool `toml:"secure_cookies"`
}

type LogConfig struct {
	// Level is debug, info, warn or error.
	Level string `toml:"level"`
	// Format is "text" or "json".
	Format string `toml:"format"`
}

// Duration wraps time.Duration so it can be written as "30s" in TOML.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// Default returns a configuration with defaults filled in relative to dataDir.
func Default() *Config {
	return &Config{
		DataDir: "./data",
		Server: ServerConfig{
			Listen:          ":3000",
			ShutdownTimeout: Duration{30 * time.Second},
		},
		Database: DatabaseConfig{Driver: "sqlite"},
		Repos:    ReposConfig{GitBinary: "git", MaxPushBytes: 2 << 30},
		Storage:  StorageConfig{Type: "local"},
		Webhooks: WebhooksConfig{Timeout: Duration{10 * time.Second}, Workers: 4},
		Log:      LogConfig{Level: "info", Format: "text"},
	}
}

// Load reads the config file at path (if non-empty and present), applies
// environment overrides, fills derived defaults and validates the result.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			dec := toml.NewDecoder(strings.NewReader(string(b)))
			dec.DisallowUnknownFields()
			if err := dec.Decode(cfg); err != nil {
				var sme *toml.StrictMissingError
				if errors.As(err, &sme) {
					return nil, fmt.Errorf("config %s: %s", path, sme.String())
				}
				return nil, fmt.Errorf("config %s: %w", path, err)
			}
		case errors.Is(err, os.ErrNotExist):
			return nil, fmt.Errorf("config file %s does not exist", path)
		default:
			return nil, fmt.Errorf("read config: %w", err)
		}
	}
	if err := applyEnv(cfg, os.LookupEnv); err != nil {
		return nil, err
	}
	cfg.fillDerived()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// fillDerived sets paths that default to locations under DataDir.
func (c *Config) fillDerived() {
	c.DataDir = filepath.Clean(c.DataDir)
	if c.Database.DSN == "" && c.Database.Driver == "sqlite" {
		c.Database.DSN = filepath.Join(c.DataDir, "tgp.db")
	}
	if c.Repos.Root == "" {
		c.Repos.Root = filepath.Join(c.DataDir, "repos")
	}
	if c.Storage.Local.Path == "" {
		c.Storage.Local.Path = filepath.Join(c.DataDir, "storage")
	}
	if c.Security.MasterKeyFile == "" {
		c.Security.MasterKeyFile = filepath.Join(c.DataDir, "master.key")
	}
	c.Server.BaseURL = strings.TrimRight(c.Server.BaseURL, "/")
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	var errs []error
	switch c.Database.Driver {
	case "sqlite", "postgres":
	default:
		errs = append(errs, fmt.Errorf("database.driver must be sqlite or postgres, got %q", c.Database.Driver))
	}
	if c.Database.Driver == "postgres" && c.Database.DSN == "" {
		errs = append(errs, errors.New("database.dsn is required for postgres"))
	}
	switch c.Storage.Type {
	case "local":
	case "s3":
		if c.Storage.S3.Bucket == "" {
			errs = append(errs, errors.New("storage.s3.bucket is required for s3 storage"))
		}
	default:
		errs = append(errs, fmt.Errorf("storage.type must be local or s3, got %q", c.Storage.Type))
	}
	if c.Server.BaseURL != "" {
		u, err := url.Parse(c.Server.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("server.base_url must be an absolute http(s) URL, got %q", c.Server.BaseURL))
		}
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("log.level must be debug, info, warn or error, got %q", c.Log.Level))
	}
	switch c.Log.Format {
	case "text", "json":
	default:
		errs = append(errs, fmt.Errorf("log.format must be text or json, got %q", c.Log.Format))
	}
	return errors.Join(errs...)
}

// UseSecureCookies reports whether cookies should carry the Secure flag.
func (c *Config) UseSecureCookies() bool {
	if c.Security.SecureCookies != nil {
		return *c.Security.SecureCookies
	}
	return strings.HasPrefix(c.Server.BaseURL, "https://")
}

// WebhooksBlockPrivate reports whether webhook deliveries to loopback and
// private networks are refused (the default).
func (c *Config) WebhooksBlockPrivate() bool {
	return c.Webhooks.BlockPrivateNetworks == nil || *c.Webhooks.BlockPrivateNetworks
}

// envBinding maps an environment variable to a config field setter.
type envBinding struct {
	name string
	set  func(c *Config, v string) error
}

func str(f func(c *Config) *string) func(*Config, string) error {
	return func(c *Config, v string) error { *f(c) = v; return nil }
}

func boolean(f func(c *Config) *bool) func(*Config, string) error {
	return func(c *Config, v string) error {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		*f(c) = b
		return nil
	}
}

func optBoolean(f func(c *Config) **bool) func(*Config, string) error {
	return func(c *Config, v string) error {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return err
		}
		*f(c) = &b
		return nil
	}
}

var envBindings = []envBinding{
	{"TGP_DATA_DIR", str(func(c *Config) *string { return &c.DataDir })},
	{"TGP_LISTEN", str(func(c *Config) *string { return &c.Server.Listen })},
	{"TGP_BASE_URL", str(func(c *Config) *string { return &c.Server.BaseURL })},
	{"TGP_TRUST_PROXY", boolean(func(c *Config) *bool { return &c.Server.TrustProxy })},
	{"TGP_DB_DRIVER", str(func(c *Config) *string { return &c.Database.Driver })},
	{"TGP_DB_DSN", str(func(c *Config) *string { return &c.Database.DSN })},
	{"TGP_REPOS_ROOT", str(func(c *Config) *string { return &c.Repos.Root })},
	{"TGP_GIT_BINARY", str(func(c *Config) *string { return &c.Repos.GitBinary })},
	{"TGP_STORAGE_TYPE", str(func(c *Config) *string { return &c.Storage.Type })},
	{"TGP_STORAGE_PATH", str(func(c *Config) *string { return &c.Storage.Local.Path })},
	{"TGP_S3_ENDPOINT", str(func(c *Config) *string { return &c.Storage.S3.Endpoint })},
	{"TGP_S3_REGION", str(func(c *Config) *string { return &c.Storage.S3.Region })},
	{"TGP_S3_BUCKET", str(func(c *Config) *string { return &c.Storage.S3.Bucket })},
	{"TGP_S3_PREFIX", str(func(c *Config) *string { return &c.Storage.S3.Prefix })},
	{"TGP_S3_ACCESS_KEY", str(func(c *Config) *string { return &c.Storage.S3.AccessKey })},
	{"TGP_S3_SECRET_KEY", str(func(c *Config) *string { return &c.Storage.S3.SecretKey })},
	{"TGP_MASTER_KEY_FILE", str(func(c *Config) *string { return &c.Security.MasterKeyFile })},
	{"TGP_WEBHOOKS_BLOCK_PRIVATE", optBoolean(func(c *Config) **bool { return &c.Webhooks.BlockPrivateNetworks })},
	{"TGP_LOG_LEVEL", str(func(c *Config) *string { return &c.Log.Level })},
	{"TGP_LOG_FORMAT", str(func(c *Config) *string { return &c.Log.Format })},
}

func applyEnv(c *Config, lookup func(string) (string, bool)) error {
	for _, b := range envBindings {
		if v, ok := lookup(b.name); ok {
			if err := b.set(c, v); err != nil {
				return fmt.Errorf("env %s: %w", b.name, err)
			}
		}
	}
	return nil
}
