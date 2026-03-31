package augment

import (
	"encoding/json"
	"strings"
)

type RequestFallbackPayload struct {
	Name string
	Body []byte
}

func BuildRequestFallbackPayloads(targetType string, body []byte) []RequestFallbackPayload {
	switch targetType {
	case "openai", "openai2":
		return buildOpenAIRequestFallbackPayloads(targetType, body)
	case "claude", "cli":
		return buildClaudeRequestFallbackPayloads(targetType, body)
	default:
		return nil
	}
}

func buildOpenAIRequestFallbackPayloads(targetType string, body []byte) []RequestFallbackPayload {
	var original map[string]interface{}
	if err := json.Unmarshal(body, &original); err != nil {
		return nil
	}

	seen := map[string]struct{}{string(body): {}}
	attempts := make([]RequestFallbackPayload, 0, 8)

	// Each fallback is built independently from original, not cumulatively.
	// This matches BYOK's approach where each attempt is a fresh modification.

	// 1. drop tool_choice
	if p := deepCloneRequestMap(original); dropKey(p, "tool_choice") {
		appendFallbackPayload(&attempts, seen, targetType, "drop_tool_choice", p)
	}

	// 2. drop parallel_tool_calls
	if p := deepCloneRequestMap(original); dropKey(p, "parallel_tool_calls") {
		appendFallbackPayload(&attempts, seen, targetType, "drop_parallel_tool_calls", p)
	}

	// 3. convert tools to functions (openai chat/completions only, not responses API)
	// Also converts messages: tool_calls -> function_call, role:tool -> role:function
	if targetType == "openai" {
		if p := deepCloneRequestMap(original); convertOpenAIToolsToFunctions(p) {
			convertOpenAIMessagesToFunctionCalling(p)
			appendFallbackPayload(&attempts, seen, targetType, "convert_tools_to_functions", p)
		}
	}

	// 4. strip vision content from messages (for gateways that don't support multimodal)
	if p := deepCloneRequestMap(original); stripOpenAIVisionFromMessages(p) {
		appendFallbackPayload(&attempts, seen, targetType, "strip_vision", p)
	}

	// 5. drop all tool-related fields
	if p := deepCloneRequestMap(original); dropOpenAITools(p) {
		appendFallbackPayload(&attempts, seen, targetType, "drop_tools", p)
	}

	// 6. strip vision + drop tools (combined)
	if p := deepCloneRequestMap(original); stripOpenAIVisionFromMessages(p) || dropOpenAITools(p) {
		// Re-apply both to ensure combined effect
		p2 := deepCloneRequestMap(original)
		stripOpenAIVisionFromMessages(p2)
		dropOpenAITools(p2)
		appendFallbackPayload(&attempts, seen, targetType, "strip_vision_drop_tools", p2)
	}

	return attempts
}

func appendFallbackPayload(attempts *[]RequestFallbackPayload, seen map[string]struct{}, targetType string, name string, payload map[string]interface{}) {
	if payload == nil {
		return
	}
	payload = sanitizeProviderRequest(targetType, payload)
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	key := string(body)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*attempts = append(*attempts, RequestFallbackPayload{Name: name, Body: body})
}

func deepCloneRequestMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	cloned, _ := cloneJSONValue(src).(map[string]interface{})
	return cloned
}

func cloneJSONValue(v interface{}) interface{} {
	switch value := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(value))
		for k, item := range value {
			out[k] = cloneJSONValue(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(value))
		for i, item := range value {
			out[i] = cloneJSONValue(item)
		}
		return out
	case []map[string]interface{}:
		out := make([]interface{}, len(value))
		for i, item := range value {
			out[i] = cloneJSONValue(item)
		}
		return out
	default:
		return value
	}
}

func dropKey(payload map[string]interface{}, key string) bool {
	if payload == nil {
		return false
	}
	if _, ok := payload[key]; !ok {
		return false
	}
	delete(payload, key)
	return true
}

func dropNestedKey(payload map[string]interface{}, key string, nested string) bool {
	if payload == nil {
		return false
	}
	inner, ok := payload[key].(map[string]interface{})
	if !ok || inner == nil {
		return false
	}
	if _, ok := inner[nested]; !ok {
		return false
	}
	delete(inner, nested)
	if len(inner) == 0 {
		delete(payload, key)
	} else {
		payload[key] = inner
	}
	return true
}

func convertOpenAIToolsToFunctions(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	rawTools, ok := payload["tools"].([]interface{})
	if !ok || len(rawTools) == 0 {
		return false
	}

	functions := make([]interface{}, 0, len(rawTools))
	for _, raw := range rawTools {
		tool, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if !strings.EqualFold(firstString(tool, "type"), "function") {
			continue
		}
		fn := firstMap(tool, "function")
		if len(fn) == 0 {
			continue
		}
		functions = append(functions, cloneJSONValue(fn))
	}
	if len(functions) == 0 {
		return false
	}

	payload["functions"] = functions
	delete(payload, "tools")
	delete(payload, "tool_choice")
	return true
}

