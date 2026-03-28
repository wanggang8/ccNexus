package proxy

import (
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
)

func TestCursorResponsesRequestPreservesInstructionsAndTokenLimitsAcrossBackends(t *testing.T) {
	requestBody := `{
		"model":"gpt-5",
		"instructions":"follow the rules",
		"input":"hello",
		"max_output_tokens":256,
		"stream":false
	}`

	tests := []struct {
		name     string
		endpoint config.Endpoint
		validate func(*testing.T, preparedCursorRoundTrip)
	}{
		{
			name:     "openai",
			endpoint: config.Endpoint{Name: "openai", Transformer: "openai", Model: "gpt-5"},
			validate: func(t *testing.T, prepared preparedCursorRoundTrip) {
				t.Helper()
				messages := prepared.requestPayload["messages"].([]interface{})
				first := messages[0].(map[string]interface{})
				if first["role"] != "system" || first["content"] != "follow the rules" {
					t.Fatalf("expected instructions promoted to system message, got %#v", first)
				}
				if prepared.requestPayload["max_tokens"] != float64(256) {
					t.Fatalf("expected max_output_tokens mapped to max_tokens=256, got %#v", prepared.requestPayload["max_tokens"])
				}
			},
		},
		{
			name:     "claude",
			endpoint: config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			validate: func(t *testing.T, prepared preparedCursorRoundTrip) {
				t.Helper()
				systemText := flattenTextForTest(prepared.requestPayload["system"])
				if !strings.Contains(systemText, "follow the rules") {
					t.Fatalf("expected instructions promoted to claude system, got %#v", prepared.requestPayload["system"])
				}
				if prepared.requestPayload["max_tokens"] != float64(8192) {
					t.Fatalf("expected claude max_tokens floor after mapping, got %#v", prepared.requestPayload["max_tokens"])
				}
			},
		},
		{
			name:     "gemini",
			endpoint: config.Endpoint{Name: "gemini", Transformer: "gemini", Model: "gemini-2.5-pro"},
			validate: func(t *testing.T, prepared preparedCursorRoundTrip) {
				t.Helper()
				systemInstruction := prepared.requestPayload["systemInstruction"].(map[string]interface{})
				if !strings.Contains(flattenTextForTest(systemInstruction["parts"]), "follow the rules") {
					t.Fatalf("expected instructions promoted to gemini systemInstruction, got %#v", prepared.requestPayload["systemInstruction"])
				}
				generationConfig := prepared.requestPayload["generationConfig"].(map[string]interface{})
				if generationConfig["maxOutputTokens"] != float64(256) {
					t.Fatalf("expected max_output_tokens preserved for gemini, got %#v", generationConfig["maxOutputTokens"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepared := prepareCursorRoundTrip(t, "/cursor/v1/responses", requestBody, tt.endpoint)
			tt.validate(t, prepared)
		})
	}
}
