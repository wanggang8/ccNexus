package augment

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func isPlaceholderMessage(message string) bool {
	s := strings.TrimSpace(message)
	if s == "" || len(s) > 16 {
		return false
	}
	for _, ch := range s {
		if ch != '-' {
			return false
		}
	}
	return true
}

func excludeToolResultNodes(nodes []Node) []Node {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Type == 1 && node.ToolResultNode != nil {
			continue
		}
		out = append(out, node)
	}
	return out
}

func coerceRulesText(rules interface{}) string {
	switch v := rules.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []string:
		return strings.TrimSpace(strings.Join(v, "\n"))
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := strings.TrimSpace(toString(item)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return strings.TrimSpace(toString(v))
	}
}

func repairOpenAIToolCallMessages(messages []map[string]interface{}) []map[string]interface{} {
	if len(messages) == 0 {
		return messages
	}

	type toolCall struct {
		ID   string
		Name string
		Args string
	}

	out := make([]map[string]interface{}, 0, len(messages)+2)
	var pending map[string]toolCall
	var bufferedOrphans []map[string]interface{}

	flushBufferedOrphans := func() {
		if len(bufferedOrphans) == 0 {
			return
		}
		for _, msg := range bufferedOrphans {
			out = append(out, map[string]interface{}{
				"role":    "user",
				"content": buildOrphanToolResultAsUserContent(msg["tool_call_id"], msg["content"]),
			})
		}
		bufferedOrphans = nil
	}

	injectMissing := func() {
		if len(pending) == 0 {
			pending = nil
			return
		}
		keys := make([]string, 0, len(pending))
		for id := range pending {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		for _, id := range keys {
			tc := pending[id]
			out = append(out, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      buildMissingToolResultContent("tool_call_id", tc.ID, tc.Name, tc.Args),
			})
		}
		pending = nil
	}

	closePending := func() {
		injectMissing()
		flushBufferedOrphans()
	}

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		if pending != nil {
			if role == "tool" {
				id, _ := msg["tool_call_id"].(string)
				if id != "" {
					if _, ok := pending[id]; ok {
						delete(pending, id)
						out = append(out, msg)
						if len(pending) == 0 {
							pending = nil
							flushBufferedOrphans()
						}
					} else {
						bufferedOrphans = append(bufferedOrphans, msg)
					}
					continue
				}
			}
			closePending()
		}

		if role == "assistant" {
			if toolCalls := normalizeOpenAIToolCalls(msg["tool_calls"]); len(toolCalls) > 0 {
				out = append(out, msg)
				pending = make(map[string]toolCall, len(toolCalls))
				for _, tc := range toolCalls {
					pending[tc.ID] = tc
				}
				bufferedOrphans = nil
				continue
			}
		}

		if role == "tool" {
			out = append(out, map[string]interface{}{
				"role":    "user",
				"content": buildOrphanToolResultAsUserContent(msg["tool_call_id"], msg["content"]),
			})
			continue
		}

		out = append(out, msg)
	}

	closePending()
	return out
}

