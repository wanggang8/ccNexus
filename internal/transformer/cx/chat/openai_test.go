package chat

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
	"github.com/lich0821/ccNexus/internal/transformer/convert"
)

func readLogJSONLine(t *testing.T, path string, lineIndex int) []byte {
	t.Helper()

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log failed: %v", err)
	}

	lines := strings.Split(string(payload), "\n")
	if lineIndex < 0 || lineIndex >= len(lines) {
		t.Fatalf("expected log %s to contain line %d, got %d lines", path, lineIndex+1, len(lines))
	}

	line := strings.TrimSpace(lines[lineIndex])
	if line == "" {
		t.Fatalf("expected log %s line %d to contain json payload", path, lineIndex+1)
	}

	return []byte(line)
}

func TestOpenAITransformer_Name(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	if trans.Name() != "cx_chat_openai" {
		t.Errorf("Expected name 'cx_chat_openai', got '%s'", trans.Name())
	}
}

func TestOpenAITransformer_TransformRequest_ModelOverride(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4-turbo")

	openaiReq := `{
		"model": "gpt-3.5-turbo",
		"messages": [{"role": "user", "content": "Hello"}]
	}`

	result, err := trans.TransformRequest([]byte(openaiReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["model"] != "gpt-4-turbo" {
		t.Errorf("Expected model 'gpt-4-turbo', got '%v'", data["model"])
	}
}

func TestOpenAITransformer_TransformRequest_FixClaudeTools(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	// Claude format tools (from Cursor)
	openaiReq := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "Hello"}],
		"tools": [{
			"name": "read_file",
			"description": "Read a file",
			"input_schema": {"type": "object", "properties": {"path": {"type": "string"}}}
		}]
	}`

	result, err := trans.TransformRequest([]byte(openaiReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	tools := data["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})

	// Should be converted to OpenAI format
	if tool["type"] != "function" {
		t.Errorf("Expected type 'function', got '%v'", tool["type"])
	}

	funcObj := tool["function"].(map[string]interface{})
	if funcObj["name"] != "read_file" {
		t.Errorf("Expected function name 'read_file', got '%v'", funcObj["name"])
	}
	if funcObj["parameters"] == nil {
		t.Errorf("Expected parameters, got nil")
	}
}

func TestOpenAITransformer_TransformRequest_FixToolResult(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	// Claude format tool_result (from Cursor)
	openaiReq := `{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "Read file"},
			{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "call_1", "content": "file content"}]}
		]
	}`

	result, err := trans.TransformRequest([]byte(openaiReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	messages := data["messages"].([]interface{})
	toolMsg := messages[1].(map[string]interface{})

	// Should be converted to OpenAI tool message format
	if toolMsg["role"] != "tool" {
		t.Errorf("Expected role 'tool', got '%v'", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_1" {
		t.Errorf("Expected tool_call_id 'call_1', got '%v'", toolMsg["tool_call_id"])
	}
	if toolMsg["content"] != "file content" {
		t.Errorf("Expected content 'file content', got '%v'", toolMsg["content"])
	}
}

func TestOpenAITransformer_TransformRequest_KeepOpenAIFormat(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	// Already OpenAI format
	openaiReq := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "Hello"}],
		"tools": [{
			"type": "function",
			"function": {
				"name": "read_file",
				"description": "Read a file",
				"parameters": {"type": "object", "properties": {"path": {"type": "string"}}}
			}
		}]
	}`

	result, err := trans.TransformRequest([]byte(openaiReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	tools := data["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})

	// Should remain OpenAI format
	if tool["type"] != "function" {
		t.Errorf("Expected type 'function', got '%v'", tool["type"])
	}
}

func TestOpenAITransformer_TransformRequest_NormalizesLegacyFunctionsAndFunctionCall(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	openaiReq := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "Hello"}],
		"functions": [{"name": "legacy_func", "description": "Legacy", "parameters": {"type": "object"}}],
		"function_call": {"name": "legacy_func"}
	}`

	result, err := trans.TransformRequest([]byte(openaiReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	tools, ok := data["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("expected legacy functions normalized to tools, got %#v", data["tools"])
	}
	if _, ok := data["functions"]; ok {
		t.Fatalf("expected legacy functions key removed, got %#v", data["functions"])
	}
	toolChoice, ok := data["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected function_call normalized to tool_choice, got %#v", data["tool_choice"])
	}
	fn, _ := toolChoice["function"].(map[string]interface{})
	if toolChoice["type"] != "function" || fn["name"] != "legacy_func" {
		t.Fatalf("unexpected tool_choice normalization result: %#v", toolChoice)
	}
	if _, ok := data["function_call"]; ok {
		t.Fatalf("expected legacy function_call key removed, got %#v", data["function_call"])
	}
}

func TestOpenAITransformer_TransformRequest_PreservesInternalThinkingFields(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	openaiReq := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "Hello"}],
		"thinking": {"type": "enabled", "budget_tokens": 2048},
		"enable_thinking": true,
		"budget_tokens": 2048,
		"reasoning_effort": "medium"
	}`

	result, err := trans.TransformRequest([]byte(openaiReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	thinking, ok := data["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected thinking to be preserved, got %#v", data["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("expected thinking.type to be preserved, got %#v", thinking["type"])
	}
	if data["enable_thinking"] != true {
		t.Fatalf("expected enable_thinking to be preserved, got %#v", data["enable_thinking"])
	}
	if data["budget_tokens"] != float64(2048) {
		t.Fatalf("expected budget_tokens to be preserved, got %#v", data["budget_tokens"])
	}
	if data["reasoning_effort"] != "medium" {
		t.Fatalf("expected reasoning_effort to be preserved, got %#v", data["reasoning_effort"])
	}
}

// ========== Response Tests ==========

func TestNormalizeOpenAISSE_ToolCallMissingType_IsFilled(t *testing.T) {
	ctx := transformer.NewStreamContext()

	chunk := []byte(`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_123","function":{"name":"read_file","arguments":"{\"path\":\"/tmp/a.txt\"}"}}]}}]}`)

	result, err := normalizeOpenAISSE(chunk, ctx)
	if err != nil {
		t.Fatalf("normalizeOpenAISSE failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected normalized SSE chunk, got nil")
	}

	_, payload := convert.ParseSSE(result)
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("failed to unmarshal normalized payload: %v", err)
	}

	choices := got["choices"].([]interface{})
	delta := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
	toolCalls := delta["tool_calls"].([]interface{})
	toolCall := toolCalls[0].(map[string]interface{})

	if toolCall["id"] != "call_123" {
		t.Fatalf("expected tool call id preserved, got %#v", toolCall["id"])
	}
	if toolCall["type"] != "function" {
		t.Fatalf("expected missing type to be normalized to function, got %#v", toolCall["type"])
	}
}

func TestNormalizeOpenAISSE_DuplicateDone_IsIgnored(t *testing.T) {
	ctx := transformer.NewStreamContext()

	first, err := normalizeOpenAISSE([]byte("data: [DONE]\n\n"), ctx)
	if err != nil {
		t.Fatalf("first DONE normalize failed: %v", err)
	}
	if string(first) != "data: [DONE]\n\n" {
		t.Fatalf("expected first DONE passthrough, got %q", string(first))
	}

	second, err := normalizeOpenAISSE([]byte("data: [DONE]\n\n"), ctx)
	if err != nil {
		t.Fatalf("second DONE normalize failed: %v", err)
	}
	if second != nil {
		t.Fatalf("expected duplicate DONE to be ignored, got %q", string(second))
	}
}

func TestNormalizeOpenAISSE_ToolCallWithoutIndex_UsesZeroAndFillsType(t *testing.T) {
	ctx := transformer.NewStreamContext()

	chunk := []byte(`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_no_index","function":{"name":"read_file","arguments":"{\"path\":\"/tmp/a.txt\"}"}}]}}]}`)

	result, err := normalizeOpenAISSE(chunk, ctx)
	if err != nil {
		t.Fatalf("normalizeOpenAISSE failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected normalized SSE chunk, got nil")
	}

	_, payload := convert.ParseSSE(result)
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("failed to unmarshal normalized payload: %v", err)
	}

	choices := got["choices"].([]interface{})
	delta := choices[0].(map[string]interface{})["delta"].(map[string]interface{})
	toolCalls := delta["tool_calls"].([]interface{})
	toolCall := toolCalls[0].(map[string]interface{})

	if toolCall["index"] != float64(0) {
		t.Fatalf("expected missing index to normalize to 0, got %#v", toolCall["index"])
	}
	if toolCall["type"] != "function" {
		t.Fatalf("expected missing type to normalize to function, got %#v", toolCall["type"])
	}
}

