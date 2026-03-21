package chat

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestClaudeTransformer_Name(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")
	if trans.Name() != "cx_chat_claude" {
		t.Errorf("Expected name 'cx_chat_claude', got '%s'", trans.Name())
	}
}

func TestClaudeTransformer_TransformRequest(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

	openaiReq := `{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello"}
		],
		"max_tokens": 1024,
		"stream": false
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

	// Check system prompt is extracted
	if claudeReq["system"] == nil {
		t.Errorf("Expected system prompt, got nil")
	}

	// Check messages (should exclude system)
	messages, ok := claudeReq["messages"].([]interface{})
	if !ok {
		t.Fatalf("Expected messages to be array, got %T", claudeReq["messages"])
	}
	if len(messages) != 1 {
		t.Errorf("Expected 1 message (user only), got %d", len(messages))
	}

	// Check max_tokens
	if claudeReq["max_tokens"] != float64(1024) {
		t.Errorf("Expected max_tokens 1024, got %v", claudeReq["max_tokens"])
	}
}

func TestClaudeTransformer_TransformRequest_WithTools(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

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

func TestClaudeTransformer_TransformRequest_ToolMessage(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

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
}

func TestClaudeTransformer_TransformRequest_PreservesAssistantTextBlocksWithToolCalls(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

	openaiReq := `{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "Read file"},
			{"role": "assistant", "content": [{"type": "text", "text": "我先检查一下。"}], "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\":\"/tmp/a\"}"}}]}
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
	assistantMsg := messages[1].(map[string]interface{})
	content := assistantMsg["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(content))
	}

	textBlock := content[0].(map[string]interface{})
	if textBlock["type"] != "text" || textBlock["text"] != "我先检查一下。" {
		t.Fatalf("Expected preserved assistant text block, got %#v", textBlock)
	}

	toolUse := content[1].(map[string]interface{})
	if toolUse["type"] != "tool_use" {
		t.Fatalf("Expected tool_use block, got %#v", toolUse)
	}
}

