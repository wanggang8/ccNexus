package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	newcursor "github.com/lich0821/ccNexus/internal/cursorbridge"
	"github.com/lich0821/ccNexus/internal/transformer"
	cxchat "github.com/lich0821/ccNexus/internal/transformer/cx/chat"
)

type noUsageStreamTransformer struct{}

type noUsageChatStreamTransformer struct{}

func (t *noUsageStreamTransformer) Name() string {
	return "test_no_usage"
}

func (t *noUsageStreamTransformer) TransformRequest(claudeReq []byte) ([]byte, error) {
	return claudeReq, nil
}

func (t *noUsageChatStreamTransformer) Name() string {
	return "test_no_usage_chat"
}

func (t *noUsageChatStreamTransformer) TransformRequest(req []byte) ([]byte, error) {
	return req, nil
}

func (t *noUsageChatStreamTransformer) TransformResponse(targetResp []byte, isStreaming bool) ([]byte, error) {
	return targetResp, nil
}

func (t *noUsageChatStreamTransformer) TransformResponseWithContext(targetResp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
	if !isStreaming {
		return targetResp, nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(targetResp, &payload); err != nil {
		return targetResp, nil
	}
	delete(payload, "usage")
	encoded, err := json.Marshal(payload)
	if err != nil {
		return targetResp, nil
	}
	return append([]byte("data: "), append(encoded, []byte("\n\n")...)...), nil
}

func (t *noUsageStreamTransformer) TransformResponse(targetResp []byte, isStreaming bool) ([]byte, error) {
	return targetResp, nil
}

func (t *noUsageStreamTransformer) TransformResponseWithContext(targetResp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
	if !isStreaming {
		return targetResp, nil
	}
	// Simulate transformer that drops usage data.
	return []byte("data: {\"type\":\"response.completed\"}\n\n"), nil
}

type passthroughStreamTransformer struct{}

func (t *passthroughStreamTransformer) Name() string {
	return "test_passthrough"
}

func (t *passthroughStreamTransformer) TransformRequest(req []byte) ([]byte, error) {
	return req, nil
}

func (t *passthroughStreamTransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
	return resp, nil
}

func (t *passthroughStreamTransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
	return resp, nil
}

type errorStreamTransformer struct{}

func (t *errorStreamTransformer) Name() string {
	return "test_error"
}

func (t *errorStreamTransformer) TransformRequest(req []byte) ([]byte, error) {
	return req, nil
}

func (t *errorStreamTransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
	return resp, nil
}

func (t *errorStreamTransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
	return nil, io.ErrClosedPipe
}

type terminalErrReadCloser struct {
	remaining   string
	terminalErr error
	done        bool
}

func (r *terminalErrReadCloser) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	if r.remaining == "" {
		r.done = true
		return 0, r.terminalErr
	}
	n := copy(p, r.remaining)
	r.remaining = r.remaining[n:]
	if r.remaining == "" {
		r.done = true
		return n, r.terminalErr
	}
	return n, nil
}

func (r *terminalErrReadCloser) Close() error {
	r.done = true
	return nil
}

func TestHandleStreamingResponseCursorChatOpenAIDataOnlySSEPreservesTextUsageAndDone(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "OpenAI",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai",
			Model:       "gpt-4.1",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	originalSSE := strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		"",
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(originalSSE)),
	}
	rec := httptest.NewRecorder()
	meta := proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIChat,
			ClientModel:  "cursor-model",
		},
		TransformerName: "cx_chat_openai",
		CursorState:     &newcursor.StreamFinalizeState{},
	}

	p.handleStreamingResponse(rec, resp, endpoint, cxchat.NewOpenAITransformer("cursor-model"), "cx_chat_openai", false, "cursor-model", []byte(`{"model":"cursor-model","messages":[{"role":"user","content":"hello"}],"stream":true}`), 0, meta)

	body := rec.Body.String()
	if !strings.Contains(body, `"content":"hello"`) {
		t.Fatalf("expected text chunk preserved, got %s", body)
	}
	if !strings.Contains(body, `"usage":{"completion_tokens":2,"prompt_tokens":3,"total_tokens":5}`) {
		t.Fatalf("expected final usage chunk preserved, got %s", body)
	}
	if !strings.Contains(body, `data: [DONE]`) {
		t.Fatalf("expected DONE terminator preserved, got %s", body)
	}
}

