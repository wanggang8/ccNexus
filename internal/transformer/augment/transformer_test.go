package augment

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- Helper ---

func boolPtr(b bool) *bool { return &b }

// --- TransformRequest tests (Augment → Claude) ---

func TestToClaudeRequest_SimpleMessage(t *testing.T) {
	tr, err := New("claude", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	input := AugmentRequest{
		Model:   "claude-3-5-sonnet-20241022",
		Message: "Hello, world!",
		Stream:  boolPtr(false),
	}
	body, _ := json.Marshal(input)

	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	if req["model"] != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected model claude-3-5-sonnet-20241022, got %v", req["model"])
	}
	msgs := req["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]interface{})
	if msg["role"] != "user" {
		t.Errorf("expected role user, got %v", msg["role"])
	}
	if msg["content"] != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got %v", msg["content"])
	}
}

func TestToClaudeRequest_DefaultModelApplied(t *testing.T) {
	tr, _ := New("claude", "")
	input := AugmentRequest{Message: "hi"}
	body, _ := json.Marshal(input)
	out, _ := tr.TransformRequest(body)

	var req map[string]interface{}
	json.Unmarshal(out, &req)
	if req["model"] == "" || req["model"] == nil {
		t.Error("expected default model to be set")
	}
}

func TestToClaudeRequest_ModelOverride(t *testing.T) {
	tr, _ := New("claude", "claude-opus-4-20250514")
	input := AugmentRequest{Model: "claude-3-5-sonnet-20241022", Message: "hi"}
	body, _ := json.Marshal(input)
	out, _ := tr.TransformRequest(body)

	var req map[string]interface{}
	json.Unmarshal(out, &req)
	if req["model"] != "claude-opus-4-20250514" {
		t.Errorf("expected model override claude-opus-4-20250514, got %v", req["model"])
	}
}

