// Package config loads optional wcaw-specific settings from a JSON file at
// $XDG_CONFIG_HOME/wcaw/config.json (or ~/.config/wcaw/config.json), giving
// users a place to keep tokens and LLM credentials without exporting them in
// their shell.
//
// Like the cache layer, this package is fail-open: a missing file yields an
// empty Config with no error, and malformed JSON returns an empty Config plus
// an error so the caller can log a single stderr line and continue. Env vars
// always take precedence over config values — see EnvOr.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Config holds the wcaw-specific settings that may live in config.json. All
// fields are optional; the zero value is a valid, empty configuration.
type Config struct {
	GitHubToken string     `json:"github_token,omitempty"`
	DGPT        DGPTConfig `json:"dgpt,omitempty"`
}

// DGPTConfig groups the OpenAI-compatible LLM summarizer credentials. APIKey
// and Model are both required to enable the summarizer at runtime; BaseURL is
// an optional endpoint override.
type DGPTConfig struct {
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
}

// DefaultPath returns the path wcaw reads its config from when none is given:
// $XDG_CONFIG_HOME/wcaw/config.json when XDG_CONFIG_HOME is set, otherwise
// ~/.config/wcaw/config.json on every platform (including macOS). This
// mirrors cache.DefaultPath so users only need to learn one convention.
func DefaultPath() (string, error) {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "wcaw", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home: %w", err)
	}
	return filepath.Join(home, ".config", "wcaw", "config.json"), nil
}

// Load reads and parses the config file at DefaultPath. A missing file is not
// an error — it returns an empty Config and nil. A malformed file returns an
// empty Config plus a non-nil error so main can warn the user without
// aborting; this preserves the graceful-degradation contract.
func Load() (Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return Config{}, err
	}
	return LoadFrom(path)
}

// LoadFrom is Load with an explicit path; exposed for tests.
func LoadFrom(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return c, nil
}

// EnvOr returns os.Getenv(envKey) when non-empty, otherwise fallback. This is
// the helper main uses to enforce "env wins over config": an empty env var is
// treated as unset so it doesn't accidentally blank out a configured value.
func EnvOr(envKey, fallback string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return fallback
}
