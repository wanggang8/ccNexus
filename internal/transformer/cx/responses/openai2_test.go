package responses

import (
	"encoding/json"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestOpenAI2Transformer_Name(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")
	if trans.Name() != "cx_resp_openai2" {
		t.Errorf("Expected name 'cx_resp_openai2', got '%s'", trans.Name())
	}
}

func TestOpenAI2Transformer_TransformRequest_Passthrough(t *testing.T) {
	trans := NewOpenAI2Transformer("")

	openai2Req := `{
		"model": "gpt-4o",
		"input": "Hello",
		"max_output_tokens": 1024
	}`

	result, err := trans.TransformRequest([]byte(openai2Req))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	// Should passthrough unchanged when no model override
	if string(result) != openai2Req {
		t.Errorf("Expected passthrough, got different result")
	}
}

func TestOpenAI2Transformer_TransformRequest_ModelOverride(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o-mini")

	openai2Req := `{
		"model": "gpt-4o",
		"input": "Hello",
		"max_output_tokens": 1024
	}`

	result, err := trans.TransformRequest([]byte(openai2Req))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["model"] != "gpt-4o-mini" {
		t.Errorf("Expected model 'gpt-4o-mini', got '%v'", data["model"])
	}
}

func TestOpenAI2Transformer_TransformRequest_PreservesInternalThinkingFields(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o-mini")

	openai2Req := `{
		"model": "gpt-4o",
		"input": "Hello",
		"thinking": {"type": "enabled", "budget_tokens": 2048},
		"enable_thinking": true,
		"budget_tokens": 2048,
		"reasoning_effort": "low"
	}`

	result, err := trans.TransformRequest([]byte(openai2Req))
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if data["model"] != "gpt-4o-mini" {
		t.Fatalf("expected model override to remain applied, got %#v", data["model"])
	}
	thinking, ok := data["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected thinking to be preserved, got %#v", data["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("expected thinking.type to be preserved, got %#v", thinking["type"])
	}
	if data["enable_thinking"] != true {
		t.Fatalf("expected enable_thinking to be preserved, got %#v", data["enable_thinking"])
	}
	if data["budget_tokens"] != float64(2048) {
		t.Fatalf("expected budget_tokens to be preserved, got %#v", data["budget_tokens"])
	}
	if data["reasoning_effort"] != "low" {
		t.Fatalf("expected reasoning_effort to be preserved, got %#v", data["reasoning_effort"])
	}
}

func TestOpenAI2Transformer_TransformResponse(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	openai2Resp := `{"id": "resp_123", "object": "response"}`

	result, err := trans.TransformResponse([]byte(openai2Resp), false)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	// Should passthrough
	if string(result) != openai2Resp {
		t.Errorf("Expected passthrough")
	}
}

func TestOpenAI2Transformer_TransformResponse_Streaming(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

	openai2SSE := `data: {"type":"response.output_text.delta"}`

	result, err := trans.TransformResponse([]byte(openai2SSE), true)
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}

	// Should passthrough
	if string(result) != openai2SSE {
		t.Errorf("Expected passthrough")
	}
}

func TestOpenAI2Transformer_TransformResponseWithContext(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")
	ctx := transformer.NewStreamContext()

	openai2SSE := `data: {"type":"response.output_text.delta","delta":"Hello"}`

	result, err := trans.TransformResponseWithContext([]byte(openai2SSE), true, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	// Should passthrough
	if string(result) != openai2SSE {
		t.Errorf("Expected passthrough")
	}
}

func TestOpenAI2Transformer_TransformResponseWithContext_NonStreaming(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")
	ctx := transformer.NewStreamContext()

	openai2Resp := `{"id": "resp_123", "object": "response"}`

	result, err := trans.TransformResponseWithContext([]byte(openai2Resp), false, ctx)
	if err != nil {
		t.Fatalf("TransformResponseWithContext failed: %v", err)
	}

	// Should passthrough
	if string(result) != openai2Resp {
		t.Errorf("Expected passthrough")
	}
}

func TestOpenAI2Transformer_TransformRequest_ClaudeShapeUsesClaudeBridge(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

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
	if data["input"] == nil {
		t.Fatalf("expected Claude shape to convert to responses input")
	}
	if data["metadata"] == nil {
		t.Fatalf("expected metadata preserved")
	}
}

func TestOpenAI2Transformer_TransformRequest_OpenAIChatShapeUsesChatBridge(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

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

	if data["input"] == nil {
		t.Fatalf("expected chat shape to convert into responses input")
	}
	if _, ok := data["stream_options"]; ok {
		t.Fatalf("expected stream_options dropped for responses target, got %#v", data["stream_options"])
	}
}

func TestOpenAI2Transformer_TransformRequest_RealGpt542LogPreservesResponsesFields(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-5.4")

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

	for _, key := range []string{"input", "include", "metadata", "reasoning", "tools", "user", "store"} {
		if data[key] == nil {
			t.Fatalf("expected %s preserved for responses target, got %#v", key, data[key])
		}
	}
	if _, ok := data["messages"]; ok {
		t.Fatalf("expected responses target to avoid chat downgrade, got %#v", data["messages"])
	}
	if _, ok := data["stream_options"]; ok {
		t.Fatalf("expected stream_options dropped for responses target, got %#v", data["stream_options"])
	}
	if data["prompt_cache_retention"] != raw["prompt_cache_retention"] {
		t.Fatalf("expected prompt_cache_retention preserved, got %#v want %#v", data["prompt_cache_retention"], raw["prompt_cache_retention"])
	}
	if data["store"] != raw["store"] {
		t.Fatalf("expected store preserved, got %#v want %#v", data["store"], raw["store"])
	}
	reasoning, ok := data["reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoning object preserved, got %#v", data["reasoning"])
	}
	if reasoning["summary"] != "auto" || reasoning["effort"] != "medium" {
		t.Fatalf("expected reasoning.summary/effort preserved, got %#v", reasoning)
	}

	rawTools := raw["tools"].([]interface{})
	tools := data["tools"].([]interface{})
	if len(tools) != len(rawTools) {
		t.Fatalf("expected tool count %d preserved, got %d", len(rawTools), len(tools))
	}

	foundCustom := false
	for i, toolRaw := range tools {
		tool := toolRaw.(map[string]interface{})
		source := rawTools[i].(map[string]interface{})
		if tool["type"] != source["type"] {
			t.Fatalf("expected tool %d type preserved, got %#v want %#v", i, tool["type"], source["type"])
		}
		if tool["name"] != source["name"] {
			t.Fatalf("expected tool %d name preserved, got %#v want %#v", i, tool["name"], source["name"])
		}
		if source["type"] == "custom" {
			foundCustom = true
			if _, ok := tool["format"].(map[string]interface{}); !ok {
				t.Fatalf("expected custom tool %d format preserved, got %#v", i, tool)
			}
		}
	}
	if !foundCustom {
		t.Fatal("expected real log to contain custom tool")
	}
}

func TestOpenAI2Transformer_TransformRequest_OpenAIChatToResponses_MapsToolChoice(t *testing.T) {
	trans := NewOpenAI2Transformer("gpt-4o")

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

	if data["tool_choice"] == nil {
		t.Fatalf("expected tool_choice mapped into responses target")
	}
}
