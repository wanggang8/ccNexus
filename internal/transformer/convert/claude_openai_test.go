package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestClaudeReqToOpenAI_ToolResultMixedContent_DegradesPredictably(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4",
		"messages": [
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_123",
						"content": [
							{"type": "text", "text": "stdout: ok"},
							{
								"type": "image",
								"source": {
									"type": "base64",
									"media_type": "image/png",
									"data": "aGVsbG8="
								}
							}
						]
					}
				]
			}
		],
		"stream": false
	}`

	result, err := ClaudeReqToOpenAI([]byte(claudeReq), "gpt-4o")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI failed: %v", err)
	}

	var got transformer.OpenAIRequest
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if len(got.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got.Messages))
	}

	msg := got.Messages[0]
	if msg.Role != "tool" {
		t.Fatalf("expected tool role, got %q", msg.Role)
	}
	if msg.ToolCallID != "toolu_123" {
		t.Fatalf("expected tool_call_id toolu_123, got %q", msg.ToolCallID)
	}

	content, ok := msg.Content.(string)
	if !ok {
		t.Fatalf("expected tool content to degrade into string, got %T (%#v)", msg.Content, msg.Content)
	}
	if !strings.Contains(content, "stdout: ok") {
		t.Fatalf("expected degraded tool result to preserve text payload, got %q", content)
	}
}

func TestOpenAIReqToClaude_MergesConsecutiveToolMessagesIntoSingleUserMessage(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4.1",
		"messages": [
			{"role": "user", "content": "Solve this"},
			{
				"role": "assistant",
				"tool_calls": [
					{
						"id": "call_1",
						"type": "function",
						"function": {"name": "read_file", "arguments": "{\"path\":\"/tmp/a.txt\"}"}
					},
					{
						"id": "call_2",
						"type": "function",
						"function": {"name": "list_dir", "arguments": "{\"path\":\"/tmp\"}"}
					}
				]
			},
			{"role": "tool", "tool_call_id": "call_1", "content": "file content A"},
			{"role": "tool", "tool_call_id": "call_2", "content": "dir content B"}
		],
		"stream": false
	}`

	result, err := OpenAIReqToClaude([]byte(openaiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAIReqToClaude failed: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	messages, ok := got["messages"].([]interface{})
	if !ok {
		t.Fatalf("expected messages array, got %#v", got["messages"])
	}

	if len(messages) != 3 {
		t.Fatalf("expected 3 Claude messages (user -> assistant(tool_use) -> user(tool_result)), got %d", len(messages))
	}

	lastMsg := messages[2].(map[string]interface{})
	if lastMsg["role"] != "user" {
		t.Fatalf("expected merged tool result message to be user role, got %#v", lastMsg["role"])
	}

	contentBlocks, ok := lastMsg["content"].([]interface{})
	if !ok {
		t.Fatalf("expected merged tool result content array, got %#v", lastMsg["content"])
	}
	if len(contentBlocks) != 2 {
		t.Fatalf("expected 2 tool_result blocks, got %d", len(contentBlocks))
	}

	block1 := contentBlocks[0].(map[string]interface{})
	block2 := contentBlocks[1].(map[string]interface{})

	if block1["type"] != "tool_result" || block1["tool_use_id"] != "call_1" {
		t.Fatalf("unexpected first tool_result block: %#v", block1)
	}
	if block2["type"] != "tool_result" || block2["tool_use_id"] != "call_2" {
		t.Fatalf("unexpected second tool_result block: %#v", block2)
	}
}

func TestOpenAIReqToClaude_ToolMessagesSeparatedByAssistantMessage_NotMerged(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4.1",
		"messages": [
			{"role": "user", "content": "Solve this"},
			{
				"role": "assistant",
				"tool_calls": [
					{
						"id": "call_1",
						"type": "function",
						"function": {"name": "read_file", "arguments": "{\"path\":\"/tmp/a.txt\"}"}
					}
				]
			},
			{"role": "tool", "tool_call_id": "call_1", "content": "file content A"},
			{"role": "assistant", "content": "I have processed the first tool result."},
			{"role": "tool", "tool_call_id": "call_2", "content": "late tool content"}
		],
		"stream": false
	}`

	result, err := OpenAIReqToClaude([]byte(openaiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAIReqToClaude failed: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	messages, ok := got["messages"].([]interface{})
	if !ok {
		t.Fatalf("expected messages array, got %#v", got["messages"])
	}

	if len(messages) != 5 {
		t.Fatalf("expected 5 messages, got %d: %#v", len(messages), messages)
	}

	msg3 := messages[2].(map[string]interface{})
	msg4 := messages[3].(map[string]interface{})
	msg5 := messages[4].(map[string]interface{})

	if msg3["role"] != "user" {
		t.Fatalf("expected message 3 role=user, got %#v", msg3["role"])
	}
	if msg4["role"] != "assistant" {
		t.Fatalf("expected message 4 role=assistant, got %#v", msg4["role"])
	}
	if msg5["role"] != "user" {
		t.Fatalf("expected message 5 role=user, got %#v", msg5["role"])
	}

	content3, ok := msg3["content"].([]interface{})
	if !ok || len(content3) != 1 {
		t.Fatalf("expected message 3 to contain one tool_result block, got %#v", msg3["content"])
	}
	block3 := content3[0].(map[string]interface{})
	if block3["type"] != "tool_result" || block3["tool_use_id"] != "call_1" {
		t.Fatalf("unexpected message 3 tool_result block: %#v", block3)
	}

	content5, ok := msg5["content"].([]interface{})
	if !ok || len(content5) != 1 {
		t.Fatalf("expected message 5 to contain one tool_result block, got %#v", msg5["content"])
	}
	block5 := content5[0].(map[string]interface{})
	if block5["type"] != "tool_result" || block5["tool_use_id"] != "call_2" {
		t.Fatalf("unexpected message 5 tool_result block: %#v", block5)
	}
}

func TestOpenAIReqToClaude_ToolCallInvalidArgumentsFallbackToEmptyObject(t *testing.T) {
	openaiReq := `{
		"model": "gpt-4.1",
		"messages": [
			{
				"role": "assistant",
				"tool_calls": [
					{
						"id": "call_invalid",
						"type": "function",
						"function": {"name": "read_file", "arguments": "{not-json"}
					}
				]
			}
		],
		"stream": false
	}`

	result, err := OpenAIReqToClaude([]byte(openaiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAIReqToClaude failed: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	messages := got["messages"].([]interface{})
	if len(messages) != 1 {
		t.Fatalf("expected one assistant message, got %#v", messages)
	}
	assistant := messages[0].(map[string]interface{})
	content := assistant["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected one content block, got %#v", content)
	}
	toolUse := content[0].(map[string]interface{})
	input, ok := toolUse["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected fallback input object, got %#v", toolUse["input"])
	}
	if len(input) != 0 {
		t.Fatalf("expected empty object fallback for invalid tool arguments, got %#v", input)
	}
}

func TestMapClaudeStopToOpenAIFinish_Variants(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "tool use", in: "tool_use", want: "tool_calls"},
		{name: "max tokens", in: "max_tokens", want: "length"},
		{name: "end turn", in: "end_turn", want: "stop"},
		{name: "stop sequence", in: "stop_sequence", want: "stop"},
		{name: "unknown defaults to stop", in: "vendor_custom", want: "stop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapClaudeStopToOpenAIFinish(tt.in); got != tt.want {
				t.Fatalf("mapClaudeStopToOpenAIFinish(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMapOpenAIFinishToClaudeStop_Variants(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "tool calls", in: "tool_calls", want: "tool_use"},
		{name: "legacy function call", in: "function_call", want: "tool_use"},
		{name: "length", in: "length", want: "max_tokens"},
		{name: "content filter", in: "content_filter", want: "end_turn"},
		{name: "stop", in: "stop", want: "end_turn"},
		{name: "unknown defaults to end turn", in: "vendor_custom", want: "end_turn"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapOpenAIFinishToClaudeStop(tt.in); got != tt.want {
				t.Fatalf("mapOpenAIFinishToClaudeStop(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestOpenAIRespToClaudeWithThinking(t *testing.T) {
	openaiResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-3.5-turbo",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "<think>Thinking about the weather...</think>\n\nIt is a nice day."
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 9,
			"completion_tokens": 12,
			"total_tokens": 21
		}
	}`

	claudeRespBytes, err := OpenAIRespToClaude([]byte(openaiResp))
	if err != nil {
		t.Fatalf("OpenAIRespToClaude failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(claudeRespBytes, &claudeResp); err != nil {
		t.Fatalf("Failed to unmarshal Claude response: %v", err)
	}

	content, ok := claudeResp["content"].([]interface{})
	if !ok {
		t.Fatalf("Expected content to be an array, got %T", claudeResp["content"])
	}

	if len(content) != 2 {
		t.Fatalf("Expected 2 content blocks, got %d", len(content))
	}

	block1 := content[0].(map[string]interface{})
	if block1["type"] != "thinking" {
		t.Errorf("Expected first block to be thinking, got %v", block1["type"])
	}
	if block1["thinking"] != "Thinking about the weather..." {
		t.Errorf("Unexpected thinking content: %v", block1["thinking"])
	}

	block2 := content[1].(map[string]interface{})
	if block2["type"] != "text" {
		t.Errorf("Expected second block to be text, got %v", block2["type"])
	}
	if strings.TrimSpace(block2["text"].(string)) != "It is a nice day." {
		t.Errorf("Unexpected text content: %v", block2["text"])
	}
}

func TestOpenAIRespToClaude_WithReasoningContentMessageField(t *testing.T) {
	openaiResp := `{
		"id": "chatcmpl-reasoning",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-4.1",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"reasoning_content": "Need to inspect the repository first.",
				"content": "Final answer"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 8,
			"total_tokens": 18
		}
	}`

	claudeRespBytes, err := OpenAIRespToClaude([]byte(openaiResp))
	if err != nil {
		t.Fatalf("OpenAIRespToClaude failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(claudeRespBytes, &claudeResp); err != nil {
		t.Fatalf("Failed to unmarshal Claude response: %v", err)
	}

	content, ok := claudeResp["content"].([]interface{})
	if !ok {
		t.Fatalf("Expected content array, got %T", claudeResp["content"])
	}
	if len(content) != 2 {
		t.Fatalf("Expected thinking block plus text block, got %d blocks: %#v", len(content), content)
	}

	thinkingBlock := content[0].(map[string]interface{})
	if thinkingBlock["type"] != "thinking" {
		t.Fatalf("Expected first block type thinking, got %#v", thinkingBlock["type"])
	}
	if thinkingBlock["thinking"] != "Need to inspect the repository first." {
		t.Fatalf("Unexpected thinking block content: %#v", thinkingBlock["thinking"])
	}

	textBlock := content[1].(map[string]interface{})
	if textBlock["type"] != "text" {
		t.Fatalf("Expected second block type text, got %#v", textBlock["type"])
	}
	if textBlock["text"] != "Final answer" {
		t.Fatalf("Unexpected text block content: %#v", textBlock["text"])
	}
}

func TestOpenAIRespToClaudeWithMultipleThinking(t *testing.T) {
	openaiResp := `{
		"id": "chatcmpl-456",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-3.5-turbo",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "A<think>X</think>B<think>Y</think>C"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 9,
			"completion_tokens": 12,
			"total_tokens": 21
		}
	}`

	claudeRespBytes, err := OpenAIRespToClaude([]byte(openaiResp))
	if err != nil {
		t.Fatalf("OpenAIRespToClaude failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(claudeRespBytes, &claudeResp); err != nil {
		t.Fatalf("Failed to unmarshal Claude response: %v", err)
	}

	content, ok := claudeResp["content"].([]interface{})
	if !ok {
		t.Fatalf("Expected content to be an array, got %T", claudeResp["content"])
	}
	if len(content) != 5 {
		t.Fatalf("Expected 5 content blocks, got %d", len(content))
	}

	expect := []map[string]string{
		{"type": "text", "text": "A"},
		{"type": "thinking", "thinking": "X"},
		{"type": "text", "text": "B"},
		{"type": "thinking", "thinking": "Y"},
		{"type": "text", "text": "C"},
	}

	for i, exp := range expect {
		block := content[i].(map[string]interface{})
		if block["type"] != exp["type"] {
			t.Fatalf("Block %d type mismatch: %v", i, block["type"])
		}
		if exp["type"] == "text" && block["text"] != exp["text"] {
			t.Fatalf("Block %d text mismatch: %v", i, block["text"])
		}
		if exp["type"] == "thinking" && block["thinking"] != exp["thinking"] {
			t.Fatalf("Block %d thinking mismatch: %v", i, block["thinking"])
		}
	}
}

func TestClaudeStreamToOpenAI_EmitsToolShellAtToolUseStart(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "msg_1"
	ctx.ModelName = "gpt-4"

	toolStart := `event: content_block_start
data: {"type":"content_block_start","content_block":{"type":"tool_use","id":"toolu_1","name":"read_file","input":{}}}

`
	argDelta := `event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"path\":\"/tmp/a\"}"}}

`

	out1, err := ClaudeStreamToOpenAI([]byte(toolStart), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("tool start failed: %v", err)
	}
	if out1 == nil {
		t.Fatal("expected tool chunk at tool_use start, got nil")
	}
	resultStart := string(out1)
	if !strings.Contains(resultStart, `"id":"toolu_1"`) {
		t.Fatalf("expected tool shell id at tool_use start, got %s", resultStart)
	}
	if !strings.Contains(resultStart, `"name":"read_file"`) {
		t.Fatalf("expected tool shell name at tool_use start, got %s", resultStart)
	}
	if !strings.Contains(resultStart, `"function":{"arguments":"","name":"read_file"}`) {
		t.Fatalf("expected empty tool arguments at tool_use start, got %s", resultStart)
	}

	out2, err := ClaudeStreamToOpenAI([]byte(argDelta), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("tool delta failed: %v", err)
	}
	resultStr := string(out2)
	if !strings.Contains(resultStr, `"function":{"arguments":"{\"path\":\"/tmp/a\"}"}`) {
		t.Fatalf("expected argument delta payload on first argument delta, got %s", resultStr)
	}
}

func TestOpenAIStreamToClaudeWithThinking(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"

	chunks := []string{
		`data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"<think>"}}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"Thinking"}}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"..."}}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"</think>"}}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"Hello!"}}]}`,
		`data: [DONE]`,
	}

	var allEvents []string
	for _, chunk := range chunks {
		events, err := OpenAIStreamToClaude([]byte(chunk), ctx)
		if err != nil {
			t.Fatalf("OpenAIStreamToClaude failed: %v", err)
		}
		if events != nil {
			allEvents = append(allEvents, string(events))
		}
	}

	fullEvents := strings.Join(allEvents, "")

	assertContains(t, fullEvents, "\"type\":\"thinking\"", "Expected thinking block start, but not found")
	if !strings.Contains(fullEvents, "\"thinking\":\"Thinking...\"") {
		if !(strings.Contains(fullEvents, "\"thinking\":\"Thinking\"") && strings.Contains(fullEvents, "\"thinking\":\"...\"")) {
			t.Errorf("Expected thinking delta chunks, but not found")
		}
	}
	assertContains(t, fullEvents, "\"type\":\"content_block_stop\"", "Expected content block stop, but not found")
	assertContains(t, fullEvents, "\"text\":\"Hello!\"", "Expected text delta 'Hello!', but not found")
}

func TestOpenAIStreamToClaudeWithReasoningContentChunkBoundary(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"

	chunks := []string{
		`data: {"id":"1","choices":[{"index":0,"delta":{"reasoning_content":"Thinking about"}}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"reasoning_content":" the answer","content":"Final answer"},"finish_reason":"stop"}]}`,
	}

	var allEvents []string
	for _, chunk := range chunks {
		events, err := OpenAIStreamToClaude([]byte(chunk), ctx)
		if err != nil {
			t.Fatalf("OpenAIStreamToClaude failed: %v", err)
		}
		if events != nil {
			allEvents = append(allEvents, string(events))
		}
	}

	fullEvents := strings.Join(allEvents, "")
	assertContains(t, fullEvents, `"type":"thinking"`, "Expected thinking block start for reasoning_content")
	assertContains(t, fullEvents, `"thinking":"Thinking about"`, "Expected first reasoning_content delta to be preserved")
	assertContains(t, fullEvents, `"thinking":" the answer"`, "Expected second reasoning_content delta to be preserved")
	assertContains(t, fullEvents, `"text":"Final answer"`, "Expected final assistant text to be preserved")
}

func TestOpenAIStreamToClaudeWithThinkingSingleChunk(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"

	chunk := `data: {"id":"1","choices":[{"index":0,"delta":{"content":"<think>Reasoning</think>Hello!"}}]}`

	events, err := OpenAIStreamToClaude([]byte(chunk), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToClaude failed: %v", err)
	}

	fullEvents := string(events)
	assertContains(t, fullEvents, "\"type\":\"thinking\"", "Expected thinking block start")
	assertContains(t, fullEvents, "\"thinking\":\"Reasoning\"", "Expected thinking delta 'Reasoning'")
	assertContains(t, fullEvents, "\"type\":\"content_block_stop\"", "Expected content block stop")
	assertContains(t, fullEvents, "\"type\":\"text\"", "Expected text block start")
	assertContains(t, fullEvents, "\"text\":\"Hello!\"", "Expected text delta 'Hello!'")
}

func TestOpenAIStreamToClaudeWithThinkingSplitTag(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"

	chunks := []string{
		`data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"<thi"}}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"nk>Thinking"}}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"..."}}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"</think>"}}]}`,
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"Hello!"}}]}`,
		`data: [DONE]`,
	}

	var allEvents []string
	for _, chunk := range chunks {
		events, err := OpenAIStreamToClaude([]byte(chunk), ctx)
		if err != nil {
			t.Fatalf("OpenAIStreamToClaude failed: %v", err)
		}
		if events != nil {
			allEvents = append(allEvents, string(events))
		}
	}

	fullEvents := strings.Join(allEvents, "")
	assertContains(t, fullEvents, "\"type\":\"thinking\"", "Expected thinking block start, but not found")
	assertNotContains(t, fullEvents, "<think>", "Unexpected think tag leaked into output")
	assertNotContains(t, fullEvents, "</think>", "Unexpected think tag leaked into output")
}

func TestOpenAIStreamToClaudeWithThinkingMissingCloseDone(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"

	chunks := []string{
		`data: {"id":"1","choices":[{"index":0,"delta":{"content":"<think>this is some thinking content"}}]}`,
		`data: [DONE]`,
	}

	var allEvents []string
	for _, chunk := range chunks {
		events, err := OpenAIStreamToClaude([]byte(chunk), ctx)
		if err != nil {
			t.Fatalf("OpenAIStreamToClaude failed: %v", err)
		}
		if events != nil {
			allEvents = append(allEvents, string(events))
		}
	}

	fullEvents := strings.Join(allEvents, "")
	assertNotContains(t, fullEvents, "<think>", "Unexpected think tag leaked into output")
	assertNotContains(t, fullEvents, "</think>", "Unexpected think tag leaked into output")
	assertContains(t, fullEvents, "\"type\":\"thinking\"", "Expected thinking block for missing close")
	assertContains(t, fullEvents, "\"thinking\":\"this is some thinking content\"", "Expected thinking delta 'this is some thinking content', but not found")
	assertContains(t, fullEvents, "\"type\":\"content_block_stop\"", "Expected thinking block stop, but not found")
}

func assertContains(t *testing.T, haystack, needle, msg string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Error(msg)
	}
}

func assertNotContains(t *testing.T, haystack, needle, msg string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Error(msg)
	}
}

func TestClaudeReqToOpenAIWithToolUseAndResult(t *testing.T) {
	claudeReq := `{
		"model": "claude-3-opus-20240229",
		"messages": [
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "toolu_1", "name": "read_file", "input": {"path": "/tmp/a"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "toolu_1", "content": "ok"}
			]}
		],
		"max_tokens": 1024
	}`

	openaiReqBytes, err := ClaudeReqToOpenAI([]byte(claudeReq), "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI failed: %v", err)
	}

	var openaiReq transformer.OpenAIRequest
	if err := json.Unmarshal(openaiReqBytes, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal OpenAI request: %v", err)
	}

	if len(openaiReq.Messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(openaiReq.Messages))
	}

	assistantMsg := openaiReq.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Fatalf("Expected assistant role, got %s", assistantMsg.Role)
	}
	if len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(assistantMsg.ToolCalls))
	}
	if assistantMsg.ToolCalls[0].ID != "toolu_1" || assistantMsg.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("Unexpected tool call: %#v", assistantMsg.ToolCalls[0])
	}

	toolMsg := openaiReq.Messages[2]
	if toolMsg.Role != "tool" {
		t.Fatalf("Expected tool role, got %s", toolMsg.Role)
	}
	if toolMsg.ToolCallID != "toolu_1" {
		t.Fatalf("Unexpected tool_call_id: %s", toolMsg.ToolCallID)
	}
	if toolMsg.Content != "ok" {
		t.Fatalf("Unexpected tool content: %#v", toolMsg.Content)
	}
}

func TestClaudeReqToOpenAI_PreservesToolStrictFlag(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [{"role": "user", "content": "Hello"}],
		"tools": [
			{"name": "read_file", "description": "Read file", "input_schema": {"type": "object"}, "strict": true}
		]
	}`

	result, err := ClaudeReqToOpenAI([]byte(claudeReq), "gpt-4o")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI failed: %v", err)
	}

	var openaiReq map[string]interface{}
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	tools, ok := openaiReq["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %#v", openaiReq["tools"])
	}
	tool := tools[0].(map[string]interface{})
	fn, ok := tool["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected function wrapper, got %#v", tool)
	}
	if fn["strict"] != true {
		t.Fatalf("expected strict=true to be preserved in function wrapper, got %#v", fn["strict"])
	}
}

