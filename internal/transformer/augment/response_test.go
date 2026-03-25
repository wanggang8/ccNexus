package augment

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/tokencount"
)

func readNDJSONLines(t *testing.T, s string) []map[string]interface{} {
	t.Helper()
	var out []map[string]interface{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("invalid json line: %v\nline=%s", err, line)
		}
		out = append(out, obj)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return out
}

func collectTokenUsageNodes(lines []map[string]interface{}) []map[string]interface{} {
	var usageNodes []map[string]interface{}
	for _, obj := range lines {
		nodes, _ := obj["nodes"].([]interface{})
		for _, n := range nodes {
			node, _ := n.(map[string]interface{})
			if node["type"] == float64(augmentNodeTypeTokenUsage) {
				usage, _ := node["token_usage"].(map[string]interface{})
				usageNodes = append(usageNodes, usage)
			}
		}
	}
	return usageNodes
}

func TestStreamConvertClaude_TextDeltaToNDJSONText(t *testing.T) {
	sse := "" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "claude", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())
	if len(lines) < 2 {
		t.Fatalf("expected >=2 lines, got %d", len(lines))
	}
	if lines[0]["text"] != "Hi" {
		t.Fatalf("expected first text Hi, got %#v", lines[0]["text"])
	}
	if _, ok := lines[len(lines)-1]["stop_reason"]; !ok {
		t.Fatalf("expected final stop_reason, got %#v", lines[len(lines)-1])
	}
}
func TestStreamConvertClaude_ToolUseBufferedAsNodes(t *testing.T) {
	sse := "" +
		"data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool_1\",\"name\":\"read_file\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"README.md\\\"\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"}\"}}\n\n" +
		"data: {\"type\":\"content_block_stop\"}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "claude", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())

	// Should have TOOL_USE_START (type=7) and TOOL_USE (type=5) nodes
	foundStart := false
	foundTool := false

	for _, obj := range lines {
		nodes, _ := obj["nodes"].([]interface{})
		if len(nodes) == 0 {
			continue
		}
		n0, _ := nodes[0].(map[string]interface{})
		nodeType := n0["type"].(float64)

		// Check for TOOL_USE_START (type=7)
		if nodeType == float64(augmentNodeTypeToolUseStart) {
			tu, _ := n0["tool_use"].(map[string]interface{})
			if tu["tool_name"] == "read_file" && tu["tool_use_id"] == "tool_1" {
				foundStart = true
				if tu["input_json"] != "" {
					t.Fatalf("TOOL_USE_START should have empty input_json, got %#v", tu["input_json"])
				}
			}
		}

		// Check for TOOL_USE (type=5)
		if nodeType == float64(augmentNodeTypeToolUse) {
			tu, _ := n0["tool_use"].(map[string]interface{})
			if tu["tool_name"] == "read_file" && tu["tool_use_id"] == "tool_1" {
				foundTool = true
				if tu["input_json"] != "{\"path\":\"README.md\"}" {
					t.Fatalf("unexpected input_json: %#v", tu["input_json"])
				}
				if obj["stop_reason"] != float64(augmentStopReasonToolUseRequested) {
					t.Fatalf("expected stop_reason tool_use_requested, got %#v", obj["stop_reason"])
				}
			}
		}
	}

	if !foundStart {
		t.Fatalf("expected TOOL_USE_START node (type=7) in output")
	}
	if !foundTool {
		t.Fatalf("expected TOOL_USE node (type=5) in output")
	}
}

func TestStreamConvertOpenAI_ToolCallsFinishEmitNodes(t *testing.T) {
	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"{\\\"q\\\":\\\"a\\\"\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "openai", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())
	found := false
	for _, obj := range lines {
		nodes, _ := obj["nodes"].([]interface{})
		for _, n := range nodes {
			node, _ := n.(map[string]interface{})
			if node["type"] == float64(augmentNodeTypeToolUse) {
				tu, _ := node["tool_use"].(map[string]interface{})
				if tu["tool_use_id"] == "call_1" && tu["tool_name"] == "search" {
					found = true
					if tu["input_json"] != "{\"q\":\"a\"}" {
						t.Fatalf("unexpected input_json: %#v", tu["input_json"])
					}
					if obj["stop_reason"] != float64(augmentStopReasonToolUseRequested) {
						t.Fatalf("expected stop_reason tool_use_requested, got %#v", obj["stop_reason"])
					}
				}
			}
		}
	}
	if !found {
		t.Fatalf("expected tool_calls -> nodes output")
	}
}

