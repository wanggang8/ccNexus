package convert

import (
	"encoding/json"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// ========== 边界条件测试 ==========

// --- ClaudeReqToOpenAI 边界条件 ---

func TestClaudeReqToOpenAI_EmptyMessages(t *testing.T) {
	claudeReq := `{
		"model": "claude-3-opus-20240229",
		"messages": [],
		"max_tokens": 1024
	}`

	result, err := ClaudeReqToOpenAI([]byte(claudeReq), "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI failed: %v", err)
	}

	var openaiReq map[string]interface{}
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Empty messages may result in nil or empty array
	messages := openaiReq["messages"]
	if messages != nil {
		if arr, ok := messages.([]interface{}); ok && len(arr) != 0 {
			t.Errorf("Expected 0 or nil messages, got %d", len(arr))
		}
	}
}

func TestClaudeReqToOpenAI_InvalidJSON(t *testing.T) {
	_, err := ClaudeReqToOpenAI([]byte("not valid json"), "gpt-4")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestClaudeReqToOpenAI_NilSystem(t *testing.T) {
	claudeReq := `{
		"model": "claude-3-opus-20240229",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 1024
	}`

	result, err := ClaudeReqToOpenAI([]byte(claudeReq), "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI failed: %v", err)
	}

	var openaiReq map[string]interface{}
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should have only user message, no system
	messages := openaiReq["messages"].([]interface{})
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
}

func TestClaudeReqToOpenAI_EmptyModel(t *testing.T) {
	claudeReq := `{
		"model": "claude-3-opus-20240229",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 1024
	}`

	result, err := ClaudeReqToOpenAI([]byte(claudeReq), "")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI failed: %v", err)
	}

	var openaiReq map[string]interface{}
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Empty model should result in empty model field
	if openaiReq["model"] != "" {
		t.Errorf("Expected empty model, got '%v'", openaiReq["model"])
	}
}

// --- OpenAIReqToClaude 边界条件 ---

func TestOpenAIReqToClaude_EmptyMessages(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4",
		"messages": []
	}`

	result, err := OpenAIReqToClaude([]byte(openaiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAIReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Empty messages may result in nil or empty array
	messages := claudeReq["messages"]
	if messages != nil {
		if arr, ok := messages.([]interface{}); ok && len(arr) != 0 {
			t.Errorf("Expected 0 or nil messages, got %d", len(arr))
		}
	}
}

func TestOpenAIReqToClaude_InvalidJSON(t *testing.T) {
	_, err := OpenAIReqToClaude([]byte("not valid json"), "claude-sonnet-4-20250514")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestOpenAIReqToClaude_NullContent(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": null}]
	}`

	result, err := OpenAIReqToClaude([]byte(openaiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAIReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should handle null content gracefully
	if claudeReq["messages"] == nil {
		t.Error("Expected messages field")
	}
}

func TestOpenAIReqToClaude_EmptyToolArguments(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "test", "arguments": ""}}
			]}
		]
	}`

	result, err := OpenAIReqToClaude([]byte(openaiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAIReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should handle empty arguments gracefully
	if claudeReq["messages"] == nil {
		t.Error("Expected messages field")
	}
}

// --- ClaudeRespToOpenAI 边界条件 ---

func TestClaudeRespToOpenAI_EmptyContent(t *testing.T) {
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 0}
	}`

	result, err := ClaudeRespToOpenAI([]byte(claudeResp), "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	choices := openaiResp["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})

	// Empty content should result in empty string
	if message["content"] != "" {
		t.Errorf("Expected empty content, got '%v'", message["content"])
	}
}

