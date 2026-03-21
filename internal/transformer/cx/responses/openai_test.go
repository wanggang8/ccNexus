package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestOpenAITransformer_Name(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	if trans.Name() != "cx_resp_openai" {
		t.Errorf("Expected name 'cx_resp_openai', got '%s'", trans.Name())
	}
}

func TestOpenAITransformer_TransformRequest(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

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

	var openaiReq map[string]interface{}
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check model is overridden
	if openaiReq["model"] != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%v'", openaiReq["model"])
	}

	// Check OpenAI Chat format
	if openaiReq["messages"] == nil {
		t.Errorf("Expected messages field, got nil")
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

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check OpenAI2 (Responses API) format
	if openai2Resp["object"] != "response" {
		t.Errorf("Expected object 'response', got '%v'", openai2Resp["object"])
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

	openaiSSE := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`

	result, err := trans.TransformResponseWithContext([]byte(openaiSSE), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	// Should convert to OpenAI2 SSE format
	if result != nil && !strings.Contains(resultStr, "data:") {
		t.Errorf("Expected SSE format with 'data:', got '%s'", resultStr)
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

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if openai2Resp["object"] != "response" {
		t.Errorf("Expected object 'response', got '%v'", openai2Resp["object"])
	}
}

func TestOpenAITransformer_TransformRequest_ClaudeShapeUsesClaudeConverter(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

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

	if data["model"] != "gpt-4o" {
		t.Fatalf("expected model override, got %#v", data["model"])
	}
	if _, ok := data["messages"].([]interface{}); !ok {
		t.Fatalf("expected converted OpenAI chat messages, got %#v", data["messages"])
	}
	if _, ok := data["metadata"].(map[string]interface{}); !ok {
		t.Fatalf("expected metadata preserved, got %#v", data["metadata"])
	}
}

func TestOpenAITransformer_TransformRequest_OpenAIChatShapeUsesChatBridge(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

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
		t.Fatalf("expected OpenAI chat bridge to responses->openai target output")
	}
	if data["stream_options"] == nil {
		t.Fatalf("expected stream_options preserved, got nil")
	}
}

func TestOpenAITransformer_TransformRequest_OpenAIChatToOpenAI_PreservesToolChoice(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	chatReq := `{
		"model": "gpt-4.1",
		"messages": [{"role": "user", "content": "Hello"}],
		"tools": [{"type": "function", "function": {"name": "read_file", "parameters": {"type": "object"}}}],
		"tool_choice": "required"
	}`

	result, err := trans.TransformRequest([]byte(chatReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["tool_choice"] != "required" {
		t.Fatalf("expected tool_choice preserved for openai target, got %#v", data["tool_choice"])
	}
}

func TestOpenAITransformer_TransformRequest_OpenAIChatToOpenAI_NormalizesLegacyFunctionsAndFunctionCall(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	chatReq := `{
		"model": "gpt-4.1",
		"messages": [{"role": "user", "content": "Hello"}],
		"functions": [{"name": "legacy_func", "description": "Legacy", "parameters": {"type": "object"}}],
		"function_call": {"name": "legacy_func"}
	}`

	result, err := trans.TransformRequest([]byte(chatReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if _, ok := data["functions"]; ok {
		t.Fatalf("expected legacy functions removed, got %#v", data["functions"])
	}
	if _, ok := data["function_call"]; ok {
		t.Fatalf("expected legacy function_call removed, got %#v", data["function_call"])
	}
	if _, ok := data["tools"].([]interface{}); !ok {
		t.Fatalf("expected tools after normalization, got %#v", data["tools"])
	}
	toolChoice, ok := data["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected normalized tool_choice object, got %#v", data["tool_choice"])
	}
	fn, _ := toolChoice["function"].(map[string]interface{})
	if toolChoice["type"] != "function" || fn["name"] != "legacy_func" {
		t.Fatalf("unexpected normalized tool_choice: %#v", toolChoice)
	}
}
