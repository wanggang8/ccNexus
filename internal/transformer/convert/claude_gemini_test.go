package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// ========== Claude ↔ Gemini 转换测试 ==========

// --- ClaudeReqToGemini ---

func TestClaudeReqToGemini_Basic(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"system": "You are helpful.",
		"messages": [{"role": "user", "content": "Hello"}],
		"max_tokens": 1024
	}`

	result, err := ClaudeReqToGemini([]byte(claudeReq), "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("ClaudeReqToGemini failed: %v", err)
	}

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check systemInstruction
	sysInstr := geminiReq["systemInstruction"].(map[string]interface{})
	parts := sysInstr["parts"].([]interface{})
	if len(parts) != 1 {
		t.Errorf("Expected 1 system part, got %d", len(parts))
	}

	// Check contents
	contents := geminiReq["contents"].([]interface{})
	if len(contents) != 1 {
		t.Errorf("Expected 1 content, got %d", len(contents))
	}

	// Check generationConfig
	genConfig := geminiReq["generationConfig"].(map[string]interface{})
	if genConfig["maxOutputTokens"] != float64(1024) {
		t.Errorf("Expected maxOutputTokens 1024, got %v", genConfig["maxOutputTokens"])
	}
}

func TestClaudeReqToGemini_WithTools(t *testing.T) {
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

	result, err := ClaudeReqToGemini([]byte(claudeReq), "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("ClaudeReqToGemini failed: %v", err)
	}

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check tools
	tools := geminiReq["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool group, got %d", len(tools))
	}

	toolGroup := tools[0].(map[string]interface{})
	funcDecls := toolGroup["functionDeclarations"].([]interface{})
	if len(funcDecls) != 1 {
		t.Fatalf("Expected 1 function declaration, got %d", len(funcDecls))
	}

	// Check toolConfig
	toolConfig := geminiReq["toolConfig"].(map[string]interface{})
	funcCallingConfig := toolConfig["functionCallingConfig"].(map[string]interface{})
	if funcCallingConfig["mode"] != "AUTO" {
		t.Errorf("Expected mode 'AUTO', got '%v'", funcCallingConfig["mode"])
	}
}

func TestClaudeReqToGemini_WithContentArray(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": [{"type": "text", "text": "Hello"}]}],
		"max_tokens": 1024
	}`

	result, err := ClaudeReqToGemini([]byte(claudeReq), "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("ClaudeReqToGemini failed: %v", err)
	}

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	contents := geminiReq["contents"].([]interface{})
	if len(contents) != 1 {
		t.Errorf("Expected 1 content, got %d", len(contents))
	}
}

