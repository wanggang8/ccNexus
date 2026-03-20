package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// ========== Claude ↔ OpenAI2 (Responses API) 转换测试 ==========

// --- ClaudeReqToOpenAI2 ---

func TestClaudeReqToOpenAI2_Basic(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"system": "You are helpful.",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 1024,
		"stream": true
	}`

	result, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4o")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var openai2Req map[string]interface{}
	if err := json.Unmarshal(result, &openai2Req); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check model
	if openai2Req["model"] != "gpt-4o" {
		t.Errorf("Expected model 'gpt-4o', got '%v'", openai2Req["model"])
	}

	// Check instructions
	if openai2Req["instructions"] != "You are helpful." {
		t.Errorf("Expected instructions 'You are helpful.', got '%v'", openai2Req["instructions"])
	}

	// Check input
	input := openai2Req["input"].([]interface{})
	if len(input) != 1 {
		t.Errorf("Expected 1 input item, got %d", len(input))
	}

	// Check stream
	if openai2Req["stream"] != true {
		t.Errorf("Expected stream=true, got %v", openai2Req["stream"])
	}
}

func TestClaudeReqToOpenAI2_WithTools(t *testing.T) {
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

	result, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4o")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var openai2Req map[string]interface{}
	if err := json.Unmarshal(result, &openai2Req); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	tools := openai2Req["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	tool := tools[0].(map[string]interface{})
	if tool["name"] != "read_file" {
		t.Errorf("Expected tool name 'read_file', got '%v'", tool["name"])
	}
}

func TestClaudeReqToOpenAI2_WithContentArray(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": [{"type": "text", "text": "Hello"}]}],
		"max_tokens": 1024
	}`

	result, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4o")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var openai2Req map[string]interface{}
	if err := json.Unmarshal(result, &openai2Req); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	input := openai2Req["input"].([]interface{})
	if len(input) != 1 {
		t.Errorf("Expected 1 input item, got %d", len(input))
	}
}

func TestClaudeReqToOpenAI2_WithImages(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "Describe this"},
				{"type": "image", "source": {"type": "base64", "media_type": "image/jpeg", "data": "/9j/4AAQSkZJRgABAQAAAQABAAD/"}}
			]
		}],
		"max_tokens": 1024
	}`

	result, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4o")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var openai2Req map[string]interface{}
	if err := json.Unmarshal(result, &openai2Req); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	input := openai2Req["input"].([]interface{})
	if len(input) != 1 {
		t.Fatalf("Expected 1 input item, got %d", len(input))
	}

	msg := input[0].(map[string]interface{})
	content := msg["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("Expected 2 content parts, got %d", len(content))
	}

	imagePart := content[1].(map[string]interface{})
	if imagePart["type"] != "input_image" {
		t.Fatalf("Expected second part to be input_image, got %#v", imagePart["type"])
	}
	if imagePart["image_url"] != "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQAAAQABAAD/" {
		t.Fatalf("Expected image url to be preserved, got %#v", imagePart["image_url"])
	}
}

func TestClaudeReqToOpenAI2_InvalidJSON(t *testing.T) {
	_, err := ClaudeReqToOpenAI2([]byte("not valid json"), "gpt-4o")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- OpenAI2ReqToClaude ---

func TestOpenAI2ReqToClaude_Basic(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"instructions": "You are helpful.",
		"input": "Hello",
		"max_output_tokens": 1024,
		"stream": true
	}`

	result, err := OpenAI2ReqToClaude([]byte(openai2Req), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAI2ReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check model
	if claudeReq["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("Expected model 'claude-sonnet-4-20250514', got '%v'", claudeReq["model"])
	}

	// Check system
	if claudeReq["system"] != "You are helpful." {
		t.Errorf("Expected system 'You are helpful.', got '%v'", claudeReq["system"])
	}

	// Check max_tokens
	if claudeReq["max_tokens"] != float64(1024) {
		t.Errorf("Expected max_tokens 1024, got %v", claudeReq["max_tokens"])
	}

	// Check messages
	messages := claudeReq["messages"].([]interface{})
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
}

