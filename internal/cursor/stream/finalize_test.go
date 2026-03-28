package stream

import (
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func TestFinalizeEmitsThinkCloseChunk(t *testing.T) {
	state := &FinalizeState{InThinkingTag: true}
	chunk := Finalize(shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIChat,
	}, state, "gpt-5")
	if len(chunk) == 0 {
		t.Fatalf("expected finalize chunk")
	}
	if state.InThinkingTag {
		t.Fatalf("expected finalize to clear thinking state")
	}
	if !strings.Contains(string(chunk), `</think>`) && !strings.Contains(string(chunk), `\u003c/think\u003e`) {
		t.Fatalf("expected finalize chunk to contain think close marker, got %s", chunk)
	}
}

func TestFinalizeNoopForNonChat(t *testing.T) {
	chunk := Finalize(shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatClaude,
	}, &FinalizeState{InThinkingTag: true}, "claude")
	if chunk != nil {
		t.Fatalf("expected nil chunk for non-chat finalize, got %s", chunk)
	}
}
