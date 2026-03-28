package route

import (
	"testing"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func TestPolicyHelpers(t *testing.T) {
	if !NeedClaudeMaxTokensFloor(shared.ClientFormatOpenAIChat, shared.BackendAnthropic) {
		t.Fatalf("expected claude floor for cursor chat -> anthropic")
	}
	if NeedClaudeMaxTokensFloor(shared.ClientFormatClaude, shared.BackendAnthropic) {
		t.Fatalf("did not expect claude floor for messages passthrough")
	}
	if !NeedClaudeCacheControl(shared.ClientFormatOpenAIResponses, shared.BackendAnthropic) {
		t.Fatalf("expected claude cache control for cursor responses -> anthropic")
	}
	if !NeedPassthroughModelOverride(shared.ClientFormatOpenAIChat, shared.BackendOpenAI) {
		t.Fatalf("expected model override for cursor chat passthrough openai")
	}
	if !NeedPassthroughModelOverride(shared.ClientFormatOpenAIResponses, shared.BackendOpenAI2) {
		t.Fatalf("expected model override for cursor responses passthrough openai2")
	}
	if NeedPassthroughModelOverride(shared.ClientFormatClaude, shared.BackendAnthropic) {
		t.Fatalf("did not expect passthrough override for messages passthrough")
	}
}
