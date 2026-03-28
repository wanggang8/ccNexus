package proxy

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	newcursor "github.com/lich0821/ccNexus/internal/cursorbridge"
)

func TestCursorRequestSemanticMatrix(t *testing.T) {
	newcursor.SetDefaultThinkingCacheForTest(newcursor.NewThinkingCache())

	chatBody := `{
		"model":"gpt-5",
		"messages":[{"role":"user","content":"hi"}],
		"tools":[{"name":"read_file","description":"Read file"}],
		"tool_choice":{"type":"any"},
		"stream":true
	}`
	responsesBody := `{
		"model":"gpt-5",
		"messages":[{"role":"user","content":"hi"}],
		"tools":[{"name":"read_file","description":"Read file"}],
		"tool_choice":{"type":"any"},
		"stream":true
	}`
	messagesBody := `{
		"model":"claude-sonnet-4-20250514",
		"messages":[{"role":"user","content":"hi"}],
		"stream":true
	}`

	type matrixCase struct {
		name            string
		path            string
		body            string
		endpoint        config.Endpoint
		wantTransformer string
		wantTargetPath  string
		validate        func(*testing.T, proxyRequestMeta, map[string]interface{}, []byte)
	}

	tests := []matrixCase{
		{
			name:            "chat to openai",
			path:            "/cursor/v1/chat/completions",
			body:            chatBody,
			endpoint:        config.Endpoint{Name: "openai", Transformer: "openai", Model: "gpt-5"},
			wantTransformer: "cx_chat_openai",
			wantTargetPath:  "/v1/chat/completions",
			validate: func(t *testing.T, meta proxyRequestMeta, payload map[string]interface{}, _ []byte) {
				t.Helper()
				if _, ok := payload["messages"].([]interface{}); !ok {
					t.Fatalf("expected openai chat bridge payload to keep messages, got %#v", payload)
				}
				if _, ok := payload["input"]; ok {
					t.Fatalf("did not expect openai chat bridge payload to expose responses input, got %#v", payload["input"])
				}
				if payload["tool_choice"] != "required" {
					t.Fatalf("expected openai chat tool_choice normalization, got %#v", payload["tool_choice"])
				}
				tool := payload["tools"].([]interface{})[0].(map[string]interface{})
				if tool["type"] != "function" {
					t.Fatalf("expected openai chat tool normalized to function, got %#v", tool)
				}
				if meta.CacheMessages == nil || len(meta.CacheMessages) != 1 {
					t.Fatalf("expected cursor chat cache messages to be extracted, got %#v", meta.CacheMessages)
				}
			},
		},
		{
			name:            "chat to openai2",
			path:            "/cursor/v1/chat/completions",
			body:            chatBody,
			endpoint:        config.Endpoint{Name: "openai2", Transformer: "openai2", Model: "gpt-5"},
			wantTransformer: "cx_chat_openai2",
			wantTargetPath:  "/v1/responses",
			validate: func(t *testing.T, meta proxyRequestMeta, payload map[string]interface{}, _ []byte) {
				t.Helper()
				if _, ok := payload["input"]; !ok {
					t.Fatalf("expected chat -> openai2 payload to target responses input, got %#v", payload)
				}
				if _, ok := payload["messages"]; ok {
					t.Fatalf("did not expect chat -> openai2 payload to keep chat messages, got %#v", payload["messages"])
				}
				if meta.CacheMessages == nil || len(meta.CacheMessages) != 1 {
					t.Fatalf("expected chat cache extraction before backend routing, got %#v", meta.CacheMessages)
				}
			},
		},
		{
			name:            "chat to claude",
			path:            "/cursor/v1/chat/completions",
			body:            chatBody,
			endpoint:        config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			wantTransformer: "cx_chat_claude",
			wantTargetPath:  "/v1/messages",
			validate: func(t *testing.T, _ proxyRequestMeta, payload map[string]interface{}, _ []byte) {
				t.Helper()
				if payload["max_tokens"] != float64(8192) {
					t.Fatalf("expected chat -> claude max_tokens floor, got %#v", payload["max_tokens"])
				}
				messages, ok := payload["messages"].([]interface{})
				if !ok || len(messages) == 0 {
					t.Fatalf("expected claude messages payload, got %#v", payload["messages"])
				}
				content := messages[0].(map[string]interface{})["content"].([]interface{})
				if content[0].(map[string]interface{})["type"] != "text" {
					t.Fatalf("expected claude content blocks, got %#v", content[0])
				}
				tool := payload["tools"].([]interface{})[0].(map[string]interface{})
				if _, ok := tool["input_schema"].(map[string]interface{}); !ok {
					t.Fatalf("expected claude tool input_schema, got %#v", tool)
				}
			},
		},
		{
			name:            "chat to gemini",
			path:            "/cursor/v1/chat/completions",
			body:            chatBody,
			endpoint:        config.Endpoint{Name: "gemini", Transformer: "gemini", Model: "gemini-2.5-pro"},
			wantTransformer: "cx_chat_gemini",
			wantTargetPath:  "/v1/models/gemini-2.5-pro:streamGenerateContent?alt=sse",
			validate: func(t *testing.T, _ proxyRequestMeta, payload map[string]interface{}, _ []byte) {
				t.Helper()
				if _, ok := payload["contents"].([]interface{}); !ok {
					t.Fatalf("expected gemini contents payload, got %#v", payload)
				}
			},
		},
		{
			name:            "responses to openai",
			path:            "/cursor/v1/responses",
			body:            responsesBody,
			endpoint:        config.Endpoint{Name: "openai", Transformer: "openai", Model: "gpt-5"},
			wantTransformer: "cx_resp_openai",
			wantTargetPath:  "/v1/chat/completions",
			validate: func(t *testing.T, _ proxyRequestMeta, payload map[string]interface{}, _ []byte) {
				t.Helper()
				if _, ok := payload["messages"].([]interface{}); !ok {
					t.Fatalf("expected responses -> openai bridge payload to expose chat messages, got %#v", payload)
				}
				if _, ok := payload["input"]; ok {
					t.Fatalf("did not expect responses -> openai bridge payload to keep responses input, got %#v", payload["input"])
				}
				if payload["tool_choice"] != "required" {
					t.Fatalf("expected responses -> openai tool_choice normalization, got %#v", payload["tool_choice"])
				}
				tool := payload["tools"].([]interface{})[0].(map[string]interface{})
				if tool["type"] != "function" {
					t.Fatalf("expected responses -> openai tool normalized to function, got %#v", tool)
				}
			},
		},
		{
			name:            "responses to openai2",
			path:            "/cursor/v1/responses",
			body:            responsesBody,
			endpoint:        config.Endpoint{Name: "openai2", Transformer: "openai2", Model: "gpt-5"},
			wantTransformer: "cx_resp_openai2",
			wantTargetPath:  "/v1/responses",
			validate: func(t *testing.T, _ proxyRequestMeta, payload map[string]interface{}, _ []byte) {
				t.Helper()
				if _, ok := payload["input"]; !ok {
					t.Fatalf("expected native responses payload to keep input, got %#v", payload)
				}
			},
		},
		{
			name:            "responses to claude",
			path:            "/cursor/v1/responses",
			body:            responsesBody,
			endpoint:        config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			wantTransformer: "cx_resp_claude",
			wantTargetPath:  "/v1/messages",
			validate: func(t *testing.T, _ proxyRequestMeta, payload map[string]interface{}, _ []byte) {
				t.Helper()
				messages, ok := payload["messages"].([]interface{})
				if !ok || len(messages) == 0 {
					t.Fatalf("expected responses -> claude messages payload, got %#v", payload["messages"])
				}
				content := messages[0].(map[string]interface{})["content"].([]interface{})
				if content[0].(map[string]interface{})["type"] != "text" {
					t.Fatalf("expected claude content blocks, got %#v", content[0])
				}
				tool := payload["tools"].([]interface{})[0].(map[string]interface{})
				if _, ok := tool["input_schema"].(map[string]interface{}); !ok {
					t.Fatalf("expected responses -> claude tool input_schema, got %#v", tool)
				}
			},
		},
		{
			name:            "responses to gemini",
			path:            "/cursor/v1/responses",
			body:            responsesBody,
			endpoint:        config.Endpoint{Name: "gemini", Transformer: "gemini", Model: "gemini-2.5-pro"},
			wantTransformer: "cx_resp_gemini",
			wantTargetPath:  "/v1/models/gemini-2.5-pro:streamGenerateContent?alt=sse",
			validate: func(t *testing.T, _ proxyRequestMeta, payload map[string]interface{}, _ []byte) {
				t.Helper()
				if _, ok := payload["contents"].([]interface{}); !ok {
					t.Fatalf("expected responses -> gemini contents payload, got %#v", payload)
				}
			},
		},
		{
			name:            "messages to claude",
			path:            "/cursor/v1/messages",
			body:            messagesBody,
			endpoint:        config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			wantTransformer: "cc_claude",
			wantTargetPath:  "/v1/messages",
			validate: func(t *testing.T, _ proxyRequestMeta, payload map[string]interface{}, transformedBody []byte) {
				t.Helper()
				if _, ok := payload["messages"].([]interface{}); !ok {
					t.Fatalf("expected cursor messages payload to stay anthropic-shaped, got %#v", payload)
				}
				if strings.Contains(string(transformedBody), "cache_control") {
					t.Fatalf("did not expect cursor messages passthrough to inject cache_control, got %s", string(transformedBody))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://localhost"+tt.path, strings.NewReader(""))
			_, normalizedBody, meta, err := prepareProxyRequest(req, []byte(tt.body))
			if err != nil {
				t.Fatalf("prepareProxyRequest failed: %v", err)
			}
			if !meta.CursorMode {
				t.Fatalf("expected cursor mode for %s", tt.path)
			}

			trans, err := prepareTransformerForClient(meta.ClientFormat, tt.endpoint)
			if err != nil {
				t.Fatalf("prepareTransformerForClient failed: %v", err)
			}
			if trans.Name() != tt.wantTransformer {
				t.Fatalf("expected transformer %s, got %s", tt.wantTransformer, trans.Name())
			}
			if err := newcursor.ValidateTransformer(meta.cursorRequestMeta(), trans.Name()); err != nil {
				t.Fatalf("expected transformer %s to be cursor-compatible: %v", trans.Name(), err)
			}

			transformedBody, err := trans.TransformRequest(normalizedBody)
			if err != nil {
				t.Fatalf("TransformRequest failed: %v", err)
			}

			meta.TransformerName = trans.Name()
			transformedBody, err = applyCursorTransformedRequestCompat(transformedBody, &meta, trans.Name())
			if err != nil {
				t.Fatalf("applyCursorTransformedRequestCompat failed: %v", err)
			}

			targetPath, ok := newcursor.ResolveTargetPath(meta.cursorRequestMeta(), trans.Name(), tt.endpoint.Model, transformedBody)
			if !ok {
				t.Fatalf("expected target path resolution for transformer %s", trans.Name())
			}
			if targetPath != tt.wantTargetPath {
				t.Fatalf("expected target path %s, got %s", tt.wantTargetPath, targetPath)
			}

			var payload map[string]interface{}
			if err := json.Unmarshal(transformedBody, &payload); err != nil {
				t.Fatalf("transformed body is not valid json: %v", err)
			}
			tt.validate(t, meta, payload, transformedBody)
		})
	}
}

func TestCursorChatToOpenAIStripsResponsesOnlyPreviousResponseID(t *testing.T) {
	newcursor.SetDefaultThinkingCacheForTest(newcursor.NewThinkingCache())

	req := httptest.NewRequest("POST", "http://localhost/cursor/v1/chat/completions", strings.NewReader(""))
	body := []byte(`{
		"model":"gpt-5",
		"previous_response_id":"resp_123",
		"messages":[{"role":"user","content":"hi"}]
	}`)

	_, normalizedBody, meta, err := prepareProxyRequest(req, body)
	if err != nil {
		t.Fatalf("prepareProxyRequest failed: %v", err)
	}

	endpoint := config.Endpoint{Name: "openai", Transformer: "openai", Model: "gpt-5"}
	trans, err := prepareTransformerForClient(meta.ClientFormat, endpoint)
	if err != nil {
		t.Fatalf("prepareTransformerForClient failed: %v", err)
	}

	transformedBody, err := trans.TransformRequest(normalizedBody)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	meta.TransformerName = trans.Name()
	transformedBody, err = applyCursorTransformedRequestCompat(transformedBody, &meta, trans.Name())
	if err != nil {
		t.Fatalf("applyCursorTransformedRequestCompat failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(transformedBody, &payload); err != nil {
		t.Fatalf("transformed body is not valid json: %v", err)
	}
	if _, ok := payload["previous_response_id"]; ok {
		t.Fatalf("expected openai chat cursor payload to strip previous_response_id, got %#v", payload["previous_response_id"])
	}
}

func TestCursorMessagesBackendCompatibilityMatrix(t *testing.T) {
	meta := newcursor.RequestMeta{
		CursorMode:   true,
		ClientFormat: ClientFormatClaude,
	}

	if err := newcursor.ValidateEndpointTransformer(meta, "claude"); err != nil {
		t.Fatalf("expected claude endpoint transformer to be allowed for cursor messages, got %v", err)
	}
	for _, transformer := range []string{"openai", "openai2", "gemini"} {
		if err := newcursor.ValidateEndpointTransformer(meta, transformer); err == nil {
			t.Fatalf("expected cursor messages to reject endpoint transformer %s", transformer)
		}
	}
}

func TestCursorResponsesStreamShapeMatrix(t *testing.T) {
	tests := []struct {
		name           string
		transformer    string
		wantUnwrapped  bool
		wantNativeType bool
	}{
		{name: "responses openai bridge", transformer: "cx_resp_openai", wantUnwrapped: true},
		{name: "responses claude bridge", transformer: "cx_resp_claude", wantUnwrapped: true},
		{name: "responses gemini bridge", transformer: "cx_resp_gemini", wantUnwrapped: true},
		{name: "responses openai2 native", transformer: "cx_resp_openai2", wantNativeType: true},
	}

	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"hello"}`,
		"",
		`data: {"type":"response.content_part.done","output_index":1,"content_index":0,"part":{"type":"output_text"}}`,
		"",
	}, "\n"))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixed, err := fixCursorStreamBundle(bundle, proxyRequestMeta{
				RequestMeta: newcursor.RequestMeta{
					CursorMode:   true,
					ClientFormat: ClientFormatOpenAIResponses,
					ClientModel:  "cursor-model",
				},
				TransformerName: tt.transformer,
				CursorState: &newcursor.StreamFinalizeState{
					ResponsesTools:  make(map[int]*newcursor.ResponseToolState),
					ResponsesOutput: make([]map[string]interface{}, 0),
				},
			})
			if err != nil {
				t.Fatalf("fixCursorStreamBundle failed: %v", err)
			}

			fixedStr := string(fixed)
			if tt.wantUnwrapped {
				if !strings.Contains(fixedStr, `"type":"output_text"`) {
					t.Fatalf("expected transformed responses bridge to emit output_text payload, got %s", fixedStr)
				}
				if strings.Contains(fixedStr, `"type":"response.output_text.delta"`) {
					t.Fatalf("did not expect transformed responses bridge to keep native response.output_text.delta type, got %s", fixedStr)
				}
				if strings.Contains(fixedStr, `"output_index":`) || strings.Contains(fixedStr, `"content_index":`) {
					t.Fatalf("did not expect transformed responses bridge context fields, got %s", fixedStr)
				}
				if strings.Contains(fixedStr, `event: response.content_part.done`) {
					t.Fatalf("did not expect transformed responses bridge content_part.done, got %s", fixedStr)
				}
			}
			if tt.wantNativeType {
				if !strings.Contains(fixedStr, `"type":"response.output_text.delta"`) {
					t.Fatalf("expected native responses stream to preserve response.output_text.delta type, got %s", fixedStr)
				}
				if !strings.Contains(fixedStr, `"output_index":1`) || !strings.Contains(fixedStr, `"content_index":0`) {
					t.Fatalf("expected native responses stream to preserve context fields, got %s", fixedStr)
				}
				if !strings.Contains(fixedStr, `"type":"response.content_part.done"`) {
					t.Fatalf("expected native responses stream to preserve content_part.done, got %s", fixedStr)
				}
			}
		})
	}
}
