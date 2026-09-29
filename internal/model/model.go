// Package model defines the channel-agnostic quota data model shared by the
// CPA collectors and the local channel collectors.
package model

import (
	"sort"
	"strings"
	"time"
)

// QuotaWindow is a single quota window (5h / weekly / monthly / credits ...).
type QuotaWindow struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	UsedPercent      *float64 `json:"used_percent,omitempty"`
	RemainingPercent *float64 `json:"remaining_percent,omitempty"`
	Remaining        *float64 `json:"remaining,omitempty"`
	Limit            *float64 `json:"limit,omitempty"`
	ResetLabel       string   `json:"reset_label"`
	ResetAt          *float64 `json:"reset_at,omitempty"`
	ResetNote        string   `json:"reset_note,omitempty"`
	Direction        string   `json:"direction,omitempty"`
}

// WindowIsUsed reports whether the progress bar is expressed as "used".
func WindowIsUsed(w QuotaWindow) bool {
	switch w.Direction {
	case "used":
		return true
	case "remaining":
		return false
	}
	return strings.HasPrefix(w.ID, "grok-")
}

// AccountQuota is one account's quota card.
type AccountQuota struct {
	Platform                 string
	Name                     string
	AuthIndex                string
	Plan                     string
	PlanBadges               [][2]string
	SubscriptionBadges       [][2]string
	Status                   string
	Error                    string
	Windows                  []QuotaWindow
	Disabled                 bool
	Cooling                  bool
	Instance                 string
	SubscriptionExpiresAt    *float64
	SubscriptionExpiresLabel string
	ResetCredits             *int
}

// PlatformQuota groups accounts of a single platform.
type PlatformQuota struct {
	Platform string
	Title    string
	Accounts []AccountQuota
}

// QuotaBoard groups platform sections.
type QuotaBoard struct {
	Platforms []PlatformQuota
	Cached    bool
}

// FloatPtr returns a pointer to v.
func FloatPtr(v float64) *float64 { return &v }

// IntPtr returns a pointer to v.
func IntPtr(v int) *int { return &v }

// StrPtr returns a pointer to v.
func StrPtr(v string) *string { return &v }

// Truncate shortens text to limit runes, appending an ellipsis when cut.
func Truncate(text string, limit int) string {
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}

// ResetLabelFromEpoch formats an epoch timestamp as a "XdXh" countdown label.
func ResetLabelFromEpoch(epoch float64, now time.Time) string {
	d := time.Unix(int64(epoch), 0).Sub(now)
	if d <= 0 {
		return "已过期"
	}
	return HumanDuration(d)
}

// HumanDuration renders a duration as "XdXhXm" (dropping leading zero units).
func HumanDuration(d time.Duration) string {
	total := int(d.Seconds())
	if total <= 0 {
		return "0m"
	}
	days := total / 86400
	hours := (total % 86400) / 3600
	mins := (total % 3600) / 60
	var b strings.Builder
	if days > 0 {
		b.WriteString(itoa(days))
		b.WriteByte('d')
	}
	if hours > 0 {
		b.WriteString(itoa(hours))
		b.WriteByte('h')
	}
	if mins > 0 || b.Len() == 0 {
		b.WriteString(itoa(mins))
		b.WriteByte('m')
	}
	return b.String()
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// SortWindows orders windows by span (hour, week, month, other) then by id.
var windowOrder = map[string]int{
	"gemini-5h": 0, "gemini-week": 1, "gemini-month": 2,
	"claude-gpt-5h": 3, "claude-gpt-week": 4, "claude-gpt-month": 5,
	"code-5h": 6, "code-7d": 7,
	"five_hour": 8, "seven_day": 9, "seven_day_opus": 10, "seven_day_sonnet": 11,
	"seven_day_oauth_apps": 12, "seven_day_cowork": 13, "iguana_necktie": 14,
	"extra": 15, "billing": 16, "grok-build": 17, "grok-chat": 18, "grok-imagine": 19,
	"usage": 20, "wb-credits": 21, "qoder-general": 22, "qoder-addon": 23,
}

// SortWindows returns windows ordered by span then canonical order.
func SortWindows(windows []QuotaWindow) {
	sort.SliceStable(windows, func(i, j int) bool {
		si, sj := windowSpan(windows[i]), windowSpan(windows[j])
		if si != sj {
			return si < sj
		}
		oi, iok := windowOrder[windows[i].ID]
		oj, jok := windowOrder[windows[j].ID]
		if !iok {
			oi = len(windowOrder)
		}
		if !jok {
			oj = len(windowOrder)
		}
		if oi != oj {
			return oi < oj
		}
		return windows[i].Label < windows[j].Label
	})
}

func windowSpan(w QuotaWindow) int {
	blob := strings.ToLower(w.ID + " " + w.Label)
	switch {
	case containsAny(blob, "5h", "five", "hour", "滚动", "rolling"):
		return 0
	case containsAny(blob, "week", "weekly", "7d", "seven", "周"):
		return 1
	case containsAny(blob, "month", "monthly", "月"):
		return 2
	default:
		return 3
	}
}

func containsAny(text string, tokens ...string) bool {
	for _, t := range tokens {
		if strings.Contains(text, t) {
			return true
		}
	}
	return false
}