func TestOpenAITransformer_TransformResponse_Passthrough(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	// Standard OpenAI response
	openaiResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hello!"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 9, "completion_tokens": 5, "total_tokens": 14}
	}`

	result, err := trans.TransformResponse([]byte(openaiResp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	// Should passthrough unchanged
	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["object"] != "chat.completion" {
		t.Errorf("Expected object 'chat.completion', got '%v'", data["object"])
	}
	if data["id"] != "chatcmpl-123" {
		t.Errorf("Expected id 'chatcmpl-123', got '%v'", data["id"])
	}
}

func TestOpenAITransformer_TransformResponse_DetectClaude(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	// Claude format response (backend returned Claude format)
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello from Claude!"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	result, err := trans.TransformResponse([]byte(claudeResp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should be converted to OpenAI format
	if data["object"] != "chat.completion" {
		t.Errorf("Expected object 'chat.completion', got '%v'", data["object"])
	}

	choices := data["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})

	if message["content"] != "Hello from Claude!" {
		t.Errorf("Expected content 'Hello from Claude!', got '%v'", message["content"])
	}
}

func TestOpenAITransformer_TransformResponse_DetectClaude_WithToolUse(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	// Claude format response with tool_use
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Let me read that file."},
			{"type": "tool_use", "id": "toolu_1", "name": "read_file", "input": {"path": "/tmp/a"}}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 10, "output_tokens": 15}
	}`

	result, err := trans.TransformResponse([]byte(claudeResp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	choices := data["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})

	// Check finish_reason
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("Expected finish_reason 'tool_calls', got '%v'", choice["finish_reason"])
	}

	// Check tool_calls
	message := choice["message"].(map[string]interface{})
	toolCalls := message["tool_calls"].([]interface{})
	if len(toolCalls) != 1 {
		t.Fatalf("Expected 1 tool_call, got %d", len(toolCalls))
	}

	tc := toolCalls[0].(map[string]interface{})
	if tc["id"] != "toolu_1" {
		t.Errorf("Expected tool_call id 'toolu_1', got '%v'", tc["id"])
	}
}

func TestOpenAITransformer_TransformResponse_InvalidJSON(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	// Invalid JSON should passthrough
	invalidResp := `not valid json`

	result, err := trans.TransformResponse([]byte(invalidResp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	if string(result) != invalidResp {
		t.Errorf("Expected passthrough for invalid JSON")
	}
}

func TestOpenAITransformer_TransformResponse_UnknownFormat(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	// Unknown format (not OpenAI, not Claude)
	unknownResp := `{"foo": "bar", "baz": 123}`

	result, err := trans.TransformResponse([]byte(unknownResp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	// Should passthrough
	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["foo"] != "bar" {
		t.Errorf("Expected passthrough for unknown format")
	}
}

func TestOpenAITransformer_TransformResponse_Streaming(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	// Streaming flag should passthrough
	result, err := trans.TransformResponse([]byte(`{}`), true)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	if string(result) != "{}" {
		t.Errorf("Expected passthrough for streaming")
	}
}

// ========== Streaming Response Tests ==========

func TestOpenAITransformer_TransformResponseWithContext_Passthrough(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()

	// OpenAI SSE format
	openaiSSE := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`

	result, err := trans.TransformResponseWithContext([]byte(openaiSSE), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	// Should passthrough
	if string(result) != openaiSSE {
		t.Errorf("Expected passthrough for OpenAI SSE")
	}
}

func TestOpenAITransformer_TransformResponseWithContext_DetectClaudeSSE(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()

	// Claude SSE format
	claudeSSE := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}

`

	result, err := trans.TransformResponseWithContext([]byte(claudeSSE), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	// Should convert to OpenAI SSE format
	resultStr := string(result)
	if !strings.Contains(resultStr, "data:") {
		t.Errorf("Expected OpenAI SSE format with 'data:', got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "chat.completion.chunk") {
		t.Errorf("Expected 'chat.completion.chunk' in result, got '%s'", resultStr)
	}
}

func TestOpenAITransformer_TransformResponseWithContext_DetectClaudeSSE_ContentDelta(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "msg_1"

	// Claude content_block_delta event
	claudeSSE := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

`

	result, err := trans.TransformResponseWithContext([]byte(claudeSSE), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "Hello") {
		t.Errorf("Expected 'Hello' in result, got '%s'", resultStr)
	}
}

func TestOpenAITransformer_TransformResponseWithContext_DetectClaudeSSE_MessageStop(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "msg_1"

	// Claude message_stop event
	claudeSSE := `event: message_stop
data: {"type":"message_stop"}

`

	result, err := trans.TransformResponseWithContext([]byte(claudeSSE), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "[DONE]") {
		t.Errorf("Expected '[DONE]' in result, got '%s'", resultStr)
	}
}

func TestOpenAITransformer_TransformResponseWithContext_NonStreaming(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()

	// Non-streaming should call TransformResponse
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

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should be converted to OpenAI format
	if data["object"] != "chat.completion" {
		t.Errorf("Expected object 'chat.completion', got '%v'", data["object"])
	}
}

func TestOpenAITransformer_TransformResponseWithContext_PartialClaudeDetection(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()

	// Test each Claude event type detection
	claudeEvents := []string{
		"event: message_start\ndata: {}",
		"event: content_block_start\ndata: {}",
		"event: content_block_delta\ndata: {}",
		"event: content_block_stop\ndata: {}",
		"event: message_delta\ndata: {}",
		"event: message_stop\ndata: {}",
	}

	for _, event := range claudeEvents {
		_, err := trans.TransformResponseWithContext([]byte(event), true, ctx)
		if err != nil {
			t.Errorf("TransformResponseWithContext failed for event '%s': %v", event[:20], err)
		}
		// Reset context for next test
		ctx = transformer.NewStreamContext()
	}
}

func TestOpenAITransformer_TransformResponseWithContext_NormalizeMalformedToolCallChunk(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()

	first := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_1","type":"function","index":0,"function":{"name":"Read","arguments":""}}]},"finish_reason":null}]}`
	second := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4","choices":[{"index":0,"delta":{"role":null,"content":null,"reasoning":null,"reasoning_content":null,"tool_calls":[{"id":"call_2","type":"function","index":0,"function":{"name":null,"arguments":"{\"path\":\"/tmp/a\"}"}}]},"finish_reason":null}]}`

	firstResult, err := trans.TransformResponseWithContext([]byte(first), true, ctx)
	if err != nil {
		t.Fatalf("first TransformResponseWithContext failed: %v", err)
	}
	if string(firstResult) != first {
		t.Fatalf("expected first chunk passthrough, got %s", string(firstResult))
	}

	secondResult, err := trans.TransformResponseWithContext([]byte(second), true, ctx)
	if err != nil {
		t.Fatalf("second TransformResponseWithContext failed: %v", err)
	}

	var wrapper map[string]interface{}
	line := strings.TrimPrefix(strings.TrimSpace(string(secondResult)), "data: ")
	if err := json.Unmarshal([]byte(line), &wrapper); err != nil {
		t.Fatalf("failed to unmarshal normalized chunk: %v", err)
	}

	choices := wrapper["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	delta := choice["delta"].(map[string]interface{})

	if _, exists := delta["role"]; !exists {
		t.Fatalf("expected non-tool fields to remain untouched")
	}
	if _, exists := delta["reasoning_content"]; !exists {
		t.Fatalf("expected reasoning fields to remain untouched")
	}

	toolCalls := delta["tool_calls"].([]interface{})
	tc := toolCalls[0].(map[string]interface{})
	if tc["id"] != "call_1" {
		t.Fatalf("expected tool call id to stay call_1, got %v", tc["id"])
	}
	function := tc["function"].(map[string]interface{})
	if function["name"] != "Read" {
		t.Fatalf("expected repaired function name Read, got %v", function["name"])
	}
	if function["arguments"] != "{\"path\":\"/tmp/a\"}" {
		t.Fatalf("expected argument fragment to be preserved, got %v", function["arguments"])
	}
}

func TestOpenAITransformer_TransformResponseWithContext_PreserveReasoningChunks(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()

	reasoningChunk := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":null},"finish_reason":null}]}`

	result, err := trans.TransformResponseWithContext([]byte(reasoningChunk), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}
	if string(result) != reasoningChunk {
		t.Fatalf("expected reasoning chunk passthrough, got %s", string(result))
	}
}

func TestOpenAITransformer_TransformResponseWithContext_DropPostDoneChunks(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()

	doneResult, err := trans.TransformResponseWithContext([]byte("data: [DONE]\n\n"), true, ctx)
	if err != nil {
		t.Fatalf("done TransformResponseWithContext failed: %v", err)
	}
	if string(doneResult) != "data: [DONE]\n\n" {
		t.Fatalf("expected done passthrough, got %q", string(doneResult))
	}

	postDone := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"late"},"finish_reason":null}]}`
	postDoneResult, err := trans.TransformResponseWithContext([]byte(postDone), true, ctx)
	if err != nil {
		t.Fatalf("post-done TransformResponseWithContext failed: %v", err)
	}
	if postDoneResult != nil {
		t.Fatalf("expected post-done chunks to be dropped, got %s", string(postDoneResult))
	}
}

func TestOpenAITransformer_TransformRequest_ClaudeShapeUsesClaudeConverter(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	claudeReq := `{
		"model": "claude-4-sonnet",
		"system": [{"type": "text", "text": "You are helpful."}],
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Hello"}]}
		],
		"tools": [{
			"name": "read_file",
			"description": "Read a file",
			"input_schema": {"type": "object", "properties": {"path": {"type": "string"}}}
		}],
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
	if _, ok := data["metadata"].(map[string]interface{}); !ok {
		t.Fatalf("expected metadata to be preserved for Claude-shaped body, got %#v", data["metadata"])
	}
	messages := data["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("expected system + user messages after Claude conversion, got %d", len(messages))
	}
	first := messages[0].(map[string]interface{})
	if first["role"] != "system" {
		t.Fatalf("expected first message to be system after Claude conversion, got %#v", first)
	}
	tools := data["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})
	if tool["type"] != "function" {
		t.Fatalf("expected Claude tools to be converted via ClaudeReqToOpenAI, got %#v", tool)
	}
}