func TestClaudeReqToGemini_WithToolUseAndResult(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Read file"},
			{"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_1", "name": "read_file", "input": {"path": "/tmp/a"}}]},
			{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_1", "content": "file content"}]}
		],
		"max_tokens": 1024
	}`

	result, err := ClaudeReqToGemini([]byte(claudeReq), "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("ClaudeReqToGemini failed: %v", err)
	}

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	contents := geminiReq["contents"].([]interface{})
	if len(contents) != 3 {
		t.Fatalf("Expected 3 contents, got %d", len(contents))
	}

	// Check assistant message has functionCall
	assistantContent := contents[1].(map[string]interface{})
	parts := assistantContent["parts"].([]interface{})
	part := parts[0].(map[string]interface{})
	if part["functionCall"] == nil {
		t.Error("Expected functionCall in assistant message")
	}

	// Check user message has functionResponse
	userContent := contents[2].(map[string]interface{})
	userParts := userContent["parts"].([]interface{})
	userPart := userParts[0].(map[string]interface{})
	if userPart["functionResponse"] == nil {
		t.Error("Expected functionResponse in user message")
	}
}

func TestClaudeReqToGemini_InvalidJSON(t *testing.T) {
	_, err := ClaudeReqToGemini([]byte("not valid json"), "gemini-2.0-flash")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- GeminiReqToClaude ---

func TestGeminiReqToClaude_Basic(t *testing.T) {
	geminiReq := `{
		"systemInstruction": {"parts": [{"text": "You are helpful."}]},
		"contents": [{"role": "user", "parts": [{"text": "Hello"}]}],
		"generationConfig": {"maxOutputTokens": 1024, "temperature": 0.7}
	}`

	result, err := GeminiReqToClaude([]byte(geminiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("GeminiReqToClaude failed: %v", err)
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

	// Check temperature
	if claudeReq["temperature"] != 0.7 {
		t.Errorf("Expected temperature 0.7, got %v", claudeReq["temperature"])
	}
}

func TestGeminiReqToClaude_WithFunctionCall(t *testing.T) {
	geminiReq := `{
		"contents": [
			{"role": "user", "parts": [{"text": "Read file"}]},
			{"role": "model", "parts": [{"functionCall": {"name": "read_file", "args": {"path": "/tmp/a"}}}]}
		]
	}`

	result, err := GeminiReqToClaude([]byte(geminiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("GeminiReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	messages := claudeReq["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(messages))
	}

	// Check assistant message has tool_use
	assistantMsg := messages[1].(map[string]interface{})
	content := assistantMsg["content"].([]interface{})
	block := content[0].(map[string]interface{})
	if block["type"] != "tool_use" {
		t.Errorf("Expected type 'tool_use', got '%v'", block["type"])
	}
}

func TestGeminiReqToClaude_WithTools(t *testing.T) {
	geminiReq := `{
		"contents": [{"role": "user", "parts": [{"text": "Hello"}]}],
		"tools": [{"functionDeclarations": [{"name": "read_file", "description": "Read a file", "parameters": {"type": "object"}}]}]
	}`

	result, err := GeminiReqToClaude([]byte(geminiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("GeminiReqToClaude failed: %v", err)
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
}

func TestGeminiReqToClaude_WithThought(t *testing.T) {
	geminiReq := `{
		"contents": [
			{"role": "user", "parts": [{"text": "Hello"}]},
			{"role": "model", "parts": [{"text": "Let me think...", "thought": true}, {"text": "Hi!"}]}
		]
	}`

	result, err := GeminiReqToClaude([]byte(geminiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("GeminiReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(result, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	messages := claudeReq["messages"].([]interface{})
	assistantMsg := messages[1].(map[string]interface{})
	content := assistantMsg["content"].([]interface{})

	// Should have thinking block and text block
	if len(content) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(content))
	}

	thinkingBlock := content[0].(map[string]interface{})
	if thinkingBlock["type"] != "thinking" {
		t.Errorf("Expected type 'thinking', got '%v'", thinkingBlock["type"])
	}
}

func TestGeminiReqToClaude_InvalidJSON(t *testing.T) {
	_, err := GeminiReqToClaude([]byte("not valid json"), "claude-sonnet-4-20250514")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- ClaudeRespToGemini ---

func TestClaudeRespToGemini_Basic(t *testing.T) {
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello!"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	result, err := ClaudeRespToGemini([]byte(claudeResp))
	if err != nil {
		t.Fatalf("ClaudeRespToGemini failed: %v", err)
	}

	var geminiResp map[string]interface{}
	if err := json.Unmarshal(result, &geminiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check candidates
	candidates := geminiResp["candidates"].([]interface{})
	if len(candidates) != 1 {
		t.Fatalf("Expected 1 candidate, got %d", len(candidates))
	}

	candidate := candidates[0].(map[string]interface{})
	if candidate["finishReason"] != "STOP" {
		t.Errorf("Expected finishReason 'STOP', got '%v'", candidate["finishReason"])
	}

	// Check usageMetadata
	usage := geminiResp["usageMetadata"].(map[string]interface{})
	if usage["promptTokenCount"] != float64(10) {
		t.Errorf("Expected promptTokenCount 10, got %v", usage["promptTokenCount"])
	}
}

func TestClaudeRespToGemini_WithToolUse(t *testing.T) {
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "tool_use", "id": "toolu_1", "name": "read_file", "input": {"path": "/tmp/a"}}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 10, "output_tokens": 15}
	}`

	result, err := ClaudeRespToGemini([]byte(claudeResp))
	if err != nil {
		t.Fatalf("ClaudeRespToGemini failed: %v", err)
	}

	var geminiResp map[string]interface{}
	if err := json.Unmarshal(result, &geminiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	candidates := geminiResp["candidates"].([]interface{})
	candidate := candidates[0].(map[string]interface{})

	// Check finishReason
	if candidate["finishReason"] != "STOP" {
		t.Errorf("Expected finishReason 'STOP', got '%v'", candidate["finishReason"])
	}

	// Check functionCall
	content := candidate["content"].(map[string]interface{})
	parts := content["parts"].([]interface{})
	part := parts[0].(map[string]interface{})
	if part["functionCall"] == nil {
		t.Error("Expected functionCall in response")
	}
}

