package response

import (
	"encoding/json"
	"testing"
)

func TestFixMessagesBodyInjectsThinkingBlock(t *testing.T) {
	body := []byte(`{
		"id":"msg_1",
		"type":"message",
		"content":[{"type":"text","text":"answer"}],
		"reasoning_content":"think first"
	}`)

	updated, err := FixMessagesBody(body)
	if err != nil {
		t.Fatalf("FixMessagesBody failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("updated body is not valid json: %v", err)
	}
	if _, ok := payload["reasoning_content"]; ok {
		t.Fatalf("expected reasoning_content field consumed into thinking block, got %#v", payload["reasoning_content"])
	}
	content := payload["content"].([]interface{})
	first := content[0].(map[string]interface{})
	if first["type"] != "thinking" || first["thinking"] != "think first" {
		t.Fatalf("expected thinking block prepended, got %#v", first)
	}
}
