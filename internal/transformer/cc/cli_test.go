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

func TestCLITransformer_TransformRequest_ClaudeFormat(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	// CC client sends Claude Messages format
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"system": "You are helpful.",
		"messages": [
			{"role": "user", "content": "Hello"}
		],
		"max_tokens": 4096,
		"stream": true
	}`

	result, err := trans.TransformRequest([]byte(claudeReq))
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

	// Check system: CLI identity injected + original system preserved
	system, ok := cliReq["system"].([]interface{})
	if !ok {
		t.Fatalf("Expected system to be array, got %T", cliReq["system"])
	}
	if len(system) < 2 {
		t.Fatalf("Expected at least 2 system blocks (CLI identity + original), got %d", len(system))
	}

	// First block should be CLI identity
	firstBlock := system[0].(map[string]interface{})
	if !strings.Contains(firstBlock["text"].(string), "Claude Code") {
		t.Errorf("Expected CLI identity in first system block")
	}

	// Second block should be original system prompt
	secondBlock := system[1].(map[string]interface{})
	if secondBlock["text"] != "You are helpful." {
		t.Errorf("Expected original system prompt preserved, got '%v'", secondBlock["text"])
	}

	// Check metadata is added
	metadata, ok := cliReq["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected metadata, got %T", cliReq["metadata"])
	}
	userID, _ := metadata["user_id"].(string)
	if !strings.Contains(userID, "user_") || !strings.Contains(userID, "session_") {
		t.Errorf("Expected user_id with user_ and session_, got '%s'", userID)
	}

	// Check messages preserved
	messages, ok := cliReq["messages"].([]interface{})
	if !ok || len(messages) != 1 {
		t.Fatalf("Expected 1 message, got %v", cliReq["messages"])
	}
}

func TestCLITransformer_TransformRequest_WithToolUse(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	// Claude format with tool_use in assistant response and tool_result
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Read /tmp/a"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "toolu_1", "name": "read_file", "input": {"path": "/tmp/a"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "toolu_1", "content": "file contents"}
			]}
		],
		"tools": [{"name": "read_file", "description": "Read a file", "input_schema": {"type": "object", "properties": {"path": {"type": "string"}}}}],
		"max_tokens": 4096,
		"stream": true
	}`

	result, err := trans.TransformRequest([]byte(claudeReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var cliReq map[string]interface{}
	if err := json.Unmarshal(result, &cliReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Messages should be preserved as-is (Claude format)
	messages := cliReq["messages"].([]interface{})
	if len(messages) != 3 {
		t.Fatalf("Expected 3 messages preserved, got %d", len(messages))
	}

	// Assistant message should still have tool_use content block
	assistantMsg := messages[1].(map[string]interface{})
	content := assistantMsg["content"].([]interface{})
	toolUse := content[0].(map[string]interface{})
	if toolUse["type"] != "tool_use" {
		t.Errorf("Expected tool_use content block preserved, got %v", toolUse["type"])
	}

	// Tools should be preserved
	tools := cliReq["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}
}

func TestCLITransformer_TransformRequestWithHeaders(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Hello"}],
		"stream": true
	}`

	body, headers, err := trans.TransformRequestWithHeaders([]byte(claudeReq))
	if err != nil {
		t.Fatalf("TransformRequestWithHeaders failed: %v", err)
	}

	if body == nil {
		t.Errorf("Expected body, got nil")
	}

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
	if headers["x-app"] != "cli" {
		t.Errorf("Expected x-app=cli, got '%v'", headers["x-app"])
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

// cc_cli response is passthrough: CLI returns Claude format, CC expects Claude format.

func TestCLITransformer_TransformResponse_Passthrough(t *testing.T) {
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

	// Should passthrough Claude format unchanged
	var resp map[string]interface{}
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}
	if resp["type"] != "message" {
		t.Errorf("Expected type 'message' (Claude format preserved), got '%v'", resp["type"])
	}
	if resp["stop_reason"] != "end_turn" {
		t.Errorf("Expected stop_reason 'end_turn', got '%v'", resp["stop_reason"])
	}
}

func TestCLITransformer_TransformResponse_StreamingPassthrough(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	claudeSSE := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}

`
	result, err := trans.TransformResponse([]byte(claudeSSE), true)
	if err != nil {
		t.Fatalf("TransformResponse streaming failed: %v", err)
	}
	// Passthrough returns the SSE unchanged
	if string(result) != claudeSSE {
		t.Errorf("Expected passthrough, got different content")
	}
}

func TestCLITransformer_TransformResponseWithContext_Passthrough(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")
	ctx := transformer.NewStreamContext()

	claudeEvent := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}

`
	result, err := trans.TransformResponseWithContext([]byte(claudeEvent), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	// Should passthrough Claude SSE unchanged
	resultStr := string(result)
	if !strings.Contains(resultStr, "message_start") {
		t.Errorf("Expected Claude SSE format passthrough, got '%s'", resultStr)
	}
}

func TestCLITransformer_DefaultMaxTokens(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	// Request without max_tokens
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Hello"}],
		"stream": true
	}`

	result, err := trans.TransformRequest([]byte(claudeReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var cliReq map[string]interface{}
	json.Unmarshal(result, &cliReq)

	if _, ok := cliReq["max_tokens"]; ok {
		t.Fatalf("expected max_tokens to stay absent when not provided, got %#v", cliReq["max_tokens"])
	}
}
