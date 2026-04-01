package stream

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFixChatBundleSplitsThinkTags(t *testing.T) {
	bundle := []byte("data: {\"id\":\"cmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"upstream\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"<think>reason</think>Hello\"},\"finish_reason\":null}]}\n\n")
	fixed, err := FixChatBundle(bundle, "cursor-model", &FinalizeState{})
	if err != nil {
		t.Fatalf("FixChatBundle failed: %v", err)
	}
	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, `"reasoning_content":"reason"`) {
		t.Fatalf("expected reasoning_content split, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"content":"Hello"`) {
		t.Fatalf("expected content chunk preserved, got %s", fixedStr)
	}
}

func TestFixChatBundleStopsThinkingBeforeToolCalls(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"<think>reason"},"finish_reason":null}]}`,
		"",
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		"",
	}, "\n"))
	fixed, err := FixChatBundle(bundle, "cursor-model", &FinalizeState{})
	if err != nil {
		t.Fatalf("FixChatBundle failed: %v", err)
	}
	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, `"reasoning_content":"reason"`) {
		t.Fatalf("expected opening think content to become reasoning delta, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"tool_calls":[`) {
		t.Fatalf("expected tool_calls chunk preserved, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"\n\u003c/think\u003e\n\n"`) && !strings.Contains(fixedStr, `"\n</think>\n\n"`) {
		t.Fatalf("expected explicit </think> closing chunk before tool_calls, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"content":"Hello"`) {
		t.Fatalf("expected post-tool text to stay normal content, got %s", fixedStr)
	}
	if strings.Contains(fixedStr, `"reasoning_content":"Hello"`) {
		t.Fatalf("did not expect post-tool text to remain in reasoning mode, got %s", fixedStr)
	}
}

func TestFixChatBundleSplitsContentAndToolCallsFromSameChunk(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"Hello","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		"",
	}, "\n"))

	state := &FinalizeState{}
	state.ChatToolCallsSeen = true
	fixed, err := FixChatBundle(bundle, "cursor-model", state)
	if err != nil {
		t.Fatalf("FixChatBundle failed: %v", err)
	}

	payloads := decodeChatChunkPayloads(t, fixed)
	if len(payloads) != 2 {
		t.Fatalf("expected content/tool_call chunks, got %d in %s", len(payloads), string(fixed))
	}
	if payloadChoiceDelta(t, payloads[0])["content"] != "Hello" {
		t.Fatalf("expected first chunk to keep content, got %#v", payloadChoiceDelta(t, payloads[0]))
	}
	if payloadChoiceDelta(t, payloads[0])["tool_calls"] != nil {
		t.Fatalf("did not expect tool_calls mixed into content chunk, got %#v", payloadChoiceDelta(t, payloads[0]))
	}
	toolCalls := payloadChoiceDelta(t, payloads[1])["tool_calls"].([]interface{})
	if toolCalls[0].(map[string]interface{})["id"] != "call_1" {
		t.Fatalf("expected final chunk to preserve tool call, got %#v", toolCalls[0])
	}
	if payloadChoice(t, payloads[1])["finish_reason"] != "tool_calls" {
		t.Fatalf("expected tool_calls finish_reason on tool chunk, got %#v", payloadChoice(t, payloads[1])["finish_reason"])
	}
}

func TestFixChatBundleDropsEmptyToolCallFields(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"","arguments":""}}]},"finish_reason":null}]}`,
		"",
	}, "\n"))

	fixed, err := FixChatBundle(bundle, "cursor-model", &FinalizeState{})
	if err != nil {
		t.Fatalf("FixChatBundle failed: %v", err)
	}

	payloads := decodeChatChunkPayloads(t, fixed)
	if len(payloads) != 1 {
		t.Fatalf("expected one chunk, got %d in %s", len(payloads), string(fixed))
	}
	delta := payloadChoiceDelta(t, payloads[0])
	toolCalls := delta["tool_calls"].([]interface{})
	toolCall := toolCalls[0].(map[string]interface{})
	if _, ok := toolCall["function"]; ok {
		t.Fatalf("expected empty function object removed, got %#v", toolCall)
	}
}

func TestFixChatBundleKeepsToolCallIDWhenPresent(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file","arguments":""}}]},"finish_reason":null}]}`,
		"",
	}, "\n"))

	fixed, err := FixChatBundle(bundle, "cursor-model", &FinalizeState{})
	if err != nil {
		t.Fatalf("FixChatBundle failed: %v", err)
	}

	payloads := decodeChatChunkPayloads(t, fixed)
	if len(payloads) != 1 {
		t.Fatalf("expected one chunk, got %d in %s", len(payloads), string(fixed))
	}
	delta := payloadChoiceDelta(t, payloads[0])
	toolCalls := delta["tool_calls"].([]interface{})
	toolCall := toolCalls[0].(map[string]interface{})
	if toolCall["id"] != "call_1" {
		t.Fatalf("expected tool call id preserved, got %#v", toolCall)
	}
}