func TestClaudeRespToGemini_WithThinking(t *testing.T) {
	claudeResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "thinking", "thinking": "Let me think...", "signature": "sig123"},
			{"type": "text", "text": "Hello!"}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`

	result, err := ClaudeRespToGemini([]byte(claudeResp))
	if err != nil {
		t.Fatalf("ClaudeRespToGemini failed: %v", err)
	}

	var geminiResp map[string]interface{}
	if err := json.Unmarshal(result, &geminiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	candidates := geminiResp["candidates"].([]interface{})
	candidate := candidates[0].(map[string]interface{})
	content := candidate["content"].(map[string]interface{})
	parts := content["parts"].([]interface{})

	// Should have thought and text parts
	if len(parts) != 2 {
		t.Fatalf("Expected 2 parts, got %d", len(parts))
	}

	thoughtPart := parts[0].(map[string]interface{})
	if thoughtPart["thought"] != true {
		t.Errorf("Expected thought=true, got %v", thoughtPart["thought"])
	}
	if thoughtPart["thoughtSignature"] != "sig123" {
		t.Errorf("Expected thoughtSignature 'sig123', got '%v'", thoughtPart["thoughtSignature"])
	}
}

func TestClaudeRespToGemini_InvalidJSON(t *testing.T) {
	_, err := ClaudeRespToGemini([]byte("not valid json"))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- GeminiRespToClaude ---

func TestGeminiRespToClaude_Basic(t *testing.T) {
	geminiResp := `{
		"candidates": [{"content": {"role": "model", "parts": [{"text": "Hello!"}]}, "finishReason": "STOP"}],
		"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 5, "totalTokenCount": 15}
	}`

	result, err := GeminiRespToClaude([]byte(geminiResp))
	if err != nil {
		t.Fatalf("GeminiRespToClaude failed: %v", err)
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

	// Check content
	content := claudeResp["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(content))
	}
}

func TestGeminiRespToClaude_WithFunctionCall(t *testing.T) {
	geminiResp := `{
		"candidates": [{"content": {"role": "model", "parts": [{"functionCall": {"name": "read_file", "args": {"path": "/tmp/a"}}}]}, "finishReason": "STOP"}],
		"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 15, "totalTokenCount": 25}
	}`

	result, err := GeminiRespToClaude([]byte(geminiResp))
	if err != nil {
		t.Fatalf("GeminiRespToClaude failed: %v", err)
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

func TestGeminiRespToClaude_WithThought(t *testing.T) {
	geminiResp := `{
		"candidates": [{"content": {"role": "model", "parts": [{"text": "Let me think...", "thought": true, "thoughtSignature": "sig123"}, {"text": "Hello!"}]}, "finishReason": "STOP"}],
		"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 5, "totalTokenCount": 15}
	}`

	result, err := GeminiRespToClaude([]byte(geminiResp))
	if err != nil {
		t.Fatalf("GeminiRespToClaude failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(result, &claudeResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	content := claudeResp["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(content))
	}

	thinkingBlock := content[0].(map[string]interface{})
	if thinkingBlock["type"] != "thinking" {
		t.Errorf("Expected type 'thinking', got '%v'", thinkingBlock["type"])
	}
	if thinkingBlock["signature"] != "sig123" {
		t.Errorf("Expected signature 'sig123', got '%v'", thinkingBlock["signature"])
	}
}

func TestGeminiRespToClaude_EmptyCandidates(t *testing.T) {
	geminiResp := `{
		"candidates": [],
		"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 0, "totalTokenCount": 10}
	}`

	result, err := GeminiRespToClaude([]byte(geminiResp))
	if err != nil {
		t.Fatalf("GeminiRespToClaude failed: %v", err)
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

func TestGeminiRespToClaude_InvalidJSON(t *testing.T) {
	_, err := GeminiRespToClaude([]byte("not valid json"))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- ClaudeStreamToGemini ---

func TestClaudeStreamToGemini_TextDelta(t *testing.T) {
	ctx := transformer.NewStreamContext()

	claudeEvent := `event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

