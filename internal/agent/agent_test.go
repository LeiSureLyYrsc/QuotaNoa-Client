package agent

import (
	"context"
	"encoding/json"
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
