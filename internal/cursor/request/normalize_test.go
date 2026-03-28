package request

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeRequestBodyChatFromResponsesLike(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"input":"hello",
		"tools":[{"name":"read_file","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}],
		"tool_choice":{"type":"any"}
	}`)

	normalized, err := NormalizeRequestBody("/v1/chat/completions", body)
	if err != nil {
		t.Fatalf("NormalizeRequestBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if _, ok := payload["messages"].([]interface{}); !ok {
		t.Fatalf("expected messages in normalized payload, got %#v", payload)
	}
	if payload["tool_choice"] != "required" {
		t.Fatalf("expected required tool_choice, got %#v", payload["tool_choice"])
	}
	tools := payload["tools"].([]interface{})
	functionTool := tools[0].(map[string]interface{})["function"].(map[string]interface{})
	if _, ok := functionTool["parameters"].(map[string]interface{}); !ok {
		t.Fatalf("expected function.parameters to keep JSON schema object, got %#v", functionTool["parameters"])
	}
}

func TestNormalizeRequestBodyResponsesFromChatLikePreservesToolChoiceAndTools(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{"name":"read_file","description":"Read file"}],
		"tool_choice":{"type":"any"},
		"stream":true
	}`)

	normalized, err := NormalizeRequestBody("/v1/responses", body)
	if err != nil {
		t.Fatalf("NormalizeRequestBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if payload["stream"] != true {
		t.Fatalf("expected stream flag preserved through responses normalize, got %#v", payload["stream"])
	}
	tools := payload["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})
	if tool["type"] != "function" {
		t.Fatalf("expected responses normalize to preserve normalized function tool, got %#v", tool)
	}
	if payload["tool_choice"] != "required" {
		t.Fatalf("expected responses normalize to preserve required tool_choice, got %#v", payload["tool_choice"])
	}
}

func TestNormalizeRequestBodyChatPreservesToolSchemaFieldsLikeAPI2Cursor(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{"name":"read_file","description":"Read file","input_schema":{
			"$schema":"https://json-schema.org/draft/2020-12/schema",
			"title":"ReadFile",
			"type":"object",
			"description":"schema root",
			"properties":{
				"path":{"type":"string","description":"file path"}
			}
		}}]
	}`)

	normalized, err := NormalizeRequestBody("/v1/chat/completions", body)
	if err != nil {
		t.Fatalf("NormalizeRequestBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	functionTool := payload["tools"].([]interface{})[0].(map[string]interface{})["function"].(map[string]interface{})
	parameters := functionTool["parameters"].(map[string]interface{})
	if parameters["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("expected $schema to be preserved, got %#v", parameters["$schema"])
	}
	if parameters["title"] != "ReadFile" {
		t.Fatalf("expected title to be preserved, got %#v", parameters["title"])
	}
	if parameters["description"] != "schema root" {
		t.Fatalf("expected description to be preserved, got %#v", parameters["description"])
	}
	pathProp := parameters["properties"].(map[string]interface{})["path"].(map[string]interface{})
	if pathProp["description"] != "file path" {
		t.Fatalf("expected nested property description preserved, got %#v", pathProp["description"])
	}
}

func TestNormalizeRequestBodyChatStripsResponsesOnlyPreviousResponseID(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"previous_response_id":"resp_123",
		"messages":[{"role":"user","content":"hello"}]
	}`)

	normalized, err := NormalizeRequestBody("/v1/chat/completions", body)
	if err != nil {
		t.Fatalf("NormalizeRequestBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if _, ok := payload["previous_response_id"]; ok {
		t.Fatalf("expected previous_response_id to be stripped from chat payload, got %#v", payload["previous_response_id"])
	}
}

