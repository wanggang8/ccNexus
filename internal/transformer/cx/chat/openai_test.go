package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

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

// ========== Response Tests ==========

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
