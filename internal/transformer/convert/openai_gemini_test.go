package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// ========== OpenAI ↔ Gemini 转换测试 ==========

// --- OpenAIReqToGemini ---

func TestOpenAIReqToGemini_Basic(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello"}
		],
		"max_tokens": 1024
	}`

	result, err := OpenAIReqToGemini([]byte(openaiReq), "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("OpenAIReqToGemini failed: %v", err)
	}

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check contents
	contents := geminiReq["contents"].([]interface{})
	if len(contents) != 1 {
		t.Errorf("Expected 1 content (user only), got %d", len(contents))
	}

	// Check systemInstruction
	if geminiReq["systemInstruction"] == nil {
		t.Error("Expected systemInstruction, got nil")
	}

	// Check generationConfig
	genConfig := geminiReq["generationConfig"].(map[string]interface{})
	if genConfig["maxOutputTokens"] != float64(1024) {
		t.Errorf("Expected maxOutputTokens 1024, got %v", genConfig["maxOutputTokens"])
	}
}

func TestOpenAIReqToGemini_WithTools(t *testing.T) {
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

	result, err := OpenAIReqToGemini([]byte(openaiReq), "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("OpenAIReqToGemini failed: %v", err)
	}

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check tools
	tools := geminiReq["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	tool := tools[0].(map[string]interface{})
	funcDecls := tool["functionDeclarations"].([]interface{})
	if len(funcDecls) != 1 {
		t.Fatalf("Expected 1 functionDeclaration, got %d", len(funcDecls))
	}

	// Check toolConfig
	if geminiReq["toolConfig"] == nil {
		t.Error("Expected toolConfig, got nil")
	}
}

func TestOpenAIReqToGemini_WithToolCalls(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "Read file"},
			{"role": "assistant", "content": "", "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\":\"/tmp/a\"}"}}]},
			{"role": "tool", "tool_call_id": "call_1", "content": "file content"}
		]
	}`

	result, err := OpenAIReqToGemini([]byte(openaiReq), "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("OpenAIReqToGemini failed: %v", err)
	}

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	contents := geminiReq["contents"].([]interface{})
	if len(contents) != 3 {
		t.Fatalf("Expected 3 contents, got %d", len(contents))
	}

	// Check tool response (functionResponse)
	toolContent := contents[2].(map[string]interface{})
	parts := toolContent["parts"].([]interface{})
	part := parts[0].(map[string]interface{})
	if part["functionResponse"] == nil {
		t.Error("Expected functionResponse in tool message")
	}
}