func TestClaudeReqToOpenAISkipsInvalidToolBlocks(t *testing.T) {
	claudeReq := `{
		"model": "claude-3-opus-20240229",
		"messages": [
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": 123, "name": false, "input": {"path": "/tmp/a"}},
				{"type": "tool_result", "tool_use_id": 456, "content": "bad"},
				{"type": "text", "text": "ok"}
			]}
		],
		"max_tokens": 128
	}`

	openaiReqBytes, err := ClaudeReqToOpenAI([]byte(claudeReq), "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI failed: %v", err)
	}

	var openaiReq transformer.OpenAIRequest
	if err := json.Unmarshal(openaiReqBytes, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal OpenAI request: %v", err)
	}

	if len(openaiReq.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(openaiReq.Messages))
	}
	if openaiReq.Messages[0].Content != "ok" {
		t.Fatalf("Unexpected content: %#v", openaiReq.Messages[0].Content)
	}
	if len(openaiReq.Messages[0].ToolCalls) != 0 {
		t.Fatalf("Expected no tool calls, got %d", len(openaiReq.Messages[0].ToolCalls))
	}
}

func TestClaudeReqToOpenAIThinkingOnly(t *testing.T) {
	claudeReq := `{
		"model": "claude-3-opus-20240229",
		"messages": [
			{
				"role": "user",
				"content": "Hello"
			},
			{
				"role": "assistant",
				"content": [
					{
						"type": "thinking",
						"thinking": "I should say hello back"
					}
				]
			},
			{
				"role": "user",
				"content": "How are you?"
			}
		],
		"max_tokens": 1024
	}`

	openaiReqBytes, err := ClaudeReqToOpenAI([]byte(claudeReq), "gpt-4")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI failed: %v", err)
	}

	var openaiReq struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(openaiReqBytes, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal OpenAI request: %v", err)
	}

	// The assistant message with only thinking should now have a placeholder
	if len(openaiReq.Messages) != 3 {
		t.Errorf("Expected 3 messages (user, assistant, user), got %d", len(openaiReq.Messages))
		for i, m := range openaiReq.Messages {
			t.Logf("Message %d: %s - %s", i, m.Role, m.Content)
		}
	} else {
		if openaiReq.Messages[1].Role != "assistant" || openaiReq.Messages[1].Content != "(thinking...)" {
			t.Errorf("Expected placeholder for assistant message, got %s: %s", openaiReq.Messages[1].Role, openaiReq.Messages[1].Content)
		}
	}
}
