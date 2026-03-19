package convert

import (
	"encoding/json"
	"testing"
)

func TestOpenAI2ReqToGemini_WithImages(t *testing.T) {
	openai2Req := `{
		"model": "gpt-4o",
		"input": [
			{
				"type": "message",
				"role": "user",
				"content": [
					{"type": "input_text", "text": "Look at this"},
					{"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB"}
				]
			}
		]
	}`

	result, err := OpenAI2ReqToGemini([]byte(openai2Req), "gemini-1.5-pro")
	if err != nil {
		t.Fatalf("OpenAI2ReqToGemini failed: %v", err)
	}

	var geminiReq map[string]interface{}
	if err := json.Unmarshal(result, &geminiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	contents := geminiReq["contents"].([]interface{})
	if len(contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(contents))
	}

	parts := contents[0].(map[string]interface{})["parts"].([]interface{})
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].(map[string]interface{})["text"] != "Look at this" {
		t.Fatalf("expected first part text to be preserved, got %#v", parts[0])
	}

	inlineData := parts[1].(map[string]interface{})["inlineData"].(map[string]interface{})
	if inlineData["mimeType"] != "image/png" {
		t.Fatalf("expected mimeType image/png, got %#v", inlineData["mimeType"])
	}
	if inlineData["data"] != "iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB" {
		t.Fatalf("expected image data to be preserved, got %#v", inlineData["data"])
	}
}
