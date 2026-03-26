package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestOpenAI2RespToClaudeWithThinking(t *testing.T) {
	openai2Resp := `{
		"id": "resp_1",
		"object": "response",
		"status": "completed",
		"output": [{
			"type": "message",
			"role": "assistant",
			"content": [{
				"type": "output_text",
				"text": "<think>Reason</think>Answer"
			}]
		}],
		"usage": {
			"input_tokens": 3,
			"output_tokens": 5,
			"total_tokens": 8
		}
	}`

	claudeRespBytes, err := OpenAI2RespToClaude([]byte(openai2Resp))
	if err != nil {
		t.Fatalf("OpenAI2RespToClaude failed: %v", err)
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
	if content[0].(map[string]interface{})["type"] != "thinking" {
		t.Fatalf("Expected first block thinking, got %v", content[0])
	}
	if content[1].(map[string]interface{})["type"] != "text" {
		t.Fatalf("Expected second block text, got %v", content[1])
	}
}

func TestOpenAI2StreamToClaudeWithThinking(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"

	chunks := []string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		`data: {"type":"response.output_text.delta","delta":"<think>Reason</think>Hello"}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed"}}`,
		`data: [DONE]`,
	}

	var allEvents []string
	for _, chunk := range chunks {
		events, err := OpenAI2StreamToClaude([]byte(chunk), ctx)
		if err != nil {
			t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
		}
		if events != nil {
			allEvents = append(allEvents, string(events))
		}
	}

	fullEvents := strings.Join(allEvents, "")
	if !strings.Contains(fullEvents, "\"type\":\"thinking\"") {
		t.Fatalf("Expected thinking block start, but not found")
	}
	if !strings.Contains(fullEvents, "\"thinking\":\"Reason\"") {
		t.Fatalf("Expected thinking delta 'Reason', but not found")
	}
	if !strings.Contains(fullEvents, "\"text\":\"Hello\"") {
		t.Fatalf("Expected text delta 'Hello', but not found")
	}
	if strings.Contains(fullEvents, "<think>") || strings.Contains(fullEvents, "</think>") {
		t.Fatalf("Unexpected think tags leaked into output")
	}
}

func TestOpenAI2StreamToClaudeCompletesWithoutDone(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"

	chunks := []string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed"}}`,
	}

	var allEvents []string
	for _, chunk := range chunks {
		events, err := OpenAI2StreamToClaude([]byte(chunk), ctx)
		if err != nil {
			t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
		}
		if events != nil {
			allEvents = append(allEvents, string(events))
		}
	}

	fullEvents := strings.Join(allEvents, "")
	if !strings.Contains(fullEvents, "\"type\":\"message_delta\"") {
		t.Fatalf("Expected message_delta in transformed events, got: %s", fullEvents)
	}
	if !strings.Contains(fullEvents, "event: message_stop") {
		t.Fatalf("Expected message_stop when response.completed arrives without [DONE], got: %s", fullEvents)
	}
}

func TestOpenAI2StreamToClaudePropagatesUsageFromCompleted(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"

	chunks := []string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","usage":{"input_tokens":7,"output_tokens":3,"total_tokens":10}}}`,
	}

	var allEvents []string
	for _, chunk := range chunks {
		events, err := OpenAI2StreamToClaude([]byte(chunk), ctx)
		if err != nil {
			t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
		}
		if events != nil {
			allEvents = append(allEvents, string(events))
		}
	}

	fullEvents := strings.Join(allEvents, "")
	if !strings.Contains(fullEvents, `"usage":{"output_tokens":3}`) {
		t.Fatalf("expected message_delta usage output_tokens=3, got: %s", fullEvents)
	}
	if ctx.InputTokens != 7 || ctx.OutputTokens != 3 {
		t.Fatalf("expected context usage input=7 output=3, got input=%d output=%d", ctx.InputTokens, ctx.OutputTokens)
	}
}

