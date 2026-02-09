package cc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestOpenAI2Transformer_Name(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")
	if trans.Name() != "cc_openai2" {
		t.Errorf("Expected name 'cc_openai2', got '%s'", trans.Name())
	}
}

func TestOpenAI2Transformer_TransformRequest(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"system": "You are helpful.",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 1024
	}`

	result, err := trans.TransformRequest([]byte(claudeReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var openai2Req map[string]interface{}
	if err := json.Unmarshal(result, &openai2Req); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check model is overridden
	if openai2Req["model"] != "gpt-4o" {
		t.Errorf("Expected model 'gpt-4o', got '%v'", openai2Req["model"])
	}

	// Check OpenAI2 (Responses API) format
	if openai2Req["input"] == nil {
		t.Errorf("Expected input field for Responses API, got nil")
	}
}

func TestOpenAI2Transformer_TransformRequest_WithTools(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Read file"}],
		"tools": [{
			"name": "read_file",
			"description": "Read a file",
			"input_schema": {"type": "object", "properties": {"path": {"type": "string"}}}
		}],
		"max_tokens": 1024
	}`

	result, err := trans.TransformRequest([]byte(claudeReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var openai2Req map[string]interface{}
	if err := json.Unmarshal(result, &openai2Req); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check tools are present
	tools, ok := openai2Req["tools"].([]interface{})
	if !ok {
		t.Fatalf("Expected tools to be array, got %T", openai2Req["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}
}

func TestOpenAI2Transformer_TransformResponse(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	openai2Resp := `{
		"id": "resp_123",
		"object": "response",
		"created_at": 1677652288,
		"model": "gpt-4o",
		"output": [
			{
				"type": "message",
				"id": "msg_1",
				"role": "assistant",
				"content": [{"type": "output_text", "text": "Hello!"}]
			}
		],
		"usage": {
			"input_tokens": 10,
			"output_tokens": 5,
			"total_tokens": 15
		}
	}`

	result, err := trans.TransformResponse([]byte(openai2Resp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(result, &claudeResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check Claude format
	if claudeResp["type"] != "message" {
		t.Errorf("Expected type 'message', got '%v'", claudeResp["type"])
	}
	if claudeResp["role"] != "assistant" {
		t.Errorf("Expected role 'assistant', got '%v'", claudeResp["role"])
	}
}

func TestOpenAI2Transformer_TransformResponse_Streaming(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	result, err := trans.TransformResponse([]byte("data: {}"), true)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for streaming, got %v", result)
	}
}

func TestOpenAI2Transformer_TransformResponseWithContext(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")
	ctx := transformer.NewStreamContext()

	openai2SSE := `data: {"type":"response.output_text.delta","delta":"Hello"}

`

	result, err := trans.TransformResponseWithContext([]byte(openai2SSE), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	// Should convert to Claude SSE format
	if result != nil && !strings.Contains(resultStr, "event:") {
		t.Errorf("Expected Claude SSE format with 'event:', got '%s'", resultStr)
	}
}

func TestOpenAI2Transformer_TransformResponseWithContext_NonStreaming(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")
	ctx := transformer.NewStreamContext()

	openai2Resp := `{
		"id": "resp_123",
		"object": "response",
		"created_at": 1677652288,
		"model": "gpt-4o",
		"output": [
			{
				"type": "message",
				"id": "msg_1",
				"role": "assistant",
				"content": [{"type": "output_text", "text": "Hello!"}]
			}
		],
		"usage": {
			"input_tokens": 10,
			"output_tokens": 5,
			"total_tokens": 15
		}
	}`

	result, err := trans.TransformResponseWithContext([]byte(openai2Resp), false, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(result, &claudeResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if claudeResp["type"] != "message" {
		t.Errorf("Expected type 'message', got '%v'", claudeResp["type"])
	}
}