func TestFixChatBundleSplitsContentAndToolCallsAtStart(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"Hello","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		"",
	}, "\n"))

	fixed, err := FixChatBundle(bundle, "cursor-model", &FinalizeState{})
	if err != nil {
		t.Fatalf("FixChatBundle failed: %v", err)
	}

	payloads := decodeChatChunkPayloads(t, fixed)
	if len(payloads) != 3 {
		t.Fatalf("expected split content/newline/tool_calls, got %d in %s", len(payloads), string(fixed))
	}
	if payloadChoiceDelta(t, payloads[0])["content"] != "Hello" {
		t.Fatalf("expected content chunk first, got %#v", payloadChoiceDelta(t, payloads[0]))
	}
	if payloadChoiceDelta(t, payloads[1])["content"] != "\n" {
		t.Fatalf("expected newline chunk second, got %#v", payloadChoiceDelta(t, payloads[1]))
	}
	if payloadChoiceDelta(t, payloads[2])["tool_calls"] == nil {
		t.Fatalf("expected tool_calls chunk third, got %#v", payloadChoiceDelta(t, payloads[2]))
	}
}


func TestFixChatBundleAddsNewlineBeforeFirstToolCall(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		"",
	}, "\n"))

	fixed, err := FixChatBundle(bundle, "cursor-model", &FinalizeState{})
	if err != nil {
		t.Fatalf("FixChatBundle failed: %v", err)
	}

	payloads := decodeChatChunkPayloads(t, fixed)
	if len(payloads) != 1 {
		t.Fatalf("expected single tool_call chunk without synthetic newline, got %d in %s", len(payloads), string(fixed))
	}
	firstDelta := payloadChoiceDelta(t, payloads[0])
	if firstDelta["tool_calls"] == nil {
		t.Fatalf("expected tool_calls to remain present, got %#v", firstDelta)
	}
}

func TestFixChatBundleDoesNotSynthesizeToolCallIDBeforeTerminalChunk(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"read_file","arguments":""}}]},"finish_reason":null}]}`,
		"",
	}, "\n"))

	fixed, err := FixChatBundle(bundle, "cursor-model", &FinalizeState{})
	if err != nil {
		t.Fatalf("FixChatBundle failed: %v", err)
	}

	payloads := decodeChatChunkPayloads(t, fixed)
	if len(payloads) != 1 {
		t.Fatalf("expected one tool_call chunk, got %d in %s", len(payloads), string(fixed))
	}
	toolCalls := payloadChoiceDelta(t, payloads[0])["tool_calls"].([]interface{})
	toolCall := toolCalls[0].(map[string]interface{})
	if _, exists := toolCall["id"]; exists {
		t.Fatalf("did not expect synthesized tool call id before terminal chunk, got %#v", toolCall)
	}
	if toolCall["type"] != "function" {
		t.Fatalf("expected function type preserved, got %#v", toolCall)
	}
}

func TestFixChatBundleSynthesizesToolCallIDAtTerminalChunk(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"read_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		"",
	}, "\n"))

	fixed, err := FixChatBundle(bundle, "cursor-model", &FinalizeState{})
	if err != nil {
		t.Fatalf("FixChatBundle failed: %v", err)
	}

	payloads := decodeChatChunkPayloads(t, fixed)
	if len(payloads) != 1 {
		t.Fatalf("expected one tool_call chunk, got %d in %s", len(payloads), string(fixed))
	}
	toolCalls := payloadChoiceDelta(t, payloads[0])["tool_calls"].([]interface{})
	toolCall := toolCalls[0].(map[string]interface{})
	id, _ := toolCall["id"].(string)
	if !strings.HasPrefix(id, "call_") {
		t.Fatalf("expected synthesized tool call id at terminal chunk, got %#v", toolCall)
	}
	if toolCall["type"] != "function" {
		t.Fatalf("expected function type preserved, got %#v", toolCall)
	}
}

func TestFixChatBundleClosesThinkingBeforeDone(t *testing.T) {
	state := &FinalizeState{}
	bundle := []byte(strings.Join([]string{
		`data: {"id":"cmpl_1","object":"chat.completion.chunk","model":"upstream","choices":[{"index":0,"delta":{"content":"<think>reason"},"finish_reason":null}]}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n"))

	fixed, err := FixChatBundle(bundle, "cursor-model", state)
	if err != nil {
		t.Fatalf("FixChatBundle failed: %v", err)
	}

	fixedStr := string(fixed)
	closeIdx := strings.Index(fixedStr, `\u003c/think\u003e`)
	if closeIdx == -1 {
		closeIdx = strings.Index(fixedStr, `</think>`)
	}
	doneIdx := strings.Index(fixedStr, `[DONE]`)
	if closeIdx == -1 {
		t.Fatalf("expected explicit </think> close chunk before done, got %s", fixedStr)
	}
	if doneIdx == -1 {
		t.Fatalf("expected [DONE] to remain in stream, got %s", fixedStr)
	}
	if closeIdx > doneIdx {
		t.Fatalf("expected </think> close chunk before [DONE], got %s", fixedStr)
	}
	if state.InThinkingTag {
		t.Fatalf("expected thinking state cleared after [DONE] handling")
	}
}

