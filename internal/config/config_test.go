package config

import (
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