func TestStreamConvertClaude_StopReasonMapping(t *testing.T) {
	tests := []struct {
		name           string
		stopReason     string
		expectedReason int
	}{
		{"end_turn", "end_turn", augmentStopReasonEndTurn},
		{"max_tokens", "max_tokens", augmentStopReasonMaxTokens},
		{"tool_use", "tool_use", augmentStopReasonToolUseRequested},
		{"safety", "safety", augmentStopReasonSafety},
		{"recitation", "recitation", augmentStopReasonRecitation},
		{"stop_sequence", "stop_sequence", augmentStopReasonEndTurn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sse := "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"" + tt.stopReason + "\"}}\n\n"
			var b strings.Builder
			if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "claude", nil); err != nil {
				t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
			}
			lines := readNDJSONLines(t, b.String())
			if len(lines) == 0 {
				t.Fatalf("expected at least one line")
			}
			if lines[0]["stop_reason"] != float64(tt.expectedReason) {
				t.Fatalf("expected stop_reason %d, got %#v", tt.expectedReason, lines[0]["stop_reason"])
			}
		})
	}
}

func TestStreamConvertOpenAI_FinishReasonMapping(t *testing.T) {
	tests := []struct {
		name           string
		finishReason   string
		expectedReason int
	}{
		{"stop", "stop", augmentStopReasonEndTurn},
		{"length", "length", augmentStopReasonMaxTokens},
		{"content_filter", "content_filter", augmentStopReasonSafety},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sse := "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"" + tt.finishReason + "\"}]}\n\n"
			var b strings.Builder
			if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "openai", nil); err != nil {
				t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
			}
			lines := readNDJSONLines(t, b.String())
			if len(lines) == 0 {
				t.Fatalf("expected at least one line")
			}
			if lines[0]["stop_reason"] != float64(tt.expectedReason) {
				t.Fatalf("expected stop_reason %d, got %#v", tt.expectedReason, lines[0]["stop_reason"])
			}
		})
	}
}

func TestStreamConvertClaude_TokenUsageNode(t *testing.T) {
	sse := "" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":50}}\n\n" +
		"data: {\"type\":\"message_stop\",\"usage\":{\"input_tokens\":100,\"output_tokens\":50}}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "claude", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())

	// Collect all TOKEN_USAGE nodes; the last one (from message_stop) should have full data.
	var usageNodes []map[string]interface{}
	for _, obj := range lines {
		nodes, _ := obj["nodes"].([]interface{})
		if len(nodes) == 0 {
			continue
		}
		n0, _ := nodes[0].(map[string]interface{})
		if n0["type"] == float64(augmentNodeTypeTokenUsage) {
			usage, _ := n0["token_usage"].(map[string]interface{})
			usageNodes = append(usageNodes, usage)
		}
	}

	if len(usageNodes) == 0 {
		t.Fatalf("expected at least one TOKEN_USAGE node (type=10) in output")
	}

	// The last TOKEN_USAGE node (from message_stop) should contain both input and output tokens.
	lastUsage := usageNodes[len(usageNodes)-1]
	if _, ok := lastUsage["input_tokens"]; !ok {
		t.Fatalf("expected input_tokens in last TOKEN_USAGE node, got %#v", lastUsage)
	}
	if _, ok := lastUsage["output_tokens"]; !ok {
		t.Fatalf("expected output_tokens in last TOKEN_USAGE node, got %#v", lastUsage)
	}
}

