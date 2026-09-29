package quota

import (
	"context"
	"strings"
	"time"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

const codexUserAgent = "codex_cli_rs/0.76.0 (Debian 13.0.0; x86_64) WindowsTerminal"

func codexHeaders(file map[string]any) map[string]string {
	headers := map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"User-Agent":    codexUserAgent,
	}
	if account := str(file["chatgpt_account_id"]); account != "" && !strings.Contains(account, "@") {
		headers["Chatgpt-Account-Id"] = account
	}
	return headers
}

func (c *Collector) fillCodex(ctx context.Context, report *model.AccountQuota, file map[string]any) error {
	headers := codexHeaders(file)
	body, err := c.upstream(ctx, file, "GET", "https://chatgpt.com/backend-api/wham/usage", headers, nil)
	if err != nil {
		return err
	}
	report.Windows = parseCodexUsage(body)
	if credits, cerr := c.upstream(ctx, file, "GET", "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits", headers, nil); cerr == nil {
		report.ResetCredits = countResetCredits(credits)
	}
	if at, label := subscriptionExpiry(file); at != nil {
		report.SubscriptionExpiresAt = at
		report.SubscriptionExpiresLabel = label
	}
	return nil
}

func parseCodexUsage(body map[string]any) []model.QuotaWindow {
	rate := asMap(body["rate_limit"])
	if rate == nil {
		return nil
	}
	var windows []model.QuotaWindow
	for _, key := range []string{"primary_window", "secondary_window"} {
		window := asMap(rate[key])
		if window == nil {
			continue
		}
		id, label := codexWindowID(window)
		windows = append(windows, buildCodexWindow(id, label, window, rate))
	}
	return windows
}

func codexWindowID(window map[string]any) (string, string) {
	secs, _ := number(window["limit_window_seconds"])
	if secs >= 600000 {
		return "code-7d", "周"
	}
	return "code-5h", "5h"
}

func buildCodexWindow(id, label string, window, rate map[string]any) model.QuotaWindow {
	used, ok := number(window["used_percent"])
	if !ok {
		used = 0
	}
	used = clampPercent(used)
	if truthy(rate["limit_reached"]) {
		used = 100
	}
	return model.QuotaWindow{
		ID:               id,
		Label:            label,
		UsedPercent:      model.FloatPtr(used),
		RemainingPercent: model.FloatPtr(clampPercent(100 - used)),
		ResetAt:          resetAtFromEntry(window),
		ResetLabel:       "-",
		Direction:        "remaining",
	}
}

func countResetCredits(body map[string]any) *int {
	credits := resetCreditList(body)
	if credits == nil {
		return nil
	}
	count := 0
	for _, item := range credits {
		if creditAvailable(item) {
			count++
		}
	}
	return model.IntPtr(count)
}

func resetCreditList(body map[string]any) []map[string]any {
	for _, key := range []string{"credits", "reset_credits", "available_credits"} {
		list := asList(body[key])
		if list == nil {
			continue
		}
		out := make([]map[string]any, 0, len(list))
		for _, item := range list {
			if m := asMap(item); m != nil {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

func creditAvailable(item map[string]any) bool {
	if truthy(item["redeemed"]) {
		return false
	}
	status := strings.ToLower(str(item["status"]))
	switch status {
	case "", "available", "ok", "active", "unused":
		return true
	default:
		return false
	}
}

func subscriptionExpiry(file map[string]any) (*float64, string) {
	lookup := func(m map[string]any) *float64 {
		for _, key := range []string{"chatgpt_subscription_active_until", "subscription_active_until"} {
			if ts, ok := number(m[key]); ok {
				return model.FloatPtr(normalizeEpoch(ts))
			}
		}
		return nil
	}
	at := lookup(file)
	if at == nil {
		if idToken := asMap(file["id_token"]); idToken != nil {
			at = lookup(idToken)
		}
	}
	if at == nil {
		return nil, ""
	}
	label := time.Unix(int64(*at), 0).Local().Format("2006-01-02 15:04")
	return at, label
}
