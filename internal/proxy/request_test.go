package proxy

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
)

func TestEnsureCodexResponsesPayload(t *testing.T) {
	raw := []byte(`{"model":"gpt-4.1","stream":true}`)
	out := ensureCodexResponsesPayload(raw)

	var payload map[string]interface{}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	store, ok := payload["store"].(bool)
	if !ok || store {
		t.Fatalf("expected store=false, got %#v", payload["store"])
	}
	stream, ok := payload["stream"].(bool)
	if !ok || !stream {
		t.Fatalf("expected stream=true, got %#v", payload["stream"])
	}
	if instructions, ok := payload["instructions"].(string); !ok || instructions != "" {
		t.Fatalf("expected instructions empty string, got %#v", payload["instructions"])
	}
}

func TestEnsureCodexResponsesPayloadOverridesStoreAndStream(t *testing.T) {
	raw := []byte(`{"model":"gpt-4.1","store":true}`)
	out := ensureCodexResponsesPayload(raw)

	var payload map[string]interface{}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	store, ok := payload["store"].(bool)
	if !ok || store {
		t.Fatalf("expected store=false, got %#v", payload["store"])
	}
	stream, ok := payload["stream"].(bool)
	if !ok || !stream {
		t.Fatalf("expected stream=true, got %#v", payload["stream"])
	}
}

func TestNormalizeTargetPathForBaseURLOnCodexBackend(t *testing.T) {
	got := normalizeTargetPathForBaseURL("https://chatgpt.com/backend-api/codex", "/v1/responses")
	if got != "/responses" {
		t.Fatalf("expected /responses, got %s", got)
	}
}

func TestOverrideModelInPayload(t *testing.T) {
	raw := []byte(`{"model":"gpt-5.3-codex","stream":true}`)
	out := overrideModelInPayload(raw, "gpt-5.2-codex")

	var payload map[string]interface{}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if payload["model"] != "gpt-5.2-codex" {
		t.Fatalf("expected model override to gpt-5.2-codex, got %#v", payload["model"])
	}
}

func TestShouldHandleAsStreamingResponseForCodexWithoutContentType(t *testing.T) {
	endpoint := config.Endpoint{
		Name:        "TokenPool",
		APIUrl:      "https://chatgpt.com/backend-api/codex",
		Transformer: "openai2",
	}
	if !shouldHandleAsStreamingResponse("", true, endpoint, "cx_chat_openai2") {
		t.Fatal("expected stream=true Codex response with empty content-type to be treated as streaming")
	}
	if shouldHandleAsStreamingResponse("", false, endpoint, "cx_chat_openai2") {
		t.Fatal("expected non-stream client request to not be treated as streaming when content-type is empty")
	}
	if !shouldHandleAsStreamingResponse("text/event-stream", false, endpoint, "cx_chat_openai2") {
		t.Fatal("expected text/event-stream content-type to be treated as streaming")
	}
}

func TestBuildProxyRequestForCLIUsesBetaPathAndHeaders(t *testing.T) {
	r, err := http.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions?trace=1", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}

	endpoint := config.Endpoint{
		Name:        "CLI",
		APIUrl:      "https://api.anthropic.com",
		APIKey:      "test-key",
		Transformer: "cli",
		Model:       "claude-sonnet-4-20250514",
		Enabled:     true,
	}
	body := []byte(`{"stream":true,"tools":[{"name":"read_file"}]}`)

	req, err := buildProxyRequest(r, endpoint, "test-key", body, "cx_chat_cli", nil)
	if err != nil {
		t.Fatalf("buildProxyRequest failed: %v", err)
	}

	if got := req.URL.String(); got != "https://api.anthropic.com/v1/messages?beta=true&trace=1" {
		t.Fatalf("expected CLI URL with beta query, got %s", got)
	}
	if got := req.Header.Get("x-app"); got != "cli" {
		t.Fatalf("expected x-app=cli, got %q", got)
	}
	if beta := req.Header.Get("anthropic-beta"); beta == "" {
		t.Fatal("expected anthropic-beta header")
	}
	if got := req.Header.Get("x-api-key"); got != "test-key" {
		t.Fatalf("expected x-api-key=test-key, got %q", got)
	}
}
