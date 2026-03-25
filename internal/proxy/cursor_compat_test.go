package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	if !strings.Contains(fixedStr, `"type":"response.output_text.delta"`) {
		t.Fatalf("expected delta payload to be preserved for text extraction, got %s", fixedStr)
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
