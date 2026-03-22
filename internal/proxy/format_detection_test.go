package proxy

import (
	"encoding/json"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
)

// ========== detectClientFormat tests ==========

func TestDetectClientFormat_OpenAIChat(t *testing.T) {
	tests := []struct {
		path     string
		expected ClientFormat
	}{
		{"/v1/chat/completions", ClientFormatOpenAIChat},
		{"/chat/completions", ClientFormatOpenAIChat},
		{"/api/v1/chat/completions", ClientFormatOpenAIChat},
	}

	for _, tt := range tests {
		result := detectClientFormat(tt.path)
		if result != tt.expected {
			t.Errorf("detectClientFormat(%q) = %q, want %q", tt.path, result, tt.expected)
		}
	}
}

func TestDetectClientFormat_OpenAIResponses(t *testing.T) {
	tests := []struct {
		path     string
		expected ClientFormat
	}{
		{"/v1/responses", ClientFormatOpenAIResponses},
		{"/responses", ClientFormatOpenAIResponses},
	}

	for _, tt := range tests {
		result := detectClientFormat(tt.path)
		if result != tt.expected {
			t.Errorf("detectClientFormat(%q) = %q, want %q", tt.path, result, tt.expected)
		}
	}
}

func TestDetectClientFormat_Claude(t *testing.T) {
	tests := []struct {
		path string
	}{
		{"/v1/messages"},
		{"/api/complete"},
		{"/"},
		{"/some/random/path"},
	}

	for _, tt := range tests {
		result := detectClientFormat(tt.path)
		if result != ClientFormatClaude {
			t.Errorf("detectClientFormat(%q) = %q, want %q", tt.path, result, ClientFormatClaude)
		}
	}
}

func TestDetectClientFormat_ChatTakesPrecedence(t *testing.T) {
	// If path contains both substrings, chat should win (checked first)
	result := detectClientFormat("/v1/chat/completions/from/responses")
	if result != ClientFormatOpenAIChat {
		t.Errorf("Expected chat format for ambiguous path, got %q", result)
	}
}

func TestDetectEffectiveClientFormat_ChatPathWithResponsesBody(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":[],"reasoning":{"effort":"medium"},"store":false}`)
	result := detectEffectiveClientFormat(ClientFormatOpenAIChat, body)
	if result != ClientFormatOpenAIResponses {
		t.Fatalf("detectEffectiveClientFormat(chat, responses body) = %q, want %q", result, ClientFormatOpenAIResponses)
	}
}

func TestDetectEffectiveClientFormat_ChatPathWithChatBody(t *testing.T) {
	body := []byte(`{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`)
	result := detectEffectiveClientFormat(ClientFormatOpenAIChat, body)
	if result != ClientFormatOpenAIChat {
		t.Fatalf("detectEffectiveClientFormat(chat, chat body) = %q, want %q", result, ClientFormatOpenAIChat)
	}
}

func TestPrepareTransformerForClient_ResponsesBodyUsesResponsesTransformer(t *testing.T) {
	endpoint := config.Endpoint{
		Name:        "OpenAI2",
		Transformer: "openai2",
		Model:       "gpt-5.4",
	}
	body := []byte(`{"model":"gpt-5.4","input":[],"reasoning":{"effort":"medium"},"store":false}`)

	trans, err := prepareTransformerForClient(ClientFormatOpenAIChat, endpoint, body)
	if err != nil {
		t.Fatalf("prepareTransformerForClient returned error: %v", err)
	}
	if trans.Name() != "cx_resp_openai2" {
		t.Fatalf("expected cx_resp_openai2, got %q", trans.Name())
	}
}

func TestPrepareTransformerForClient_GPT54LogShapeUsesResponsesTransformer(t *testing.T) {
	endpoint := config.Endpoint{
		Name:        "OpenAI2",
		Transformer: "openai2",
		Model:       "gpt-5.4",
	}
	body := []byte(`{
		"user":"95dfaae8bbc5aaa3",
		"model":"gpt-5.4",
		"input":[
			{"role":"system","content":"You are GPT-5.4."},
			{"role":"user","content":"test"}
		],
		"tools":[
			{
				"type":"function",
				"name":"ReadFile",
				"description":"Reads a file",
				"parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}
			}
		],
		"store":false,
		"include":["reasoning.encrypted_content"],
		"reasoning":{"effort":"medium","summary":"auto"},
		"stream":true,
		"stream_options":{"include_usage":true},
		"metadata":{"cursorConversationId":"5e66cef0-7372-496e-aa21-476a66401a2a"}
	}`)

	trans, err := prepareTransformerForClient(ClientFormatOpenAIChat, endpoint, body)
	if err != nil {
		t.Fatalf("prepareTransformerForClient returned error: %v", err)
	}
	if trans.Name() != "cx_resp_openai2" {
		t.Fatalf("expected GPT-5.4 log shape to use cx_resp_openai2, got %q", trans.Name())
	}
}

