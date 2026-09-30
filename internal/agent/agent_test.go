package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/config"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/protocol"
)

// TestRefreshDisabledOverWebSocket verifies the end-to-end anti-forgery gate:
// even when a (possibly hostile) server sends codex.refresh, a client with
// refresh.enabled=false answers ok:false without any upstream I/O.
func TestRefreshDisabledOverWebSocket(t *testing.T) {
	serverDone := make(chan struct{})
	var refreshErr string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		serverHello, _ := json.Marshal(protocol.Envelope{
			Version: protocol.Version,
			Type:    protocol.TypeHello,
			ID:      "",
			Payload: json.RawMessage(`{"server_name":"Server","protocol_version":2}`),
		})
		if err := conn.Write(ctx, websocket.MessageText, serverHello); err != nil {
			return
		}
		_, helloData, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var clientHello protocol.Envelope
		if err := json.Unmarshal(helloData, &clientHello); err != nil || clientHello.Type != protocol.TypeHello {
			t.Errorf("expected client hello, got %s (%v)", clientHello.Type, err)
			return
		}

		payload, _ := json.Marshal(protocol.CodexRefreshPayload{Account: "user@example.com"})
		req, _ := json.Marshal(protocol.Envelope{
			Version: protocol.Version,
			Type:    protocol.TypeRequest,
			ID:      "req-1",
			Action:  protocol.ActionCodexRefresh,
			Payload: payload,
		})
		if err := conn.Write(ctx, websocket.MessageText, req); err != nil {
			return
		}
		_, replyData, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var reply protocol.Envelope
		if err := json.Unmarshal(replyData, &reply); err != nil {
			t.Errorf("bad reply: %v", err)
			return
		}
		if reply.OK == nil || *reply.OK {
			t.Errorf("expected ok=false, got %v", reply.OK)
		}
		refreshErr = reply.Error
		close(serverDone)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	cfg := config.Default()
	cfg.Client.Name = "Home"
	cfg.Client.Key = "key"
	cfg.Client.ServerURL = "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/client/ws"
	cfg.Refresh.Enabled = false

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	go func() { _ = New(cfg).Run(ctx) }()

	select {
	case <-serverDone:
	case <-ctx.Done():
		t.Fatal("timed out waiting for end-to-end refresh rejection")
	}
	if !strings.Contains(refreshErr, "未启用") {
		t.Fatalf("unexpected rejection message: %q", refreshErr)
	}
}

// TestDialErrorSurfacesServerReason verifies a rejected handshake surfaces both
// the HTTP status and the reason body the server sent (e.g. "客户端未注册").
func TestDialErrorSurfacesServerReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "客户端未注册", http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Client.Name = "Local"
	cfg.Client.Key = "secret"
	cfg.Client.ServerURL = "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/client/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connected, err := New(cfg).session(ctx)
	if connected {
		t.Fatal("expected the handshake to be rejected")
	}
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "403") {
		t.Fatalf("missing HTTP status: %q", msg)
	}
	if !strings.Contains(msg, "客户端未注册") {
		t.Fatalf("missing server-provided reason: %q", msg)
	}
	if !strings.Contains(msg, "client list") {
		t.Fatalf("missing actionable hint: %q", msg)
	}
}

// TestDialErrorUnreachableServer covers the transport-level failure path.
func TestDialErrorUnreachableServer(t *testing.T) {
	cfg := config.Default()
	cfg.Client.ServerURL = "ws://127.0.0.1:1/v1/client/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := New(cfg).session(ctx)
	if err == nil {
		t.Fatal("expected a connection error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "无法连接服务端") {
		t.Fatalf("unexpected message: %q", msg)
	}
	if !strings.Contains(msg, "server on") {
		t.Fatalf("missing enable hint: %q", msg)
	}
}

// TestDescribeSessionErrorCloseReason checks server-initiated closes are
// reported with their code and reason instead of a bare transport error.
func TestDescribeSessionErrorCloseReason(t *testing.T) {
	err := describeSessionError(websocket.CloseError{
		Code:   websocket.StatusPolicyViolation,
		Reason: "客户端未注册",
	})
	msg := err.Error()
	if !strings.Contains(msg, "1008") || !strings.Contains(msg, "客户端未注册") {
		t.Fatalf("missing close code/reason: %q", msg)
	}
	if !strings.Contains(msg, "client.key") {
		t.Fatalf("expected a policy-violation hint: %q", msg)
	}

	// An empty reason falls back to a human-readable status text.
	err = describeSessionError(websocket.CloseError{Code: websocket.StatusGoingAway})
	if !strings.Contains(err.Error(), "服务端正在关闭") {
		t.Fatalf("expected status text fallback: %q", err.Error())
	}

	// Ordinary errors keep their cause.
	err = describeSessionError(io.ErrUnexpectedEOF)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("cause must be preserved: %v", err)
	}
	if !strings.Contains(err.Error(), "连接中断") {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}