func TestHandleStreamingResponseCursorChatOpenAIInjectsUsageFallbackWhenFinalChunkMissingUsage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "OpenAI",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai",
			Model:       "gpt-4.1",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	originalSSE := strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		"",
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(originalSSE)),
	}
	rec := httptest.NewRecorder()
	meta := proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIChat,
			ClientModel:  "cursor-model",
		},
		TransformerName: "cx_chat_openai",
		CursorState:     &newcursor.StreamFinalizeState{},
	}

	p.handleStreamingResponse(rec, resp, endpoint, &noUsageChatStreamTransformer{}, "cx_chat_openai", false, "cursor-model", []byte(`{"model":"cursor-model","messages":[{"role":"user","content":"hello"}],"stream":true}`), 0, meta)

	body := rec.Body.String()
	if !strings.Contains(body, `"content":"hello"`) {
		t.Fatalf("expected text chunk preserved, got %s", body)
	}
	if !strings.Contains(body, `"usage":{"completion_tokens":2,"prompt_tokens":3,"total_tokens":5}`) {
		t.Fatalf("expected injected usage fallback chunk, got %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Fatalf("expected usage fallback chunk to finalize stream, got %s", body)
	}
}

func TestHandleStreamingResponseExtractsUsageFromOriginalEvent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "TokenPool",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai2",
			Model:       "gpt-4.1",
		},
	})

	recorder := NewTrafficRecorder()
	recorder.SetRecording(true)
	p := &Proxy{config: cfg, trafficRecorder: recorder}
	endpoint := cfg.GetEndpoints()[0]
	originalSSE := strings.Join([]string{
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":7,"output_tokens":5,"total_tokens":12}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(originalSSE)),
	}
	rec := httptest.NewRecorder()

	in, out, _, _, _ := p.handleStreamingResponse(
		rec,
		resp,
		endpoint,
		&noUsageStreamTransformer{},
		"cc_openai2",
		false,
		"gpt-4.1",
		[]byte(`{}`),
		0,
		proxyRequestMeta{},
	)

	if in != 7 || out != 5 {
		t.Fatalf("expected tokens from original stream usage in=7 out=5, got in=%d out=%d", in, out)
	}
}

func TestHandleStreamingResponseSetsNoCacheHeadersOnlyForCursorMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "TokenPool",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai2",
			Model:       "gpt-4.1",
		},
	})
	trafficRecorder := NewTrafficRecorder()
	trafficRecorder.SetRecording(true)
	p := &Proxy{config: cfg, trafficRecorder: trafficRecorder}
	endpoint := cfg.GetEndpoints()[0]
	originalSSE := "data: [DONE]\n\n"

	makeResp := func() *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(originalSSE)),
		}
	}

	cursorRec := httptest.NewRecorder()
	p.handleStreamingResponse(cursorRec, makeResp(), endpoint, &noUsageStreamTransformer{}, "cc_openai2", false, "gpt-4.1", []byte(`{}`), 0, proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode: true,
		},
	})
	if got := cursorRec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("expected cursor mode Cache-Control=no-cache, got %q", got)
	}
	if got := cursorRec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("expected cursor mode X-Accel-Buffering=no, got %q", got)
	}

	normalRec := httptest.NewRecorder()
	p.handleStreamingResponse(normalRec, makeResp(), endpoint, &noUsageStreamTransformer{}, "cc_openai2", false, "gpt-4.1", []byte(`{}`), 0, proxyRequestMeta{})
	if got := normalRec.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("expected non-cursor mode not to force Cache-Control, got %q", got)
	}
}

func TestHandleStreamingResponsePreservesLargeRecordedBodies(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "TokenPool",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai",
			Model:       "gpt-4.1",
		},
	})
	trafficRecorder := NewTrafficRecorder()
	trafficRecorder.SetRecording(true)
	p := &Proxy{config: cfg, trafficRecorder: trafficRecorder}
	endpoint := cfg.GetEndpoints()[0]

	largeDelta := strings.Repeat("x", 600*1024)
	originalSSE := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"` + largeDelta + `"}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(originalSSE)),
	}
	rec := httptest.NewRecorder()

	_, _, _, original, transformed := p.handleStreamingResponse(
		rec,
		resp,
		endpoint,
		&passthroughStreamTransformer{},
		"cx_chat_openai",
		false,
		"gpt-4.1",
		[]byte(`{}`),
		0,
		proxyRequestMeta{},
	)

	if !strings.Contains(string(original), largeDelta) {
		t.Fatalf("expected original stream capture to preserve large delta, got length %d", len(original))
	}
	if !strings.Contains(string(transformed), largeDelta) {
		t.Fatalf("expected transformed stream capture to preserve large delta, got length %d", len(transformed))
	}
	if len(original) <= len(largeDelta) || len(transformed) <= len(largeDelta) {
		t.Fatalf("expected stream captures to include full SSE envelope, got original=%d transformed=%d", len(original), len(transformed))
	}
}

