package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestCLITransformer_Name(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")
	if trans.Name() != "cx_resp_cli" {
		t.Errorf("Expected name 'cx_resp_cli', got '%s'", trans.Name())
	}
}

func TestCLITransformer_TransformRequest(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	// OpenAI Responses API format
	openai2Req := `{
		"model": "gpt-4o",
		"instructions": "You are helpful.",
		"input": "Hello",
		"max_output_tokens": 1024,
		"stream": true
	}`

	result, err := trans.TransformRequest([]byte(openai2Req))
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

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check OpenAI2 (Responses API) format
	if openai2Resp["object"] != "response" {
		t.Errorf("Expected object 'response', got '%v'", openai2Resp["object"])
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
	// Should convert to OpenAI2 SSE format
	if !strings.Contains(resultStr, "data:") {
		t.Errorf("Expected SSE format with 'data:', got '%s'", resultStr)
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

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if openai2Resp["object"] != "response" {
		t.Errorf("Expected object 'response', got '%v'", openai2Resp["object"])
	}
}
