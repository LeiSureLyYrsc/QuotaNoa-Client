// Package management is a deliberately restricted CLIProxyAPI management
// client: it only exposes the read-only quota paths the client needs and
// enforces a strict upstream URL/method allowlist for /api-call.
package management

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	authFailCooldown  = 60 * time.Second
	forbiddenCooldown = 300 * time.Second
)

// Error is a management API failure with an operator-friendly message.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func errorf(format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...)}
}

// management allowlist: (method, path)
var managementAllow = map[string]map[string]bool{
	"GET":  {"/auth-files": true},
	"POST": {"/api-call": true},
}

// apiCallAllow maps "host\tpath" to the allowed HTTP methods.
var apiCallAllow = map[string]map[string]bool{
	"api.anthropic.com\t/api/oauth/usage":                                             {"GET": true},
	"chatgpt.com\t/backend-api/wham/usage":                                            {"GET": true},
	"chatgpt.com\t/backend-api/wham/rate-limit-reset-credits":                         {"GET": true},
	"chatgpt.com\t/backend-api/wham/rate-limit-reset-credits/consume":                 {"POST": true},
	"api.kimi.com\t/coding/v1/usages":                                                 {"GET": true},
	"cli-chat-proxy.grok.com\t/v1/billing":                                            {"GET": true},
	"daily-cloudcode-pa.googleapis.com\t/v1internal:retrieveUserQuotaSummary":         {"POST": true},
	"daily-cloudcode-pa.sandbox.googleapis.com\t/v1internal:retrieveUserQuotaSummary": {"POST": true},
	"cloudcode-pa.googleapis.com\t/v1internal:retrieveUserQuotaSummary":               {"POST": true},
	"cloudcode-pa.googleapis.com\t/v1internal:retrieveUserQuota":                      {"POST": true},
	"daily-cloudcode-pa.googleapis.com\t/v1internal:loadCodeAssist":                   {"POST": true},
	"daily-cloudcode-pa.sandbox.googleapis.com\t/v1internal:loadCodeAssist":           {"POST": true},
	"cloudcode-pa.googleapis.com\t/v1internal:loadCodeAssist":                         {"POST": true},
}

// AllowedAPICall reports whether the given method+URL is on the quota allowlist.
func AllowedAPICall(method, rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return false
	}
	methods, ok := apiCallAllow[parsed.Hostname()+"\t"+parsed.Path]
	if !ok {
		return false
	}
	return methods[strings.ToUpper(method)]
}

// ManagementRoot normalizes a base URL to the /v0/management root.
func ManagementRoot(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	suffix := "/v0/management"
	if strings.HasSuffix(base, suffix) {
		return base
	}
	return base + suffix
}

// Client talks to one CLIProxyAPI management endpoint.
type Client struct {
	root   string
	key    string
	http   *http.Client
	mu     sync.Mutex
	block  time.Time
	reason string
}

// New builds a management client.
func New(baseURL, key string, timeout float64) *Client {
	if timeout <= 0 {
		timeout = 15
	}
	return &Client{
		root: ManagementRoot(baseURL),
		key:  key,
		http: &http.Client{Timeout: time.Duration(timeout * float64(time.Second))},
	}
}

func (c *Client) ensureReady() error {
	if c.key == "" {
		return errorf("未配置 CPA management_key。")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.block) {
		return &Error{Msg: c.reason}
	}
	return nil
}

func (c *Client) blockFor(d time.Duration, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.block = time.Now().Add(d)
	c.reason = reason
}

func (c *Client) request(ctx context.Context, method, path string, body any, timeout time.Duration) ([]byte, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	verb := strings.ToUpper(method)
	normalized := "/" + strings.Trim(path, "/")
	if !managementAllow[verb][normalized] {
		return nil, errorf("客户端拒绝该管理路径：%s %s", verb, normalized)
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	reqCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(reqCtx, verb, c.root+normalized, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("X-Management-Key", c.key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "QuotaNoa-Client/0.1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errorf("无法连接 CLIProxyAPI：%v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case 401:
		reason := "管理密钥无效（401）。1 分钟内暂停后续请求。"
		c.blockFor(authFailCooldown, reason)
		return nil, &Error{Msg: reason}
	case 403:
		reason := "管理接口拒绝访问（403）。5 分钟内暂停后续请求。"
		c.blockFor(forbiddenCooldown, reason)
		return nil, &Error{Msg: reason}
	}
	if resp.StatusCode >= 400 {
		return nil, errorf("CLIProxyAPI 返回 HTTP %d", resp.StatusCode)
	}
	return data, nil
}

func (c *Client) requestJSON(ctx context.Context, method, path string, body any, timeout time.Duration) (map[string]any, error) {
	data, err := c.request(ctx, method, path, body, timeout)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, errorf("CLIProxyAPI 返回了无法解析的 JSON。")
	}
	return out, nil
}

// ListAuthFiles returns the credential file list.
func (c *Client) ListAuthFiles(ctx context.Context) ([]map[string]any, error) {
	data, err := c.requestJSON(ctx, http.MethodGet, "/auth-files", nil, 0)
	if err != nil {
		return nil, err
	}
	raw, _ := data["files"].([]any)
	files := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			files = append(files, m)
		}
	}
	return files, nil
}

// APICall proxies one allowlisted upstream request through CPA.
func (c *Client) APICall(ctx context.Context, authIndex, method, rawURL string, header map[string]string, data *string, timeout time.Duration) (map[string]any, error) {
	verb := strings.ToUpper(method)
	if !AllowedAPICall(verb, rawURL) {
		return nil, errorf("拒绝调用未列入额度白名单的方法或上游地址。")
	}
	payload := map[string]any{"auth_index": authIndex, "method": verb, "url": rawURL}
	if len(header) > 0 {
		payload["header"] = header
	}
	if data != nil {
		payload["data"] = *data
	}
	return c.requestJSON(ctx, http.MethodPost, "/api-call", payload, timeout)
}

// UpstreamBody extracts and decodes the upstream response body from an
// /api-call result.
func UpstreamBody(result map[string]any) (map[string]any, error) {
	raw, ok := result["body"]
	if !ok {
		return nil, errorf("上游响应缺少 body。")
	}
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return map[string]any{}, nil
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil, errorf("上游 body 不是合法 JSON。")
		}
		return out, nil
	case map[string]any:
		return v, nil
	default:
		return nil, errorf("上游 body 类型无法识别。")
	}
}

// StatusCode returns the upstream status code from an /api-call result.
func StatusCode(result map[string]any) int {
	switch v := result["status_code"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}