func TestStreamConvertOpenAI_TokenUsageNode(t *testing.T) {
	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":80,\"completion_tokens\":40,\"total_tokens\":120}}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "openai", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())

	foundUsage := false
	for _, obj := range lines {
		nodes, _ := obj["nodes"].([]interface{})
		if len(nodes) == 0 {
			continue
		}
		n0, _ := nodes[0].(map[string]interface{})
		if n0["type"] == float64(augmentNodeTypeTokenUsage) {
			foundUsage = true
			usage, _ := n0["token_usage"].(map[string]interface{})
			// OpenAI uses prompt_tokens/completion_tokens, we map to input_tokens/output_tokens
			if usage["input_tokens"] != float64(80) {
				t.Fatalf("expected input_tokens 80, got %#v", usage["input_tokens"])
			}
			if usage["output_tokens"] != float64(40) {
				t.Fatalf("expected output_tokens 40, got %#v", usage["output_tokens"])
			}
		}
	}

	if !foundUsage {
		t.Fatalf("expected TOKEN_USAGE node (type=10) in output")
	}
}

func TestStreamConvertClaude_MCPFieldsInToolUse(t *testing.T) {
	sse := "" +
		"data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool_1\",\"name\":\"read_file\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"test.txt\\\"}\"}}\n\n" +
		"data: {\"type\":\"content_block_stop\"}\n\n"

	toolCtx := map[string]*ToolContext{
		"read_file": {
			McpServerName: "filesystem",
			McpToolName:   "read_file",
		},
	}

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "claude", toolCtx); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())

	// Verify TOOL_USE_START node has MCP fields
	foundStart := false
	foundTool := false
	for _, line := range lines {
		if nodes, ok := line["nodes"].([]interface{}); ok {
			for _, n := range nodes {
				node := n.(map[string]interface{})
				nodeType := int(node["type"].(float64))
				if nodeType == 7 { // TOOL_USE_START
					foundStart = true
					toolUse := node["tool_use"].(map[string]interface{})
					if toolUse["mcp_server_name"] != "filesystem" {
						t.Errorf("Expected mcp_server_name=filesystem, got %v", toolUse["mcp_server_name"])
					}
					if toolUse["mcp_tool_name"] != "read_file" {
						t.Errorf("Expected mcp_tool_name=read_file, got %v", toolUse["mcp_tool_name"])
					}
				}
				if nodeType == 5 { // TOOL_USE
					foundTool = true
					toolUse := node["tool_use"].(map[string]interface{})
					if toolUse["mcp_server_name"] != "filesystem" {
						t.Errorf("Expected mcp_server_name=filesystem in TOOL_USE, got %v", toolUse["mcp_server_name"])
					}
					if toolUse["mcp_tool_name"] != "read_file" {
						t.Errorf("Expected mcp_tool_name=read_file in TOOL_USE, got %v", toolUse["mcp_tool_name"])
					}
				}
			}
		}
	}
	if !foundStart {
		t.Error("Expected TOOL_USE_START node (type=7) not found")
	}
	if !foundTool {
		t.Error("Expected TOOL_USE node (type=5) not found")
	}
}

