package responses

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func readPrefixedLogJSONLine(t *testing.T, path string, lineIndex int, prefix string) []byte {
	t.Helper()

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log failed: %v", err)
	}

	lines := strings.Split(string(payload), "\n")
	if lineIndex < 0 || lineIndex >= len(lines) {
		t.Fatalf("expected log %s to contain line %d, got %d lines", path, lineIndex+1, len(lines))
	}

	line := strings.TrimSpace(lines[lineIndex])
	if line == "" {
		t.Fatalf("expected log %s line %d to contain payload", path, lineIndex+1)
	}
	if prefix != "" {
		if !strings.HasPrefix(line, prefix) {
			t.Fatalf("expected log %s line %d to start with %q, got %q", path, lineIndex+1, prefix, line)
		}
		line = strings.TrimPrefix(line, prefix)
	}

	return []byte(line)
}

func TestOpenAITransformer_Name(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	if trans.Name() != "cx_resp_openai" {
		t.Errorf("Expected name 'cx_resp_openai', got '%s'", trans.Name())
	}
}

func TestOpenAITransformer_TransformRequest(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	// OpenAI Responses API format
	openai2Req := `{
		"model": "gpt-4o",
		"instructions": "You are helpful.",
		"input": "Hello",
		"max_output_tokens": 1024
	}`

	result, err := trans.TransformRequest([]byte(openai2Req))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var openaiReq map[string]interface{}
	if err := json.Unmarshal(result, &openaiReq); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check model is overridden
	if openaiReq["model"] != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%v'", openaiReq["model"])
	}

	// Check OpenAI Chat format
	if openaiReq["messages"] == nil {
		t.Errorf("Expected messages field, got nil")
	}
}

