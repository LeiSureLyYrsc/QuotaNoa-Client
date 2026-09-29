package channels

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/config"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/model"
)

const (
	volcHost          = "open.volcengineapi.com"
	volcService       = "ark"
	volcVersion       = "2024-01-01"
	volcContentType   = "application/json; charset=utf-8"
	volcEmptyBodyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

func collectVolcengine(ctx context.Context, cfg *config.Config) ([]model.AccountQuota, error) {
	accounts := cfg.Volcengine.Accounts
	if len(accounts) == 0 {
		return nil, fmt.Errorf("未配置火山方舟账号")
	}
	out := make([]model.AccountQuota, 0, len(accounts)*2)
	for _, account := range accounts {
		coding, agent := queryVolcAccount(ctx, account)
		if coding != nil {
			out = append(out, *coding)
		}
		if agent != nil {
			out = append(out, *agent)
		}
	}
	return out, nil
}

func queryVolcAccount(ctx context.Context, account config.VolcengineAccount) (*model.AccountQuota, *model.AccountQuota) {
	codingUsage, cerr := volcRequest(ctx, account, "GetCodingPlanUsage", nil)
	var coding *model.AccountQuota
	if cerr != nil {
		coding = &model.AccountQuota{Platform: "volcengine", Name: account.Name, Status: "error", Error: cerr.Error()}
	} else {
		planCoding, _ := volcRequest(ctx, account, "GetPersonalPlan", []byte(`{"Plan":"CodingPlan"}`))
		coding = parseCodingPlan(account, codingUsage, planCoding)
	}
	agentUsage, aerr := volcRequest(ctx, account, "GetAFPUsage", nil)
	var agent *model.AccountQuota
	if aerr == nil {
		planAgent, _ := volcRequest(ctx, account, "GetPersonalPlan", []byte(`{"Plan":"AgentPlan"}`))
		agent = parseAgentPlan(account, agentUsage, planAgent)
	}
	return coding, agent
}

func volcRequest(ctx context.Context, account config.VolcengineAccount, action string, body []byte) (map[string]any, error) {
	region := account.Region
	if region == "" {
		region = "cn-beijing"
	}
	if account.AccessKeyID == "" || account.SecretAccessKey == "" {
		return nil, fmt.Errorf("火山账号 %s 缺少 AK/SK", account.Name)
	}
	params := map[string]string{"Action": action, "Region": region, "Version": volcVersion}
	canonicalQuery := canonicalQuery(params)
	rawURL := "https://" + volcHost + "/?" + canonicalQuery
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	signVolcRequest(req, account.AccessKeyID, account.SecretAccessKey, region, canonicalQuery, body, time.Now())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("火山接口请求失败：%w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("火山接口返回无法解析（HTTP %d）", resp.StatusCode)
	}
	if meta := asMap(parsed["ResponseMetadata"]); meta != nil {
		if errObj := asMap(meta["Error"]); errObj != nil {
			return nil, fmt.Errorf("火山接口错误：%s", firstNonEmpty(str(errObj["Message"]), str(errObj["Code"])))
		}
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("火山接口返回 HTTP %d", resp.StatusCode)
	}
	return parsed, nil
}

func parseCodingPlan(account config.VolcengineAccount, usage, plan map[string]any) *model.AccountQuota {
	report := &model.AccountQuota{Platform: "volcengine", Name: account.Name, Status: "ready"}
	result := asMap(usage["Result"])
	if result == nil {
		result = usage
	}
	tier := volcPlanType(plan)
	if tier != "" {
		report.Plan = "Coding " + tier
		report.PlanBadges = [][2]string{{"coding", "Coding " + tier}}
	}
	for _, raw := range asList(result["QuotaUsage"]) {
		item := asMap(raw)
		if item == nil {
			continue
		}
		percent, ok := number(item["Percent"])
		if !ok {
			continue
		}
		level := strings.ToLower(str(item["Level"]))
		id, label := "volc-session", "5h"
		switch level {
		case "weekly":
			id, label = "volc-week", "周"
		case "monthly":
			id, label = "volc-month", "月"
		}
		window := model.QuotaWindow{
			ID:               id,
			Label:            label,
			UsedPercent:      model.FloatPtr(clampPercent(percent)),
			RemainingPercent: model.FloatPtr(clampPercent(100 - percent)),
			Direction:        "used",
			ResetLabel:       "-",
		}
		if level == "session" {
			window.ResetNote = "将会在首次调用后进行重置计时"
		} else {
			window.ResetAt = parseTimestamp(item["ResetTimestamp"])
		}
		report.Windows = append(report.Windows, window)
	}
	return report
}

func parseAgentPlan(account config.VolcengineAccount, usage, plan map[string]any) *model.AccountQuota {
	report := &model.AccountQuota{Platform: "volcengine", Name: account.Name, Status: "ready"}
	result := asMap(usage["Result"])
	if result == nil {
		result = usage
	}
	tier := volcPlanType(plan)
	if tier != "" {
		report.Plan = "Agent " + tier
		report.PlanBadges = [][2]string{{"agent", "Agent " + tier}}
	}
	specs := []struct{ key, id, label string }{
		{"AFPFiveHour", "volc-agent-5h", "5h"},
		{"AFPWeekly", "volc-agent-week", "周"},
		{"AFPMonthly", "volc-agent-month", "月"},
		{"AFPDaily", "volc-agent-day", "日"},
	}
	for _, spec := range specs {
		item := asMap(result[spec.key])
		if item == nil {
			continue
		}
		quota, _ := number(item["Quota"])
		used, _ := number(item["Used"])
		if spec.key == "AFPDaily" && used <= 0 {
			continue
		}
		percent := 0.0
		if quota > 0 {
			percent = clampPercent(used / quota * 100)
		}
		window := model.QuotaWindow{
			ID:               spec.id,
			Label:            spec.label,
			UsedPercent:      model.FloatPtr(percent),
			RemainingPercent: model.FloatPtr(clampPercent(100 - percent)),
			Direction:        "used",
			ResetLabel:       "-",
		}
		if ts, ok := number(item["ResetTime"]); ok {
			window.ResetAt = model.FloatPtr(normalizeEpoch(ts))
		}
		report.Windows = append(report.Windows, window)
	}
	return report
}

func volcPlanType(plan map[string]any) string {
	if plan == nil {
		return ""
	}
	result := asMap(plan["Result"])
	if result == nil {
		result = plan
	}
	return firstNonEmpty(str(result["PlanType"]), str(plan["PlanType"]))
}

// --------------------------------------------------------------------------- #
// Volcengine Signature V4
// --------------------------------------------------------------------------- #

func canonicalQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, rfc3986(key)+"="+rfc3986(params[key]))
	}
	return strings.Join(parts, "&")
}

