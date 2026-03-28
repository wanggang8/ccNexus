package entry

import (
	"strings"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func StripCursorPrefix(path string) (string, bool) {
	trimmed := strings.TrimSpace(path)
	switch {
	case trimmed == "/cursor":
		return "/", true
	case strings.HasPrefix(trimmed, "/cursor/"):
		stripped := strings.TrimPrefix(trimmed, "/cursor")
		if stripped == "" {
			return "/", true
		}
		return stripped, true
	default:
		return path, false
	}
}

func DetectClientFormat(path string) shared.ClientFormat {
	switch {
	case strings.HasPrefix(path, "/v1/messages") || strings.HasPrefix(path, "/messages"):
		return shared.ClientFormatClaude
	case strings.HasPrefix(path, "/v1/chat/completions") || strings.HasPrefix(path, "/chat/completions"):
		return shared.ClientFormatOpenAIChat
	case strings.HasPrefix(path, "/v1/responses") || strings.HasPrefix(path, "/responses"):
		return shared.ClientFormatOpenAIResponses
	default:
		return shared.ClientFormatUnknown
	}
}
