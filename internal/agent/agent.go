// Package agent owns the outbound WebSocket connection to the Bot server,
// including the capability handshake and reconnect/backoff loop.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/config"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/protocol"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/service"
)

// Agent connects to the server and answers protocol requests.
type Agent struct {
	cfg *config.Config
	svc *service.Service
}

// New builds an agent.
func New(cfg *config.Config) *Agent {
	return &Agent{cfg: cfg, svc: service.New(cfg)}
}

// Run connects forever with exponential backoff until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	backoff := a.cfg.Reconnect.Min
	everConnected := false
	hinted := false
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		connected, err := a.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if connected {
			everConnected = true
		}
		if err != nil {
			slog.Warn("连接中断", "err", err)
			// 首次就连不上时给出一次性排查清单，避免每次重连都刷屏。
			if !everConnected && !hinted {
				hinted = true
				for _, line := range a.troubleshooting() {
					slog.Warn(line)
				}
			}
		}
		delay := backoff + rand.Float64()*(backoff/4)
		slog.Info("等待重连", "seconds", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(delay * float64(time.Second))):
		}
		backoff = min(a.cfg.Reconnect.Max, backoff*2)
		if backoff < a.cfg.Reconnect.Min {
			backoff = a.cfg.Reconnect.Min
		}
	}
}

// session runs one connection lifetime. It reports whether the handshake
// completed (so callers can distinguish "never reachable" from "dropped").
func (a *Agent) session(ctx context.Context) (bool, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+a.cfg.Client.Key)
	header.Set("X-CPA-Client-Name", a.cfg.Client.Name)
	header.Set("X-CPA-Client-Protocol", strconv.Itoa(protocol.Version))

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(dialCtx, a.cfg.Client.ServerURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return false, describeDialError(err, resp, a.cfg)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(1 << 20)

	// Read the server hello (best effort) before announcing ourselves.
	readCtx, readCancel := context.WithTimeout(ctx, 10*time.Second)
	_, _, err = conn.Read(readCtx)
	readCancel()
	if err != nil {
		return true, describeSessionError(err)
	}

	helloPayload, _ := json.Marshal(a.svc.Hello())
	helloEnv := protocol.Envelope{
		Version: protocol.Version,
		Type:    protocol.TypeHello,
		ID:      "",
		Payload: helloPayload,
	}
	helloBytes, _ := json.Marshal(helloEnv)
	if err := conn.Write(ctx, websocket.MessageText, helloBytes); err != nil {
		return true, describeSessionError(err)
	}
	capabilities := a.svc.Capabilities()
	slog.Info("已连接服务端", "url", a.cfg.Client.ServerURL, "name", a.cfg.Client.Name, "refresh", capabilities.Refresh)

	seen := map[string]bool{}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return true, describeSessionError(err)
		}
		var env protocol.Envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		if env.ID != "" {
			if seen[env.ID] {
				continue
			}
			seen[env.ID] = true
			if len(seen) > 1024 {
				seen = map[string]bool{env.ID: true}
			}
		}
		reply, ok := a.svc.Handle(ctx, data)
		if !ok || len(reply) == 0 {
			continue
		}
		if err := conn.Write(ctx, websocket.MessageText, reply); err != nil {
			return true, describeSessionError(err)
		}
	}
}

// troubleshooting returns one-time hints for a first-attempt failure.
func (a *Agent) troubleshooting() []string {
	cfg := a.cfg
	return []string{
		"连接失败排查清单：",
		"  1) 服务端是否已启用？在 Bot 侧发送 /quotanoa client server on（地址 " + cfg.Client.ServerURL + "）",
		"  2) 客户端是否已注册？/quotanoa client list 应包含名称「" + cfg.Client.Name + "」",
		"  3) 密钥是否一致？比对 /quotanoa client show " + cfg.Client.Name + " 与 client.key" +
			"（轮换后需同步客户端：/quotanoa client key " + cfg.Client.Name + " --rotate）",
		"  4) 名称是否被占用？服务端保留名不可用，同一名称也不能同时在线（含多开的客户端进程）",
	}
}

// dialError describes a rejected handshake with an actionable hint.
type dialError struct {
	// status is the HTTP status returned during the handshake (0 when the
	// connection never reached the server).
	status int
	// reason is the server-provided explanation (响应体) or the transport error.
	reason string
	// hint is an actionable suggestion for the operator.
	hint string
}

func (e *dialError) Error() string {
	var b strings.Builder
	if e.status > 0 {
		fmt.Fprintf(&b, "服务端拒绝连接：HTTP %d", e.status)
		if text := http.StatusText(e.status); text != "" {
			b.WriteString(" " + text)
		}
		if e.reason != "" {
			b.WriteString("：" + e.reason)
		}
	} else {
		b.WriteString("无法连接服务端")
		if e.reason != "" {
			b.WriteString("：" + e.reason)
		}
	}
	if e.hint != "" {
		b.WriteString("；提示：" + e.hint)
	}
	return b.String()
}

// describeDialError turns a failed handshake into an actionable message,
// surfacing the HTTP status and the reason body the server sent.
func describeDialError(err error, resp *http.Response, cfg *config.Config) error {
	if resp == nil {
		return &dialError{
			reason: err.Error(),
			hint: "确认服务端已启用（在 Bot 侧发送 /quotanoa client server on）、" +
				"地址与端口正确且本机可达：" + cfg.Client.ServerURL,
		}
	}
	status := resp.StatusCode
	reason := readResponseBody(resp)
	hint := ""
	switch status {
	case http.StatusForbidden:
		hint = fmt.Sprintf(
			"服务端注册表里没有该客户端或密钥不符：/quotanoa client list 应包含「%s」，"+
				"并用 /quotanoa client show %s 与 client.key 比对",
			cfg.Client.Name, cfg.Client.Name,
		)
	case http.StatusNotFound:
		hint = "server_url 路径应为 /v1/client/ws（例：ws://127.0.0.1:8320/v1/client/ws）"
	}
	return &dialError{status: status, reason: reason, hint: hint}
}

// describeSessionError adds context to errors raised on an established
// connection (server-initiated close, protocol violations, ...).
func describeSessionError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var closeErr websocket.CloseError
	if errors.As(err, &closeErr) {
		reason := strings.TrimSpace(closeErr.Reason)
		if reason == "" {
			reason = closeStatusText(closeErr.Code)
		}
		message := fmt.Sprintf("连接被服务端关闭：%d %s", int(closeErr.Code), reason)
		if closeErr.Code == websocket.StatusPolicyViolation {
			message += "；提示：多为鉴权或协议不符，检查 client.key、客户端名称是否被占用" +
				"（/quotanoa client list 可看在线状态）"
		}
		return errors.New(message)
	}
	return fmt.Errorf("连接中断：%w", err)
}

func closeStatusText(code websocket.StatusCode) string {
	switch code {
	case websocket.StatusNormalClosure:
		return "正常关闭"
	case websocket.StatusGoingAway:
		return "服务端正在关闭"
	case websocket.StatusProtocolError:
		return "协议错误"
	case websocket.StatusPolicyViolation:
		return "策略拒绝"
	case websocket.StatusMessageTooBig:
		return "消息过大"
	case websocket.StatusInternalError:
		return "服务端内部错误"
	default:
		return "未提供原因"
	}
}

// readResponseBody reads the (bounded) handshake failure body, if any.
// coder/websocket buffers at most 1024 bytes for failed handshakes.
func readResponseBody(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
