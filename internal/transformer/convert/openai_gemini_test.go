package convert

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAIReqToGeminiIncludesReasoningAsThought(t *testing.T) {
	openaiReq := []byte(`{
		"model":"gemini-2.5-pro",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"hello","reasoning_content":"think first"}
		]
	}`)

	converted, err := OpenAIReqToGemini(openaiReq, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("OpenAIReqToGemini failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatalf("converted request is not valid json: %v", err)
	}

	contents := payload["contents"].([]interface{})
	modelMsg := contents[1].(map[string]interface{})
	parts := modelMsg["parts"].([]interface{})
	first := parts[0].(map[string]interface{})
	if first["text"] != "think first" || first["thought"] != true {
		t.Fatalf("expected reasoning to be encoded as Gemini thought part, got %#v", first)
	}
}

func TestGeminiRespToOpenAIRestoresReasoningContent(t *testing.T) {
	geminiResp := []byte(`{
		"candidates":[
			{
				"content":{
					"role":"model",
					"parts":[
						{"text":"think first","thought":true},
						{"text":"hello"}
					]
				},
				"finishReason":"STOP"
			}
		]
	}`)

	converted, err := GeminiRespToOpenAI(geminiResp, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("GeminiRespToOpenAI failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatalf("converted response is not valid json: %v", err)
	}

	message := payload["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	if message["reasoning_content"] != "think first" {
		t.Fatalf("expected reasoning_content restored, got %#v", message["reasoning_content"])
	}
	if message["content"] != "hello" {
		t.Fatalf("expected normal text preserved, got %#v", message["content"])
	}
}

func TestGeminiStreamToOpenAIEmitsReasoningContent(t *testing.T) {
	event := []byte("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"think first\",\"thought\":true},{\"text\":\"hello\"}]},\"finishReason\":\"STOP\"}]}\n\n")

	converted, err := GeminiStreamToOpenAI(event, nil, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("GeminiStreamToOpenAI failed: %v", err)
	}

	output := string(converted)
	if !strings.Contains(output, `"reasoning_content":"think first"`) {
		t.Fatalf("expected reasoning_content stream chunk, got %s", output)
	}
	if !strings.Contains(output, `"content":"hello"`) {
		t.Fatalf("expected normal content stream chunk, got %s", output)
	}
}

func TestGeminiRespToOpenAICountsThoughtTokensInUsage(t *testing.T) {
	geminiResp := []byte(`{
		"candidates":[
			{
				"content":{"role":"model","parts":[{"text":"hello"}]},
				"finishReason":"STOP"
			}
		],
		"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20,"thoughtsTokenCount":5,"totalTokenCount":35}
	}`)

	converted, err := GeminiRespToOpenAI(geminiResp, "gemini-2.5-pro")
	if err != nil {
		t.Fatalf("GeminiRespToOpenAI failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatalf("converted response is not valid json: %v", err)
	}

	usage := payload["usage"].(map[string]interface{})
	if usage["completion_tokens"] != float64(25) {
		t.Fatalf("expected completion_tokens to include thoughtsTokenCount, got %#v", usage["completion_tokens"])
	}
}
