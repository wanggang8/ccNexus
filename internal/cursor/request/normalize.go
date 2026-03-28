package request

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/lich0821/ccNexus/internal/cursor/shared"
	"github.com/lich0821/ccNexus/internal/transformer/convert"
)

func NormalizeRequestBody(path string, body []byte) ([]byte, error) {
	clientFormat := detectClientFormat(path)
	model := extractModelFromPayload(body)
	normalized := body

	switch clientFormat {
	case shared.ClientFormatClaude:
		// Align with api2cursor messages route behavior: keep request passthrough.
		normalized = body
	case shared.ClientFormatOpenAIChat:
		if isResponsesLikePayload(body) && !hasMessagesPayload(body) {
			converted, err := convert.OpenAI2ReqToOpenAI(body, model)
			if err == nil {
				normalized = mergeChatFields(body, converted)
			}
		}
		normalized = NormalizeOpenAIChatBody(normalized)
	case shared.ClientFormatOpenAIResponses:
		if hasMessagesPayload(body) && !isResponsesLikePayload(body) {
			normalizedChat := NormalizeOpenAIChatBody(body)
			converted, err := convert.OpenAIReqToOpenAI2(normalizedChat, model)
			if err == nil {
				normalized = converted
			}
		}
	}

	return normalized, nil
}

func NormalizeOpenAIChatBody(body []byte) []byte {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body
	}
	normalizeChatTopLevelSystem(payload)
	if messages, ok := payload["messages"].([]interface{}); ok {
		payload["messages"] = normalizeChatMessages(messages)
		messages = payload["messages"].([]interface{})
		payload["messages"] = convertMessages(messages)
	}
	if tools, ok := payload["tools"].([]interface{}); ok {
		normalizedTools := make([]interface{}, 0, len(tools))
		for _, tool := range tools {
			normalizedTools = append(normalizedTools, normalizeToolDefinition(tool))
		}
		payload["tools"] = normalizedTools
	}
	normalizeToolChoice(payload)
	// Cursor may carry Responses-only continuation hints on the chat endpoint.
	// OpenAI chat-compatible backends reject these fields, so strip them at the
	// Cursor chat normalize layer instead of leaking them to passthrough routes.
	dropResponsesOnlyChatFields(payload)
	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

func detectClientFormat(path string) shared.ClientFormat {
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

func extractModelFromPayload(body []byte) string {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return ""
	}
	return stringValue(payload["model"])
}

func hasMessagesPayload(body []byte) bool {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return false
	}
	messages, ok := payload["messages"].([]interface{})
	return ok && len(messages) > 0
}

func isResponsesLikePayload(body []byte) bool {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return false
	}
	_, hasInput := payload["input"]
	return hasInput
}

func mergeChatFields(sourceBody, targetBody []byte) []byte {
	source, ok := decodeJSONObject(sourceBody)
	if !ok {
		return targetBody
	}
	target, ok := decodeJSONObject(targetBody)
	if !ok {
		return targetBody
	}
	if _, exists := target["tool_choice"]; !exists {
		if toolChoice, exists := source["tool_choice"]; exists {
			target["tool_choice"] = toolChoice
		}
	}
	if _, exists := target["tools"]; !exists {
		if tools, exists := source["tools"]; exists {
			target["tools"] = tools
		}
	}
	merged, err := json.Marshal(target)
	if err != nil {
		return targetBody
	}
	return merged
}

func convertMessages(messages []interface{}) []interface{} {
	converted := make([]interface{}, 0, len(messages))
	for _, messageValue := range messages {
		message, ok := messageValue.(map[string]interface{})
		if !ok {
			converted = append(converted, messageValue)
			continue
		}
		content, ok := message["content"].([]interface{})
		if !ok {
			converted = append(converted, message)
			continue
		}
		hasToolUse := false
		hasToolResult := false
		for _, blockValue := range content {
			block, ok := blockValue.(map[string]interface{})
			if !ok {
				continue
			}
			switch stringValue(block["type"]) {
			case "tool_use":
				hasToolUse = true
			case "tool_result":
				hasToolResult = true
			}
		}
		if !hasToolUse && !hasToolResult {
			converted = append(converted, message)
			continue
		}
		role := stringValue(message["role"])
		if role == "assistant" && hasToolUse {
			converted = append(converted, convertAssistantToolUseMessage(content))
			continue
		}
		if hasToolResult {
			converted = append(converted, convertToolResultMessage(role, content)...)
			continue
		}
		converted = append(converted, message)
	}
	return converted
}

func convertAssistantToolUseMessage(content []interface{}) map[string]interface{} {
	textParts := make([]string, 0)
	toolCalls := make([]interface{}, 0)
	for _, blockValue := range content {
		block, ok := blockValue.(map[string]interface{})
		if !ok {
			continue
		}
		switch stringValue(block["type"]) {
		case "text":
			if text := stringValue(block["text"]); text != "" {
				textParts = append(textParts, text)
			}
		case "tool_use":
			inputData, _ := block["input"].(map[string]interface{})
			inputJSON, _ := json.Marshal(inputData)
			callID := stringValue(block["id"])
			if callID == "" {
				callID = "call_" + uuidString()
			}
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   callID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      stringValue(block["name"]),
					"arguments": string(inputJSON),
				},
			})
		}
	}
	message := map[string]interface{}{"role": "assistant"}
	if len(textParts) > 0 {
		message["content"] = strings.Join(textParts, "\n")
	} else {
		message["content"] = nil
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	return message
}

func ConvertAssistantToolUseMessageCompat(content []interface{}) map[string]interface{} {
	return convertAssistantToolUseMessage(content)
}

