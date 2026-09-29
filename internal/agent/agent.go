// Package agent owns the outbound WebSocket connection to the Bot server,
// including the capability handshake and reconnect/backoff loop.
package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
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
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := a.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			slog.Warn("连接中断", "err", err)
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

func (a *Agent) session(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+a.cfg.Client.Key)
	header.Set("X-CPA-Client-Name", a.cfg.Client.Name)
	header.Set("X-CPA-Client-Protocol", strconv.Itoa(protocol.Version))

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, a.cfg.Client.ServerURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(1 << 20)

	// Read the server hello (best effort) before announcing ourselves.
	readCtx, readCancel := context.WithTimeout(ctx, 10*time.Second)
	_, _, err = conn.Read(readCtx)
	readCancel()
	if err != nil {
		return err
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
		return err
	}
	capabilities := a.svc.Capabilities()
	slog.Info("已连接服务端", "url", a.cfg.Client.ServerURL, "name", a.cfg.Client.Name, "refresh", capabilities.Refresh)

	seen := map[string]bool{}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
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
			return err
		}
	}
}
