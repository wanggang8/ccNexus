package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	newcursor "github.com/lich0821/ccNexus/internal/cursorbridge"
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

	req, err := buildProxyRequest(r, endpoint, "test-key", body, "cx_chat_cli", nil, nil)
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

func TestBuildProxyRequestForAnthropicUsesAPIKeyHeaderForSKKeys(t *testing.T) {
	r, err := http.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}

	endpoint := config.Endpoint{
		Name:        "Claude",
		APIUrl:      "https://api.anthropic.com",
		APIKey:      "sk-ant-test",
		Transformer: "claude",
		Model:       "claude-sonnet-4-20250514",
		Enabled:     true,
	}

	req, err := buildProxyRequest(r, endpoint, endpoint.APIKey, []byte(`{"messages":[]}`), "cx_chat_claude", nil, nil)
	if err != nil {
		t.Fatalf("buildProxyRequest failed: %v", err)
	}

	if got := req.Header.Get("anthropic-version"); got == "" {
		t.Fatal("expected anthropic-version header")
	}
	if got := req.Header.Get("x-api-key"); got != "sk-ant-test" {
		t.Fatalf("expected x-api-key=sk-ant-test, got %q", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("did not expect bearer auth for sk-* key, got %q", got)
	}
}

func TestBuildProxyRequestForAnthropicUsesBearerHeaderForNonSKKeys(t *testing.T) {
	r, err := http.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}

	endpoint := config.Endpoint{
		Name:        "Claude",
		APIUrl:      "https://api.anthropic.com",
		APIKey:      "session-token",
		Transformer: "claude",
		Model:       "claude-sonnet-4-20250514",
		Enabled:     true,
	}

	req, err := buildProxyRequest(r, endpoint, endpoint.APIKey, []byte(`{"messages":[]}`), "cx_chat_claude", nil, nil)
	if err != nil {
		t.Fatalf("buildProxyRequest failed: %v", err)
	}

	if got := req.Header.Get("anthropic-version"); got == "" {
		t.Fatal("expected anthropic-version header")
	}
	if got := req.Header.Get("Authorization"); got != "Bearer session-token" {
		t.Fatalf("expected bearer auth, got %q", got)
	}
	if got := req.Header.Get("x-api-key"); got != "" {
		t.Fatalf("did not expect x-api-key for non sk-* token, got %q", got)
	}
}

func TestBuildProxyRequestForGeminiUsesV1ModelsAndGoogleAPIKeyHeader(t *testing.T) {
	r, err := http.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions?trace=1", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}

	endpoint := config.Endpoint{
		Name:        "Gemini",
		APIUrl:      "https://generativelanguage.googleapis.com",
		APIKey:      "AIza-test-key",
		Transformer: "gemini",
		Model:       "gemini-2.5-pro",
		Enabled:     true,
	}

	req, err := buildProxyRequest(r, endpoint, endpoint.APIKey, []byte(`{"stream":true}`), "cx_chat_gemini", nil, nil)
	if err != nil {
		t.Fatalf("buildProxyRequest failed: %v", err)
	}

	if got := req.URL.String(); got != "https://generativelanguage.googleapis.com/v1/models/gemini-2.5-pro:streamGenerateContent?alt=sse&trace=1" {
		t.Fatalf("expected Gemini stream URL with v1 models path, got %s", got)
	}
	if got := req.Header.Get("x-goog-api-key"); got != endpoint.APIKey {
		t.Fatalf("expected x-goog-api-key=%s, got %q", endpoint.APIKey, got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("expected Authorization to be empty for Google API key auth, got %q", got)
	}
}

func TestBuildProxyRequestForGeminiNonStreamOmitsAltSSE(t *testing.T) {
	r, err := http.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}

	endpoint := config.Endpoint{
		Name:        "Gemini",
		APIUrl:      "https://generativelanguage.googleapis.com",
		APIKey:      "bearer-token",
		Transformer: "gemini",
		Model:       "gemini-2.5-pro",
		Enabled:     true,
	}

	req, err := buildProxyRequest(r, endpoint, endpoint.APIKey, []byte(`{"stream":false}`), "cx_resp_gemini", nil, nil)
	if err != nil {
		t.Fatalf("buildProxyRequest failed: %v", err)
	}

	if got := req.URL.String(); got != "https://generativelanguage.googleapis.com/v1/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("expected Gemini non-stream URL without alt=sse, got %s", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer bearer-token" {
		t.Fatalf("expected bearer auth for non-Google Gemini key, got %q", got)
	}
	if got := req.Header.Get("x-goog-api-key"); got != "" {
		t.Fatalf("expected x-goog-api-key to be empty for bearer auth, got %q", got)
	}
}

