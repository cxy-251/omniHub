package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.Port != 8998 {
		t.Errorf("expected port 8998, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected host 0.0.0.0, got %s", cfg.Server.Host)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid, got: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for port 0, got nil")
	}

	cfg = DefaultConfig()
	cfg.Server.Host = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty host, got nil")
	}
}

func TestConfigSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	cfg := DefaultConfig()
	cfg.Server.Port = 9000
	cfg.Libraries = []string{"/test/lib1", "/test/lib2"}

	if err := Save(cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.Server.Port != 9000 {
		t.Errorf("expected port 9000, got %d", loaded.Server.Port)
	}
	if len(loaded.Libraries) != 2 {
		t.Errorf("expected 2 libraries, got %d", len(loaded.Libraries))
	}

	expectedPath := filepath.Join(tempDir, "omnihub", "config.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected config file at %s, but not found", expectedPath)
	}
}