func TestPrepareTransformerForClient_ChatBodyKeepsChatTransformer(t *testing.T) {
	endpoint := config.Endpoint{
		Name:        "OpenAI",
		Transformer: "openai",
		Model:       "gpt-4.1",
	}
	body := []byte(`{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`)

	trans, err := prepareTransformerForClient(ClientFormatOpenAIChat, endpoint, body)
	if err != nil {
		t.Fatalf("prepareTransformerForClient returned error: %v", err)
	}
	if trans.Name() != "cx_chat_openai" {
		t.Fatalf("expected cx_chat_openai, got %q", trans.Name())
	}
}

func TestPrepareTransformerForClient_InvalidJSONKeepsPathBasedFormat(t *testing.T) {
	endpoint := config.Endpoint{
		Name:        "OpenAI",
		Transformer: "openai",
		Model:       "gpt-4.1",
	}
	body := []byte(`{"messages":`)

	trans, err := prepareTransformerForClient(ClientFormatOpenAIChat, endpoint, body)
	if err != nil {
		t.Fatalf("prepareTransformerForClient returned error: %v", err)
	}
	if trans.Name() != "cx_chat_openai" {
		t.Fatalf("expected invalid JSON to keep cx_chat_openai, got %q", trans.Name())
	}
}

func TestDetectEffectiveClientFormat_ResponsesBodyRoundTripJSON(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":[],"include":["reasoning.encrypted_content"],"store":false}`)
	if !json.Valid(body) {
		t.Fatal("test body should be valid JSON")
	}
	result := detectEffectiveClientFormat(ClientFormatOpenAIChat, body)
	if result != ClientFormatOpenAIResponses {
		t.Fatalf("detectEffectiveClientFormat(valid responses body) = %q, want %q", result, ClientFormatOpenAIResponses)
	}
}

// ========== getTargetPath tests ==========

func TestGetTargetPath_Claude(t *testing.T) {
	tests := []string{"cc_claude", "cx_chat_claude", "cx_resp_claude"}
	for _, name := range tests {
		path := getTargetPath("/original", config.Endpoint{}, nil, name)
		if path != "/v1/messages" {
			t.Errorf("getTargetPath(_, _, _, %q) = %q, want /v1/messages", name, path)
		}
	}
}

func TestGetTargetPath_OpenAI(t *testing.T) {
	tests := []string{"cc_openai", "cx_chat_openai", "cx_resp_openai"}
	for _, name := range tests {
		path := getTargetPath("/original", config.Endpoint{}, nil, name)
		if path != "/v1/chat/completions" {
			t.Errorf("getTargetPath(_, _, _, %q) = %q, want /v1/chat/completions", name, path)
		}
	}
}

func TestGetTargetPath_OpenAI2(t *testing.T) {
	tests := []string{"cc_openai2", "cx_resp_openai2", "cx_chat_openai2"}
	for _, name := range tests {
		path := getTargetPath("/original", config.Endpoint{}, nil, name)
		if path != "/v1/responses" {
			t.Errorf("getTargetPath(_, _, _, %q) = %q, want /v1/responses", name, path)
		}
	}
}

func TestGetTargetPath_Gemini_Stream(t *testing.T) {
	endpoint := config.Endpoint{Model: "gemini-pro"}
	body := []byte(`{"stream":true}`)
	path := getTargetPath("/original", endpoint, body, "cc_gemini")
	expected := "/v1beta/models/gemini-pro:streamGenerateContent"
	if path != expected {
		t.Errorf("getTargetPath for Gemini stream = %q, want %q", path, expected)
	}
}

func TestGetTargetPath_Gemini_NonStream(t *testing.T) {
	endpoint := config.Endpoint{Model: "gemini-pro"}
	body := []byte(`{"stream":false}`)
	path := getTargetPath("/original", endpoint, body, "cc_gemini")
	expected := "/v1beta/models/gemini-pro:generateContent"
	if path != expected {
		t.Errorf("getTargetPath for Gemini non-stream = %q, want %q", path, expected)
	}
}

func TestGetTargetPath_CLI(t *testing.T) {
	tests := []string{"cc_cli", "cx_chat_cli", "cx_resp_cli"}
	for _, name := range tests {
		path := getTargetPath("/original", config.Endpoint{}, nil, name)
		if path != "/v1/messages?beta=true" {
			t.Errorf("getTargetPath(_, _, _, %q) = %q, want /v1/messages?beta=true", name, path)
		}
	}
}

func TestGetTargetPath_Unknown(t *testing.T) {
	path := getTargetPath("/original/path", config.Endpoint{}, nil, "unknown_transformer")
	if path != "/original/path" {
		t.Errorf("Expected original path for unknown transformer, got %q", path)
	}
}
