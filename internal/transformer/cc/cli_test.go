package cc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestCLITransformer_Name(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")
	if trans.Name() != "cc_cli" {
		t.Errorf("Expected name 'cc_cli', got '%s'", trans.Name())
	}
}

func TestCLITransformer_TransformRequest(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	// Note: cc_cli expects OpenAI format input (from Claude Code client sending OpenAI-like requests)
	openaiReq := `{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello"}
		],
		"stream": true
	}`

	result, err := trans.TransformRequest([]byte(openaiReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var cliReq map[string]interface{}
	if err := json.Unmarshal(result, &cliReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check model is overridden
	if cliReq["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("Expected model 'claude-sonnet-4-20250514', got '%v'", cliReq["model"])
	}

	// Check CLI format has system array
	system, ok := cliReq["system"].([]interface{})
	if !ok {
		t.Fatalf("Expected system to be array, got %T", cliReq["system"])
	}
	if len(system) < 1 {
		t.Errorf("Expected at least 1 system block")
	}

	// Check CLI identity is injected
	firstBlock := system[0].(map[string]interface{})
	if !strings.Contains(firstBlock["text"].(string), "Claude Code") {
		t.Errorf("Expected CLI identity in first system block")
	}
}

func TestCLITransformer_TransformRequestWithHeaders(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	openaiReq := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "Hello"}],
		"stream": true
	}`

	body, headers, err := trans.TransformRequestWithHeaders([]byte(openaiReq))
	if err != nil {
		t.Fatalf("TransformRequestWithHeaders failed: %v", err)
	}

	if body == nil {
		t.Errorf("Expected body, got nil")
	}

	// Check headers
	if headers["x-api-key"] != "test-api-key" {
		t.Errorf("Expected x-api-key 'test-api-key', got '%v'", headers["x-api-key"])
	}
	if headers["anthropic-version"] == "" {
		t.Errorf("Expected anthropic-version header")
	}
	if headers["anthropic-beta"] == "" {
		t.Errorf("Expected anthropic-beta header")
	}
	if !strings.Contains(headers["user-agent"], "claude-cli") {
		t.Errorf("Expected user-agent to contain 'claude-cli', got '%v'", headers["user-agent"])
	}
}

func TestCLITransformer_GetURL(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	url := trans.GetURL("https://api.anthropic.com")
	if !strings.Contains(url, "/v1/messages") {
		t.Errorf("Expected URL to contain '/v1/messages', got '%s'", url)
	}
	if !strings.Contains(url, "beta=true") {
		t.Errorf("Expected URL to contain 'beta=true', got '%s'", url)
	}
}

func TestCLITransformer_TransformResponse(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello!"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	result, err := trans.TransformResponse([]byte(claudeResp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check OpenAI format
	if openaiResp["object"] != "chat.completion" {
		t.Errorf("Expected object 'chat.completion', got '%v'", openaiResp["object"])
	}
}

func TestCLITransformer_TransformResponse_Streaming(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	result, err := trans.TransformResponse([]byte("data: {}"), true)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for streaming, got %v", result)
	}
}

func TestCLITransformer_TransformResponseWithContext(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")
	ctx := transformer.NewStreamContext()

	claudeEvent := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}

`

	result, err := trans.TransformResponseWithContext([]byte(claudeEvent), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	// Should convert to OpenAI SSE format
	if !strings.Contains(resultStr, "data:") {
		t.Errorf("Expected OpenAI SSE format with 'data:', got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "chat.completion.chunk") {
		t.Errorf("Expected 'chat.completion.chunk' in result, got '%s'", resultStr)
	}
}

func TestCLITransformer_TransformResponseWithContext_NonStreaming(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")
	ctx := transformer.NewStreamContext()

	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello!"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	result, err := trans.TransformResponseWithContext([]byte(claudeResp), false, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if openaiResp["object"] != "chat.completion" {
		t.Errorf("Expected object 'chat.completion', got '%v'", openaiResp["object"])
	}
}
