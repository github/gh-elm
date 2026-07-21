// Package config manages gh-elm's non-secret configuration — the source (GHES)
// and target (GHEC/Proxima) API URLs — and locates gh-elm's config directory.
//
// Secret tokens are NOT stored here; they live behind a creds.Store. Environment
// variables (see the Env* constants) and command flags override stored values at
// resolution time (see the endpoints package).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Environment variable names. gh-elm uses a unified GH_SOURCE_*/GH_TARGET_*
// scheme for the source (GHES) and target (GHEC/Proxima) host and token, so
// scripts and CI can override the stored config without gh elm configure.
const (
	EnvSourceURL   = "GH_SOURCE_HOST"
	// #nosec G101 -- This is the name of an environment variable, not a credential.
	EnvSourceToken = "GH_SOURCE_TOKEN"
	EnvTargetURL   = "GH_TARGET_HOST"
	// #nosec G101 -- This is the name of an environment variable, not a credential.
	EnvTargetToken = "GH_TARGET_TOKEN"
)

// Config is gh-elm's persisted, non-secret configuration.
type Config struct {
	SourceURL string `json:"source_url,omitempty"`
	TargetURL string `json:"target_url,omitempty"`
}

// Dir returns gh-elm's config directory, honoring GH_ELM_CONFIG_DIR, then
// GH_CONFIG_DIR, then the OS/XDG default (e.g. ~/.config/gh-elm). The directory
// is not created.
func Dir() (string, error) {
	if d := os.Getenv("GH_ELM_CONFIG_DIR"); d != "" {
		return d, nil
	}
	if d := os.Getenv("GH_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "gh-elm"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config dir: %w", err)
	}
	return filepath.Join(base, "gh-elm"), nil
}

// Path returns the full path to the config file.
func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// Load reads the persisted config. A missing file yields an empty Config.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", p, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p, err)
	}
	return &c, nil
}

// Save writes the config, creating the directory as needed. The file is written
// 0600 (it holds no secrets today, but keeps parity with the credentials file).
func (c *Config) Save() error {
	d, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", d, err)
	}
	p := filepath.Join(d, "config.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", p, err)
	}
	return nil
}