func TestBuildProxyRequestForCursorOpenAIUsesSanitizedHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "http://localhost/cursor/v1/chat/completions", nil)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("OpenAI-Previous-Response-ID", "resp_123")
	r.Header.Set("X-Cursor-Debug", "keep-me-out")

	endpoint := config.Endpoint{
		Name:        "OpenAI",
		APIUrl:      "https://api.openai.com",
		APIKey:      "sk-test",
		Transformer: "openai",
		Model:       "gpt-5",
		Enabled:     true,
	}
	meta := &proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode: true,
		},
	}

	req, err := buildProxyRequest(r, endpoint, endpoint.APIKey, []byte(`{"messages":[]}`), "cx_chat_openai", nil, meta)
	if err != nil {
		t.Fatalf("buildProxyRequest failed: %v", err)
	}

	if got := req.Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Fatalf("expected Authorization bearer header, got %q", got)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", got)
	}
	if got := req.Header.Get("Accept-Encoding"); got != "gzip, identity" {
		t.Fatalf("expected Accept-Encoding gzip, identity, got %q", got)
	}
	if got := req.Header.Get("OpenAI-Previous-Response-ID"); got != "" {
		t.Fatalf("expected cursor request to drop client continuation header, got %q", got)
	}
	if got := req.Header.Get("X-Cursor-Debug"); got != "" {
		t.Fatalf("expected cursor request to drop arbitrary client header, got %q", got)
	}
}

func TestBuildProxyRequestForOrdinaryOpenAIPreservesClientHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", nil)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Cursor-Debug", "preserve-me")

	endpoint := config.Endpoint{
		Name:        "OpenAI",
		APIUrl:      "https://api.openai.com",
		APIKey:      "sk-test",
		Transformer: "openai",
		Model:       "gpt-5",
		Enabled:     true,
	}

	req, err := buildProxyRequest(r, endpoint, endpoint.APIKey, []byte(`{"messages":[]}`), "cx_chat_openai", nil, nil)
	if err != nil {
		t.Fatalf("buildProxyRequest failed: %v", err)
	}

	if got := req.Header.Get("X-Cursor-Debug"); got != "preserve-me" {
		t.Fatalf("expected ordinary request to preserve client header, got %q", got)
	}
}

func TestBuildProxyRequestClosesPlainHTTPStreamingUpstreamConnections(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "http://localhost/cursor/v1/chat/completions", nil)
	r.Header.Set("Content-Type", "application/json")

	endpoint := config.Endpoint{
		Name:        "ClaudeGateway",
		APIUrl:      "http://160.187.211.168:8080",
		APIKey:      "sk-test",
		Transformer: "claude",
		Model:       "claude-sonnet-4-20250514",
		Enabled:     true,
	}
	meta := &proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode: true,
		},
	}

	req, err := buildProxyRequest(r, endpoint, endpoint.APIKey, []byte(`{"messages":[],"stream":true}`), "cx_chat_claude", nil, meta)
	if err != nil {
		t.Fatalf("buildProxyRequest failed: %v", err)
	}

	if !req.Close {
		t.Fatal("expected plain HTTP streaming upstream request to disable keep-alive")
	}
	if got := req.Header.Get("Connection"); got != "close" {
		t.Fatalf("expected Connection close header, got %q", got)
	}
}

func TestShouldCloseUpstreamConnectionOnlyForPlainHTTPStreaming(t *testing.T) {
	httpURL, _ := url.Parse("http://example.test/v1/messages")
	httpsURL, _ := url.Parse("https://example.test/v1/messages")

	if !shouldCloseUpstreamConnection(httpURL, []byte(`{"stream":true}`)) {
		t.Fatal("expected plain HTTP streaming request to close upstream connection")
	}
	if shouldCloseUpstreamConnection(httpURL, []byte(`{"stream":false}`)) {
		t.Fatal("expected plain HTTP non-stream request to keep upstream connection reusable")
	}
	if shouldCloseUpstreamConnection(httpsURL, []byte(`{"stream":true}`)) {
		t.Fatal("expected HTTPS streaming request to keep upstream connection reusable")
	}
}

func TestGetOrCreateProxyClientReusesClientByProxyURL(t *testing.T) {
	cfg := &config.Config{}
	p := New(cfg, nil, nil, "test-device")

	client1, err := p.getOrCreateProxyClient("http://127.0.0.1:8080", p.httpClient)
	if err != nil {
		t.Fatalf("getOrCreateProxyClient first call failed: %v", err)
	}
	client2, err := p.getOrCreateProxyClient("http://127.0.0.1:8080", p.httpClient)
	if err != nil {
		t.Fatalf("getOrCreateProxyClient second call failed: %v", err)
	}
	if client1 != client2 {
		t.Fatal("expected proxy client to be reused for same proxy URL")
	}
}

func TestCreateProxyTransportSupportsSocks5WithDialContext(t *testing.T) {
	transport, err := CreateProxyTransport("socks5://127.0.0.1:1080")
	if err != nil {
		t.Fatalf("CreateProxyTransport failed: %v", err)
	}
	if transport.DialContext == nil {
		t.Fatal("expected SOCKS5 transport to configure DialContext")
	}
}
