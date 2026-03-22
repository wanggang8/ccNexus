package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/transformer"
	"github.com/lich0821/ccNexus/internal/transformer/cx/responses"
)

type noUsageStreamTransformer struct{}

func (t *noUsageStreamTransformer) Name() string {
	return "test_no_usage"
}

func (t *noUsageStreamTransformer) TransformRequest(claudeReq []byte) ([]byte, error) {
	return claudeReq, nil
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

	p := &Proxy{config: cfg, trafficRecorder: NewTrafficRecorder()}
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
	)

	if in != 7 || out != 5 {
		t.Fatalf("expected tokens from original stream usage in=7 out=5, got in=%d out=%d", in, out)
	}
}

func TestHandleStreamingResponse_DoesNotInjectClaudeUsageFallbackIntoResponsesStream(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "TokenPool",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai",
			Model:       "gpt-5.4",
		},
	})

	p := &Proxy{config: cfg, trafficRecorder: NewTrafficRecorder()}
	endpoint := cfg.GetEndpoints()[0]
	upstreamSSE := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}
	rec := httptest.NewRecorder()

	_, _, _, _, transformed := p.handleStreamingResponse(
		rec,
		resp,
		endpoint,
		&passthroughResponseTransformer{},
		"cx_resp_openai",
		false,
		"gpt-5.4",
		[]byte(`{"stream":true}`),
		0,
	)

	got := rec.Body.String()
	if strings.Contains(got, `"type":"message_delta"`) {
		t.Fatalf("responses stream should not contain injected claude message_delta event: %s", got)
	}
	if strings.Contains(string(transformed), `"type":"message_delta"`) {
		t.Fatalf("transformed responses stream should not record injected claude message_delta event: %s", string(transformed))
	}
}

// TestHandleStreamingResponse_CxRespOpenai_EndToEnd converts upstream OpenAI Chat SSE to Responses SSE
// through the real cx_resp_openai transformer (same path as production).
func TestHandleStreamingResponse_CxRespOpenai_EndToEnd(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateEndpoints([]config.Endpoint{
		{
			Name:        "E2E",
			APIUrl:      "https://example.com",
			APIKey:      "x",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai",
			Model:       "gpt-5.4",
		},
	})

	p := &Proxy{config: cfg, trafficRecorder: NewTrafficRecorder()}
	endpoint := cfg.GetEndpoints()[0]
	trans := responses.NewOpenAITransformer("gpt-5.4")

	upstreamSSE := strings.Join([]string{
		`data: {"id":"chatcmpl-e2e","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-e2e","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-e2e","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_e2e","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"/a\"}"}}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl-e2e","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}
	rec := httptest.NewRecorder()

	_, _, _, _, _ = p.handleStreamingResponse(
		rec,
		resp,
		endpoint,
		trans,
		"cx_resp_openai",
		false,
		"gpt-5.4",
		[]byte(`{"stream":true}`),
		0,
	)

	got := rec.Body.String()
	if strings.Contains(got, "chat.completion.chunk") {
		t.Fatalf("client must not see upstream chat chunks, got: %s", got)
	}
	if !strings.Contains(got, `"type":"response.created"`) {
		t.Fatalf("expected response.created in client stream: %s", got)
	}
	if !strings.Contains(got, `"type":"response.output_text.delta"`) {
		t.Fatalf("expected response.output_text.delta: %s", got)
	}
	if !strings.Contains(got, `"type":"response.function_call_arguments.done"`) {
		t.Fatalf("expected response.function_call_arguments.done: %s", got)
	}
	if !strings.Contains(got, `"type":"response.completed"`) {
		t.Fatalf("expected response.completed: %s", got)
	}
	if !strings.Contains(got, "data: [DONE]") {
		t.Fatalf("expected [DONE]: %s", got)
	}
	textDone := strings.Index(got, `"type":"response.output_text.done"`)
	toolDone := strings.Index(got, `"type":"response.function_call_arguments.done"`)
	if textDone == -1 || toolDone == -1 || textDone > toolDone {
		t.Fatalf("expected text output_text.done before tool arguments.done, got: %s", got)
	}
}
