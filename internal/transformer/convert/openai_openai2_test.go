package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// ========== OpenAI ↔ OpenAI2 (Responses API) 转换测试 ==========

// --- OpenAIReqToOpenAI2 ---

func TestOpenAIReqToOpenAI2_Basic(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello"}
		],
		"stream": true
	}`

	result, err := OpenAIReqToOpenAI2([]byte(openaiReq), "gpt-4o")
	if err != nil {
		t.Fatalf("OpenAIReqToOpenAI2 failed: %v", err)
	}

	var openai2Req map[string]interface{}
	if err := json.Unmarshal(result, &openai2Req); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check model
	if openai2Req["model"] != "gpt-4o" {
		t.Errorf("Expected model 'gpt-4o', got '%v'", openai2Req["model"])
	}

	// Check instructions (from system)
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

func TestOpenAIReqToOpenAI2_WithTools(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "Read file"}],
		"tools": [{
			"type": "function",
			"function": {
				"name": "read_file",
				"description": "Read a file",
				"parameters": {"type": "object", "properties": {"path": {"type": "string"}}}
			}
		}]
	}`

	result, err := OpenAIReqToOpenAI2([]byte(openaiReq), "gpt-4o")
	if err != nil {
		t.Fatalf("OpenAIReqToOpenAI2 failed: %v", err)
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

func TestOpenAIReqToOpenAI2_InvalidJSON(t *testing.T) {
	_, err := OpenAIReqToOpenAI2([]byte("not valid json"), "gpt-4o")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- OpenAI2ReqToOpenAI ---

func TestOpenAI2ReqToOpenAI_Basic(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"instructions": "You are helpful.",
		"input": "Hello",
		"max_output_tokens": 1024,
		"stream": true
	}`

	result, err := OpenAI2ReqToOpenAI([]byte(openai2Req), "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}

	var openaiReq transformer.OpenAIRequest
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check model
	if openaiReq.Model != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%v'", openaiReq.Model)
	}

	// Check messages (system + user)
	if len(openaiReq.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(openaiReq.Messages))
	}

	if openaiReq.Messages[0].Role != "system" {
		t.Errorf("Expected first message role 'system', got '%v'", openaiReq.Messages[0].Role)
	}

	// Check max_completion_tokens
	if openaiReq.MaxCompletionTokens != 1024 {
		t.Errorf("Expected max_completion_tokens 1024, got %d", openaiReq.MaxCompletionTokens)
	}
}

func TestOpenAI2ReqToOpenAI_WithArrayInput(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Hello"}]},
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "Hi!"}]}
		]
	}`

	result, err := OpenAI2ReqToOpenAI([]byte(openai2Req), "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}

	var openaiReq transformer.OpenAIRequest
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if len(openaiReq.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(openaiReq.Messages))
	}
}

func TestOpenAI2ReqToOpenAI_WithFunctionCall(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Read file"}]},
			{"type": "function_call", "call_id": "call_1", "name": "read_file", "arguments": "{\"path\":\"/tmp/a\"}"},
			{"type": "function_call_output", "call_id": "call_1", "output": "file content"}
		]
	}`

	result, err := OpenAI2ReqToOpenAI([]byte(openai2Req), "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}

	var openaiReq transformer.OpenAIRequest
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should have: user, assistant (with tool_calls), tool
	if len(openaiReq.Messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(openaiReq.Messages))
	}

	// Check tool message
	toolMsg := openaiReq.Messages[2]
	if toolMsg.Role != "tool" {
		t.Errorf("Expected role 'tool', got '%v'", toolMsg.Role)
	}
	if toolMsg.ToolCallID != "call_1" {
		t.Errorf("Expected tool_call_id 'call_1', got '%v'", toolMsg.ToolCallID)
	}
}

func TestOpenAI2ReqToOpenAI_WithCustomTool(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"input": "Hello",
		"tools": [
			{"type": "custom", "name": "apply_patch", "description": "Apply a patch"}
		]
	}`

	result, err := OpenAI2ReqToOpenAI([]byte(openai2Req), "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}

	var openaiReq transformer.OpenAIRequest
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if len(openaiReq.Tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(openaiReq.Tools))
	}

	// Custom tool should be converted to function with input parameter
	if openaiReq.Tools[0].Function.Name != "apply_patch" {
		t.Errorf("Expected tool name 'apply_patch', got '%v'", openaiReq.Tools[0].Function.Name)
	}
}

