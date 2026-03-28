package request

import (
	"encoding/json"
	"testing"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func TestValidateTransformerRestrictsMessagesPathToAnthropic(t *testing.T) {
	if err := ValidateTransformer(shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatClaude,
	}, "cc_claude"); err != nil {
		t.Fatalf("expected cc_claude to be allowed, got %v", err)
	}

	if err := ValidateTransformer(shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatClaude,
	}, "cc_openai"); err == nil {
		t.Fatalf("expected non-claude transformer to be rejected for cursor messages path")
	}

	if err := ValidateTransformer(shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIChat,
	}, "cx_chat_openai"); err != nil {
		t.Fatalf("expected non-messages cursor path to remain allowed, got %v", err)
	}
}

func TestValidateTransformerMatrixForCursorRoutes(t *testing.T) {
	tests := []struct {
		name        string
		format      shared.ClientFormat
		transformer string
		wantErr     bool
	}{
		{name: "chat claude allowed", format: shared.ClientFormatOpenAIChat, transformer: "cx_chat_claude"},
		{name: "chat openai allowed", format: shared.ClientFormatOpenAIChat, transformer: "cx_chat_openai"},
		{name: "chat openai2 allowed", format: shared.ClientFormatOpenAIChat, transformer: "cx_chat_openai2"},
		{name: "chat gemini allowed", format: shared.ClientFormatOpenAIChat, transformer: "cx_chat_gemini"},
		{name: "chat cli rejected", format: shared.ClientFormatOpenAIChat, transformer: "cx_chat_cli", wantErr: true},
		{name: "responses claude allowed", format: shared.ClientFormatOpenAIResponses, transformer: "cx_resp_claude"},
		{name: "responses openai allowed", format: shared.ClientFormatOpenAIResponses, transformer: "cx_resp_openai"},
		{name: "responses openai2 allowed", format: shared.ClientFormatOpenAIResponses, transformer: "cx_resp_openai2"},
		{name: "responses gemini allowed", format: shared.ClientFormatOpenAIResponses, transformer: "cx_resp_gemini"},
		{name: "responses cli rejected", format: shared.ClientFormatOpenAIResponses, transformer: "cx_resp_cli", wantErr: true},
		{name: "messages claude allowed", format: shared.ClientFormatClaude, transformer: "cc_claude"},
		{name: "messages openai rejected", format: shared.ClientFormatClaude, transformer: "cc_openai", wantErr: true},
		{name: "messages openai2 rejected", format: shared.ClientFormatClaude, transformer: "cc_openai2", wantErr: true},
		{name: "messages gemini rejected", format: shared.ClientFormatClaude, transformer: "cc_gemini", wantErr: true},
	}

	for _, tt := range tests {
		err := ValidateTransformer(shared.RequestMeta{
			CursorMode:   true,
			ClientFormat: tt.format,
		}, tt.transformer)
		if tt.wantErr && err == nil {
			t.Fatalf("%s: expected error for transformer %s", tt.name, tt.transformer)
		}
		if !tt.wantErr && err != nil {
			t.Fatalf("%s: expected transformer %s to be allowed, got %v", tt.name, tt.transformer, err)
		}
	}
}

func TestValidateEndpointTransformerUsesCursorRouteMatrix(t *testing.T) {
	if err := ValidateEndpointTransformer(shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatClaude,
	}, "claude"); err != nil {
		t.Fatalf("expected claude endpoint transformer to be allowed, got %v", err)
	}

	if err := ValidateEndpointTransformer(shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatClaude,
	}, "openai"); err == nil {
		t.Fatalf("expected openai endpoint transformer to be rejected for cursor messages path")
	}

	if err := ValidateEndpointTransformer(shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIResponses,
	}, "openai2"); err != nil {
		t.Fatalf("expected openai2 endpoint transformer to remain allowed for cursor responses path, got %v", err)
	}
}

func TestValidateEndpointTransformerMatrixForCursorRoutes(t *testing.T) {
	tests := []struct {
		name                string
		format              shared.ClientFormat
		endpointTransformer string
		wantErr             bool
	}{
		{name: "chat claude allowed", format: shared.ClientFormatOpenAIChat, endpointTransformer: "claude"},
		{name: "chat openai allowed", format: shared.ClientFormatOpenAIChat, endpointTransformer: "openai"},
		{name: "chat openai2 allowed", format: shared.ClientFormatOpenAIChat, endpointTransformer: "openai2"},
		{name: "chat gemini allowed", format: shared.ClientFormatOpenAIChat, endpointTransformer: "gemini"},
		{name: "chat cli rejected", format: shared.ClientFormatOpenAIChat, endpointTransformer: "cli", wantErr: true},
		{name: "responses claude allowed", format: shared.ClientFormatOpenAIResponses, endpointTransformer: "claude"},
		{name: "responses openai allowed", format: shared.ClientFormatOpenAIResponses, endpointTransformer: "openai"},
		{name: "responses openai2 allowed", format: shared.ClientFormatOpenAIResponses, endpointTransformer: "openai2"},
		{name: "responses gemini allowed", format: shared.ClientFormatOpenAIResponses, endpointTransformer: "gemini"},
		{name: "responses cli rejected", format: shared.ClientFormatOpenAIResponses, endpointTransformer: "cli", wantErr: true},
		{name: "messages claude allowed", format: shared.ClientFormatClaude, endpointTransformer: "claude"},
		{name: "messages openai rejected", format: shared.ClientFormatClaude, endpointTransformer: "openai", wantErr: true},
		{name: "messages openai2 rejected", format: shared.ClientFormatClaude, endpointTransformer: "openai2", wantErr: true},
		{name: "messages gemini rejected", format: shared.ClientFormatClaude, endpointTransformer: "gemini", wantErr: true},
	}

	for _, tt := range tests {
		err := ValidateEndpointTransformer(shared.RequestMeta{
			CursorMode:   true,
			ClientFormat: tt.format,
		}, tt.endpointTransformer)
		if tt.wantErr && err == nil {
			t.Fatalf("%s: expected error for endpoint transformer %s", tt.name, tt.endpointTransformer)
		}
		if !tt.wantErr && err != nil {
			t.Fatalf("%s: expected endpoint transformer %s to be allowed, got %v", tt.name, tt.endpointTransformer, err)
		}
	}
}

