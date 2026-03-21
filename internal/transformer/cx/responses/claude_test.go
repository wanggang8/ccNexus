package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestClaudeTransformer_Name(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")
	if trans.Name() != "cx_resp_claude" {
		t.Errorf("Expected name 'cx_resp_claude', got '%s'", trans.Name())
	}
}

func TestClaudeTransformer_TransformRequest(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

	// OpenAI Responses API format
	openai2Req := `{
		"model": "gpt-4o",
		"instructions": "You are helpful.",
		"input": "Hello",
		"max_output_tokens": 1024
	}`

	result, err := trans.TransformRequest([]byte(openai2Req))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check model is overridden
	if claudeReq["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("Expected model 'claude-sonnet-4-20250514', got '%v'", claudeReq["model"])
	}

	// Check Claude format
	if claudeReq["messages"] == nil {
		t.Errorf("Expected messages field, got nil")
	}
}

func TestClaudeTransformer_TransformResponse(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

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

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check OpenAI2 (Responses API) format
	if openai2Resp["object"] != "response" {
		t.Errorf("Expected object 'response', got '%v'", openai2Resp["object"])
	}
}

func TestClaudeTransformer_TransformResponse_Streaming(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

	result, err := trans.TransformResponse([]byte("data: {}"), true)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for streaming, got %v", result)
	}
}

func TestClaudeTransformer_TransformResponseWithContext(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")
	ctx := transformer.NewStreamContext()

	claudeEvent := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}

`

	result, err := trans.TransformResponseWithContext([]byte(claudeEvent), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	// Should convert to OpenAI2 SSE format
	if !strings.Contains(resultStr, "data:") {
		t.Errorf("Expected SSE format with 'data:', got '%s'", resultStr)
	}
}

func TestClaudeTransformer_TransformResponseWithContext_NonStreaming(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")
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

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if openai2Resp["object"] != "response" {
		t.Errorf("Expected object 'response', got '%v'", openai2Resp["object"])
	}
}

func TestClaudeTransformer_TransformRequest_ClaudeShapeUsesClaudeResponsesBridge(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

	claudeReq := `{
		"model": "claude-4-sonnet",
		"system": [{"type": "text", "text": "You are helpful."}],
		"messages": [{"role": "user", "content": [{"type": "text", "text": "Hello"}]}],
		"metadata": {"source": "cursor"}
	}`

	result, err := trans.TransformRequest([]byte(claudeReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["model"] != "claude-sonnet-4-20250514" {
		t.Fatalf("expected model override, got %#v", data["model"])
	}
	if data["input"] == nil {
		t.Fatalf("expected Claude body to become responses input first and then Claude target output policy keep structure")
	}
}

func TestClaudeTransformer_TransformRequest_OpenAIChatShapeUsesChatToClaudeBridge(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

	chatReq := `{
		"model": "gpt-4.1",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello"}
		],
		"stream_options": {"include_usage": true}
	}`

	result, err := trans.TransformRequest([]byte(chatReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["messages"] == nil {
		t.Fatalf("expected Claude messages output")
	}
	if data["system"] == nil {
		t.Fatalf("expected system preserved/mapped")
	}
}
