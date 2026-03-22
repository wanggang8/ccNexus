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

func TestOpenAIReqToOpenAI2_PreservesReasoningEffortAndImages(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4.1",
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "Describe this image"},
				{"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB"}}
			]
		}],
		"reasoning_effort": "medium"
	}`

	result, err := OpenAIReqToOpenAI2([]byte(openaiReq), "gpt-4o")
	if err != nil {
		t.Fatalf("OpenAIReqToOpenAI2 failed: %v", err)
	}

	var openai2Req map[string]interface{}
	if err := json.Unmarshal(result, &openai2Req); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if openai2Req["reasoning_effort"] != "medium" {
		t.Fatalf("expected reasoning_effort to be preserved, got %#v", openai2Req["reasoning_effort"])
	}

	input := openai2Req["input"].([]interface{})
	if len(input) != 1 {
		t.Fatalf("expected 1 input item, got %d", len(input))
	}
	msg := input[0].(map[string]interface{})
	content := msg["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(content))
	}
	if content[0].(map[string]interface{})["type"] != "input_text" {
		t.Fatalf("expected first part to be input_text, got %#v", content[0])
	}
	imagePart := content[1].(map[string]interface{})
	if imagePart["type"] != "input_image" {
		t.Fatalf("expected second part to be input_image, got %#v", imagePart)
	}
	if imagePart["image_url"] != "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB" {
		t.Fatalf("expected image url to be preserved, got %#v", imagePart["image_url"])
	}
}

func TestOpenAIReqToOpenAI2DefaultsToolChoiceAutoWhenToolsPresent(t *testing.T) {
	openaiReq := `{
		"model":"gpt-4.1",
		"stream":true,
		"messages":[{"role":"user","content":"test"}],
		"tools":[{"type":"function","function":{"name":"Write","description":"Write file","parameters":{"type":"object"}}}]
	}`

	reqBytes, err := OpenAIReqToOpenAI2([]byte(openaiReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAIReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	if req["tool_choice"] != nil {
		t.Fatalf("expected tool_choice to stay absent when not provided, got %#v", req["tool_choice"])
	}

	if _, ok := req["store"]; ok {
		t.Fatalf("did not expect store in generic openai2 conversion, got %#v", req["store"])
	}
	if _, ok := req["instructions"]; ok {
		t.Fatalf("did not expect instructions without system prompt, got %#v", req["instructions"])
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
				"parameters": {"type": "object", "properties": {"path": {"type": "string"}}},
				"strict": true
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
	if tool["strict"] != true {
		t.Fatalf("expected strict=true to be preserved, got %#v", tool["strict"])
	}
}

func TestOpenAIReqToOpenAI2_InvalidJSON(t *testing.T) {
	_, err := OpenAIReqToOpenAI2([]byte("not valid json"), "gpt-4o")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestOpenAIReqToOpenAI2_DropsUnsupportedParametersExplicitly(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4.1",
		"messages": [{"role": "user", "content": "Hello"}],
		"stream": false,
		"functions": [{"name": "legacy_func"}],
		"function_call": {"name": "legacy_func"},
		"logprobs": true,
		"thinking": {"type": "enabled", "budget_tokens": 2048},
		"enable_thinking": true,
		"budget_tokens": 2048
	}`

	result, err := OpenAIReqToOpenAI2([]byte(openaiReq), "gpt-4o")
	if err != nil {
		t.Fatalf("OpenAIReqToOpenAI2 failed: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if got["model"] != "gpt-4o" {
		t.Fatalf("expected model override gpt-4o, got %#v", got["model"])
	}
	if _, ok := got["functions"]; ok {
		t.Fatalf("expected legacy functions to be dropped, got %#v", got["functions"])
	}
	if _, ok := got["function_call"]; ok {
		t.Fatalf("expected legacy function_call to be dropped, got %#v", got["function_call"])
	}
	if _, ok := got["logprobs"]; ok {
		t.Fatalf("expected logprobs to be dropped, got %#v", got["logprobs"])
	}
	if _, ok := got["thinking"]; ok {
		t.Fatalf("expected thinking to be dropped, got %#v", got["thinking"])
	}
	if _, ok := got["enable_thinking"]; ok {
		t.Fatalf("expected enable_thinking to be dropped, got %#v", got["enable_thinking"])
	}
	if _, ok := got["budget_tokens"]; ok {
		t.Fatalf("expected budget_tokens to be dropped, got %#v", got["budget_tokens"])
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

func TestOpenAI2ReqToOpenAI_PreservesImagesAndReasoningEffort(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"input": [
			{
				"type": "message",
				"role": "user",
				"content": [
					{"type": "input_text", "text": "Look"},
					{"type": "input_image", "image_url": "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQAAAQABAAD/"}
				]
			}
		],
		"reasoning_effort": "high"
	}`

	result, err := OpenAI2ReqToOpenAI([]byte(openai2Req), "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}

	var openaiReq transformer.OpenAIRequest
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if openaiReq.ReasoningEffort != "high" {
		t.Fatalf("expected reasoning_effort to be preserved, got %#v", openaiReq.ReasoningEffort)
	}
	if len(openaiReq.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(openaiReq.Messages))
	}
	content, ok := openaiReq.Messages[0].Content.([]interface{})
	if !ok {
		t.Fatalf("expected array content with image, got %T", openaiReq.Messages[0].Content)
	}
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(content))
	}
	imagePart := content[1].(map[string]interface{})
	if imagePart["type"] != "image_url" {
		t.Fatalf("expected second part to be image_url, got %#v", imagePart["type"])
	}
	urlObj := imagePart["image_url"].(map[string]interface{})
	if urlObj["url"] != "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQAAAQABAAD/" {
		t.Fatalf("expected image url to be preserved, got %#v", urlObj["url"])
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
	if toolMsg.Content != "file content" {
		t.Fatalf("expected tool content preserved, got %#v", toolMsg.Content)
	}
}

func TestOpenAI2ReqToOpenAI_FunctionCallOutputArrayPreservesText(t *testing.T) {
	openai2Req := `{
		"model": "gpt-5.4",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Questionnaire"}]},
			{"type": "function_call", "call_id": "call_1", "name": "AskQuestion", "arguments": "{\"title\":\"t\"}"},
			{"type": "function_call_output", "call_id": "call_1", "output": [{"type": "input_text", "text": "User questions responses:\nQuestion a: x"}]}
		]
	}`

	result, err := OpenAI2ReqToOpenAI([]byte(openai2Req), "gpt-4o")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}

	var openaiReq transformer.OpenAIRequest
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if len(openaiReq.Messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(openaiReq.Messages))
	}
	toolMsg := openaiReq.Messages[2]
	if toolMsg.Role != "tool" {
		t.Fatalf("expected tool role, got %#v", toolMsg.Role)
	}
	content, ok := toolMsg.Content.(string)
	if !ok {
		t.Fatalf("expected tool content string after normalization, got %T (%#v)", toolMsg.Content, toolMsg.Content)
	}
	if !strings.Contains(content, "User questions responses:") {
		t.Fatalf("expected normalized tool output text preserved, got %#v", content)
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

func TestOpenAI2ReqToOpenAI_CustomToolArgumentsWrappedForChat(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Patch file"}]},
			{"type": "function_call", "call_id": "call_patch", "name": "ApplyPatch", "arguments": "*** Begin Patch\n*** Update File: /tmp/a.txt\n@@\n-old\n+new\n*** End Patch"}
		],
		"tools": [
			{"type": "custom", "name": "ApplyPatch", "description": "Apply patch"}
		]
	}`

	result, err := OpenAI2ReqToOpenAI([]byte(openai2Req), "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}

	var openaiReq map[string]interface{}
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	messages := openaiReq["messages"].([]interface{})
	assistant := messages[1].(map[string]interface{})
	toolCalls := assistant["tool_calls"].([]interface{})
	toolCall := toolCalls[0].(map[string]interface{})
	function := toolCall["function"].(map[string]interface{})
	arguments := function["arguments"].(string)

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &decoded); err != nil {
		t.Fatalf("expected wrapped custom arguments json, got %q: %v", arguments, err)
	}
	if decoded["input"] == nil {
		t.Fatalf("expected custom tool arguments wrapped into input field, got %#v", decoded)
	}
}

