package quota

import (
	"context"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

func (c *Collector) fillXAI(ctx context.Context, report *model.AccountQuota, file map[string]any) error {
	body, err := c.upstream(ctx, file, "GET", "https://cli-chat-proxy.grok.com/v1/billing?format=credits", map[string]string{
		"Authorization":         "Bearer $TOKEN$",
		"x-xai-token-auth":      "xai-grok-cli",
		"x-grok-client-version": "0.2.91",
		"accept":                "*/*",
	}, nil)
	if err != nil {
		return err
	}
	report.Windows = parseXAIBilling(body)
	return nil
}

func parseXAIBilling(body map[string]any) []model.QuotaWindow {
	var windows []model.QuotaWindow
	config := asMap(body["config"])
	if config == nil {
		config = body
	}
	if percent, ok := number(config["creditUsagePercent"]); ok {
		percent = clampPercent(percent)
		windows = append(windows, model.QuotaWindow{
			ID:               "billing",
			Label:            "周额度",
			UsedPercent:      model.FloatPtr(percent),
			RemainingPercent: model.FloatPtr(clampPercent(100 - percent)),
			ResetAt:          resetAtFromEntry(config),
			ResetLabel:       "-",
			Direction:        "used",
		})
	}
	for _, key := range []string{"productUsage", "product_usage", "products", "usages"} {
		products := asMap(body[key])
		for name, raw := range products {
			item := asMap(raw)
			if item == nil {
				continue
			}
			used := firstNumber(item["usagePercent"], item["usedPercent"], item["percent"])
			if used == nil {
				continue
			}
			windows = append(windows, model.QuotaWindow{
				ID:               "grok-" + slug(name),
				Label:            name,
				UsedPercent:      model.FloatPtr(clampPercent(*used)),
				RemainingPercent: model.FloatPtr(clampPercent(100 - *used)),
				ResetLabel:       "-",
				Direction:        "used",
			})
		}
	}
	return windows
}
