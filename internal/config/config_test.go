package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "mirrors")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	full := content + "\n[[repo_source]]\ntype=\"directory\"\npath=\"" + repoDir + "\"\n"
	if err := os.WriteFile(path, []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaults(t *testing.T) {
	path := writeTemp(t, "")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Bind != "127.0.0.1:8888" {
		t.Errorf("default bind = %q", cfg.Server.Bind)
	}
	if cfg.Renderers.MaxRenderBytes != 1<<20 {
		t.Errorf("default max render bytes = %d", cfg.Renderers.MaxRenderBytes)
	}
	if cfg.Sources[0].Glob != "*.git" {
		t.Errorf("default glob = %q", cfg.Sources[0].Glob)
	}
}

func TestAuthEnabledByDefault(t *testing.T) {
	cfg, err := Load(writeTemp(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Auth.Enabled {
		t.Error("auth should be enabled by default (secure by default)")
	}
}

func TestAuthCanBeDisabled(t *testing.T) {
	cfg, err := Load(writeTemp(t, "[auth]\nenabled=false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Enabled {
		t.Error("auth should be disabled when explicitly set to false")
	}
}

func TestValidateRejectsBadBind(t *testing.T) {
	path := writeTemp(t, "[server]\nbind=\"noport\"\n")
	if _, err := Load(path); err == nil {
		t.Error("expected error for bad bind")
	}
}

func TestValidateRejectsUnknownProvider(t *testing.T) {
	path := writeTemp(t, "[metadata]\nprovider=\"bogus\"\n")
	if _, err := Load(path); err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestValidateRequiresSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.toml")
	os.WriteFile(path, []byte("[server]\nbind=\"127.0.0.1:9\"\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Error("expected error when no repo_source configured")
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	path := writeTemp(t, "[server]\nbananas=true\n")
	if _, err := Load(path); err == nil {
		t.Error("expected error for unknown key")
	}
}
