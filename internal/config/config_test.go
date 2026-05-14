package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFrom_MissingFile(t *testing.T) {
	c, err := LoadFrom(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if c != (Config{}) {
		t.Fatalf("missing file should yield empty Config, got %+v", c)
	}
}

func TestLoadFrom_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{
	  "github_token": "ghp_test",
	  "dgpt": {
	    "api_key": "sk-test",
	    "model": "gpt-4o-mini",
	    "base_url": "https://example.test/v1"
	  }
	}`
	if err := writeFile(path, body); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.GitHubToken != "ghp_test" {
		t.Errorf("GitHubToken = %q, want %q", c.GitHubToken, "ghp_test")
	}
	if c.DGPT.APIKey != "sk-test" {
		t.Errorf("DGPT.APIKey = %q, want %q", c.DGPT.APIKey, "sk-test")
	}
	if c.DGPT.Model != "gpt-4o-mini" {
		t.Errorf("DGPT.Model = %q, want %q", c.DGPT.Model, "gpt-4o-mini")
	}
	if c.DGPT.BaseURL != "https://example.test/v1" {
		t.Errorf("DGPT.BaseURL = %q, want %q", c.DGPT.BaseURL, "https://example.test/v1")
	}
}

func TestLoadFrom_MalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeFile(path, "{"); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFrom(path)
	if err == nil {
		t.Fatal("malformed JSON should return an error")
	}
	if c != (Config{}) {
		t.Fatalf("malformed JSON should yield empty Config, got %+v", c)
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("WCAW_TEST_KEY", "from-env")
	if got := EnvOr("WCAW_TEST_KEY", "from-config"); got != "from-env" {
		t.Errorf("env-set: got %q, want %q", got, "from-env")
	}
	t.Setenv("WCAW_TEST_KEY", "")
	if got := EnvOr("WCAW_TEST_KEY", "from-config"); got != "from-config" {
		t.Errorf("env-empty: got %q, want %q", got, "from-config")
	}
}

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}
