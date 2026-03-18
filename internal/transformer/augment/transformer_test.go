package augment

import (
	"encoding/json"
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

func TestToClaudeRequest_IdeStateDedup(t *testing.T) {
	tr, _ := New("claude", "")
	ideState := &IdeStateNode{
		WorkspaceFolders: []WorkspaceFolder{{FolderRoot: "/project"}},
	}
	input := AugmentRequest{
		Message: "test",
		ChatHistory: []ChatHistoryEntry{
			{
				RequestNodes: []Node{{Type: 4, IdeStateNode: ideState}},
				RequestMessage: "prev",
				ResponseText:  "response",
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
	found := false
	for _, m := range msgs {
		msg := m.(map[string]interface{})
		if msg["role"] == "tool" {
			found = true
			if msg["tool_call_id"] != "call_abc" {
				t.Errorf("expected tool_call_id call_abc, got %v", msg["tool_call_id"])
			}
		}
	}
	if !found {
		t.Error("expected tool role message")
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
