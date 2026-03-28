package request

import (
	"encoding/json"
	"testing"

	cursorcache "github.com/lich0821/ccNexus/internal/cursor/cache"
	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func TestApplyStatelessTransformedCompatNormalizesResponsesBridgeToChat(t *testing.T) {
	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIResponses,
		ClientModel:  "gpt-5",
	}

	transformedBody := []byte(`{
		"model":"gpt-5",
		"messages":[
			{
				"role":"assistant",
				"content":[{"type":"tool_use","id":"call_1","name":"read_file","input":{"file_path":"README.md"}}]
			},
			{
				"role":"user",
				"content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"ok"}]}]
			}
		],
		"tools":[{"name":"read_file","description":"Read file","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}],
		"tool_choice":{"type":"any"}
	}`)

	updated, err := ApplyStatelessTransformedCompat(transformedBody, meta, "cx_resp_openai")
	if err != nil {
		t.Fatalf("ApplyStatelessTransformedCompat failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("updated body is not valid json: %v", err)
	}
	if payload["tool_choice"] != "required" {
		t.Fatalf("expected normalized tool_choice required, got %#v", payload["tool_choice"])
	}
	tools := payload["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})
	if tool["type"] != "function" {
		t.Fatalf("expected normalized function tool, got %#v", tool)
	}
	messages := payload["messages"].([]interface{})
	assistant := messages[0].(map[string]interface{})
	if _, ok := assistant["tool_calls"]; !ok {
		t.Fatalf("expected tool_use blocks converted to tool_calls, got %#v", assistant)
	}
}

func TestApplyStatelessTransformedCompatAppliesClaudeFloorAndCacheControl(t *testing.T) {
	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIChat,
		ClientModel:  "claude-sonnet",
	}

	transformedBody := []byte(`{
		"model":"claude-sonnet",
		"max_tokens":128,
		"tools":[{"name":"Read","input_schema":{"type":"object"}}],
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"hello"}
		]
	}`)

	updated, err := ApplyStatelessTransformedCompat(transformedBody, meta, "cx_chat_claude")
	if err != nil {
		t.Fatalf("ApplyStatelessTransformedCompat failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("updated body is not valid claude json: %v", err)
	}
	if payload["max_tokens"] != float64(8192) {
		t.Fatalf("expected cursor Claude chat max_tokens floor to 8192, got %#v", payload["max_tokens"])
	}
	tools := payload["tools"].([]interface{})
	lastTool := tools[len(tools)-1].(map[string]interface{})
	if _, ok := lastTool["cache_control"]; !ok {
		t.Fatalf("expected cache_control anchor on tools, got %#v", lastTool)
	}
}

func TestApplyStatelessTransformedCompatAddsDefaultClaudeToolSchemaLikeAPI2Cursor(t *testing.T) {
	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIChat,
		ClientModel:  "claude-sonnet",
	}

	transformedBody := []byte(`{
		"model":"claude-sonnet",
		"tools":[{"name":"Read","description":"Read file"}],
		"messages":[{"role":"user","content":"hi"}]
	}`)

	updated, err := ApplyStatelessTransformedCompat(transformedBody, meta, "cx_chat_claude")
	if err != nil {
		t.Fatalf("ApplyStatelessTransformedCompat failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("updated body is not valid claude json: %v", err)
	}

	tools := payload["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})
	inputSchema, ok := tool["input_schema"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected default input_schema object, got %#v", tool["input_schema"])
	}
	if inputSchema["type"] != "object" {
		t.Fatalf("expected default input_schema type object, got %#v", inputSchema["type"])
	}
	properties, ok := inputSchema["properties"].(map[string]interface{})
	if !ok || len(properties) != 0 {
		t.Fatalf("expected empty default input_schema properties, got %#v", inputSchema["properties"])
	}
}

func TestApplyStatelessTransformedCompatDoesNotInjectClaudeSchemaIntoOpenAIChat(t *testing.T) {
	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIChat,
		ClientModel:  "gpt-5",
	}

	transformedBody := []byte(`{
		"model":"gpt-5",
		"tools":[{"type":"function","function":{"name":"Read","description":"Read file","parameters":{"type":"object"}}}],
		"messages":[{"role":"user","content":"hi"}]
	}`)

	updated, err := ApplyStatelessTransformedCompat(transformedBody, meta, "cx_chat_openai")
	if err != nil {
		t.Fatalf("ApplyStatelessTransformedCompat failed: %v", err)
	}

	if string(updated) != string(transformedBody) {
		t.Fatalf("expected openai chat payload to remain unchanged, got %s", string(updated))
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("updated body is not valid openai json: %v", err)
	}

	tools := payload["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})
	if _, ok := tool["input_schema"]; ok {
		t.Fatalf("did not expect Claude input_schema on openai tool, got %#v", tool)
	}
	if _, ok := tool["cache_control"]; ok {
		t.Fatalf("did not expect Claude cache_control on openai tool, got %#v", tool)
	}
}

func TestApplyStatelessTransformedCompatKeepsCursorMessagesPassthrough(t *testing.T) {
	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatClaude,
		ClientModel:  "claude-sonnet-4-20250514",
	}

	transformedBody := []byte(`{
		"model":"claude-sonnet-4-20250514",
		"messages":[{"role":"user","content":"hi"}],
		"tools":[{"name":"Read","description":"Read file"}]
	}`)

	updated, err := ApplyStatelessTransformedCompat(transformedBody, meta, "cc_claude")
	if err != nil {
		t.Fatalf("ApplyStatelessTransformedCompat failed: %v", err)
	}

	if string(updated) != string(transformedBody) {
		t.Fatalf("expected cursor messages request to stay passthrough, got %s", string(updated))
	}
}

func TestApplyStatelessTransformedCompatFlattensSafeOpenAI2MessagesToEasyInput(t *testing.T) {
	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIChat,
		ClientModel:  "gpt-5",
	}

	transformedBody := []byte(`{
		"model":"gpt-5",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi there"}]},
			{"type":"message","role":"user","content":[
				{"type":"input_text","text":"look"},
				{"type":"input_image","image_url":"https://example.com/cat.png"}
			]},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]
	}`)

	updated, err := ApplyStatelessTransformedCompat(transformedBody, meta, "cx_chat_openai2")
	if err != nil {
		t.Fatalf("ApplyStatelessTransformedCompat failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("updated body is not valid openai2 json: %v", err)
	}

	input := payload["input"].([]interface{})
	first := input[0].(map[string]interface{})
	if _, ok := first["type"]; ok || first["role"] != "user" || first["content"] != "hello" {
		t.Fatalf("expected user pure-text message flattened to easy input, got %#v", first)
	}

	second := input[1].(map[string]interface{})
	if _, ok := second["type"]; ok || second["role"] != "assistant" || second["content"] != "hi there" {
		t.Fatalf("expected assistant pure-text message flattened to easy input, got %#v", second)
	}

	third := input[2].(map[string]interface{})
	if third["type"] != "message" {
		t.Fatalf("expected multimodal message to remain typed, got %#v", third)
	}
	content := third["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("expected multimodal content preserved, got %#v", content)
	}

	fourth := input[3].(map[string]interface{})
	if fourth["type"] != "function_call_output" || fourth["call_id"] != "call_1" {
		t.Fatalf("expected function_call_output untouched, got %#v", fourth)
	}
}

func TestApplyStatelessTransformedCompatWrapsGeminiPlainStringFunctionPayloads(t *testing.T) {
	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIChat,
		ClientModel:  "gemini-2.5-pro",
	}

	transformedBody := []byte(`{
		"contents":[
			{
				"role":"model",
				"parts":[
					{"functionCall":{"name":"tool_a","args":"plain args"}},
					{"functionResponse":{"name":"tool_a","response":"plain output"}}
				]
			}
		]
	}`)

	updated, err := ApplyStatelessTransformedCompat(transformedBody, meta, "cx_chat_gemini")
	if err != nil {
		t.Fatalf("ApplyStatelessTransformedCompat failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("updated body is not valid gemini json: %v", err)
	}

	contents := payload["contents"].([]interface{})
	parts := contents[0].(map[string]interface{})["parts"].([]interface{})
	functionCall := parts[0].(map[string]interface{})["functionCall"].(map[string]interface{})
	callArgs := functionCall["args"].(map[string]interface{})
	if callArgs["result"] != "plain args" {
		t.Fatalf("expected plain gemini args to be wrapped like api2cursor, got %#v", functionCall["args"])
	}
}

func TestApplyPreparedCacheInjectsThinkingIntoChatMessages(t *testing.T) {
	cacheStore := cursorcache.NewThinkingCache()
	cacheMessages := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
		{"role": "assistant", "content": ""},
		{"role": "user", "content": "continue"},
	}
	cacheStore.StoreFromResponse(cacheMessages, map[string]interface{}{"reasoning_content": "cached think"})

	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIChat,
		ClientModel:  "gpt-5",
	}
	body := []byte(`{
		"model":"gpt-5",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"hello"},
			{"role":"assistant","content":""},
			{"role":"user","content":"continue"}
		]
	}`)

	updated, injected, err := ApplyPreparedCache(body, meta, cacheMessages, cacheStore)
	if err != nil {
		t.Fatalf("ApplyPreparedCache failed: %v", err)
	}
	if injected[2]["reasoning_content"] != "cached think" {
		t.Fatalf("expected injected cache reasoning, got %#v", injected[2]["reasoning_content"])
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("updated body is not valid json: %v", err)
	}
	messages := payload["messages"].([]interface{})
	assistant := messages[2].(map[string]interface{})
	if assistant["reasoning_content"] != "cached think" {
		t.Fatalf("expected rewritten chat request to include reasoning_content, got %#v", assistant["reasoning_content"])
	}
}

