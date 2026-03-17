package proxy

import (
	"encoding/json"
	"testing"
)

// ========== parseTokenNumber edge cases ==========

func TestParseTokenNumber_Float64(t *testing.T) {
	result := parseTokenNumber(float64(42))
	if result != 42 {
		t.Errorf("Expected 42, got %d", result)
	}
}

func TestParseTokenNumber_Int(t *testing.T) {
	result := parseTokenNumber(int(100))
	if result != 100 {
		t.Errorf("Expected 100, got %d", result)
	}
}

func TestParseTokenNumber_Int64(t *testing.T) {
	result := parseTokenNumber(int64(200))
	if result != 200 {
		t.Errorf("Expected 200, got %d", result)
	}
}

func TestParseTokenNumber_JSONNumber(t *testing.T) {
	result := parseTokenNumber(json.Number("350"))
	if result != 350 {
		t.Errorf("Expected 350, got %d", result)
	}
}

func TestParseTokenNumber_JSONNumberFloat(t *testing.T) {
	result := parseTokenNumber(json.Number("12.5"))
	if result != 12 {
		t.Errorf("Expected 12 (truncated), got %d", result)
	}
}

func TestParseTokenNumber_String(t *testing.T) {
	result := parseTokenNumber("500")
	if result != 500 {
		t.Errorf("Expected 500, got %d", result)
	}
}

func TestParseTokenNumber_EmptyString(t *testing.T) {
	result := parseTokenNumber("")
	if result != 0 {
		t.Errorf("Expected 0, got %d", result)
	}
}

func TestParseTokenNumber_Nil(t *testing.T) {
	result := parseTokenNumber(nil)
	if result != 0 {
		t.Errorf("Expected 0, got %d", result)
	}
}

func TestParseTokenNumber_Bool(t *testing.T) {
	result := parseTokenNumber(true)
	if result != 0 {
		t.Errorf("Expected 0 for unsupported type, got %d", result)
	}
}

// ========== extractInputOutputTokens tests ==========

func TestExtractInputOutputTokens_ClaudeFormat(t *testing.T) {
	usage := map[string]interface{}{
		"input_tokens":  float64(100),
		"output_tokens": float64(50),
	}
	in, out := extractInputOutputTokens(usage)
	if in != 100 || out != 50 {
		t.Errorf("Expected (100, 50), got (%d, %d)", in, out)
	}
}

func TestExtractInputOutputTokens_OpenAIFormat(t *testing.T) {
	usage := map[string]interface{}{
		"prompt_tokens":     float64(200),
		"completion_tokens": float64(75),
	}
	in, out := extractInputOutputTokens(usage)
	if in != 200 || out != 75 {
		t.Errorf("Expected (200, 75), got (%d, %d)", in, out)
	}
}

func TestExtractInputOutputTokens_MixedFormat(t *testing.T) {
	// input_tokens takes precedence over prompt_tokens
	usage := map[string]interface{}{
		"input_tokens":      float64(100),
		"prompt_tokens":     float64(200),
		"output_tokens":     float64(50),
		"completion_tokens": float64(75),
	}
	in, out := extractInputOutputTokens(usage)
	if in != 100 || out != 50 {
		t.Errorf("Expected (100, 50) (input_tokens takes precedence), got (%d, %d)", in, out)
	}
}

func TestExtractInputOutputTokens_Empty(t *testing.T) {
	usage := map[string]interface{}{}
	in, out := extractInputOutputTokens(usage)
	if in != 0 || out != 0 {
		t.Errorf("Expected (0, 0), got (%d, %d)", in, out)
	}
}

// ========== extractTokenUsage tests ==========

func TestExtractTokenUsage_ValidJSON(t *testing.T) {
	resp := `{"usage":{"input_tokens":10,"output_tokens":5}}`
	in, out := extractTokenUsage([]byte(resp))
	if in != 10 || out != 5 {
		t.Errorf("Expected (10, 5), got (%d, %d)", in, out)
	}
}

func TestExtractTokenUsage_InvalidJSON(t *testing.T) {
	in, out := extractTokenUsage([]byte("not json"))
	if in != 0 || out != 0 {
		t.Errorf("Expected (0, 0), got (%d, %d)", in, out)
	}
}

func TestExtractTokenUsage_NoUsage(t *testing.T) {
	resp := `{"id":"123","content":"hello"}`
	in, out := extractTokenUsage([]byte(resp))
	if in != 0 || out != 0 {
		t.Errorf("Expected (0, 0), got (%d, %d)", in, out)
	}
}

// ========== extractResponseOutputText tests ==========

func TestExtractResponseOutputText_OpenAIFormat(t *testing.T) {
	resp := `{"choices":[{"message":{"content":"Hello world"}}]}`
	text := extractResponseOutputText([]byte(resp))
	if text != "Hello world" {
		t.Errorf("Expected 'Hello world', got %q", text)
	}
}

func TestExtractResponseOutputText_ClaudeFormat(t *testing.T) {
	resp := `{"content":[{"type":"text","text":"Claude says hi"}]}`
	text := extractResponseOutputText([]byte(resp))
	if text != "Claude says hi" {
		t.Errorf("Expected 'Claude says hi', got %q", text)
	}
}

func TestExtractResponseOutputText_OpenAI2Format(t *testing.T) {
	resp := `{"output":[{"type":"message","content":[{"type":"output_text","text":"Response text"}]}]}`
	text := extractResponseOutputText([]byte(resp))
	if text != "Response text" {
		t.Errorf("Expected 'Response text', got %q", text)
	}
}

func TestExtractResponseOutputText_Empty(t *testing.T) {
	text := extractResponseOutputText([]byte("{}"))
	if text != "" {
		t.Errorf("Expected empty string, got %q", text)
	}
}

func TestExtractResponseOutputText_InvalidJSON(t *testing.T) {
	text := extractResponseOutputText([]byte("invalid"))
	if text != "" {
		t.Errorf("Expected empty string, got %q", text)
	}
}
