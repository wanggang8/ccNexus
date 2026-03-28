package response

import (
	"encoding/json"
	"testing"

	cursorcache "github.com/lich0821/ccNexus/internal/cursor/cache"
)

func TestFixChatBodyRepairsLegacyFields(t *testing.T) {
	body := []byte(`{
		"id":"chatcmpl_1",
		"model":"upstream-model",
		"choices":[
			{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"<think>Reason here</think>Hello",
					"reasoningContent":"Reason field",
					"function_call":{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}
				},
				"finish_reason":"function_call"
			}
		]
	}`)

	fixed, err := FixChatBody(body, "cursor-model", []map[string]interface{}{
		{"role": "user", "content": "hello"},
	}, cursorcache.NewThinkingCache())
	if err != nil {
		t.Fatalf("FixChatBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(fixed, &payload); err != nil {
		t.Fatalf("fixed body is not valid json: %v", err)
	}
	if payload["model"] != "cursor-model" {
		t.Fatalf("expected client model rewrite, got %#v", payload["model"])
	}
	choices := payload["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})
	if _, ok := message["reasoning_content"]; !ok {
		t.Fatalf("expected reasoning_content to be present: %#v", message)
	}
	if _, ok := message["function_call"]; ok {
		t.Fatalf("expected legacy function_call to be removed: %#v", message)
	}
	toolCalls, ok := message["tool_calls"].([]interface{})
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected tool_calls to be synthesized: %#v", message["tool_calls"])
	}
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("expected finish_reason=tool_calls, got %#v", choice["finish_reason"])
	}
}
