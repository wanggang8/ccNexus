package response

import (
	"encoding/json"
	"testing"

	cursorcache "github.com/lich0821/ccNexus/internal/cursor/cache"
)

func TestFixResponsesBodyStoresThinkingCacheFromResponsesOutput(t *testing.T) {
	cacheStore := cursorcache.NewThinkingCache()
	cacheMessages := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
		{"role": "user", "content": "continue"},
	}
	body := []byte(`{
		"id":"resp_2",
		"object":"response",
		"model":"upstream-model",
		"output":[
			{"type":"reasoning","summary":[{"type":"summary_text","text":"stored think"}]},
			{"type":"message","content":[{"type":"output_text","text":"ok"}]}
		]
	}`)

	fixed, err := FixResponsesBody(body, "cursor-model", cacheMessages, "cx_resp_openai", cacheStore)
	if err != nil {
		t.Fatalf("FixResponsesBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(fixed, &payload); err != nil {
		t.Fatalf("fixed body is not valid json: %v", err)
	}
	if payload["model"] != "cursor-model" {
		t.Fatalf("expected client model rewrite, got %#v", payload["model"])
	}

	injected := cacheStore.Inject([]map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
		{"role": "assistant", "content": ""},
		{"role": "user", "content": "continue"},
	})
	if injected[2]["reasoning_content"] != "stored think" {
		t.Fatalf("expected responses route to write thinking cache from output, got %#v", injected[2]["reasoning_content"])
	}
}

func TestFixResponsesBodyNormalizesClaudeFunctionCallArguments(t *testing.T) {
	body := []byte(`{
		"id":"resp_2",
		"object":"response",
		"output":[
			{
				"type":"function_call",
				"id":"fc_1",
				"call_id":"call_1",
				"name":"str_replace",
				"arguments":"{\"file_path\":\"README.md\",\"old_string\":\"old\",\"new_string\":\"new\"}"
			}
		]
	}`)

	fixed, err := FixResponsesBody(body, "cursor-model", nil, "cx_resp_claude", cursorcache.NewThinkingCache())
	if err != nil {
		t.Fatalf("FixResponsesBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(fixed, &payload); err != nil {
		t.Fatalf("fixed body is not valid json: %v", err)
	}
	output := payload["output"].([]interface{})
	item := output[0].(map[string]interface{})

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(item["arguments"].(string)), &args); err != nil {
		t.Fatalf("arguments are not valid json: %v", err)
	}
	if args["path"] != "README.md" {
		t.Fatalf("expected file_path to be normalized to path, got %#v", args)
	}
	if _, ok := args["file_path"]; ok {
		t.Fatalf("did not expect file_path to remain after normalization, got %#v", args)
	}
}
