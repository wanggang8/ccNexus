package route

import (
	"testing"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func TestAllowedBackendsMatrix(t *testing.T) {
	chatBackends := AllowedBackends(shared.ClientFormatOpenAIChat)
	if len(chatBackends) != 4 {
		t.Fatalf("expected 4 backends for chat route, got %d", len(chatBackends))
	}

	responsesBackends := AllowedBackends(shared.ClientFormatOpenAIResponses)
	if len(responsesBackends) != 4 {
		t.Fatalf("expected 4 backends for responses route, got %d", len(responsesBackends))
	}

	messagesBackends := AllowedBackends(shared.ClientFormatClaude)
	if len(messagesBackends) != 1 || messagesBackends[0] != shared.BackendAnthropic {
		t.Fatalf("expected messages route to be anthropic-only, got %#v", messagesBackends)
	}
}

func TestValidateBackendRejectsUnsupportedMessagesMatrix(t *testing.T) {
	if err := ValidateBackend(shared.ClientFormatClaude, shared.BackendAnthropic); err != nil {
		t.Fatalf("expected anthropic to be allowed on messages route, got %v", err)
	}
	if err := ValidateBackend(shared.ClientFormatClaude, shared.BackendOpenAI); err == nil {
		t.Fatalf("expected openai to be rejected on messages route")
	}
}

func TestBackendFromTransformer(t *testing.T) {
	tests := []struct {
		transformer string
		want        shared.Backend
	}{
		{transformer: "cx_chat_claude", want: shared.BackendAnthropic},
		{transformer: "cx_chat_openai", want: shared.BackendOpenAI},
		{transformer: "cx_resp_openai2", want: shared.BackendOpenAI2},
		{transformer: "cx_resp_gemini", want: shared.BackendGemini},
		{transformer: "cc_cli", want: shared.BackendCLI},
	}

	for _, tt := range tests {
		if got := BackendFromTransformer(tt.transformer); got != tt.want {
			t.Fatalf("BackendFromTransformer(%q) = %q, want %q", tt.transformer, got, tt.want)
		}
	}
}
