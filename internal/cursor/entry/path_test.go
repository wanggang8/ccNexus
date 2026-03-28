package entry

import (
	"testing"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func TestStripCursorPrefix(t *testing.T) {
	tests := []struct {
		path     string
		want     string
		isCursor bool
	}{
		{path: "/cursor/v1/chat/completions", want: "/v1/chat/completions", isCursor: true},
		{path: "/cursor/v1/responses", want: "/v1/responses", isCursor: true},
		{path: "/cursor/v1/messages", want: "/v1/messages", isCursor: true},
		{path: "/v1/chat/completions", want: "/v1/chat/completions", isCursor: false},
	}

	for _, tt := range tests {
		got, ok := StripCursorPrefix(tt.path)
		if got != tt.want || ok != tt.isCursor {
			t.Fatalf("StripCursorPrefix(%q) = (%q,%v), want (%q,%v)", tt.path, got, ok, tt.want, tt.isCursor)
		}
	}
}

func TestDetectClientFormat(t *testing.T) {
	tests := []struct {
		path string
		want shared.ClientFormat
	}{
		{path: "/v1/chat/completions", want: shared.ClientFormatOpenAIChat},
		{path: "/v1/responses", want: shared.ClientFormatOpenAIResponses},
		{path: "/v1/messages", want: shared.ClientFormatClaude},
		{path: "/v1/unknown", want: shared.ClientFormatUnknown},
	}

	for _, tt := range tests {
		if got := DetectClientFormat(tt.path); got != tt.want {
			t.Fatalf("DetectClientFormat(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