func TestOpenAI2ReqToOpenAI_InvalidJSON(t *testing.T) {
	_, err := OpenAI2ReqToOpenAI([]byte("not valid json"), "gpt-4")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- OpenAIRespToOpenAI2 ---

func TestOpenAIRespToOpenAI2_Basic(t *testing.T) {
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

	result, err := OpenAIRespToOpenAI2([]byte(openaiResp))
	if err != nil {
		t.Fatalf("OpenAIRespToOpenAI2 failed: %v", err)
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

func TestOpenAIRespToOpenAI2_WithToolCalls(t *testing.T) {
	openaiResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\":\"/tmp/a\"}"}}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`

	result, err := OpenAIRespToOpenAI2([]byte(openaiResp))
	if err != nil {
		t.Fatalf("OpenAIRespToOpenAI2 failed: %v", err)
	}

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	output := openai2Resp["output"].([]interface{})
	if len(output) != 1 {
		t.Fatalf("Expected 1 output item (function_call), got %d", len(output))
	}

	funcCall := output[0].(map[string]interface{})
	if funcCall["type"] != "function_call" {
		t.Errorf("Expected type 'function_call', got '%v'", funcCall["type"])
	}
}

func TestOpenAIRespToOpenAI2_InvalidJSON(t *testing.T) {
	_, err := OpenAIRespToOpenAI2([]byte("not valid json"))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- OpenAI2RespToOpenAI ---

func TestOpenAI2RespToOpenAI_Basic(t *testing.T) {
	openai2Resp := `{
		"id": "resp_123",
		"object": "response",
		"status": "completed",
		"output": [
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "Hello!"}]}
		],
		"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
	}`

	result, err := OpenAI2RespToOpenAI([]byte(openai2Resp), "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2RespToOpenAI failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check object
	if openaiResp["object"] != "chat.completion" {
		t.Errorf("Expected object 'chat.completion', got '%v'", openaiResp["object"])
	}

	// Check choices
	choices := openaiResp["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})

	if message["content"] != "Hello!" {
		t.Errorf("Expected content 'Hello!', got '%v'", message["content"])
	}
}

func TestOpenAI2RespToOpenAI_WithFunctionCall(t *testing.T) {
	openai2Resp := `{
		"id": "resp_123",
		"object": "response",
		"status": "completed",
		"output": [
			{"type": "function_call", "call_id": "call_1", "name": "read_file", "arguments": "{\"path\":\"/tmp/a\"}"}
		],
		"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
	}`

	result, err := OpenAI2RespToOpenAI([]byte(openai2Resp), "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2RespToOpenAI failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	choices := openaiResp["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})

	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("Expected finish_reason 'tool_calls', got '%v'", choice["finish_reason"])
	}

	message := choice["message"].(map[string]interface{})
	toolCalls := message["tool_calls"].([]interface{})
	if len(toolCalls) != 1 {
		t.Fatalf("Expected 1 tool_call, got %d", len(toolCalls))
	}
}

func TestOpenAI2RespToOpenAI_InvalidJSON(t *testing.T) {
	_, err := OpenAI2RespToOpenAI([]byte("not valid json"), "gpt-4")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- OpenAIStreamToOpenAI2 ---

func TestOpenAIStreamToOpenAI2_TextDelta(t *testing.T) {
	ctx := transformer.NewStreamContext()

	openaiSSE := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`

	result, err := OpenAIStreamToOpenAI2([]byte(openaiSSE), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToOpenAI2 failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "response.created") {
		t.Errorf("Expected 'response.created' event, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "response.output_text.delta") {
		t.Errorf("Expected 'response.output_text.delta' event, got '%s'", resultStr)
	}
}

