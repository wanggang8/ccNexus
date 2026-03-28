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

func TestOpenAIReqToGeminiMatchesApi2CursorRequestShape(t *testing.T) {
	topP := 0.8
	openaiReq := []byte(`{
		"model":"gpt-4.1",
		"top_p":0.8,
		"stop":["DONE","END"],
		"tools":[
			{"type":"function","function":{"name":"read_file","description":"Read file","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}
		],
		"messages":[
			{"role":"system","content":"sys"},
			{"role":"developer","content":"dev"},
			{"role":"user","content":"hello"},
			{"role":"user","content":"again"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"{\"ok\":true}"}
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

	systemInstruction := payload["systemInstruction"].(map[string]interface{})
	systemParts := systemInstruction["parts"].([]interface{})
	if systemParts[0].(map[string]interface{})["text"] != "sys\n\ndev" {
		t.Fatalf("expected system and developer prompts merged, got %#v", systemParts[0])
	}

	genConfig := payload["generationConfig"].(map[string]interface{})
	if genConfig["topP"] != topP {
		t.Fatalf("expected topP=%v, got %#v", topP, genConfig["topP"])
	}
	stopSequences := genConfig["stopSequences"].([]interface{})
	if len(stopSequences) != 2 || stopSequences[0] != "DONE" || stopSequences[1] != "END" {
		t.Fatalf("expected stopSequences preserved, got %#v", stopSequences)
	}
	if _, ok := payload["toolConfig"]; ok {
		t.Fatalf("did not expect toolConfig helper field, got %#v", payload["toolConfig"])
	}

	contents := payload["contents"].([]interface{})
	if len(contents) != 3 {
		t.Fatalf("expected merged Gemini contents, got %#v", contents)
	}
	first := contents[0].(map[string]interface{})
	if first["role"] != "user" {
		t.Fatalf("expected first merged content to stay user role, got %#v", first["role"])
	}
	firstParts := first["parts"].([]interface{})
	if len(firstParts) != 2 {
		t.Fatalf("expected adjacent user messages merged into one content, got %#v", firstParts)
	}

	toolResponse := contents[2].(map[string]interface{})["parts"].([]interface{})[0].(map[string]interface{})["functionResponse"].(map[string]interface{})
	responseValue := toolResponse["response"].(map[string]interface{})
	if responseValue["ok"] != true {
		t.Fatalf("expected tool response to stay raw parsed object, got %#v", responseValue)
	}
}
