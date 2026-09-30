package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFileAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	content := `{"client":{"name":"Office","server_url":"ws://example/v1/client/ws","key":"fromfile"},"refresh":{"enabled":true}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QUOTANOA_CPA_MANAGEMENT_KEY", "secret")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Client.Name != "Office" || cfg.Client.Key != "fromfile" {
		t.Fatalf("unexpected client: %+v", cfg.Client)
	}
	if !cfg.Refresh.Enabled {
		t.Fatal("expected refresh enabled")
	}
	if cfg.CPA.ManagementKey != "secret" {
		t.Fatal("expected env override for management key")
	}
	if cfg.CPA.Timeout <= 0 || cfg.Reconnect.Min <= 0 {
		t.Fatal("expected defaults applied")
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("QUOTANOA_CLIENT_NAME", "EnvName")
	t.Setenv("QUOTANOA_CLIENT_KEY", "k")
	t.Setenv("QUOTANOA_SERVER_URL", "wss://host/v1/client/ws")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Client.Name != "EnvName" || cfg.Client.Key != "k" {
		t.Fatalf("env override failed: %+v", cfg.Client)
	}
}

func TestValidateRequiresKey(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error without key")
	}
	cfg.Client.Key = "k"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultCacheTTL(t *testing.T) {
	cfg := Default()
	if cfg.RefreshCache.Default != 600 {
		t.Fatalf("expected 10-minute default cache, got %g", cfg.RefreshCache.Default)
	}
	if cfg.CPA.QuotaCacheTTL != 0 {
		t.Fatalf("expected cpa.quota_cache_ttl to follow refreshcache (0), got %g", cfg.CPA.QuotaCacheTTL)
	}
}

func TestSaveDefaultWritesVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := SaveDefault(path, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string]any{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["config_version"] != float64(CurrentConfigVersion) {
		t.Fatalf("expected config_version %d, got %v", CurrentConfigVersion, raw["config_version"])
	}
}

func TestPatchRepairsLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	legacy := `{"client":{"name":"Home","server_url":"ws://x/v1/client/ws","key":"k"}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Patch(path)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected legacy config to be patched")
	}
	if result.Version != CurrentConfigVersion {
		t.Fatalf("expected version %d, got %d", CurrentConfigVersion, result.Version)
	}
	if result.BackupPath == "" {
		t.Fatal("expected a backup path")
	}
	if _, err := os.Stat(result.BackupPath); err != nil {
		t.Fatalf("backup file missing: %v", err)
	}
	if len(result.AddedKeys) == 0 {
		t.Fatal("expected reported added keys")
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ConfigVersion != CurrentConfigVersion {
		t.Fatalf("expected patched version, got %d", loaded.ConfigVersion)
	}
	if loaded.Client.Name != "Home" || loaded.Client.Key != "k" {
		t.Fatalf("existing values must be preserved: %+v", loaded.Client)
	}
	if loaded.CPA.Timeout <= 0 {
		t.Fatal("expected default cpa.timeout to be filled in")
	}
	// Idempotent: a second patch is a no-op.
	second, err := Patch(path)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatal("second patch must be a no-op")
	}
}

func TestPatchRefusesNewerVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	newer := `{"config_version":999,"client":{"name":"Home","server_url":"ws://x","key":"k"}}`
	if err := os.WriteFile(path, []byte(newer), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Patch(path); err == nil {
		t.Fatal("expected refusal for a newer config_version")
	}
}
