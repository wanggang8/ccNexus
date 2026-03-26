package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lich0821/ccNexus/internal/transformer/convert"
)

func TestPrepareProxyRequestStripsCursorPrefixAndNormalizesChatPayload(t *testing.T) {
	req := httptest.NewRequest("POST", "http://localhost/cursor/v1/chat/completions", strings.NewReader(""))
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"input":"hello",
		"stream":true,
		"tools":[{"name":"read_file","description":"Read file","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}],
		"tool_choice":{"type":"any"}
	}`)

	effectiveReq, normalizedBody, meta, err := prepareProxyRequest(req, body)
	if err != nil {
		t.Fatalf("prepareProxyRequest failed: %v", err)
	}
	if !meta.CursorMode {
		t.Fatalf("expected cursor mode")
	}
	if meta.EffectivePath != "/v1/chat/completions" {
		t.Fatalf("unexpected effective path: %s", meta.EffectivePath)
	}
	if effectiveReq.URL.Path != "/v1/chat/completions" {
		t.Fatalf("unexpected cloned request path: %s", effectiveReq.URL.Path)
	}
	if meta.ClientFormat != ClientFormatOpenAIChat {
		t.Fatalf("unexpected client format: %s", meta.ClientFormat)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalizedBody, &payload); err != nil {
		t.Fatalf("normalized body is not valid json: %v", err)
	}
	if _, ok := payload["messages"].([]interface{}); !ok {
		t.Fatalf("expected responses payload to be converted into chat messages: %s", string(normalizedBody))
	}
	if payload["tool_choice"] != "required" {
		t.Fatalf("expected tool_choice any to normalize to required, got %#v", payload["tool_choice"])
	}
	tools, ok := payload["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("expected one normalized tool, got %#v", payload["tools"])
	}
	tool, ok := tools[0].(map[string]interface{})
	if !ok || tool["type"] != "function" {
		t.Fatalf("expected normalized function tool, got %#v", tools[0])
	}
}

func TestPrepareProxyRequestConvertsChatPayloadForCursorResponses(t *testing.T) {
	req := httptest.NewRequest("POST", "http://localhost/cursor/v1/responses", strings.NewReader(""))
	body := []byte(`{
		"model":"gpt-5",
		"messages":[{"role":"user","content":"hello"}],
		"stream":false
	}`)

	_, normalizedBody, meta, err := prepareProxyRequest(req, body)
	if err != nil {
		t.Fatalf("prepareProxyRequest failed: %v", err)
	}
	if meta.ClientFormat != ClientFormatOpenAIResponses {
		t.Fatalf("unexpected client format: %s", meta.ClientFormat)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalizedBody, &payload); err != nil {
		t.Fatalf("normalized body is not valid json: %v", err)
	}
	if _, ok := payload["input"]; !ok {
		t.Fatalf("expected chat payload to be converted into responses input: %s", string(normalizedBody))
	}
}

func TestApplyCursorTransformedRequestCompatInjectsThinkingCacheForResponses(t *testing.T) {
	globalCursorThinkingCache = &cursorThinkingCache{store: make(map[string]cursorThinkingCacheEntry)}
	cacheMessages := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
		{"role": "assistant", "content": ""},
		{"role": "user", "content": "continue"},
	}
	globalCursorThinkingCache.StoreFromResponse(cacheMessages, map[string]interface{}{"reasoning_content": "cached think"})

	body := []byte(`{
		"model":"gpt-5",
		"input":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"hello"},
			{"role":"assistant","content":""},
			{"role":"user","content":"continue"}
		]
	}`)
	meta := proxyRequestMeta{
		CursorMode:    true,
		ClientFormat:  ClientFormatOpenAIResponses,
		ClientModel:   "gpt-5",
		CacheMessages: cacheMessages,
	}

	transformedBody, err := convert.OpenAI2ReqToOpenAI(body, "gpt-5")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}
	updated, err := applyCursorTransformedRequestCompat(transformedBody, &meta, "cx_resp_openai")
	if err != nil {
		t.Fatalf("applyCursorTransformedRequestCompat failed: %v", err)
	}
	if meta.CacheMessages[2]["reasoning_content"] != "cached think" {
		t.Fatalf("expected cache messages to be injected, got %#v", meta.CacheMessages[2]["reasoning_content"])
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("updated body is not valid chat json: %v", err)
	}
	messages := payload["messages"].([]interface{})
	assistant := messages[2].(map[string]interface{})
	if assistant["reasoning_content"] != "cached think" {
		t.Fatalf("expected injected reasoning_content in transformed chat request, got %#v", assistant["reasoning_content"])
	}
}

func TestApplyCursorTransformedRequestCompatInjectsThinkingCacheForClaudeResponses(t *testing.T) {
	globalCursorThinkingCache = &cursorThinkingCache{store: make(map[string]cursorThinkingCacheEntry)}
	cacheMessages := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
		{"role": "assistant", "content": ""},
		{"role": "user", "content": "continue"},
	}
	globalCursorThinkingCache.StoreFromResponse(cacheMessages, map[string]interface{}{"reasoning_content": "cached think"})

	meta := proxyRequestMeta{
		CursorMode:    true,
		ClientFormat:  ClientFormatOpenAIResponses,
		ClientModel:   "claude-sonnet",
		CacheMessages: cacheMessages,
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
	updated, err := applyCursorTransformedRequestCompat(transformedBody, &meta, "cx_resp_claude")
	if err != nil {
		t.Fatalf("applyCursorTransformedRequestCompat failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("updated body is not valid claude json: %v", err)
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
		t.Fatalf("expected injected thinking block in claude request, got %#v", messages)
	}
}

func TestApplyCursorTransformedRequestCompatInjectsThinkingCacheForGeminiResponses(t *testing.T) {
	globalCursorThinkingCache = &cursorThinkingCache{store: make(map[string]cursorThinkingCacheEntry)}
	cacheMessages := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
		{"role": "assistant", "content": ""},
		{"role": "user", "content": "continue"},
	}
	globalCursorThinkingCache.StoreFromResponse(cacheMessages, map[string]interface{}{"reasoning_content": "cached think"})

	meta := proxyRequestMeta{
		CursorMode:    true,
		ClientFormat:  ClientFormatOpenAIResponses,
		ClientModel:   "gemini-2.5-pro",
		CacheMessages: cacheMessages,
	}

	transformedBody := []byte(`{
		"contents":[
			{"role":"user","parts":[{"text":"hi"}]},
			{"role":"model","parts":[{"text":"hello"}]},
			{"role":"model","parts":[{"text":""}]},
			{"role":"user","parts":[{"text":"continue"}]}
		]
	}`)
	updated, err := applyCursorTransformedRequestCompat(transformedBody, &meta, "cx_resp_gemini")
	if err != nil {
		t.Fatalf("applyCursorTransformedRequestCompat failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("updated body is not valid gemini json: %v", err)
	}
	contents := payload["contents"].([]interface{})
	found := false
	for _, rawContent := range contents {
		content, ok := rawContent.(map[string]interface{})
		if !ok {
			continue
		}
		parts, ok := content["parts"].([]interface{})
		if !ok {
			continue
		}
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]interface{})
			if ok && part["thought"] == true && part["text"] == "cached think" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected injected thought part in gemini request, got %#v", contents)
	}
}

func TestFixCursorChatResponseBodyRepairsLegacyFields(t *testing.T) {
	body := []byte(`{
		"id":"chatcmpl_1",
		"model":"upstream-model",
		"choices":[
			{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"<think>Reason here</think>Hello",
					"reasoningContent":"Reason field",
					"function_call":{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}
				},
				"finish_reason":"function_call"
			}
		]
	}`)

	fixed, err := fixCursorResponseBody(body, proxyRequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatOpenAIChat,
		ClientModel:  "cursor-model",
		CacheMessages: []map[string]interface{}{
			{"role": "user", "content": "hello"},
		},
	})
	if err != nil {
		t.Fatalf("fixCursorResponseBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(fixed, &payload); err != nil {
		t.Fatalf("fixed body is not valid json: %v", err)
	}
	if payload["model"] != "cursor-model" {
		t.Fatalf("expected client model rewrite, got %#v", payload["model"])
	}
	choices := payload["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})
	if _, ok := message["reasoning_content"]; !ok {
		t.Fatalf("expected reasoning_content to be present: %#v", message)
	}
	if _, ok := message["function_call"]; ok {
		t.Fatalf("expected legacy function_call to be removed: %#v", message)
	}
	toolCalls, ok := message["tool_calls"].([]interface{})
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected tool_calls to be synthesized: %#v", message["tool_calls"])
	}
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("expected finish_reason=tool_calls, got %#v", choice["finish_reason"])
	}
}

func TestCursorThinkingCacheStoresAndInjectsForChatRequests(t *testing.T) {
	globalCursorThinkingCache = &cursorThinkingCache{store: make(map[string]cursorThinkingCacheEntry)}

	sourceMessages := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
		{"role": "user", "content": "continue"},
	}
	assistantResponse := []byte(`{
		"id":"chatcmpl_2",
		"model":"upstream-model",
		"choices":[{"index":0,"message":{"role":"assistant","content":"ok","reasoning_content":"cached think"},"finish_reason":"stop"}]
	}`)

	if _, err := fixCursorResponseBody(assistantResponse, proxyRequestMeta{
		CursorMode:    true,
		ClientFormat:  ClientFormatOpenAIChat,
		ClientModel:   "cursor-model",
		CacheMessages: sourceMessages,
	}); err != nil {
		t.Fatalf("fixCursorResponseBody failed: %v", err)
	}

	req := httptest.NewRequest("POST", "http://localhost/cursor/v1/chat/completions", strings.NewReader(""))
	body := []byte(`{
		"model":"gpt-5",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"hello"},
			{"role":"assistant","content":""},
			{"role":"user","content":"continue"}
		]
	}`)

	_, normalizedBody, meta, err := prepareProxyRequest(req, body)
	if err != nil {
		t.Fatalf("prepareProxyRequest failed: %v", err)
	}
	if !meta.CursorMode {
		t.Fatalf("expected cursor mode")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalizedBody, &payload); err != nil {
		t.Fatalf("normalized body is not valid json: %v", err)
	}
	messages := payload["messages"].([]interface{})
	assistant := messages[2].(map[string]interface{})
	if assistant["reasoning_content"] != "cached think" {
		t.Fatalf("expected cached reasoning_content injection, got %#v", assistant["reasoning_content"])
	}
}

func TestPrepareProxyRequestDoesNotInjectThinkingCacheWithoutCursorPrefix(t *testing.T) {
	globalCursorThinkingCache = &cursorThinkingCache{store: make(map[string]cursorThinkingCacheEntry)}
	globalCursorThinkingCache.store["deadbeef:deadbeef"] = cursorThinkingCacheEntry{
		Reasoning: "should not inject",
		StoredAt:  time.Now(),
	}

	req := httptest.NewRequest("POST", "http://localhost/v1/chat/completions", strings.NewReader(""))
	body := []byte(`{
		"model":"gpt-5",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"hello"},
			{"role":"assistant","content":""},
			{"role":"user","content":"continue"}
		]
	}`)

	_, normalizedBody, meta, err := prepareProxyRequest(req, body)
	if err != nil {
		t.Fatalf("prepareProxyRequest failed: %v", err)
	}
	if meta.CursorMode {
		t.Fatalf("did not expect cursor mode")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalizedBody, &payload); err != nil {
		t.Fatalf("normalized body is not valid json: %v", err)
	}
	messages := payload["messages"].([]interface{})
	assistant := messages[2].(map[string]interface{})
	if _, ok := assistant["reasoning_content"]; ok {
		t.Fatalf("did not expect reasoning_content injection for normal route: %#v", assistant)
	}
}

func TestFixCursorStreamBundleRewritesResponsesEvents(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress","model":"upstream-model"}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","usage":{"input_tokens":7,"output_tokens":5,"total_tokens":12}}}`,
		"",
	}, "\n"))

	fixed, err := fixCursorStreamBundle(bundle, proxyRequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatOpenAIResponses,
		ClientModel:  "cursor-model",
	})
	if err != nil {
		t.Fatalf("fixCursorStreamBundle failed: %v", err)
	}

	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, "event: response.created") {
		t.Fatalf("expected response.created event line, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, "event: response.completed") {
		t.Fatalf("expected response.completed event line, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"model":"cursor-model"`) {
		t.Fatalf("expected model rewrite in responses events, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"type":"output_text"`) || !strings.Contains(fixedStr, `"delta":"hello"`) {
		t.Fatalf("expected output_text delta payload shape, got %s", fixedStr)
	}
	if strings.Contains(fixedStr, `"type":"response.created"`) {
		t.Fatalf("did not expect event type echoed inside created payload, got %s", fixedStr)
	}
}

func TestFixCursorResponseBodyStoresThinkingCacheForResponses(t *testing.T) {
	globalCursorThinkingCache = &cursorThinkingCache{store: make(map[string]cursorThinkingCacheEntry)}

	cacheMessages := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
		{"role": "user", "content": "continue"},
	}
	body := []byte(`{
		"id":"resp_2",
		"object":"response",
		"model":"upstream-model",
		"output":[
			{"type":"reasoning","summary":[{"type":"summary_text","text":"stored think"}]},
			{"type":"message","content":[{"type":"output_text","text":"ok"}]}
		]
	}`)

	if _, err := fixCursorResponseBody(body, proxyRequestMeta{
		CursorMode:      true,
		ClientFormat:    ClientFormatOpenAIResponses,
		ClientModel:     "cursor-model",
		TransformerName: "cx_resp_openai",
		CacheMessages:   cacheMessages,
	}); err != nil {
		t.Fatalf("fixCursorResponseBody failed: %v", err)
	}

	reqBody := []byte(`{
		"model":"gpt-5",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"hello"},
			{"role":"assistant","content":""},
			{"role":"user","content":"continue"}
		]
	}`)
	req := httptest.NewRequest("POST", "http://localhost/cursor/v1/chat/completions", strings.NewReader(""))
	_, normalizedBody, _, err := prepareProxyRequest(req, reqBody)
	if err != nil {
		t.Fatalf("prepareProxyRequest failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(normalizedBody, &payload); err != nil {
		t.Fatalf("normalized body is not valid json: %v", err)
	}
	messages := payload["messages"].([]interface{})
	assistant := messages[2].(map[string]interface{})
	if assistant["reasoning_content"] != "stored think" {
		t.Fatalf("expected responses reasoning to be stored for later injection, got %#v", assistant["reasoning_content"])
	}
}

func TestFixCursorStreamBundlePreservesNativeResponsesEventShape(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress","model":"upstream-model","output":[]}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","status":"in_progress","role":"assistant","content":[]}}`,
		"",
	}, "\n"))

	fixed, err := fixCursorStreamBundle(bundle, proxyRequestMeta{
		CursorMode:      true,
		ClientFormat:    ClientFormatOpenAIResponses,
		ClientModel:     "cursor-model",
		TransformerName: "cx_resp_openai2",
	})
	if err != nil {
		t.Fatalf("fixCursorStreamBundle failed: %v", err)
	}

	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, `"type":"response.created"`) {
		t.Fatalf("expected native responses payload to preserve type field, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"response":{"id":"resp_1"`) {
		t.Fatalf("expected native responses payload to preserve response wrapper, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"item":{"content":[],"id":"msg_1"`) {
		t.Fatalf("expected native responses payload to preserve item wrapper, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"model":"cursor-model"`) {
		t.Fatalf("expected native responses model rewrite, got %s", fixedStr)
	}
}

func TestFixCursorStreamBundleReconstructsCompletedResponsesOutput(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress","model":"upstream-model"}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[]}}`,
		"",
		`data: {"type":"response.reasoning_summary_text.delta","delta":"think"}`,
		"",
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"msg_1","status":"in_progress","role":"assistant","content":[]}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"fc_1","status":"in_progress","call_id":"call_1","name":"read_file","arguments":""}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","delta":"{\"path\":\"README.md\"}"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","usage":{"input_tokens":7,"output_tokens":5,"total_tokens":12}}}`,
		"",
	}, "\n"))

	fixed, err := fixCursorStreamBundle(bundle, proxyRequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatOpenAIResponses,
		ClientModel:  "cursor-model",
		CursorState: &cursorCompatState{
			ResponsesTools:  make(map[int]*cursorResponsesToolState),
			ResponsesOutput: make([]map[string]interface{}, 0),
		},
	})
	if err != nil {
		t.Fatalf("fixCursorStreamBundle failed: %v", err)
	}

	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, `"summary":[{"text":"think","type":"summary_text"}]`) {
		t.Fatalf("expected reconstructed reasoning output in completed event, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"content":[{"text":"hello","type":"output_text"}]`) {
		t.Fatalf("expected reconstructed message output in completed event, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"name":"read_file"`) || !strings.Contains(fixedStr, `"arguments":"{\"path\":\"README.md\"}"`) {
		t.Fatalf("expected reconstructed tool output in completed event, got %s", fixedStr)
	}
}

func TestFixCursorStreamBundleEnrichesTransformedResponsesEventShape(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning"}}`,
		"",
		`data: {"type":"response.reasoning_summary_text.delta","delta":"think first"}`,
		"",
		`data: {"type":"response.reasoning_summary_text.done"}`,
		"",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning"}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"message"}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"type":"response.output_text.done"}`,
		"",
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"message"}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","name":"read_file"}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","delta":"{\"path\":\"README.md\"}"}`,
		"",
		`data: {"type":"response.function_call_arguments.done"}`,
		"",
	}, "\n"))

	fixed, err := fixCursorStreamBundle(bundle, proxyRequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatOpenAIResponses,
		ClientModel:  "cursor-model",
		CursorState: &cursorCompatState{
			ResponsesTools:  make(map[int]*cursorResponsesToolState),
			ResponsesOutput: make([]map[string]interface{}, 0),
		},
	})
	if err != nil {
		t.Fatalf("fixCursorStreamBundle failed: %v", err)
	}

	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, `event: response.created`) || !strings.Contains(fixedStr, `"output":[]`) || !strings.Contains(fixedStr, `"model":"cursor-model"`) {
		t.Fatalf("expected enriched created event with output/model, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `event: response.output_item.added`) || !strings.Contains(fixedStr, `"type":"reasoning"`) || !strings.Contains(fixedStr, `"id":"rs_`) {
		t.Fatalf("expected reasoning added item to receive generated id, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `event: response.reasoning_summary_text.done`) || !strings.Contains(fixedStr, `"text":"think first"`) {
		t.Fatalf("expected reasoning done payload to include summary text, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"type":"message"`) || !strings.Contains(fixedStr, `"id":"msg_`) || !strings.Contains(fixedStr, `"status":"in_progress"`) {
		t.Fatalf("expected message added item to be enriched, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `event: response.output_text.done`) || !strings.Contains(fixedStr, `"text":"hello"`) {
		t.Fatalf("expected output_text.done to carry final text, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `event: response.function_call_arguments.done`) || !strings.Contains(fixedStr, `"arguments":"{\"path\":\"README.md\"}"`) {
		t.Fatalf("expected function_call_arguments.done to carry buffered arguments, got %s", fixedStr)
	}
}

func TestFixCursorStreamBundleHandlesInterleavedResponseToolCalls(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","call_id":"call_1","name":"read_file"}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"path\":\"REA"}`,
		"",
		`data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","call_id":"call_2","name":"write_file"}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"path\":\"OUT"}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"DME.md\"}"}`,
		"",
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","call_id":"call_1","name":"read_file"}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"PUT.md\"}"}`,
		"",
		`data: {"type":"response.output_item.done","output_index":2,"item":{"type":"function_call","call_id":"call_2","name":"write_file"}}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed"}}`,
		"",
	}, "\n"))

	fixed, err := fixCursorStreamBundle(bundle, proxyRequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatOpenAIResponses,
		ClientModel:  "cursor-model",
		CursorState: &cursorCompatState{
			ResponsesTools:  make(map[int]*cursorResponsesToolState),
			ResponsesOutput: make([]map[string]interface{}, 0),
		},
	})
	if err != nil {
		t.Fatalf("fixCursorStreamBundle failed: %v", err)
	}

	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, `"call_id":"call_1"`) || !strings.Contains(fixedStr, `"arguments":"{\"path\":\"README.md\"}"`) {
		t.Fatalf("expected first tool call arguments to stay isolated, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"call_id":"call_2"`) || !strings.Contains(fixedStr, `"arguments":"{\"path\":\"OUTPUT.md\"}"`) {
		t.Fatalf("expected second tool call arguments to stay isolated, got %s", fixedStr)
	}
}

func TestFixCursorStreamBundleKeepsCompletedBeforeDone(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n"))

	fixed, err := fixCursorStreamBundle(bundle, proxyRequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatOpenAIResponses,
		ClientModel:  "cursor-model",
		CursorState: &cursorCompatState{
			ResponsesTools:  make(map[int]*cursorResponsesToolState),
			ResponsesOutput: make([]map[string]interface{}, 0),
		},
	})
	if err != nil {
		t.Fatalf("fixCursorStreamBundle failed: %v", err)
	}

	fixedStr := string(fixed)
	completedIndex := strings.Index(fixedStr, "event: response.completed")
	doneIndex := strings.Index(fixedStr, "data: [DONE]")
	if completedIndex == -1 || doneIndex == -1 || completedIndex > doneIndex {
		t.Fatalf("expected response.completed before [DONE], got %s", fixedStr)
	}
}

func TestFixCursorStreamBundleRoutesArgumentsDoneByOutputIndex(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","call_id":"call_1","name":"read_file"}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"path\":\"README.md\"}"}`,
		"",
		`data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","call_id":"call_2","name":"write_file"}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"path\":\"OUTPUT.md\"}"}`,
		"",
		`data: {"type":"response.function_call_arguments.done","output_index":1}`,
		"",
	}, "\n"))

	fixed, err := fixCursorStreamBundle(bundle, proxyRequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatOpenAIResponses,
		ClientModel:  "cursor-model",
		CursorState: &cursorCompatState{
			ResponsesTools:  make(map[int]*cursorResponsesToolState),
			ResponsesOutput: make([]map[string]interface{}, 0),
		},
	})
	if err != nil {
		t.Fatalf("fixCursorStreamBundle failed: %v", err)
	}

	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, `event: response.function_call_arguments.done`) || !strings.Contains(fixedStr, `"arguments":"{\"path\":\"README.md\"}"`) {
		t.Fatalf("expected arguments.done to inherit the first tool arguments, got %s", fixedStr)
	}
	if strings.Contains(fixedStr, `event: response.function_call_arguments.done`) && strings.Contains(fixedStr, `"arguments":"{\"path\":\"OUTPUT.md\"}"`) {
		t.Fatalf("did not expect arguments.done for output_index 1 to pick the latest active tool, got %s", fixedStr)
	}
}