func TestOpenAITransformer_TransformResponse(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	openaiResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hello!"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`

	result, err := trans.TransformResponse([]byte(openaiResp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check OpenAI2 (Responses API) format
	if openai2Resp["object"] != "response" {
		t.Errorf("Expected object 'response', got '%v'", openai2Resp["object"])
	}
}

func TestOpenAITransformer_TransformResponse_Streaming(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")

	result, err := trans.TransformResponse([]byte("data: {}"), true)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for streaming, got %v", result)
	}
}

func TestOpenAITransformer_TransformResponseWithContext(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()

	openaiSSE := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`

	result, err := trans.TransformResponseWithContext([]byte(openaiSSE), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	resultStr := string(result)
	// Should convert to OpenAI2 SSE format
	if result != nil && !strings.Contains(resultStr, "data:") {
		t.Errorf("Expected SSE format with 'data:', got '%s'", resultStr)
	}
}

func TestOpenAITransformer_TransformResponseWithContext_NonStreaming(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4")
	ctx := transformer.NewStreamContext()

	openaiResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hello!"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`

	result, err := trans.TransformResponseWithContext([]byte(openaiResp), false, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	var openai2Resp map[string]interface{}
	if err := json.Unmarshal(result, &openai2Resp); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if openai2Resp["object"] != "response" {
		t.Errorf("Expected object 'response', got '%v'", openai2Resp["object"])
	}
}

func TestOpenAITransformer_TransformRequest_ClaudeShapeUsesClaudeConverter(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	claudeReq := `{
		"model": "claude-4-sonnet",
		"system": [{"type": "text", "text": "You are helpful."}],
		"messages": [{"role": "user", "content": [{"type": "text", "text": "Hello"}]}],
		"metadata": {"source": "cursor"}
	}`

	result, err := trans.TransformRequest([]byte(claudeReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["model"] != "gpt-4o" {
		t.Fatalf("expected model override, got %#v", data["model"])
	}
	if _, ok := data["messages"].([]interface{}); !ok {
		t.Fatalf("expected converted OpenAI chat messages, got %#v", data["messages"])
	}
	if _, ok := data["metadata"].(map[string]interface{}); !ok {
		t.Fatalf("expected metadata preserved, got %#v", data["metadata"])
	}
}

func TestOpenAITransformer_TransformRequest_OpenAIChatShapeUsesChatBridge(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	chatReq := `{
		"model": "gpt-4.1",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello"}
		],
		"stream_options": {"include_usage": true}
	}`

	result, err := trans.TransformRequest([]byte(chatReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["messages"] == nil {
		t.Fatalf("expected OpenAI chat bridge to responses->openai target output")
	}
	if data["stream_options"] == nil {
		t.Fatalf("expected stream_options preserved, got nil")
	}
}

func TestOpenAITransformer_TransformRequest_OpenAIChatToOpenAI_PreservesToolChoice(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	chatReq := `{
		"model": "gpt-4.1",
		"messages": [{"role": "user", "content": "Hello"}],
		"tools": [{"type": "function", "function": {"name": "read_file", "parameters": {"type": "object"}}}],
		"tool_choice": "required"
	}`

	result, err := trans.TransformRequest([]byte(chatReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["tool_choice"] != "required" {
		t.Fatalf("expected tool_choice preserved for openai target, got %#v", data["tool_choice"])
	}
}

func TestOpenAITransformer_TransformRequest_OpenAIChatToOpenAI_NormalizesLegacyFunctionsAndFunctionCall(t *testing.T) {
	trans := NewOpenAITransformer("gpt-4o")

	chatReq := `{
		"model": "gpt-4.1",
		"messages": [{"role": "user", "content": "Hello"}],
		"functions": [{"name": "legacy_func", "description": "Legacy", "parameters": {"type": "object"}}],
		"function_call": {"name": "legacy_func"}
	}`

	result, err := trans.TransformRequest([]byte(chatReq))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if _, ok := data["functions"]; ok {
		t.Fatalf("expected legacy functions removed, got %#v", data["functions"])
	}
	if _, ok := data["function_call"]; ok {
		t.Fatalf("expected legacy function_call removed, got %#v", data["function_call"])
	}
	if _, ok := data["tools"].([]interface{}); !ok {
		t.Fatalf("expected tools after normalization, got %#v", data["tools"])
	}
	toolChoice, ok := data["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected normalized tool_choice object, got %#v", data["tool_choice"])
	}
	fn, _ := toolChoice["function"].(map[string]interface{})
	if toolChoice["type"] != "function" || fn["name"] != "legacy_func" {
		t.Fatalf("unexpected normalized tool_choice: %#v", toolChoice)
	}
}

func TestOpenAITransformer_TransformRequest_RealGpt54LogShowsChatDowngradeLoss(t *testing.T) {
	trans := NewOpenAITransformer("gpt-5.4")

	rawReq := readPrefixedLogJSONLine(t, "/Users/vick/Desktop/project/ccNexus/docs/gpt5.4-2.log", 0, "原始：")
	result, err := trans.TransformRequest(rawReq)
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(rawReq, &raw); err != nil {
		t.Fatalf("unmarshal raw payload failed: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("unmarshal transformed payload failed: %v", err)
	}

	if _, ok := data["messages"].([]interface{}); !ok {
		t.Fatalf("expected OpenAI target to produce chat messages, got %#v", data["messages"])
	}
	for _, key := range []string{"include", "store", "reasoning", "input"} {
		if _, ok := data[key]; ok {
			t.Fatalf("expected %s to be dropped after responses->chat downgrade, got %#v", key, data[key])
		}
	}
	if data["reasoning_effort"] != "medium" {
		t.Fatalf("expected reasoning.effort downgraded to reasoning_effort=medium, got %#v", data["reasoning_effort"])
	}
	if _, ok := data["stream_options"].(map[string]interface{}); !ok {
		t.Fatalf("expected stream_options preserved, got %#v", data["stream_options"])
	}
	if _, ok := data["metadata"].(map[string]interface{}); !ok {
		t.Fatalf("expected metadata preserved, got %#v", data["metadata"])
	}
	if data["prompt_cache_retention"] != raw["prompt_cache_retention"] {
		t.Fatalf("expected prompt_cache_retention preserved, got %#v want %#v", data["prompt_cache_retention"], raw["prompt_cache_retention"])
	}

	rawTools, ok := raw["tools"].([]interface{})
	if !ok || len(rawTools) == 0 {
		t.Fatalf("expected raw tools in log, got %#v", raw["tools"])
	}
	transformedTools, ok := data["tools"].([]interface{})
	if !ok || len(transformedTools) != len(rawTools) {
		t.Fatalf("expected transformed tool count to match raw count %d, got %#v", len(rawTools), data["tools"])
	}

	foundCustomRaw := false
	foundApplyPatch := false
	for _, rawTool := range rawTools {
		tool, _ := rawTool.(map[string]interface{})
		if tool["type"] == "custom" {
			foundCustomRaw = true
			if _, ok := tool["format"].(map[string]interface{}); !ok {
				t.Fatalf("expected raw custom tool to carry format, got %#v", tool)
			}
		}
	}
	if !foundCustomRaw {
		t.Fatal("expected raw log to contain at least one custom tool")
	}

	for i, transformedTool := range transformedTools {
		tool, _ := transformedTool.(map[string]interface{})
		if tool["type"] != "function" {
			t.Fatalf("expected downgraded tool %d to be function, got %#v", i, tool)
		}
		fn, ok := tool["function"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected downgraded tool %d to wrap function payload, got %#v", i, tool)
		}
		if fn["name"] == "ApplyPatch" {
			foundApplyPatch = true
			params, ok := fn["parameters"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected ApplyPatch parameters after downgrade, got %#v", fn["parameters"])
			}
			props, ok := params["properties"].(map[string]interface{})
			if !ok || props["input"] == nil {
				t.Fatalf("expected ApplyPatch to keep generic input schema, got %#v", params)
			}
			input, _ := props["input"].(map[string]interface{})
			desc, _ := input["description"].(string)
			if !strings.Contains(desc, "Custom tool format constraints:") || !strings.Contains(desc, "type=grammar") {
				t.Fatalf("expected ApplyPatch custom format hint preserved in description, got %q", desc)
			}
		}
	}
	if !foundApplyPatch {
		t.Fatal("expected transformed tools to contain ApplyPatch")
	}
}
