package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/zalando/go-keyring"
)

const keyringService = "tgp"

// Config is the CLI's hosts file (hosts.toml in the config dir).
type Config struct {
	DefaultHost string                 `toml:"default_host,omitempty"`
	Hosts       map[string]*HostConfig `toml:"hosts,omitempty"`

	path string
}

// HostConfig holds the login for one server. Host keys are "host[:port]".
type HostConfig struct {
	URL  string `toml:"url"`
	User string `toml:"user"`
	// Token is set only when it couldn't be (or wasn't allowed to be)
	// stored in the OS keyring.
	Token string `toml:"token,omitempty"`
	// TokenID is the server-side id of a token `tgp auth login` created, so
	// logout can revoke it. Zero for tokens supplied with --with-token.
	TokenID int64 `toml:"token_id,omitempty"`

	token       string // resolved token
	tokenSource string // "keyring", "file" or "env"
}

func configDir() (string, error) {
	if d := os.Getenv("TGP_CONFIG_DIR"); d != "" {
		return d, nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("can't find a config directory (set TGP_CONFIG_DIR): %w", err)
	}
	return filepath.Join(d, "tgp"), nil
}

func loadConfig() (*Config, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	c := &Config{path: filepath.Join(dir, "hosts.toml"), Hosts: map[string]*HostConfig{}}
	data, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("%s: %w", c.path, err)
	}
	if c.Hosts == nil {
		c.Hosts = map[string]*HostConfig{}
	}
	return c, nil
}

func (c *Config) save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// hostNames returns configured host keys, sorted.
func (c *Config) hostNames() []string {
	names := make([]string, 0, len(c.Hosts))
	for h := range c.Hosts {
		names = append(names, h)
	}
	sort.Strings(names)
	return names
}

// normalizeHostURL turns user input ("git.example.com", "http://localhost:3000/")
// into a base URL and its host key.
func normalizeHostURL(in string) (base, key string, err error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", "", usageErr("empty host")
	}
	if !strings.Contains(in, "://") {
		in = "https://" + in
	}
	u, err := url.Parse(in)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", usageErr("invalid host %q: use a host name or an http(s) URL", in)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return u.String(), strings.ToLower(u.Host), nil
}

// Keyring access is bounded by a timeout because some keyring daemons block
// waiting for a GUI unlock that will never come in a headless session.

func keyringDisabled() bool {
	v := os.Getenv("TGP_NO_KEYRING")
	return v != "" && v != "0"
}

func withTimeout[T any](f func() (T, error)) (T, error) {
	type res struct {
		v   T
		err error
	}
	ch := make(chan res, 1)
	go func() {
		v, err := f()
		ch <- res{v, err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(3 * time.Second):
		var zero T
		return zero, errors.New("timed out talking to the OS keyring")
	}
}

func keyringGet(host string) (string, error) {
	return withTimeout(func() (string, error) { return keyring.Get(keyringService, host) })
}

func keyringSet(host, token string) error {
	_, err := withTimeout(func() (struct{}, error) { return struct{}{}, keyring.Set(keyringService, host, token) })
	return err
}

func keyringDelete(host string) {
	_, _ = withTimeout(func() (struct{}, error) { return struct{}{}, keyring.Delete(keyringService, host) })
}

// storeLogin saves a login, putting the token in the keyring when possible.
// It returns where the token went ("keyring" or "file").
func (c *Config) storeLogin(key string, h *HostConfig, token string, insecure bool) (string, error) {
	source := "file"
	h.Token = token
	if !insecure && !keyringDisabled() {
		if err := keyringSet(key, token); err == nil {
			h.Token = ""
			source = "keyring"
		}
	}
	if source == "file" {
		keyringDelete(key)
	}
	c.Hosts[key] = h
	if c.DefaultHost == "" || len(c.Hosts) == 1 {
		c.DefaultHost = key
	}
	return source, c.save()
}

func (c *Config) removeLogin(key string) error {
	keyringDelete(key)
	delete(c.Hosts, key)
	if c.DefaultHost == key {
		c.DefaultHost = ""
		if names := c.hostNames(); len(names) > 0 {
			c.DefaultHost = names[0]
		}
	}
	return c.save()
}

// resolveToken fills in h.token and h.tokenSource.
func (h *HostConfig) resolveToken(key string) {
	if h.Token != "" {
		h.token, h.tokenSource = h.Token, "file"
		return
	}
	if keyringDisabled() {
		return
	}
	if t, err := keyringGet(key); err == nil && t != "" {
		h.token, h.tokenSource = t, "keyring"
	}
}