func TestFixCursorStreamBundlePreservesErrorAfterPartialOutput(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		"",
		`data: {"error":{"message":"boom","type":"server_error"}}`,
		"",
	}, "\n"))

	fixed, err := fixCursorStreamBundle(bundle, proxyRequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatOpenAIChat,
		ClientModel:  "cursor-model",
		CursorState:  &cursorCompatState{},
	})
	if err != nil {
		t.Fatalf("fixCursorStreamBundle failed: %v", err)
	}

	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, `"content":"hello"`) {
		t.Fatalf("expected partial content to survive before error, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"message":"boom"`) {
		t.Fatalf("expected structured error payload to pass through, got %s", fixedStr)
	}
	if strings.Contains(fixedStr, "[DONE]") {
		t.Fatalf("did not expect synthetic [DONE] after error, got %s", fixedStr)
	}
}

func TestFixCursorResponseBodyInjectsMessagesThinkingBlock(t *testing.T) {
	body := []byte(`{
		"id":"msg_1",
		"type":"message",
		"content":[{"type":"text","text":"hello"}],
		"reasoningContent":"think first"
	}`)

	fixed, err := fixCursorResponseBody(body, proxyRequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatClaude,
	})
	if err != nil {
		t.Fatalf("fixCursorResponseBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(fixed, &payload); err != nil {
		t.Fatalf("fixed body is not valid json: %v", err)
	}
	content := payload["content"].([]interface{})
	first := content[0].(map[string]interface{})
	if first["type"] != "thinking" || first["thinking"] != "think first" {
		t.Fatalf("expected injected thinking block, got %#v", first)
	}
}