func TestOpenAI2StreamToClaudeHandlesInterleavedToolCalls(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"

	chunks := []string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","status":"in_progress"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"path\":\"REA"}`,
		`data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"write_file","status":"in_progress"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"path\":\"OUT"}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"DME.md\"}"}`,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","status":"completed"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"PUT.md\"}"}`,
		`data: {"type":"response.output_item.done","output_index":2,"item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"write_file","status":"completed"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed"}}`,
	}

	var allEvents []string
	for _, chunk := range chunks {
		events, err := OpenAI2StreamToClaude([]byte(chunk), ctx)
		if err != nil {
			t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
		}
		if events != nil {
			allEvents = append(allEvents, string(events))
		}
	}

	fullEvents := strings.Join(allEvents, "")
	if strings.Count(fullEvents, `"type":"tool_use"`) != 2 {
		t.Fatalf("expected 2 tool_use blocks, got %s", fullEvents)
	}
	if !strings.Contains(fullEvents, `"id":"call_1"`) || !strings.Contains(fullEvents, `"name":"read_file"`) {
		t.Fatalf("expected first tool metadata preserved, got %s", fullEvents)
	}
	if !strings.Contains(fullEvents, `"id":"call_2"`) || !strings.Contains(fullEvents, `"name":"write_file"`) {
		t.Fatalf("expected second tool metadata preserved, got %s", fullEvents)
	}
	if !strings.Contains(fullEvents, `"partial_json":"{\"path\":\"REA"`) || !strings.Contains(fullEvents, `"partial_json":"DME.md\"}"`) {
		t.Fatalf("expected first tool deltas preserved, got %s", fullEvents)
	}
	if !strings.Contains(fullEvents, `"partial_json":"{\"path\":\"OUT"`) || !strings.Contains(fullEvents, `"partial_json":"PUT.md\"}"`) {
		t.Fatalf("expected second tool deltas preserved, got %s", fullEvents)
	}
}

func TestOpenAI2StreamToClaudeAssignsSequentialToolBlockIndexes(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"

	chunks := []string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		`data: {"type":"response.output_item.added","output_index":3,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","status":"in_progress"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":3,"delta":"{\"path\":\"README.md\"}"}`,
	}

	var allEvents []string
	for _, chunk := range chunks {
		events, err := OpenAI2StreamToClaude([]byte(chunk), ctx)
		if err != nil {
			t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
		}
		if events != nil {
			allEvents = append(allEvents, string(events))
		}
	}

	fullEvents := strings.Join(allEvents, "")
	if !strings.Contains(fullEvents, `event: content_block_start`) || !strings.Contains(fullEvents, `"index":0`) {
		t.Fatalf("expected first tool_use block to use sequential index 0, got %s", fullEvents)
	}
	if strings.Contains(fullEvents, `"index":3`) {
		t.Fatalf("did not expect Claude content block index to mirror responses output_index, got %s", fullEvents)
	}
}