func TestOpenAITransformer_TransformRequest_OpenAIResponsesShapeUsesResponsesConverter(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	responsesReq := `{
		"model": "gpt-5.4",
		"instructions": "You are helpful.",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Hello"}]}
		],
		"reasoning": {"effort": "medium", "summary": "auto"},
		"include": ["reasoning.encrypted_content"],
		"stream_options": {"include_usage": true},
		"user": "user-123"
	}`

	result, err := trans.TransformRequest([]byte(responsesReq))
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
	messages := data["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("expected system + user messages after OpenAI2 conversion, got %d", len(messages))
	}
	if data["reasoning_effort"] != "medium" {
		t.Fatalf("expected reasoning to map to reasoning_effort, got %#v", data["reasoning_effort"])
	}
	if data["stream_options"] == nil {
		t.Fatalf("expected stream_options preserved for responses-like body")
	}
	if data["user"] != "user-123" {
		t.Fatalf("expected user preserved for responses-like body, got %#v", data["user"])
	}
}

func TestOpenAITransformer_TransformRequest_ResponsesShapeNormalizesReasoningAliasWithoutOpenAIPassthrough(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	responsesReq := `{
		"model": "gpt-5.4",
		"input": [{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Hello"}]}],
		"reasoning": {"effort": "medium"},
		"reasoningContent": "Need to inspect files first.",
		"user": "user-123"
	}`

	result, err := trans.TransformRequest([]byte(responsesReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["reasoning_effort"] != "medium" {
		t.Fatalf("expected reasoning effort preserved, got %#v", data["reasoning_effort"])
	}
	if _, ok := data["reasoningContent"]; ok {
		t.Fatalf("expected camelCase reasoningContent to be normalized away, got %#v", data["reasoningContent"])
	}
	if _, ok := data["reasoning_content"]; ok {
		t.Fatalf("expected reasoning_content not to be passed through to openai target, got %#v", data["reasoning_content"])
	}
}

func TestOpenAITransformer_TransformRequest_RealCursorClaudeLog(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	result, err := trans.TransformRequest(readLogJSONLine(t, "/Users/vick/Desktop/project/ccNexus/docs/claude-cursor.log", 3))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("unmarshal transformed payload failed: %v", err)
	}

	if data["model"] != "gpt-4o" {
		t.Fatalf("expected model override, got %#v", data["model"])
	}
	if _, ok := data["messages"].([]interface{}); !ok {
		t.Fatalf("expected messages after Claude-shaped chat conversion, got %#v", data["messages"])
	}
	if _, ok := data["tools"].([]interface{}); !ok {
		t.Fatalf("expected tools preserved, got %#v", data["tools"])
	}
}

func TestOpenAITransformer_TransformRequest_RealChatGPT54Log(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	result, err := trans.TransformRequest(readLogJSONLine(t, "/Users/vick/Desktop/project/ccNexus/docs/chatgpt5.4-request.log", 0))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("unmarshal transformed payload failed: %v", err)
	}

	if data["user"] != "95dfaae8bbc5aaa3" {
		t.Fatalf("expected responses-like user preserved, got %#v", data["user"])
	}
	if data["stream_options"] == nil {
		t.Fatalf("expected stream_options preserved")
	}
	if _, ok := data["messages"].([]interface{}); !ok {
		t.Fatalf("expected responses-like body converted to openai messages")
	}
}

