package stream

import (
	"strings"
	"testing"
)

func TestFixMessagesBundleInjectsThinkingEvents(t *testing.T) {
	bundle := []byte(strings.Join([]string{
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello","reasoningContent":"think"}}`,
		"",
	}, "\n"))
	fixed, err := FixMessagesBundle(bundle, &FinalizeState{})
	if err != nil {
		t.Fatalf("FixMessagesBundle failed: %v", err)
	}
	fixedStr := string(fixed)
	if !strings.Contains(fixedStr, "event: content_block_start") || !strings.Contains(fixedStr, `"thinking":"think"`) {
		t.Fatalf("expected injected thinking SSE events, got %s", fixedStr)
	}
	if !strings.Contains(fixedStr, `"index":1`) {
		t.Fatalf("expected original text block index offset, got %s", fixedStr)
	}
}