func TestOpenAI2ReqToClaude_WithArrayInput(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Hello"}]},
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "Hi!"}]}
		]
	}`

	result, err := OpenAI2ReqToClaude([]byte(openai2Req), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAI2ReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	messages := claudeReq["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(messages))
	}
}

func TestOpenAI2ReqToClaude_WithImagesAndEnableThinking(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"input": [
			{
				"type": "message",
				"role": "user",
				"content": [
					{"type": "input_text", "text": "Describe this"},
					{"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB"}
				]
			}
		],
		"enable_thinking": true,
		"max_output_tokens": 10
	}`

	result, err := OpenAI2ReqToClaude([]byte(openai2Req), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAI2ReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	thinking, ok := claudeReq["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected thinking config, got %#v", claudeReq["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("expected thinking.type=enabled, got %#v", thinking["type"])
	}
	if thinking["budget_tokens"] != float64(9) {
		t.Fatalf("expected budget_tokens=9, got %#v", thinking["budget_tokens"])
	}

	messages := claudeReq["messages"].([]interface{})
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	content := messages[0].(map[string]interface{})["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(content))
	}
	imageBlock := content[1].(map[string]interface{})
	if imageBlock["type"] != "image" {
		t.Fatalf("expected image block, got %#v", imageBlock["type"])
	}
	source := imageBlock["source"].(map[string]interface{})
	if source["type"] != "base64" {
		t.Fatalf("expected base64 source, got %#v", source["type"])
	}
	if source["media_type"] != "image/png" {
		t.Fatalf("expected media_type image/png, got %#v", source["media_type"])
	}
}

func TestOpenAI2ReqToClaude_WithFunctionCall(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Read file"}]},
			{"type": "function_call", "call_id": "call_1", "name": "read_file", "arguments": "{\"path\":\"/tmp/a\"}"},
			{"type": "function_call_output", "call_id": "call_1", "output": "file content"}
		]
	}`

	result, err := OpenAI2ReqToClaude([]byte(openai2Req), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAI2ReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	messages := claudeReq["messages"].([]interface{})
	// Should have: user, assistant (with tool_use), user (with tool_result)
	if len(messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(messages))
	}
}

func TestOpenAI2ReqToClaude_WithTools(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"input": "Hello",
		"tools": [
			{"type": "function", "name": "read_file", "description": "Read a file", "parameters": {"type": "object"}}
		]
	}`

	result, err := OpenAI2ReqToClaude([]byte(openai2Req), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAI2ReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	tools := claudeReq["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	tool := tools[0].(map[string]interface{})
	if tool["name"] != "read_file" {
		t.Errorf("Expected tool name 'read_file', got '%v'", tool["name"])
	}
	if tool["input_schema"] == nil {
		t.Error("Expected input_schema, got nil")
	}
}

func TestOpenAI2ReqToClaude_WithCustomTool(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"input": "Hello",
		"tools": [
			{"type": "custom", "name": "apply_patch", "description": "Apply a patch"}
		]
	}`

	result, err := OpenAI2ReqToClaude([]byte(openai2Req), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAI2ReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	tools := claudeReq["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}
}

func TestOpenAI2ReqToClaude_WithTemperature(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"input": "Hello",
		"temperature": 0.7
	}`

	result, err := OpenAI2ReqToClaude([]byte(openai2Req), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAI2ReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if claudeReq["temperature"] != 0.7 {
		t.Errorf("Expected temperature 0.7, got %v", claudeReq["temperature"])
	}
}

func TestOpenAI2ReqToClaude_InvalidJSON(t *testing.T) {
	_, err := OpenAI2ReqToClaude([]byte("not valid json"), "claude-sonnet-4-20250514")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- ClaudeRespToOpenAI2 ---

func TestClaudeRespToOpenAI2_Basic(t *testing.T) {
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello!"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	result, err := ClaudeRespToOpenAI2([]byte(claudeResp))
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI2 failed: %v", err)
	}

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check object
	if openai2Resp["object"] != "response" {
		t.Errorf("Expected object 'response', got '%v'", openai2Resp["object"])
	}

	// Check status
	if openai2Resp["status"] != "completed" {
		t.Errorf("Expected status 'completed', got '%v'", openai2Resp["status"])
	}

	// Check output
	output := openai2Resp["output"].([]interface{})
	if len(output) != 1 {
		t.Fatalf("Expected 1 output item, got %d", len(output))
	}
}

func TestClaudeRespToOpenAI2_WithToolUse(t *testing.T) {
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Let me read that."},
			{"type": "tool_use", "id": "toolu_1", "name": "read_file", "input": {"path": "/tmp/a"}}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 10, "output_tokens": 15}
	}`

	result, err := ClaudeRespToOpenAI2([]byte(claudeResp))
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI2 failed: %v", err)
	}

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	output := openai2Resp["output"].([]interface{})
	// Should have message + function_call
	if len(output) != 2 {
		t.Fatalf("Expected 2 output items, got %d", len(output))
	}

	funcCall := output[1].(map[string]interface{})
	if funcCall["type"] != "function_call" {
		t.Errorf("Expected type 'function_call', got '%v'", funcCall["type"])
	}
}

func TestClaudeRespToOpenAI2_WithThinking(t *testing.T) {
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "thinking", "thinking": "Let me think..."},
			{"type": "text", "text": "Hello!"}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	result, err := ClaudeRespToOpenAI2([]byte(claudeResp))
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI2 failed: %v", err)
	}

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Thinking should be skipped
	output := openai2Resp["output"].([]interface{})
	if len(output) != 1 {
		t.Fatalf("Expected 1 output item (thinking skipped), got %d", len(output))
	}
}

func TestClaudeRespToOpenAI2_PreservesContentOrder(t *testing.T) {
	claudeResp := `{
		"id": "msg_order",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "tool_use", "id": "toolu_early", "name": "read_file", "input": {"path": "/tmp/a"}},
			{"type": "text", "text": "Done."}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	result, err := ClaudeRespToOpenAI2([]byte(claudeResp))
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI2 failed: %v", err)
	}

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	output := openai2Resp["output"].([]interface{})
	if len(output) != 2 {
		t.Fatalf("Expected 2 output items, got %d", len(output))
	}
	first := output[0].(map[string]interface{})
	if first["type"] != "function_call" {
		t.Fatalf("expected first output item to preserve tool order, got %#v", first)
	}
}

