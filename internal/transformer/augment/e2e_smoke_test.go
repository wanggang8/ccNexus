package augment

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEndToEndSmoke_Claude(t *testing.T) {
	tr, err := New("claude", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reqBody, err := json.Marshal(AugmentRequest{
		Model:               "claude-sonnet-4-20250514",
		Message:             "请继续",
		UserGuidelines:      "简洁回答。",
		WorkspaceGuidelines: "遵循本地仓库规范。",
		ToolDefinitions: []ToolDefinition{
			{
				Name:        "search",
				Description: "Search docs",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"q": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
		Nodes: []Node{
			{Type: 4, IdeStateNode: &IdeStateNode{WorkspaceFolders: []WorkspaceFolder{{FolderRoot: "/repo"}}}},
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	transformed, err := tr.TransformRequest(reqBody)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var outbound map[string]interface{}
	if err := json.Unmarshal(transformed, &outbound); err != nil {
		t.Fatalf("unmarshal transformed: %v", err)
	}
	if outbound["model"] != "claude-sonnet-4-20250514" {
		t.Fatalf("unexpected model: %#v", outbound["model"])
	}
	messages := outbound["messages"].([]interface{})
	if len(messages) != 1 {
		t.Fatalf("expected 1 current user message, got %d", len(messages))
	}

	sse := "" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"先查一下\"}}\n\n" +
		"data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool_1\",\"name\":\"search\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"q\\\":\\\"augment\\\"}\"}}\n\n" +
		"data: {\"type\":\"content_block_stop\"}\n\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":6}}\n\n" +
		"data: {\"type\":\"message_stop\",\"usage\":{\"input_tokens\":12,\"output_tokens\":6}}\n\n"

	var out strings.Builder
	if _, _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &out, "claude", tr.GetToolContext()); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, out.String())
	if len(lines) == 0 {
		t.Fatalf("expected ndjson output")
	}
	if lines[len(lines)-1]["stop_reason"] != float64(augmentStopReasonToolUseRequested) {
		t.Fatalf("expected final stop reason tool_use_requested, got %#v", lines[len(lines)-1]["stop_reason"])
	}
}

func TestEndToEndSmoke_OpenAI(t *testing.T) {
	tr, err := New("openai", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reqBody, err := json.Marshal(AugmentRequest{
		Model:          "gpt-4.1",
		Message:        "继续分析",
		UserGuidelines: "不要展开无关内容。",
		ChatHistory: []ChatHistoryEntry{
			{
				RequestMessage: "先看文档",
				ResponseNodes: []Node{
					{Type: 7, ToolUse: &ToolUseNode{ToolUseID: "call_1", ToolName: "search", InputJSON: "{\"q\":\"augment\"}"}},
				},
			},
			{
				RequestNodes: []Node{
					{Type: 1, ToolResultNode: &ToolResultNode{ToolUseID: "call_1", Content: "docs found"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	transformed, err := tr.TransformRequest(reqBody)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var outbound map[string]interface{}
	if err := json.Unmarshal(transformed, &outbound); err != nil {
		t.Fatalf("unmarshal transformed: %v", err)
	}
	messages := outbound["messages"].([]interface{})
	if len(messages) < 3 {
		t.Fatalf("expected repaired history plus current user message, got %d", len(messages))
	}

	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"thinking\":\"先整理历史\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"给你结果\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_2\",\"function\":{\"name\":\"search\",\"arguments\":\"{\\\"q\\\":\\\"followup\\\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n"

	var out strings.Builder
	if _, _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &out, "openai", tr.GetToolContext()); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, out.String())
	if len(lines) == 0 {
		t.Fatalf("expected ndjson output")
	}
	if lines[len(lines)-1]["stop_reason"] != float64(augmentStopReasonToolUseRequested) {
		t.Fatalf("expected final stop reason tool_use_requested, got %#v", lines[len(lines)-1]["stop_reason"])
	}
}

func TestEndToEndSmoke_OpenAIResponses(t *testing.T) {
	tr, err := New("openai2", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	reqBody, err := json.Marshal(AugmentRequest{
		Model:         "gpt-5-codex",
		Message:       "继续",
		AgentMemories: "之前已经确认改用 responses api",
		ToolDefinitions: []ToolDefinition{
			{
				Name:        "search",
				Description: "Search docs",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"q": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
		Nodes: []Node{
			{Type: 8, FileNode: &FileNode{FileData: "SGVsbG8=", Format: "text/plain"}},
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	transformed, err := tr.TransformRequest(reqBody)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}

	var outbound map[string]interface{}
	if err := json.Unmarshal(transformed, &outbound); err != nil {
		t.Fatalf("unmarshal transformed: %v", err)
	}
	if outbound["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice auto, got %#v", outbound["tool_choice"])
	}

	jsonResp := []byte(`{
		"output":[
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"已处理"}]},
			{"type":"reasoning","summary":[{"type":"summary_text","text":"先读取上下文"}]},
			{"type":"function_call","call_id":"call_3","name":"search","arguments":"{\"q\":\"augment byok\"}"}
		],
		"usage":{"input_tokens":15,"output_tokens":9}
	}`)
	_, _, _, ndjson, err := ConvertJSONToNDJSON(jsonResp, "openai2", tr.GetToolContext())
	if err != nil {
		t.Fatalf("ConvertJSONToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, string(ndjson))
	if len(lines) == 0 {
		t.Fatalf("expected ndjson output")
	}
	if lines[len(lines)-1]["stop_reason"] != float64(augmentStopReasonToolUseRequested) {
		t.Fatalf("expected final stop reason tool_use_requested, got %#v", lines[len(lines)-1]["stop_reason"])
	}
}
