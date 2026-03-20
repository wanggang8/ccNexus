package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
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

func TestGeminiRespToOpenAI2_PreservesPartOrder(t *testing.T) {
	geminiResp := `{
		"candidates": [{
			"content": {
				"role": "model",
				"parts": [
					{"functionCall": {"name": "read_file", "args": {"path": "/tmp/a"}}},
					{"text": "done"}
				]
			},
			"finishReason": "STOP",
			"index": 0
		}],
		"usageMetadata": {"promptTokenCount": 1, "candidatesTokenCount": 2, "totalTokenCount": 3}
	}`

	result, err := GeminiRespToOpenAI2([]byte(geminiResp))
	if err != nil {
		t.Fatalf("GeminiRespToOpenAI2 failed: %v", err)
	}

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	output := openai2Resp["output"].([]interface{})
	if output[0].(map[string]interface{})["type"] != "function_call" {
		t.Fatalf("expected first output item to be function_call, got %#v", output[0])
	}
}

func TestOpenAI2StreamToGemini_UsesDoneArguments(t *testing.T) {
	ctx := transformer.NewStreamContext()

	_, _ = OpenAI2StreamToGemini([]byte(`data: {"type":"response.output_item.added","output_index":4,"item":{"type":"function_call","call_id":"call_done","name":"read_file"}}`), ctx)
	_, _ = OpenAI2StreamToGemini([]byte(`data: {"type":"response.function_call_arguments.delta","output_index":4,"delta":"{\"path\":"}`), ctx)
	_, _ = OpenAI2StreamToGemini([]byte(`data: {"type":"response.function_call_arguments.done","output_index":4,"arguments":"{\"path\":\"/tmp/a\"}"}`), ctx)
	out, err := OpenAI2StreamToGemini([]byte(`data: {"type":"response.output_item.done","output_index":4,"item":{"type":"function_call","call_id":"call_done","name":"read_file"}}`), ctx)
	if err != nil {
		t.Fatalf("OpenAI2StreamToGemini failed: %v", err)
	}
	if out == nil {
		t.Fatal("expected output chunk")
	}
	if !strings.Contains(string(out), `"path":"/tmp/a"`) {
		t.Fatalf("expected final arguments from done event, got: %s", string(out))
	}
}

func TestGeminiStreamToOpenAI2_UsesStableOutputIndexes(t *testing.T) {
	ctx := transformer.NewStreamContext()

	chunk := `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"done"},{"functionCall":{"name":"read_file","args":{"path":"/tmp/a"}}}]},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2,"totalTokenCount":9}}`

	out, err := GeminiStreamToOpenAI2([]byte(chunk), ctx)
	if err != nil {
		t.Fatalf("GeminiStreamToOpenAI2 failed: %v", err)
	}
	outStr := string(out)
	if !strings.Contains(outStr, `"output_index":0`) {
		t.Fatalf("expected message item to use output_index 0, got: %s", outStr)
	}
	if !strings.Contains(outStr, `"output_index":1`) {
		t.Fatalf("expected tool item to use output_index 1, got: %s", outStr)
	}
	if !strings.Contains(outStr, `"total_tokens":9`) {
		t.Fatalf("expected authoritative total_tokens to pass through, got: %s", outStr)
	}
}