func TestHandleStreamingResponseCursorResponsesBridgeEmitsCreatedAndNoDone(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "TokenPool",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai",
			Model:       "gpt-4.1",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	originalSSE := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(originalSSE)),
	}
	rec := httptest.NewRecorder()
	meta := proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIResponses,
			ClientModel:  "cursor-model",
		},
		TransformerName: "cx_resp_openai",
		CursorState: &newcursor.StreamFinalizeState{
			ResponsesTools:  make(map[int]*newcursor.ResponseToolState),
			ResponsesOutput: make([]map[string]interface{}, 0),
		},
	}

	p.handleStreamingResponse(rec, resp, endpoint, &passthroughStreamTransformer{}, "cx_resp_openai", false, "cursor-model", []byte(`{}`), 0, meta)

	out := rec.Body.String()
	createdIndex := strings.Index(out, "event: response.created")
	completedIndex := strings.Index(out, "event: response.completed")
	doneIndex := strings.Index(out, "data: [DONE]")
	if createdIndex == -1 || completedIndex == -1 {
		t.Fatalf("expected created and completed events, got %s", out)
	}
	if createdIndex > completedIndex {
		t.Fatalf("expected created before completed, got %s", out)
	}
	if doneIndex != -1 {
		t.Fatalf("expected responses stream without [DONE], got %s", out)
	}
}

func TestHandleStreamingResponseFinalizesUnclosedCursorThinkTag(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "TokenPool",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai2",
			Model:       "gpt-4.1",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	originalSSE := "data: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(originalSSE)),
	}
	rec := httptest.NewRecorder()
	meta := proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIChat,
		},
		CursorState: &newcursor.StreamFinalizeState{InThinkingTag: true},
	}
	p.handleStreamingResponse(rec, resp, endpoint, &noUsageStreamTransformer{}, "cc_openai2", false, "gpt-4.1", []byte(`{}`), 0, meta)

	if !strings.Contains(rec.Body.String(), `\u003c/think\u003e`) {
		t.Fatalf("expected finalize chunk to close think tag, got %q", rec.Body.String())
	}
}

func TestHandleStreamingResponseGracefullyEndsCursorChatOnUnexpectedEOF(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "TokenPool",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai",
			Model:       "gpt-4.1",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	originalSSE := `data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: &terminalErrReadCloser{
			remaining:   originalSSE,
			terminalErr: io.ErrUnexpectedEOF,
		},
	}
	rec := httptest.NewRecorder()
	meta := proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIChat,
			ClientModel:  "cursor-model",
		},
		TransformerName: "cx_chat_openai",
		CursorState:     &newcursor.StreamFinalizeState{},
	}

	p.handleStreamingResponse(rec, resp, endpoint, &passthroughStreamTransformer{}, "cx_chat_openai", false, "cursor-model", []byte(`{}`), 0, meta)

	body := rec.Body.String()
	if !strings.Contains(body, `"content":"hello"`) {
		t.Fatalf("expected partial chat chunk preserved on unexpected EOF, got %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected graceful [DONE] termination on unexpected EOF, got %s", body)
	}
}

func TestHandleStreamingResponseCursorChatEmptyUpstreamEmitsDoneAfterUsageFallback(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "OpenAI",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai",
			Model:       "gpt-4.1",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("")),
	}
	rec := httptest.NewRecorder()
	reqBody := []byte(`{"model":"cursor-model","messages":[{"role":"user","content":"hello from cursor"}],"stream":true}`)
	meta := proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIChat,
			ClientModel:  "cursor-model",
		},
		TransformerName: "cx_chat_openai",
		CursorState:     &newcursor.StreamFinalizeState{},
	}

	p.handleStreamingResponse(rec, resp, endpoint, &passthroughStreamTransformer{}, "cx_chat_openai", false, "cursor-model", reqBody, 0, meta)

	out := rec.Body.String()
	if !strings.Contains(out, `"finish_reason":"stop"`) {
		t.Fatalf("expected post-loop usage fallback final chunk, got %s", out)
	}
	if !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("expected [DONE] after empty upstream (api2cursor parity), got %s", out)
	}
}

func TestHandleStreamingResponseDoesNotEmitDoneWithoutCursorChatPayload(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "TokenPool",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai",
			Model:       "gpt-4.1",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	originalSSE := `data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: &terminalErrReadCloser{
			remaining:   originalSSE,
			terminalErr: io.ErrUnexpectedEOF,
		},
	}
	rec := httptest.NewRecorder()
	meta := proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIChat,
			ClientModel:  "cursor-model",
		},
		TransformerName: "cx_chat_openai",
		CursorState:     &newcursor.StreamFinalizeState{},
	}

	// Invalid JSON so estimateInputTokens returns 0; otherwise post-loop usage fallback + [DONE] would still run.
	p.handleStreamingResponse(rec, resp, endpoint, &errorStreamTransformer{}, "cx_chat_openai", false, "cursor-model", []byte(`not-json`), 0, meta)

	body := rec.Body.String()
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected no synthetic [DONE] when no chat payload reached client, got %s", body)
	}
	if strings.Contains(body, `"content":"hello"`) {
		t.Fatalf("expected no partial chat chunk when transformer failed, got %s", body)
	}
}

