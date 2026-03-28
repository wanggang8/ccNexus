package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

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

func TestOpenAIStreamToClaudeUsageHasDelta(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.ModelName = "claude-3-5-sonnet-20241022"

	chunk := `data: {"id":"usage-1","object":"chat.completion.chunk","created":123,"model":"gpt-4","choices":[{"index":0,"delta":{}}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`
	result, err := OpenAIStreamToClaude([]byte(chunk), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToClaude failed: %v", err)
	}

	parts := strings.Split(string(result), "\n\n")
	found := false
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		eventType, jsonData := parseSSE([]byte(part + "\n"))
		if eventType != "message_delta" {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(jsonData), &payload); err != nil {
			t.Fatalf("Failed to unmarshal message_delta: %v", err)
		}
		if _, ok := payload["delta"].(map[string]interface{}); !ok {
			t.Fatalf("Expected delta object in message_delta, got %T", payload["delta"])
		}
		usage, ok := payload["usage"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected usage object in message_delta, got %T", payload["usage"])
		}
		if usage["input_tokens"] != float64(5) || usage["output_tokens"] != float64(7) {
			t.Fatalf("Unexpected usage: %#v", usage)
		}
		found = true
	}

	if !found {
		t.Fatal("message_delta event not found in result")
	}
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

func TestClaudeRespToOpenAIPreservesThinkingAsReasoningContent(t *testing.T) {
	claudeResp := `{
		"id":"msg_1",
		"type":"message",
		"role":"assistant",
		"content":[
			{"type":"thinking","thinking":"think first"},
			{"type":"text","text":"answer"}
		],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":3,"output_tokens":5}
	}`

	openaiRespBytes, err := ClaudeRespToOpenAI([]byte(claudeResp), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(openaiRespBytes, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal OpenAI response: %v", err)
	}

	message := openaiResp["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	if message["reasoning_content"] != "think first" {
		t.Fatalf("expected reasoning_content preserved, got %#v", message["reasoning_content"])
	}
}

func TestClaudeRespToOpenAIFixesMissingToolUseIDAndFinishReason(t *testing.T) {
	claudeResp := `{
		"id":"msg_2",
		"type":"message",
		"role":"assistant",
		"content":[
			{"type":"tool_use","name":"read_file","input":{"path":"README.md"}}
		],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":1,"output_tokens":2}
	}`

	openaiRespBytes, err := ClaudeRespToOpenAI([]byte(claudeResp), "gpt-4.1")
	if err != nil {
		t.Fatalf("ClaudeRespToOpenAI failed: %v", err)
	}

	var openaiResp map[string]interface{}
	if err := json.Unmarshal(openaiRespBytes, &openaiResp); err != nil {
		t.Fatalf("Failed to unmarshal OpenAI response: %v", err)
	}

	choice := openaiResp["choices"].([]interface{})[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})
	toolCalls := message["tool_calls"].([]interface{})
	toolCall := toolCalls[0].(map[string]interface{})
	if !strings.HasPrefix(toolCall["id"].(string), "toolu_") {
		t.Fatalf("expected synthesized tool_use id, got %#v", toolCall["id"])
	}
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("expected finish_reason=tool_calls, got %#v", choice["finish_reason"])
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
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
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
		if openaiReq.Messages[1].ReasoningContent != "I should say hello back" {
			t.Errorf("Expected reasoning_content preserved, got %s", openaiReq.Messages[1].ReasoningContent)
		}
	}
}