func TestStreamConvertOpenAI_MCPFieldsInToolUse(t *testing.T) {
	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"search\",\"arguments\":\"\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"query\\\":\\\"test\\\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n"

	toolCtx := map[string]*ToolContext{
		"search": {
			McpServerName: "web",
			McpToolName:   "search_web",
		},
	}

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "openai", toolCtx); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())

	// Verify TOOL_USE_START node has MCP fields
	foundToolStart := false
	for _, line := range lines {
		if nodes, ok := line["nodes"].([]interface{}); ok {
			for _, n := range nodes {
				node := n.(map[string]interface{})
				if int(node["type"].(float64)) == augmentNodeTypeToolUseStart { // TOOL_USE_START
					foundToolStart = true
					// After fix, content should be empty string, tool info in tool_use field
					if node["content"] != "" {
						t.Errorf("Expected empty content in TOOL_USE_START, got %v", node["content"])
					}
					tu := node["tool_use"].(map[string]interface{})
					if tu["tool_name"] != "search" {
						t.Errorf("Expected tool_name=search, got %v", tu["tool_name"])
					}
					if tu["tool_use_id"] != "call_1" {
						t.Errorf("Expected tool_use_id=call_1, got %v", tu["tool_use_id"])
					}
					if tu["input_json"] != "" {
						t.Errorf("Expected empty input_json in TOOL_USE_START, got %v", tu["input_json"])
					}
					if tu["mcp_server_name"] != "web" {
						t.Errorf("Expected mcp_server_name=web, got %v", tu["mcp_server_name"])
					}
					if tu["mcp_tool_name"] != "search_web" {
						t.Errorf("Expected mcp_tool_name=search_web, got %v", tu["mcp_tool_name"])
					}
				}
			}
		}
	}
	if !foundToolStart {
		t.Error("Expected TOOL_USE_START node (type=7) not found")
	}

	// Verify TOOL_USE node has MCP fields
	foundTool := false
	for _, line := range lines {
		if nodes, ok := line["nodes"].([]interface{}); ok {
			for _, n := range nodes {
				node := n.(map[string]interface{})
				if int(node["type"].(float64)) == augmentNodeTypeToolUse { // TOOL_USE
					foundTool = true
					toolUse := node["tool_use"].(map[string]interface{})
					if toolUse["mcp_server_name"] != "web" {
						t.Errorf("Expected mcp_server_name=web, got %v", toolUse["mcp_server_name"])
					}
					if toolUse["mcp_tool_name"] != "search_web" {
						t.Errorf("Expected mcp_tool_name=search_web, got %v", toolUse["mcp_tool_name"])
					}
				}
			}
		}
	}
	if !foundTool {
		t.Error("Expected TOOL_USE node (type=5) not found")
	}
}

// TestStreamConvertClaude_ThinkingDelta verifies thinking content is handled
func TestStreamConvertClaude_ThinkingDelta(t *testing.T) {
	sse := "" +
		"data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"thinking\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"Let me think...\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig123\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\" about this.\"}}\n\n" +
		"data: {\"type\":\"content_block_stop\"}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "claude", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())

	// Should have exactly one THINKING node with combined summary and signature.
	foundThinking := false
	thinkingCount := 0
	for _, obj := range lines {
		nodes, _ := obj["nodes"].([]interface{})
		for _, n := range nodes {
			node, _ := n.(map[string]interface{})
			if node["type"] == float64(augmentNodeTypeThinking) {
				thinkingCount++
				if thinking, ok := node["thinking"].(map[string]interface{}); ok {
					summary, _ := thinking["summary"].(string)
					signature, _ := thinking["signature"].(string)
					if summary == "Let me think... about this." && signature == "sig123" {
						foundThinking = true
					}
				}
			}
		}
	}

	if thinkingCount != 1 {
		t.Fatalf("expected exactly 1 thinking node, got %d", thinkingCount)
	}
	if !foundThinking {
		t.Errorf("expected combined thinking content and signature in output")
	}
}

func TestStreamConvertOpenAI_ThinkingDeltaAggregated(t *testing.T) {
	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"Let me think\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"reasoning_content\":\" about this\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Answer\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "openai", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())

	thinkingCount := 0
	foundCombined := false
	for _, obj := range lines {
		nodes, _ := obj["nodes"].([]interface{})
		for _, n := range nodes {
			node, _ := n.(map[string]interface{})
			if node["type"] == float64(augmentNodeTypeThinking) {
				thinkingCount++
				if thinking, ok := node["thinking"].(map[string]interface{}); ok {
					summary, _ := thinking["summary"].(string)
					if summary == "Let me think about this" {
						foundCombined = true
					}
				}
			}
		}
	}

	if thinkingCount != 1 {
		t.Fatalf("expected exactly 1 thinking node, got %d", thinkingCount)
	}
	if !foundCombined {
		t.Fatalf("expected combined reasoning_content in thinking node")
	}
}