func convertToolResultMessage(role string, content []interface{}) []interface{} {
	converted := make([]interface{}, 0)
	otherParts := make([]interface{}, 0)
	for _, blockValue := range content {
		block, ok := blockValue.(map[string]interface{})
		if !ok {
			continue
		}
		if stringValue(block["type"]) == "tool_result" {
			converted = append(converted, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": stringValue(block["tool_use_id"]),
				"content":      stringifyToolResultContent(block["content"]),
			})
			continue
		}
		otherParts = append(otherParts, block)
	}
	if len(otherParts) > 0 {
		converted = append(converted, map[string]interface{}{
			"role":    role,
			"content": otherParts,
		})
	}
	return converted
}

func ConvertToolResultMessageCompat(role string, content []interface{}) []interface{} {
	return convertToolResultMessage(role, content)
}

func stringifyToolResultContent(content interface{}) string {
	switch value := content.(type) {
	case string:
		return value
	case []interface{}:
		parts := make([]string, 0)
		for _, blockValue := range value {
			block, ok := blockValue.(map[string]interface{})
			if !ok {
				continue
			}
			if stringValue(block["type"]) == "text" {
				if text := stringValue(block["text"]); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprint(content)
	}
}

func StringifyToolResultContentCompat(content interface{}) string {
	return stringifyToolResultContent(content)
}

func normalizeToolDefinition(tool interface{}) interface{} {
	toolMap, ok := tool.(map[string]interface{})
	if !ok {
		return tool
	}

	if stringValue(toolMap["type"]) == "function" {
		if _, ok := toolMap["function"].(map[string]interface{}); ok {
			return tool
		}
		return tool
	}

	if name := stringValue(toolMap["name"]); name != "" {
		normalized := map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        name,
				"description": stringValue(toolMap["description"]),
			},
		}

		functionData := normalized["function"].(map[string]interface{})
		if inputSchema, exists := toolMap["input_schema"]; exists {
			functionData["parameters"] = inputSchema
		} else if parameters, exists := toolMap["parameters"]; exists {
			functionData["parameters"] = parameters
		} else {
			functionData["parameters"] = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}

		return normalized
	}

	return tool
}

func normalizeToolChoice(payload map[string]interface{}) {
	toolChoice, ok := payload["tool_choice"].(map[string]interface{})
	if !ok {
		return
	}

	switch stringValue(toolChoice["type"]) {
	case "auto":
		payload["tool_choice"] = "auto"
	case "any":
		payload["tool_choice"] = "required"
	}
}

func normalizeChatTopLevelSystem(payload map[string]interface{}) {
	if payload == nil {
		return
	}

	rawSystem, exists := payload["system"]
	if !exists {
		return
	}
	delete(payload, "system")

	systemText := flattenOpenAIText(rawSystem)
	if systemText == "" {
		return
	}

	rawMessages, _ := payload["messages"].([]interface{})
	if len(rawMessages) > 0 {
		if firstMessage, ok := rawMessages[0].(map[string]interface{}); ok && stringValue(firstMessage["role"]) == "system" {
			firstMessage["content"] = mergeSystemMessageContent(systemText, firstMessage["content"])
			payload["messages"] = rawMessages
			return
		}
	}

	systemMessage := map[string]interface{}{
		"role":    "system",
		"content": systemText,
	}
	payload["messages"] = append([]interface{}{systemMessage}, rawMessages...)
}

func normalizeChatMessages(messages []interface{}) []interface{} {
	normalized := make([]interface{}, 0, len(messages))
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			normalized = append(normalized, rawMessage)
			continue
		}
		if content, ok := message["content"].([]interface{}); ok {
			message["content"] = normalizeOpenAIContentBlocks(content)
		}
		normalized = append(normalized, message)
	}
	return normalized
}

func normalizeOpenAIContentBlocks(content []interface{}) []interface{} {
	normalized := make([]interface{}, 0, len(content))
	for _, rawBlock := range content {
		switch block := rawBlock.(type) {
		case string:
			normalized = append(normalized, map[string]interface{}{
				"type": "text",
				"text": block,
			})
		case map[string]interface{}:
			cloned := cloneJSONObject(block)
			delete(cloned, "cache_control")
			if stringValue(cloned["type"]) == "" && cloned["text"] != nil {
				cloned["type"] = "text"
			}
			normalized = append(normalized, cloned)
		default:
			normalized = append(normalized, rawBlock)
		}
	}
	return normalized
}

func mergeSystemMessageContent(prefix string, existing interface{}) interface{} {
	if prefix == "" {
		return existing
	}

	switch value := existing.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return prefix
		}
		return prefix + "\n\n" + value
	case []interface{}:
		return append([]interface{}{
			map[string]interface{}{"type": "text", "text": prefix},
		}, normalizeOpenAIContentBlocks(value)...)
	default:
		if text := flattenOpenAIText(existing); text != "" {
			return prefix + "\n\n" + text
		}
		return prefix
	}
}

func flattenOpenAIText(content interface{}) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, rawPart := range value {
			switch part := rawPart.(type) {
			case string:
				text := strings.TrimSpace(part)
				if text != "" {
					parts = append(parts, text)
				}
			case map[string]interface{}:
				text := strings.TrimSpace(stringValue(part["text"]))
				if text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n\n")
	case map[string]interface{}:
		return strings.TrimSpace(stringValue(value["text"]))
	default:
		return ""
	}
}

func dropResponsesOnlyChatFields(payload map[string]interface{}) {
	delete(payload, "previous_response_id")
}

func decodeJSONObject(body []byte) (map[string]interface{}, bool) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func uuidString() string {
	return uuid.NewString()
}
