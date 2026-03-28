package cursor

import (
	"strings"
	"testing"
)

func TestTransformCursorUpstreamStreamEventFixesOpenAIResponsesBridgeChunks(t *testing.T) {
	eventData := []byte("data: {\"id\":\"cmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"reasoningContent\":\"think\",\"function_call\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}},\"finish_reason\":\"function_call\"}]}\n\n")

	transformed, err := TransformCursorUpstreamStreamEvent(
		RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIResponses,
		},
		eventData,
		"cx_resp_openai",
		"",
		&StreamFinalizeState{},
		func(b []byte) ([]byte, error) {
			return b, nil
		},
	)
	if err != nil {
		t.Fatalf("TransformCursorUpstreamStreamEvent failed: %v", err)
	}

	transformedStr := string(transformed)
	if !strings.Contains(transformedStr, `"reasoning_content":"think"`) {
		t.Fatalf("expected reasoningContent promoted before transform, got %s", transformedStr)
	}
	if !strings.Contains(transformedStr, `"tool_calls"`) {
		t.Fatalf("expected legacy function_call rewritten before transform, got %s", transformedStr)
	}
	if strings.Contains(transformedStr, `"function_call"`) {
		t.Fatalf("did not expect legacy function_call after upstream fix, got %s", transformedStr)
	}
}

func TestTransformCursorUpstreamStreamEventBypassesCursorMessagesClaudeFallback(t *testing.T) {
	eventData := []byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}\n\n")
	called := false

	transformed, err := TransformCursorUpstreamStreamEvent(
		RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatClaude,
		},
		eventData,
		"cc_claude",
		"",
		&StreamFinalizeState{},
		func(b []byte) ([]byte, error) {
			called = true
			return append([]byte("modified:"), b...), nil
		},
	)
	if err != nil {
		t.Fatalf("TransformCursorUpstreamStreamEvent failed: %v", err)
	}
	if called {
		t.Fatalf("expected Cursor /messages stream to bypass cc_claude fallback transform")
	}
	if string(transformed) != string(eventData) {
		t.Fatalf("expected original event to pass through unchanged, got %s", string(transformed))
	}
}

func TestTransformCursorUpstreamStreamEventPreservesGeminiChatToolCallIDs(t *testing.T) {
	eventData := []byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"gem_call_1\",\"name\":\"read_file\",\"args\":{\"path\":\"README.md\"}}}]}}]}\n\n")

	transformed, err := TransformCursorUpstreamStreamEvent(
		RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIChat,
		},
		eventData,
		"cx_chat_gemini",
		"",
		&StreamFinalizeState{},
		func([]byte) ([]byte, error) {
			return []byte("data: {\"id\":\"gemini-chunk\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_0\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]}}]}\n\n"), nil
		},
	)
	if err != nil {
		t.Fatalf("TransformCursorUpstreamStreamEvent failed: %v", err)
	}
	if !strings.Contains(string(transformed), `"id":"gem_call_1"`) {
		t.Fatalf("expected raw gemini tool id to be preserved, got %s", string(transformed))
	}
}

func TestTransformCursorUpstreamStreamEventPreservesGeminiResponsesCallIDs(t *testing.T) {
	eventData := []byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"gem_call_1\",\"name\":\"read_file\",\"args\":{\"path\":\"README.md\"}}}]}}]}\n\n")

	transformed, err := TransformCursorUpstreamStreamEvent(
		RequestMeta{
			CursorMode:   true,
			ClientFormat: ClientFormatOpenAIResponses,
		},
		eventData,
		"cx_resp_gemini",
		"",
		&StreamFinalizeState{},
		func([]byte) ([]byte, error) {
			return []byte(strings.Join([]string{
				`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","call_id":"call_0","name":"read_file","arguments":"","status":"in_progress"}}`,
				"",
				`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","call_id":"call_0","name":"read_file","arguments":"{\"path\":\"README.md\"}","status":"completed"}}`,
				"",
			}, "\n")), nil
		},
	)
	if err != nil {
		t.Fatalf("TransformCursorUpstreamStreamEvent failed: %v", err)
	}
	transformedStr := string(transformed)
	if !strings.Contains(transformedStr, `"call_id":"gem_call_1"`) {
		t.Fatalf("expected raw gemini call_id to be preserved, got %s", transformedStr)
	}
}
