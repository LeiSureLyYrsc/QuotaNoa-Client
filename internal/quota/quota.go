// Package quota implements the CPA (CLIProxyAPI) quota query engine and the
// controlled Codex reset-credit refresh.
package quota

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/config"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/management"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

// CPAChannels are the channels the local CPA can answer.
var CPAChannels = []string{"claude", "codex", "antigravity", "kimi", "xai", "gemini-cli"}

// LocalChannels are the channels served by the local collectors.
var LocalChannels = []string{"volcengine", "workbuddy", "qoder"}

var platformAliases = map[string]string{
	"anthropic": "claude", "claude": "claude",
	"codex": "codex", "gpt": "codex", "openai": "codex",
	"antigravity": "antigravity", "反重力": "antigravity", "agy": "antigravity",
	"kimi": "kimi",
	"xai":  "xai", "x-ai": "xai", "grok": "xai",
	"gemini-cli": "gemini-cli", "gemini": "gemini-cli",
	"volcengine": "volcengine", "volc": "volcengine", "火山": "volcengine", "ark": "volcengine",
	"workbuddy": "workbuddy", "work-buddy": "workbuddy", "wb": "workbuddy",
	"qoder": "qoder", "qd": "qoder",
}

// NormalizePlatform maps an alias to a canonical channel name ("" if unknown).
func NormalizePlatform(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return ""
	}
	if canonical, ok := platformAliases[strings.ToLower(strings.ReplaceAll(raw, "_", "-"))]; ok {
		return canonical
	}
	return platformAliases[raw]
}

// IsCPAChannel reports whether platform is a CPA provider channel.
func IsCPAChannel(platform string) bool {
	for _, item := range CPAChannels {
		if item == platform {
			return true
		}
	}
	return false
}

// IsLocalChannel reports whether platform is a local channel.
func IsLocalChannel(platform string) bool {
	for _, item := range LocalChannels {
		if item == platform {
			return true
		}
	}
	return false
}

// PlatformOf infers the canonical channel from a CPA credential file.
func PlatformOf(file map[string]any) string {
	raw := str(file["provider"])
	if raw == "" {
		raw = str(file["type"])
	}
	canonical := NormalizePlatform(raw)
	if canonical == "" {
		return "other"
	}
	return canonical
}

// Result is a CPA collection outcome.
type Result struct {
	Accounts []model.AccountQuota
	Cached   bool
}

// Collector queries the local CLIProxyAPI.
type Collector struct {
	cfg   *config.Config
	mgmt  *management.Client
	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	expires  time.Time
	accounts []model.AccountQuota
}

// New builds a CPA collector.
func New(cfg *config.Config) *Collector {
	return &Collector{
		cfg:   cfg,
		mgmt:  management.New(cfg.CPA.BaseURL, cfg.CPA.ManagementKey, cfg.CPA.Timeout),
		cache: map[string]cacheEntry{},
	}
}

// ClearCache drops all cached quota results.
func (c *Collector) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = map[string]cacheEntry{}
}

func (c *Collector) ttl(platform string) float64 {
	if platform != "" {
		if v, ok := c.cfg.RefreshCache.Channels[platform]; ok {
			return v
		}
	}
	if c.cfg.CPA.QuotaCacheTTL > 0 {
		return c.cfg.CPA.QuotaCacheTTL
	}
	return c.cfg.RefreshCache.Default
}

func (c *Collector) getCached(key string) ([]model.AccountQuota, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok || time.Now().After(entry.expires) {
		delete(c.cache, key)
		return nil, false
	}
	return entry.accounts, true
}

func (c *Collector) setCached(key string, accounts []model.AccountQuota, ttl float64) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = cacheEntry{expires: time.Now().Add(time.Duration(ttl * float64(time.Second))), accounts: accounts}
}

// CollectCPA queries credentials, optionally limited to a platform or a single
// account query string.
func (c *Collector) CollectCPA(ctx context.Context, platform *string, account *string, force bool) (*Result, error) {
	files, err := c.mgmt.ListAuthFiles(ctx)
	if err != nil {
		return nil, err
	}
	platformName := ""
	if platform != nil {
		platformName = NormalizePlatform(*platform)
		if platformName == "" {
			platformName = strings.TrimSpace(*platform)
		}
	}
	single := false
	target := files
	if account != nil && strings.TrimSpace(*account) != "" {
		matched := MatchAuth(files, *account)
		if len(matched) == 0 {
			return nil, fmt.Errorf("没有找到凭证：%s", *account)
		}
		if len(matched) > 1 {
			return nil, fmt.Errorf("「%s」匹配到多个凭证", *account)
		}
		target = matched
		single = true
	} else if platformName != "" {
		filtered := make([]map[string]any, 0, len(files))
		for _, file := range files {
			if PlatformOf(file) == platformName {
				filtered = append(filtered, file)
			}
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("没有 %s 平台的凭证。", platformName)
		}
		target = filtered
	}
	skipDisabled := !single

	cacheKey := c.cacheKey(files, platformName, single)
	if !force && !single {
		if cached, ok := c.getCached(cacheKey); ok {
			return &Result{Accounts: cached, Cached: true}, nil
		}
	}
	accounts := c.collectFiles(ctx, target, skipDisabled)
	if !single {
		c.setCached(cacheKey, accounts, c.ttl(platformName))
	}
	return &Result{Accounts: accounts}, nil
}

