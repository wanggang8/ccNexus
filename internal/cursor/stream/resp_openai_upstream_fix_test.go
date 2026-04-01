package stream

import (
	"strings"
	"testing"
)

func TestFixRespOpenAIUpstreamChatSSESplitsContentAndToolCalls(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"Hello","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		"",
	}, "\n"))

	fixed := FixRespOpenAIUpstreamChatSSE(bundle)
	payloads := decodeChatChunkPayloads(t, fixed)
	if len(payloads) != 2 {
		t.Fatalf("expected split content/tool_calls chunks, got %d in %s", len(payloads), string(fixed))
	}
	if payloadChoiceDelta(t, payloads[0])["content"] != "Hello" {
		t.Fatalf("expected content chunk first, got %#v", payloadChoiceDelta(t, payloads[0]))
	}
	if payloadChoiceDelta(t, payloads[0])["tool_calls"] != nil {
		t.Fatalf("did not expect tool_calls in content chunk, got %#v", payloadChoiceDelta(t, payloads[0]))
	}
	toolCalls := payloadChoiceDelta(t, payloads[1])["tool_calls"].([]interface{})
	if toolCalls[0].(map[string]interface{})["id"] != "call_1" {
		t.Fatalf("expected tool_call chunk second, got %#v", toolCalls[0])
	}
}


func TestFixRespOpenAIUpstreamChatSSEPreservesDone(t *testing.T) {
	bundle := []byte("data: [DONE]\n\n")
	fixed := FixRespOpenAIUpstreamChatSSE(bundle)
	if string(fixed) != string(bundle) {
		t.Fatalf("expected [DONE] passthrough, got %s", string(fixed))
	}
}

func TestFixRespOpenAIUpstreamChatSSEPassesThroughNonJSON(t *testing.T) {
	bundle := []byte("data: not-json\n\n")
	fixed := FixRespOpenAIUpstreamChatSSE(bundle)
	if string(fixed) != string(bundle) {
		t.Fatalf("expected passthrough for non-json, got %s", string(fixed))
	}
}

func TestFixRespOpenAIUpstreamChatSSESkipsSplitWhenNoToolCalls(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		"",
	}, "\n"))

	fixed := FixRespOpenAIUpstreamChatSSE(bundle)
	payloads := decodeChatChunkPayloads(t, fixed)
	if len(payloads) != 1 {
		t.Fatalf("expected single chunk, got %d in %s", len(payloads), string(fixed))
	}
	if payloadChoiceDelta(t, payloads[0])["content"] != "Hello" {
		t.Fatalf("expected content preserved, got %#v", payloadChoiceDelta(t, payloads[0]))
	}
}

