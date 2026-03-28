package response

import (
	"encoding/json"
	"testing"
)

func TestFixGeminiUpstreamChatBodyPreservesRawToolCallIDs(t *testing.T) {
	rawUpstream := []byte(`{
		"candidates":[
			{"content":{"parts":[
				{"functionCall":{"id":"gem_call_1","name":"read_file","args":{"path":"README.md"}}}
			]}}
		]
	}`)
	transformed := []byte(`{
		"choices":[
			{"message":{
				"role":"assistant",
				"tool_calls":[
					{"id":"call_0","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}}
				]
			}}
		]
	}`)

	fixed, err := FixGeminiUpstreamChatBody(rawUpstream, transformed)
	if err != nil {
		t.Fatalf("FixGeminiUpstreamChatBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(fixed, &payload); err != nil {
		t.Fatalf("fixed body is not valid json: %v", err)
	}
	toolCalls := payload["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})["tool_calls"].([]interface{})
	if toolCalls[0].(map[string]interface{})["id"] != "gem_call_1" {
		t.Fatalf("expected raw gemini tool_call id to be preserved, got %#v", toolCalls[0])
	}
}

func TestFixGeminiUpstreamResponsesBodyPreservesRawCallIDs(t *testing.T) {
	rawUpstream := []byte(`{
		"candidates":[
			{"content":{"parts":[
				{"functionCall":{"id":"gem_call_1","name":"read_file","args":{"path":"README.md"}}}
			]}}
		]
	}`)
	transformed := []byte(`{
		"output":[
			{"type":"function_call","id":"call_0","call_id":"call_0","name":"read_file","arguments":"{\"path\":\"README.md\"}"}
		]
	}`)

	fixed, err := FixGeminiUpstreamResponsesBody(rawUpstream, transformed)
	if err != nil {
		t.Fatalf("FixGeminiUpstreamResponsesBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(fixed, &payload); err != nil {
		t.Fatalf("fixed body is not valid json: %v", err)
	}
	output := payload["output"].([]interface{})
	if output[0].(map[string]interface{})["call_id"] != "gem_call_1" {
		t.Fatalf("expected raw gemini call_id to be preserved, got %#v", output[0])
	}
}