func TestApplyTransformedCompatInjectsThinkingIntoClaudeResponses(t *testing.T) {
	cacheStore := cursorcache.NewThinkingCache()
	cacheMessages := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
		{"role": "assistant", "content": ""},
		{"role": "user", "content": "continue"},
	}
	cacheStore.StoreFromResponse(cacheMessages, map[string]interface{}{"reasoning_content": "cached think"})

	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIResponses,
		ClientModel:  "claude-sonnet",
	}
	transformedBody := []byte(`{
		"model":"claude-sonnet",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"hello"},
			{"role":"assistant","content":""},
			{"role":"user","content":"continue"}
		]
	}`)

	updated, injected, err := ApplyTransformedCompat(transformedBody, meta, "cx_resp_claude", cacheMessages, cacheStore)
	if err != nil {
		t.Fatalf("ApplyTransformedCompat failed: %v", err)
	}
	if injected[2]["reasoning_content"] != "cached think" {
		t.Fatalf("expected cache messages to be injected, got %#v", injected[2]["reasoning_content"])
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("updated body is not valid json: %v", err)
	}
	messages := payload["messages"].([]interface{})
	found := false
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := message["content"].([]interface{})
		if !ok {
			continue
		}
		for _, rawBlock := range content {
			block, ok := rawBlock.(map[string]interface{})
			if ok && block["type"] == "thinking" && block["thinking"] == "cached think" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected injected thinking block in Claude request, got %#v", messages)
	}
}

