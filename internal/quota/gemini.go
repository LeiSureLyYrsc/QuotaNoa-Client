package quota

import (
	"context"
	"fmt"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

func (c *Collector) fillGemini(ctx context.Context, report *model.AccountQuota, file map[string]any) error {
	body, err := c.upstream(ctx, file, "POST", "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota", map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
	}, strPtr("{}"))
	if err != nil {
		return err
	}
	report.Windows = parseGeminiQuota(body)
	return nil
}

func parseGeminiQuota(body map[string]any) []model.QuotaWindow {
	var windows []model.QuotaWindow
	for index, raw := range asList(body["buckets"]) {
		item := asMap(raw)
		if item == nil {
			continue
		}
		fraction, ok := number(item["remainingFraction"])
		if !ok {
			continue
		}
		label := firstNonEmpty(str(item["name"]), str(item["modelId"]), fmt.Sprintf("额度 %d", index+1))
		windows = append(windows, model.QuotaWindow{
			ID:               "gemini-" + slug(label),
			Label:            label,
			RemainingPercent: model.FloatPtr(clampPercent(fraction * 100)),
			ResetLabel:       "-",
			Direction:        "remaining",
		})
	}
	return windows
}
