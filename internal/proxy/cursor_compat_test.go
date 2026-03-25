package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