func TestHandleStreamingResponseCursorChatClaudeDataOnlySSEPreservesTextAndUsage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "Claude",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "claude",
			Model:       "claude-sonnet-4-20250514",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	originalSSE := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(originalSSE)),
	}
	rec := httptest.NewRecorder()
	meta := proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIChat,
			ClientModel:  "gpt-5.4",
		},
		TransformerName: "cx_chat_claude",
		CursorState:     &newcursor.StreamFinalizeState{},
	}

	p.handleStreamingResponse(rec, resp, endpoint, cxchat.NewClaudeTransformer("gpt-5.4"), "cx_chat_claude", false, "gpt-5.4", []byte(`{}`), 0, meta)

	body := rec.Body.String()
	if !strings.Contains(body, `"content":"hello"`) {
		t.Fatalf("expected text chunk preserved for data-only Claude SSE, got %s", body)
	}
	if !strings.Contains(body, `"usage":{"completion_tokens":7,"prompt_tokens":10,"total_tokens":17}`) {
		t.Fatalf("expected final usage chunk preserved, got %s", body)
	}
	if !strings.Contains(body, `data: [DONE]`) {
		t.Fatalf("expected DONE terminator preserved, got %s", body)
	}
}

func TestHandleStreamingResponseClaudeEmptyStartEOFEmitsDoneLikeAPI2Cursor(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "Claude",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "claude",
			Model:       "claude-sonnet-4-20250514",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	originalSSE := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: &terminalErrReadCloser{
			remaining:   originalSSE,
			terminalErr: io.ErrUnexpectedEOF,
		},
	}
	rec := httptest.NewRecorder()
	meta := proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIChat,
			ClientModel:  "gpt-5.4",
		},
		TransformerName: "cx_chat_claude",
		CursorState:     &newcursor.StreamFinalizeState{},
	}

	p.handleStreamingResponse(rec, resp, endpoint, cxchat.NewClaudeTransformer("gpt-5.4"), "cx_chat_claude", false, "gpt-5.4", []byte(`{}`), 0, meta)

	body := rec.Body.String()
	if !strings.Contains(body, `"role":"assistant"`) {
		t.Fatalf("expected initial assistant chunk preserved, got %s", body)
	}
	if strings.Contains(body, `"tool_calls":[`) {
		t.Fatalf("did not expect synthesized tool calls, got %s", body)
	}
	// Align with api2cursor: chat streams always end with data: [DONE] after upstream closes.
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected trailing [DONE] after partial Claude stream (api2cursor parity), got %s", body)
	}
}