func TestClaudeStreamToOpenAI2ReturnsErrorAfterPartialOutput(t *testing.T) {
	ctx := transformer.NewStreamContext()

	if _, err := ClaudeStreamToOpenAI2([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":1}}}\n\n"), ctx); err != nil {
		t.Fatalf("ClaudeStreamToOpenAI2 message_start failed: %v", err)
	}
	if _, err := ClaudeStreamToOpenAI2([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"), ctx); err != nil {
		t.Fatalf("ClaudeStreamToOpenAI2 content_block_start failed: %v", err)
	}

	first, err := ClaudeStreamToOpenAI2([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n"), ctx)
	if err != nil {
		t.Fatalf("ClaudeStreamToOpenAI2 first chunk failed: %v", err)
	}
	if first == nil || !strings.Contains(string(first), `"delta":"hello"`) {
		t.Fatalf("expected partial output before error, got %s", string(first))
	}

	second, err := ClaudeStreamToOpenAI2([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"boom\"}}\n\n"), ctx)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected upstream error, got out=%s err=%v", string(second), err)
	}
	if second != nil {
		t.Fatalf("did not expect output chunk on error, got %s", string(second))
	}
}

func TestOpenAI2StreamToClaudePreservesUsageWhenCompletedOmitsOutputTokens(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-sonnet-20240229"
	ctx.InputTokens = 7
	ctx.OutputTokens = 3

	out, err := OpenAI2StreamToClaude([]byte(`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`), ctx)
	if err != nil {
		t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
	}
	if out == nil {
		t.Fatal("expected output on completed event")
	}

	output := string(out)
	if !strings.Contains(output, `"usage":{"output_tokens":3}`) {
		t.Fatalf("expected earlier output_tokens to be preserved, got %s", output)
	}
}

func TestClaudeReqToOpenAI2PreservesToolChain(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"stream": false,
		"messages": [
			{"role":"user","content":"请写文件"},
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Write","input":{"file_path":"/tmp/a.txt","content":"hello"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}
		],
		"tools": [
			{"name":"Write","description":"Write file","input_schema":{"type":"object","properties":{"file_path":{"type":"string"},"content":{"type":"string"}},"required":["file_path","content"]}}
		]
	}`

	reqBytes, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	input, ok := req["input"].([]interface{})
	if !ok {
		t.Fatalf("input should be array, got %T", req["input"])
	}
	if len(input) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(input))
	}

	functionCall, ok := input[1].(map[string]interface{})
	if !ok || functionCall["type"] != "function_call" {
		t.Fatalf("expected input[1] function_call, got %#v", input[1])
	}
	if functionCall["call_id"] != "toolu_1" {
		t.Fatalf("expected call_id toolu_1, got %#v", functionCall["call_id"])
	}
	if _, hasID := functionCall["id"]; hasID {
		t.Fatalf("function_call.id should not be set for upstream compatibility, got %#v", functionCall["id"])
	}

	argsStr, _ := functionCall["arguments"].(string)
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		t.Fatalf("function arguments is not valid json: %v, raw=%s", err, argsStr)
	}
	if args["file_path"] != "/tmp/a.txt" {
		t.Fatalf("unexpected function arguments: %#v", args)
	}

	functionOutput, ok := input[2].(map[string]interface{})
	if !ok || functionOutput["type"] != "function_call_output" {
		t.Fatalf("expected input[2] function_call_output, got %#v", input[2])
	}
	if functionOutput["call_id"] != "toolu_1" {
		t.Fatalf("expected output call_id toolu_1, got %#v", functionOutput["call_id"])
	}
	if functionOutput["output"] != "ok" {
		t.Fatalf("expected output ok, got %#v", functionOutput["output"])
	}

	if strings.Contains(string(reqBytes), "[Tool Call:") || strings.Contains(string(reqBytes), "[Tool Result:") {
		t.Fatalf("found legacy pseudo tool text in transformed request: %s", string(reqBytes))
	}
}

func TestOpenAI2RespToClaudeFallbackToItemID(t *testing.T) {
	openai2Resp := `{
		"id":"resp_1",
		"object":"response",
		"status":"completed",
		"output":[{"type":"function_call","id":"fc_123","name":"Write","arguments":"{\"file_path\":\"/tmp/a.txt\"}"}],
		"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}
	}`

	claudeRespBytes, err := OpenAI2RespToClaude([]byte(openai2Resp))
	if err != nil {
		t.Fatalf("OpenAI2RespToClaude failed: %v", err)
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(claudeRespBytes, &claudeResp); err != nil {
		t.Fatalf("unmarshal claude resp failed: %v", err)
	}

	content, ok := claudeResp["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("unexpected content: %#v", claudeResp["content"])
	}

	toolUse, ok := content[0].(map[string]interface{})
	if !ok {
		t.Fatalf("tool_use item type invalid: %#v", content[0])
	}
	if toolUse["type"] != "tool_use" {
		t.Fatalf("expected tool_use type, got %#v", toolUse["type"])
	}
	if toolUse["id"] != "fc_123" {
		t.Fatalf("expected tool_use id from item.id fallback, got %#v", toolUse["id"])
	}
}

func TestClaudeReqToOpenAI2MapsToolChoiceAnyToRequired(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"stream": true,
		"messages": [{"role":"user","content":"test"}],
		"tools": [{"name":"Write","description":"Write file","input_schema":{"type":"object"}}],
		"tool_choice": {"type":"any"}
	}`

	reqBytes, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	if req["tool_choice"] != "required" {
		t.Fatalf("expected tool_choice=required, got %#v", req["tool_choice"])
	}
	if _, ok := req["store"]; ok {
		t.Fatalf("did not expect store in generic claude->openai2 conversion, got %#v", req["store"])
	}
	if _, ok := req["instructions"]; ok {
		t.Fatalf("did not expect instructions without system prompt, got %#v", req["instructions"])
	}
}

func TestClaudeReqToOpenAI2MapsNamedToolChoice(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"stream": true,
		"messages": [{"role":"user","content":"test"}],
		"tools": [{"name":"Write","description":"Write file","input_schema":{"type":"object"}}],
		"tool_choice": {"type":"tool","name":"Write"}
	}`

	reqBytes, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	toolChoice, ok := req["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected object tool_choice, got %#v", req["tool_choice"])
	}
	if toolChoice["type"] != "function" || toolChoice["name"] != "Write" {
		t.Fatalf("unexpected tool_choice mapping: %#v", toolChoice)
	}
}

func TestClaudeReqToOpenAI2PreservesThinkingAsReasoningItem(t *testing.T) {
	claudeReq := `{
		"model":"claude-sonnet-4-20250514",
		"messages":[
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"think first"},
				{"type":"text","text":"answer"}
			]}
		]
	}`

	reqBytes, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	input := req["input"].([]interface{})
	if len(input) != 2 {
		t.Fatalf("expected reasoning + message items, got %d", len(input))
	}
	if input[0].(map[string]interface{})["type"] != "reasoning" {
		t.Fatalf("expected first input item to be reasoning, got %#v", input[0])
	}
}

func TestClaudeRespToOpenAI2IncludesReasoningOutputItem(t *testing.T) {
	claudeResp := `{
		"id":"msg_1",
		"type":"message",
		"role":"assistant",
		"content":[
			{"type":"thinking","thinking":"think first"},
			{"type":"text","text":"answer"}
		],
		"usage":{"input_tokens":3,"output_tokens":5}
	}`

	respBytes, err := ClaudeRespToOpenAI2([]byte(claudeResp))
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI2 failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal transformed resp failed: %v", err)
	}

	output := resp["output"].([]interface{})
	if len(output) != 2 || output[0].(map[string]interface{})["type"] != "reasoning" {
		t.Fatalf("expected reasoning output item first, got %#v", output)
	}
	if resp["model"] != "" {
		t.Fatalf("expected empty model when upstream omitted it, got %#v", resp["model"])
	}
	if output[0].(map[string]interface{})["id"] == "" {
		t.Fatalf("expected reasoning id, got %#v", output[0])
	}
	if output[1].(map[string]interface{})["status"] != "completed" {
		t.Fatalf("expected completed message item, got %#v", output[1])
	}
}

func TestClaudeRespToOpenAI2MapsMaxTokensToIncomplete(t *testing.T) {
	claudeResp := `{
		"id":"msg_1",
		"type":"message",
		"role":"assistant",
		"model":"claude-sonnet-4",
		"stop_reason":"max_tokens",
		"content":[{"type":"text","text":"answer"}],
		"usage":{"input_tokens":3,"output_tokens":5}
	}`

	respBytes, err := ClaudeRespToOpenAI2([]byte(claudeResp))
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI2 failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal transformed resp failed: %v", err)
	}

	if resp["status"] != "incomplete" {
		t.Fatalf("expected incomplete status, got %#v", resp["status"])
	}
	if resp["model"] != "claude-sonnet-4" {
		t.Fatalf("expected model preserved, got %#v", resp["model"])
	}
}

func TestOpenAI2ReqToClaudeInjectsReasoningThinkingBlock(t *testing.T) {
	openai2Req := `{
		"model":"gpt-4.1",
		"input":[
			{"type":"reasoning","summary":[{"type":"summary_text","text":"think first"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}
		]
	}`

	reqBytes, err := OpenAI2ReqToClaude([]byte(openai2Req), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAI2ReqToClaude failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	messages := req["messages"].([]interface{})
	content := messages[0].(map[string]interface{})["content"].([]interface{})
	first := content[0].(map[string]interface{})
	if first["type"] != "thinking" || first["thinking"] != "think first" {
		t.Fatalf("expected leading thinking block, got %#v", first)
	}
}

func TestOpenAI2RespToClaudeRestoresReasoningItemAsThinking(t *testing.T) {
	openai2Resp := `{
		"id":"resp_1",
		"object":"response",
		"status":"completed",
		"output":[
			{"type":"reasoning","summary":[{"type":"summary_text","text":"think first"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}
		],
		"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}
	}`

	respBytes, err := OpenAI2RespToClaude([]byte(openai2Resp))
	if err != nil {
		t.Fatalf("OpenAI2RespToClaude failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal transformed resp failed: %v", err)
	}

	content := resp["content"].([]interface{})
	first := content[0].(map[string]interface{})
	if first["type"] != "thinking" || first["thinking"] != "think first" {
		t.Fatalf("expected reasoning restored as thinking block, got %#v", first)
	}
}

func TestClaudeStreamToOpenAI2EmitsReasoningEvents(t *testing.T) {
	ctx := transformer.NewStreamContext()

	events := []string{
		`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":3}}}
`,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}
`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"think first"}}
`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}
`,
	}

	var all string
	for _, event := range events {
		out, err := ClaudeStreamToOpenAI2([]byte(event), ctx)
		if err != nil {
			t.Fatalf("ClaudeStreamToOpenAI2 failed: %v", err)
		}
		all += string(out)
	}

	if !strings.Contains(all, `"type":"response.reasoning_summary_text.delta"`) {
		t.Fatalf("expected reasoning_summary_text delta event, got %s", all)
	}
	if !strings.Contains(all, `"type":"response.output_item.done"`) {
		t.Fatalf("expected reasoning output item done event, got %s", all)
	}
}