func TestOpenAI2ReqToOpenAI_PreservesToolStrictFlag(t *testing.T) {
	openai2Req := `{
		"model": "gpt-5.4",
		"input": "Hello",
		"tools": [
			{"type": "function", "name": "read_file", "description": "Read file", "parameters": {"type": "object"}, "strict": true}
		]
	}`

	result, err := OpenAI2ReqToOpenAI([]byte(openai2Req), "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}

	var openaiReq map[string]interface{}
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	tools, ok := openaiReq["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %#v", openaiReq["tools"])
	}
	tool := tools[0].(map[string]interface{})
	fn, ok := tool["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected function wrapper, got %#v", tool)
	}
	if fn["strict"] != true {
		t.Fatalf("expected strict=true to be preserved in function wrapper, got %#v", fn["strict"])
	}
}

func TestOpenAI2ReqToOpenAI_CursorStyleInputWithoutTypedMessage(t *testing.T) {
	openai2Req := `{
		"model": "gpt-5.4",
		"input": [
			{"role": "system", "content": "You are GPT-5.4."},
			{"role": "user", "content": "Hello"}
		],
		"tools": [
			{"type": "function", "name": "ReadFile", "description": "Read file", "parameters": {"type": "object"}},
			{"type": "custom", "name": "ApplyPatch", "description": "Apply patch"}
		]
	}`

	result, err := OpenAI2ReqToOpenAI([]byte(openai2Req), "gpt-4o")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}

	var openaiReq map[string]interface{}
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	messages, ok := openaiReq["messages"].([]interface{})
	if !ok || len(messages) != 2 {
		t.Fatalf("expected cursor-style input to convert into 2 chat messages, got %#v", openaiReq["messages"])
	}

	first := messages[0].(map[string]interface{})
	if first["role"] != "system" || first["content"] != "You are GPT-5.4." {
		t.Fatalf("unexpected first message: %#v", first)
	}

	tools, ok := openaiReq["tools"].([]interface{})
	if !ok || len(tools) != 2 {
		t.Fatalf("expected tools converted for chat target, got %#v", openaiReq["tools"])
	}
	for i, rawTool := range tools {
		tool := rawTool.(map[string]interface{})
		if tool["type"] != "function" {
			t.Fatalf("expected tool %d type=function, got %#v", i, tool)
		}
		if _, ok := tool["function"].(map[string]interface{}); !ok {
			t.Fatalf("expected tool %d to use chat function wrapper, got %#v", i, tool)
		}
	}

	if _, ok := openaiReq["input"]; ok {
		t.Fatalf("expected chat target to drop responses input, got %#v", openaiReq["input"])
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

func TestOpenAIRespToOpenAI2_CustomToolArgumentsUnwrappedForResponses(t *testing.T) {
	openaiResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [{"id": "call_patch", "type": "function", "function": {"name": "ApplyPatch", "arguments": "{\"input\":\"*** Begin Patch\\n*** Update File: /tmp/a.txt\\n@@\\n-old\\n+new\\n*** End Patch\"}"}}]
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
	funcCall := output[0].(map[string]interface{})
	arguments := funcCall["arguments"].(string)
	if !strings.HasPrefix(arguments, "*** Begin Patch") {
		t.Fatalf("expected wrapped custom arguments to be unwrapped for responses, got %#v", arguments)
	}
}

func TestOpenAIReqToOpenAI2_ToolMessageArrayContentPreserved(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "Questionnaire"},
			{"role": "assistant", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "AskQuestion", "arguments": "{\"title\":\"t\"}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": [{"type": "input_text", "text": "User questions responses:\nQuestion a: x"}]}
		]
	}`

	result, err := OpenAIReqToOpenAI2([]byte(openaiReq), "gpt-5.4")
	if err != nil {
		t.Fatalf("OpenAIReqToOpenAI2 failed: %v", err)
	}

	var openai2Req map[string]interface{}
	if err := json.Unmarshal(result, &openai2Req); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	input := openai2Req["input"].([]interface{})
	if len(input) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(input))
	}
	functionOutput := input[2].(map[string]interface{})
	if functionOutput["type"] != "function_call_output" {
		t.Fatalf("expected function_call_output item, got %#v", functionOutput)
	}
	output, ok := functionOutput["output"].([]interface{})
	if !ok || len(output) != 1 {
		t.Fatalf("expected array output preserved, got %#v", functionOutput["output"])
	}
	part := output[0].(map[string]interface{})
	if part["type"] != "input_text" {
		t.Fatalf("expected input_text part preserved, got %#v", part)
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

func TestOpenAI2RespToOpenAIPreservesTotalTokens(t *testing.T) {
	openai2Resp := `{
		"id":"resp_123",
		"object":"response",
		"status":"completed",
		"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],
		"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":99}
	}`

	respBytes, err := OpenAI2RespToOpenAI([]byte(openai2Resp), "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAI2RespToOpenAI failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal transformed response failed: %v", err)
	}

	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected usage object, got %#v", resp["usage"])
	}

	if usage["total_tokens"] != float64(99) {
		t.Fatalf("expected total_tokens=99, got %#v", usage["total_tokens"])
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

func TestOpenAI2StreamToOpenAIIncludesUsageOnCompleted(t *testing.T) {
	ctx := transformer.NewStreamContext()

	created := `data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`
	if out, err := OpenAI2StreamToOpenAI([]byte(created), ctx, "gpt-4.1"); err != nil {
		t.Fatalf("response.created failed: %v", err)
	} else if out != nil {
		t.Fatalf("expected nil output for response.created, got %s", string(out))
	}

	completed := `data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","usage":{"input_tokens":7,"output_tokens":3,"total_tokens":42}}}`
	out, err := OpenAI2StreamToOpenAI([]byte(completed), ctx, "gpt-4.1")
	if err != nil {
		t.Fatalf("response.completed failed: %v", err)
	}
	if out == nil {
		t.Fatal("expected transformed chunk, got nil")
	}

	_, jsonData := ParseSSE(out)
	var chunk map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
		t.Fatalf("unmarshal chunk failed: %v, raw=%s", err, jsonData)
	}

	usage, ok := chunk["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected usage in final chunk, got %#v", chunk["usage"])
	}
	if usage["prompt_tokens"] != float64(7) {
		t.Fatalf("expected prompt_tokens=7, got %#v", usage["prompt_tokens"])
	}
	if usage["completion_tokens"] != float64(3) {
		t.Fatalf("expected completion_tokens=3, got %#v", usage["completion_tokens"])
	}
	if usage["total_tokens"] != float64(42) {
		t.Fatalf("expected total_tokens=42, got %#v", usage["total_tokens"])
	}
}