func TestOpenAITransformer_TransformRequest_RealGPTCursorLog(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	result, err := trans.TransformRequest(readLogJSONLine(t, "/Users/vick/Desktop/project/ccNexus/docs/gpt-cursor.log", 1))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("unmarshal transformed payload failed: %v", err)
	}

	if data["model"] != "gpt-4o" {
		t.Fatalf("expected model override, got %#v", data["model"])
	}
	if data["input"] != nil {
		t.Fatalf("expected responses input to be converted away for openai target, got %#v", data["input"])
	}
	if _, ok := data["messages"].([]interface{}); !ok {
		t.Fatalf("expected converted openai messages, got %#v", data["messages"])
	}
	if _, ok := data["metadata"].(map[string]interface{}); !ok {
		t.Fatalf("expected metadata preserved, got %#v", data["metadata"])
	}
	if _, ok := data["stream_options"].(map[string]interface{}); !ok {
		t.Fatalf("expected stream_options preserved, got %#v", data["stream_options"])
	}
	if data["user"] != "95dfaae8bbc5aaa3" {
		t.Fatalf("expected user preserved, got %#v", data["user"])
	}
	if data["reasoning_effort"] != "medium" {
		t.Fatalf("expected reasoning_effort preserved, got %#v", data["reasoning_effort"])
	}
	if _, ok := data["include"]; ok {
		t.Fatalf("expected include to be dropped for openai target, got %#v", data["include"])
	}
	if _, ok := data["store"]; ok {
		t.Fatalf("expected store to be dropped for openai target, got %#v", data["store"])
	}
	if _, ok := data["reasoning"]; ok {
		t.Fatalf("expected reasoning to be dropped for openai target, got %#v", data["reasoning"])
	}
	tools, ok := data["tools"].([]interface{})
	if !ok || len(tools) != 18 {
		t.Fatalf("expected 18 converted tools for openai target, got %#v", data["tools"])
	}
	for i, rawTool := range tools {
		tool := rawTool.(map[string]interface{})
		if tool["type"] != "function" {
			t.Fatalf("expected tool %d to be function, got %#v", i, tool)
		}
		fn, ok := tool["function"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected tool %d to carry function block, got %#v", i, tool)
		}
		if fn["name"] == "" {
			t.Fatalf("expected tool %d name preserved, got %#v", i, fn)
		}
		if _, ok := fn["parameters"].(map[string]interface{}); !ok {
			t.Fatalf("expected tool %d parameters preserved, got %#v", i, fn)
		}
		if strict, ok := fn["strict"].(bool); ok && strict {
			t.Fatalf("expected tool %d strict=false preserved, got %#v", i, fn["strict"])
		}
	}
}

