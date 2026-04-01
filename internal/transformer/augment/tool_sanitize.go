package augment

import (
	"regexp"
	"strings"
)

var toolWireIDSanitizeRE = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// sanitizeToolUseIDString replaces characters outside [A-Za-z0-9_-] with underscores
// so Claude/OpenAI tool_use_id and tool_call_id stay aligned across turns
// (same idea as augment-open-gateway sanitizeToolUseID).
func sanitizeToolUseIDString(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return toolWireIDSanitizeRE.ReplaceAllString(id, "_")
}

// stripToolResultTrainingSuffix removes trailing Augment/plugin reminder text from tool
// results so it is not sent to the upstream model (augment-open-gateway separateToolResultContent).
func stripToolResultTrainingSuffix(content string) string {
	s := strings.TrimSpace(content)
	if s == "" {
		return s
	}
	separators := []string{
		"\n\n✔️请记住",
		"\n\n❌请记住",
		"\n✔️请记住",
		"\n❌请记住",
		"✔️请记住",
		"❌请记住",
	}
	for _, sep := range separators {
		if idx := strings.Index(s, sep); idx >= 0 {
			return strings.TrimSpace(s[:idx])
		}
	}
	return s
}