`

	result, err := ClaudeStreamToGemini([]byte(claudeEvent), ctx)
	if err != nil {
		t.Fatalf("ClaudeStreamToGemini failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "data:") {
		t.Errorf("Expected SSE data, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "Hello") {
		t.Errorf("Expected 'Hello' in result, got '%s'", resultStr)
	}
}

func TestClaudeStreamToGemini_MessageStop(t *testing.T) {
	ctx := transformer.NewStreamContext()

	claudeEvent := `event: message_stop
data: {"type":"message_stop"}

`

	result, err := ClaudeStreamToGemini([]byte(claudeEvent), ctx)
	if err != nil {
		t.Fatalf("ClaudeStreamToGemini failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "[DONE]") {
		t.Errorf("Expected '[DONE]' in result, got '%s'", resultStr)
	}
}

func TestClaudeStreamToGemini_Empty(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := ClaudeStreamToGemini([]byte(""), ctx)
	if err != nil {
		t.Fatalf("ClaudeStreamToGemini failed: %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil for empty event, got %v", result)
	}
}

// --- GeminiStreamToClaude ---

func TestGeminiStreamToClaude_TextChunk(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-sonnet-4-20250514"

	geminiSSE := `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]}}]}`

	result, err := GeminiStreamToClaude([]byte(geminiSSE), ctx)
	if err != nil {
		t.Fatalf("GeminiStreamToClaude failed: %v", err)
	}

	resultStr := string(result)
	// Should have message_start and content events
	if !strings.Contains(resultStr, "message_start") {
		t.Errorf("Expected 'message_start' event, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "content_block") {
		t.Errorf("Expected content_block events, got '%s'", resultStr)
	}
}

func TestGeminiStreamToClaude_WithFunctionCall(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-sonnet-4-20250514"
	ctx.MessageStartSent = true

	geminiSSE := `data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read_file","args":{"path":"/tmp/a"}}}]},"finishReason":"STOP"}]}`

	result, err := GeminiStreamToClaude([]byte(geminiSSE), ctx)
	if err != nil {
		t.Fatalf("GeminiStreamToClaude failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "tool_use") {
		t.Errorf("Expected 'tool_use' in result, got '%s'", resultStr)
	}
}

func TestGeminiStreamToClaude_Done(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageStartSent = true

	result, err := GeminiStreamToClaude([]byte("data: [DONE]"), ctx)
	if err != nil {
		t.Fatalf("GeminiStreamToClaude failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "message_stop") {
		t.Errorf("Expected 'message_stop' event, got '%s'", resultStr)
	}
}

func TestGeminiStreamToClaude_Empty(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := GeminiStreamToClaude([]byte(""), ctx)
	if err != nil {
		t.Fatalf("GeminiStreamToClaude failed: %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil for empty event, got %v", result)
	}
}
