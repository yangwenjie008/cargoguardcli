package main

import (
	"os"
	"testing"
)

// ==================== Config Tests ====================

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Verify app defaults
	if cfg.App.Name != "cargoguardcli" {
		t.Errorf("App.Name = %s, want cargoguardcli", cfg.App.Name)
	}
	if cfg.App.Version != "1.0.0" {
		t.Errorf("App.Version = %s, want 1.0.0", cfg.App.Version)
	}
	if cfg.App.Env != "dev" {
		t.Errorf("App.Env = %s, want dev", cfg.App.Env)
	}

	// Verify log defaults
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %s, want info", cfg.Log.Level)
	}

	// Verify scan defaults
	if cfg.Scan.DefaultPath != "." {
		t.Errorf("Scan.DefaultPath = %s, want .", cfg.Scan.DefaultPath)
	}
	if cfg.Scan.Format != "table" {
		t.Errorf("Scan.Format = %s, want table", cfg.Scan.Format)
	}

	// Verify guard defaults
	if cfg.Guard.Interval != 30 {
		t.Errorf("Guard.Interval = %d, want 30", cfg.Guard.Interval)
	}
	if !cfg.Guard.Notify {
		t.Error("Guard.Notify should be true by default")
	}

	// Verify report defaults
	if cfg.Report.DefaultType != "summary" {
		t.Errorf("Report.DefaultType = %s, want summary", cfg.Report.DefaultType)
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	// Should return default config when file not found
	cfg, err := LoadConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Errorf("LoadConfig() error = %v, want nil", err)
	}
	if cfg == nil {
		t.Error("LoadConfig() returned nil config")
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	// Create temp file
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Create and save config
	cfg := &Config{
		App: AppConfig{
			Name:    "test-app",
			Version: "2.0.0",
			Env:     "test",
		},
		Log: LogConfig{
			Level: "debug",
		},
		Scan: ScanConfig{
			Format: "json",
		},
	}

	err = SaveConfig(cfg, tmpPath)
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	// Load and verify
	loadedCfg, err := LoadConfig(tmpPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if loadedCfg.App.Name != "test-app" {
		t.Errorf("App.Name = %s, want test-app", loadedCfg.App.Name)
	}
	if loadedCfg.App.Version != "2.0.0" {
		t.Errorf("App.Version = %s, want 2.0.0", loadedCfg.App.Version)
	}
	if loadedCfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %s, want debug", loadedCfg.Log.Level)
	}
	if loadedCfg.Scan.Format != "json" {
		t.Errorf("Scan.Format = %s, want json", loadedCfg.Scan.Format)
	}
}

func TestGetConfigPath(t *testing.T) {
	// Test environment variable override
	os.Setenv("CARGOGUARDCLI_CONFIG", "/custom/path/config.yaml")
	defer os.Unsetenv("CARGOGUARDCLI_CONFIG")

	path := GetConfigPath()
	if path != "/custom/path/config.yaml" {
		t.Errorf("GetConfigPath() = %s, want /custom/path/config.yaml", path)
	}
}

func TestGetConfigPathDefault(t *testing.T) {
	// When env var not set, should return default
	os.Unsetenv("CARGOGUARDCLI_CONFIG")
	path := GetConfigPath()
	// Should return either ~/.cargoguardcli/config.yaml or ./config.yaml
	if path == "" {
		t.Error("GetConfigPath() returned empty string")
	}
}

func TestScanConfigExcludeDirs(t *testing.T) {
	cfg := DefaultConfig()

	expectedDirs := []string{"target", "node_modules", ".git", ".svn"}
	if len(cfg.Scan.ExcludeDirs) != len(expectedDirs) {
		t.Errorf("Scan.ExcludeDirs length = %d, want %d", len(cfg.Scan.ExcludeDirs), len(expectedDirs))
	}

	for i, dir := range cfg.Scan.ExcludeDirs {
		if dir != expectedDirs[i] {
			t.Errorf("Scan.ExcludeDirs[%d] = %s, want %s", i, dir, expectedDirs[i])
		}
	}
}

func TestGuardConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Guard.Interval != 30 {
		t.Errorf("Guard.Interval = %d, want 30", cfg.Guard.Interval)
	}
	if !cfg.Guard.Notify {
		t.Error("Guard.Notify should default to true")
	}
	if cfg.Guard.AutoRestart {
		t.Error("Guard.AutoRestart should default to false")
	}
}

func TestTelemetryConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Telemetry.Enabled {
		t.Error("Telemetry.Enabled should default to true")
	}
	if cfg.Telemetry.Env != "dev" {
		t.Errorf("Telemetry.Env = %s, want dev", cfg.Telemetry.Env)
	}
}
