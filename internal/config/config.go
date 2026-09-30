// Package config loads the client configuration (JSON file + environment
// overrides) and validates it.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
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

// Config is the full client configuration. ConfigVersion is the schema version
// written into generated files; `config patch` uses it to detect and repair
// older files (adding newly introduced keys) after backing them up.
type Config struct {
	ConfigVersion int                `json:"config_version"`
	Client        ClientConfig       `json:"client"`
	Refresh       RefreshConfig      `json:"refresh"`
	CPA           CPAConfig          `json:"cpa"`
	Volcengine    VolcengineConfig   `json:"volcengine"`
	Workbuddy     WorkbuddyConfig    `json:"workbuddy"`
	Qoder         QoderConfig        `json:"qoder"`
	RefreshCache  RefreshCacheConfig `json:"refreshcache"`
	Reconnect     ReconnectConfig    `json:"reconnect"`
}

// CurrentConfigVersion is the schema version written into generated configs.
const CurrentConfigVersion = 1

// Default returns a configuration with safe defaults (refresh disabled).
func Default() *Config {
	return &Config{
		ConfigVersion: CurrentConfigVersion,
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

// PatchResult reports the outcome of Patch.
type PatchResult struct {
	// Changed reports whether the file was rewritten (missing keys or an older
	// schema version).
	Changed bool
	// AddedKeys are the dotted paths of the keys that were filled in.
	AddedKeys []string
	// BackupPath is the backup file the original content was copied to; empty
	// when nothing was written.
	BackupPath string
	// Path is the patched config file.
	Path string
	// Version is the config_version after patching.
	Version int
}

// Patch repairs an existing config file: it backs the file up, fills in keys
// that are missing relative to the current defaults and stamps the current
// config_version. It never overwrites existing values and refuses to downgrade
// a file written by a newer client.
func Patch(path string) (*PatchResult, error) {
	if path == "" {
		path = "config.json"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败：%w", err)
	}
	raw := map[string]any{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析配置文件失败：%w", err)
	}
	version := configVersionOf(raw)
	if version > CurrentConfigVersion {
		return nil, fmt.Errorf(
			"配置版本 %d 高于当前程序支持的 %d，请升级 quotanoa-client 后再试",
			version, CurrentConfigVersion,
		)
	}
	defaults, err := defaultConfigMap()
	if err != nil {
		return nil, err
	}
	missing := missingDefaults(raw, defaults)
	result := &PatchResult{Path: path, AddedKeys: flattenMissingKeys(missing), Version: version}
	if len(missing) == 0 && version == CurrentConfigVersion {
		return result, nil
	}
	backup, err := backupFile(path)
	if err != nil {
		return nil, err
	}
	merged := mergeMissing(raw, missing)
	merged["config_version"] = CurrentConfigVersion
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, err
	}
	out = append(out, '\n')
	if err := writeFileAtomic(path, out, 0o600); err != nil {
		return nil, err
	}
	result.Changed = true
	result.BackupPath = backup
	result.Version = CurrentConfigVersion
	return result, nil
}

func configVersionOf(raw map[string]any) int {
	switch v := raw["config_version"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return 0
}

// defaultConfigMap is the default config as a generic map, without
// config_version (which is handled separately).
func defaultConfigMap() (map[string]any, error) {
	data, err := json.Marshal(Default())
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	delete(out, "config_version")
	return out, nil
}

// missingDefaults returns the nested structure of keys absent from raw.
func missingDefaults(raw, defaults map[string]any) map[string]any {
	missing := map[string]any{}
	for key, def := range defaults {
		current, ok := raw[key]
		if !ok {
			missing[key] = def
			continue
		}
		currentMap, currentOK := current.(map[string]any)
		defMap, defOK := def.(map[string]any)
		if currentOK && defOK {
			if nested := missingDefaults(currentMap, defMap); len(nested) > 0 {
				missing[key] = nested
			}
		}
	}
	return missing
}

// mergeMissing deep-merges missing into base, returning a new map.
func mergeMissing(base, missing map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(missing))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range missing {
		current, currentOK := merged[key].(map[string]any)
		nested, nestedOK := value.(map[string]any)
		if currentOK && nestedOK {
			merged[key] = mergeMissing(current, nested)
			continue
		}
		merged[key] = value
	}
	return merged
}

// flattenMissingKeys renders a nested missing structure as sorted dotted paths.
func flattenMissingKeys(missing map[string]any) []string {
	keys := []string{}
	for key, value := range missing {
		if nested, ok := value.(map[string]any); ok {
			for _, sub := range flattenMissingKeys(nested) {
				keys = append(keys, key+"."+sub)
			}
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// backupFile copies path next to itself as <stem>_<timestamp>_bak.<ext>,
// appending a counter if needed to avoid overwriting an existing backup.
func backupFile(path string) (string, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if ext == "" {
		ext = ".json"
	}
	stamp := time.Now().Format("20060102-150405")
	target := filepath.Join(dir, fmt.Sprintf("%s_%s_bak%s", stem, stamp, ext))
	for counter := 1; ; counter++ {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			break
		}
		target = filepath.Join(dir, fmt.Sprintf("%s_%s_%d_bak%s", stem, stamp, counter, ext))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取待备份配置失败：%w", err)
	}
	if err := writeFileAtomic(target, data, 0o600); err != nil {
		return "", fmt.Errorf("备份配置失败：%w", err)
	}
	return target, nil
}

// writeFileAtomic writes data to path via a temp file + rename.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".quotanoa-config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// --------------------------------------------------------------------------- #
// 实例/账号条目管理（config add / list / remove）
// --------------------------------------------------------------------------- #

// Section describes where a kind of credential entry lives in the config.
type Section struct {
	Key     string // canonical key
	Parent  string // top-level config section
	ListKey string // array field inside the parent
	Label   string // human-readable label
}

// sections maps a canonical section key to its JSON location.
var sections = map[string]Section{
	"cpa":        {Key: "cpa", Parent: "cpa", ListKey: "instances", Label: "CPA 实例"},
	"volcengine": {Key: "volcengine", Parent: "volcengine", ListKey: "accounts", Label: "火山账号"},
	"workbuddy":  {Key: "workbuddy", Parent: "workbuddy", ListKey: "servers", Label: "WorkBuddy 网关"},
	"qoder":      {Key: "qoder", Parent: "qoder", ListKey: "servers", Label: "Qoder 代理"},
}

// SectionOrder is the display order for `config list`.
var SectionOrder = []string{"cpa", "volcengine", "workbuddy", "qoder"}

var sectionAliases = map[string]string{
	"cpa":  "cpa",
	"volc": "volcengine", "volcengine": "volcengine", "火山": "volcengine", "ark": "volcengine",
	"wb": "workbuddy", "workbuddy": "workbuddy", "work-buddy": "workbuddy",
	"qoder": "qoder", "qd": "qoder",
}

// NormalizeSection resolves a user-supplied section name/alias to its canonical
// key (e.g. "火山" / "volc" -> "volcengine").
func NormalizeSection(value string) (string, bool) {
	key, ok := sectionAliases[strings.ToLower(strings.TrimSpace(value))]
	return key, ok
}

// SectionLabel returns the display label for a canonical section key.
func SectionLabel(value string) string {
	key, ok := NormalizeSection(value)
	if !ok {
		return value
	}
	return sections[key].Label
}

// Mask returns a redacted display form of a secret (for list output).
func Mask(value string) string { return mask(value) }

func readRawConfig(path string) (map[string]any, error) {
	if path == "" {
		path = "config.json"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败：%w", err)
	}
	raw := map[string]any{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析配置文件失败：%w", err)
	}
	return raw, nil
}

// writeRawConfig backs the file up, then atomically writes raw as indented JSON.
func writeRawConfig(path string, raw map[string]any) (string, error) {
	backup, err := backupFile(path)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return "", err
	}
	return backup, nil
}

func sectionList(raw map[string]any, spec Section) []any {
	parent, ok := raw[spec.Parent].(map[string]any)
	if !ok {
		return nil
	}
	list, ok := parent[spec.ListKey].([]any)
	if !ok {
		return nil
	}
	return list
}

func entryName(item any) string {
	if m, ok := item.(map[string]any); ok {
		if name, ok := m["name"].(string); ok {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

// AddEntry appends a credential entry to the given section, rejecting a
// duplicate name. The previous file is backed up before writing.
func AddEntry(path, section, name string, entry map[string]any) (string, error) {
	key, ok := NormalizeSection(section)
	if !ok {
		return "", fmt.Errorf("未知类型：%s", section)
	}
	spec := sections[key]
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("名称（--name）不能为空")
	}
	raw, err := readRawConfig(path)
	if err != nil {
		return "", err
	}
	for _, item := range sectionList(raw, spec) {
		if strings.EqualFold(entryName(item), name) {
			return "", fmt.Errorf("%s 已存在同名条目：%s", spec.Label, name)
		}
	}
	parent, ok := raw[spec.Parent].(map[string]any)
	if !ok {
		parent = map[string]any{}
		raw[spec.Parent] = parent
	}
	list, _ := parent[spec.ListKey].([]any)
	parent[spec.ListKey] = append(list, entry)
	return writeRawConfig(path, raw)
}

// RemoveEntry deletes the entry with the given name from a section. It reports
// whether anything was removed; nothing is written when the name is absent.
func RemoveEntry(path, section, name string) (bool, string, error) {
	key, ok := NormalizeSection(section)
	if !ok {
		return false, "", fmt.Errorf("未知类型：%s", section)
	}
	spec := sections[key]
	name = strings.TrimSpace(name)
	if name == "" {
		return false, "", fmt.Errorf("名称（--name）不能为空")
	}
	raw, err := readRawConfig(path)
	if err != nil {
		return false, "", err
	}
	parent, ok := raw[spec.Parent].(map[string]any)
	if !ok {
		return false, "", nil
	}
	list, _ := parent[spec.ListKey].([]any)
	kept := make([]any, 0, len(list))
	removed := false
	for _, item := range list {
		if !removed && strings.EqualFold(entryName(item), name) {
			removed = true
			continue
		}
		kept = append(kept, item)
	}
	if !removed {
		return false, "", nil
	}
	parent[spec.ListKey] = kept
	backup, err := writeRawConfig(path, raw)
	if err != nil {
		return false, "", err
	}
	return true, backup, nil
}

// ListEntries returns the credential entries of a section (empty when absent).
func ListEntries(path, section string) ([]map[string]any, error) {
	key, ok := NormalizeSection(section)
	if !ok {
		return nil, fmt.Errorf("未知类型：%s", section)
	}
	spec := sections[key]
	raw, err := readRawConfig(path)
	if err != nil {
		return nil, err
	}
	list := sectionList(raw, spec)
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}
