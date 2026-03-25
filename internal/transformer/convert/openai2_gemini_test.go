package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestOpenAI2ReqToGeminiInjectsReasoningAsThoughtPart(t *testing.T) {
	openai2Req := []byte(`{
		"model":"gpt-4.1",
		"input":[
			{"type":"reasoning","summary":[{"type":"summary_text","text":"think first"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}
		]
	}`)

	converted, err := OpenAI2ReqToGemini(openai2Req, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("OpenAI2ReqToGemini failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatalf("converted request is not valid json: %v", err)
	}

	contents := payload["contents"].([]interface{})
	parts := contents[0].(map[string]interface{})["parts"].([]interface{})
	first := parts[0].(map[string]interface{})
	if first["text"] != "think first" || first["thought"] != true {
		t.Fatalf("expected leading Gemini thought part, got %#v", first)
	}
}

func TestGeminiRespToOpenAI2CountsThoughtTokensInUsage(t *testing.T) {
	geminiResp := []byte(`{
		"candidates":[
			{
				"content":{"role":"model","parts":[{"text":"hello"}]},
				"finishReason":"STOP"
			}
		],
		"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20,"thoughtsTokenCount":5,"totalTokenCount":35}
	}`)

	converted, err := GeminiRespToOpenAI2(geminiResp)
	if err != nil {
		t.Fatalf("GeminiRespToOpenAI2 failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatalf("converted response is not valid json: %v", err)
	}

	usage := payload["usage"].(map[string]interface{})
	if usage["output_tokens"] != float64(25) {
		t.Fatalf("expected output_tokens to include thoughtsTokenCount, got %#v", usage["output_tokens"])
	}
}

func TestGeminiRespToOpenAI2IncludesReasoningOutputItem(t *testing.T) {
	geminiResp := []byte(`{
		"candidates":[
			{
				"content":{"role":"model","parts":[{"text":"think first","thought":true},{"text":"answer"}]},
				"finishReason":"STOP"
			}
		]
	}`)

	converted, err := GeminiRespToOpenAI2(geminiResp)
	if err != nil {
		t.Fatalf("GeminiRespToOpenAI2 failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatalf("converted response is not valid json: %v", err)
	}

	output := payload["output"].([]interface{})
	if len(output) != 2 || output[0].(map[string]interface{})["type"] != "reasoning" {
		t.Fatalf("expected reasoning output item first, got %#v", output)
	}
	if output[0].(map[string]interface{})["id"] == "" {
		t.Fatalf("expected reasoning id, got %#v", output[0])
	}
	if output[1].(map[string]interface{})["status"] != "completed" {
		t.Fatalf("expected completed message item, got %#v", output[1])
	}
}

func TestGeminiRespToOpenAI2MapsMaxTokensToIncomplete(t *testing.T) {
	geminiResp := []byte(`{
		"candidates":[
			{
				"content":{"role":"model","parts":[{"text":"answer"}]},
				"finishReason":"MAX_TOKENS"
			}
		]
	}`)

	converted, err := GeminiRespToOpenAI2(geminiResp)
	if err != nil {
		t.Fatalf("GeminiRespToOpenAI2 failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatalf("converted response is not valid json: %v", err)
	}

	if payload["status"] != "incomplete" {
		t.Fatalf("expected incomplete status, got %#v", payload["status"])
	}
}

func TestGeminiStreamToOpenAI2EmitsReasoningEvents(t *testing.T) {
	ctx := transformer.NewStreamContext()

	event := []byte(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"think first","thought":true}]}}]}`)
	out, err := GeminiStreamToOpenAI2(event, ctx)
	if err != nil {
		t.Fatalf("GeminiStreamToOpenAI2 failed: %v", err)
	}
	if out == nil {
		t.Fatal("expected transformed output, got nil")
	}

	output := string(out)
	if !strings.Contains(output, `"type":"response.reasoning_summary_text.delta"`) {
		t.Fatalf("expected reasoning_summary_text delta event, got %s", output)
	}
	if !strings.Contains(output, `"delta":"think first"`) {
		t.Fatalf("expected reasoning delta payload, got %s", output)
	}
}

func TestOpenAI2ReqToGeminiSupportsEasyInputMessage(t *testing.T) {
	openai2Req := []byte(`{
		"model":"gpt-4.1",
		"input":[
			{"role":"assistant","content":"answer"}
		]
	}`)

	converted, err := OpenAI2ReqToGemini(openai2Req, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("OpenAI2ReqToGemini failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatalf("converted request is not valid json: %v", err)
	}

	contents := payload["contents"].([]interface{})
	first := contents[0].(map[string]interface{})
	if first["role"] != "model" {
		t.Fatalf("expected assistant easy input to map to model role, got %#v", first["role"])
	}
}

func TestOpenAI2StreamToGeminiEmitsThoughtPartFromReasoningSummary(t *testing.T) {
	ctx := transformer.NewStreamContext()

	event := []byte(`data: {"type":"response.reasoning_summary_text.delta","delta":"think first"}`)
	out, err := OpenAI2StreamToGemini(event, ctx)
	if err != nil {
		t.Fatalf("OpenAI2StreamToGemini failed: %v", err)
	}
	if out == nil {
		t.Fatal("expected transformed output, got nil")
	}

	output := string(out)
	if !strings.Contains(output, `"thought":true`) || !strings.Contains(output, `"text":"think first"`) {
		t.Fatalf("expected reasoning summary to become Gemini thought part, got %s", output)
	}
}
