package quota

import (
	"context"
	"strings"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

var claudeWindowSpecs = []struct {
	key   string
	label string
}{
	{"five_hour", "5h"},
	{"seven_day", "周"},
	{"seven_day_opus", "周 Opus"},
	{"seven_day_sonnet", "周 Sonnet"},
	{"seven_day_oauth_apps", "周 OAuth"},
	{"seven_day_cowork", "周 Cowork"},
	{"iguana_necktie", "Necktie"},
}

func (c *Collector) fillClaude(ctx context.Context, report *model.AccountQuota, file map[string]any) error {
	body, err := c.upstream(ctx, file, "GET", "https://api.anthropic.com/api/oauth/usage", map[string]string{
		"Authorization":  "Bearer $TOKEN$",
		"anthropic-beta": "oauth-2025-04-20",
		"Content-Type":   "application/json",
	}, nil)
	if err != nil {
		return err
	}
	windows, plan := parseClaudeUsage(body)
	report.Windows = windows
	if plan != "" {
		report.Plan = plan
	}
	return nil
}

func parseClaudeUsage(body map[string]any) ([]model.QuotaWindow, string) {
	var windows []model.QuotaWindow
	for _, spec := range claudeWindowSpecs {
		entry := asMap(body[spec.key])
		if entry == nil {
			continue
		}
		used, ok := number(entry["utilization"])
		if !ok {
			continue
		}
		used = clampPercent(used)
		windows = append(windows, model.QuotaWindow{
			ID:               spec.key,
			Label:            spec.label,
			UsedPercent:      model.FloatPtr(used),
			RemainingPercent: model.FloatPtr(clampPercent(100 - used)),
			ResetAt:          resetAtFromEntry(entry),
			ResetLabel:       "-",
			Direction:        "remaining",
		})
	}
	return windows, strings.TrimSpace(str(body["plan_type"]))
}