func TestFixCursorStreamBundleSplitsThinkTagsForChat(t *testing.T) {
	bundle := []byte("data: {\"id\":\"cmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"upstream\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"<think>reason</think>Hello\"},\"finish_reason\":null}]}\n\n")
	fixed, err := fixCursorStreamBundle(bundle, proxyRequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatOpenAIChat,
		ClientModel:  "cursor-model",
		CursorState:  &cursorCompatState{},
	})
	if err != nil {
		t.Fatalf("fixCursorStreamBundle failed: %v", err)
	}
	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, `"reasoning_content":"reason"`) {
		t.Fatalf("expected reasoning_content split, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"content":"Hello"`) {
		t.Fatalf("expected content chunk preserved, got %s", fixedStr)
	}
}

func TestFixCursorStreamBundleStopsThinkingBeforeToolCalls(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"<think>reason"},"finish_reason":null}]}`,
		"",
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		"",
	}, "\n"))
	fixed, err := fixCursorStreamBundle(bundle, proxyRequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatOpenAIChat,
		ClientModel:  "cursor-model",
		CursorState:  &cursorCompatState{},
	})
	if err != nil {
		t.Fatalf("fixCursorStreamBundle failed: %v", err)
	}
	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, `"reasoning_content":"reason"`) {
		t.Fatalf("expected opening think content to become reasoning delta, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"tool_calls":[`) {
		t.Fatalf("expected tool_calls chunk preserved, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"content":"Hello"`) {
		t.Fatalf("expected post-tool text to stay normal content, got %s", fixedStr)
	}
	if strings.Contains(fixedStr, `"reasoning_content":"Hello"`) {
		t.Fatalf("did not expect post-tool text to remain in reasoning mode, got %s", fixedStr)
	}
}

