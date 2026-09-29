package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/config"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/protocol"
)

func newService(refresh bool) *Service {
	cfg := config.Default()
	cfg.Client.Name = "Home"
	cfg.Client.Key = "key"
	cfg.Refresh.Enabled = refresh
	return New(cfg)
}

func decode(t *testing.T, reply []byte) protocol.Envelope {
	t.Helper()
	var env protocol.Envelope
	if err := json.Unmarshal(reply, &env); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	return env
}

func TestRefreshDisabledRejectedWithZeroSideEffect(t *testing.T) {
	svc := newService(false)
	payload, _ := json.Marshal(protocol.CodexRefreshPayload{Account: "user@example.com"})
	req, _ := json.Marshal(protocol.Envelope{
		Version: protocol.Version,
		Type:    protocol.TypeRequest,
		ID:      "1",
		Action:  protocol.ActionCodexRefresh,
		Payload: payload,
	})
	reply, ok := svc.Handle(context.Background(), req)
	if !ok || reply == nil {
		t.Fatal("expected a reply")
	}
	env := decode(t, reply)
	if env.OK == nil || *env.OK {
		t.Fatalf("expected ok=false, got %v", env.OK)
	}
	if env.Error == "" {
		t.Fatal("expected an error message")
	}
}

func TestUnknownActionRejected(t *testing.T) {
	svc := newService(false)
	req, _ := json.Marshal(protocol.Envelope{Version: protocol.Version, Type: protocol.TypeRequest, ID: "2", Action: "nope"})
	reply, ok := svc.Handle(context.Background(), req)
	if !ok {
		t.Fatal("expected a reply")
	}
	env := decode(t, reply)
	if env.OK == nil || *env.OK {
		t.Fatal("expected ok=false for unknown action")
	}
}

func TestWrongVersionRejected(t *testing.T) {
	svc := newService(false)
	req, _ := json.Marshal(protocol.Envelope{Version: 1, Type: protocol.TypeRequest, ID: "3", Action: protocol.ActionQuotaQuery})
	reply, ok := svc.Handle(context.Background(), req)
	if !ok {
		t.Fatal("expected a reply")
	}
	env := decode(t, reply)
	if env.OK == nil || *env.OK {
		t.Fatal("expected ok=false for wrong version")
	}
}

func TestNonRequestIgnored(t *testing.T) {
	svc := newService(false)
	req, _ := json.Marshal(protocol.Envelope{Version: protocol.Version, Type: protocol.TypeResponse, ID: "4"})
	if _, ok := svc.Handle(context.Background(), req); ok {
		t.Fatal("responses must be ignored")
	}
}

func TestHelloCapabilities(t *testing.T) {
	if newService(false).Hello().Capabilities.Refresh {
		t.Fatal("refresh must default to false")
	}
	if !newService(true).Hello().Capabilities.Refresh {
		t.Fatal("refresh must reflect local config")
	}
	channels := newService(false).Hello().Capabilities.Channels
	if len(channels) == 0 {
		t.Fatal("expected advertised channels")
	}
}
