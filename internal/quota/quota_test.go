package quota

import "testing"

func TestParseCodexUsage(t *testing.T) {
	body := map[string]any{
		"rate_limit": map[string]any{
			"primary_window":   map[string]any{"used_percent": 14.0, "limit_window_seconds": 18000.0},
			"secondary_window": map[string]any{"used_percent": 40.0, "limit_window_seconds": 604800.0},
		},
	}
	windows := parseCodexUsage(body)
	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(windows))
	}
	if windows[0].ID != "code-5h" || windows[1].ID != "code-7d" {
		t.Fatalf("unexpected window ids: %s / %s", windows[0].ID, windows[1].ID)
	}
	if windows[0].RemainingPercent == nil || *windows[0].RemainingPercent != 86 {
		t.Fatalf("unexpected remaining: %v", windows[0].RemainingPercent)
	}
}

func TestParseCodexLimitReached(t *testing.T) {
	body := map[string]any{
		"rate_limit": map[string]any{
			"limit_reached":    true,
			"primary_window":   map[string]any{"used_percent": 10.0, "limit_window_seconds": 18000.0},
			"secondary_window": nil,
		},
	}
	windows := parseCodexUsage(body)
	if len(windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(windows))
	}
	if windows[0].UsedPercent == nil || *windows[0].UsedPercent != 100 {
		t.Fatalf("expected 100%% used, got %v", windows[0].UsedPercent)
	}
}

func TestMatchAuth(t *testing.T) {
	files := []map[string]any{
		{"auth_index": "abc123", "name": "user@example.com.json", "email": "user@example.com"},
		{"auth_index": "def456", "name": "other@example.com.json"},
	}
	if got := MatchAuth(files, "abc123"); len(got) != 1 || str(got[0]["auth_index"]) != "abc123" {
		t.Fatalf("exact match failed: %v", got)
	}
	if got := MatchAuth(files, "user@example.com"); len(got) != 1 {
		t.Fatalf("expected 1 match, got %d", len(got))
	}
	if got := MatchAuth(files, "zzz"); len(got) != 0 {
		t.Fatalf("expected no match, got %d", len(got))
	}
}

func TestParseTimestamp(t *testing.T) {
	if ts := parseTimestamp(1800000000.0); ts == nil || *ts != 1800000000.0 {
		t.Fatalf("epoch seconds failed: %v", ts)
	}
	if ts := parseTimestamp(1800000000000.0); ts == nil || *ts != 1800000000.0 {
		t.Fatalf("epoch ms failed: %v", ts)
	}
	if ts := parseTimestamp("2026-01-01T00:00:00Z"); ts == nil {
		t.Fatal("iso failed")
	}
}

func TestParseXAIBilling(t *testing.T) {
	body := map[string]any{
		"config": map[string]any{"creditUsagePercent": 20.0},
		"productUsage": map[string]any{
			"GrokBuild": map[string]any{"usagePercent": 10.0},
		},
	}
	windows := parseXAIBilling(body)
	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(windows))
	}
	found := false
	for _, w := range windows {
		if w.ID == "grok-grokbuild" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected grok-grokbuild window, got %+v", windows)
	}
}

func TestCountResetCredits(t *testing.T) {
	body := map[string]any{
		"credits": []any{
			map[string]any{"credit_id": "a", "status": "available"},
			map[string]any{"credit_id": "b", "status": "redeemed"},
			map[string]any{"credit_id": "c", "status": "available"},
		},
	}
	credits := countResetCredits(body)
	if credits == nil || *credits != 2 {
		t.Fatalf("expected 2 available credits, got %v", credits)
	}
}
