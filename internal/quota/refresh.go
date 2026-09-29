package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/management"
)

// RefreshCodex consumes one Codex reset credit for the matched account and
// clears the local quota cache. It performs no network I/O until the caller has
// already verified the local refresh gate.
func (c *Collector) RefreshCodex(ctx context.Context, accountQuery string) (string, *int, error) {
	files, err := c.mgmt.ListAuthFiles(ctx)
	if err != nil {
		return "", nil, err
	}
	codexFiles := make([]map[string]any, 0, len(files))
	for _, file := range files {
		if PlatformOf(file) == "codex" {
			codexFiles = append(codexFiles, file)
		}
	}
	matched := MatchAuth(codexFiles, accountQuery)
	if len(matched) == 0 {
		return "", nil, fmt.Errorf("没有找到 Codex 凭证：%s", accountQuery)
	}
	if len(matched) > 1 {
		return "", nil, fmt.Errorf("「%s」匹配到多个 Codex 凭证", accountQuery)
	}
	file := matched[0]
	headers := codexHeaders(file)
	body, err := c.upstream(ctx, file, "GET", "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits", headers, nil)
	if err != nil {
		return "", nil, err
	}
	credit := pickResetCredit(body)
	if credit == nil {
		return "", nil, fmt.Errorf("没有可用的 Codex 重置券")
	}
	creditID := firstNonEmpty(str(credit["credit_id"]), str(credit["id"]))
	payload, _ := json.Marshal(map[string]string{"credit_id": creditID, "redeem_request_id": uuid.NewString()})
	text := string(payload)
	consume, err := c.upstream(ctx, file, "POST", "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume", headers, &text)
	if err != nil {
		return "", nil, err
	}
	if code := management.StatusCode(consume); code >= 400 {
		return "", nil, fmt.Errorf("上游拒绝重置（HTTP %d）", code)
	}
	c.ClearCache()
	remaining := countResetCredits(body)
	if remaining != nil && *remaining > 0 {
		value := *remaining - 1
		remaining = &value
	}
	return "已消耗 1 次 Codex 重置券，额度已刷新。", remaining, nil
}

func pickResetCredit(body map[string]any) map[string]any {
	credits := resetCreditList(body)
	if len(credits) == 0 {
		return nil
	}
	available := make([]map[string]any, 0, len(credits))
	for _, item := range credits {
		if creditAvailable(item) {
			available = append(available, item)
		}
	}
	if len(available) == 0 {
		return nil
	}
	sort.SliceStable(available, func(i, j int) bool {
		return creditExpiry(available[i]) < creditExpiry(available[j])
	})
	return available[0]
}

func creditExpiry(item map[string]any) float64 {
	for _, key := range []string{"expires_at", "expiresAt", "expiration", "expire_time"} {
		if ts := parseTimestamp(item[key]); ts != nil {
			return *ts
		}
	}
	return float64(1 << 62)
}