func TestToClaudeRequest_WithTools(t *testing.T) {
	tr, _ := New("claude", "")
	input := AugmentRequest{
		Message: "use a tool",
		ToolDefinitions: []ToolDefinition{
			{
				Name:        "read_file",
				Description: "Reads a file",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(input)
	out, _ := tr.TransformRequest(body)

	var req map[string]interface{}
	json.Unmarshal(out, &req)

	tools, ok := req["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %v", req["tools"])
	}
	tool := tools[0].(map[string]interface{})
	if tool["name"] != "read_file" {
		t.Errorf("expected tool name read_file, got %v", tool["name"])
	}
}

func TestToClaudeRequest_EnableThinkingFlag(t *testing.T) {
	tr, _ := New("claude", "")
	input := AugmentRequest{
		Message:        "think about this",
		EnableThinking: true,
		MaxTokens:      2000,
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	thinking, ok := req["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected thinking config, got %v", req["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("expected thinking type enabled, got %v", thinking["type"])
	}
	if thinking["budget_tokens"] != float64(1999) {
		t.Fatalf("expected budget_tokens 1999, got %v", thinking["budget_tokens"])
	}
}

func TestToClaudeRequest_ThinkingConfigPassThrough(t *testing.T) {
	tr, _ := New("claude", "")
	input := AugmentRequest{
		Message: "think about this",
		Thinking: map[string]interface{}{
			"type": "adaptive",
		},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	thinking, ok := req["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected thinking config, got %v", req["thinking"])
	}
	if thinking["type"] != "adaptive" {
		t.Fatalf("expected thinking type adaptive, got %v", thinking["type"])
	}
	if _, ok := thinking["budget_tokens"]; ok {
		t.Fatalf("adaptive thinking should not force budget_tokens, got %v", thinking["budget_tokens"])
	}
}

func TestToClaudeRequest_ToolResult(t *testing.T) {
	tr, _ := New("claude", "")
	input := AugmentRequest{
		Nodes: []Node{
			{
				Type: 1,
				ToolResultNode: &ToolResultNode{
					ToolUseID: "tool_123",
					Content:   "file content here",
				},
			},
		},
	}
	body, _ := json.Marshal(input)
	out, _ := tr.TransformRequest(body)

	var req map[string]interface{}
	json.Unmarshal(out, &req)

	msgs := req["messages"].([]interface{})
	// tool_result should produce a separate user message
	found := false
	for _, m := range msgs {
		msg := m.(map[string]interface{})
		if msg["role"] == "user" {
			if content, ok := msg["content"].([]interface{}); ok {
				for _, c := range content {
					block := c.(map[string]interface{})
					if block["type"] == "tool_result" {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Error("expected tool_result message block")
	}
}

func TestToClaudeRequest_ChatHistory(t *testing.T) {
	tr, _ := New("claude", "")
	input := AugmentRequest{
		Message: "what did i say before?",
		ChatHistory: []ChatHistoryEntry{
			{
				RequestMessage: "hello",
				ResponseText:   "hi there",
			},
		},
	}
	body, _ := json.Marshal(input)
	out, _ := tr.TransformRequest(body)

	var req map[string]interface{}
	json.Unmarshal(out, &req)

	msgs := req["messages"].([]interface{})
	// history: user "hello" + assistant "hi there" + current user
	if len(msgs) < 3 {
		t.Errorf("expected at least 3 messages (history + current), got %d", len(msgs))
	}
}

func TestToClaudeRequest_ChatHistoryResponseNodeOrder(t *testing.T) {
	tr, _ := New("claude", "")
	input := AugmentRequest{
		ChatHistory: []ChatHistoryEntry{
			{
				ResponseNodes: []Node{
					{Type: 8, Thinking: &ThinkingNode{Summary: "first thought", Signature: "sig-1"}},
					{Type: 0, TextNode: &TextNode{Content: "final answer"}},
					{Type: 5, ToolUse: &ToolUseNode{ToolName: "search", ToolUseID: "call_1", InputJSON: "{}"}},
				},
			},
		},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	msgs := req["messages"].([]interface{})
	if len(msgs) != 3 {
		t.Fatalf("expected dummy-user + assistant + repaired tool_result message, got %d", len(msgs))
	}
	// First message is dummy user (ensureClaudeFirstMessageIsUser)
	dummy := msgs[0].(map[string]interface{})
	if dummy["role"] != "user" {
		t.Fatalf("expected dummy first message role user, got %v", dummy["role"])
	}
	msg := msgs[1].(map[string]interface{})
	if msg["role"] != "assistant" {
		t.Fatalf("expected second message role assistant, got %v", msg["role"])
	}
	content := msg["content"].([]interface{})
	if len(content) != 3 {
		t.Fatalf("expected 3 content blocks, got %d", len(content))
	}

	if content[0].(map[string]interface{})["type"] != "thinking" {
		t.Fatalf("expected first block to be thinking, got %v", content[0])
	}
	if content[1].(map[string]interface{})["type"] != "text" {
		t.Fatalf("expected second block to be text, got %v", content[1])
	}
	if content[2].(map[string]interface{})["type"] != "tool_use" {
		t.Fatalf("expected third block to be tool_use, got %v", content[2])
	}
	repaired := msgs[2].(map[string]interface{})
	if repaired["role"] != "user" {
		t.Fatalf("expected repaired tool_result message role user, got %v", repaired["role"])
	}
	repairedBlocks := repaired["content"].([]interface{})
	if len(repairedBlocks) != 1 {
		t.Fatalf("expected one repaired tool_result block, got %d", len(repairedBlocks))
	}
	if repairedBlocks[0].(map[string]interface{})["type"] != "tool_result" {
		t.Fatalf("expected repaired block type tool_result, got %v", repairedBlocks[0])
	}
}

func TestToOpenAIRequest_IncludesSpecialRequestNodePrompts(t *testing.T) {
	tr, _ := New("openai", "")
	input := AugmentRequest{
		Message:          "继续处理",
		PersonaType:      3,
		ByokSystemPrompt: "extra system",
		Nodes: []Node{
			{Type: 3, ImageIDNode: &ImageIDNode{ImageID: "img_1", Format: 4}},
			{Type: 5, EditEventsNode: &EditEventsNode{Source: "editor", EditEvents: []FileEditEvent{{Path: "a.go", Edits: []TextEditDiff{{AfterLineStart: 12, BeforeLineStart: 10, BeforeText: "old", AfterText: "new"}}}}}},
			{Type: 6, CheckpointRef: &CheckpointRefNode{RequestID: "req_1", FromTimestamp: 1, ToTimestamp: 2, Source: "history"}},
			{Type: 7, Personality: &ChangePersonalityNode{PersonalityType: 2, CustomInstructions: "think broad"}},
			{Type: 8, FileNode: &FileNode{FileData: "SGVsbG8=", Format: "text/plain"}},
			{Type: 9, FileIDNode: &FileIDNode{FileID: "file_1", FileName: "demo.txt"}},
		},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	msgs := req["messages"].([]interface{})
	system := msgs[0].(map[string]interface{})["content"].(string)
	if !strings.Contains(system, "Persona: REVIEWER") || !strings.Contains(system, "extra system") {
		t.Fatalf("expected persona/byok system prompt in system message, got %q", system)
	}
	user := msgs[1].(map[string]interface{})["content"].(string)
	for _, needle := range []string{"[IMAGE_ID]", "[EDIT_EVENTS]", "[CHECKPOINT_REF]", "[CHANGE_PERSONALITY]", "[FILE]", "[FILE_ID]"} {
		if !strings.Contains(user, needle) {
			t.Fatalf("expected %s in user message, got %q", needle, user)
		}
	}
	for _, needle := range []string{"source=editor", "request_id=req_1", "custom_instructions=think broad", "demo.txt", "Hello"} {
		if !strings.Contains(user, needle) {
			t.Fatalf("expected rich node detail %q in user message, got %q", needle, user)
		}
	}
}

func TestToOpenAIRequest_SpecialRequestNodesRichDetails(t *testing.T) {
	tr, _ := New("openai", "")
	input := AugmentRequest{
		Message: "继续处理",
		Nodes: []Node{
			{Type: 4, IdeStateNode: &IdeStateNode{WorkspaceFolders: []WorkspaceFolder{{FolderRoot: "/repo", RepositoryRoot: "/repo"}}, CurrentTerminal: &TerminalState{TerminalID: 3, CurrentWorkingDirectory: "/repo/sub"}}},
			{Type: 5, EditEventsNode: &EditEventsNode{Source: "editor", EditEvents: []FileEditEvent{{Path: "a.go", AfterBlobName: "blob_after", Edits: []TextEditDiff{{AfterLineStart: 12, BeforeLineStart: 10, BeforeText: "old", AfterText: "new"}}}}}},
			{Type: 6, CheckpointRef: &CheckpointRefNode{RequestID: "req_1", FromTimestamp: 1, ToTimestamp: 2, Source: "history"}},
			{Type: 7, Personality: &ChangePersonalityNode{PersonalityType: 2, CustomInstructions: "think broad"}},
			{Type: 8, FileNode: &FileNode{FileData: "SGVsbG8=", Format: "text/plain"}},
			{Type: 9, FileIDNode: &FileIDNode{FileID: "file_1", FileName: "demo.txt"}},
		},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	msgs := req["messages"].([]interface{})
	user := msgs[0].(map[string]interface{})["content"].(string)
	for _, needle := range []string{"[IDE_STATE]", "workspace_folders:", "current_terminal", "[EDIT_EVENTS]", "blob_after", "[CHECKPOINT_REF]", "request_id=req_1", "[CHANGE_PERSONALITY]", "custom_instructions=think broad", "[FILE]", "Hello", "[FILE_ID]", "demo.txt"} {
		if !strings.Contains(user, needle) {
			t.Fatalf("expected rich special node detail %q in user message, got %q", needle, user)
		}
	}
}

func TestToOpenAIRequest_ToolChoiceAutoWhenToolsPresent(t *testing.T) {
	tr, _ := New("openai", "")
	input := AugmentRequest{
		Model: "gpt-4.1",
		ToolDefinitions: []ToolDefinition{{
			Name:        "search",
			Description: "search docs",
			Parameters:  map[string]interface{}{"type": "object"},
		}},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if req["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice auto, got %#v", req["tool_choice"])
	}
}

func TestToClaudeRequest_ToolChoiceAutoWhenToolsPresent(t *testing.T) {
	tr, _ := New("claude", "")
	input := AugmentRequest{
		Model: "claude-sonnet-4-20250514",
		ToolDefinitions: []ToolDefinition{{
			Name:        "search",
			Description: "search docs",
			Parameters:  map[string]interface{}{"type": "object"},
		}},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	toolChoice, ok := req["tool_choice"].(map[string]interface{})
	if !ok || toolChoice["type"] != "auto" {
		t.Fatalf("expected anthropic tool_choice auto, got %#v", req["tool_choice"])
	}
}

func TestToOpenAIRequest_PromptSourceSkipsSelectedCodeContext(t *testing.T) {
	tr, _ := New("openai", "")
	input := map[string]interface{}{
		"model":         "gpt-4.1",
		"prompt":        "直接回答这个 prompt",
		"selected_code": "should not be appended",
		"diff":          "should also not be appended",
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	msgs := req["messages"].([]interface{})
	user := msgs[0].(map[string]interface{})["content"].(string)
	if strings.Contains(user, "[selected_code]") || strings.Contains(user, "[diff]") {
		t.Fatalf("prompt source should not append extra code context, got %q", user)
	}
}

func TestToOpenAIRequest_ResponseMainTextFinishedAndToolUseStart(t *testing.T) {
	tr, _ := New("openai", "")
	input := AugmentRequest{
		ChatHistory: []ChatHistoryEntry{
			{
				ResponseNodes: []Node{
					{Type: 0, TextNode: &TextNode{Content: "partial"}},
					{Type: 2, TextNode: &TextNode{Content: "final"}},
					{Type: 7, ToolUse: &ToolUseNode{ToolName: "search", ToolUseID: "call_1", InputJSON: "{}"}},
				},
			},
		},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	msgs := req["messages"].([]interface{})
	if len(msgs) != 2 {
		t.Fatalf("expected assistant message plus repaired tool result, got %d", len(msgs))
	}
	assistant := msgs[0].(map[string]interface{})
	if assistant["content"] != "final" {
		t.Fatalf("expected main_text_finished content to win, got %#v", assistant["content"])
	}
	toolCalls := assistant["tool_calls"].([]interface{})
	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call from tool_use_start fallback, got %#v", toolCalls)
	}
}

func TestAssistantResponseTextPrefersFallbackNodes(t *testing.T) {
	text := assistantResponseText("", []Node{{Type: 2, TextNode: &TextNode{Text: "done"}}})
	if text != "done" {
		t.Fatalf("expected assistantResponseText to use node text fallback, got %q", text)
	}
}

func TestToOpenAIRequest_HistorySummaryPreprocessesEarlierHistory(t *testing.T) {
	tr, _ := New("openai", "")
	input := AugmentRequest{
		ChatHistory: []ChatHistoryEntry{
			{
				RequestMessage: "very old request",
				ResponseText:   "very old response",
			},
			{
				RequestNodes: []Node{
					{Type: 10, HistorySummary: &HistorySummaryNode{
						MessageTemplate: "Summary block\n{summary}\n{end_part_full}",
						SummaryText:     "compressed summary",
						HistoryEnd: []map[string]interface{}{
							{
								"request_message": "tail request",
								"response_nodes": []map[string]interface{}{
									{
										"type": 5,
										"tool_use": map[string]interface{}{
											"tool_use_id": "call_tail_1",
											"tool_name":   "read_file",
											"input_json":  "{\"path\":\"README.md\"}",
										},
									},
									{
										"type": 2,
										"text_node": map[string]interface{}{
											"text": "tail response",
										},
									},
								},
							},
						},
					}},
				},
			},
		},
		Message: "current turn",
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	msgs := req["messages"].([]interface{})
	if len(msgs) != 2 {
		t.Fatalf("expected summary user message plus current user message, got %d", len(msgs))
	}
	summary := msgs[0].(map[string]interface{})["content"].(string)
	if strings.Contains(summary, "very old request") || strings.Contains(summary, "very old response") {
		t.Fatalf("expected old history to be dropped, got %q", summary)
	}
	if !strings.Contains(summary, "Summary block") || !strings.Contains(summary, "tail request") {
		t.Fatalf("expected rendered summary content, got %q", summary)
	}
	if !strings.Contains(summary, "tool_use_id=\"call_tail_1\"") || !strings.Contains(summary, "tail response") {
		t.Fatalf("expected summary to preserve tail tool_use_start/text nodes, got %q", summary)
	}
	current := msgs[1].(map[string]interface{})["content"].(string)
	if current != "current turn" {
		t.Fatalf("expected current turn message preserved, got %q", current)
	}
}

func TestToClaudeRequest_ImageOnlyDataURL(t *testing.T) {
	tr, _ := New("claude", "")
	input := AugmentRequest{
		Images: []string{"data:image/jpeg;base64,Zm9vYmFy"},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	msgs := req["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]interface{})
	content := msg["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}
	img := content[0].(map[string]interface{})
	if img["type"] != "image" {
		t.Fatalf("expected image block, got %v", img["type"])
	}
	source := img["source"].(map[string]interface{})
	if source["media_type"] != "image/jpeg" {
		t.Fatalf("expected media_type image/jpeg, got %v", source["media_type"])
	}
	if source["data"] != "Zm9vYmFy" {
		t.Fatalf("expected stripped base64 payload, got %v", source["data"])
	}
}

func TestToClaudeRequest_ImageNodeDataURL(t *testing.T) {
	tr, _ := New("claude", "")
	input := AugmentRequest{
		Nodes: []Node{
			{
				Type: 2,
				ImageNode: &ImageNode{
					ImageData: "data:image/webp;base64,Zm9vYmFy",
					Format:    1,
				},
			},
		},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	msgs := req["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]interface{})
	content := msg["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}
	img := content[0].(map[string]interface{})
	source := img["source"].(map[string]interface{})
	if source["media_type"] != "image/webp" {
		t.Fatalf("expected media_type image/webp, got %v", source["media_type"])
	}
	if source["data"] != "Zm9vYmFy" {
		t.Fatalf("expected stripped base64 payload, got %v", source["data"])
	}
}

func TestToClaudeRequest_IdeStateDedup(t *testing.T) {
	tr, _ := New("claude", "")
	ideState := &IdeStateNode{
		WorkspaceFolders: []WorkspaceFolder{{FolderRoot: "/project"}},
	}
	input := AugmentRequest{
		Message: "test",
		ChatHistory: []ChatHistoryEntry{
			{
				RequestNodes:   []Node{{Type: 4, IdeStateNode: ideState}},
				RequestMessage: "prev",
				ResponseText:   "response",
			},
		},
		Nodes: []Node{{Type: 4, IdeStateNode: ideState}},
	}
	body, _ := json.Marshal(input)
	out, _ := tr.TransformRequest(body)

	var req map[string]interface{}
	json.Unmarshal(out, &req)

	msgs := req["messages"].([]interface{})
	ideCount := 0
	for _, m := range msgs {
		msg := m.(map[string]interface{})
		switch c := msg["content"].(type) {
		case string:
			if contains(c, "[ide_state]") {
				ideCount++
			}
		}
	}
	// ide_state in history user message, should not be duplicated in current turn
	if ideCount > 1 {
		t.Errorf("ide_state appeared %d times, expected deduplication", ideCount)
	}
}

// --- TransformRequest tests (Augment → OpenAI) ---

func TestToOpenAIRequest_SimpleMessage(t *testing.T) {
	tr, _ := New("openai", "")
	input := AugmentRequest{
		Model:   "gpt-4o",
		Message: "Hello!",
	}
	body, _ := json.Marshal(input)
	out, _ := tr.TransformRequest(body)

	var req map[string]interface{}
	json.Unmarshal(out, &req)

	if req["model"] != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %v", req["model"])
	}
	msgs := req["messages"].([]interface{})
	found := false
	for _, m := range msgs {
		msg := m.(map[string]interface{})
		if msg["role"] == "user" && msg["content"] == "Hello!" {
			found = true
		}
	}
	if !found {
		t.Error("expected user message with content 'Hello!'")
	}
}

func TestToOpenAIRequest_WithUserGuidelines(t *testing.T) {
	tr, _ := New("openai", "")
	input := AugmentRequest{
		Message:        "hello",
		UserGuidelines: "You are a helpful assistant",
	}
	body, _ := json.Marshal(input)
	out, _ := tr.TransformRequest(body)

	var req map[string]interface{}
	json.Unmarshal(out, &req)

	msgs := req["messages"].([]interface{})
	if len(msgs) < 1 {
		t.Fatal("expected messages")
	}
	first := msgs[0].(map[string]interface{})
	if first["role"] != "system" {
		t.Errorf("expected first message to be system, got %v", first["role"])
	}
}

func TestToOpenAIRequest_ImageOnlyDataURL(t *testing.T) {
	tr, _ := New("openai", "")
	input := AugmentRequest{
		Images: []string{"data:image/jpeg;base64,Zm9vYmFy"},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	msgs := req["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]interface{})
	content := msg["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}
	img := content[0].(map[string]interface{})
	if img["type"] != "image_url" {
		t.Fatalf("expected image_url block, got %v", img["type"])
	}
	imageURL := img["image_url"].(map[string]interface{})
	if imageURL["url"] != "data:image/jpeg;base64,Zm9vYmFy" {
		t.Fatalf("expected preserved data URL, got %v", imageURL["url"])
	}
}

func TestToOpenAIRequest_ToolsConverted(t *testing.T) {
	tr, _ := New("openai", "")
	input := AugmentRequest{
		Message: "use tool",
		ToolDefinitions: []ToolDefinition{
			{Name: "search", Description: "Search the web"},
		},
	}
	body, _ := json.Marshal(input)
	out, _ := tr.TransformRequest(body)

	var req map[string]interface{}
	json.Unmarshal(out, &req)

	tools, ok := req["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 OpenAI tool, got %v", req["tools"])
	}
	tool := tools[0].(map[string]interface{})
	if tool["type"] != "function" {
		t.Errorf("expected tool type function, got %v", tool["type"])
	}
	fn := tool["function"].(map[string]interface{})
	if fn["name"] != "search" {
		t.Errorf("expected tool name search, got %v", fn["name"])
	}
}

func TestToOpenAIRequest_ToolResultsAsToolRole(t *testing.T) {
	tr, _ := New("openai", "")
	input := AugmentRequest{
		Nodes: []Node{
			{Type: 1, ToolResultNode: &ToolResultNode{ToolUseID: "call_abc", Content: "result data"}},
		},
	}
	body, _ := json.Marshal(input)
	out, _ := tr.TransformRequest(body)

	var req map[string]interface{}
	json.Unmarshal(out, &req)

	msgs := req["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Fatalf("expected repaired orphan tool_result user message, got %d messages", len(msgs))
	}
	msg := msgs[0].(map[string]interface{})
	if msg["role"] != "user" {
		t.Fatalf("expected user role for orphan tool_result, got %v", msg["role"])
	}
	content, _ := msg["content"].(string)
	if !strings.Contains(content, "[orphan_tool_result id=call_abc]") {
		t.Fatalf("expected orphan tool_result marker, got %q", content)
	}
	if !strings.Contains(content, "result data") {
		t.Fatalf("expected tool result payload in repaired user content, got %q", content)
	}
}

func TestToOpenAI2Request_SanitizesResponsesOnlyFields(t *testing.T) {
	tr, err := New("openai2", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	input := map[string]interface{}{
		"model":                "gpt-5-codex",
		"message":              "继续",
		"previous_response_id": "resp_from_log",
		"store":                true,
		"tool_definitions": []map[string]interface{}{
			{
				"name":        "search",
				"description": "Search docs",
				"input_schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"q": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if _, ok := req["previous_response_id"]; ok {
		t.Fatalf("expected previous_response_id stripped, got %#v", req["previous_response_id"])
	}
	if _, ok := req["store"]; ok {
		t.Fatalf("expected store stripped, got %#v", req["store"])
	}
	if _, ok := req["instructions"]; ok {
		t.Fatalf("expected empty instructions to be dropped, got %#v", req["instructions"])
	}
	if req["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice auto, got %#v", req["tool_choice"])
	}
}

func TestToOpenAI2Request_RealAugmentLogCase(t *testing.T) {
	tr, err := New("openai2", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	input := map[string]interface{}{
		"model":                "gpt-5.4",
		"message":              "继续",
		"path":                 "docs/augmet.log",
		"lang":                 "plaintext",
		"mode":                 "agent",
		"previous_response_id": "resp_real_log_case",
		"store":                true,
		"selected_code":        strings.Repeat("x", 16000),
		"suffix":               strings.Repeat("y", 4000),
		"nodes": []map[string]interface{}{
			{
				"type": 0,
				"text_node": map[string]interface{}{
					"content": "当前日志显示 upstream 返回 400 unsupported parameter",
				},
			},
		},
		"chat_history": []map[string]interface{}{
			{
				"request_message": "先分析日志",
				"response_text":   "日志里有 400 错误",
			},
			{
				"request_message": "继续定位 augment 源码",
				"response_nodes": []map[string]interface{}{
					{
						"type": 5,
						"tool_use": map[string]interface{}{
							"tool_name":   "search_code",
							"tool_use_id": "call_hist_1",
							"input_json":  "{\"q\":\"previous_response_id\"}",
						},
					},
				},
			},
			{
				"request_nodes": []map[string]interface{}{
					{
						"type": 1,
						"tool_result_node": map[string]interface{}{
							"tool_use_id": "call_hist_1",
							"content":     "命中了 augment sanitize 链路",
						},
					},
				},
			},
		},
		"tool_definitions": []map[string]interface{}{
			{
				"name":        "search_code",
				"description": "Search codebase",
				"input_schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"q": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if _, ok := req["previous_response_id"]; ok {
		t.Fatalf("expected real log case previous_response_id stripped, got %#v", req["previous_response_id"])
	}
	if _, ok := req["store"]; ok {
		t.Fatalf("expected real log case store stripped, got %#v", req["store"])
	}
	inputItems, ok := req["input"].([]interface{})
	if !ok || len(inputItems) == 0 {
		t.Fatalf("expected responses input items, got %#v", req["input"])
	}
	foundCurrentUser := false
	foundHistoryCall := false
	foundHistoryOutput := false
	for _, raw := range inputItems {
		item := raw.(map[string]interface{})
		switch item["type"] {
		case "message":
			if item["role"] == "user" {
				if text, ok := item["content"].(string); ok && strings.Contains(text, "继续") {
					foundCurrentUser = true
				}
			}
		case "function_call":
			if item["call_id"] == "call_hist_1" && item["name"] == "search_code" {
				foundHistoryCall = true
			}
		case "function_call_output":
			if item["call_id"] == "call_hist_1" && strings.Contains(item["output"].(string), "命中了 augment sanitize 链路") {
				foundHistoryOutput = true
			}
		}
	}
	if !foundCurrentUser {
		t.Fatalf("expected current user message in real log case input")
	}
	if !foundHistoryCall {
		t.Fatalf("expected history function_call in real log case input")
	}
	if !foundHistoryOutput {
		t.Fatalf("expected history function_call_output in real log case input")
	}
}

func TestToOpenAI2Request_UsesResponsesAPIShape(t *testing.T) {
	tr, _ := New("openai2", "")
	input := AugmentRequest{
		Model:          "gpt-5-codex",
		Message:        "请继续",
		UserGuidelines: "回复简洁。",
		Mode:           "agent",
		ToolDefinitions: []ToolDefinition{
			{
				Name:        "search",
				Description: "Search docs",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"q": map[string]interface{}{"description": "query"},
						"filters": map[string]interface{}{
							"properties": map[string]interface{}{
								"lang": map[string]interface{}{"enum": []interface{}{"go", "ts"}},
							},
						},
					},
				},
			},
		},
		ChatHistory: []ChatHistoryEntry{
			{
				RequestMessage: "先查一下",
				ResponseNodes: []Node{
					{Type: 8, Thinking: &ThinkingNode{Summary: "先读取上下文", OpenAIID: "rs_hist_1", EncryptedContent: "enc_hist_1", ProviderMetadata: map[string]interface{}{"effort": "medium"}}},
					{Type: 5, ToolUse: &ToolUseNode{ToolName: "search", ToolUseID: "call_1", InputJSON: "{\"q\":\"augment\"}"}},
				},
			},
			{
				RequestNodes: []Node{
					{Type: 1, ToolResultNode: &ToolResultNode{ToolUseID: "call_1", Content: "docs found"}},
				},
			},
		},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	if req["model"] != "gpt-5-codex" {
		t.Fatalf("expected gpt-5-codex model, got %v", req["model"])
	}
	instructions, _ := req["instructions"].(string)
	if instructions == "" || !strings.Contains(instructions, "[context-history]") {
		t.Fatalf("expected instructions with context-history rule for responses api, got %q", instructions)
	}
	if req["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice auto, got %v", req["tool_choice"])
	}

	inputItems := req["input"].([]interface{})
	foundReasoning := false
	foundCall := false
	foundOutput := false
	foundUser := false
	for _, raw := range inputItems {
		item := raw.(map[string]interface{})
		switch item["type"] {
		case "reasoning":
			if item["id"] == "rs_hist_1" && item["encrypted_content"] == "enc_hist_1" {
				summary := item["summary"].([]interface{})
				providerEffort := item["effort"]
				if summary[0].(map[string]interface{})["text"] == "先读取上下文" && providerEffort == "medium" {
					foundReasoning = true
				}
			}
		case "function_call":
			if item["call_id"] == "call_1" && item["name"] == "search" && item["arguments"] == "{\"q\":\"augment\"}" {
				foundCall = true
			}
		case "function_call_output":
			if item["call_id"] == "call_1" && strings.Contains(item["output"].(string), "docs found") {
				foundOutput = true
			}
		case "message":
			if item["role"] == "user" && item["content"] == "请继续" {
				foundUser = true
			}
		}
	}
	if !foundReasoning {
		t.Fatalf("expected reasoning item in responses input")
	}
	if !foundCall {
		t.Fatalf("expected function_call item in responses input")
	}
	if !foundOutput {
		t.Fatalf("expected function_call_output item in responses input")
	}
	if !foundUser {
		t.Fatalf("expected current user message item in responses input")
	}

	tools := req["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("expected 1 responses tool, got %d", len(tools))
	}
	tool := tools[0].(map[string]interface{})
	if tool["type"] != "function" || tool["name"] != "search" || tool["strict"] != true {
		t.Fatalf("unexpected responses tool payload: %#v", tool)
	}
	params := tool["parameters"].(map[string]interface{})
	if params["additionalProperties"] != false {
		t.Fatalf("expected strict schema additionalProperties=false, got %#v", params["additionalProperties"])
	}
	props := params["properties"].(map[string]interface{})
	q := props["q"].(map[string]interface{})
	if q["type"] != "string" {
		t.Fatalf("expected missing q.type normalized to string, got %#v", q)
	}
	filters := props["filters"].(map[string]interface{})
	if filters["type"] != "object" {
		t.Fatalf("expected nested filters.type normalized to object, got %#v", filters)
	}
	lang := filters["properties"].(map[string]interface{})["lang"].(map[string]interface{})
	if lang["type"] != "string" {
		t.Fatalf("expected enum-only lang.type normalized to string, got %#v", lang)
	}
}

// --- Transformer Name ---

func TestTransformerName(t *testing.T) {
	cases := []struct {
		targetType string
		wantName   string
	}{
		{"claude", "augment_claude"},
		{"cli", "augment_cli"},
		{"openai", "augment_openai"},
		{"openai2", "augment_openai2"},
	}
	for _, tc := range cases {
		tr, _ := New(tc.targetType, "")
		if tr.Name() != tc.wantName {
			t.Errorf("targetType=%s: expected %s, got %s", tc.targetType, tc.wantName, tr.Name())
		}
	}
}

func TestTransformerInvalidTargetType(t *testing.T) {
	_, err := New("gemini", "")
	if err == nil {
		t.Error("expected error for unsupported target type gemini")
	}
}

// --- TargetPath ---

func TestTargetPath(t *testing.T) {
	if TargetPath("claude") != "/v1/messages" {
		t.Error("claude should map to /v1/messages")
	}
	if TargetPath("cli") != "/v1/messages?beta=true" {
		t.Error("cli should map to /v1/messages?beta=true")
	}
	if TargetPath("openai") != "/v1/chat/completions" {
		t.Error("openai should map to /v1/chat/completions")
	}
	if TargetPath("openai2") != "/v1/responses" {
		t.Error("openai2 should map to /v1/responses")
	}
}

// --- AugmentRequest helpers ---

func TestIsStreaming_DefaultTrue(t *testing.T) {
	req := &AugmentRequest{}
	if !req.IsStreaming() {
		t.Error("expected IsStreaming to default to true when Stream is nil")
	}
}

func TestIsStreaming_ExplicitFalse(t *testing.T) {
	f := false
	req := &AugmentRequest{Stream: &f}
	if req.IsStreaming() {
		t.Error("expected IsStreaming to return false")
	}
}

func TestEffectiveTools_ToolDefinitions(t *testing.T) {
	req := &AugmentRequest{
		ToolDefinitions: []ToolDefinition{{Name: "a"}},
		Tools:           []ToolDefinition{{Name: "b"}},
	}
	tools := req.EffectiveTools()
	if len(tools) != 1 || tools[0].Name != "a" {
		t.Error("expected ToolDefinitions to take priority over Tools")
	}
}

// --- helper ---

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestBuildOpenAIRequestFallbackPayloads_Independent(t *testing.T) {
	original := map[string]interface{}{
		"model": "gpt-4",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
		"stream": true,
		"tools": []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":       "get_weather",
					"parameters": map[string]interface{}{"type": "object"},
				},
			},
		},
		"tool_choice":         "auto",
		"parallel_tool_calls": true,
	}
	body, _ := json.Marshal(original)
	payloads := BuildRequestFallbackPayloads("openai", body)

	if len(payloads) == 0 {
		t.Fatal("expected at least one fallback payload")
	}
}

func TestBuildOpenAIRequestFallbackPayloads_ConvertToolsToFunctions(t *testing.T) {
	original := map[string]interface{}{
		"model": "gpt-3.5-turbo",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{
				"role": "assistant",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "search",
							"arguments": `{"q":"test"}`,
						},
					},
				},
			},
			map[string]interface{}{
				"role":         "tool",
				"tool_call_id": "call_1",
				"content":      "result",
			},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":       "search",
					"parameters": map[string]interface{}{"type": "object"},
				},
			},
		},
		"tool_choice": "auto",
	}
	body, _ := json.Marshal(original)
	payloads := BuildRequestFallbackPayloads("openai", body)

	var funcPayload map[string]interface{}
	for _, p := range payloads {
		if p.Name == "convert_tools_to_functions" {
			if err := json.Unmarshal(p.Body, &funcPayload); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			break
		}
	}
	if funcPayload == nil {
		t.Fatal("expected convert_tools_to_functions payload")
	}

	// Should have functions instead of tools
	if _, ok := funcPayload["functions"]; !ok {
		t.Error("expected functions key")
	}
	if _, ok := funcPayload["tools"]; ok {
		t.Error("should not have tools key")
	}

	// Messages should be converted: role:tool -> role:function
	msgs, _ := funcPayload["messages"].([]interface{})
	foundFunction := false
	for _, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if msg["role"] == "function" {
			foundFunction = true
			if msg["name"] != "search" {
				t.Errorf("expected function name 'search', got %v", msg["name"])
			}
		}
		if msg["role"] == "tool" {
			t.Error("should not have role:tool messages after conversion")
		}
		if msg["role"] == "assistant" {
			if _, ok := msg["function_call"]; !ok {
				t.Error("expected assistant to have function_call")
			}
			if _, ok := msg["tool_calls"]; ok {
				t.Error("should not have tool_calls after conversion")
			}
		}
	}
	if !foundFunction {
		t.Error("expected at least one role:function message")
	}
}

func TestBuildOpenAIRequestFallbackPayloads_StripVision(t *testing.T) {
	original := map[string]interface{}{
		"model": "gpt-4",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "What is this?"},
					map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "https://example.com/img.png"}},
				},
			},
		},
	}
	body, _ := json.Marshal(original)
	payloads := BuildRequestFallbackPayloads("openai", body)

	var visionPayload map[string]interface{}
	for _, p := range payloads {
		if p.Name == "strip_vision" {
			if err := json.Unmarshal(p.Body, &visionPayload); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			break
		}
	}
	if visionPayload == nil {
		t.Fatal("expected strip_vision payload")
	}

	msgs, _ := visionPayload["messages"].([]interface{})
	if len(msgs) == 0 {
		t.Fatal("expected messages")
	}
	msg, _ := msgs[0].(map[string]interface{})
	content, ok := msg["content"].(string)
	if !ok {
		t.Fatalf("expected content to be string after vision strip, got %T", msg["content"])
	}
	if !strings.Contains(content, "What is this?") {
		t.Error("expected text to be preserved")
	}
	if !strings.Contains(content, "[non-text content omitted]") {
		t.Error("expected non-text omission marker")
	}
}

func TestBuildOpenAIRequestFallbackPayloads_ToolToText(t *testing.T) {
	original := map[string]interface{}{
		"model": "gpt-4.1",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{
				"role": "assistant",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id":       "call_1",
						"type":     "function",
						"function": map[string]interface{}{"name": "search", "arguments": `{"q":"test"}`},
					},
				},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "result"},
		},
		"tools":       []interface{}{map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "search", "parameters": map[string]interface{}{"type": "object"}}}},
		"tool_choice": "auto",
	}
	body, _ := json.Marshal(original)
	payloads := BuildRequestFallbackPayloads("openai", body)

	var toolToText map[string]interface{}
	for _, p := range payloads {
		if p.Name == "tool_to_text" {
			if err := json.Unmarshal(p.Body, &toolToText); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			break
		}
	}
	if toolToText == nil {
		t.Fatal("expected tool_to_text payload")
	}
	if _, ok := toolToText["tools"]; ok {
		t.Fatal("expected tools to be dropped in tool_to_text payload")
	}
	msgs := toolToText["messages"].([]interface{})
	raw, _ := json.Marshal(msgs)
	if !strings.Contains(string(raw), "Historical tool call.") || !strings.Contains(string(raw), "Historical tool result.") {
		t.Fatalf("expected tool history degraded to text, got %s", raw)
	}
}

func TestBuildClaudeRequestFallbackPayloads_Independent(t *testing.T) {
	original := map[string]interface{}{
		"model": "claude-3-5-sonnet",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
		"system":      "be helpful",
		"tool_choice": map[string]interface{}{"type": "auto"},
		"tools": []interface{}{
			map[string]interface{}{"name": "search"},
		},
		"max_tokens": 1024,
	}
	body, _ := json.Marshal(original)
	payloads := BuildRequestFallbackPayloads("claude", body)

	if len(payloads) == 0 {
		t.Fatal("expected at least one fallback payload")
	}

	// Verify independence: drop_tool_choice should still have system as string
	var dropToolChoice map[string]interface{}
	for _, p := range payloads {
		if p.Name == "drop_tool_choice" {
			if err := json.Unmarshal(p.Body, &dropToolChoice); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			break
		}
	}
	if dropToolChoice == nil {
		t.Fatal("expected drop_tool_choice payload")
	}
	if _, ok := dropToolChoice["system"].(string); !ok {
		t.Error("drop_tool_choice should still have system as string (independent build)")
	}
}

func TestBuildClaudeRequestFallbackPayloads_NormalizeAllBlocks(t *testing.T) {
	original := map[string]interface{}{
		"model": "claude-3-5-sonnet",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
		"system":     "be helpful",
		"max_tokens": 1024,
	}
	body, _ := json.Marshal(original)
	payloads := BuildRequestFallbackPayloads("claude", body)

	var allBlocks map[string]interface{}
	for _, p := range payloads {
		if p.Name == "normalize_all_blocks" {
			if err := json.Unmarshal(p.Body, &allBlocks); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			break
		}
	}
	if allBlocks == nil {
		t.Fatal("expected normalize_all_blocks payload")
	}

	// System should be blocks
	systemArr, ok := allBlocks["system"].([]interface{})
	if !ok {
		t.Fatalf("expected system as array, got %T", allBlocks["system"])
	}
	if len(systemArr) == 0 {
		t.Fatal("expected non-empty system blocks")
	}
	block, _ := systemArr[0].(map[string]interface{})
	if block["type"] != "text" {
		t.Errorf("expected type=text, got %v", block["type"])
	}
}

func TestBuildClaudeRequestFallbackPayloads_ToolToText(t *testing.T) {
	original := map[string]interface{}{
		"model": "claude-3-5-sonnet",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "text", "text": "hi"}}},
			map[string]interface{}{"role": "assistant", "content": []interface{}{map[string]interface{}{"type": "tool_use", "id": "tu_1", "name": "search", "input": map[string]interface{}{"q": "test"}}}},
			map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "tool_result", "tool_use_id": "tu_1", "content": "result"}}},
		},
		"tools":       []interface{}{map[string]interface{}{"name": "search"}},
		"tool_choice": map[string]interface{}{"type": "auto"},
		"max_tokens":  1024,
	}
	body, _ := json.Marshal(original)
	payloads := BuildRequestFallbackPayloads("claude", body)

	var toolToText map[string]interface{}
	for _, p := range payloads {
		if p.Name == "tool_to_text" {
			if err := json.Unmarshal(p.Body, &toolToText); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			break
		}
	}
	if toolToText == nil {
		t.Fatal("expected tool_to_text payload")
	}
	if _, ok := toolToText["tools"]; ok {
		t.Fatal("expected tools to be dropped in tool_to_text payload")
	}
	raw, _ := json.Marshal(toolToText["messages"])
	if !strings.Contains(string(raw), "Historical tool call.") || !strings.Contains(string(raw), "Historical tool result.") {
		t.Fatalf("expected claude tool history degraded to text, got %s", raw)
	}
}