func TestClaudeRespToOpenAI2_InvalidJSON(t *testing.T) {
	_, err := ClaudeRespToOpenAI2([]byte("not valid json"))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- OpenAI2RespToClaude ---

func TestOpenAI2RespToClaude_Basic(t *testing.T) {
	openai2Resp := `{
		"id": "resp_123",
		"object": "response",
		"status": "completed",
		"output": [
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "Hello!"}]}
		],
		"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
	}`

	result, err := OpenAI2RespToClaude([]byte(openai2Resp))
	if err != nil {
		t.Fatalf("OpenAI2RespToClaude failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(result, &claudeResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check type
	if claudeResp["type"] != "message" {
		t.Errorf("Expected type 'message', got '%v'", claudeResp["type"])
	}

	// Check role
	if claudeResp["role"] != "assistant" {
		t.Errorf("Expected role 'assistant', got '%v'", claudeResp["role"])
	}

	// Check stop_reason
	if claudeResp["stop_reason"] != "end_turn" {
		t.Errorf("Expected stop_reason 'end_turn', got '%v'", claudeResp["stop_reason"])
	}
}

func TestOpenAI2RespToClaude_WithFunctionCall(t *testing.T) {
	openai2Resp := `{
		"id": "resp_123",
		"object": "response",
		"status": "completed",
		"output": [
			{"type": "function_call", "call_id": "call_1", "name": "read_file", "arguments": "{\"path\":\"/tmp/a\"}"}
		],
		"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
	}`

	result, err := OpenAI2RespToClaude([]byte(openai2Resp))
	if err != nil {
		t.Fatalf("OpenAI2RespToClaude failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(result, &claudeResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check stop_reason
	if claudeResp["stop_reason"] != "tool_use" {
		t.Errorf("Expected stop_reason 'tool_use', got '%v'", claudeResp["stop_reason"])
	}

	// Check content
	content := claudeResp["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(content))
	}

	block := content[0].(map[string]interface{})
	if block["type"] != "tool_use" {
		t.Errorf("Expected type 'tool_use', got '%v'", block["type"])
	}
}

func TestOpenAI2RespToClaude_InvalidJSON(t *testing.T) {
	_, err := OpenAI2RespToClaude([]byte("not valid json"))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- ClaudeStreamToOpenAI2 ---

func TestClaudeStreamToOpenAI2_MessageStart(t *testing.T) {
	ctx := transformer.NewStreamContext()

	claudeEvent := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"usage":{"input_tokens":10,"output_tokens":0}}}

`

	result, err := ClaudeStreamToOpenAI2([]byte(claudeEvent), ctx)
	if err != nil {
		t.Fatalf("ClaudeStreamToOpenAI2 failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "response.created") {
		t.Errorf("Expected 'response.created' event, got '%s'", resultStr)
	}

	// Check context updated
	if ctx.MessageID != "msg_1" {
		t.Errorf("Expected MessageID 'msg_1', got '%v'", ctx.MessageID)
	}
}

func TestClaudeStreamToOpenAI2_TextDelta(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "msg_1"
	ctx.ContentBlockStarted = true
	ctx.ContentIndex = 0

	claudeEvent := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

`

	result, err := ClaudeStreamToOpenAI2([]byte(claudeEvent), ctx)
	if err != nil {
		t.Fatalf("ClaudeStreamToOpenAI2 failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "response.output_text.delta") {
		t.Errorf("Expected 'response.output_text.delta' event, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "Hello") {
		t.Errorf("Expected 'Hello' in result, got '%s'", resultStr)
	}
}

func TestClaudeStreamToOpenAI2_ToolUse(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "msg_1"

	claudeEvent := `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file","input":{}}}

`

	result, err := ClaudeStreamToOpenAI2([]byte(claudeEvent), ctx)
	if err != nil {
		t.Fatalf("ClaudeStreamToOpenAI2 failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "response.output_item.added") {
		t.Errorf("Expected 'response.output_item.added' event, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "function_call") {
		t.Errorf("Expected 'function_call' in result, got '%s'", resultStr)
	}
}

func TestClaudeStreamToOpenAI2_UsesStableOutputIndexes(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "msg_1"

	textStart := `event: content_block_start
data: {"type":"content_block_start","index":3,"content_block":{"type":"text","text":""}}

`
	toolStart := `event: content_block_start
data: {"type":"content_block_start","index":7,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file","input":{}}}

`

	out1, err := ClaudeStreamToOpenAI2([]byte(textStart), ctx)
	if err != nil {
		t.Fatalf("text start failed: %v", err)
	}
	out2, err := ClaudeStreamToOpenAI2([]byte(toolStart), ctx)
	if err != nil {
		t.Fatalf("tool start failed: %v", err)
	}

	combined := string(out1) + string(out2)
	if !strings.Contains(combined, `"output_index":0`) {
		t.Fatalf("expected first emitted item to use stable index 0, got: %s", combined)
	}
	if !strings.Contains(combined, `"output_index":1`) {
		t.Fatalf("expected second emitted item to use stable index 1, got: %s", combined)
	}
}

func TestClaudeStreamToOpenAI2_MessageStop(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "msg_1"
	ctx.InputTokens = 10
	ctx.OutputTokens = 5

	claudeEvent := `event: message_stop
data: {"type":"message_stop"}

`

	result, err := ClaudeStreamToOpenAI2([]byte(claudeEvent), ctx)
	if err != nil {
		t.Fatalf("ClaudeStreamToOpenAI2 failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "response.completed") {
		t.Errorf("Expected 'response.completed' event, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "[DONE]") {
		t.Errorf("Expected '[DONE]' in result, got '%s'", resultStr)
	}
}

func TestClaudeStreamToOpenAI2_Empty(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := ClaudeStreamToOpenAI2([]byte(""), ctx)
	if err != nil {
		t.Fatalf("ClaudeStreamToOpenAI2 failed: %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil for empty event, got %v", result)
	}
}

// --- OpenAI2StreamToClaude ---

func TestOpenAI2StreamToClaude_Created(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-sonnet-4-20250514"

	openai2SSE := `data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`

	result, err := OpenAI2StreamToClaude([]byte(openai2SSE), ctx)
	if err != nil {
		t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "message_start") {
		t.Errorf("Expected 'message_start' event, got '%s'", resultStr)
	}

	// Check context updated
	if ctx.MessageID != "resp_1" {
		t.Errorf("Expected MessageID 'resp_1', got '%v'", ctx.MessageID)
	}
}

func TestOpenAI2StreamToClaude_TextDelta(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "resp_1"

	openai2SSE := `data: {"type":"response.output_text.delta","delta":"Hello"}`

	result, err := OpenAI2StreamToClaude([]byte(openai2SSE), ctx)
	if err != nil {
		t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "content_block") {
		t.Errorf("Expected content_block events, got '%s'", resultStr)
	}
}

func TestOpenAI2StreamToClaude_FunctionCall(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "resp_1"

	openai2SSE := `data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_1","name":"read_file"}}`

	result, err := OpenAI2StreamToClaude([]byte(openai2SSE), ctx)
	if err != nil {
		t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "content_block_start") {
		t.Errorf("Expected 'content_block_start' event, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "tool_use") {
		t.Errorf("Expected 'tool_use' in result, got '%s'", resultStr)
	}
}

func TestOpenAI2StreamToClaude_Completed(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "resp_1"

	openai2SSE := `data: {"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`

	result, err := OpenAI2StreamToClaude([]byte(openai2SSE), ctx)
	if err != nil {
		t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "message_delta") {
		t.Errorf("Expected 'message_delta' event, got '%s'", resultStr)
	}
}

func TestOpenAI2StreamToClaude_Done(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "resp_1"

	result, err := OpenAI2StreamToClaude([]byte("data: [DONE]"), ctx)
	if err != nil {
		t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "message_stop") {
		t.Errorf("Expected 'message_stop' event, got '%s'", resultStr)
	}
}

func TestOpenAI2StreamToClaude_Empty(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := OpenAI2StreamToClaude([]byte(""), ctx)
	if err != nil {
		t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil for empty event, got %v", result)
	}
}

func TestOpenAI2StreamToClaudeCompletesWithoutDone(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"

	chunks := []string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed"}}`,
	}

	var allEvents []string
	for _, chunk := range chunks {
		events, err := OpenAI2StreamToClaude([]byte(chunk), ctx)
		if err != nil {
			t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
		}
		if events != nil {
			allEvents = append(allEvents, string(events))
		}
	}

	fullEvents := strings.Join(allEvents, "")
	if !strings.Contains(fullEvents, "\"type\":\"message_delta\"") {
		t.Fatalf("Expected message_delta in transformed events, got: %s", fullEvents)
	}
	if !strings.Contains(fullEvents, "event: message_stop") {
		t.Fatalf("Expected message_stop when response.completed arrives without [DONE], got: %s", fullEvents)
	}
}

func TestOpenAI2StreamToClaudePropagatesUsageFromCompleted(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"

	chunks := []string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","usage":{"input_tokens":7,"output_tokens":3,"total_tokens":10}}}`,
	}

	var allEvents []string
	for _, chunk := range chunks {
		events, err := OpenAI2StreamToClaude([]byte(chunk), ctx)
		if err != nil {
			t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
		}
		if events != nil {
			allEvents = append(allEvents, string(events))
		}
	}

	fullEvents := strings.Join(allEvents, "")
	if !strings.Contains(fullEvents, `"usage":{"output_tokens":3}`) {
		t.Fatalf("expected message_delta usage output_tokens=3, got: %s", fullEvents)
	}
	if ctx.InputTokens != 7 || ctx.OutputTokens != 3 {
		t.Fatalf("expected context usage input=7 output=3, got input=%d output=%d", ctx.InputTokens, ctx.OutputTokens)
	}
}

func TestClaudeReqToOpenAI2PreservesToolChain(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"stream": false,
		"messages": [
			{"role":"user","content":"请写文件"},
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Write","input":{"file_path":"/tmp/a.txt","content":"hello"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}
		],
		"tools": [
			{"name":"Write","description":"Write file","input_schema":{"type":"object","properties":{"file_path":{"type":"string"},"content":{"type":"string"}},"required":["file_path","content"]}}
		]
	}`

	reqBytes, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	input, ok := req["input"].([]interface{})
	if !ok {
		t.Fatalf("input should be array, got %T", req["input"])
	}
	if len(input) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(input))
	}

	functionCall, ok := input[1].(map[string]interface{})
	if !ok || functionCall["type"] != "function_call" {
		t.Fatalf("expected input[1] function_call, got %#v", input[1])
	}
	if functionCall["call_id"] != "toolu_1" {
		t.Fatalf("expected call_id toolu_1, got %#v", functionCall["call_id"])
	}
	if _, hasID := functionCall["id"]; hasID {
		t.Fatalf("function_call.id should not be set for upstream compatibility, got %#v", functionCall["id"])
	}

	argsStr, _ := functionCall["arguments"].(string)
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		t.Fatalf("function arguments is not valid json: %v, raw=%s", err, argsStr)
	}
	if args["file_path"] != "/tmp/a.txt" {
		t.Fatalf("unexpected function arguments: %#v", args)
	}

	functionOutput, ok := input[2].(map[string]interface{})
	if !ok || functionOutput["type"] != "function_call_output" {
		t.Fatalf("expected input[2] function_call_output, got %#v", input[2])
	}
	if functionOutput["call_id"] != "toolu_1" {
		t.Fatalf("expected output call_id toolu_1, got %#v", functionOutput["call_id"])
	}
	if functionOutput["output"] != "ok" {
		t.Fatalf("expected output ok, got %#v", functionOutput["output"])
	}

	if strings.Contains(string(reqBytes), "[Tool Call:") || strings.Contains(string(reqBytes), "[Tool Result:") {
		t.Fatalf("found legacy pseudo tool text in transformed request: %s", string(reqBytes))
	}
}

func TestOpenAI2RespToClaudeFallbackToItemID(t *testing.T) {
	openai2Resp := `{
		"id":"resp_1",
		"object":"response",
		"status":"completed",
		"output":[{"type":"function_call","id":"fc_123","name":"Write","arguments":"{\"file_path\":\"/tmp/a.txt\"}"}],
		"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}
	}`

	claudeRespBytes, err := OpenAI2RespToClaude([]byte(openai2Resp))
	if err != nil {
		t.Fatalf("OpenAI2RespToClaude failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(claudeRespBytes, &claudeResp); err != nil {
		t.Fatalf("unmarshal claude resp failed: %v", err)
	}

	content, ok := claudeResp["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("unexpected content: %#v", claudeResp["content"])
	}

	toolUse, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("tool_use item type invalid: %#v", content[0])
	}
	if toolUse["type"] != "tool_use" {
		t.Fatalf("expected tool_use type, got %#v", toolUse["type"])
	}
	if toolUse["id"] != "fc_123" {
		t.Fatalf("expected tool_use id from item.id fallback, got %#v", toolUse["id"])
	}
}

func TestClaudeReqToOpenAI2MapsToolChoiceAnyToRequired(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"stream": true,
		"messages": [{"role":"user","content":"test"}],
		"tools": [{"name":"Write","description":"Write file","input_schema":{"type":"object"}}],
		"tool_choice": {"type":"any"}
	}`

	reqBytes, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	if req["tool_choice"] != "required" {
		t.Fatalf("expected tool_choice=required, got %#v", req["tool_choice"])
	}
	if _, ok := req["store"]; ok {
		t.Fatalf("did not expect store in generic claude->openai2 conversion, got %#v", req["store"])
	}
	if _, ok := req["instructions"]; ok {
		t.Fatalf("did not expect instructions without system prompt, got %#v", req["instructions"])
	}
}

func TestClaudeReqToOpenAI2MapsNamedToolChoice(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"stream": true,
		"messages": [{"role":"user","content":"test"}],
		"tools": [{"name":"Write","description":"Write file","input_schema":{"type":"object"}}],
		"tool_choice": {"type":"tool","name":"Write"}
	}`

	reqBytes, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	toolChoice, ok := req["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected object tool_choice, got %#v", req["tool_choice"])
	}
	if toolChoice["type"] != "function" || toolChoice["name"] != "Write" {
		t.Fatalf("unexpected tool_choice mapping: %#v", toolChoice)
	}
}

func TestClaudeReqToOpenAI2DefaultsToolChoiceRequiredWhenToolsPresent(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"stream": true,
		"messages": [{"role":"user","content":"test"}],
		"tools": [{"name":"Write","description":"Write file","input_schema":{"type":"object"}}]
	}`

	reqBytes, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	if req["tool_choice"] != "required" {
		t.Fatalf("expected tool_choice=required, got %#v", req["tool_choice"])
	}
}

func TestClaudeReqToOpenAI2DefaultsToolChoiceAutoAfterToolResult(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"stream": true,
		"messages": [
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/tmp/a"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}
		],
		"tools": [{"name":"Read","description":"Read file","input_schema":{"type":"object"}}]
	}`

	reqBytes, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	if req["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice=auto after tool_result, got %#v", req["tool_choice"])
	}
}