func normalizeOpenAIToolCalls(value interface{}) []struct {
	ID   string
	Name string
	Args string
} {
	rawCalls, ok := value.([]interface{})
	if !ok {
		if typed, ok := value.([]map[string]interface{}); ok {
			rawCalls = make([]interface{}, 0, len(typed))
			for _, item := range typed {
				rawCalls = append(rawCalls, item)
			}
		} else {
			return nil
		}
	}

	out := make([]struct {
		ID   string
		Name string
		Args string
	}, 0, len(rawCalls))
	seen := make(map[string]struct{}, len(rawCalls))
	for _, raw := range rawCalls {
		tc, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := tc["id"].(string)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		fn, _ := tc["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		args, _ := fn["arguments"].(string)
		out = append(out, struct {
			ID   string
			Name string
			Args string
		}{ID: id, Name: name, Args: args})
	}
	return out
}

func repairClaudeToolUseMessages(messages []map[string]interface{}) []map[string]interface{} {
	if len(messages) == 0 {
		return messages
	}

	type toolUse struct {
		ID    string
		Name  string
		Input map[string]interface{}
	}

	out := make([]map[string]interface{}, 0, len(messages)+1)
	var pending map[string]toolUse

	injectMissing := func() {
		if len(pending) == 0 {
			pending = nil
			return
		}
		keys := make([]string, 0, len(pending))
		for id := range pending {
			keys = append(keys, id)
		}
		sort.Strings(keys)

		blocks := make([]map[string]interface{}, 0, len(keys))
		for _, id := range keys {
			tc := pending[id]
			blocks = append(blocks, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": tc.ID,
				"is_error":    true,
				"content":     buildMissingToolResultContent("tool_use_id", tc.ID, tc.Name, stableJSON(tc.Input)),
			})
		}
		out = append(out, map[string]interface{}{"role": "user", "content": blocks})
		pending = nil
	}

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		if pending != nil {
			if role == "user" {
				blocks := normalizeClaudeBlocks(msg["content"])
				toolResults := make([]map[string]interface{}, 0, len(blocks))
				otherBlocks := make([]map[string]interface{}, 0, len(blocks))
				changed := false
				for _, block := range blocks {
					blockType, _ := block["type"].(string)
					if blockType != "tool_result" {
						otherBlocks = append(otherBlocks, block)
						continue
					}
					id, _ := block["tool_use_id"].(string)
					if id != "" {
						if _, ok := pending[id]; ok {
							delete(pending, id)
							toolResults = append(toolResults, block)
							continue
						}
					}
					otherBlocks = append(otherBlocks, map[string]interface{}{
						"type": "text",
						"text": buildOrphanToolResultAsUserContent(block["tool_use_id"], block["content"]),
					})
					changed = true
				}
				if len(pending) > 0 {
					keys := make([]string, 0, len(pending))
					for id := range pending {
						keys = append(keys, id)
					}
					sort.Strings(keys)
					for _, id := range keys {
						tc := pending[id]
						toolResults = append(toolResults, map[string]interface{}{
							"type":        "tool_result",
							"tool_use_id": tc.ID,
							"is_error":    true,
							"content":     buildMissingToolResultContent("tool_use_id", tc.ID, tc.Name, stableJSON(tc.Input)),
						})
					}
					pending = nil
					changed = true
				} else {
					pending = nil
				}

				if changed || len(toolResults) > 0 {
					newBlocks := make([]map[string]interface{}, 0, len(toolResults)+len(otherBlocks))
					newBlocks = append(newBlocks, toolResults...)
					newBlocks = append(newBlocks, otherBlocks...)
					out = append(out, map[string]interface{}{"role": "user", "content": newBlocks})
				} else {
					out = append(out, msg)
				}
				continue
			}

			injectMissing()
		}

		if role == "assistant" {
			out = append(out, msg)
			blocks := normalizeClaudeBlocks(msg["content"])
			found := make(map[string]toolUse)
			for _, block := range blocks {
				blockType, _ := block["type"].(string)
				if blockType != "tool_use" {
					continue
				}
				id, _ := block["id"].(string)
				name, _ := block["name"].(string)
				if id == "" || name == "" {
					continue
				}
				input, _ := block["input"].(map[string]interface{})
				found[id] = toolUse{ID: id, Name: name, Input: input}
			}
			if len(found) > 0 {
				pending = found
			}
			continue
		}

		out = append(out, msg)
	}

	injectMissing()
	return out
}

func normalizeClaudeBlocks(content interface{}) []map[string]interface{} {
	switch blocks := content.(type) {
	case []map[string]interface{}:
		return blocks
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(blocks))
		for _, raw := range blocks {
			block, ok := raw.(map[string]interface{})
			if ok {
				out = append(out, block)
			}
		}
		return out
	case string:
		if strings.TrimSpace(blocks) == "" {
			return nil
		}
		return []map[string]interface{}{{"type": "text", "text": blocks}}
	default:
		return nil
	}
}

func buildMissingToolResultContent(idKey, id, toolName, args string) string {
	payload := map[string]interface{}{
		"error":   "tool_result_missing",
		"message": "未收到对应的 tool_result（可能是工具未执行、被禁用、权限不足，或历史中丢失）。请在缺失结果的前提下继续推理，或改为不依赖该工具。",
	}
	if strings.TrimSpace(id) != "" {
		payload[idKey] = id
	}
	if strings.TrimSpace(toolName) != "" {
		payload["tool_name"] = toolName
	}
	if strings.TrimSpace(args) != "" {
		payload["arguments"] = truncateString(args, 4000)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return payload["message"].(string)
	}
	return string(data)
}

func buildOrphanToolResultAsUserContent(id interface{}, content interface{}) string {
	return buildTaggedOrphanContent("orphan_tool_result", "id", id, content)
}

func buildTaggedOrphanContent(kind string, idLabel string, id interface{}, content interface{}) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "orphan_tool_result"
	}
	idLabel = strings.TrimSpace(idLabel)
	if idLabel == "" {
		idLabel = "id"
	}
	header := "[" + kind + "]"
	if toolID := strings.TrimSpace(toString(id)); toolID != "" {
		header = fmt.Sprintf("[%s %s=%s]", kind, idLabel, toolID)
	}
	body := strings.TrimSpace(stringifyToolResultContent(content))
	if body == "" {
		return header
	}
	return header + "\n" + truncateString(body, 8000)
}

func stringifyToolResultContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, raw := range v {
			block, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if text, _ := block["text"].(string); strings.TrimSpace(text) != "" {
				parts = append(parts, text)
				continue
			}
			data, err := json.Marshal(block)
			if err == nil {
				parts = append(parts, string(data))
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return toString(content)
	}
}

func truncateString(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	keep := (max - 3) / 2
	return text[:keep] + "..." + text[len(text)-(max-3-keep):]
}