func TestNormalizeGeminiFunctionPartsWrapsPlainStringsLikeAPI2Cursor(t *testing.T) {
	payload := []byte(`{
		"contents":[
			{
				"role":"model",
				"parts":[
					{"functionCall":{"name":"tool_a","args":"plain text"}},
					{"functionResponse":{"name":"tool_a","response":"tool output"}}
				]
			}
		]
	}`)

	updated, err := NormalizeGeminiFunctionParts(payload)
	if err != nil {
		t.Fatalf("NormalizeGeminiFunctionParts failed: %v", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(updated, &body); err != nil {
		t.Fatalf("updated payload is not valid json: %v", err)
	}

	contents := body["contents"].([]interface{})
	parts := contents[0].(map[string]interface{})["parts"].([]interface{})

	functionCall := parts[0].(map[string]interface{})["functionCall"].(map[string]interface{})
	callArgs := functionCall["args"].(map[string]interface{})
	if callArgs["result"] != "plain text" {
		t.Fatalf("expected plain string args to be wrapped, got %#v", functionCall["args"])
	}

	functionResponse := parts[1].(map[string]interface{})["functionResponse"].(map[string]interface{})
	response := functionResponse["response"].(map[string]interface{})
	if response["result"] != "tool output" {
		t.Fatalf("expected plain string tool output to be wrapped, got %#v", functionResponse["response"])
	}
}

func TestNormalizeGeminiFunctionPartsKeepsValidJSONValues(t *testing.T) {
	payload := []byte(`{
		"contents":[
			{
				"role":"model",
				"parts":[
					{"functionCall":{"name":"tool_a","args":"{\"x\":1}"}},
					{"functionResponse":{"name":"tool_a","response":{"ok":true}}}
				]
			}
		]
	}`)

	updated, err := NormalizeGeminiFunctionParts(payload)
	if err != nil {
		t.Fatalf("NormalizeGeminiFunctionParts failed: %v", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(updated, &body); err != nil {
		t.Fatalf("updated payload is not valid json: %v", err)
	}

	contents := body["contents"].([]interface{})
	parts := contents[0].(map[string]interface{})["parts"].([]interface{})

	functionCall := parts[0].(map[string]interface{})["functionCall"].(map[string]interface{})
	callArgs := functionCall["args"].(map[string]interface{})
	if callArgs["x"] != float64(1) {
		t.Fatalf("expected valid json args to remain parsed object, got %#v", functionCall["args"])
	}

	functionResponse := parts[1].(map[string]interface{})["functionResponse"].(map[string]interface{})
	response := functionResponse["response"].(map[string]interface{})
	if response["ok"] != true {
		t.Fatalf("expected object tool output to remain unchanged, got %#v", functionResponse["response"])
	}
}

func TestNormalizeOpenAI2EasyInputMessagesKeepsAssistantMessageBeforeFunctionCalls(t *testing.T) {
	payload := []byte(`{
		"input":[
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"thinking aloud"}]},
			{"type":"function_call","call_id":"call_raw","name":"read_file","arguments":"{\"path\":\"README.md\"}"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]
	}`)

	updated, err := NormalizeOpenAI2EasyInputMessages(payload)
	if err != nil {
		t.Fatalf("NormalizeOpenAI2EasyInputMessages failed: %v", err)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(updated, &body); err != nil {
		t.Fatalf("updated payload is not valid json: %v", err)
	}

	input := body["input"].([]interface{})
	first := input[0].(map[string]interface{})
	if first["type"] != "message" {
		t.Fatalf("expected assistant item to remain structured message, got %#v", first)
	}
	content := first["content"].([]interface{})
	if content[0].(map[string]interface{})["type"] != "output_text" {
		t.Fatalf("expected assistant output_text block to be preserved, got %#v", content[0])
	}

	third := input[2].(map[string]interface{})
	if third["role"] != "user" || third["content"] != "continue" {
		t.Fatalf("expected plain user message to still simplify, got %#v", third)
	}
}
