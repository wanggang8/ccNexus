package cc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestOpenAITransformer_Name(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	if trans.Name() != "cc_openai" {
		t.Errorf("Expected name 'cc_openai', got '%s'", trans.Name())
	}
}

func TestOpenAITransformer_TransformRequest(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

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

	var openaiReq map[string]interface{}
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check model is overridden
	if openaiReq["model"] != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%v'", openaiReq["model"])
	}

	// Check messages exist
	messages, ok := openaiReq["messages"].([]interface{})
	if !ok {
		t.Fatalf("Expected messages to be array, got %T", openaiReq["messages"])
	}
	if len(messages) < 1 {
		t.Errorf("Expected at least 1 message")
	}
}

func TestOpenAITransformer_TransformRequest_WithTools(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

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

	var openaiReq map[string]interface{}
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check tools are converted to OpenAI format
	tools, ok := openaiReq["tools"].([]interface{})
	if !ok {
		t.Fatalf("Expected tools to be array, got %T", openaiReq["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	tool := tools[0].(map[string]interface{})
	if tool["type"] != "function" {
		t.Errorf("Expected type 'function', got '%v'", tool["type"])
	}
}

func TestOpenAITransformer_TransformResponse(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	openaiResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hello!"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`

	result, err := trans.TransformResponse([]byte(openaiResp), false)
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

func TestOpenAITransformer_TransformResponse_Streaming(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	result, err := trans.TransformResponse([]byte("data: {}"), true)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for streaming, got %v", result)
	}
}

func TestOpenAITransformer_TransformResponseWithContext(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "gpt-4"

	openaiSSE := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`

	result, err := trans.TransformResponseWithContext([]byte(openaiSSE), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	// Should convert to Claude SSE format
	if !strings.Contains(resultStr, "event:") {
		t.Errorf("Expected Claude SSE format with 'event:', got '%s'", resultStr)
	}
}

func TestOpenAITransformer_TransformResponseWithContext_NonStreaming(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()

	openaiResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hello!"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`

	result, err := trans.TransformResponseWithContext([]byte(openaiResp), false, ctx)
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
