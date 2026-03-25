package augment

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestTransformAugmentToClaude_InjectsWorkspaceGuidelinesContextAndHistorySummary(t *testing.T) {
	transformer, err := New("claude", "")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	input := map[string]interface{}{
		"model":                "claude-sonnet-4-20250514",
		"message":              "继续处理当前文件",
		"workspace_guidelines": "遵循仓库规范，先修复测试。",
		"user_guidelines":      "回复尽量简洁。",
		"context": map[string]interface{}{
			"path":          "internal/transformer/augment/to_claude.go",
			"lang":          "go",
			"prefix":        "func example() {",
			"selected_code": "return oldValue",
			"suffix":        "}",
			"diff":          "- return oldValue\n+ return newValue",
		},
		"structured_request_nodes": []interface{}{
			map[string]interface{}{
				"type": 10,
				"history_summary": map[string]interface{}{
					"text": "用户已经确认默认启用所有兼容增强。",
				},
			},
		},
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input failed: %v", err)
	}

	result, err := transformer.TransformRequest(inputJSON)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var output map[string]interface{}
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("unmarshal output failed: %v", err)
	}

	systemBlocks, ok := output["system"].([]interface{})
	if !ok || len(systemBlocks) == 0 {
		t.Fatalf("expected non-empty system blocks, got %#v", output["system"])
	}

	systemText := systemBlocks[0].(map[string]interface{})["text"].(string)
	assertContains(t, systemText, "遵循仓库规范，先修复测试。")
	assertContains(t, systemText, "回复尽量简洁。")
	assertContains(t, systemText, "[context]")
	assertContains(t, systemText, "path=internal/transformer/augment/to_claude.go")
	assertContains(t, systemText, "lang=go")
	assertNotContains(t, systemText, "[selected_code]")
	assertOrderedContains(t, systemText, []string{"遵循仓库规范，先修复测试。", "回复尽量简洁。", "[context]"})

	messages, ok := output["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		t.Fatalf("expected messages, got %#v", output["messages"])
	}
	messageText, ok := messages[0].(map[string]interface{})["content"].(string)
	if !ok {
		t.Fatalf("expected string user content, got %#v", messages[0].(map[string]interface{})["content"])
	}
	assertContains(t, messageText, "继续处理当前文件")
	assertContains(t, messageText, "[prefix]")
	assertContains(t, messageText, "[selected_code]")
	assertContains(t, messageText, "[suffix]")
	assertContains(t, messageText, "[diff]")
	assertContains(t, messageText, "[history_summary]")
	assertContains(t, messageText, "用户已经确认默认启用所有兼容增强。")
}

func TestTransformAugmentToOpenAI_InjectsWorkspaceGuidelinesContextAndHistorySummary(t *testing.T) {
	transformer, err := New("openai", "")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	input := map[string]interface{}{
		"model":                "gpt-4.1",
		"message":              "解释一下这段代码",
		"workspace_guidelines": "优先依据本地代码事实。",
		"user_guidelines":      "不要展开无关内容。",
		"context": map[string]interface{}{
			"path":          "internal/transformer/augment/to_openai.go",
			"lang":          "go",
			"selected_code": "messages = append(messages, current)",
		},
		"request_nodes": []interface{}{
			map[string]interface{}{
				"type": 10,
				"history_summary": map[string]interface{}{
					"text": "上一轮已经完成字段兼容性分析。",
				},
			},
		},
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input failed: %v", err)
	}

	result, err := transformer.TransformRequest(inputJSON)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var output map[string]interface{}
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("unmarshal output failed: %v", err)
	}

	messages, ok := output["messages"].([]interface{})
	if !ok || len(messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %#v", output["messages"])
	}

	systemText := messages[0].(map[string]interface{})["content"].(string)
	assertContains(t, systemText, "优先依据本地代码事实。")
	assertContains(t, systemText, "不要展开无关内容。")
	assertContains(t, systemText, "[context]")
	assertContains(t, systemText, "path=internal/transformer/augment/to_openai.go")
	assertContains(t, systemText, "lang=go")
	assertNotContains(t, systemText, "[selected_code]")
	assertOrderedContains(t, systemText, []string{"优先依据本地代码事实。", "不要展开无关内容。", "[context]"})

	userText := messages[1].(map[string]interface{})["content"].(string)
	assertContains(t, userText, "解释一下这段代码")
	assertContains(t, userText, "[selected_code]")
	assertContains(t, userText, "[history_summary]")
	assertContains(t, userText, "上一轮已经完成字段兼容性分析。")
}

