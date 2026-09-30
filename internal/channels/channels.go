// Package channels implements the local (non-CPA) quota collectors: Volcengine
// Ark, WorkBuddy2API and Qoder2OAPI.
package channels

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/config"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

// Options controls a collection.
type Options struct {
	Fresh bool
}

// Supported lists the local channels.
func Supported() []string {
	return []string{"volcengine", "workbuddy", "qoder"}
}

// Collector caches local-channel quota results with a per-channel TTL,
// mirroring the CPA collector. Fresh bypasses the cache.
type Collector struct {
	cfg   *config.Config
	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	expires   time.Time
	fetchedAt time.Time
	accounts  []model.AccountQuota
}

// New builds a local-channel collector.
func New(cfg *config.Config) *Collector {
	return &Collector{cfg: cfg, cache: map[string]cacheEntry{}}
}

// ClearCache drops all cached local-channel results.
func (c *Collector) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = map[string]cacheEntry{}
}

// TTL reports the effective cache TTL (seconds) for a local channel.
func (c *Collector) TTL(platform string) float64 {
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

func (c *Collector) getCached(key string) ([]model.AccountQuota, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok || time.Now().After(entry.expires) {
		delete(c.cache, key)
		return nil, time.Time{}, false
	}
	return entry.accounts, entry.fetchedAt, true
}

func (c *Collector) setCached(key string, accounts []model.AccountQuota, ttl float64) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.cache[key] = cacheEntry{
		expires:   now.Add(time.Duration(ttl * float64(time.Second))),
		fetchedAt: now,
		accounts:  accounts,
	}
}

// Collect returns a channel's accounts plus the time the data was actually
// obtained upstream and whether the result was served from cache.
func (c *Collector) Collect(ctx context.Context, platform string, opts Options) ([]model.AccountQuota, time.Time, bool, error) {
	if !opts.Fresh {
		if accounts, fetchedAt, ok := c.getCached(platform); ok {
			return accounts, fetchedAt, true, nil
		}
	}
	var (
		accounts []model.AccountQuota
		err      error
	)
	switch platform {
	case "volcengine":
		accounts, err = collectVolcengine(ctx, c.cfg)
	case "workbuddy":
		accounts, err = collectWorkbuddy(ctx, c.cfg)
	case "qoder":
		accounts, err = collectQoder(ctx, c.cfg)
	default:
		return nil, time.Time{}, false, fmt.Errorf("本地渠道未实现：%s", platform)
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}
	fetchedAt := time.Now()
	c.setCached(platform, accounts, c.TTL(platform))
	return accounts, fetchedAt, false, nil
}

// --------------------------------------------------------------------------- #
// Helpers
// --------------------------------------------------------------------------- #

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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

func normalizeEpoch(value float64) float64 {
	if value > 1e11 {
		return value / 1000.0
	}
	return value
}

func parseTimestamp(value any) *float64 {
	switch v := value.(type) {
	case float64:
		return model.FloatPtr(normalizeEpoch(v))
	case int:
		return model.FloatPtr(normalizeEpoch(float64(v)))
	case int64:
		return model.FloatPtr(normalizeEpoch(float64(v)))
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
			if t, err := time.Parse(layout, text); err == nil {
				return model.FloatPtr(float64(t.Unix()))
			}
		}
		if f, ok := number(text); ok {
			return model.FloatPtr(normalizeEpoch(f))
		}
	}
	return nil
}

func resetLabel(epoch *float64) string {
	if epoch == nil {
		return "-"
	}
	return model.ResetLabelFromEpoch(*epoch, time.Now())
}
