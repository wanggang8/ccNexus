package response

import (
	"encoding/json"
	"testing"
)

func TestFixOpenAIUpstreamChatBodyRepairsLegacyFields(t *testing.T) {
	body := []byte(`{
		"id":"chatcmpl_1",
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

	fixed, err := FixOpenAIUpstreamChatBody(body)
	if err != nil {
		t.Fatalf("FixOpenAIUpstreamChatBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(fixed, &payload); err != nil {
		t.Fatalf("fixed body is not valid json: %v", err)
	}
	choices := payload["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})
	if message["reasoning_content"] != "Reason field" {
		t.Fatalf("expected reasoningContent promoted, got %#v", message["reasoning_content"])
	}
	if _, ok := message["function_call"]; ok {
		t.Fatalf("expected legacy function_call to be removed: %#v", message)
	}
	toolCalls, ok := message["tool_calls"].([]interface{})
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected tool_calls synthesized, got %#v", message["tool_calls"])
	}
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("expected finish_reason rewritten to tool_calls, got %#v", choice["finish_reason"])
	}
}
