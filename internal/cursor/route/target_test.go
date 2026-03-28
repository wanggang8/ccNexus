package route

import (
	"testing"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func TestResolveTargetPathForCursorRoutes(t *testing.T) {
	meta := shared.RequestMeta{
		CursorMode: true,
	}

	if path, ok := ResolveTargetPath(meta, "cx_chat_openai", "gpt-5", []byte(`{"stream":true}`)); !ok || path != "/v1/chat/completions" {
		t.Fatalf("expected chat openai path, got ok=%v path=%s", ok, path)
	}

	if path, ok := ResolveTargetPath(meta, "cx_chat_openai2", "gpt-5", []byte(`{"stream":true}`)); !ok || path != "/v1/responses" {
		t.Fatalf("expected chat openai2 to bridge through responses path, got ok=%v path=%s", ok, path)
	}

	if path, ok := ResolveTargetPath(meta, "cx_resp_openai2", "gpt-5", []byte(`{"stream":true}`)); !ok || path != "/v1/responses" {
		t.Fatalf("expected native responses path, got ok=%v path=%s", ok, path)
	}

	if path, ok := ResolveTargetPath(meta, "cc_claude", "claude-sonnet-4-20250514", []byte(`{"stream":false}`)); !ok || path != "/v1/messages" {
		t.Fatalf("expected anthropic messages path, got ok=%v path=%s", ok, path)
	}

	if path, ok := ResolveTargetPath(meta, "cx_resp_gemini", "gemini-2.5-pro", []byte(`{"stream":true}`)); !ok || path != "/v1/models/gemini-2.5-pro:streamGenerateContent?alt=sse" {
		t.Fatalf("expected gemini stream path, got ok=%v path=%s", ok, path)
	}

	if path, ok := ResolveTargetPath(meta, "cx_resp_gemini", "gemini-2.5-pro", []byte(`{"stream":false}`)); !ok || path != "/v1/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("expected gemini non-stream path, got ok=%v path=%s", ok, path)
	}
}

func TestResolveTargetPathUsesCursorStreamIntentForGemini(t *testing.T) {
	meta := shared.RequestMeta{
		CursorMode: true,
		Stream:     true,
	}

	if path, ok := ResolveTargetPath(meta, "cx_chat_gemini", "gemini-2.5-pro", []byte(`{"contents":[]}`)); !ok || path != "/v1/models/gemini-2.5-pro:streamGenerateContent?alt=sse" {
		t.Fatalf("expected gemini cursor stream intent to force stream path, got ok=%v path=%s", ok, path)
	}
}

func TestResolveTargetPathSkipsNonCursorRequests(t *testing.T) {
	if path, ok := ResolveTargetPath(shared.RequestMeta{}, "cx_chat_openai", "gpt-5", nil); ok || path != "" {
		t.Fatalf("expected non-cursor request to skip route resolution, got ok=%v path=%s", ok, path)
	}
}
