package augment

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFilterToolResultsWhenAllowedIDsFromHistory(t *testing.T) {
	tr, err := New("claude", "")
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]interface{}{
		"model":   "claude-sonnet-4-20250514",
		"message": "next",
		"chat_history": []interface{}{
			map[string]interface{}{
				"request_message": "run",
				"response_text":   "",
				"response_nodes": []interface{}{
					map[string]interface{}{
						"type": 5,
						"tool_use": map[string]interface{}{
							"tool_use_id": "call_ok",
							"tool_name":   "search",
							"input_json":  "{}",
						},
					},
				},
			},
			map[string]interface{}{
				"request_nodes": []interface{}{
					map[string]interface{}{
						"type": 1,
						"tool_result_node": map[string]interface{}{
							"tool_use_id": "call_ok",
							"content":     "found",
						},
					},
					map[string]interface{}{
						"type": 1,
						"tool_result_node": map[string]interface{}{
							"tool_use_id": "phantom_id",
							"content":     "should degrade",
						},
					},
				},
				"response_text": "ok",
			},
		},
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(req["messages"])
	if !strings.Contains(string(raw), "Historical tool result.") || !strings.Contains(string(raw), "should degrade") {
		t.Fatalf("phantom tool_result should degrade to text instead of disappearing, got messages: %s", raw)
	}
	if !strings.Contains(string(raw), "found") {
		t.Fatalf("expected valid tool result preserved: %s", raw)
	}
}

func TestBadJSONToolSchemaDropsTool(t *testing.T) {
	tr, err := New("claude", "")
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]interface{}{
		"model":   "claude-sonnet-4-20250514",
		"message": "hi",
		"tool_definitions": []interface{}{
			map[string]interface{}{
				"name":              "good",
				"input_schema_json": `{"type":"object","properties":{"x":{"type":"string"}}}`,
				"description":       "ok",
			},
			map[string]interface{}{
				"name":              "bad",
				"input_schema_json": `{not valid json`,
				"description":       "skip",
			},
		},
	}
	body, _ := json.Marshal(input)
	out, err := tr.TransformRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]interface{}
	json.Unmarshal(out, &req)
	tools, _ := req["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("expected exactly one tool (good), got %d", len(tools))
	}
	t0 := tools[0].(map[string]interface{})
	if t0["name"] != "good" {
		t.Fatalf("expected tool good, got %#v", t0["name"])
	}
}

func TestRepairOpenAIToolCallMessages_DegradesOrphanToolCallAndResultToUserText(t *testing.T) {
	messages := []map[string]interface{}{
		{
			"role": "assistant",
			"tool_calls": []interface{}{
				map[string]interface{}{
					"id":   "call_orphan_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "search",
						"arguments": `{"q":"augment"}`,
					},
				},
			},
		},
		{"role": "assistant", "content": "done"},
		{"role": "tool", "tool_call_id": "call_ghost", "content": "ghost output"},
	}
	got := repairOpenAIToolCallMessages(messages)
	if len(got) != 4 {
		t.Fatalf("expected 4 messages after repair, got %#v", got)
	}
	if got[1]["role"] != "tool" || !strings.Contains(toString(got[1]["content"]), "tool_result_missing") {
		t.Fatalf("expected orphan tool call repaired with synthetic tool result, got %#v", got[1])
	}
	if got[3]["role"] != "user" || !strings.Contains(toString(got[3]["content"]), "Historical tool result.") {
		t.Fatalf("expected orphan tool result degraded to user text, got %#v", got[3])
	}
}

func TestRepairOpenAI2Input_DegradesOrphanFunctionCallAndOutputToUserMessages(t *testing.T) {
	input := []map[string]interface{}{
		{"type": "function_call", "call_id": "call_orphan_1", "name": "search", "arguments": `{"q":"augment"}`},
		{"type": "message", "role": "assistant", "content": "done"},
		{"type": "function_call_output", "call_id": "call_ghost", "output": "ghost output"},
	}
	got := repairOpenAI2Input(input)
	if len(got) != 4 {
		t.Fatalf("expected 4 input items after repair, got %#v", got)
	}
	if got[1]["type"] != "function_call_output" || !strings.Contains(toString(got[1]["output"]), "tool_result_missing") {
		t.Fatalf("expected orphan function call repaired with synthetic output, got %#v", got[1])
	}
	if got[3]["type"] != "message" || !strings.Contains(toString(got[3]["content"]), "Historical tool result.") {
		t.Fatalf("expected orphan function_call_output degraded to message, got %#v", got[3])
	}
}
