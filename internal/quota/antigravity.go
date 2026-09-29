package quota

import (
	"context"
	"strings"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

var antigravitySummaryURLs = []string{
	"https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:retrieveUserQuotaSummary",
	"https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
}

var antigravityPlanURLs = []string{
	"https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:loadCodeAssist",
	"https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
}

func antigravityHeaders() map[string]string {
	return map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"User-Agent":    "antigravity/cli/1.0.13 (aidev_client; os_type=darwin; arch=arm64)",
	}
}

func (c *Collector) fillAntigravity(ctx context.Context, report *model.AccountQuota, file map[string]any) error {
	headers := antigravityHeaders()
	var body map[string]any
	var lastErr error
	for _, url := range antigravitySummaryURLs {
		body, lastErr = c.upstream(ctx, file, "POST", url, headers, strPtr("{}"))
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return lastErr
	}
	report.Windows = parseAntigravitySummary(body)
	for _, url := range antigravityPlanURLs {
		planBody, err := c.upstream(ctx, file, "POST", url, headers, strPtr(`{"metadata":{"ideType":"ANTIGRAVITY"}}`))
		if err == nil {
			report.Plan = parseAntigravityPlan(planBody)
			break
		}
	}
	return nil
}

func parseAntigravitySummary(body map[string]any) []model.QuotaWindow {
	var windows []model.QuotaWindow
	for _, groupRaw := range asList(body["groups"]) {
		group := asMap(groupRaw)
		if group == nil {
			continue
		}
		groupName := firstNonEmpty(str(group["name"]), str(group["displayName"]))
		for _, bucketRaw := range asList(group["buckets"]) {
			bucket := asMap(bucketRaw)
			if bucket == nil {
				continue
			}
			bucketName := firstNonEmpty(str(bucket["name"]), str(bucket["displayName"]))
			fraction, ok := number(bucket["remainingFraction"])
			if !ok {
				continue
			}
			id, label := antigravityWindow(groupName, bucketName)
			windows = append(windows, model.QuotaWindow{
				ID:               id,
				Label:            label,
				RemainingPercent: model.FloatPtr(clampPercent(fraction * 100)),
				ResetAt:          resetAtFromEntry(bucket),
				ResetLabel:       "-",
				Direction:        "remaining",
			})
		}
	}
	return windows
}

func antigravityWindow(groupName, bucketName string) (string, string) {
	blob := strings.ToLower(groupName + " " + bucketName)
	family := "gemini"
	if strings.Contains(blob, "claude") || strings.Contains(blob, "gpt") {
		family = "claude-gpt"
	}
	span := "5h"
	switch {
	case strings.Contains(blob, "week") || strings.Contains(blob, "周"):
		span = "week"
	case strings.Contains(blob, "month") || strings.Contains(blob, "月"):
		span = "month"
	}
	label := "5h"
	switch span {
	case "week":
		label = "周"
	case "month":
		label = "月"
	}
	return family + "-" + span, label
}

func parseAntigravityPlan(body map[string]any) string {
	if plan := firstNonEmpty(str(body["planType"]), str(body["plan_type"])); plan != "" {
		return plan
	}
	if result := asMap(body["result"]); result != nil {
		return firstNonEmpty(str(result["planType"]), str(result["plan_type"]))
	}
	return ""
}
