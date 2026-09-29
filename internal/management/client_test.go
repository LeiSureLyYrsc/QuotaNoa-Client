package management

import "testing"

func TestAllowedAPICall(t *testing.T) {
	cases := []struct {
		method string
		url    string
		want   bool
	}{
		{"GET", "https://chatgpt.com/backend-api/wham/usage", true},
		{"POST", "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume", true},
		{"GET", "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits", true},
		{"GET", "https://api.anthropic.com/api/oauth/usage", true},
		{"GET", "https://cli-chat-proxy.grok.com/v1/billing?format=credits", true},
		{"POST", "https://chatgpt.com/backend-api/wham/usage", false},
		{"GET", "http://chatgpt.com/backend-api/wham/usage", false},
		{"GET", "https://evil.example.com/api/oauth/usage", false},
	}
	for _, tc := range cases {
		if got := AllowedAPICall(tc.method, tc.url); got != tc.want {
			t.Errorf("AllowedAPICall(%q, %q) = %v, want %v", tc.method, tc.url, got, tc.want)
		}
	}
}

func TestManagementRoot(t *testing.T) {
	if got := ManagementRoot("http://127.0.0.1:8317/"); got != "http://127.0.0.1:8317/v0/management" {
		t.Errorf("unexpected root: %s", got)
	}
	if got := ManagementRoot("http://127.0.0.1:8317/v0/management"); got != "http://127.0.0.1:8317/v0/management" {
		t.Errorf("unexpected root: %s", got)
	}
}

func TestUpstreamBody(t *testing.T) {
	body, err := UpstreamBody(map[string]any{"status_code": float64(200), "body": `{"ok":true}`})
	if err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true {
		t.Fatalf("unexpected body: %v", body)
	}
	if _, err := UpstreamBody(map[string]any{"status_code": float64(200)}); err == nil {
		t.Fatal("expected error for missing body")
	}
}