func TestClaudeTransformer_TransformRequest_PreservesStructuredToolResultContent(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

	openaiReq := `{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "回答问题"},
			{"role": "assistant", "content": "", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "AskQuestion", "arguments": "{}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": [{"type": "text", "text": "User questions responses:\nQuestion gitlab_config_status: Selected option(s) have_token\nQuestion gitlab_instance: Selected option(s) gobies"}]}
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
	toolMsg := messages[2].(map[string]interface{})
	content := toolMsg["content"].([]interface{})
	toolResult := content[0].(map[string]interface{})
	resultContent := toolResult["content"].([]interface{})
	textBlock := resultContent[0].(map[string]interface{})

	if textBlock["type"] != "text" {
		t.Fatalf("Expected text block in tool result, got %#v", textBlock)
	}
	text := textBlock["text"].(string)
	if !strings.Contains(text, "have_token") || !strings.Contains(text, "gobies") {
		t.Fatalf("Expected AskQuestion answers to be preserved, got %q", text)
	}
}

func TestClaudeTransformer_TransformResponse(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

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
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})

	if message["role"] != "assistant" {
		t.Errorf("Expected role 'assistant', got '%v'", message["role"])
	}
	if message["content"] != "Hello!" {
		t.Errorf("Expected content 'Hello!', got '%v'", message["content"])
	}
}

func TestClaudeTransformer_TransformRequest_ClaudeShapePassthroughNormalize(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

	claudeReq := `{
		"model": "claude-3-5-sonnet",
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

	if data["model"] != "claude-sonnet-4-20250514" {
		t.Fatalf("expected model override, got %#v", data["model"])
	}
	if _, ok := data["system"]; !ok {
		t.Fatalf("expected Claude-shaped body to preserve system, got %#v", data)
	}
	if _, ok := data["metadata"].(map[string]interface{}); !ok {
		t.Fatalf("expected metadata preserved, got %#v", data["metadata"])
	}
	tools := data["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})
	if tool["input_schema"] == nil {
		t.Fatalf("expected Claude tool schema preserved, got %#v", tool)
	}
}

func TestClaudeTransformer_TransformRequest_OpenAIResponsesShapeUsesResponsesConverter(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

	responsesReq := `{
		"model": "gpt-5.4",
		"instructions": "You are helpful.",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Hello"}]}
		],
		"reasoning": {"effort": "medium", "summary": "auto"},
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

	if data["model"] != "claude-sonnet-4-20250514" {
		t.Fatalf("expected model override, got %#v", data["model"])
	}
	if data["system"] != "You are helpful." {
		t.Fatalf("expected instructions to map to Claude system, got %#v", data["system"])
	}
	if _, ok := data["thinking"]; !ok {
		t.Fatalf("expected reasoning to map into Claude thinking config")
	}
	messages := data["messages"].([]interface{})
	if len(messages) != 1 {
		t.Fatalf("expected one converted Claude message, got %d", len(messages))
	}
}

func TestClaudeTransformer_TransformRequest_RealCursorClaudeLog(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

	payload, err := os.ReadFile("/Users/vick/Desktop/project/ccNexus/docs/cursor-claude-request.log")
	if err != nil {
		t.Fatalf("read cursor claude log failed: %v", err)
	}

	result, err := trans.TransformRequest(payload)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("unmarshal transformed payload failed: %v", err)
	}

	if data["model"] != "claude-sonnet-4-20250514" {
		t.Fatalf("expected model override, got %#v", data["model"])
	}
	if data["messages"] == nil {
		t.Fatalf("expected Claude messages preserved")
	}
	if data["tools"] == nil {
		t.Fatalf("expected tools preserved")
	}
}

func TestClaudeTransformer_TransformRequest_RealChatGPT54Log(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

	payload, err := os.ReadFile("/Users/vick/Desktop/project/ccNexus/docs/chatgpt5.4-request.log")
	if err != nil {
		t.Fatalf("read chatgpt5.4 log failed: %v", err)
	}

	result, err := trans.TransformRequest(payload)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("unmarshal transformed payload failed: %v", err)
	}

	if data["system"] == nil {
		t.Fatalf("expected system mapped from responses-like input")
	}
	if data["messages"] == nil {
		t.Fatalf("expected messages converted for Claude target")
	}
}

func TestClaudeTransformer_TransformResponse_WithToolUse(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

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

	result, err := trans.TransformResponse([]byte(claudeResp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
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

func TestClaudeTransformer_TransformResponse_Streaming(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")

	result, err := trans.TransformResponse([]byte("data: {}"), true)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for streaming, got %v", result)
	}
}

func TestClaudeTransformer_TransformResponseWithContext(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")
	ctx := transformer.NewStreamContext()

	claudeEvent := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}

`

	result, err := trans.TransformResponseWithContext([]byte(claudeEvent), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "data:") {
		t.Errorf("Expected OpenAI SSE format with 'data:', got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "chat.completion.chunk") {
		t.Errorf("Expected 'chat.completion.chunk' in result, got '%s'", resultStr)
	}
}

func TestClaudeTransformer_TransformResponseWithContext_TextDelta(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "msg_1"

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

func TestClaudeTransformer_TransformResponseWithContext_IncludeUsage(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "msg_1"
	ctx.IncludeUsage = true
	ctx.InputTokens = 10

	claudeEvent := `event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":5}}

`

	result, err := trans.TransformResponseWithContext([]byte(claudeEvent), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "\"finish_reason\":\"stop\"") {
		t.Fatalf("Expected finish_reason stop in result, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "\"usage\":") ||
		!strings.Contains(resultStr, "\"prompt_tokens\":10") ||
		!strings.Contains(resultStr, "\"completion_tokens\":5") ||
		!strings.Contains(resultStr, "\"total_tokens\":15") {
		t.Fatalf("Expected usage chunk in result, got '%s'", resultStr)
	}
}

func TestClaudeTransformer_TransformResponseWithContext_SuppressesUsageWhenDisabled(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "msg_1"
	ctx.InputTokens = 10

	claudeEvent := `event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":5}}

`

	result, err := trans.TransformResponseWithContext([]byte(claudeEvent), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "\"finish_reason\":\"stop\"") {
		t.Fatalf("Expected finish_reason stop in result, got '%s'", resultStr)
	}
	if strings.Contains(resultStr, "\"usage\":") {
		t.Fatalf("Expected no usage chunk when IncludeUsage is disabled, got '%s'", resultStr)
	}
}

func TestClaudeTransformer_TransformResponseWithContext_NonStreaming(t *testing.T) {
	trans := NewClaudeTransformer("claude-sonnet-4-20250514")
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