func TestOpenAIReqToGemini_WithTemperature(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "Hello"}],
		"temperature": 0.7
	}`

	result, err := OpenAIReqToGemini([]byte(openaiReq), "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("OpenAIReqToGemini failed: %v", err)
	}

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	genConfig := geminiReq["generationConfig"].(map[string]interface{})
	if genConfig["temperature"] != 0.7 {
		t.Errorf("Expected temperature 0.7, got %v", genConfig["temperature"])
	}
}

func TestOpenAIReqToGemini_InvalidJSON(t *testing.T) {
	_, err := OpenAIReqToGemini([]byte("not valid json"), "gemini-2.0-flash")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- GeminiRespToOpenAI ---

func TestGeminiRespToOpenAI_Basic(t *testing.T) {
	geminiResp := `{
		"candidates": [{
			"content": {
				"parts": [{"text": "Hello!"}],
				"role": "model"
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 5,
			"totalTokenCount": 15
		}
	}`

	result, err := GeminiRespToOpenAI([]byte(geminiResp), "gpt-4")
	if err != nil {
		t.Fatalf("GeminiRespToOpenAI failed: %v", err)
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

	// Check usage
	usage := openaiResp["usage"].(map[string]interface{})
	if usage["prompt_tokens"] != float64(10) {
		t.Errorf("Expected prompt_tokens 10, got %v", usage["prompt_tokens"])
	}
}

func TestGeminiRespToOpenAI_WithFunctionCall(t *testing.T) {
	geminiResp := `{
		"candidates": [{
			"content": {
				"parts": [
					{"text": "Let me read that."},
					{"functionCall": {"name": "read_file", "args": {"path": "/tmp/a"}}}
				],
				"role": "model"
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 15,
			"totalTokenCount": 25
		}
	}`

	result, err := GeminiRespToOpenAI([]byte(geminiResp), "gpt-4")
	if err != nil {
		t.Fatalf("GeminiRespToOpenAI failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	choices := openaiResp["choices"].([]interface{})
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
}

func TestGeminiRespToOpenAI_EmptyCandidates(t *testing.T) {
	geminiResp := `{
		"candidates": [],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 0,
			"totalTokenCount": 10
		}
	}`

	result, err := GeminiRespToOpenAI([]byte(geminiResp), "gpt-4")
	if err != nil {
		t.Fatalf("GeminiRespToOpenAI failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(result, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Should still have valid structure
	if openaiResp["object"] != "chat.completion" {
		t.Errorf("Expected object 'chat.completion', got '%v'", openaiResp["object"])
	}
}

func TestGeminiRespToOpenAI_InvalidJSON(t *testing.T) {
	_, err := GeminiRespToOpenAI([]byte("not valid json"), "gpt-4")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// --- GeminiStreamToOpenAI ---

func TestGeminiStreamToOpenAI_TextDelta(t *testing.T) {
	ctx := transformer.NewStreamContext()

	geminiSSE := `data: {"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"}}]}`

	result, err := GeminiStreamToOpenAI([]byte(geminiSSE), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("GeminiStreamToOpenAI failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "Hello") {
		t.Errorf("Expected 'Hello' in result, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "chat.completion.chunk") {
		t.Errorf("Expected 'chat.completion.chunk' in result, got '%s'", resultStr)
	}
}

func TestGeminiStreamToOpenAI_FunctionCall(t *testing.T) {
	ctx := transformer.NewStreamContext()

	geminiSSE := `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"read_file","args":{"path":"/tmp/a"}}}],"role":"model"}}]}`

	result, err := GeminiStreamToOpenAI([]byte(geminiSSE), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("GeminiStreamToOpenAI failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "read_file") {
		t.Errorf("Expected 'read_file' in result, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "tool_calls") {
		t.Errorf("Expected 'tool_calls' in result, got '%s'", resultStr)
	}
}

func TestGeminiStreamToOpenAI_FinishReason(t *testing.T) {
	ctx := transformer.NewStreamContext()

	geminiSSE := `data: {"candidates":[{"content":{"parts":[{"text":"Done"}],"role":"model"},"finishReason":"STOP"}]}`

	result, err := GeminiStreamToOpenAI([]byte(geminiSSE), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("GeminiStreamToOpenAI failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "finish_reason") {
		t.Errorf("Expected 'finish_reason' in result, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "[DONE]") {
		t.Errorf("Expected '[DONE]' in result, got '%s'", resultStr)
	}
}

func TestGeminiStreamToOpenAI_Done(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := GeminiStreamToOpenAI([]byte("data: [DONE]"), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("GeminiStreamToOpenAI failed: %v", err)
	}

	if string(result) != "data: [DONE]\n\n" {
		t.Errorf("Expected 'data: [DONE]\\n\\n', got '%s'", result)
	}
}

func TestGeminiStreamToOpenAI_Empty(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := GeminiStreamToOpenAI([]byte(""), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("GeminiStreamToOpenAI failed: %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil for empty event, got %v", result)
	}
}

func TestGeminiStreamToOpenAI_EmptyCandidates(t *testing.T) {
	ctx := transformer.NewStreamContext()

	geminiSSE := `data: {"candidates":[]}`

	result, err := GeminiStreamToOpenAI([]byte(geminiSSE), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("GeminiStreamToOpenAI failed: %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil for empty candidates, got %v", result)
	}
}

// --- OpenAIStreamToGemini ---

func TestOpenAIStreamToGemini_TextDelta(t *testing.T) {
	ctx := transformer.NewStreamContext()

	openaiSSE := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`

	result, err := OpenAIStreamToGemini([]byte(openaiSSE), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToGemini failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "Hello") {
		t.Errorf("Expected 'Hello' in result, got '%s'", resultStr)
	}
	if !strings.Contains(resultStr, "candidates") {
		t.Errorf("Expected 'candidates' in result, got '%s'", resultStr)
	}
}

func TestOpenAIStreamToGemini_Done(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := OpenAIStreamToGemini([]byte("data: [DONE]"), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToGemini failed: %v", err)
	}

	if string(result) != "data: [DONE]\n\n" {
		t.Errorf("Expected 'data: [DONE]\\n\\n', got '%s'", result)
	}
}

func TestOpenAIStreamToGemini_Empty(t *testing.T) {
	ctx := transformer.NewStreamContext()

	result, err := OpenAIStreamToGemini([]byte(""), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToGemini failed: %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil for empty event, got %v", result)
	}
}

func TestOpenAIStreamToGemini_EmptyChoices(t *testing.T) {
	ctx := transformer.NewStreamContext()

	openaiSSE := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-4","choices":[]}`

	result, err := OpenAIStreamToGemini([]byte(openaiSSE), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToGemini failed: %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil for empty choices, got %v", result)
	}
}