// TestStreamConvertOpenAI_UsageBeforeFinish verifies usage is captured even when finish_reason is present
func TestStreamConvertOpenAI_UsageBeforeFinish(t *testing.T) {
	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "openai", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())

	foundUsage := false
	for _, obj := range lines {
		nodes, _ := obj["nodes"].([]interface{})
		for _, n := range nodes {
			node, _ := n.(map[string]interface{})
			if node["type"] == float64(augmentNodeTypeTokenUsage) {
				foundUsage = true
				tu, _ := node["token_usage"].(map[string]interface{})
				if tu["input_tokens"] != float64(10) {
					t.Errorf("expected input_tokens 10, got %#v", tu["input_tokens"])
				}
				if tu["output_tokens"] != float64(5) {
					t.Errorf("expected output_tokens 5, got %#v", tu["output_tokens"])
				}
			}
		}
	}

	if !foundUsage {
		t.Errorf("expected token usage node in output")
	}
}

// TestStreamConvertOpenAI_MultipleToolCalls verifies multiple concurrent tool calls are handled
func TestStreamConvertOpenAI_MultipleToolCalls(t *testing.T) {
	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"tool1\",\"arguments\":\"{\\\"a\\\":1\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"call_2\",\"function\":{\"name\":\"tool2\",\"arguments\":\"{\\\"b\\\":2\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"function\":{\"arguments\":\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "openai", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())

	toolCount := 0
	for _, obj := range lines {
		nodes, _ := obj["nodes"].([]interface{})
		for _, n := range nodes {
			node, _ := n.(map[string]interface{})
			if node["type"] == float64(augmentNodeTypeToolUse) {
				toolCount++
			}
		}
	}

	if toolCount != 2 {
		t.Errorf("expected 2 tool use nodes, got %d", toolCount)
	}
}

// TestStreamConvertOpenAI_ToolUseStartStructure verifies TOOL_USE_START has correct structure
func TestStreamConvertOpenAI_ToolUseStartStructure(t *testing.T) {
	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"{\\\"q\\\":\\\"test\\\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "openai", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())

	foundStart := false
	for _, obj := range lines {
		nodes, _ := obj["nodes"].([]interface{})
		for _, n := range nodes {
			node, _ := n.(map[string]interface{})
			nodeType := node["type"].(float64)

			if nodeType == float64(augmentNodeTypeToolUseStart) {
				foundStart = true
				// Verify structure: content should be empty string, tool info in tool_use field
				if node["content"] != "" {
					t.Errorf("TOOL_USE_START content should be empty string, got %#v", node["content"])
				}
				tu, ok := node["tool_use"].(map[string]interface{})
				if !ok {
					t.Fatalf("TOOL_USE_START should have tool_use field")
				}
				if tu["tool_name"] != "search" {
					t.Errorf("expected tool_name search, got %#v", tu["tool_name"])
				}
				if tu["tool_use_id"] != "call_1" {
					t.Errorf("expected tool_use_id call_1, got %#v", tu["tool_use_id"])
				}
				if tu["input_json"] != "" {
					t.Errorf("expected empty input_json in TOOL_USE_START, got %#v", tu["input_json"])
				}
			}
		}
	}

	if !foundStart {
		t.Errorf("expected TOOL_USE_START node")
	}
}