func TestTransformAugmentToClaude_UsesStructuredRequestNodesAndContentNodes(t *testing.T) {
	transformer, err := New("claude", "")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	imageData := base64.StdEncoding.EncodeToString([]byte("fakepngdata"))
	input := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"structured_request_nodes": []interface{}{
			map[string]interface{}{
				"type": 0,
				"text_node": map[string]interface{}{
					"text": "请结合工具输出总结问题。",
				},
			},
			map[string]interface{}{
				"type": 1,
				"tool_result_node": map[string]interface{}{
					"tool_call_id": "toolu_123",
					"tool_result":  "命令执行成功",
					"content_nodes": []interface{}{
						map[string]interface{}{
							"type": "text",
							"text": "stdout: ok",
						},
						map[string]interface{}{
							"type":       "image",
							"media_type": "image/png",
							"data":       imageData,
						},
					},
				},
			},
		},
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input failed: %v", err)
	}

	result, err := transformer.TransformRequest(inputJSON)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var output map[string]interface{}
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("unmarshal output failed: %v", err)
	}

	messages := output["messages"].([]interface{})
	if len(messages) != 2 {
		t.Fatalf("expected tool_result message and user text message, got %d", len(messages))
	}

	first := messages[0].(map[string]interface{})
	if first["role"] != "user" {
		t.Fatalf("expected first message role user, got %#v", first)
	}
	toolBlocks, ok := first["content"].([]interface{})
	if !ok || len(toolBlocks) != 1 {
		t.Fatalf("expected one tool_result block, got %#v", first["content"])
	}
	toolResultBlock := toolBlocks[0].(map[string]interface{})
	if toolResultBlock["type"] != "tool_result" {
		t.Fatalf("expected tool_result block, got %#v", toolResultBlock)
	}
	if toolResultBlock["tool_use_id"] != "toolu_123" {
		t.Fatalf("expected tool_use_id toolu_123, got %#v", toolResultBlock["tool_use_id"])
	}
	toolContent := toolResultBlock["content"].([]interface{})
	if len(toolContent) != 2 {
		t.Fatalf("expected 2 content nodes in tool result, got %#v", toolContent)
	}
	if toolContent[0].(map[string]interface{})["text"] != "stdout: ok" {
		t.Fatalf("expected text content node, got %#v", toolContent[0])
	}
	imageBlock := toolContent[1].(map[string]interface{})
	if imageBlock["type"] != "image" {
		t.Fatalf("expected image content node, got %#v", imageBlock)
	}

	second := messages[1].(map[string]interface{})
	if second["content"] != "请结合工具输出总结问题。" {
		t.Fatalf("expected text message from structured_request_nodes, got %#v", second["content"])
	}
}

func TestTransformAugmentToOpenAI_UsesStructuredRequestNodesAndToolResultContentNodes(t *testing.T) {
	transformer, err := New("openai", "")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	imageData := base64.StdEncoding.EncodeToString([]byte("fakepngdata"))
	input := map[string]interface{}{
		"model": "gpt-4.1",
		"structured_request_nodes": []interface{}{
			map[string]interface{}{
				"type": 0,
				"text_node": map[string]interface{}{
					"text": "请分析命令输出。",
				},
			},
			map[string]interface{}{
				"type": 1,
				"tool_result_node": map[string]interface{}{
					"tool_call_id": "call_456",
					"content_nodes": []interface{}{
						map[string]interface{}{
							"type": "text",
							"text": "stderr: warning",
						},
						map[string]interface{}{
							"type":       "image",
							"media_type": "image/png",
							"data":       imageData,
						},
					},
				},
			},
		},
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input failed: %v", err)
	}

	result, err := transformer.TransformRequest(inputJSON)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var output map[string]interface{}
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("unmarshal output failed: %v", err)
	}

	messages := output["messages"].([]interface{})
	if len(messages) != 3 {
		t.Fatalf("expected assistant, tool and user messages, got %d", len(messages))
	}
	if messages[0].(map[string]interface{})["role"] != "assistant" {
		t.Fatalf("expected first message to be assistant, got %#v", messages[0])
	}
	if messages[1].(map[string]interface{})["role"] != "tool" {
		t.Fatalf("expected second message to be tool, got %#v", messages[1])
	}
	toolContent, ok := messages[1].(map[string]interface{})["content"].(string)
	if !ok {
		t.Fatalf("expected string tool content, got %#v", messages[1].(map[string]interface{})["content"])
	}
	assertContains(t, toolContent, "stderr: warning")
	assertContains(t, toolContent, "[tool_result_image]")
	assertContains(t, toolContent, "media_type=image/png")
	if messages[1].(map[string]interface{})["tool_call_id"] != "call_456" {
		t.Fatalf("expected tool_call_id call_456, got %#v", messages[1].(map[string]interface{})["tool_call_id"])
	}
	if messages[2].(map[string]interface{})["role"] != "user" {
		t.Fatalf("expected third message to be user, got %#v", messages[2])
	}
	if messages[2].(map[string]interface{})["content"] != "请分析命令输出。" {
		t.Fatalf("expected user text from structured_request_nodes, got %#v", messages[2].(map[string]interface{})["content"])
	}
}

