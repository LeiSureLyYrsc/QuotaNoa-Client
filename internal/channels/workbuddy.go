package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/config"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

type wbSession struct {
	token     string
	apiKey    string
	expiresAt time.Time
}

var (
	wbMu       sync.Mutex
	wbSessions = map[string]wbSession{}
)

func collectWorkbuddy(ctx context.Context, cfg *config.Config) ([]model.AccountQuota, error) {
	servers := cfg.Workbuddy.Servers
	if len(servers) == 0 {
		return nil, fmt.Errorf("未配置 WorkBuddy 网关")
	}
	out := make([]model.AccountQuota, 0)
	for _, server := range servers {
		payload, err := fetchWorkbuddyQuota(ctx, server)
		if err != nil {
			out = append(out, model.AccountQuota{
				Platform: "workbuddy",
				Name:     server.Name,
				Instance: server.Name,
				Status:   "error",
				Error:    err.Error(),
			})
			continue
		}
		out = append(out, parseWorkbuddyAccounts(server, payload)...)
	}
	return out, nil
}

func wbAPIKey(ctx context.Context, server config.WorkbuddyServer, timeout time.Duration) (string, error) {
	if server.APIKey != "" {
		return server.APIKey, nil
	}
	if server.Username == "" || server.Password == "" {
		return "", fmt.Errorf("网关 %s 未配置 api_key 或账号密码", server.Name)
	}
	wbMu.Lock()
	session, ok := wbSessions[server.Name]
	wbMu.Unlock()
	if ok && time.Now().Before(session.expiresAt) && session.apiKey != "" {
		return session.apiKey, nil
	}
	loginBody, _ := json.Marshal(map[string]string{"username": server.Username, "password": server.Password})
	data, err := wbDo(ctx, timeout, http.MethodPost, server.BaseURL+"/api/login", "", loginBody)
	if err != nil {
		return "", err
	}
	var loginResp map[string]any
	_ = json.Unmarshal(data, &loginResp)
	token := firstNonEmpty(str(loginResp["token"]), str(loginResp["access_token"]), str(loginResp["api_key"]))
	if token == "" {
		return "", fmt.Errorf("网关 %s 登录未返回 token", server.Name)
	}
	configData, err := wbDo(ctx, timeout, http.MethodGet, server.BaseURL+"/api/config", token, nil)
	if err != nil {
		return "", err
	}
	var configResp map[string]any
	_ = json.Unmarshal(configData, &configResp)
	apiKey := firstNonEmpty(str(configResp["api_key"]), str(configResp["apiKey"]))
	if apiKey == "" {
		return "", fmt.Errorf("网关 %s 未返回 api_key", server.Name)
	}
	wbMu.Lock()
	wbSessions[server.Name] = wbSession{token: token, apiKey: apiKey, expiresAt: time.Now().Add(10 * time.Hour)}
	wbMu.Unlock()
	return apiKey, nil
}

func fetchWorkbuddyQuota(ctx context.Context, server config.WorkbuddyServer) (map[string]any, error) {
	timeout := time.Duration(server.Timeout * float64(time.Second))
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	apiKey, err := wbAPIKey(ctx, server, timeout)
	if err != nil {
		return nil, err
	}
	data, err := wbDo(ctx, timeout, http.MethodGet, server.BaseURL+"/v1/quota", apiKey, nil)
	if err != nil {
		// Invalidate a stale session so the next attempt re-logs in.
		wbMu.Lock()
		delete(wbSessions, server.Name)
		wbMu.Unlock()
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("WorkBuddy 返回无法解析")
	}
	return payload, nil
}

func wbDo(ctx context.Context, timeout time.Duration, method, rawURL, bearer string, body []byte) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, rawURL, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 WorkBuddy：%w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 401 {
		return nil, fmt.Errorf("WorkBuddy 鉴权失败（401）")
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("WorkBuddy 返回 HTTP %d", resp.StatusCode)
	}
	return data, nil
}

func parseWorkbuddyAccounts(server config.WorkbuddyServer, payload map[string]any) []model.AccountQuota {
	rows := asList(payload["accounts"])
	out := make([]model.AccountQuota, 0, len(rows))
	for _, raw := range rows {
		row := asMap(raw)
		if row == nil {
			continue
		}
		uid := str(row["uid"])
		nickname := str(row["nickname"])
		name := firstNonEmpty(nickname, uid, "(unknown)")
		report := model.AccountQuota{
			Platform:  "workbuddy",
			Name:      name,
			AuthIndex: uid,
			Instance:  server.Name,
			Disabled:  truthy(row["disabled"]),
			Cooling:   truthy(row["cooling"]),
		}
		quotas := asMap(row["quotas"])
		if quotas == nil {
			report.Status = "unknown"
			if report.Cooling {
				report.Status = "cooling"
			}
			if msg := str(row["error"]); msg != "" {
				report.Error = msg
			} else {
				report.Error = "本次未探测（冷却/超时/失败）"
			}
			out = append(out, report)
			continue
		}
		var total, used, remaining float64
		var earliestReset time.Time
		hasReset := false
		packageCount := 0
		for _, value := range quotas {
			item := asMap(value)
			if item == nil {
				continue
			}
			packageCount++
			if v, ok := number(item["total"]); ok {
				total += v
			}
			if v, ok := number(item["used"]); ok {
				used += v
			}
			if v, ok := number(item["remaining"]); ok {
				remaining += v
			}
			if truthy(item["recurring"]) {
				if ts := parseTimestamp(item["resetAt"]); ts != nil {
					resetTime := time.Unix(int64(*ts), 0)
					if !hasReset || resetTime.Before(earliestReset) {
						earliestReset = resetTime
						hasReset = true
					}
				}
			}
		}
		_ = used
		report.Plan = fmt.Sprintf("%d 套餐", packageCount)
		window := model.QuotaWindow{
			ID:         "wb-credits",
			Label:      "积分",
			Remaining:  model.FloatPtr(remaining),
			Limit:      model.FloatPtr(total),
			Direction:  "remaining",
			ResetLabel: "-",
		}
		if total > 0 {
			window.RemainingPercent = model.FloatPtr(clampPercent(remaining / total * 100))
		}
		if hasReset {
			window.ResetAt = model.FloatPtr(float64(earliestReset.Unix()))
			window.ResetLabel = model.ResetLabelFromEpoch(float64(earliestReset.Unix()), time.Now())
			window.ResetNote = "最早的(空)套餐 " + window.ResetLabel + " 后过期"
		}
		report.Windows = []model.QuotaWindow{window}
		report.Status = "ready"
		out = append(out, report)
	}
	return out
}

// workbuddyLogin validates credentials and refreshes the cached session.
func workbuddyLogin(ctx context.Context, server config.WorkbuddyServer) (string, error) {
	wbMu.Lock()
	delete(wbSessions, server.Name)
	wbMu.Unlock()
	timeout := time.Duration(server.Timeout * float64(time.Second))
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if _, err := wbAPIKey(ctx, server, timeout); err != nil {
		return "", err
	}
	return fmt.Sprintf("网关 %s 会话已刷新。", server.Name), nil
}

func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		}
		return false
	case float64:
		return v != 0
	default:
		return false
	}
}
