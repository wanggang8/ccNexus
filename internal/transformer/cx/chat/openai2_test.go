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