func rfc3986(value string) string {
	escaped := url.QueryEscape(value)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}

func signVolcRequest(req *http.Request, ak, sk, region, canonicalQuery string, body []byte, now time.Time) {
	xDate := now.UTC().Format("20060102T150405Z")
	shortDate := now.UTC().Format("20060102")
	bodyHash := sha256Hex(body)
	req.Header.Set("X-Date", xDate)
	req.Header.Set("X-Content-Sha256", bodyHash)
	req.Header.Set("Content-Type", volcContentType)
	req.Header.Set("Host", volcHost)
	signedHeaders := "host;x-date;x-content-sha256;content-type"
	canonicalHeaders := "host:" + volcHost + "\n" +
		"x-date:" + xDate + "\n" +
		"x-content-sha256:" + bodyHash + "\n" +
		"content-type:" + volcContentType + "\n"
	canonicalRequest := strings.Join([]string{
		req.Method,
		"/",
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		bodyHash,
	}, "\n")
	scope := shortDate + "/" + region + "/" + volcService + "/request"
	stringToSign := strings.Join([]string{
		"HMAC-SHA256",
		xDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signingKey := hmacSHA256(hmacSHA256(hmacSHA256(hmacSHA256([]byte(sk), []byte(shortDate)), []byte(region)), []byte(volcService)), []byte("request"))
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))
	req.Header.Set("Authorization", "HMAC-SHA256 Credential="+ak+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}
