package quota

import (
	"context"
	"fmt"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

func (c *Collector) fillKimi(ctx context.Context, report *model.AccountQuota, file map[string]any) error {
	body, err := c.upstream(ctx, file, "GET", "https://api.kimi.com/coding/v1/usages", map[string]string{
		"Authorization": "Bearer $TOKEN$",
	}, nil)
	if err != nil {
		return err
	}
	report.Windows = parseKimiUsage(body)
	return nil
}

func parseKimiUsage(body map[string]any) []model.QuotaWindow {
	var windows []model.QuotaWindow
	if usage := asMap(body["usage"]); usage != nil {
		remaining, rok := number(usage["remaining"])
		limit, lok := number(usage["limit"])
		if rok && lok {
			windows = append(windows, percentWindow("usage", "用量", remaining, limit))
		}
	}
	for index, raw := range asList(body["limits"]) {
		item := asMap(raw)
		if item == nil {
			continue
		}
		remaining, rok := number(item["remaining"])
		limit, lok := number(item["limit"])
		if !rok || !lok {
			continue
		}
		label := firstNonEmpty(str(item["name"]), str(item["label"]), fmt.Sprintf("限额 %d", index+1))
		windows = append(windows, percentWindow(fmt.Sprintf("limit-%d", index), label, remaining, limit))
	}
	return windows
}
