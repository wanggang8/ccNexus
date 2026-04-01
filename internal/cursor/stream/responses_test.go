package stream

import (
	"strings"
	"testing"

	cursorcache "github.com/lich0821/ccNexus/internal/cursor/cache"
)

func newResponsesState() *FinalizeState {
	return &FinalizeState{
		ResponsesTools:  make(map[int]*ResponseToolState),
		ResponsesOutput: make([]map[string]interface{}, 0),
	}
}

func TestFixResponsesBundleRewritesEvents(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress","model":"upstream-model"}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","usage":{"input_tokens":7,"output_tokens":5,"total_tokens":12}}}`,
		"",
	}, "\n"))

	fixed, err := FixResponsesBundle(bundle, "cursor-model", "cx_resp_openai", nil, cursorcache.NewThinkingCache(), newResponsesState())
	if err != nil {
		t.Fatalf("FixResponsesBundle failed: %v", err)
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

func TestFixResponsesBundlePrefixesCreatedWhenMissing(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
	}, "\n"))

	fixed, err := FixResponsesBundle(bundle, "cursor-model", "cx_resp_openai", nil, cursorcache.NewThinkingCache(), newResponsesState())
	if err != nil {
		t.Fatalf("FixResponsesBundle failed: %v", err)
	}

	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, "event: response.created") {
		t.Fatalf("expected response.created prefix, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"delta":"hello"`) {
		t.Fatalf("expected output_text delta preserved, got %s", fixedStr)
	}
}

func TestFixResponsesBundleHandlesMultilineData(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		"event: response.output_text.delta",
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}",
		"data: {\"extra\":\"line\"}",
		"",
	}, "\n"))

	fixed, err := FixResponsesBundle(bundle, "cursor-model", "cx_resp_openai", nil, cursorcache.NewThinkingCache(), newResponsesState())
	if err != nil {
		t.Fatalf("FixResponsesBundle failed: %v", err)
	}
	if !strings.Contains(string(fixed), "event: response.output_text.delta") {
		t.Fatalf("expected event preserved for multiline data, got %s", string(fixed))
	}
}

func TestFixResponsesBundlePreservesNativeResponsesShape(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress","model":"upstream-model","output":[]}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","status":"in_progress","role":"assistant","content":[]}}`,
		"",
	}, "\n"))

	fixed, err := FixResponsesBundle(bundle, "cursor-model", "cx_resp_openai2", nil, cursorcache.NewThinkingCache(), newResponsesState())
	if err != nil {
		t.Fatalf("FixResponsesBundle failed: %v", err)
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

func TestFixResponsesBundleReconstructsCompletedOutput(t *testing.T) {
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

	fixed, err := FixResponsesBundle(bundle, "cursor-model", "cx_resp_openai", nil, cursorcache.NewThinkingCache(), newResponsesState())
	if err != nil {
		t.Fatalf("FixResponsesBundle failed: %v", err)
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

func TestFixResponsesBundleOmitsDoneAndAddsFinalizeEvents(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
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
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed"}}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n"))

	fixed, err := FixResponsesBundle(bundle, "cursor-model", "cx_resp_openai", nil, cursorcache.NewThinkingCache(), newResponsesState())
	if err != nil {
		t.Fatalf("FixResponsesBundle failed: %v", err)
	}

	fixedStr := string(fixed)
	doneReasoningIndex := strings.Index(fixedStr, "event: response.reasoning_summary_text.done")
	doneTextIndex := strings.Index(fixedStr, "event: response.output_text.done")
	doneArgsIndex := strings.Index(fixedStr, "event: response.function_call_arguments.done")
	completedIndex := strings.Index(fixedStr, "event: response.completed")
	if doneReasoningIndex == -1 || doneTextIndex == -1 || doneArgsIndex == -1 {
		t.Fatalf("expected finalize done events, got %s", fixedStr)
	}
	if completedIndex == -1 {
		t.Fatalf("expected response.completed event, got %s", fixedStr)
	}
	if !(doneReasoningIndex < completedIndex && doneTextIndex < completedIndex && doneArgsIndex < completedIndex) {
		t.Fatalf("expected done events before completed, got %s", fixedStr)
	}
	if strings.Contains(fixedStr, "[DONE]") {
		t.Fatalf("expected responses stream to omit [DONE], got %s", fixedStr)
	}
}

func TestFixResponsesBundleStoresThinkingCacheFromCompletedOutput(t *testing.T) {
	cacheStore := cursorcache.NewThinkingCache()
	cacheMessages := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
		{"role": "user", "content": "continue"},
	}
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[]}}`,
		"",
		`data: {"type":"response.reasoning_summary_text.delta","delta":"stream think"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed"}}`,
		"",
	}, "\n"))

	fixed, err := FixResponsesBundle(bundle, "cursor-model", "cx_resp_openai", cacheMessages, cacheStore, newResponsesState())
	if err != nil {
		t.Fatalf("FixResponsesBundle failed: %v", err)
	}

	if !strings.Contains(string(fixed), "event: response.completed") {
		t.Fatalf("expected response.completed event, got %s", string(fixed))
	}
	if len(cacheStore.Store) == 0 {
		t.Fatalf("expected thinking cache store from completed response")
	}
}

func TestFixResponsesBundleIgnoresToolArgumentDeltaWithoutTool(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.function_call_arguments.delta","delta":"{\"path\":\"README.md\"}"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed"}}`,
		"",
	}, "\n"))

	fixed, err := FixResponsesBundle(bundle, "cursor-model", "cx_resp_openai", nil, cursorcache.NewThinkingCache(), newResponsesState())
	if err != nil {
		t.Fatalf("FixResponsesBundle failed: %v", err)
	}

	fixedStr := string(fixed)
	if strings.Contains(fixedStr, "\"call_id\"") {
		t.Fatalf("did not expect call_id when tool not started, got %s", fixedStr)
	}
	if strings.Contains(fixedStr, "\"id\":\"fc_") || strings.Contains(fixedStr, "\"id\":\"call_") {
		t.Fatalf("did not expect synthetic tool ids when tool not started, got %s", fixedStr)
	}
}

func TestFixResponsesBundleSkipsEmptyToolArgumentsDone(t *testing.T) {
	state := newResponsesState()
	state.ResponsesTools[0] = &ResponseToolState{
		ID:        "fc_1",
		CallID:    "call_1",
		Name:      "read_file",
		Arguments: "{\"path\":\"README.md\"}",
		Active:    true,
	}
	bundle := []byte(strings.Join([]string{
		`data: {"type":"response.function_call_arguments.done","arguments":""}`,
		"",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":""}}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed"}}`,
		"",
	}, "\n"))

	fixed, err := FixResponsesBundle(bundle, "cursor-model", "cx_resp_openai", nil, cursorcache.NewThinkingCache(), state)
	if err != nil {
		t.Fatalf("FixResponsesBundle failed: %v", err)
	}

	if strings.Contains(string(fixed), "\\\"arguments\\\":\\\"{\\\\\\\"path\\\\\\\":\\\\\\\"README.md\\\\\\\"}\\\"") {
		t.Fatalf("did not expect empty done to preserve arguments in output item, got %s", string(fixed))
	}
}
