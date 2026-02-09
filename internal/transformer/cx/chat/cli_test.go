package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestCLITransformer_Name(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")
	if trans.Name() != "cx_chat_cli" {
		t.Errorf("Expected name 'cx_chat_cli', got '%s'", trans.Name())
	}
}

func TestCLITransformer_TransformRequest(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	openaiReq := `{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello"}
		],
		"stream": true
	}`

	result, err := trans.TransformRequest([]byte(openaiReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check model is overridden
	if claudeReq["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("Expected model 'claude-sonnet-4-20250514', got '%v'", claudeReq["model"])
	}

	// Check system prompt structure
	system, ok := claudeReq["system"].([]interface{})
	if !ok {
		t.Fatalf("Expected system to be array, got %T", claudeReq["system"])
	}
	if len(system) < 2 {
		t.Fatalf("Expected at least 2 system blocks, got %d", len(system))
	}

	// Check CLI identity is injected
	firstBlock := system[0].(map[string]interface{})
	if !strings.Contains(firstBlock["text"].(string), "Claude Code") {
		t.Errorf("Expected CLI identity in first system block")
	}

	// Check messages (should exclude system)
	messages, ok := claudeReq["messages"].([]interface{})
	if !ok {
		t.Fatalf("Expected messages to be array, got %T", claudeReq["messages"])
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message (user only), got %d", len(messages))
	}

	// Check stream
	if claudeReq["stream"] != true {
		t.Errorf("Expected stream=true, got %v", claudeReq["stream"])
	}
}

func TestCLITransformer_TransformRequest_WithTools(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	// OpenAI format tools
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

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check tools are converted to Claude format
	tools, ok := claudeReq["tools"].([]interface{})
	if !ok {
		t.Fatalf("Expected tools to be array, got %T", claudeReq["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	tool := tools[0].(map[string]interface{})
	if tool["name"] != "read_file" {
		t.Errorf("Expected tool name 'read_file', got '%v'", tool["name"])
	}
	if tool["input_schema"] == nil {
		t.Errorf("Expected input_schema, got nil")
	}
}

func TestCLITransformer_TransformRequest_ToolMessage(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	// OpenAI tool message
	openaiReq := `{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "Read file"},
			{"role": "assistant", "content": "", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\":\"/tmp/a\"}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": "file content"}
		]
	}`

	result, err := trans.TransformRequest([]byte(openaiReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	messages := claudeReq["messages"].([]interface{})
	if len(messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(messages))
	}

	// Check tool message is converted to user + tool_result
	toolMsg := messages[2].(map[string]interface{})
	if toolMsg["role"] != "user" {
		t.Errorf("Expected role 'user' for tool_result, got '%v'", toolMsg["role"])
	}

	content := toolMsg["content"].([]interface{})
	block := content[0].(map[string]interface{})
	if block["type"] != "tool_result" {
		t.Errorf("Expected type 'tool_result', got '%v'", block["type"])
	}
	if block["tool_use_id"] != "call_1" {
		t.Errorf("Expected tool_use_id 'call_1', got '%v'", block["tool_use_id"])
	}
}

func TestCLITransformer_TransformResponse(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello!"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	result, err := trans.TransformResponse([]byte(claudeResp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check OpenAI format
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

func TestCLITransformer_TransformResponse_Streaming(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")

	// Streaming response should return nil
	result, err := trans.TransformResponse([]byte("data: {}"), true)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for streaming, got %v", result)
	}
}

func TestCLITransformer_TransformResponseWithContext(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")
	ctx := transformer.NewStreamContext()

	// Claude SSE event
	claudeEvent := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}

`

	result, err := trans.TransformResponseWithContext([]byte(claudeEvent), true, ctx)
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

func TestCLITransformer_TransformResponseWithContext_TextDelta(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "msg_1"

	// Claude text delta event
	claudeEvent := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

`

	result, err := trans.TransformResponseWithContext([]byte(claudeEvent), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "Hello") {
		t.Errorf("Expected 'Hello' in result, got '%s'", resultStr)
	}
}

func TestCLITransformer_TransformResponseWithContext_NonStreaming(t *testing.T) {
	trans := NewCLITransformer("claude-sonnet-4-20250514", "test-api-key")
	ctx := transformer.NewStreamContext()

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

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if openaiResp["object"] != "chat.completion" {
		t.Errorf("Expected object 'chat.completion', got '%v'", openaiResp["object"])
	}
}