func TestStreamConvertClaude_EmitSingleFinalTokenUsageNode(t *testing.T) {
	sse := "" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":50}}\n\n" +
		"data: {\"type\":\"message_stop\",\"usage\":{\"input_tokens\":100,\"output_tokens\":50}}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "claude", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())
	usageNodes := collectTokenUsageNodes(lines)
	if len(usageNodes) != 1 {
		t.Fatalf("expected exactly one TOKEN_USAGE node, got %d", len(usageNodes))
	}
	usage := usageNodes[0]
	if usage["input_tokens"] != float64(100) {
		t.Fatalf("expected input_tokens 100, got %#v", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(50) {
		t.Fatalf("expected output_tokens 50, got %#v", usage["output_tokens"])
	}
}

func TestStreamConvertClaude_MessageStartUsageMerged(t *testing.T) {
	sse := "" +
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":120,\"cache_read_input_tokens\":30}}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
		"data: {\"type\":\"message_stop\",\"usage\":{\"output_tokens\":20}}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "claude", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())
	usageNodes := collectTokenUsageNodes(lines)
	if len(usageNodes) != 1 {
		t.Fatalf("expected exactly one TOKEN_USAGE node, got %d", len(usageNodes))
	}
	usage := usageNodes[0]
	if usage["input_tokens"] != float64(120) {
		t.Fatalf("expected input_tokens 120, got %#v", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(20) {
		t.Fatalf("expected output_tokens 20, got %#v", usage["output_tokens"])
	}
	if usage["cache_read_input_tokens"] != float64(0) {
		t.Fatalf("expected cache_read_input_tokens 0, got %#v", usage["cache_read_input_tokens"])
	}
}

func TestStreamConvertOpenAI_ChoicesEmptyUsageCaptured(t *testing.T) {
	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":80,\"completion_tokens\":40,\"total_tokens\":120}}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "openai", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())
	usageNodes := collectTokenUsageNodes(lines)
	if len(usageNodes) != 1 {
		t.Fatalf("expected exactly one TOKEN_USAGE node, got %d", len(usageNodes))
	}
	usage := usageNodes[0]
	if usage["input_tokens"] != float64(80) {
		t.Fatalf("expected input_tokens 80, got %#v", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(40) {
		t.Fatalf("expected output_tokens 40, got %#v", usage["output_tokens"])
	}
}

func TestStreamConvertOpenAI_EmitSingleFinalTokenUsageNode(t *testing.T) {
	sse := "" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":6}}\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "openai", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())
	usageNodes := collectTokenUsageNodes(lines)
	if len(usageNodes) != 1 {
		t.Fatalf("expected exactly one TOKEN_USAGE node, got %d", len(usageNodes))
	}
	usage := usageNodes[0]
	if usage["input_tokens"] != float64(11) {
		t.Fatalf("expected input_tokens 11, got %#v", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(6) {
		t.Fatalf("expected output_tokens 6, got %#v", usage["output_tokens"])
	}
}

func TestStreamConvertOpenAI_OutputUsageFallbackToEstimateWhenTooSmall(t *testing.T) {
	longText := strings.Repeat("This is a long answer segment. ", 90)
	estimatedOutput := tokencount.EstimateOutputTokens(longText)
	if estimatedOutput < usageFallbackMinEstimatedOutputTokens {
		t.Fatalf("test setup invalid: estimated output too small: %d", estimatedOutput)
	}

	contentChunk, err := json.Marshal(map[string]interface{}{
		"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{"content": longText}},
		},
	})
	if err != nil {
		t.Fatalf("marshal content chunk: %v", err)
	}

	finishChunk, err := json.Marshal(map[string]interface{}{
		"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{}, "finish_reason": "stop"},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     50,
			"completion_tokens": 10,
		},
	})
	if err != nil {
		t.Fatalf("marshal finish chunk: %v", err)
	}

	sse := "data: " + string(contentChunk) + "\n\n" +
		"data: " + string(finishChunk) + "\n\n"

	var b strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(sse), &b, "openai", nil); err != nil {
		t.Fatalf("StreamConvertSSEToNDJSON: %v", err)
	}
	lines := readNDJSONLines(t, b.String())
	usageNodes := collectTokenUsageNodes(lines)
	if len(usageNodes) != 1 {
		t.Fatalf("expected exactly one TOKEN_USAGE node, got %d", len(usageNodes))
	}
	usage := usageNodes[0]
	if usage["input_tokens"] != float64(50) {
		t.Fatalf("expected input_tokens 50, got %#v", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(estimatedOutput) {
		t.Fatalf("expected output_tokens fallback to %d, got %#v", estimatedOutput, usage["output_tokens"])
	}
}