func TestOpenAI2ReqToClaudeSupportsEasyInputMessage(t *testing.T) {
	openai2Req := `{
		"model":"gpt-4.1",
		"input":[
			{"role":"assistant","content":"answer"}
		]
	}`

	reqBytes, err := OpenAI2ReqToClaude([]byte(openai2Req), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAI2ReqToClaude failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	messages := req["messages"].([]interface{})
	content := messages[0].(map[string]interface{})["content"]
	if content != "answer" {
		t.Fatalf("expected easy input message preserved as Claude content, got %#v", content)
	}
}

func TestOpenAI2StreamToClaudeEmitsThinkingFromReasoningSummary(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "resp_1"
	ctx.ModelName = "claude-sonnet-4-20250514"

	events := []string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		`data: {"type":"response.reasoning_summary_text.delta","delta":"think first"}`,
	}

	var all string
	for _, event := range events {
		out, err := OpenAI2StreamToClaude([]byte(event), ctx)
		if err != nil {
			t.Fatalf("OpenAI2StreamToClaude failed: %v", err)
		}
		all += string(out)
	}

	if !strings.Contains(all, `"type":"thinking_delta"`) || !strings.Contains(all, `"thinking":"think first"`) {
		t.Fatalf("expected reasoning summary to become Claude thinking delta, got %s", all)
	}
}

func TestClaudeReqToOpenAI2DefaultsToolChoiceRequiredWhenToolsPresent(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"stream": true,
		"messages": [{"role":"user","content":"test"}],
		"tools": [{"name":"Write","description":"Write file","input_schema":{"type":"object"}}]
	}`

	reqBytes, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	if req["tool_choice"] != "required" {
		t.Fatalf("expected tool_choice=required, got %#v", req["tool_choice"])
	}
}

func TestClaudeReqToOpenAI2DefaultsToolChoiceAutoAfterToolResult(t *testing.T) {
	claudeReq := `{
		"model": "claude-sonnet-4-20250514",
		"stream": true,
		"messages": [
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/tmp/a"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}
		],
		"tools": [{"name":"Read","description":"Read file","input_schema":{"type":"object"}}]
	}`

	reqBytes, err := ClaudeReqToOpenAI2([]byte(claudeReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	if req["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice=auto after tool_result, got %#v", req["tool_choice"])
	}
}
