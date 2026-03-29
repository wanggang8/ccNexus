package augment

import (
	"encoding/json"
	"strings"
)

func buildClaudeRequestFallbackPayloads(targetType string, body []byte) []RequestFallbackPayload {
	var original map[string]interface{}
	if err := json.Unmarshal(body, &original); err != nil {
		return nil
	}

	seen := map[string]struct{}{string(body): {}}
	attempts := make([]RequestFallbackPayload, 0, 8)

	// Each fallback is built independently from original, not cumulatively.

	// 1. drop tool_choice
	if p := deepCloneRequestMap(original); dropKey(p, "tool_choice") {
		appendFallbackPayload(&attempts, seen, targetType, "drop_tool_choice", p)
	}

	// 2. normalize system string to blocks
	if p := deepCloneRequestMap(original); normalizeClaudeSystemBlocks(p) {
		appendFallbackPayload(&attempts, seen, targetType, "normalize_system_blocks", p)
	}

	// 3. normalize messages content string to blocks
	if p := deepCloneRequestMap(original); normalizeClaudeMessagesBlocks(p) {
		appendFallbackPayload(&attempts, seen, targetType, "normalize_message_blocks", p)
	}

	// 4. system blocks + message blocks combined
	if p := deepCloneRequestMap(original); normalizeClaudeSystemBlocks(p) || normalizeClaudeMessagesBlocks(p) {
		p2 := deepCloneRequestMap(original)
		normalizeClaudeSystemBlocks(p2)
		normalizeClaudeMessagesBlocks(p2)
		appendFallbackPayload(&attempts, seen, targetType, "normalize_all_blocks", p2)
	}

	// 5. repair tool_use/tool_result pairs (BYOK: repairAnthropicToolUsePairs)
	if p := deepCloneRequestMap(original); repairClaudeToolUsePairs(p) {
		appendFallbackPayload(&attempts, seen, targetType, "repair_tool_pairs", p)
	}

	// 6. ensure first message is role:user (Claude API requirement)
	if p := deepCloneRequestMap(original); ensureClaudeFirstMessageIsUser(p) {
		appendFallbackPayload(&attempts, seen, targetType, "ensure_first_user", p)
	}

	// 7. drop all tools
	if p := deepCloneRequestMap(original); dropClaudeTools(p) {
		appendFallbackPayload(&attempts, seen, targetType, "drop_tools", p)
	}

	return attempts
}

func normalizeClaudeSystemBlocks(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	systemValue, ok := payload["system"]
	if !ok {
		return false
	}
	converted, changed := normalizeClaudeContentValue(systemValue)
	if !changed {
		return false
	}
	payload["system"] = converted
	return true
}

func normalizeClaudeMessagesBlocks(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return false
	}
	changed := false
	for i, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		converted, msgChanged := normalizeClaudeContentValue(msg["content"])
		if !msgChanged {
			continue
		}
		msg["content"] = converted
		messages[i] = msg
		changed = true
	}
	if changed {
		payload["messages"] = messages
	}
	return changed
}

func normalizeClaudeContentValue(value interface{}) (interface{}, bool) {
	switch v := value.(type) {
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return []interface{}{}, true
		}
		return []interface{}{map[string]interface{}{"type": "text", "text": v}}, true
	case []interface{}:
		changed := false
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			switch block := item.(type) {
			case string:
				out = append(out, map[string]interface{}{"type": "text", "text": block})
				changed = true
			default:
				_ = block
				out = append(out, item)
			}
		}
		return out, changed
	default:
		return value, false
	}
}

func dropClaudeTools(payload map[string]interface{}) bool {
	changed := false
	if dropKey(payload, "tools") {
		changed = true
	}
	if dropKey(payload, "tool_choice") {
		changed = true
	}
	return changed
}