func TestBridgeChatFromResponsesBundleEmitsCursorToolCallTiming(t *testing.T) {
	state := &FinalizeState{}
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_123","object":"response","model":"gpt-5","status":"in_progress","output":[]}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_123","call_id":"call_123","name":"read_file","arguments":""}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"path\":\"README.md\"}"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_123","object":"response","model":"gpt-5","status":"completed","output":[{"type":"function_call","id":"fc_123","call_id":"call_123","name":"read_file","arguments":"{\"path\":\"README.md\"}"}],"usage":{"input_tokens":11,"output_tokens":7}}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n"))

	bridged, err := BridgeChatFromResponsesBundle(bundle, "cursor-gpt-5", state)
	if err != nil {
		t.Fatalf("BridgeChatFromResponsesBundle failed: %v", err)
	}

	payloads := decodeChatChunkPayloads(t, bridged)
	if len(payloads) != 4 {
		t.Fatalf("expected 4 json chat chunks, got %d in %s", len(payloads), string(bridged))
	}
	if !strings.Contains(string(bridged), "data: [DONE]") {
		t.Fatalf("expected [DONE] passthrough, got %s", string(bridged))
	}

	firstDelta := payloadChoiceDelta(t, payloads[0])
	if firstDelta["role"] != "assistant" {
		t.Fatalf("expected assistant role start chunk, got %#v", firstDelta)
	}

	secondDelta := payloadChoiceDelta(t, payloads[1])
	toolCalls := secondDelta["tool_calls"].([]interface{})
	toolCall := toolCalls[0].(map[string]interface{})
	if toolCall["id"] != "call_123" {
		t.Fatalf("expected tool call id from added event, got %#v", toolCall)
	}
	function := toolCall["function"].(map[string]interface{})
	if function["name"] != "read_file" || function["arguments"] != "" {
		t.Fatalf("expected tool call start chunk with empty args, got %#v", function)
	}

	thirdDelta := payloadChoiceDelta(t, payloads[2])
	argCalls := thirdDelta["tool_calls"].([]interface{})
	argFunction := argCalls[0].(map[string]interface{})["function"].(map[string]interface{})
	if argFunction["arguments"] != `{"path":"README.md"}` {
		t.Fatalf("expected argument delta passthrough, got %#v", argFunction)
	}

	finalChoice := payloadChoice(t, payloads[3])
	if finalChoice["finish_reason"] != "tool_calls" {
		t.Fatalf("expected finish_reason tool_calls, got %#v", finalChoice["finish_reason"])
	}
	usage := payloads[3]["usage"].(map[string]interface{})
	if usage["prompt_tokens"] != float64(11) || usage["completion_tokens"] != float64(7) || usage["total_tokens"] != float64(18) {
		t.Fatalf("expected chat usage mapped from responses usage, got %#v", usage)
	}

	if !state.OpenAI2ChatStarted || !state.OpenAI2ChatSawToolCall || !strings.HasPrefix(state.OpenAI2ChatResponseID, "chatcmpl-") {
		t.Fatalf("expected bridge state tracked, got %#v", state)
	}
}

func TestBridgeChatFromResponsesBundleFallsBackToLastSeenUsage(t *testing.T) {
	state := &FinalizeState{}
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_456","object":"response","model":"gpt-5","status":"in_progress","usage":{"input_tokens":9,"output_tokens":4}}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_456","object":"response","model":"gpt-5","status":"completed"}}`,
		"",
	}, "\n"))

	bridged, err := BridgeChatFromResponsesBundle(bundle, "cursor-gpt-5", state)
	if err != nil {
		t.Fatalf("BridgeChatFromResponsesBundle failed: %v", err)
	}

	payloads := decodeChatChunkPayloads(t, bridged)
	finalPayload := payloads[len(payloads)-1]
	usage := finalPayload["usage"].(map[string]interface{})
	if usage["prompt_tokens"] != float64(9) || usage["completion_tokens"] != float64(4) || usage["total_tokens"] != float64(13) {
		t.Fatalf("expected final usage to fall back to latest seen usage, got %#v", usage)
	}
}

func decodeChatChunkPayloads(t *testing.T, bundle []byte) []map[string]interface{} {
	t.Helper()

	chunks := splitSSEBundle(bundle)
	payloads := make([]map[string]interface{}, 0, len(chunks))
	for _, chunk := range chunks {
		_, data, ok := parseSSEChunk(chunk)
		if !ok || data == "[DONE]" {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			t.Fatalf("failed to decode SSE payload %q: %v", data, err)
		}
		payloads = append(payloads, payload)
	}
	return payloads
}

func payloadChoice(t *testing.T, payload map[string]interface{}) map[string]interface{} {
	t.Helper()

	choices := payload["choices"].([]interface{})
	return choices[0].(map[string]interface{})
}

func payloadChoiceDelta(t *testing.T, payload map[string]interface{}) map[string]interface{} {
	t.Helper()

	choice := payloadChoice(t, payload)
	return choice["delta"].(map[string]interface{})
}
