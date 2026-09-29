package channels

import (
	"net/http"
	"testing"
	"time"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/config"
)

func serverFixture() config.WorkbuddyServer {
	return config.WorkbuddyServer{Name: "wb-main"}
}

func qoderFixture() config.QoderServer {
	return config.QoderServer{Name: "qoder-main"}
}

func TestParseWorkbuddyAccounts(t *testing.T) {
	server := serverFixture()
	payload := map[string]any{
		"accounts": []any{
			map[string]any{
				"uid":      "u1",
				"nickname": "主号",
				"quotas": map[string]any{
					"p1": map[string]any{"total": 100.0, "used": 20.0, "remaining": 80.0, "recurring": true, "resetAt": "2099-01-01 00:00:00"},
				},
			},
			map[string]any{"uid": "u2", "nickname": "冷却号", "cooling": true, "quotas": nil},
		},
	}
	accounts := parseWorkbuddyAccounts(server, payload)
	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}
	first := accounts[0]
	if first.Name != "主号" || first.Plan != "1 套餐" || len(first.Windows) != 1 {
		t.Fatalf("unexpected first account: %+v", first)
	}
	if first.Windows[0].ID != "wb-credits" || first.Windows[0].RemainingPercent == nil || *first.Windows[0].RemainingPercent != 80 {
		t.Fatalf("unexpected window: %+v", first.Windows[0])
	}
	if accounts[1].Status != "cooling" {
		t.Fatalf("expected cooling account, got %s", accounts[1].Status)
	}
}

func TestParseQoderAccounts(t *testing.T) {
	server := qoderFixture()
	payload := map[string]any{
		"accounts": []any{
			map[string]any{
				"id":        "q1",
				"name":      "Qoder 账号",
				"user_type": "personal_professional",
				"general":   map[string]any{"total": 100.0, "remaining": 60.0},
				"addon":     map[string]any{"total": 0.0, "remaining": 0.0},
				"dedicated": []any{
					map[string]any{"title": "专属包", "total": 50.0, "remaining": 50.0, "available": true},
				},
			},
		},
	}
	accounts := parseQoderAccounts(server, payload)
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	account := accounts[0]
	if account.Plan != "专业版" {
		t.Fatalf("unexpected plan: %s", account.Plan)
	}
	if len(account.Windows) != 2 {
		t.Fatalf("expected 2 windows (general + dedicated), got %d", len(account.Windows))
	}
}

func TestVolcSigningDeterministic(t *testing.T) {
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	req, err := http.NewRequest(http.MethodPost, "https://open.volcengineapi.com/?Action=GetCodingPlanUsage&Region=cn-beijing&Version=2024-01-01", nil)
	if err != nil {
		t.Fatal(err)
	}
	signVolcRequest(req, "AKTEST", "SKTEST", "cn-beijing", "Action=GetCodingPlanUsage&Region=cn-beijing&Version=2024-01-01", nil, fixed)
	first := req.Header.Get("Authorization")
	if first == "" {
		t.Fatal("expected Authorization header")
	}
	req2, _ := http.NewRequest(http.MethodPost, req.URL.String(), nil)
	signVolcRequest(req2, "AKTEST", "SKTEST", "cn-beijing", "Action=GetCodingPlanUsage&Region=cn-beijing&Version=2024-01-01", nil, fixed)
	if req2.Header.Get("Authorization") != first {
		t.Fatal("signature must be deterministic for identical inputs")
	}
	if req.Header.Get("X-Content-Sha256") != volcEmptyBodyHash {
		t.Fatalf("unexpected empty body hash: %s", req.Header.Get("X-Content-Sha256"))
	}
}

func TestCanonicalQuery(t *testing.T) {
	got := canonicalQuery(map[string]string{"Version": "2024-01-01", "Action": "GetAFPUsage", "Region": "cn-beijing"})
	want := "Action=GetAFPUsage&Region=cn-beijing&Version=2024-01-01"
	if got != want {
		t.Fatalf("canonicalQuery = %q, want %q", got, want)
	}
}