func TestTransformAugmentToClaude_AcceptsCamelCaseInputSchemaAlias(t *testing.T) {
	transformer, err := New("claude", "")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	input := map[string]interface{}{
		"model":   "claude-sonnet-4-20250514",
		"message": "执行操作",
		"toolDefinitions": []interface{}{
			map[string]interface{}{
				"name":        "run_command",
				"description": "执行命令",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"cmd": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input failed: %v", err)
	}

	result, err := transformer.TransformRequest(inputJSON)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var output map[string]interface{}
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("unmarshal output failed: %v", err)
	}

	tools := output["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("expected one tool, got %#v", tools)
	}
	inputSchema := tools[0].(map[string]interface{})["input_schema"].(map[string]interface{})
	properties := inputSchema["properties"].(map[string]interface{})
	if _, ok := properties["cmd"]; !ok {
		t.Fatalf("expected cmd property, got %#v", properties)
	}
}

func TestTransformAugmentToClaude_PreservesToolResultIsError(t *testing.T) {
	transformer, err := New("claude", "")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	input := map[string]interface{}{
		"model": "claude-sonnet-4-20250514",
		"structured_request_nodes": []interface{}{
			map[string]interface{}{
				"type": 1,
				"tool_result_node": map[string]interface{}{
					"tool_call_id": "toolu_error",
					"tool_result":  "command failed",
					"is_error":     true,
				},
			},
		},
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input failed: %v", err)
	}

	result, err := transformer.TransformRequest(inputJSON)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var output map[string]interface{}
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("unmarshal output failed: %v", err)
	}

	messages := output["messages"].([]interface{})
	toolBlocks := messages[0].(map[string]interface{})["content"].([]interface{})
	toolResultBlock := toolBlocks[0].(map[string]interface{})
	if toolResultBlock["is_error"] != true {
		t.Fatalf("expected tool_result.is_error=true, got %#v", toolResultBlock["is_error"])
	}
}

func TestCountCurrentMessages_IgnoresDedupedIdeStateOnlyTurn(t *testing.T) {
	ideState := &IdeStateNode{
		WorkspaceFolders: []WorkspaceFolder{{FolderRoot: "/project"}},
	}
	ar := &AugmentRequest{
		ChatHistory: []ChatHistoryEntry{{
			RequestNodes: []Node{{Type: 4, IdeStateNode: ideState}},
			ResponseText: "done",
		}},
		Nodes: []Node{{Type: 4, IdeStateNode: ideState}},
	}

	if got := countCurrentMessages(ar); got != 0 {
		t.Fatalf("expected current message count 0 when only duplicated ide_state exists, got %d", got)
	}
}

func TestFormatHistorySummaryPrompt_PreservesHistoryEndPayload(t *testing.T) {
	text := formatHistorySummaryPrompt(&HistorySummaryNode{
		Text:       "summary",
		HistoryEnd: []map[string]interface{}{{"role": "user", "content": "hello"}},
	})

	assertContains(t, text, "[history_summary]")
	assertContains(t, text, "history_end=")
	assertContains(t, text, "\"content\":\"hello\"")
	assertContains(t, text, "\"role\":\"user\"")
	assertNotContains(t, text, "history_end_exchanges=1")
}