// repairClaudeToolUsePairs ensures that every tool_use block in an assistant
// message has a matching tool_result in the next user message, and converts
// orphan tool_results to text blocks. This matches BYOK's
// repairAnthropicToolUsePairs.
func repairClaudeToolUsePairs(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return false
	}

	type pendingToolUse struct {
		toolUseID string
		toolName  string
	}

	changed := false
	out := make([]interface{}, 0, len(messages)+4)
	var pending []pendingToolUse

	injectMissing := func() {
		if len(pending) == 0 {
			return
		}
		blocks := make([]interface{}, 0, len(pending))
		for _, tc := range pending {
			blocks = append(blocks, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": tc.toolUseID,
				"content":     "[tool_result not available: tool_use_id=" + tc.toolUseID + " tool_name=" + tc.toolName + "]",
				"is_error":    true,
			})
		}
		out = append(out, map[string]interface{}{
			"role":    "user",
			"content": blocks,
		})
		changed = true
		pending = nil
	}

	pendingMap := func() map[string]int {
		m := make(map[string]int, len(pending))
		for i, p := range pending {
			m[p.toolUseID] = i
		}
		return m
	}

	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			if len(pending) > 0 {
				injectMissing()
			}
			out = append(out, raw)
			continue
		}
		role := firstString(msg, "role")

		if len(pending) > 0 && role == "user" {
			// Check if this user message contains matching tool_results
			content, hasContent := msg["content"].([]interface{})
			if !hasContent {
				injectMissing()
				out = append(out, msg)
				continue
			}

			pm := pendingMap()
			toolResultBlocks := make([]interface{}, 0)
			otherBlocks := make([]interface{}, 0)
			localChanged := false

			for _, blockRaw := range content {
				block, ok := blockRaw.(map[string]interface{})
				if !ok {
					otherBlocks = append(otherBlocks, blockRaw)
					continue
				}
				if firstString(block, "type") == "tool_result" {
					id := firstString(block, "tool_use_id")
					if _, found := pm[id]; found {
						delete(pm, id)
						toolResultBlocks = append(toolResultBlocks, block)
					} else {
						// Orphan tool_result: convert to text
						orphanText := buildOrphanClaudeToolResultAsText(block)
						otherBlocks = append(otherBlocks, map[string]interface{}{
							"type": "text",
							"text": orphanText,
						})
						localChanged = true
					}
				} else {
					otherBlocks = append(otherBlocks, block)
				}
			}

			// Inject missing tool_results for unmatched pending
			if len(pm) > 0 {
				for _, p := range pending {
					if _, stillPending := pm[p.toolUseID]; stillPending {
						toolResultBlocks = append(toolResultBlocks, map[string]interface{}{
							"type":        "tool_result",
							"tool_use_id": p.toolUseID,
							"content":     "[tool_result not available: tool_use_id=" + p.toolUseID + " tool_name=" + p.toolName + "]",
							"is_error":    true,
						})
						localChanged = true
					}
				}
			}
			pending = nil

			newBlocks := append(toolResultBlocks, otherBlocks...)
			if localChanged {
				newMsg := cloneJSONValue(msg).(map[string]interface{})
				newMsg["content"] = newBlocks
				out = append(out, newMsg)
				changed = true
			} else if len(toolResultBlocks) > 0 {
				newMsg := cloneJSONValue(msg).(map[string]interface{})
				newMsg["content"] = newBlocks
				out = append(out, newMsg)
			} else {
				out = append(out, msg)
			}
			continue
		}

		if len(pending) > 0 {
			injectMissing()
		}

		if role == "assistant" {
			out = append(out, msg)
			// Collect tool_use blocks
			content, ok := msg["content"].([]interface{})
			if ok {
				for _, blockRaw := range content {
					block, ok := blockRaw.(map[string]interface{})
					if !ok {
						continue
					}
					if firstString(block, "type") == "tool_use" {
						id := firstString(block, "id")
						name := firstString(block, "name")
						if id != "" && name != "" {
							pending = append(pending, pendingToolUse{toolUseID: id, toolName: name})
						}
					}
				}
			}
			continue
		}

		// For user messages without pending tool_uses, convert orphan tool_results
		if role == "user" {
			content, ok := msg["content"].([]interface{})
			if ok {
				hasOrphan := false
				for _, blockRaw := range content {
					block, ok := blockRaw.(map[string]interface{})
					if ok && firstString(block, "type") == "tool_result" {
						hasOrphan = true
						break
					}
				}
				if hasOrphan {
					newBlocks := make([]interface{}, 0, len(content))
					for _, blockRaw := range content {
						block, ok := blockRaw.(map[string]interface{})
						if ok && firstString(block, "type") == "tool_result" {
							orphanText := buildOrphanClaudeToolResultAsText(block)
							newBlocks = append(newBlocks, map[string]interface{}{
								"type": "text",
								"text": orphanText,
							})
							changed = true
						} else {
							newBlocks = append(newBlocks, blockRaw)
						}
					}
					newMsg := cloneJSONValue(msg).(map[string]interface{})
					newMsg["content"] = newBlocks
					out = append(out, newMsg)
					continue
				}
			}
		}

		out = append(out, msg)
	}

	injectMissing()

	if changed {
		payload["messages"] = out
	}
	return changed
}

func buildOrphanClaudeToolResultAsText(block map[string]interface{}) string {
	id := firstString(block, "tool_use_id")
	content := ""
	if c, ok := block["content"].(string); ok {
		content = strings.TrimSpace(c)
	}
	const maxLen = 8000
	if len(content) > maxLen {
		content = content[:maxLen/2] + "\n...[truncated]...\n" + content[len(content)-maxLen/2:]
	}
	header := "[orphan_tool_result]"
	if id != "" {
		header = "[orphan_tool_result tool_use_id=" + id + "]"
	}
	if content != "" {
		return header + "\n" + content
	}
	return header
}

// ensureClaudeFirstMessageIsUser prepends a dummy user message if the first
// message is not role:user. Claude API requires messages[0].role == "user".
func ensureClaudeFirstMessageIsUser(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	switch msgs := payload["messages"].(type) {
	case []interface{}:
		if len(msgs) == 0 {
			return false
		}
		first, ok := msgs[0].(map[string]interface{})
		if !ok {
			return false
		}
		if firstString(first, "role") == "user" {
			return false
		}
		dummy := map[string]interface{}{"role": "user", "content": "-"}
		payload["messages"] = append([]interface{}{dummy}, msgs...)
		return true
	case []map[string]interface{}:
		if len(msgs) == 0 {
			return false
		}
		if firstString(msgs[0], "role") == "user" {
			return false
		}
		dummy := map[string]interface{}{"role": "user", "content": "-"}
		newMsgs := make([]map[string]interface{}, 0, len(msgs)+1)
		newMsgs = append(newMsgs, dummy)
		newMsgs = append(newMsgs, msgs...)
		payload["messages"] = newMsgs
		return true
	default:
		return false
	}
}