func TestClaudeRespToOpenAI_InvalidJSON(t *testing.T) {
	_, err := ClaudeRespToOpenAI([]byte("not valid json"), "gpt-4")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestClaudeRespToOpenAI_NullUsage(t *testing.T) {
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn"
	}`

	result, err := ClaudeRespToOpenAI([]byte(claudeResp), "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should have usage with zeros
	usage := openaiResp["usage"].(map[string]interface{})
	if usage["prompt_tokens"] != float64(0) {
		t.Errorf("Expected prompt_tokens 0, got %v", usage["prompt_tokens"])
	}
}

// --- OpenAIRespToClaude 边界条件 ---

func TestOpenAIRespToClaude_EmptyChoices(t *testing.T) {
	openaiResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [],
		"usage": {"prompt_tokens": 10, "completion_tokens": 0, "total_tokens": 10}
	}`

	result, err := OpenAIRespToClaude([]byte(openaiResp))
	if err != nil {
		t.Fatalf("OpenAIRespToClaude failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(result, &claudeResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should have empty content array
	content := claudeResp["content"].([]interface{})
	if len(content) != 0 {
		t.Errorf("Expected 0 content blocks, got %d", len(content))
	}
}

func TestOpenAIRespToClaude_InvalidJSON(t *testing.T) {
	_, err := OpenAIRespToClaude([]byte("not valid json"))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestOpenAIRespToClaude_NullContent(t *testing.T) {
	openaiResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": null},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 0, "total_tokens": 10}
	}`

	result, err := OpenAIRespToClaude([]byte(openaiResp))
	if err != nil {
		t.Fatalf("OpenAIRespToClaude failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(result, &claudeResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should handle null content gracefully
	content := claudeResp["content"].([]interface{})
	if len(content) != 0 {
		t.Errorf("Expected 0 content blocks for null content, got %d", len(content))
	}
}

// --- ClaudeStreamToOpenAI 边界条件 ---

func TestClaudeStreamToOpenAI_EmptyEvent(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := ClaudeStreamToOpenAI([]byte(""), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeStreamToOpenAI failed: %v", err)
	}

	// Empty input should return nil
	if result != nil {
		t.Errorf("Expected nil for empty event, got %v", result)
	}
}

func TestClaudeStreamToOpenAI_InvalidSSE(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := ClaudeStreamToOpenAI([]byte("not a valid sse event"), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeStreamToOpenAI failed: %v", err)
	}

	// Invalid SSE should return nil (graceful handling)
	if result != nil {
		t.Errorf("Expected nil for invalid SSE, got %v", result)
	}
}

func TestClaudeStreamToOpenAI_NilContext(t *testing.T) {
	claudeEvent := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1"}}

`

	// Nil context may cause panic or return nil - test graceful handling
	defer func() {
		if r := recover(); r != nil {
			// Panic is acceptable for nil context - it's a programming error
			t.Logf("Recovered from panic with nil context: %v", r)
		}
	}()

	result, err := ClaudeStreamToOpenAI([]byte(claudeEvent), nil, "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeStreamToOpenAI failed: %v", err)
	}

	// Result may be nil or valid - both are acceptable
	_ = result
}

// --- OpenAIStreamToClaude 边界条件 ---

func TestOpenAIStreamToClaude_EmptyEvent(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := OpenAIStreamToClaude([]byte(""), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToClaude failed: %v", err)
	}

	// Empty input should return nil
	if result != nil {
		t.Errorf("Expected nil for empty event, got %v", result)
	}
}

func TestOpenAIStreamToClaude_InvalidSSE(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := OpenAIStreamToClaude([]byte("not a valid sse event"), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToClaude failed: %v", err)
	}

	// Invalid SSE should return nil (graceful handling)
	if result != nil {
		t.Errorf("Expected nil for invalid SSE, got %v", result)
	}
}

func TestOpenAIStreamToClaude_DoneEvent(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageStartSent = true
	ctx.MessageID = "msg_1"

	result, err := OpenAIStreamToClaude([]byte("data: [DONE]"), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToClaude failed: %v", err)
	}

	// Should return message_stop event
	if result == nil {
		t.Error("Expected result for [DONE] event")
	}
}

// --- 工具调用边界条件 ---

func TestClaudeRespToOpenAI_InvalidToolInput(t *testing.T) {
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "tool_use", "id": "toolu_1", "name": "test", "input": "not an object"}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	result, err := ClaudeRespToOpenAI([]byte(claudeResp), "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should handle invalid input gracefully
	choices := openaiResp["choices"].([]interface{})
	if len(choices) != 1 {
		t.Errorf("Expected 1 choice, got %d", len(choices))
	}
}

func TestOpenAIRespToClaude_InvalidToolArguments(t *testing.T) {
	openaiResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "test", "arguments": "not valid json"}}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`

	result, err := OpenAIRespToClaude([]byte(openaiResp))
	if err != nil {
		t.Fatalf("OpenAIRespToClaude failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(result, &claudeResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should handle invalid arguments gracefully (use empty object)
	content := claudeResp["content"].([]interface{})
	if len(content) != 1 {
		t.Errorf("Expected 1 content block, got %d", len(content))
	}

	block := content[0].(map[string]interface{})
	if block["type"] != "tool_use" {
		t.Errorf("Expected tool_use type, got %v", block["type"])
	}
	// Input should be empty object due to invalid JSON
	input := block["input"].(map[string]interface{})
	if len(input) != 0 {
		t.Errorf("Expected empty input for invalid arguments, got %v", input)
	}
}

// --- 特殊字符和编码 ---

func TestClaudeRespToOpenAI_UnicodeContent(t *testing.T) {
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "你好世界 🌍 émoji"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	result, err := ClaudeRespToOpenAI([]byte(claudeResp), "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	choices := openaiResp["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})

	if message["content"] != "你好世界 🌍 émoji" {
		t.Errorf("Unicode content not preserved: %v", message["content"])
	}
}

func TestOpenAIRespToClaude_UnicodeContent(t *testing.T) {
	openaiResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "你好世界 🌍 émoji"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`

	result, err := OpenAIRespToClaude([]byte(openaiResp))
	if err != nil {
		t.Fatalf("OpenAIRespToClaude failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(result, &claudeResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	content := claudeResp["content"].([]interface{})
	block := content[0].(map[string]interface{})

	if block["text"] != "你好世界 🌍 émoji" {
		t.Errorf("Unicode content not preserved: %v", block["text"])
	}
}

// --- 大数据量测试 ---

func TestClaudeRespToOpenAI_LargeContent(t *testing.T) {
	// Generate large content
	largeText := ""
	for i := 0; i < 10000; i++ {
		largeText += "Hello world. "
	}

	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "` + largeText + `"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 50000}
	}`

	result, err := ClaudeRespToOpenAI([]byte(claudeResp), "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	choices := openaiResp["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})

	if len(message["content"].(string)) != len(largeText) {
		t.Errorf("Large content length mismatch: expected %d, got %d", len(largeText), len(message["content"].(string)))
	}
}