func TestOpenAIStreamToOpenAI2_ToolCall(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageStartSent = true
	ctx.MessageID = "chatcmpl-123"

	openaiSSE := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":""}}]},"finish_reason":null}]}`

	result, err := OpenAIStreamToOpenAI2([]byte(openaiSSE), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToOpenAI2 failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "response.output_item.added") {
		t.Errorf("Expected 'response.output_item.added' event, got '%s'", resultStr)
	}
}

func TestOpenAIStreamToOpenAI2_Done(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageStartSent = true
	ctx.MessageID = "chatcmpl-123"
	ctx.ContentBlockStarted = true

	result, err := OpenAIStreamToOpenAI2([]byte("data: [DONE]"), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToOpenAI2 failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "response.completed") {
		t.Errorf("Expected 'response.completed' event, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "[DONE]") {
		t.Errorf("Expected '[DONE]' in result, got '%s'", resultStr)
	}
}

func TestOpenAIStreamToOpenAI2_Empty(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := OpenAIStreamToOpenAI2([]byte(""), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToOpenAI2 failed: %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil for empty event, got %v", result)
	}
}

// --- OpenAI2StreamToOpenAI ---

func TestOpenAI2StreamToOpenAI_TextDelta(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "resp_123"

	openai2SSE := `data: {"type":"response.output_text.delta","delta":"Hello"}`

	result, err := OpenAI2StreamToOpenAI([]byte(openai2SSE), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2StreamToOpenAI failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "Hello") {
		t.Errorf("Expected 'Hello' in result, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "chat.completion.chunk") {
		t.Errorf("Expected 'chat.completion.chunk' in result, got '%s'", resultStr)
	}
}

func TestOpenAI2StreamToOpenAI_FunctionCall(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "resp_123"

	// First: output_item.added
	openai2SSE1 := `data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_1","name":"read_file"}}`
	_, err := OpenAI2StreamToOpenAI([]byte(openai2SSE1), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2StreamToOpenAI failed: %v", err)
	}

	// Check context updated
	if ctx.CurrentToolID != "call_1" {
		t.Errorf("Expected CurrentToolID 'call_1', got '%v'", ctx.CurrentToolID)
	}

	// Second: arguments delta
	openai2SSE2 := `data: {"type":"response.function_call_arguments.delta","delta":"{\"path\":\"/tmp/a\"}"}`
	_, err = OpenAI2StreamToOpenAI([]byte(openai2SSE2), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2StreamToOpenAI failed: %v", err)
	}

	// Check arguments accumulated
	if ctx.ToolArguments != "{\"path\":\"/tmp/a\"}" {
		t.Errorf("Expected ToolArguments, got '%v'", ctx.ToolArguments)
	}
}

func TestOpenAI2StreamToOpenAI_Completed(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "resp_123"

	openai2SSE := `data: {"type":"response.completed","response":{"id":"resp_123","status":"completed"}}`

	result, err := OpenAI2StreamToOpenAI([]byte(openai2SSE), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2StreamToOpenAI failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "finish_reason") {
		t.Errorf("Expected 'finish_reason' in result, got '%s'", resultStr)
	}
}

func TestOpenAI2StreamToOpenAI_Done(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := OpenAI2StreamToOpenAI([]byte("data: [DONE]"), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2StreamToOpenAI failed: %v", err)
	}

	if string(result) != "data: [DONE]\n\n" {
		t.Errorf("Expected 'data: [DONE]\\n\\n', got '%s'", result)
	}
}

func TestOpenAI2StreamToOpenAI_Empty(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := OpenAI2StreamToOpenAI([]byte(""), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2StreamToOpenAI failed: %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil for empty event, got %v", result)
	}
}
