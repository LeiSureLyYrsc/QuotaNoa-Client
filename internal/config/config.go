// Package config loads the client configuration (JSON file + environment
// overrides) and validates it.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ClientConfig holds connection/identity settings.
type ClientConfig struct {
	Name      string `json:"name"`
	ServerURL string `json:"server_url"`
	Key       string `json:"key"`
	Protocol  int    `json:"protocol"`
}

// RefreshConfig controls whether the client accepts remote refresh requests.
// Disabled by default; this value is authoritative (the client never trusts
// capability claims from the server).
type RefreshConfig struct {
	Enabled bool `json:"enabled"`
}

// CPAConfig configures the local CLIProxyAPI management endpoint.
type CPAConfig struct {
	BaseURL          string  `json:"base_url"`
	ManagementKey    string  `json:"management_key"`
	Timeout          float64 `json:"timeout"`
	QuotaTimeout     float64 `json:"quota_timeout"`
	QuotaConcurrency int     `json:"quota_concurrency"`
	QuotaCacheTTL    float64 `json:"quota_cache_ttl"`
}

// VolcengineAccount is a Volcengine Ark control-plane credential.
type VolcengineAccount struct {
	Name            string `json:"name"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	Region          string `json:"region"`
}

// VolcengineConfig lists Volcengine accounts.
type VolcengineConfig struct {
	Accounts []VolcengineAccount `json:"accounts"`
}

// WorkbuddyServer is a WorkBuddy2API gateway.
type WorkbuddyServer struct {
	Name     string  `json:"name"`
	BaseURL  string  `json:"base_url"`
	Username string  `json:"username"`
	Password string  `json:"password"`
	APIKey   string  `json:"api_key"`
	Timeout  float64 `json:"timeout"`
}

// WorkbuddyConfig lists WorkBuddy gateways.
type WorkbuddyConfig struct {
	Servers []WorkbuddyServer `json:"servers"`
}

// QoderServer is a Qoder2OAPI proxy.
type QoderServer struct {
	Name    string  `json:"name"`
	BaseURL string  `json:"base_url"`
	APIKey  string  `json:"api_key"`
	Timeout float64 `json:"timeout"`
}

// QoderConfig lists Qoder proxies.
type QoderConfig struct {
	Servers []QoderServer `json:"servers"`
}

// RefreshCacheConfig controls per-channel cache TTLs.
type RefreshCacheConfig struct {
	Default  float64            `json:"default"`
	Channels map[string]float64 `json:"channels"`
}

// ReconnectConfig controls reconnect backoff.
type ReconnectConfig struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// Config is the full client configuration.
type Config struct {
	Client       ClientConfig       `json:"client"`
	Refresh      RefreshConfig      `json:"refresh"`
	CPA          CPAConfig          `json:"cpa"`
	Volcengine   VolcengineConfig   `json:"volcengine"`
	Workbuddy    WorkbuddyConfig    `json:"workbuddy"`
	Qoder        QoderConfig        `json:"qoder"`
	RefreshCache RefreshCacheConfig `json:"refreshcache"`
	Reconnect    ReconnectConfig    `json:"reconnect"`
}

// Default returns a configuration with safe defaults (refresh disabled).
func Default() *Config {
	return &Config{
		Client: ClientConfig{
			Name:      "Home",
			ServerURL: "ws://127.0.0.1:8320/v1/client/ws",
			Protocol:  2,
		},
		Refresh: RefreshConfig{Enabled: false},
		CPA: CPAConfig{
			BaseURL:          "http://127.0.0.1:8317",
			Timeout:          15,
			QuotaTimeout:     25,
			QuotaConcurrency: 4,
			// 0 表示不单独指定，跟随 refreshcache.default。
			QuotaCacheTTL: 0,
		},
		Volcengine: VolcengineConfig{Accounts: []VolcengineAccount{}},
		Workbuddy:  WorkbuddyConfig{Servers: []WorkbuddyServer{}},
		Qoder:      QoderConfig{Servers: []QoderServer{}},
		// 默认 10 分钟：正常查询在此期间命中缓存，--fresh 可强制刷新。
		RefreshCache: RefreshCacheConfig{Default: 600, Channels: map[string]float64{}},
		Reconnect:    ReconnectConfig{Min: 1, Max: 30},
	}
}

// Load reads the JSON config file at path (missing file is allowed), applies
// environment overrides, defaults and validation.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			dec := json.NewDecoder(strings.NewReader(string(data)))
			if err := dec.Decode(cfg); err != nil {
				return nil, fmt.Errorf("解析配置文件失败：%w", err)
			}
		case os.IsNotExist(err):
			// Missing config is tolerated; env overrides may still supply values.
		default:
			return nil, fmt.Errorf("读取配置文件失败：%w", err)
		}
	}
	cfg.applyEnv()
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyEnv() {
	if v := os.Getenv("QUOTANOA_CLIENT_NAME"); v != "" {
		c.Client.Name = v
	}
	if v := os.Getenv("QUOTANOA_SERVER_URL"); v != "" {
		c.Client.ServerURL = v
	}
	if v := os.Getenv("QUOTANOA_CLIENT_KEY"); v != "" {
		c.Client.Key = v
	}
	if v := os.Getenv("QUOTANOA_REFRESH_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			c.Refresh.Enabled = b
		}
	}
	if v := os.Getenv("QUOTANOA_CPA_BASE_URL"); v != "" {
		c.CPA.BaseURL = v
	}
	if v := os.Getenv("QUOTANOA_CPA_MANAGEMENT_KEY"); v != "" {
		c.CPA.ManagementKey = v
	}
}

func (c *Config) normalize() {
	c.Client.Name = strings.TrimSpace(c.Client.Name)
	c.Client.ServerURL = strings.TrimSpace(c.Client.ServerURL)
	c.Client.Key = strings.TrimSpace(c.Client.Key)
	c.CPA.BaseURL = strings.TrimRight(strings.TrimSpace(c.CPA.BaseURL), "/")
	c.CPA.ManagementKey = strings.TrimSpace(c.CPA.ManagementKey)
	if c.Client.Protocol <= 0 {
		c.Client.Protocol = 2
	}
	if c.CPA.Timeout <= 0 {
		c.CPA.Timeout = 15
	}
	if c.CPA.QuotaTimeout <= 0 {
		c.CPA.QuotaTimeout = 25
	}
	if c.CPA.QuotaConcurrency <= 0 {
		c.CPA.QuotaConcurrency = 4
	}
	if c.CPA.QuotaCacheTTL < 0 {
		c.CPA.QuotaCacheTTL = 0
	}
	if c.RefreshCache.Default < 0 {
		c.RefreshCache.Default = 0
	}
	if c.RefreshCache.Channels == nil {
		c.RefreshCache.Channels = map[string]float64{}
	}
	if c.Reconnect.Min <= 0 {
		c.Reconnect.Min = 1
	}
	if c.Reconnect.Max < c.Reconnect.Min {
		c.Reconnect.Max = 30
	}
	for i := range c.Volcengine.Accounts {
		if strings.TrimSpace(c.Volcengine.Accounts[i].Region) == "" {
			c.Volcengine.Accounts[i].Region = "cn-beijing"
		}
	}
	for i := range c.Workbuddy.Servers {
		c.Workbuddy.Servers[i].BaseURL = strings.TrimRight(strings.TrimSpace(c.Workbuddy.Servers[i].BaseURL), "/")
		if c.Workbuddy.Servers[i].Timeout <= 0 {
			c.Workbuddy.Servers[i].Timeout = 30
		}
	}
	for i := range c.Qoder.Servers {
		c.Qoder.Servers[i].BaseURL = strings.TrimRight(strings.TrimSpace(c.Qoder.Servers[i].BaseURL), "/")
		if c.Qoder.Servers[i].BaseURL == "" {
			c.Qoder.Servers[i].BaseURL = "http://127.0.0.1:8000"
		}
		if c.Qoder.Servers[i].Timeout <= 0 {
			c.Qoder.Servers[i].Timeout = 30
		}
	}
}

// Validate checks required fields.
func (c *Config) Validate() error {
	if c.Client.Name == "" {
		return fmt.Errorf("client.name 不能为空")
	}
	if !validName(c.Client.Name) {
		return fmt.Errorf("client.name 非法（1–32 字符，不能含空白或 / \\）")
	}
	if c.Client.ServerURL == "" {
		return fmt.Errorf("client.server_url 不能为空")
	}
	if !strings.HasPrefix(c.Client.ServerURL, "ws://") && !strings.HasPrefix(c.Client.ServerURL, "wss://") {
		return fmt.Errorf("client.server_url 必须以 ws:// 或 wss:// 开头")
	}
	if c.Client.Key == "" {
		return fmt.Errorf("client.key 不能为空")
	}
	return nil
}

func validName(value string) bool {
	r := []rune(strings.TrimSpace(value))
	if len(r) == 0 || len(r) > 32 {
		return false
	}
	if r[0] == '-' {
		return false
	}
	for _, ch := range r {
		if ch == '/' || ch == '\\' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			return false
		}
	}
	return true
}

// Redacted returns a human-readable summary with secrets masked (for
// `config check`).
func (c *Config) Redacted() string {
	channels := []string{}
	if len(c.CPA.BaseURL) > 0 {
		channels = append(channels, "claude/codex/kimi/xai/antigravity/gemini-cli (CPA)")
	}
	if len(c.Volcengine.Accounts) > 0 {
		channels = append(channels, "volcengine")
	}
	if len(c.Workbuddy.Servers) > 0 {
		channels = append(channels, "workbuddy")
	}
	if len(c.Qoder.Servers) > 0 {
		channels = append(channels, "qoder")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "name=%s\n", c.Client.Name)
	fmt.Fprintf(&b, "server_url=%s\n", c.Client.ServerURL)
	fmt.Fprintf(&b, "key=%s\n", mask(c.Client.Key))
	fmt.Fprintf(&b, "refresh.enabled=%v\n", c.Refresh.Enabled)
	fmt.Fprintf(&b, "cpa.base_url=%s management_key=%s\n", c.CPA.BaseURL, mask(c.CPA.ManagementKey))
	fmt.Fprintf(&b, "channels=%s\n", strings.Join(channels, ", "))
	fmt.Fprintf(&b, "cache.default=%gs quota_cache_ttl=%gs\n", c.RefreshCache.Default, c.CPA.QuotaCacheTTL)
	return b.String()
}

func mask(value string) string {
	if value == "" {
		return "（未设置）"
	}
	r := []rune(value)
	if len(r) <= 6 {
		return strings.Repeat("*", len(r))
	}
	return string(r[:3]) + "…" + string(r[len(r)-3:])
}

// SaveDefault writes the default configuration as indented JSON to path.
// It refuses to overwrite an existing file unless force is true.
func SaveDefault(path string, force bool) error {
	if path == "" {
		path = "config.json"
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("文件已存在：%s（加 --force 覆盖）", path)
		}
	}
	data, err := json.MarshalIndent(Default(), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return nil
}