func TestClaudeReqToOpenAIPreservesThinkingBlocksAsReasoningContent(t *testing.T) {
	claudeReq := `{
		"model": "claude-3-opus-20240229",
		"messages": [
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"think first"},
				{"type":"text","text":"answer"}
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
	if openaiReq.Messages[0].ReasoningContent != "think first" {
		t.Fatalf("expected reasoning_content preserved, got %#v", openaiReq.Messages[0].ReasoningContent)
	}
}

func TestClaudeReqToOpenAISynthesizesMissingToolUseID(t *testing.T) {
	claudeReq := `{
		"model": "claude-3-opus-20240229",
		"messages": [
			{"role":"assistant","content":[
				{"type":"tool_use","name":"read_file","input":{"path":"README.md"}}
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

	if len(openaiReq.Messages) != 1 || len(openaiReq.Messages[0].ToolCalls) != 1 {
		t.Fatalf("expected synthesized tool call, got %#v", openaiReq.Messages)
	}
	if !strings.HasPrefix(openaiReq.Messages[0].ToolCalls[0].ID, "toolu_") {
		t.Fatalf("expected synthesized toolu_ id, got %#v", openaiReq.Messages[0].ToolCalls[0].ID)
	}
}

func TestOpenAIReqToClaudeInjectsReasoningContentAsThinkingBlock(t *testing.T) {
	openaiReq := `{
		"model":"gpt-4.1",
		"messages":[
			{"role":"assistant","content":"answer","reasoning_content":"think first"}
		]
	}`

	claudeReqBytes, err := OpenAIReqToClaude([]byte(openaiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAIReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(claudeReqBytes, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal Claude request: %v", err)
	}

	messages := claudeReq["messages"].([]interface{})
	content := messages[0].(map[string]interface{})["content"].([]interface{})
	first := content[0].(map[string]interface{})
	if first["type"] != "thinking" || first["thinking"] != "think first" {
		t.Fatalf("expected leading thinking block, got %#v", first)
	}
}

func TestOpenAIReqToClaudePreservesRemoteImageURL(t *testing.T) {
	openaiReq := `{
		"model":"gpt-4.1",
		"messages":[
			{"role":"user","content":[
				{"type":"image_url","image_url":{"url":"https://example.com/cat.png"}}
			]}
		]
	}`

	claudeReqBytes, err := OpenAIReqToClaude([]byte(openaiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAIReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(claudeReqBytes, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal Claude request: %v", err)
	}

	messages := claudeReq["messages"].([]interface{})
	content := messages[0].(map[string]interface{})["content"].([]interface{})
	first := content[0].(map[string]interface{})
	source := first["source"].(map[string]interface{})
	if first["type"] != "image" || source["type"] != "url" || source["url"] != "https://example.com/cat.png" {
		t.Fatalf("expected remote image url preserved, got %#v", first)
	}
}

func TestOpenAIReqToClaudePreservesStructuredSystemAndAssistantBlocksWithToolCalls(t *testing.T) {
	openaiReq := `{
		"model":"gpt-4.1",
		"messages":[
			{"role":"system","content":[{"type":"text","text":"follow the repo rules"}]},
			{"role":"assistant","content":[
				{"type":"text","text":"I found this file"},
				{"type":"image_url","image_url":{"url":"https://example.com/diagram.png"}}
			],"tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"Read","arguments":"{\"path\":\"README.md\"}"}}
			]}
		]
	}`

	claudeReqBytes, err := OpenAIReqToClaude([]byte(openaiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAIReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(claudeReqBytes, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal Claude request: %v", err)
	}

	if claudeReq["system"] != "follow the repo rules" {
		t.Fatalf("expected structured system text preserved, got %#v", claudeReq["system"])
	}

	messages := claudeReq["messages"].([]interface{})
	content := messages[0].(map[string]interface{})["content"].([]interface{})
	if len(content) != 3 {
		t.Fatalf("expected text + image + tool_use blocks, got %#v", content)
	}

	first := content[0].(map[string]interface{})
	if first["type"] != "text" || first["text"] != "I found this file" {
		t.Fatalf("expected assistant text block preserved, got %#v", first)
	}

	second := content[1].(map[string]interface{})
	source := second["source"].(map[string]interface{})
	if second["type"] != "image" || source["type"] != "url" || source["url"] != "https://example.com/diagram.png" {
		t.Fatalf("expected assistant image block preserved, got %#v", second)
	}

	third := content[2].(map[string]interface{})
	if third["type"] != "tool_use" || third["id"] != "call_1" || third["name"] != "Read" {
		t.Fatalf("expected tool_use appended after existing content, got %#v", third)
	}
}

func TestOpenAIReqToClaudeMergesToolResultWithFollowingUserMessageAndPreservesTokens(t *testing.T) {
	openaiReq := `{
		"model":"gpt-4.1",
		"max_tokens":128,
		"top_p":0.95,
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"Read","arguments":"{\"path\":\"README.md\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"ok"},
			{"role":"user","content":"continue"}
		]
	}`

	claudeReqBytes, err := OpenAIReqToClaude([]byte(openaiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAIReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(claudeReqBytes, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal Claude request: %v", err)
	}

	if claudeReq["max_tokens"] != float64(128) {
		t.Fatalf("expected max_tokens=128, got %#v", claudeReq["max_tokens"])
	}
	if claudeReq["top_p"] != 0.95 {
		t.Fatalf("expected top_p preserved, got %#v", claudeReq["top_p"])
	}

	messages := claudeReq["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("expected merged assistant/user messages, got %#v", messages)
	}

	userMessage := messages[1].(map[string]interface{})
	if userMessage["role"] != "user" {
		t.Fatalf("expected merged user message, got %#v", userMessage["role"])
	}
	content := userMessage["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("expected tool_result + text in merged user message, got %#v", content)
	}
	if content[0].(map[string]interface{})["type"] != "tool_result" || content[0].(map[string]interface{})["content"] != "ok" {
		t.Fatalf("expected tool_result preserved first, got %#v", content[0])
	}
	if content[1].(map[string]interface{})["type"] != "text" || content[1].(map[string]interface{})["text"] != "continue" {
		t.Fatalf("expected following user text merged, got %#v", content[1])
	}
}

func TestOpenAIReqToClaudeStringifiesStructuredToolMessageContent(t *testing.T) {
	openaiReq := `{
		"model":"gpt-4.1",
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"Read","arguments":"{\"path\":\"README.md\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":{"ok":true,"count":2}}
		]
	}`

	claudeReqBytes, err := OpenAIReqToClaude([]byte(openaiReq), "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("OpenAIReqToClaude failed: %v", err)
	}

	var claudeReq map[string]interface{}
	if err := json.Unmarshal(claudeReqBytes, &claudeReq); err != nil {
		t.Fatalf("Failed to unmarshal Claude request: %v", err)
	}

	messages := claudeReq["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("expected assistant and tool_result messages, got %#v", messages)
	}

	userMessage := messages[1].(map[string]interface{})
	content := userMessage["content"].([]interface{})
	toolResult := content[0].(map[string]interface{})
	raw, ok := toolResult["content"].(string)
	if !ok {
		t.Fatalf("expected stringified tool_result content, got %#v", toolResult["content"])
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("expected JSON string content, got %q (%v)", raw, err)
	}
	if decoded["ok"] != true || decoded["count"] != float64(2) {
		t.Fatalf("expected structured tool content preserved, got %#v", decoded)
	}
}
