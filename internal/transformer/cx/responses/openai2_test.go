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

func TestOpenAI2Transformer_TransformRequest_StripsInternalThinkingFields(t *testing.T) {
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
	if _, ok := data["thinking"]; ok {
		t.Fatalf("expected thinking to be stripped, got %#v", data["thinking"])
	}
	if _, ok := data["enable_thinking"]; ok {
		t.Fatalf("expected enable_thinking to be stripped, got %#v", data["enable_thinking"])
	}
	if _, ok := data["budget_tokens"]; ok {
		t.Fatalf("expected budget_tokens to be stripped, got %#v", data["budget_tokens"])
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
