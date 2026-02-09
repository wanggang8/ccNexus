package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestGeminiTransformer_Name(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")
	if trans.Name() != "cx_resp_gemini" {
		t.Errorf("Expected name 'cx_resp_gemini', got '%s'", trans.Name())
	}
}

func TestGeminiTransformer_TransformRequest(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")

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

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check Gemini format
	if geminiReq["contents"] == nil {
		t.Errorf("Expected contents field, got nil")
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

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check OpenAI2 (Responses API) format
	if openai2Resp["object"] != "response" {
		t.Errorf("Expected object 'response', got '%v'", openai2Resp["object"])
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
	// Should convert to OpenAI2 SSE format
	if result != nil && !strings.Contains(resultStr, "data:") {
		t.Errorf("Expected SSE format with 'data:', got '%s'", resultStr)
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

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if openai2Resp["object"] != "response" {
		t.Errorf("Expected object 'response', got '%v'", openai2Resp["object"])
	}
}