func TestOpenAITransformer_TransformRequest_RealGPT5LogPreservesToolOutputs(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	payload := strings.TrimPrefix(string(readLogJSONLine(t, "/Users/vick/Desktop/project/ccNexus/docs/gpt5.log", 0)), "转换前：")
	result, err := trans.TransformRequest([]byte(payload))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("unmarshal transformed payload failed: %v", err)
	}

	messages, ok := data["messages"].([]interface{})
	if !ok {
		t.Fatalf("expected converted openai messages, got %#v", data["messages"])
	}

	toolMessages := 0
	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok || msg["role"] != "tool" {
			continue
		}
		toolMessages++
		content, ok := msg["content"].(string)
		if !ok {
			t.Fatalf("expected tool message content normalized to string, got %T (%#v)", msg["content"], msg["content"])
		}
		if !strings.Contains(content, "User questions responses:") {
			t.Fatalf("expected tool output text preserved, got %#v", content)
		}
	}
	if toolMessages != 4 {
		t.Fatalf("expected 4 tool messages from gpt5 log, got %d", toolMessages)
	}
}

func TestOpenAITransformer_TransformRequest_RealClaudeCursorLog_StripsClaudeOnlyTopLevelFields(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	payload, err := os.ReadFile("/Users/vick/Desktop/project/ccNexus/docs/claude-cursor.log")
	if err != nil {
		t.Fatalf("read claude-cursor log failed: %v", err)
	}

	lines := strings.Split(string(payload), "\n")
	if len(lines) < 5 {
		t.Fatalf("expected claude-cursor log to contain transformed request line")
	}

	result, err := trans.TransformRequest([]byte(lines[3]))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("unmarshal transformed payload failed: %v", err)
	}

	messages, ok := data["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		t.Fatalf("expected claude-cursor body converted to openai messages, got %#v", data["messages"])
	}
	tools, ok := data["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatalf("expected claude-cursor tools converted for openai chat, got %#v", data["tools"])
	}
	for i, rawTool := range tools {
		tool := rawTool.(map[string]interface{})
		if tool["type"] != "function" {
			t.Fatalf("expected tool %d to be function for chat target, got %#v", i, tool)
		}
		if _, ok := tool["function"].(map[string]interface{}); !ok {
			t.Fatalf("expected tool %d to use function wrapper for chat target, got %#v", i, tool)
		}
	}
	if _, ok := data["system"]; ok {
		t.Fatalf("expected top-level claude system to be removed after conversion, got %#v", data["system"])
	}
	if _, ok := data["tool_choice"].(map[string]interface{}); ok {
		t.Fatalf("expected openai target tool_choice to avoid claude object shape, got %#v", data["tool_choice"])
	}
}

func TestOpenAITransformer_TransformRequest_OpenAIResponsesShapeCamelCaseReasoningContentPreserved(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	responsesReq := `{
		"model": "gpt-5.4",
		"input": [{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Hello"}]}],
		"reasoning": {"effort": "medium"},
		"reasoningContent": "Need to inspect files first.",
		"user": "user-123"
	}`

	result, err := trans.TransformRequest([]byte(responsesReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["reasoning_effort"] != "medium" {
		t.Fatalf("expected reasoning effort preserved, got %#v", data["reasoning_effort"])
	}
	if _, ok := data["reasoningContent"]; ok {
		t.Fatalf("expected camelCase reasoningContent to be normalized away, got %#v", data["reasoningContent"])
	}
}
