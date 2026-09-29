// Package channels implements the local (non-CPA) quota collectors: Volcengine
// Ark, WorkBuddy2API and Qoder2OAPI.
package channels

import (
	"context"
	"fmt"
	"strings"
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

// Collect dispatches to the local channel collector.
func Collect(ctx context.Context, platform string, cfg *config.Config, opts Options) ([]model.AccountQuota, error) {
	switch platform {
	case "volcengine":
		return collectVolcengine(ctx, cfg)
	case "workbuddy":
		return collectWorkbuddy(ctx, cfg)
	case "qoder":
		return collectQoder(ctx, cfg)
	default:
		return nil, fmt.Errorf("本地渠道未实现：%s", platform)
	}
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
