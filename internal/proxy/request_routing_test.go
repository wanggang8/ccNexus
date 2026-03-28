package proxy

import (
	"net/http/httptest"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	newcursor "github.com/lich0821/ccNexus/internal/cursorbridge"
)

func TestCursorResponsesRoutingModeToTargetPath(t *testing.T) {
	baseReq := httptest.NewRequest("POST", "http://localhost/cursor/v1/responses", nil)

	nativeEndpoint := config.Endpoint{
		Name:        "native-responses",
		APIUrl:      "https://api.example.com",
		APIKey:      "k",
		Enabled:     true,
		Transformer: "openai2",
		Model:       "gpt-5",
	}
	nativeTrans, err := prepareTransformerForClient(ClientFormatOpenAIResponses, nativeEndpoint)
	if err != nil {
		t.Fatalf("prepareTransformerForClient native failed: %v", err)
	}
	nativeBody, err := nativeTrans.TransformRequest([]byte(`{"model":"gpt-5","input":"hi"}`))
	if err != nil {
		t.Fatalf("native TransformRequest failed: %v", err)
	}
	nativePath := getTargetPath(baseReq.URL.Path, nativeEndpoint, nativeBody, nativeTrans.Name())
	if mode := newcursor.ResponsesRouteMode(nativeTrans.Name()); mode != "native_responses" {
		t.Fatalf("expected native_responses mode, got %s", mode)
	}
	if nativePath != "/v1/responses" {
		t.Fatalf("expected native responses path, got %s", nativePath)
	}

	bridgeEndpoint := config.Endpoint{
		Name:        "bridge-chat",
		APIUrl:      "https://api.example.com",
		APIKey:      "k",
		Enabled:     true,
		Transformer: "openai",
		Model:       "gpt-5",
	}
	bridgeTrans, err := prepareTransformerForClient(ClientFormatOpenAIResponses, bridgeEndpoint)
	if err != nil {
		t.Fatalf("prepareTransformerForClient bridge failed: %v", err)
	}
	bridgeBody, err := bridgeTrans.TransformRequest([]byte(`{"model":"gpt-5","input":"hi"}`))
	if err != nil {
		t.Fatalf("bridge TransformRequest failed: %v", err)
	}
	bridgePath := getTargetPath(baseReq.URL.Path, bridgeEndpoint, bridgeBody, bridgeTrans.Name())
	if mode := newcursor.ResponsesRouteMode(bridgeTrans.Name()); mode != "responses_to_chat_bridge" {
		t.Fatalf("expected responses_to_chat_bridge mode, got %s", mode)
	}
	if bridgePath != "/v1/chat/completions" {
		t.Fatalf("expected bridge chat path, got %s", bridgePath)
	}
}

func TestGetCurrentEndpointForRequestSkipsIncompatibleCursorEndpoints(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Endpoints = []config.Endpoint{
		{Name: "cli", Enabled: true, Transformer: "cli", Model: "sonnet"},
		{Name: "openai", Enabled: true, Transformer: "openai", Model: "gpt-5"},
		{Name: "gemini", Enabled: true, Transformer: "gemini", Model: "gemini-2.5-pro"},
	}
	p := New(cfg, nil, nil, "test-device")
	p.currentIndex = 0

	meta := proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode:   true,
			ClientFormat: newcursor.ClientFormatOpenAIChat,
		},
	}

	endpoint := p.getCurrentEndpointForRequest(meta)
	if endpoint.Name != "openai" {
		t.Fatalf("expected cursor chat request to skip incompatible cli endpoint, got %s", endpoint.Name)
	}
}

func TestGetCurrentEndpointForCursorMessagesPrefersClaudeCompatibleEndpoint(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Endpoints = []config.Endpoint{
		{Name: "openai", Enabled: true, Transformer: "openai", Model: "gpt-5"},
		{Name: "claude", Enabled: true, Transformer: "claude", Model: "claude-sonnet-4-20250514"},
		{Name: "gemini", Enabled: true, Transformer: "gemini", Model: "gemini-2.5-pro"},
	}
	p := New(cfg, nil, nil, "test-device")
	p.currentIndex = 0

	meta := proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode:   true,
			ClientFormat: newcursor.ClientFormatClaude,
		},
	}

	endpoint := p.getCurrentEndpointForRequest(meta)
	if endpoint.Name != "claude" {
		t.Fatalf("expected cursor messages request to select claude endpoint, got %s", endpoint.Name)
	}

	compatible := p.getEnabledEndpointsForRequest(meta)
	if len(compatible) != 1 || compatible[0].Name != "claude" {
		t.Fatalf("expected only claude endpoint to remain compatible, got %#v", compatible)
	}
}

func TestPrepareTransformerForCursorRouteMatrix(t *testing.T) {
	tests := []struct {
		name         string
		clientFormat ClientFormat
		endpoint     config.Endpoint
		expectedName string
		expectedErr  bool
	}{
		{
			name:         "chat openai",
			clientFormat: ClientFormatOpenAIChat,
			endpoint:     config.Endpoint{Name: "openai", Transformer: "openai", Model: "gpt-5"},
			expectedName: "cx_chat_openai",
		},
		{
			name:         "chat openai2",
			clientFormat: ClientFormatOpenAIChat,
			endpoint:     config.Endpoint{Name: "openai2", Transformer: "openai2", Model: "gpt-5"},
			expectedName: "cx_chat_openai2",
		},
		{
			name:         "chat claude",
			clientFormat: ClientFormatOpenAIChat,
			endpoint:     config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			expectedName: "cx_chat_claude",
		},
		{
			name:         "chat gemini",
			clientFormat: ClientFormatOpenAIChat,
			endpoint:     config.Endpoint{Name: "gemini", Transformer: "gemini", Model: "gemini-2.5-pro"},
			expectedName: "cx_chat_gemini",
		},
		{
			name:         "responses openai",
			clientFormat: ClientFormatOpenAIResponses,
			endpoint:     config.Endpoint{Name: "openai", Transformer: "openai", Model: "gpt-5"},
			expectedName: "cx_resp_openai",
		},
		{
			name:         "responses openai2",
			clientFormat: ClientFormatOpenAIResponses,
			endpoint:     config.Endpoint{Name: "openai2", Transformer: "openai2", Model: "gpt-5"},
			expectedName: "cx_resp_openai2",
		},
		{
			name:         "responses claude",
			clientFormat: ClientFormatOpenAIResponses,
			endpoint:     config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			expectedName: "cx_resp_claude",
		},
		{
			name:         "responses gemini",
			clientFormat: ClientFormatOpenAIResponses,
			endpoint:     config.Endpoint{Name: "gemini", Transformer: "gemini", Model: "gemini-2.5-pro"},
			expectedName: "cx_resp_gemini",
		},
		{
			name:         "messages claude",
			clientFormat: ClientFormatClaude,
			endpoint:     config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			expectedName: "cc_claude",
		},
	}

	for _, tt := range tests {
		trans, err := prepareTransformerForClient(tt.clientFormat, tt.endpoint)
		if tt.expectedErr {
			if err == nil {
				t.Fatalf("%s: expected error", tt.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: prepareTransformerForClient failed: %v", tt.name, err)
		}
		if trans.Name() != tt.expectedName {
			t.Fatalf("%s: expected transformer %s, got %s", tt.name, tt.expectedName, trans.Name())
		}
	}
}