func (c *Collector) cacheKey(files []map[string]any, platform string, single bool) string {
	if single {
		return "single"
	}
	stamps := make([]string, 0, len(files))
	for _, file := range files {
		stamps = append(stamps, str(file["auth_index"])+":"+str(file["name"])+":"+fmt.Sprint(file["disabled"]))
	}
	sort.Strings(stamps)
	return platform + "|" + strings.Join(stamps, ",")
}

func (c *Collector) collectFiles(ctx context.Context, files []map[string]any, skipDisabled bool) []model.AccountQuota {
	concurrency := c.cfg.CPA.QuotaConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	results := make([]model.AccountQuota, 0, len(files))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, file := range files {
		if skipDisabled && truthy(file["disabled"]) {
			continue
		}
		f := file
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			account := c.collectOne(ctx, f)
			mu.Lock()
			results = append(results, account)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

func (c *Collector) collectOne(ctx context.Context, file map[string]any) model.AccountQuota {
	report := baseReport(file)
	if report.Disabled {
		report.Status = "disabled"
		return report
	}
	var err error
	switch report.Platform {
	case "claude":
		err = c.fillClaude(ctx, &report, file)
	case "codex":
		err = c.fillCodex(ctx, &report, file)
	case "kimi":
		err = c.fillKimi(ctx, &report, file)
	case "xai":
		err = c.fillXAI(ctx, &report, file)
	case "antigravity":
		err = c.fillAntigravity(ctx, &report, file)
	case "gemini-cli":
		err = c.fillGemini(ctx, &report, file)
	default:
		report.Status = "unknown"
		return report
	}
	if err != nil {
		report.Error = err.Error()
		report.Status = "error"
		return report
	}
	model.SortWindows(report.Windows)
	if report.Status == "unknown" || report.Status == "" {
		report.Status = statusFromWindows(&report)
	}
	return report
}

func (c *Collector) upstream(ctx context.Context, file map[string]any, method, rawURL string, header map[string]string, data *string) (map[string]any, error) {
	authIndex := str(file["auth_index"])
	if authIndex == "" {
		return nil, fmt.Errorf("该凭证没有 auth_index")
	}
	result, err := c.mgmt.APICall(ctx, authIndex, method, rawURL, header, data, time.Duration(c.cfg.CPA.QuotaTimeout*float64(time.Second)))
	if err != nil {
		return nil, err
	}
	body, err := management.UpstreamBody(result)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// --------------------------------------------------------------------------- #
// Helpers
// --------------------------------------------------------------------------- #

func baseReport(file map[string]any) model.AccountQuota {
	return model.AccountQuota{
		Platform:  PlatformOf(file),
		Name:      displayName(file),
		AuthIndex: str(file["auth_index"]),
		Status:    "unknown",
		Disabled:  truthy(file["disabled"]),
		Cooling:   isCooling(file),
	}
}

// MatchAuth searches credentials by exact field match, then partial prefix.
func MatchAuth(files []map[string]any, query string) []map[string]any {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil
	}
	var exact, partial []map[string]any
	for _, file := range files {
		fields := []string{}
		for _, key := range []string{"auth_index", "name", "id", "email", "label", "account"} {
			if v := str(file[key]); v != "" {
				fields = append(fields, strings.ToLower(v))
			}
		}
		hitExact := false
		hitPartial := false
		for _, field := range fields {
			if field == needle {
				hitExact = true
			} else if strings.HasPrefix(field, needle) || strings.Contains(field, needle) {
				hitPartial = true
			}
		}
		if hitExact {
			exact = append(exact, file)
		} else if hitPartial {
			partial = append(partial, file)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}

func displayName(file map[string]any) string {
	if label := str(file["label"]); label != "" {
		return label
	}
	for _, key := range []string{"email", "account", "id", "name"} {
		if v := str(file[key]); v != "" {
			return v
		}
	}
	return "(unknown)"
}

func isCooling(file map[string]any) bool {
	if truthy(file["next_retry_after"]) {
		return true
	}
	status := strings.ToLower(str(file["status"]))
	message := strings.ToLower(str(file["status_message"]))
	if strings.Contains(message, "quota") || strings.Contains(message, "429") ||
		strings.Contains(message, "cooldown") || strings.Contains(message, "cooling") {
		return true
	}
	return status == "exhausted" || status == "quota"
}

func statusFromWindows(report *model.AccountQuota) string {
	if report.Error != "" {
		return "error"
	}
	if report.Disabled {
		return "disabled"
	}
	if report.Cooling {
		return "cooling"
	}
	if len(report.Windows) > 0 {
		return "ready"
	}
	return "unknown"
}

func str(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), ".")
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		}
		return false
	case float64:
		return v != 0
	case nil:
		return false
	default:
		return false
	}
}

func number(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%g", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

func asMap(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return nil
}

func asList(value any) []any {
	if l, ok := value.([]any); ok {
		return l
	}
	return nil
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
