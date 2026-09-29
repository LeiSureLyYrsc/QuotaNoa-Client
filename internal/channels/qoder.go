package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/config"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

var qoderPlanLabels = map[string]string{
	"personal":                          "个人版",
	"personal_free":                     "体验版",
	"personal_professional":             "专业版",
	"personal_premium":                  "高级版",
	"personal_ultimate":                 "旗舰版",
	"team":                              "团队版",
	"enterprise":                        "企业标准版",
	"PLAN_TIER_FREE":                    "体验版",
	"PLAN_TIER_PROFESSIONAL":            "专业版",
	"PLAN_TIER_PREMIUM":                 "高级版",
	"PLAN_TIER_ULTIMATE":                "旗舰版",
	"ORGANIZATION_PLAN_TIER_TEAM":       "团队版",
	"ORGANIZATION_PLAN_TIER_ENTERPRISE": "企业标准版",
}

func collectQoder(ctx context.Context, cfg *config.Config) ([]model.AccountQuota, error) {
	servers := cfg.Qoder.Servers
	if len(servers) == 0 {
		return nil, fmt.Errorf("未配置 Qoder 代理")
	}
	out := make([]model.AccountQuota, 0)
	for _, server := range servers {
		payload, err := fetchQoder(ctx, server)
		if err != nil {
			out = append(out, model.AccountQuota{
				Platform: "qoder",
				Name:     server.Name,
				Instance: server.Name,
				Status:   "error",
				Error:    err.Error(),
			})
			continue
		}
		out = append(out, parseQoderAccounts(server, payload)...)
	}
	return out, nil
}

func fetchQoder(ctx context.Context, server config.QoderServer) (map[string]any, error) {
	timeout := time.Duration(server.Timeout * float64(time.Second))
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	baseURL := server.BaseURL
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8000"
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, baseURL+"/v1/dashboard/billing/credits", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if server.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+server.APIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 Qoder：%w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Qoder 返回 HTTP %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("Qoder 返回无法解析")
	}
	return payload, nil
}

func parseQoderAccounts(server config.QoderServer, payload map[string]any) []model.AccountQuota {
	rows := asList(payload["accounts"])
	out := make([]model.AccountQuota, 0, len(rows))
	for _, raw := range rows {
		row := asMap(raw)
		if row == nil {
			continue
		}
		id := str(row["id"])
		name := firstNonEmpty(str(row["name"]), str(row["email"]), str(row["user_id"]), id, "(unknown)")
		report := model.AccountQuota{
			Platform:  "qoder",
			Name:      name,
			AuthIndex: id,
			Instance:  server.Name,
			Plan:      qoderPlan(str(row["user_type"])),
			Status:    "ready",
		}
		flags := asMap(row["flags"])
		if truthy(row["skip_auth"]) || (flags != nil && truthy(flags["skip_auth"])) {
			report.Disabled = true
		}
		if enabled, ok := row["enabled"]; ok && !truthy(enabled) {
			report.Disabled = true
		}
		if msg := str(row["error"]); msg != "" {
			report.Error = msg
			report.Status = "error"
		}
		report.Windows = append(report.Windows, qoderBucketWindow("qoder-general", "通用", asMap(row["general"]))...)
		report.Windows = append(report.Windows, qoderBucketWindow("qoder-addon", "加量", asMap(row["addon"]))...)
		for index, dedicatedRaw := range asList(row["dedicated"]) {
			item := asMap(dedicatedRaw)
			if item == nil || !truthyOrMissing(item["available"]) {
				continue
			}
			label := firstNonEmpty(str(item["title"]), str(item["plan_name"]), fmt.Sprintf("专属 %d", index+1))
			window := model.QuotaWindow{
				ID:         fmt.Sprintf("qoder-dedicated-%d", index),
				Label:      label,
				Direction:  "remaining",
				ResetLabel: "-",
			}
			total, _ := number(item["total"])
			remaining, _ := number(item["remaining"])
			window.Remaining = model.FloatPtr(remaining)
			window.Limit = model.FloatPtr(total)
			if total > 0 {
				window.RemainingPercent = model.FloatPtr(clampPercent(remaining / total * 100))
			}
			report.Windows = append(report.Windows, window)
		}
		out = append(out, report)
	}
	return out
}

func qoderBucketWindow(id, label string, bucket map[string]any) []model.QuotaWindow {
	if bucket == nil {
		return nil
	}
	total, _ := number(bucket["total"])
	if total <= 0 {
		return nil
	}
	remaining, _ := number(bucket["remaining"])
	return []model.QuotaWindow{{
		ID:               id,
		Label:            label,
		Remaining:        model.FloatPtr(remaining),
		Limit:            model.FloatPtr(total),
		RemainingPercent: model.FloatPtr(clampPercent(remaining / total * 100)),
		Direction:        "remaining",
		ResetLabel:       "-",
	}}
}

func qoderPlan(userType string) string {
	key := strings.TrimSpace(userType)
	if key == "" {
		return ""
	}
	if label, ok := qoderPlanLabels[key]; ok {
		return label
	}
	if label, ok := qoderPlanLabels[strings.ToUpper(key)]; ok {
		return label
	}
	return key
}

func truthyOrMissing(value any) bool {
	if value == nil {
		return true
	}
	return truthy(value)
}