func TestApplyStatelessTransformedCompatResponsesBackendMatrix(t *testing.T) {
	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIResponses,
		ClientModel:  "cursor-model",
	}

	t.Run("openai2 passthrough", func(t *testing.T) {
		body := []byte(`{"model":"gpt-5","input":[{"role":"user","content":"hi"}]}`)
		updated, err := ApplyStatelessTransformedCompat(body, meta, "cx_resp_openai2")
		if err != nil {
			t.Fatalf("ApplyStatelessTransformedCompat failed: %v", err)
		}
		if string(updated) != string(body) {
			t.Fatalf("expected native responses payload to remain unchanged, got %s", string(updated))
		}
	})

	t.Run("claude compat", func(t *testing.T) {
		body := []byte(`{
			"model":"claude-sonnet",
			"max_tokens":256,
			"tools":[{"name":"Read","description":"Read file"}],
			"messages":[{"role":"user","content":"hi"}]
		}`)
		updated, err := ApplyStatelessTransformedCompat(body, meta, "cx_resp_claude")
		if err != nil {
			t.Fatalf("ApplyStatelessTransformedCompat failed: %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(updated, &payload); err != nil {
			t.Fatalf("updated body is not valid claude json: %v", err)
		}
		if payload["max_tokens"] != float64(8192) {
			t.Fatalf("expected claude responses max_tokens floor, got %#v", payload["max_tokens"])
		}
		tools := payload["tools"].([]interface{})
		tool := tools[0].(map[string]interface{})
		if _, ok := tool["input_schema"]; !ok {
			t.Fatalf("expected default claude input_schema, got %#v", tool)
		}
		if _, ok := tool["cache_control"]; !ok {
			t.Fatalf("expected claude cache_control anchor, got %#v", tool)
		}
	})

	t.Run("claude tool_use input nil becomes object", func(t *testing.T) {
		body := []byte(`{
			"model":"claude-sonnet",
			"messages":[
				{"role":"assistant","content":[
					{"type":"tool_use","id":"toolu_1","name":"read_file","input":null}
				]}
			]
		}`)
		updated, err := ApplyStatelessTransformedCompat(body, shared.RequestMeta{
			CursorMode:   true,
			ClientFormat: shared.ClientFormatOpenAIChat,
			ClientModel:  "claude-sonnet",
		}, "cx_chat_claude")
		if err != nil {
			t.Fatalf("ApplyStatelessTransformedCompat failed: %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(updated, &payload); err != nil {
			t.Fatalf("updated body is not valid claude json: %v", err)
		}
		messages := payload["messages"].([]interface{})
		content := messages[0].(map[string]interface{})["content"].([]interface{})
		input := content[0].(map[string]interface{})["input"].(map[string]interface{})
		if len(input) != 0 {
			t.Fatalf("expected nil tool_use input to become empty object, got %#v", input)
		}
	})

	t.Run("gemini normalizes function parts", func(t *testing.T) {
		body := []byte(`{
			"contents":[
				{"role":"model","parts":[
					{"functionCall":{"name":"tool_a","args":"plain args"}},
					{"functionResponse":{"name":"tool_a","response":"plain output"}}
				]}
			]
		}`)
		updated, err := ApplyStatelessTransformedCompat(body, meta, "cx_resp_gemini")
		if err != nil {
			t.Fatalf("ApplyStatelessTransformedCompat failed: %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(updated, &payload); err != nil {
			t.Fatalf("updated body is not valid gemini json: %v", err)
		}
		parts := payload["contents"].([]interface{})[0].(map[string]interface{})["parts"].([]interface{})
		callArgs := parts[0].(map[string]interface{})["functionCall"].(map[string]interface{})["args"].(map[string]interface{})
		if callArgs["result"] != "plain args" {
			t.Fatalf("expected plain function args to be wrapped, got %#v", callArgs)
		}
		response := parts[1].(map[string]interface{})["functionResponse"].(map[string]interface{})["response"].(map[string]interface{})
		if response["result"] != "plain output" {
			t.Fatalf("expected plain function response to be wrapped, got %#v", response)
		}
	})
}