func TestFixCursorStreamBundleInjectsMessagesThinkingEvents(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello","reasoningContent":"think"}}`,
		"",
	}, "\n"))
	fixed, err := fixCursorStreamBundle(bundle, proxyRequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatClaude,
		CursorState:  &cursorCompatState{},
	})
	if err != nil {
		t.Fatalf("fixCursorStreamBundle failed: %v", err)
	}
	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, "event: content_block_start") || !strings.Contains(fixedStr, `"thinking":"think"`) {
		t.Fatalf("expected injected thinking SSE events, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"index":1`) {
		t.Fatalf("expected original text block index offset, got %s", fixedStr)
	}
}

func TestFixCursorToolCallsNormalizesArguments(t *testing.T) {
	tempFile, err := os.CreateTemp(t.TempDir(), "cursor-tool-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	if _, err := tempFile.WriteString("hello \"world\""); err != nil {
		t.Fatalf("WriteString failed: %v", err)
	}
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	message := map[string]interface{}{
		"tool_calls": []interface{}{
			map[string]interface{}{
				"function": map[string]interface{}{
					"name":      "str_replace",
					"arguments": "{\"file_path\":\"" + tempFile.Name() + "\",\"old_string\":\"hello \\u201cworld\\u201d\",\"new_string\":\"bye\\u201d\"}",
				},
			},
		},
	}
	choice := map[string]interface{}{}
	fixCursorToolCalls(message, choice)

	toolCall := message["tool_calls"].([]interface{})[0].(map[string]interface{})
	functionData := toolCall["function"].(map[string]interface{})
	args := functionData["arguments"].(string)
	if !strings.Contains(args, `"path":"`) {
		t.Fatalf("expected file_path to normalize to path, got %s", args)
	}
	if !strings.Contains(args, `hello \"world\"`) {
		t.Fatalf("expected old_string to be repaired to exact file content, got %s", args)
	}
	if strings.Contains(args, "”") {
		t.Fatalf("expected smart quotes in new_string to be normalized, got %s", args)
	}
}

func TestWithCursorPathStrippedDelegatesToBaseHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost/cursor/v1/models?refresh=true", nil)
	rec := httptest.NewRecorder()

	calledPath := ""
	withCursorPathStripped(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
	})(rec, req)

	if calledPath != "/v1/models" {
		t.Fatalf("expected stripped path /v1/models, got %s", calledPath)
	}
}