func TestEnsureClaudeFirstMessageIsUser(t *testing.T) {
	payload := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "assistant", "content": "hello"},
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	changed := ensureClaudeFirstMessageIsUser(payload)
	if !changed {
		t.Fatal("expected change")
	}
	msgs, _ := payload["messages"].([]interface{})
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	first, _ := msgs[0].(map[string]interface{})
	if first["role"] != "user" {
		t.Errorf("expected first message to be user, got %v", first["role"])
	}
}

func TestRepairClaudeToolUsePairs(t *testing.T) {
	payload := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": []interface{}{
				map[string]interface{}{"type": "text", "text": "hi"},
			}},
			map[string]interface{}{"role": "assistant", "content": []interface{}{
				map[string]interface{}{"type": "tool_use", "id": "tu_1", "name": "search", "input": map[string]interface{}{"q": "test"}},
			}},
			// Missing tool_result for tu_1, next is user without tool_result
			map[string]interface{}{"role": "user", "content": []interface{}{
				map[string]interface{}{"type": "text", "text": "thanks"},
			}},
		},
	}
	changed := repairClaudeToolUsePairs(payload)
	if !changed {
		t.Fatal("expected repair to make changes")
	}

	msgs, _ := payload["messages"].([]interface{})
	// Should inject a user message with tool_result between assistant and next user
	foundInjected := false
	for _, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		for _, blockRaw := range content {
			block, ok := blockRaw.(map[string]interface{})
			if !ok {
				continue
			}
			if block["type"] == "tool_result" && block["tool_use_id"] == "tu_1" {
				foundInjected = true
				if block["is_error"] != true {
					t.Error("expected injected tool_result to be is_error=true")
				}
			}
		}
	}
	if !foundInjected {
		t.Error("expected injected tool_result for tu_1")
	}
}