// convertOpenAIMessagesToFunctionCalling converts messages from tool_calls format
// to function_call format, matching BYOK's convertMessagesToFunctionCalling.
// - assistant.tool_calls[0] -> assistant.function_call
// - role:tool -> role:function (with name lookup from prior assistant messages)
func convertOpenAIMessagesToFunctionCalling(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return false
	}

	// Build map of tool_call_id -> function name from assistant messages
	idToName := make(map[string]string)
	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if firstString(msg, "role") != "assistant" {
			continue
		}
		toolCalls, ok := msg["tool_calls"].([]interface{})
		if !ok || len(toolCalls) == 0 {
			continue
		}
		for _, tcRaw := range toolCalls {
			tc, ok := tcRaw.(map[string]interface{})
			if !ok {
				continue
			}
			id := firstString(tc, "id")
			fn := firstMap(tc, "function")
			name := firstString(fn, "name")
			if id != "" && name != "" {
				if _, exists := idToName[id]; !exists {
					idToName[id] = name
				}
			}
		}
	}

	changed := false
	newMessages := make([]interface{}, 0, len(messages))

	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			newMessages = append(newMessages, raw)
			continue
		}
		role := firstString(msg, "role")

		// Convert role:tool to role:function
		if role == "tool" {
			toolCallID := firstString(msg, "tool_call_id")
			content := ""
			if c, ok := msg["content"].(string); ok {
				content = c
			}
			name := idToName[toolCallID]
			if name != "" {
				newMessages = append(newMessages, map[string]interface{}{
					"role":    "function",
					"name":    name,
					"content": content,
				})
			} else {
				// Orphan tool result: convert to user message
				newMessages = append(newMessages, map[string]interface{}{
					"role":    "user",
					"content": buildOrphanToolResultUserContent(toolCallID, content),
				})
			}
			changed = true
			continue
		}

		// Convert assistant.tool_calls to assistant.function_call
		if role == "assistant" {
			toolCalls, hasToolCalls := msg["tool_calls"].([]interface{})
			_, hasFunctionCall := msg["function_call"]
			if hasToolCalls && len(toolCalls) > 0 && !hasFunctionCall {
				tc, ok := toolCalls[0].(map[string]interface{})
				if ok {
					fn := firstMap(tc, "function")
					name := firstString(fn, "name")
					args := firstString(fn, "arguments")
					if args == "" {
						args = "{}"
					}
					if name != "" {
						newMsg := cloneJSONValue(msg).(map[string]interface{})
						delete(newMsg, "tool_calls")
						newMsg["function_call"] = map[string]interface{}{
							"name":      name,
							"arguments": args,
						}
						newMessages = append(newMessages, newMsg)
						changed = true
						continue
					}
				}
			}
		}

		newMessages = append(newMessages, msg)
	}

	if changed {
		payload["messages"] = newMessages
	}
	return changed
}

func buildOrphanToolResultUserContent(toolCallID, content string) string {
	return buildOrphanToolResultAsUserContent(toolCallID, content)
}

// stripOpenAIVisionFromMessages removes non-text content from messages,
// matching BYOK's stripVisionFromMessages.
func stripOpenAIVisionFromMessages(payload map[string]interface{}) bool {
	if payload == nil {
		return false
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return false
	}

	changed := false
	newMessages := make([]interface{}, 0, len(messages))

	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			newMessages = append(newMessages, raw)
			continue
		}

		content, ok := msg["content"].([]interface{})
		if !ok {
			newMessages = append(newMessages, msg)
			continue
		}

		var textParts []string
		sawNonText := false

		for _, part := range content {
			block, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			typ := firstString(block, "type")
			if typ == "text" {
				if text := firstString(block, "text"); strings.TrimSpace(text) != "" {
					textParts = append(textParts, strings.TrimSpace(text))
				}
			} else {
				sawNonText = true
			}
		}

		base := strings.Join(textParts, "\n\n")
		suffix := ""
		if sawNonText {
			suffix = "[non-text content omitted]"
		}

		var asText string
		if base != "" && suffix != "" {
			asText = base + "\n\n" + suffix
		} else if base != "" {
			asText = base
		} else if suffix != "" {
			asText = suffix
		}

		if asText == "" {
			newMessages = append(newMessages, msg)
			continue
		}

		newMsg := cloneJSONValue(msg).(map[string]interface{})
		newMsg["content"] = asText
		newMessages = append(newMessages, newMsg)
		changed = true
	}

	if changed {
		payload["messages"] = newMessages
	}
	return changed
}

func dropOpenAITools(payload map[string]interface{}) bool {
	changed := false
	if dropKey(payload, "tools") {
		changed = true
	}
	if dropKey(payload, "functions") {
		changed = true
	}
	if dropKey(payload, "tool_choice") {
		changed = true
	}
	if dropKey(payload, "parallel_tool_calls") {
		changed = true
	}
	return changed
}
