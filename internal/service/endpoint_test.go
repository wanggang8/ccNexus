package service

import (
	"net/http"
	"strings"
	"testing"
)

func TestBuildCLIProbeRequestUsesCLIPathAndPayload(t *testing.T) {
	reqURL, body, headers, err := buildCLIProbeRequest("https://example.com", "test-key", "claude-sonnet-4-5-20250929", "ping", 1)
	if err != nil {
		t.Fatalf("buildCLIProbeRequest failed: %v", err)
	}
	if reqURL != "https://example.com/v1/messages?beta=true" {
		t.Fatalf("expected CLI path https://example.com/v1/messages?beta=true, got %q", reqURL)
	}
	if headers["x-app"] != "cli" {
		t.Fatalf("expected x-app=cli, got %q", headers["x-app"])
	}
	if !strings.Contains(headers["anthropic-beta"], "claude-code-20250219") {
		t.Fatalf("expected anthropic beta header, got %q", headers["anthropic-beta"])
	}
	if !strings.Contains(string(body), `"metadata"`) || !strings.Contains(string(body), `"system"`) {
		t.Fatalf("expected CLI-shaped request body, got %s", string(body))
	}
}

func TestApplyModelRequestHeadersUsesCLIHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.com/v1/models", nil)
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	applyModelRequestHeaders(req, "test-key", "cli")
	if req.Header.Get("x-app") != "cli" {
		t.Fatalf("expected x-app=cli, got %q", req.Header.Get("x-app"))
	}
	if req.Header.Get("x-api-key") != "test-key" {
		t.Fatalf("expected x-api-key to be forwarded, got %q", req.Header.Get("x-api-key"))
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("did not expect bearer auth for cli models request, got %q", req.Header.Get("Authorization"))
	}
}
