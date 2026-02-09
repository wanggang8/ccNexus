package cc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestGeminiTransformer_Name(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")
	if trans.Name() != "cc_gemini" {
		t.Errorf("Expected name 'cc_gemini', got '%s'", trans.Name())
	}
}

func TestGeminiTransformer_TransformRequest(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")

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

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check Gemini format
	if geminiReq["contents"] == nil {
		t.Errorf("Expected contents field, got nil")
	}
	if geminiReq["systemInstruction"] == nil {
		t.Errorf("Expected systemInstruction field, got nil")
	}
}

func TestGeminiTransformer_TransformRequest_WithTools(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")

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

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check tools are converted to Gemini format
	tools, ok := geminiReq["tools"].([]interface{})
	if !ok {
		t.Fatalf("Expected tools to be array, got %T", geminiReq["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}
}

func TestGeminiTransformer_TransformResponse(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")

	geminiResp := `{
		"candidates": [{
			"content": {
				"parts": [{"text": "Hello!"}],
				"role": "model"
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 5,
			"totalTokenCount": 15
		}
	}`

	result, err := trans.TransformResponse([]byte(geminiResp), false)
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

func TestGeminiTransformer_TransformResponse_Streaming(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")

	result, err := trans.TransformResponse([]byte("data: {}"), true)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for streaming, got %v", result)
	}
}

func TestGeminiTransformer_TransformResponseWithContext(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")
	ctx := transformer.NewStreamContext()

	geminiSSE := `data: {"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"}}]}

`

	result, err := trans.TransformResponseWithContext([]byte(geminiSSE), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	// Should convert to Claude SSE format
	if !strings.Contains(resultStr, "event:") {
		t.Errorf("Expected Claude SSE format with 'event:', got '%s'", resultStr)
	}
}

func TestGeminiTransformer_TransformResponseWithContext_NonStreaming(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")
	ctx := transformer.NewStreamContext()

	geminiResp := `{
		"candidates": [{
			"content": {
				"parts": [{"text": "Hello!"}],
				"role": "model"
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 5,
			"totalTokenCount": 15
		}
	}`

	result, err := trans.TransformResponseWithContext([]byte(geminiResp), false, ctx)
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
