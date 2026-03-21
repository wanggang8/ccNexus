package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestOpenAI2Transformer_Name(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")
	if trans.Name() != "cx_chat_openai2" {
		t.Errorf("Expected name 'cx_chat_openai2', got '%s'", trans.Name())
	}
}

func TestOpenAI2Transformer_TransformRequest(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	openaiReq := `{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello"}
		],
		"max_tokens": 1024
	}`

	result, err := trans.TransformRequest([]byte(openaiReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var openai2Req map[string]interface{}
	if err := json.Unmarshal(result, &openai2Req); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check model is overridden
	if openai2Req["model"] != "gpt-4o" {
		t.Errorf("Expected model 'gpt-4o', got '%v'", openai2Req["model"])
	}

	// Check OpenAI2 (Responses API) format - uses "input" instead of "messages"
	if openai2Req["input"] == nil {
		t.Errorf("Expected input field for Responses API, got nil")
	}

	// Check instructions (system prompt)
	if openai2Req["instructions"] == nil {
		t.Errorf("Expected instructions field, got nil")
	}
}

func TestOpenAI2Transformer_TransformRequest_WithTools(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

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

	result, err := trans.TransformRequest([]byte(openaiReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var openai2Req map[string]interface{}
	if err := json.Unmarshal(result, &openai2Req); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check tools are present
	tools, ok := openai2Req["tools"].([]interface{})
	if !ok {
		t.Fatalf("Expected tools to be array, got %T", openai2Req["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}
}

func TestOpenAI2Transformer_TransformResponse(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	// OpenAI Responses API format
	openai2Resp := `{
		"id": "resp_123",
		"object": "response",
		"created_at": 1677652288,
		"model": "gpt-4o",
		"output": [
			{
				"type": "message",
				"id": "msg_1",
				"role": "assistant",
				"content": [{"type": "output_text", "text": "Hello!"}]
			}
		],
		"usage": {
			"input_tokens": 10,
			"output_tokens": 5,
			"total_tokens": 15
		}
	}`

	result, err := trans.TransformResponse([]byte(openai2Resp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check converted to OpenAI Chat format
	if openaiResp["object"] != "chat.completion" {
		t.Errorf("Expected object 'chat.completion', got '%v'", openaiResp["object"])
	}

	choices := openaiResp["choices"].([]interface{})
	if len(choices) != 1 {
		t.Fatalf("Expected 1 choice, got %d", len(choices))
	}

	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})

	if message["role"] != "assistant" {
		t.Errorf("Expected role 'assistant', got '%v'", message["role"])
	}
	if message["content"] != "Hello!" {
		t.Errorf("Expected content 'Hello!', got '%v'", message["content"])
	}
}

func TestOpenAI2Transformer_TransformResponse_WithFunctionCall(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	openai2Resp := `{
		"id": "resp_123",
		"object": "response",
		"created_at": 1677652288,
		"model": "gpt-4o",
		"output": [
			{
				"type": "function_call",
				"id": "call_1",
				"call_id": "call_1",
				"name": "read_file",
				"arguments": "{\"path\":\"/tmp/a\"}"
			}
		],
		"usage": {
			"input_tokens": 10,
			"output_tokens": 15,
			"total_tokens": 25
		}
	}`

	result, err := trans.TransformResponse([]byte(openai2Resp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	choices := openaiResp["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})

	toolCalls, ok := message["tool_calls"].([]interface{})
	if !ok {
		t.Fatalf("Expected tool_calls to be array, got %T", message["tool_calls"])
	}
	if len(toolCalls) != 1 {
		t.Fatalf("Expected 1 tool_call, got %d", len(toolCalls))
	}

	tc := toolCalls[0].(map[string]interface{})
	funcObj := tc["function"].(map[string]interface{})
	if funcObj["name"] != "read_file" {
		t.Errorf("Expected function name 'read_file', got '%v'", funcObj["name"])
	}
}

func TestOpenAI2Transformer_TransformResponse_Streaming(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	result, err := trans.TransformResponse([]byte("data: {}"), true)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for streaming, got %v", result)
	}
}

func TestOpenAI2Transformer_TransformResponseWithContext(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")
	ctx := transformer.NewStreamContext()

	// OpenAI2 SSE format
	openai2Event := `data: {"type":"response.output_text.delta","delta":"Hello"}

`

	result, err := trans.TransformResponseWithContext([]byte(openai2Event), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	// Should convert to OpenAI Chat SSE format
	if result != nil && !strings.Contains(resultStr, "data:") {
		t.Errorf("Expected OpenAI SSE format with 'data:', got '%s'", resultStr)
	}
}

func TestOpenAI2Transformer_TransformResponseWithContext_NonStreaming(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")
	ctx := transformer.NewStreamContext()

	openai2Resp := `{
		"id": "resp_123",
		"object": "response",
		"created_at": 1677652288,
		"model": "gpt-4o",
		"output": [
			{
				"type": "message",
				"id": "msg_1",
				"role": "assistant",
				"content": [{"type": "output_text", "text": "Hello!"}]
			}
		],
		"usage": {
			"input_tokens": 10,
			"output_tokens": 5,
			"total_tokens": 15
		}
	}`

	result, err := trans.TransformResponseWithContext([]byte(openai2Resp), false, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if openaiResp["object"] != "chat.completion" {
		t.Errorf("Expected object 'chat.completion', got '%v'", openaiResp["object"])
	}
}

func TestOpenAI2Transformer_TransformRequest_ClaudeShapeUsesClaudeConverter(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	claudeReq := `{
		"model": "claude-4-sonnet",
		"system": [{"type": "text", "text": "You are helpful."}],
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Hello"}]}
		],
		"tools": [{"name": "read_file", "description": "Read a file", "input_schema": {"type": "object"}}],
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
	if data["instructions"] == nil {
		t.Fatalf("expected Claude system to map to instructions")
	}
	if _, ok := data["metadata"].(map[string]interface{}); !ok {
		t.Fatalf("expected metadata preserved, got %#v", data["metadata"])
	}
	if _, ok := data["input"].([]interface{}); !ok {
		t.Fatalf("expected Claude-shaped body to become responses input, got %#v", data["input"])
	}
}

func TestOpenAI2Transformer_TransformRequest_OpenAIResponsesShapePassthroughNormalize(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	responsesReq := `{
		"model": "gpt-5.4",
		"instructions": "You are helpful.",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Hello"}]}
		],
		"reasoning": {"effort": "medium", "summary": "auto"},
		"include": ["reasoning.encrypted_content"],
		"user": "user-123",
		"stream": true
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
	if data["input"] == nil {
		t.Fatalf("expected responses input preserved")
	}
	if data["reasoning"] == nil {
		t.Fatalf("expected responses reasoning preserved")
	}
	if data["user"] != "user-123" {
		t.Fatalf("expected user preserved, got %#v", data["user"])
	}
}

func TestOpenAI2Transformer_TransformRequest_RealCursorClaudeLog(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	result, err := trans.TransformRequest(readLogJSONLine(t, "/Users/vick/Desktop/project/ccNexus/docs/claude-cursor.log", 2))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("unmarshal transformed payload failed: %v", err)
	}

	if data["input"] == nil {
		t.Fatalf("expected Claude-shaped body converted to responses input")
	}
	if data["metadata"] == nil {
		t.Fatalf("expected metadata preserved")
	}
}

func TestOpenAI2Transformer_TransformRequest_RealChatGPT54Log(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	result, err := trans.TransformRequest(readLogJSONLine(t, "/Users/vick/Desktop/project/ccNexus/docs/chatgpt5.4-request.log", 0))
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
	if _, ok := data["input"].([]interface{}); !ok {
		t.Fatalf("expected responses input preserved, got %#v", data["input"])
	}
	if _, ok := data["messages"]; ok {
		t.Fatalf("expected responses output to keep input shape instead of messages, got %#v", data["messages"])
	}
	if _, ok := data["metadata"].(map[string]interface{}); !ok {
		t.Fatalf("expected metadata preserved, got %#v", data["metadata"])
	}
	if _, ok := data["include"].([]interface{}); !ok {
		t.Fatalf("expected include preserved, got %#v", data["include"])
	}
	if _, ok := data["reasoning"].(map[string]interface{}); !ok {
		t.Fatalf("expected reasoning preserved, got %#v", data["reasoning"])
	}
	if data["user"] != "95dfaae8bbc5aaa3" {
		t.Fatalf("expected user preserved, got %#v", data["user"])
	}
	if data["store"] != false {
		t.Fatalf("expected store preserved as false, got %#v", data["store"])
	}
	if _, ok := data["stream_options"]; ok {
		t.Fatalf("expected stream_options dropped for responses target, got %#v", data["stream_options"])
	}
	tools, ok := data["tools"].([]interface{})
	if !ok || len(tools) != 18 {
		t.Fatalf("expected 18 converted tools, got %#v", data["tools"])
	}
	for i, rawTool := range tools {
		tool := rawTool.(map[string]interface{})
		switch tool["type"] {
		case "function":
			if tool["name"] == "" {
				t.Fatalf("expected tool %d name preserved, got %#v", i, tool)
			}
			if _, ok := tool["parameters"].(map[string]interface{}); !ok {
				t.Fatalf("expected tool %d parameters preserved, got %#v", i, tool)
			}
			if strict, ok := tool["strict"].(bool); ok && strict {
				t.Fatalf("expected tool %d strict=false preserved, got %#v", i, tool["strict"])
			}
		case "custom":
			if tool["name"] == "" {
				t.Fatalf("expected custom tool %d name preserved, got %#v", i, tool)
			}
			if _, ok := tool["format"]; !ok {
				t.Fatalf("expected custom tool %d format preserved, got %#v", i, tool)
			}
		default:
			t.Fatalf("unexpected tool %d type %q, got %#v", i, tool["type"], tool)
		}
	}
}
