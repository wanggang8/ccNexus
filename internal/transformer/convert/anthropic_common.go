package convert

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	anthropicCacheControlMaxBreakpoints = 4
	anthropicCacheControlBlockWindow    = 20
)

var anthropicEphemeralCacheControl = map[string]interface{}{"type": "ephemeral"}

func BuildAnthropicHeaders(apiKey string) map[string]string {
	headers := map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": AnthropicVersion,
	}
	if strings.HasPrefix(apiKey, "sk-") {
		headers["x-api-key"] = apiKey
		return headers
	}
	headers["Authorization"] = "Bearer " + apiKey
	return headers
}

func mergeAdjacentClaudeMessages(messages []map[string]interface{}) []map[string]interface{} {
	if len(messages) == 0 {
		return messages
	}

	merged := make([]map[string]interface{}, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		cloned := make(map[string]interface{}, len(message))
		for key, value := range message {
			cloned[key] = value
		}
		if len(merged) == 0 || fmt.Sprint(merged[len(merged)-1]["role"]) != fmt.Sprint(cloned["role"]) {
			merged = append(merged, cloned)
			continue
		}

		previousBlocks := normalizeClaudeMessageContent(merged[len(merged)-1]["content"])
		currentBlocks := normalizeClaudeMessageContent(cloned["content"])
		merged[len(merged)-1]["content"] = append(previousBlocks, currentBlocks...)
	}

	return merged
}

func normalizeClaudeMessageContent(content interface{}) []map[string]interface{} {
	switch typed := content.(type) {
	case nil:
		return nil
	case string:
		if typed == "" {
			return nil
		}
		return []map[string]interface{}{{"type": "text", "text": typed}}
	case []map[string]interface{}:
		return append([]map[string]interface{}{}, typed...)
	case []interface{}:
		blocks := make([]map[string]interface{}, 0, len(typed))
		for _, value := range typed {
			block, ok := value.(map[string]interface{})
			if !ok {
				continue
			}
			blocks = append(blocks, block)
		}
		return blocks
	default:
		text := fmt.Sprint(typed)
		if text == "" {
			return nil
		}
		return []map[string]interface{}{{"type": "text", "text": text}}
	}
}

func OptimizeAnthropicCacheControl(body []byte) ([]byte, error) {
	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	optimizeAnthropicCacheControlRequest(request)
	return json.Marshal(request)
}

func optimizeAnthropicCacheControlRequest(request map[string]interface{}) {
	normalizeAnthropicMessageContents(request)
	clearAnthropicCacheControls(request)

	structural := injectAnthropicStructuralAnchors(request)
	remaining := anthropicCacheControlMaxBreakpoints - structural
	if remaining <= 0 {
		return
	}

	refs := collectAnthropicCacheableBlockRefs(request)
	if len(refs) == 0 {
		return
	}

	anchors := 1
	if len(refs) >= anthropicCacheControlBlockWindow {
		anchors = 2
	}
	if anchors > remaining {
		anchors = remaining
	}

	if anchors >= 1 {
		refs[len(refs)-1]["cache_control"] = anthropicEphemeralCacheControl
	}
	if anchors >= 2 && len(refs) > 1 {
		target := len(refs) - anthropicCacheControlBlockWindow
		if idx := pickAnthropicWindowAnchor(refs, target); idx >= 0 && idx != len(refs)-1 {
			refs[idx]["cache_control"] = anthropicEphemeralCacheControl
		}
	}
}

func normalizeAnthropicMessageContents(request map[string]interface{}) {
	messages, ok := request["messages"].([]interface{})
	if !ok {
		return
	}
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			continue
		}
		content, exists := message["content"]
		if !exists || content == nil {
			message["content"] = []interface{}{}
			continue
		}
		if text, ok := content.(string); ok {
			if text == "" {
				message["content"] = []interface{}{}
			} else {
				message["content"] = []interface{}{
					map[string]interface{}{"type": "text", "text": text},
				}
			}
		}
	}
}

func clearAnthropicCacheControls(request map[string]interface{}) {
	if tools, ok := request["tools"].([]interface{}); ok {
		for _, rawTool := range tools {
			tool, ok := rawTool.(map[string]interface{})
			if !ok {
				continue
			}
			delete(tool, "cache_control")
		}
	}

	switch system := request["system"].(type) {
	case []interface{}:
		for _, rawBlock := range system {
			block, ok := rawBlock.(map[string]interface{})
			if !ok {
				continue
			}
			delete(block, "cache_control")
		}
	}

	if messages, ok := request["messages"].([]interface{}); ok {
		for _, rawMessage := range messages {
			message, ok := rawMessage.(map[string]interface{})
			if !ok {
				continue
			}
			content, ok := message["content"].([]interface{})
			if !ok {
				continue
			}
			for _, rawBlock := range content {
				block, ok := rawBlock.(map[string]interface{})
				if !ok {
					continue
				}
				delete(block, "cache_control")
			}
		}
	}
}

func injectAnthropicStructuralAnchors(request map[string]interface{}) int {
	count := 0

	if tools, ok := request["tools"].([]interface{}); ok && len(tools) > 0 {
		if tool, ok := tools[len(tools)-1].(map[string]interface{}); ok {
			tool["cache_control"] = anthropicEphemeralCacheControl
			count++
		}
	}

	switch system := request["system"].(type) {
	case []interface{}:
		if len(system) > 0 {
			if block, ok := system[len(system)-1].(map[string]interface{}); ok {
				block["cache_control"] = anthropicEphemeralCacheControl
				count++
			}
		}
	case string:
		if strings.TrimSpace(system) != "" {
			request["system"] = []interface{}{
				map[string]interface{}{"type": "text", "text": system, "cache_control": anthropicEphemeralCacheControl},
			}
			count++
		}
	}

	return count
}

func collectAnthropicCacheableBlockRefs(request map[string]interface{}) []map[string]interface{} {
	refs := make([]map[string]interface{}, 0)
	messages, ok := request["messages"].([]interface{})
	if !ok {
		return refs
	}
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := message["content"].([]interface{})
		if !ok {
			continue
		}
		for _, rawBlock := range content {
			block, ok := rawBlock.(map[string]interface{})
			if !ok || !isAnthropicCacheableBlock(block) {
				continue
			}
			refs = append(refs, block)
		}
	}
	return refs
}

func isAnthropicCacheableBlock(block map[string]interface{}) bool {
	blockType := fmt.Sprint(block["type"])
	if blockType == "thinking" || blockType == "redacted_thinking" {
		return false
	}
	if blockType == "text" && strings.TrimSpace(fmt.Sprint(block["text"])) == "" {
		return false
	}
	return true
}

func pickAnthropicWindowAnchor(refs []map[string]interface{}, target int) int {
	if target < 0 {
		target = 0
	}
	if target >= len(refs) {
		return -1
	}
	for i := target; i >= 0; i-- {
		if _, exists := refs[i]["cache_control"]; !exists {
			return i
		}
	}
	for i := target + 1; i < len(refs); i++ {
		if _, exists := refs[i]["cache_control"]; !exists {
			return i
		}
	}
	return -1
}
