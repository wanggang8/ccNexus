package augment

import "strings"

type responseToolCall struct {
	ID        string
	Name      string
	Arguments string
}

func assistantResponseText(text string, nodes []Node) string {
	if text := strings.TrimSpace(text); text != "" {
		return text
	}
	return strings.TrimSpace(extractText(nodes))
}

func extractResponseToolCalls(nodes []Node) []responseToolCall {
	var out []responseToolCall
	preferCompletedToolUse := hasCompletedToolUse(nodes)
	for _, node := range nodes {
		if node.ToolUse == nil {
			continue
		}
		if node.Type != 5 && !(node.Type == 7 && !preferCompletedToolUse) {
			continue
		}
		id := strings.TrimSpace(node.ToolUse.ToolUseID)
		name := strings.TrimSpace(node.ToolUse.ToolName)
		if id == "" || name == "" {
			continue
		}
		args := strings.TrimSpace(node.ToolUse.InputJSON)
		if args == "" {
			args = "{}"
		}
		out = append(out, responseToolCall{ID: id, Name: name, Arguments: args})
	}
	return out
}
