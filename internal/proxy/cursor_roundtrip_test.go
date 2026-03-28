package proxy

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	newcursor "github.com/lich0821/ccNexus/internal/cursorbridge"
	"github.com/lich0821/ccNexus/internal/transformer"
)

type preparedCursorRoundTrip struct {
	meta            proxyRequestMeta
	trans           transformer.Transformer
	targetPath      string
	requestPayload  map[string]interface{}
	requestBody     []byte
	normalizedBody  []byte
	originalRequest string
}

func prepareCursorRoundTrip(t *testing.T, path string, requestBody string, endpoint config.Endpoint) preparedCursorRoundTrip {
	t.Helper()

	req := httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader(""))
	_, normalizedBody, meta, err := prepareProxyRequest(req, []byte(requestBody))
	if err != nil {
		t.Fatalf("prepareProxyRequest failed: %v", err)
	}
	if !meta.CursorMode {
		t.Fatalf("expected cursor mode for %s", path)
	}

	trans, err := prepareTransformerForClient(meta.ClientFormat, endpoint)
	if err != nil {
		t.Fatalf("prepareTransformerForClient failed: %v", err)
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

	targetPath, ok := newcursor.ResolveTargetPath(meta.cursorRequestMeta(), trans.Name(), endpoint.Model, transformedBody)
	if !ok {
		t.Fatalf("expected target path resolution for transformer %s", trans.Name())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(transformedBody, &payload); err != nil {
		t.Fatalf("transformed request body is not valid json: %v", err)
	}

	return preparedCursorRoundTrip{
		meta:            meta,
		trans:           trans,
		targetPath:      targetPath,
		requestPayload:  payload,
		requestBody:     transformedBody,
		normalizedBody:  normalizedBody,
		originalRequest: requestBody,
	}
}

func runCursorRoundTripNonStream(t *testing.T, prepared preparedCursorRoundTrip, upstreamBody string) (map[string]interface{}, []byte) {
	t.Helper()

	rawUpstream := []byte(upstreamBody)
	transformInput := rawUpstream
	var err error

	if prepared.meta.ClientFormat == ClientFormatOpenAIResponses && prepared.meta.TransformerName == "cx_resp_openai" {
		transformInput, err = newcursor.FixOpenAIUpstreamChatBody(rawUpstream)
		if err != nil {
			t.Fatalf("FixOpenAIUpstreamChatBody failed: %v", err)
		}
	}

	transformedResp, err := prepared.trans.TransformResponse(transformInput, false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	transformedResp, err = newcursor.FixRawUpstreamResponseBody(
		prepared.meta.cursorRequestMeta(),
		prepared.meta.TransformerName,
		rawUpstream,
		transformedResp,
	)
	if err != nil {
		t.Fatalf("FixRawUpstreamResponseBody failed: %v", err)
	}
	transformedResp, err = fixCursorResponseBody(transformedResp, prepared.meta)
	if err != nil {
		t.Fatalf("fixCursorResponseBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(transformedResp, &payload); err != nil {
		t.Fatalf("transformed response body is not valid json: %v", err)
	}
	return payload, transformedResp
}

func runCursorRoundTripStream(t *testing.T, prepared preparedCursorRoundTrip, upstreamEvents ...string) string {
	t.Helper()

	ctx := transformer.NewStreamContext()
	var output strings.Builder

	for _, event := range upstreamEvents {
		if strings.TrimSpace(event) == "" {
			continue
		}
		transformed, err := newcursor.TransformCursorUpstreamStreamEvent(
			prepared.meta.cursorRequestMeta(),
			[]byte(event),
			prepared.meta.TransformerName,
			prepared.meta.ClientModel,
			prepared.meta.CursorState,
			func(b []byte) ([]byte, error) {
				return prepared.trans.TransformResponseWithContext(b, true, ctx)
			},
		)
		if err != nil {
			t.Fatalf("TransformCursorUpstreamStreamEvent failed: %v", err)
		}
		fixed, err := fixCursorStreamBundle(transformed, prepared.meta)
		if err != nil {
			t.Fatalf("fixCursorStreamBundle failed: %v", err)
		}
		output.Write(fixed)
	}

	return output.String()
}

func TestCursorNonStreamingRoundTripMatrix(t *testing.T) {
	newcursor.SetDefaultThinkingCacheForTest(newcursor.NewThinkingCache())

	const chatRequest = `{
		"model":"gpt-5",
		"messages":[{"role":"user","content":"hi"}],
		"tools":[{"name":"read_file","description":"Read file"}],
		"tool_choice":{"type":"any"},
		"stream":false
	}`
	const responsesRequest = `{
		"model":"gpt-5",
		"messages":[{"role":"user","content":"hi"}],
		"tools":[{"name":"read_file","description":"Read file"}],
		"tool_choice":{"type":"any"},
		"stream":false
	}`
	const messagesRequest = `{
		"model":"claude-sonnet-4-20250514",
		"messages":[{"role":"user","content":"hi"}],
		"stream":false
	}`
	const chatOpenAI2Request = `{
		"model":"gpt-5",
		"messages":[
			{"role":"assistant","content":"Working on it","tool_calls":[{"id":"call_raw","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}}]},
			{"role":"tool","tool_call_id":"call_raw","content":"ok"}
		],
		"stream":false
	}`
	const chatClaudeRequest = `{
		"model":"gpt-5",
		"messages":[
			{"role":"assistant","content":"","tool_calls":[{"id":"call_raw","type":"function","function":{"name":"read_file","arguments":""}}]}
		],
		"stream":false
	}`

	type roundTripCase struct {
		name            string
		path            string
		requestBody     string
		endpoint        config.Endpoint
		wantTransformer string
		wantTargetPath  string
		upstreamBody    string
		validateRequest func(*testing.T, preparedCursorRoundTrip)
		validateReply   func(*testing.T, map[string]interface{}, []byte)
	}

	tests := []roundTripCase{
		{
			name:            "chat openai",
			path:            "/cursor/v1/chat/completions",
			requestBody:     chatRequest,
			endpoint:        config.Endpoint{Name: "openai", Transformer: "openai", Model: "gpt-5"},
			wantTransformer: "cx_chat_openai",
			wantTargetPath:  "/v1/chat/completions",
			upstreamBody: `{
				"id":"chatcmpl_1",
				"object":"chat.completion",
				"model":"upstream-model",
				"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
			}`,
			validateRequest: func(t *testing.T, prepared preparedCursorRoundTrip) {
				t.Helper()
				if prepared.requestPayload["tool_choice"] != "required" {
					t.Fatalf("expected chat->openai tool_choice normalization, got %#v", prepared.requestPayload["tool_choice"])
				}
			},
			validateReply: func(t *testing.T, payload map[string]interface{}, _ []byte) {
				t.Helper()
				if payload["model"] != "gpt-5" {
					t.Fatalf("expected model rewrite back to cursor client model, got %#v", payload["model"])
				}
				message := payload["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
				if message["content"] != "hello" {
					t.Fatalf("expected assistant content to round-trip, got %#v", message["content"])
				}
			},
		},
		{
			name:            "chat openai2",
			path:            "/cursor/v1/chat/completions",
			requestBody:     chatOpenAI2Request,
			endpoint:        config.Endpoint{Name: "openai2", Transformer: "openai2", Model: "gpt-5"},
			wantTransformer: "cx_chat_openai2",
			wantTargetPath:  "/v1/responses",
			upstreamBody: `{
				"id":"resp_1",
				"object":"response",
				"model":"upstream-model",
				"output":[
					{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello"}]}
				],
				"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}
			}`,
			validateRequest: func(t *testing.T, prepared preparedCursorRoundTrip) {
				t.Helper()
				input := prepared.requestPayload["input"].([]interface{})
				first := input[0].(map[string]interface{})
				if first["type"] != "message" {
					t.Fatalf("expected assistant text before tool call to stay structured, got %#v", first)
				}
				second := input[1].(map[string]interface{})
				if second["type"] != "function_call" {
					t.Fatalf("expected function_call item after assistant message, got %#v", second)
				}
			},
			validateReply: func(t *testing.T, payload map[string]interface{}, _ []byte) {
				t.Helper()
				message := payload["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
				if message["content"] != "hello" {
					t.Fatalf("expected responses->chat bridge text, got %#v", message["content"])
				}
			},
		},
		{
			name:            "chat claude",
			path:            "/cursor/v1/chat/completions",
			requestBody:     chatClaudeRequest,
			endpoint:        config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			wantTransformer: "cx_chat_claude",
			wantTargetPath:  "/v1/messages",
			upstreamBody: `{
				"id":"msg_1",
				"type":"message",
				"role":"assistant",
				"model":"claude-sonnet-4-20250514",
				"content":[{"type":"text","text":"hello"}],
				"stop_reason":"end_turn",
				"usage":{"input_tokens":3,"output_tokens":2}
			}`,
			validateRequest: func(t *testing.T, prepared preparedCursorRoundTrip) {
				t.Helper()
				messages := prepared.requestPayload["messages"].([]interface{})
				content := messages[0].(map[string]interface{})["content"].([]interface{})
				toolUse := content[0].(map[string]interface{})
				input, ok := toolUse["input"].(map[string]interface{})
				if !ok || len(input) != 0 {
					t.Fatalf("expected nil tool_use input to be repaired into empty object, got %#v", toolUse["input"])
				}
			},
			validateReply: func(t *testing.T, payload map[string]interface{}, _ []byte) {
				t.Helper()
				if payload["model"] != "gpt-5" {
					t.Fatalf("expected cursor client model on chat response, got %#v", payload["model"])
				}
				message := payload["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
				if message["content"] != "hello" {
					t.Fatalf("expected claude text to round-trip into chat completion, got %#v", message["content"])
				}
			},
		},
		{
			name:            "chat gemini",
			path:            "/cursor/v1/chat/completions",
			requestBody:     chatRequest,
			endpoint:        config.Endpoint{Name: "gemini", Transformer: "gemini", Model: "gemini-2.5-pro"},
			wantTransformer: "cx_chat_gemini",
			wantTargetPath:  "/v1/models/gemini-2.5-pro:generateContent",
			upstreamBody: `{
				"candidates":[
					{"content":{"parts":[
						{"functionCall":{"id":"gem_call_1","name":"read_file","args":{"path":"README.md"}}}
					]}}
				],
				"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}
			}`,
			validateRequest: func(t *testing.T, prepared preparedCursorRoundTrip) {
				t.Helper()
				if _, ok := prepared.requestPayload["contents"].([]interface{}); !ok {
					t.Fatalf("expected gemini request contents payload, got %#v", prepared.requestPayload)
				}
			},
			validateReply: func(t *testing.T, payload map[string]interface{}, _ []byte) {
				t.Helper()
				toolCalls := payload["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["tool_calls"].([]interface{})
				if toolCalls[0].(map[string]interface{})["id"] != "gem_call_1" {
					t.Fatalf("expected raw gemini tool id to survive round-trip, got %#v", toolCalls[0])
				}
			},
		},
		{
			name:            "responses openai",
			path:            "/cursor/v1/responses",
			requestBody:     responsesRequest,
			endpoint:        config.Endpoint{Name: "openai", Transformer: "openai", Model: "gpt-5"},
			wantTransformer: "cx_resp_openai",
			wantTargetPath:  "/v1/chat/completions",
			upstreamBody: `{
				"id":"chatcmpl_1",
				"object":"chat.completion",
				"model":"upstream-model",
				"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
			}`,
			validateRequest: func(t *testing.T, prepared preparedCursorRoundTrip) {
				t.Helper()
				if _, ok := prepared.requestPayload["messages"].([]interface{}); !ok {
					t.Fatalf("expected responses->openai bridge request to expose chat messages, got %#v", prepared.requestPayload)
				}
			},
			validateReply: func(t *testing.T, payload map[string]interface{}, _ []byte) {
				t.Helper()
				output := payload["output"].([]interface{})
				item := output[0].(map[string]interface{})
				if item["type"] != "message" {
					t.Fatalf("expected responses output message, got %#v", item)
				}
				content := item["content"].([]interface{})
				if content[0].(map[string]interface{})["text"] != "hello" {
					t.Fatalf("expected bridged output text hello, got %#v", content[0])
				}
			},
		},
		{
			name:            "responses openai2",
			path:            "/cursor/v1/responses",
			requestBody:     responsesRequest,
			endpoint:        config.Endpoint{Name: "openai2", Transformer: "openai2", Model: "gpt-5"},
			wantTransformer: "cx_resp_openai2",
			wantTargetPath:  "/v1/responses",
			upstreamBody: `{
				"id":"resp_1",
				"object":"response",
				"model":"upstream-model",
				"output":[
					{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello"}]}
				],
				"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}
			}`,
			validateRequest: func(t *testing.T, prepared preparedCursorRoundTrip) {
				t.Helper()
				if _, ok := prepared.requestPayload["input"].([]interface{}); !ok {
					t.Fatalf("expected native responses request to keep input, got %#v", prepared.requestPayload)
				}
			},
			validateReply: func(t *testing.T, payload map[string]interface{}, _ []byte) {
				t.Helper()
				if payload["object"] != "response" {
					t.Fatalf("expected native responses object, got %#v", payload["object"])
				}
				if payload["model"] != "gpt-5" {
					t.Fatalf("expected model rewrite back to cursor model, got %#v", payload["model"])
				}
			},
		},
		{
			name:            "responses claude",
			path:            "/cursor/v1/responses",
			requestBody:     responsesRequest,
			endpoint:        config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			wantTransformer: "cx_resp_claude",
			wantTargetPath:  "/v1/messages",
			upstreamBody: `{
				"id":"msg_1",
				"type":"message",
				"role":"assistant",
				"model":"claude-sonnet-4-20250514",
				"content":[{"type":"tool_use","id":"toolu_1","name":"read_file","input":{"file_path":"README.md"}}],
				"stop_reason":"tool_use",
				"usage":{"input_tokens":3,"output_tokens":2}
			}`,
			validateRequest: func(t *testing.T, prepared preparedCursorRoundTrip) {
				t.Helper()
				if _, ok := prepared.requestPayload["messages"].([]interface{}); !ok {
					t.Fatalf("expected responses->claude request to target messages, got %#v", prepared.requestPayload)
				}
			},
			validateReply: func(t *testing.T, payload map[string]interface{}, _ []byte) {
				t.Helper()
				output := payload["output"].([]interface{})
				functionCall := output[0].(map[string]interface{})
				args := functionCall["arguments"].(string)
				if strings.Contains(args, "file_path") || !strings.Contains(args, `"path":"README.md"`) {
					t.Fatalf("expected cursor-only Claude arg normalization, got %s", args)
				}
			},
		},
		{
			name:            "responses gemini",
			path:            "/cursor/v1/responses",
			requestBody:     responsesRequest,
			endpoint:        config.Endpoint{Name: "gemini", Transformer: "gemini", Model: "gemini-2.5-pro"},
			wantTransformer: "cx_resp_gemini",
			wantTargetPath:  "/v1/models/gemini-2.5-pro:generateContent",
			upstreamBody: `{
				"candidates":[
					{"content":{"parts":[
						{"functionCall":{"id":"gem_call_1","name":"read_file","args":{"path":"README.md"}}}
					]}}
				],
				"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}
			}`,
			validateRequest: func(t *testing.T, prepared preparedCursorRoundTrip) {
				t.Helper()
				if _, ok := prepared.requestPayload["contents"].([]interface{}); !ok {
					t.Fatalf("expected gemini responses request contents payload, got %#v", prepared.requestPayload)
				}
			},
			validateReply: func(t *testing.T, payload map[string]interface{}, _ []byte) {
				t.Helper()
				output := payload["output"].([]interface{})
				functionCall := output[0].(map[string]interface{})
				if functionCall["call_id"] != "gem_call_1" {
					t.Fatalf("expected raw gemini call_id to survive round-trip, got %#v", functionCall)
				}
			},
		},
		{
			name:            "messages claude",
			path:            "/cursor/v1/messages",
			requestBody:     messagesRequest,
			endpoint:        config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			wantTransformer: "cc_claude",
			wantTargetPath:  "/v1/messages",
			upstreamBody: `{
				"id":"msg_1",
				"type":"message",
				"role":"assistant",
				"content":[{"type":"text","text":"hello"}],
				"reasoning_content":"think first"
			}`,
			validateRequest: func(t *testing.T, prepared preparedCursorRoundTrip) {
				t.Helper()
				if strings.Contains(string(prepared.requestBody), "cache_control") {
					t.Fatalf("did not expect /cursor/v1/messages passthrough request to inject cache_control, got %s", string(prepared.requestBody))
				}
			},
			validateReply: func(t *testing.T, payload map[string]interface{}, _ []byte) {
				t.Helper()
				content := payload["content"].([]interface{})
				first := content[0].(map[string]interface{})
				if first["type"] != "thinking" || first["thinking"] != "think first" {
					t.Fatalf("expected /cursor/v1/messages response to inject thinking block, got %#v", first)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepared := prepareCursorRoundTrip(t, tt.path, tt.requestBody, tt.endpoint)
			if prepared.trans.Name() != tt.wantTransformer {
				t.Fatalf("expected transformer %s, got %s", tt.wantTransformer, prepared.trans.Name())
			}
			if prepared.targetPath != tt.wantTargetPath {
				t.Fatalf("expected target path %s, got %s", tt.wantTargetPath, prepared.targetPath)
			}
			tt.validateRequest(t, prepared)

			payload, transformedResp := runCursorRoundTripNonStream(t, prepared, tt.upstreamBody)
			tt.validateReply(t, payload, transformedResp)
		})
	}
}

func TestCursorStreamingRoundTripMatrix(t *testing.T) {
	newcursor.SetDefaultThinkingCacheForTest(newcursor.NewThinkingCache())

	const chatRequest = `{
		"model":"gpt-5",
		"messages":[{"role":"user","content":"hi"}],
		"stream":true
	}`
	const responsesRequest = `{
		"model":"gpt-5",
		"messages":[{"role":"user","content":"hi"}],
		"stream":true
	}`
	const messagesRequest = `{
		"model":"claude-sonnet-4-20250514",
		"messages":[{"role":"user","content":"hi"}],
		"stream":true
	}`

	type streamCase struct {
		name            string
		path            string
		requestBody     string
		endpoint        config.Endpoint
		wantTransformer string
		wantTargetPath  string
		upstreamEvents  []string
		validateFinal   func(*testing.T, string)
	}

	tests := []streamCase{
		{
			name:            "chat openai",
			path:            "/cursor/v1/chat/completions",
			requestBody:     chatRequest,
			endpoint:        config.Endpoint{Name: "openai", Transformer: "openai", Model: "gpt-5"},
			wantTransformer: "cx_chat_openai",
			wantTargetPath:  "/v1/chat/completions",
			upstreamEvents: []string{
				"data: {\"id\":\"cmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"upstream-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n",
			},
			validateFinal: func(t *testing.T, final string) {
				t.Helper()
				if !strings.Contains(final, `"content":"hello"`) {
					t.Fatalf("expected chat delta text, got %s", final)
				}
			},
		},
		{
			name:            "chat openai2",
			path:            "/cursor/v1/chat/completions",
			requestBody:     chatRequest,
			endpoint:        config.Endpoint{Name: "openai2", Transformer: "openai2", Model: "gpt-5"},
			wantTransformer: "cx_chat_openai2",
			wantTargetPath:  "/v1/responses",
			upstreamEvents: []string{
				"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"model\":\"upstream-model\",\"status\":\"in_progress\",\"output\":[]}}\n\n",
				"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"in_progress\",\"role\":\"assistant\",\"content\":[]}}\n\n",
				"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"hello\"}\n\n",
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"model\":\"upstream-model\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":2,\"total_tokens\":5}}}\n\n",
				"data: [DONE]\n\n",
			},
			validateFinal: func(t *testing.T, final string) {
				t.Helper()
				if !strings.Contains(final, `"content":"hello"`) {
					t.Fatalf("expected responses->chat bridge delta, got %s", final)
				}
				if !strings.Contains(final, "data: [DONE]") {
					t.Fatalf("expected bridged chat stream to keep [DONE], got %s", final)
				}
			},
		},
		{
			name:            "chat claude",
			path:            "/cursor/v1/chat/completions",
			requestBody:     chatRequest,
			endpoint:        config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			wantTransformer: "cx_chat_claude",
			wantTargetPath:  "/v1/messages",
			upstreamEvents: []string{
				"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n",
			},
			validateFinal: func(t *testing.T, final string) {
				t.Helper()
				if !strings.Contains(final, `"content":"hello"`) {
					t.Fatalf("expected claude text delta to become chat delta, got %s", final)
				}
			},
		},
		{
			name:            "chat gemini",
			path:            "/cursor/v1/chat/completions",
			requestBody:     chatRequest,
			endpoint:        config.Endpoint{Name: "gemini", Transformer: "gemini", Model: "gemini-2.5-pro"},
			wantTransformer: "cx_chat_gemini",
			wantTargetPath:  "/v1/models/gemini-2.5-pro:streamGenerateContent?alt=sse",
			upstreamEvents: []string{
				"data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"gem_call_1\",\"name\":\"read_file\",\"args\":{\"path\":\"README.md\"}}}]}}]}\n\n",
			},
			validateFinal: func(t *testing.T, final string) {
				t.Helper()
				if !strings.Contains(final, `"id":"gem_call_1"`) {
					t.Fatalf("expected raw gemini tool id in final chat stream, got %s", final)
				}
			},
		},
		{
			name:            "responses openai",
			path:            "/cursor/v1/responses",
			requestBody:     responsesRequest,
			endpoint:        config.Endpoint{Name: "openai", Transformer: "openai", Model: "gpt-5"},
			wantTransformer: "cx_resp_openai",
			wantTargetPath:  "/v1/chat/completions",
			upstreamEvents: []string{
				"data: {\"id\":\"cmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"upstream-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n",
			},
			validateFinal: func(t *testing.T, final string) {
				t.Helper()
				if !strings.Contains(final, `event: response.output_text.delta`) {
					t.Fatalf("expected bridged responses event name, got %s", final)
				}
				if !strings.Contains(final, `"type":"output_text"`) || !strings.Contains(final, `"delta":"hello"`) {
					t.Fatalf("expected bridged responses delta payload, got %s", final)
				}
				if strings.Contains(final, `"output_index":`) {
					t.Fatalf("did not expect bridge-only context fields after compat, got %s", final)
				}
			},
		},
		{
			name:            "responses openai2",
			path:            "/cursor/v1/responses",
			requestBody:     responsesRequest,
			endpoint:        config.Endpoint{Name: "openai2", Transformer: "openai2", Model: "gpt-5"},
			wantTransformer: "cx_resp_openai2",
			wantTargetPath:  "/v1/responses",
			upstreamEvents: []string{
				"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"status\":\"in_progress\",\"model\":\"upstream-model\",\"output\":[]}}\n\n",
				"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"hello\"}\n\n",
			},
			validateFinal: func(t *testing.T, final string) {
				t.Helper()
				if !strings.Contains(final, `"type":"response.output_text.delta"`) {
					t.Fatalf("expected native responses type to be preserved, got %s", final)
				}
				if !strings.Contains(final, `"output_index":0`) || !strings.Contains(final, `"content_index":0`) {
					t.Fatalf("expected native responses context fields to survive, got %s", final)
				}
			},
		},
		{
			name:            "responses claude",
			path:            "/cursor/v1/responses",
			requestBody:     responsesRequest,
			endpoint:        config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			wantTransformer: "cx_resp_claude",
			wantTargetPath:  "/v1/messages",
			upstreamEvents: []string{
				"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-20250514\",\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n",
			},
			validateFinal: func(t *testing.T, final string) {
				t.Helper()
				if !strings.Contains(final, `event: response.output_text.delta`) {
					t.Fatalf("expected claude bridge to responses output_text event, got %s", final)
				}
				if !strings.Contains(final, `"type":"output_text"`) || !strings.Contains(final, `"delta":"hello"`) {
					t.Fatalf("expected claude bridge output_text payload, got %s", final)
				}
			},
		},
		{
			name:            "responses gemini",
			path:            "/cursor/v1/responses",
			requestBody:     responsesRequest,
			endpoint:        config.Endpoint{Name: "gemini", Transformer: "gemini", Model: "gemini-2.5-pro"},
			wantTransformer: "cx_resp_gemini",
			wantTargetPath:  "/v1/models/gemini-2.5-pro:streamGenerateContent?alt=sse",
			upstreamEvents: []string{
				"data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"gem_call_1\",\"name\":\"read_file\",\"args\":{\"path\":\"README.md\"}}}]}}]}\n\n",
			},
			validateFinal: func(t *testing.T, final string) {
				t.Helper()
				if !strings.Contains(final, `"call_id":"gem_call_1"`) {
					t.Fatalf("expected raw gemini call_id in final responses stream, got %s", final)
				}
			},
		},
		{
			name:            "messages claude",
			path:            "/cursor/v1/messages",
			requestBody:     messagesRequest,
			endpoint:        config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			wantTransformer: "cc_claude",
			wantTargetPath:  "/v1/messages",
			upstreamEvents: []string{
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\",\"reasoningContent\":\"think\"}}\n\n",
			},
			validateFinal: func(t *testing.T, final string) {
				t.Helper()
				if !strings.Contains(final, `event: content_block_start`) || !strings.Contains(final, `"thinking":"think"`) {
					t.Fatalf("expected /cursor/v1/messages stream thinking injection, got %s", final)
				}
				if !strings.Contains(final, `"index":1`) {
					t.Fatalf("expected original text block to shift to index 1, got %s", final)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepared := prepareCursorRoundTrip(t, tt.path, tt.requestBody, tt.endpoint)
			if prepared.trans.Name() != tt.wantTransformer {
				t.Fatalf("expected transformer %s, got %s", tt.wantTransformer, prepared.trans.Name())
			}
			if prepared.targetPath != tt.wantTargetPath {
				t.Fatalf("expected target path %s, got %s", tt.wantTargetPath, prepared.targetPath)
			}

			final := runCursorRoundTripStream(t, prepared, tt.upstreamEvents...)
			tt.validateFinal(t, final)
		})
	}
}
