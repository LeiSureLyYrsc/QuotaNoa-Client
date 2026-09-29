package quota

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

// strPtr returns a pointer to a string literal.
func strPtr(value string) *string { return &value }

// parseTimestamp accepts epoch seconds/milliseconds or an RFC3339/ISO string.
func parseTimestamp(value any) *float64 {
	switch v := value.(type) {
	case float64:
		return model.FloatPtr(normalizeEpoch(v))
	case int:
		return model.FloatPtr(normalizeEpoch(float64(v)))
	case int64:
		return model.FloatPtr(normalizeEpoch(float64(v)))
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return nil
		}
		return model.FloatPtr(normalizeEpoch(f))
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02 15:04:05"} {
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

func normalizeEpoch(value float64) float64 {
	if value > 1e11 {
		return value / 1000.0
	}
	return value
}

// resetAtFromEntry extracts a reset timestamp from a variety of key names.
func resetAtFromEntry(entry map[string]any) *float64 {
	for _, key := range []string{"resets_at", "reset_at", "resetAt", "ResetTimestamp", "resetTime", "ResetTime", "end_time", "billingPeriodEnd"} {
		if ts := parseTimestamp(entry[key]); ts != nil {
			return ts
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNumber(values ...any) *float64 {
	for _, value := range values {
		if f, ok := number(value); ok {
			return model.FloatPtr(f)
		}
	}
	return nil
}

func percentWindow(id, label string, remaining, limit float64) model.QuotaWindow {
	var pct *float64
	if limit > 0 {
		pct = model.FloatPtr(clampPercent(remaining / limit * 100))
	}
	return model.QuotaWindow{
		ID:               id,
		Label:            label,
		Remaining:        model.FloatPtr(remaining),
		Limit:            model.FloatPtr(limit),
		RemainingPercent: pct,
		Direction:        "remaining",
		ResetLabel:       "-",
	}
}

func slug(value string) string {
	text := strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	prevDash := false
	for _, ch := range text {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			b.WriteRune(ch)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func clamp(value, lo, hi float64) float64 {
	if value < lo {
		return lo
	}
	if value > hi {
		return hi
	}
	return value
}