func TestHandleStreamingResponseCursorChatClaudePreservesPromptUsageOnZeroInputTokens(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "Claude",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "claude",
			Model:       "claude-sonnet-4-20250514",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	trans, err := prepareTransformerForClient(ClientFormatOpenAIChat, endpoint)
	if err != nil {
		t.Fatalf("prepareTransformerForClient failed: %v", err)
	}

	originalSSE := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude","stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}}`,
		"",
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: &terminalErrReadCloser{
			remaining:   originalSSE,
			terminalErr: io.ErrUnexpectedEOF,
		},
	}
	rec := httptest.NewRecorder()
	meta := proxyRequestMeta{
		RequestMeta: newcursor.RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIChat,
			ClientModel:  "gpt-5.4",
		},
		TransformerName: trans.Name(),
		CursorState:     &newcursor.StreamFinalizeState{},
	}

	in, out, _, _, _ := p.handleStreamingResponse(
		rec,
		resp,
		endpoint,
		trans,
		trans.Name(),
		false,
		"gpt-5.4",
		[]byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":true}`),
		0,
		meta,
	)

	if in == 0 {
		t.Fatalf("expected non-zero prompt tokens from request estimate fallback, got %d", in)
	}
	if out != 7 {
		t.Fatalf("expected output tokens from message_delta usage, got %d", out)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"completion_tokens":7`) {
		t.Fatalf("expected final chunk completion usage, got %s", body)
	}
	if strings.Contains(body, `"prompt_tokens":0`) {
		t.Fatalf("expected final chunk to avoid prompt_tokens=0, got %s", body)
	}
}

func TestHandleStreamingAsNonStreamingAcceptsUnexpectedEOFAfterCompletedPayload(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "TokenPool",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai2",
			Model:       "gpt-4.1",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	originalSSE := `data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","usage":{"input_tokens":7,"output_tokens":5,"total_tokens":12}}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: &terminalErrReadCloser{
			remaining:   originalSSE,
			terminalErr: io.ErrUnexpectedEOF,
		},
	}
	rec := httptest.NewRecorder()

	in, out, outputText, err := p.handleStreamingAsNonStreaming(rec, resp, endpoint, &passthroughStreamTransformer{}, 0, proxyRequestMeta{})
	if err != nil {
		t.Fatalf("expected aggregated non-stream path to tolerate unexpected EOF after completed payload, got %v", err)
	}
	if in != 7 || out != 5 {
		t.Fatalf("expected usage preserved, got in=%d out=%d", in, out)
	}
	if outputText != "" {
		t.Fatalf("expected empty output text for bare completed response, got %q", outputText)
	}
	if !strings.Contains(rec.Body.String(), `"id":"resp_1"`) {
		t.Fatalf("expected completed payload written to client, got %s", rec.Body.String())
	}
}

func TestHandleStreamingAsNonStreamingRejectsUnexpectedEOFWithOnlyDeltaEvent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "TokenPool",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai2",
			Model:       "gpt-4.1",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	originalSSE := `data: {"type":"response.output_text.delta","delta":"hello"}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: &terminalErrReadCloser{
			remaining:   originalSSE,
			terminalErr: io.ErrUnexpectedEOF,
		},
	}
	rec := httptest.NewRecorder()

	_, _, _, err := p.handleStreamingAsNonStreaming(rec, resp, endpoint, &passthroughStreamTransformer{}, 0, proxyRequestMeta{})
	if err == nil {
		t.Fatalf("expected aggregate non-stream path to reject delta-only EOF")
	}
	if !strings.Contains(err.Error(), "response.completed") {
		t.Fatalf("expected response.completed error, got %v", err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected no response body on delta-only EOF failure, got %s", rec.Body.String())
	}
}

func TestHandleStreamingAsNonStreamingFallsBackToTerminalResponseObjectWithoutCompletedEvent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "TokenPool",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai2",
			Model:       "gpt-4.1",
		},
	})
	p := &Proxy{config: cfg}
	endpoint := cfg.GetEndpoints()[0]

	originalSSE := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"object":"response","id":"resp_fallback","status":"completed","usage":{"input_tokens":4,"output_tokens":6,"total_tokens":10},"output":[{"type":"message","content":[{"type":"output_text","text":"hello world"}]}]}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(originalSSE)),
	}
	rec := httptest.NewRecorder()

	in, out, outputText, err := p.handleStreamingAsNonStreaming(rec, resp, endpoint, &passthroughStreamTransformer{}, 0, proxyRequestMeta{})
	if err != nil {
		t.Fatalf("expected aggregate non-stream fallback to accept terminal response object, got %v", err)
	}
	if in != 4 || out != 6 {
		t.Fatalf("expected usage from terminal response object, got in=%d out=%d", in, out)
	}
	if outputText != "hello world" {
		t.Fatalf("expected extracted output text hello world, got %q", outputText)
	}
	if !strings.Contains(rec.Body.String(), `"id":"resp_fallback"`) {
		t.Fatalf("expected terminal response object written to client, got %s", rec.Body.String())
	}
}
