package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestGeminiTransformer_Name(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")
	if trans.Name() != "cx_chat_gemini" {
		t.Errorf("Expected name 'cx_chat_gemini', got '%s'", trans.Name())
	}
}

func TestGeminiTransformer_TransformRequest(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")

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

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check Gemini format structure
	if geminiReq["contents"] == nil {
		t.Errorf("Expected contents field, got nil")
	}

	// Check systemInstruction (camelCase in Gemini)
	if geminiReq["systemInstruction"] == nil {
		t.Errorf("Expected systemInstruction field, got nil")
	}

	// Check generationConfig (camelCase in Gemini)
	genConfig, ok := geminiReq["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected generationConfig to be map, got %T", geminiReq["generationConfig"])
	}
	if genConfig["maxOutputTokens"] != float64(1024) {
		t.Errorf("Expected maxOutputTokens 1024, got %v", genConfig["maxOutputTokens"])
	}
}

func TestGeminiTransformer_TransformRequest_WithTools(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")

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

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check tools are converted to Gemini format
	tools, ok := geminiReq["tools"].([]interface{})
	if !ok {
		t.Fatalf("Expected tools to be array, got %T", geminiReq["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	tool := tools[0].(map[string]interface{})
	// Gemini uses functionDeclarations (camelCase)
	funcDecls, ok := tool["functionDeclarations"].([]interface{})
	if !ok {
		t.Fatalf("Expected functionDeclarations to be array, got %T", tool["functionDeclarations"])
	}
	if len(funcDecls) != 1 {
		t.Fatalf("Expected 1 function_declaration, got %d", len(funcDecls))
	}

	funcDecl := funcDecls[0].(map[string]interface{})
	if funcDecl["name"] != "read_file" {
		t.Errorf("Expected function name 'read_file', got '%v'", funcDecl["name"])
	}
}

func TestGeminiTransformer_TransformResponse(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")

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

	result, err := trans.TransformResponse([]byte(geminiResp), false)
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

func TestGeminiTransformer_TransformResponse_WithFunctionCall(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")

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

	result, err := trans.TransformResponse([]byte(geminiResp), false)
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

func TestGeminiTransformer_TransformResponse_Streaming(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")

	result, err := trans.TransformResponse([]byte("data: {}"), true)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for streaming, got %v", result)
	}
}

func TestGeminiTransformer_TransformResponseWithContext(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")
	ctx := transformer.NewStreamContext()

	// Gemini SSE format
	geminiEvent := `data: {"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"}}]}

`

	result, err := trans.TransformResponseWithContext([]byte(geminiEvent), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "data:") {
		t.Errorf("Expected OpenAI SSE format with 'data:', got '%s'", resultStr)
	}
}

func TestGeminiTransformer_TransformResponseWithContext_NonStreaming(t *testing.T) {
	trans := NewGeminiTransformer("gemini-2.0-flash")
	ctx := transformer.NewStreamContext()

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

	result, err := trans.TransformResponseWithContext([]byte(geminiResp), false, ctx)
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