func TestNormalizeRequestBodyChatPromotesTopLevelSystemAndStripsCacheControl(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"system":[{"text":"follow the rules","cache_control":{"type":"ephemeral"}}],
		"messages":[
			{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]}
		]
	}`)

	normalized, err := NormalizeRequestBody("/v1/chat/completions", body)
	if err != nil {
		t.Fatalf("NormalizeRequestBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if _, ok := payload["system"]; ok {
		t.Fatalf("expected top-level system to be normalized into messages, got %#v", payload["system"])
	}
	messages := payload["messages"].([]interface{})
	if messages[0].(map[string]interface{})["role"] != "system" {
		t.Fatalf("expected synthetic system message prepended, got %#v", messages[0])
	}
	if messages[0].(map[string]interface{})["content"] != "follow the rules" {
		t.Fatalf("expected system text preserved, got %#v", messages[0])
	}
	content := messages[1].(map[string]interface{})["content"].([]interface{})
	block := content[0].(map[string]interface{})
	if _, ok := block["cache_control"]; ok {
		t.Fatalf("expected cache_control stripped from OpenAI chat content blocks, got %#v", block)
	}
}

func TestNormalizeRequestBodyChatKeepsStandardFunctionToolUntouchedLikeAPI2Cursor(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{
			"type":"function",
			"x-extra":"keep-me",
			"function":{
				"name":"read_file",
				"description":"Read file",
				"parameters":{"type":"object"},
				"strict":true
			}
		}]
	}`)

	normalized, err := NormalizeRequestBody("/v1/chat/completions", body)
	if err != nil {
		t.Fatalf("NormalizeRequestBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	tool := payload["tools"].([]interface{})[0].(map[string]interface{})
	if tool["x-extra"] != "keep-me" {
		t.Fatalf("expected top-level extra field preserved, got %#v", tool["x-extra"])
	}
	functionData := tool["function"].(map[string]interface{})
	if functionData["strict"] != true {
		t.Fatalf("expected nested function field preserved, got %#v", functionData["strict"])
	}
}

func TestNormalizeRequestBodyChatAddsDefaultToolParametersLikeAPI2Cursor(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[{"name":"read_file","description":"Read file"}]
	}`)

	normalized, err := NormalizeRequestBody("/v1/chat/completions", body)
	if err != nil {
		t.Fatalf("NormalizeRequestBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	functionTool := payload["tools"].([]interface{})[0].(map[string]interface{})["function"].(map[string]interface{})
	parameters, ok := functionTool["parameters"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected default function.parameters object, got %#v", functionTool["parameters"])
	}
	if parameters["type"] != "object" {
		t.Fatalf("expected default parameter type object, got %#v", parameters["type"])
	}
	properties, ok := parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected default properties object, got %#v", parameters["properties"])
	}
	if len(properties) != 0 {
		t.Fatalf("expected default properties to be empty, got %#v", properties)
	}
}

func TestNormalizeOpenAIChatBodyConvertsToolBlocks(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"read_file","input":{"file_path":"README.md"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"ok"}]}]}
		]
	}`)

	normalized := NormalizeOpenAIChatBody(body)
	var payload map[string]interface{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	messages := payload["messages"].([]interface{})
	assistant := messages[0].(map[string]interface{})
	if _, ok := assistant["tool_calls"]; !ok {
		t.Fatalf("expected tool_calls converted, got %#v", assistant)
	}
	tool := messages[1].(map[string]interface{})
	if tool["role"] != "tool" {
		t.Fatalf("expected tool role after tool_result conversion, got %#v", tool)
	}
}

func TestNormalizeOpenAIChatBodyToolUseKeepsOriginalInputFieldsLikeAPI2Cursor(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"read_file","input":{"file_path":"README.md"}}]}
		]
	}`)

	normalized := NormalizeOpenAIChatBody(body)
	var payload map[string]interface{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	messages := payload["messages"].([]interface{})
	assistant := messages[0].(map[string]interface{})
	toolCalls := assistant["tool_calls"].([]interface{})
	functionData := toolCalls[0].(map[string]interface{})["function"].(map[string]interface{})
	arguments := functionData["arguments"].(string)
	if !strings.Contains(arguments, `"file_path":"README.md"`) {
		t.Fatalf("expected tool_use input to preserve file_path, got %s", arguments)
	}
	if strings.Contains(arguments, `"path":"README.md"`) {
		t.Fatalf("did not expect request normalize to rewrite file_path to path, got %s", arguments)
	}
}

func TestNormalizeOpenAIChatBodyPreservesExistingToolCallShapeLikeAPI2Cursor(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","function":{"name":"read_file","arguments":{"file_path":"README.md"}}}]}
		]
	}`)

	normalized := NormalizeOpenAIChatBody(body)
	var payload map[string]interface{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	assistant := payload["messages"].([]interface{})[0].(map[string]interface{})
	toolCall := assistant["tool_calls"].([]interface{})[0].(map[string]interface{})
	if _, ok := toolCall["index"]; ok {
		t.Fatalf("expected request normalize not to add tool_call index, got %#v", toolCall["index"])
	}
	if _, ok := toolCall["type"]; ok {
		t.Fatalf("expected request normalize not to force tool_call type, got %#v", toolCall["type"])
	}
	functionData := toolCall["function"].(map[string]interface{})
	args, ok := functionData["arguments"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected arguments object to remain untouched, got %#v", functionData["arguments"])
	}
	if args["file_path"] != "README.md" {
		t.Fatalf("expected arguments object preserved, got %#v", args)
	}
}

func TestNormalizeRequestBodyClaudeKeepsPassthroughShape(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"answer","reasoning_content":"think"},
			{"role":"tool","tool_call_id":"call_1","content":"ok"}
		]
	}`)

	normalized, err := NormalizeRequestBody("/v1/messages", body)
	if err != nil {
		t.Fatalf("NormalizeRequestBody failed: %v", err)
	}
	if string(normalized) != string(body) {
		t.Fatalf("expected messages request passthrough, got %s", normalized)
	}
}

func TestNormalizeOpenAIChatBodyKeepsFunctionToolChoiceShape(t *testing.T) {
	body := []byte(`{
		"messages":[{"role":"user","content":"hello"}],
		"tool_choice":{"type":"function","name":"read_file"}
	}`)

	normalized := NormalizeOpenAIChatBody(body)
	var payload map[string]interface{}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	toolChoice, ok := payload["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected tool_choice object to remain unchanged, got %#v", payload["tool_choice"])
	}
	if toolChoice["type"] != "function" {
		t.Fatalf("expected function tool_choice type, got %#v", toolChoice)
	}
	if toolChoice["name"] != "read_file" {
		t.Fatalf("expected function name preserved, got %#v", toolChoice)
	}
}
