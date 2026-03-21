package compat

func NormalizeOpenAIChatRequestShape(data map[string]interface{}) Audit {
	var summary []string

	if normalizeClaudeToolDefinitions(data) {
		summary = append(summary, "claude_tools->openai_tools")
	}
	if normalizeClaudeToolResults(data) {
		summary = append(summary, "claude_tool_result->tool_message")
	}
	if stripOpenAIChatCacheControl(data) {
		summary = append(summary, "strip_cache_control")
	}
	if _, ok := data["thinking"]; ok {
		summary = append(summary, "keep_thinking")
	}
	if _, ok := data["enable_thinking"]; ok {
		summary = append(summary, "keep_enable_thinking")
	}
	if _, ok := data["budget_tokens"]; ok {
		summary = append(summary, "keep_budget_tokens")
	}

	return Audit{Changed: len(summary) > 0, Reason: "openai_chat_request_shape_normalize", Summary: summary}
}

func normalizeClaudeToolDefinitions(data map[string]interface{}) bool {
	tools, ok := data["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		return false
	}

	changed := false
	fixedTools := make([]interface{}, 0, len(tools))
	for _, toolInterface := range tools {
		tool, ok := toolInterface.(map[string]interface{})
		if !ok {
			fixedTools = append(fixedTools, toolInterface)
			continue
		}

		if name, hasName := tool["name"].(string); hasName && name != "" && tool["type"] == nil {
			parameters := tool["input_schema"]
			if parameters == nil {
				parameters = map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				}
			}
			fixedTools = append(fixedTools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        tool["name"],
					"description": tool["description"],
					"parameters":  parameters,
				},
			})
			changed = true
			continue
		}

		fixedTools = append(fixedTools, tool)
	}

	if changed {
		data["tools"] = fixedTools
	}
	return changed
}

func normalizeClaudeToolResults(data map[string]interface{}) bool {
	messages, ok := data["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return false
	}

	changed := false
	fixedMessages := make([]interface{}, 0, len(messages))
	for _, msgInterface := range messages {
		msg, ok := msgInterface.(map[string]interface{})
		if !ok {
			fixedMessages = append(fixedMessages, msgInterface)
			continue
		}

		content, ok := msg["content"].([]interface{})
		if !ok || len(content) == 0 {
			fixedMessages = append(fixedMessages, msg)
			continue
		}

		hasToolResult := false
		var otherBlocks []interface{}
		for _, item := range content {
			block, ok := item.(map[string]interface{})
			if !ok {
				otherBlocks = append(otherBlocks, item)
				continue
			}
			if block["type"] == "tool_result" {
				hasToolResult = true
				fixedMessages = append(fixedMessages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": block["tool_use_id"],
					"content":      block["content"],
				})
				continue
			}
			otherBlocks = append(otherBlocks, item)
		}
		if !hasToolResult {
			fixedMessages = append(fixedMessages, msg)
			continue
		}
		changed = true
		if len(otherBlocks) == 0 {
			continue
		}
		preservedMsg := map[string]interface{}{"role": msg["role"]}
		if len(otherBlocks) == 1 {
			if tb, ok := otherBlocks[0].(map[string]interface{}); ok {
				if text, ok := tb["text"].(string); ok && tb["type"] == "text" {
					preservedMsg["content"] = text
				} else {
					preservedMsg["content"] = otherBlocks
				}
			} else {
				preservedMsg["content"] = otherBlocks
			}
		} else {
			preservedMsg["content"] = otherBlocks
		}
		fixedMessages = append(fixedMessages, preservedMsg)
	}

	if changed {
		data["messages"] = fixedMessages
	}
	return changed
}

func stripOpenAIChatCacheControl(data map[string]interface{}) bool {
	changed := false
	if messages, ok := data["messages"].([]interface{}); ok {
		for _, msgInterface := range messages {
			if msg, ok := msgInterface.(map[string]interface{}); ok {
				if _, exists := msg["cache_control"]; exists {
					delete(msg, "cache_control")
					changed = true
				}
			}
		}
	}
	if tools, ok := data["tools"].([]interface{}); ok {
		for _, toolInterface := range tools {
			if tool, ok := toolInterface.(map[string]interface{}); ok {
				if _, exists := tool["cache_control"]; exists {
					delete(tool, "cache_control")
					changed = true
				}
				if fn, ok := tool["function"].(map[string]interface{}); ok {
					if _, exists := fn["cache_control"]; exists {
						delete(fn, "cache_control")
						changed = true
					}
				}
			}
		}
	}
	return changed
}