func TestTransformAugmentToOpenAI_FallsBackToolResultImagesToText(t *testing.T) {
	transformer, err := New("openai", "")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	imageData := base64.StdEncoding.EncodeToString([]byte("fakepngdata"))
	input := map[string]interface{}{
		"model": "gpt-4.1",
		"structured_request_nodes": []interface{}{
			map[string]interface{}{
				"type": 1,
				"tool_result_node": map[string]interface{}{
					"tool_call_id": "call_img",
					"content_nodes": []interface{}{
						map[string]interface{}{
							"type": "text",
							"text": "stderr: warning",
						},
						map[string]interface{}{
							"type":       "image",
							"media_type": "image/png",
							"data":       imageData,
						},
					},
				},
			},
		},
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input failed: %v", err)
	}

	result, err := transformer.TransformRequest(inputJSON)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var output map[string]interface{}
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("unmarshal output failed: %v", err)
	}

	messages := output["messages"].([]interface{})
	toolContent, ok := messages[1].(map[string]interface{})["content"].(string)
	if !ok {
		t.Fatalf("expected tool content to be string fallback, got %#v", messages[1].(map[string]interface{})["content"])
	}
	assertContains(t, toolContent, "stderr: warning")
	assertContains(t, toolContent, "[tool_result_image]")
	assertContains(t, toolContent, "media_type=image/png")
}

func TestFormatHistorySummaryPrompt_ReadableHistoryEnd(t *testing.T) {
	text := formatHistorySummaryPrompt(&HistorySummaryNode{
		Text: "summary",
		HistoryEnd: []map[string]interface{}{
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": "world"},
		},
	})

	assertContains(t, text, "history_end=")
	assertContains(t, text, "\n  {\"content\":\"hello\",\"role\":\"user\"}")
	assertContains(t, text, "\n  {\"content\":\"world\",\"role\":\"assistant\"}")
}

func TestEffectiveCurrentNodes_DedupsEquivalentImageRepresentations(t *testing.T) {
	req := &AugmentRequest{
		Nodes:                  []Node{{Type: 2, ImageNode: &ImageNode{ImageData: "ZmFrZQ==", Format: 1}}},
		StructuredRequestNodes: []Node{{Type: 2, ImageNode: &ImageNode{ImageData: "data:image/png;base64,ZmFrZQ==", Format: 1}}},
	}

	nodes := req.EffectiveCurrentNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected equivalent image nodes to dedup to 1, got %d (%#v)", len(nodes), nodes)
	}
}

func TestEffectiveCurrentNodes_DedupsEquivalentToolResultImageRepresentations(t *testing.T) {
	req := &AugmentRequest{
		Nodes: []Node{{Type: 1, ToolResultNode: &ToolResultNode{
			ToolUseID: "tool-1",
			ContentNodes: []ToolResultContentNode{{
				Type:      "image",
				MediaType: "image/png",
				Data:      "ZmFrZQ==",
			}},
		}}},
		StructuredRequestNodes: []Node{{Type: 1, ToolResultNode: &ToolResultNode{
			ToolUseID: "tool-1",
			ContentNodes: []ToolResultContentNode{{
				Type:      "image",
				MediaType: "IMAGE/PNG",
				Data:      "data:image/png;base64,ZmFrZQ==",
			}},
		}}},
	}

	nodes := req.EffectiveCurrentNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected equivalent tool_result image nodes to dedup to 1, got %d (%#v)", len(nodes), nodes)
	}
}

func TestEffectiveCurrentNodes_MergesWithDedup(t *testing.T) {
	req := &AugmentRequest{
		Nodes:                  []Node{{Type: 0, TextNode: &TextNode{Content: "same text"}}},
		StructuredRequestNodes: []Node{{Type: 0, TextNode: &TextNode{Text: "same text"}}},
		RequestNodes:           []Node{{Type: 0, TextNode: &TextNode{Content: "same text"}}},
	}

	nodes := req.EffectiveCurrentNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected deduped current nodes length 1, got %d (%#v)", len(nodes), nodes)
	}
}

func TestEffectiveRequestNodes_MergesWithDedup(t *testing.T) {
	entry := &ChatHistoryEntry{
		RequestNodes:           []Node{{Type: 0, TextNode: &TextNode{Content: "same history text"}}},
		StructuredRequestNodes: []Node{{Type: 0, TextNode: &TextNode{Text: "same history text"}}},
	}

	nodes := entry.EffectiveRequestNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected deduped history nodes length 1, got %d (%#v)", len(nodes), nodes)
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected %q to contain %q", got, want)
	}
}

func assertNotContains(t *testing.T, got, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Fatalf("expected %q not to contain %q", got, want)
	}
}

func assertOrderedContains(t *testing.T, got string, wants []string) {
	t.Helper()
	last := -1
	for _, want := range wants {
		idx := strings.Index(got, want)
		if idx == -1 {
			t.Fatalf("expected %q to contain %q", got, want)
		}
		if idx < last {
			t.Fatalf("expected %q to appear after previous section in %q", want, got)
		}
		last = idx
	}
}
