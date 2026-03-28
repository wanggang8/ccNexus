package stream

import (
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func TestPrefixEmitsResponsesCreatedForBridgedResponsesRoutes(t *testing.T) {
	state := newResponsesState()
	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIResponses,
	}

	prefix := Prefix(meta, state, "cursor-model", "cx_resp_openai")
	prefixStr := string(prefix)
	if !strings.Contains(prefixStr, "event: response.created") {
		t.Fatalf("expected response.created event prefix, got %s", prefixStr)
	}
	if !strings.Contains(prefixStr, `"output":[]`) || !strings.Contains(prefixStr, `"model":"cursor-model"`) {
		t.Fatalf("expected created payload with output/model, got %s", prefixStr)
	}
	if state.ResponsesResponseID == "" {
		t.Fatalf("expected response id to be stored in state")
	}
	if !state.ResponsesCreatedEmitted {
		t.Fatalf("expected created flag to be tracked in state")
	}
}

func TestPrefixSkipsNativeResponsesAndNonCursorRoutes(t *testing.T) {
	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIResponses,
	}
	if prefix := Prefix(meta, newResponsesState(), "cursor-model", "cx_resp_openai2"); len(prefix) != 0 {
		t.Fatalf("expected native responses stream to skip synthetic created prefix, got %s", string(prefix))
	}

	if prefix := Prefix(shared.RequestMeta{}, newResponsesState(), "cursor-model", "cx_resp_openai"); len(prefix) != 0 {
		t.Fatalf("expected non-cursor route to skip synthetic created prefix, got %s", string(prefix))
	}
}
