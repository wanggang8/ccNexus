package cc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestClaudeTransformer_Name(t *testing.T) {
	trans := NewClaudeTransformer()
	if trans.Name() != "cc_claude" {
		t.Errorf("Expected name 'cc_claude', got '%s'", trans.Name())
	}
}

func TestClaudeTransformer_TransformRequest_Passthrough(t *testing.T) {
	trans := NewClaudeTransformer()

	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 1024
	}`

	result, err := trans.TransformRequest([]byte(claudeReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	// Should passthrough unchanged
	if string(result) != claudeReq {
		t.Errorf("Expected passthrough, got different result")
	}
}

func TestClaudeTransformer_TransformRequest_ModelOverride(t *testing.T) {
	trans := NewClaudeTransformerWithModel("claude-opus-4-20250514")

	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 1024
	}`

	result, err := trans.TransformRequest([]byte(claudeReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["model"] != "claude-opus-4-20250514" {
		t.Errorf("Expected model 'claude-opus-4-20250514', got '%v'", data["model"])
	}
}

func TestClaudeTransformer_TransformResponse(t *testing.T) {
	trans := NewClaudeTransformer()

	claudeResp := `{"id": "msg_123", "type": "message"}`

	result, err := trans.TransformResponse([]byte(claudeResp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	// Should passthrough
	if string(result) != claudeResp {
		t.Errorf("Expected passthrough")
	}
}

func TestClaudeTransformer_TransformResponseWithContext_InputTokensFallback(t *testing.T) {
	trans := NewClaudeTransformer()
	ctx := transformer.NewStreamContext()

	// First event: message_start with input_tokens
	messageStart := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":100,"output_tokens":0}}}

`
	result, err := trans.TransformResponseWithContext([]byte(messageStart), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}
	if result == nil {
		t.Fatalf("Expected result, got nil")
	}

	// Check input_tokens was cached
	if ctx.InputTokens != 100 {
		t.Errorf("Expected InputTokens 100, got %d", ctx.InputTokens)
	}

	// Second event: message_delta with input_tokens=0 (should be filled)
	messageDelta := `event: message_delta
data: {"type":"message_delta","usage":{"input_tokens":0,"output_tokens":50}}

`
	result, err = trans.TransformResponseWithContext([]byte(messageDelta), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	// Should have filled input_tokens with cached value
	if !strings.Contains(resultStr, `"input_tokens":100`) {
		t.Errorf("Expected input_tokens to be filled with 100, got '%s'", resultStr)
	}
}

func TestClaudeTransformer_TransformResponseWithContext_NilContext(t *testing.T) {
	trans := NewClaudeTransformer()

	resp := `data: {"type":"message_start"}`

	result, err := trans.TransformResponseWithContext([]byte(resp), true, nil)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	// Should passthrough with nil context
	if string(result) != resp {
		t.Errorf("Expected passthrough with nil context")
	}
}
