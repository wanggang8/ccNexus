package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/transformer/augment"
)

func TestParseAugmentRequest_PlainAugmentRequest(t *testing.T) {
	s := &Server{config: config.DefaultConfig()}
	streamFalse := false
	raw, _ := json.Marshal(augment.AugmentRequest{
		Model:   "claude-3-5-sonnet-20241022",
		Message: "hi",
		Stream:  &streamFalse,
	})

	ar, in, err := s.parseAugmentRequest(raw)
	if err != nil {
		t.Fatalf("parseAugmentRequest: %v", err)
	}
	if ar.Message != "hi" {
		t.Fatalf("expected message hi, got %q", ar.Message)
	}
	if len(in) == 0 {
		t.Fatalf("expected non-empty input bytes")
	}
	if ar.IsStreaming() {
		t.Fatalf("expected stream=false")
	}
}

func TestParseAugmentRequest_PlaintextDataFallback(t *testing.T) {
	s := &Server{config: config.DefaultConfig()}
	raw := []byte(`{"model":"claude-3-5-sonnet-20241022","data":"hello","images":[]}`)

	ar, in, err := s.parseAugmentRequest(raw)
	if err != nil {
		t.Fatalf("parseAugmentRequest: %v", err)
	}
	if ar.Message != "hello" {
		t.Fatalf("expected message hello, got %q", ar.Message)
	}
	if len(in) == 0 {
		t.Fatalf("expected non-empty input bytes")
	}
	// ReconstructFromPlaintext defaults stream to true
	if !ar.IsStreaming() {
		t.Fatalf("expected stream default true")
	}
}

func TestParseAugmentRequest_InvalidBody(t *testing.T) {
	s := &Server{config: config.DefaultConfig()}
	raw := []byte(`{"not_json":`)

	_, _, err := s.parseAugmentRequest(raw)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestSelectTarget_MapsByEndpointTransformer(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Endpoints = []config.Endpoint{
		{Name: "disabled", APIUrl: "https://api.anthropic.com", APIKey: "k1", Enabled: false, Transformer: "claude"},
		{Name: "cli", APIUrl: "https://api.anthropic.com", APIKey: "k2", Enabled: true, Transformer: "cc_cli"},
	}
	s := &Server{config: cfg}

	targetType, ep := s.selectTarget()
	if ep == nil {
		t.Fatalf("expected endpoint")
	}
	if targetType != "cli" {
		t.Fatalf("expected targetType cli, got %q", targetType)
	}
	if ep.Name != "cli" {
		t.Fatalf("expected selected endpoint name cli, got %q", ep.Name)
	}
}

func TestMapEndpointTransformerToTargetType(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"claude", "claude", true},
		{"", "claude", true},
		{"CLI", "cli", true},
		{"cc_cli", "cli", true},
		{"openai", "openai", true},
		{"openai2", "openai2", true},
		{"gemini", "openai", true},
	}
	for _, tc := range cases {
		got, ok := mapEndpointTransformerToTargetType(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("in=%q: got(%q,%v) want(%q,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCreateUpstreamRequest_AddsThinkingBetaHeader(t *testing.T) {
	s := &Server{config: config.DefaultConfig()}
	endpoint := &config.Endpoint{APIKey: "test-key"}
	body := []byte(`{"model":"claude-sonnet-4-5-20250929","thinking":{"type":"enabled","budget_tokens":2048}}`)

	req, err := s.createUpstreamRequest(context.Background(), http.MethodPost, "https://api.anthropic.com/v1/messages", body, "claude", endpoint)
	if err != nil {
		t.Fatalf("createUpstreamRequest: %v", err)
	}

	beta := req.Header.Get("anthropic-beta")
	if !strings.Contains(beta, "interleaved-thinking-2025-05-14") {
		t.Fatalf("expected thinking beta header, got %q", beta)
	}
}

func TestExtractTextFromResponse_OpenAIResponses(t *testing.T) {
	body := []byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"你好"},{"type":"output_text","text":"，世界"}]}]}`)
	if got := extractTextFromResponse(body); got != "你好，世界" {
		t.Fatalf("expected joined responses text, got %q", got)
	}
}

func TestExtractTokenUsageFromResponse_OpenAIResponses(t *testing.T) {
	body := []byte(`{"response":{"usage":{"input_tokens":21,"output_tokens":8}}}`)
	inputTokens, outputTokens := extractTokenUsageFromResponse(body)
	if inputTokens != 21 || outputTokens != 8 {
		t.Fatalf("expected (21,8), got (%d,%d)", inputTokens, outputTokens)
	}
}

func TestHandleStreamingResponse_JSONFallback_OpenAIResponses(t *testing.T) {
	s := &Server{config: config.DefaultConfig()}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]},{"type":"function_call","call_id":"call_1","name":"search","arguments":"{\"q\":\"a\"}"}],"usage":{"input_tokens":7,"output_tokens":3}}`,
		)),
	}
	rr := httptest.NewRecorder()

	inputTokens, outputTokens, ndjson, err := s.handleStreamingResponse(rr, resp, "openai2", nil, false)
	if err != nil {
		t.Fatalf("handleStreamingResponse: %v", err)
	}
	if inputTokens != 7 || outputTokens != 3 {
		t.Fatalf("expected tokens (7,3), got (%d,%d)", inputTokens, outputTokens)
	}
	if !strings.Contains(string(ndjson), `"tool_name":"search"`) {
		t.Fatalf("expected tool node in ndjson, got %s", string(ndjson))
	}
	if rr.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatalf("expected ndjson content type, got %q", rr.Header().Get("Content-Type"))
	}
}
