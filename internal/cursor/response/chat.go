package response

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
	cursorcache "github.com/lich0821/ccNexus/internal/cursor/cache"
)

func FixChatBody(body []byte, clientModel string, cacheMessages []map[string]interface{}, thinkingCache *cursorcache.ThinkingCache) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}

	if clientModel != "" {
		payload["model"] = clientModel
	}

	if choices, ok := payload["choices"].([]interface{}); ok {
		for _, choiceValue := range choices {
			choice, ok := choiceValue.(map[string]interface{})
			if !ok {
				continue
			}
			fixChatChoice(choice)
			if len(cacheMessages) > 0 && thinkingCache != nil {
				if message, ok := choice["message"].(map[string]interface{}); ok {
					thinkingCache.StoreFromResponse(cacheMessages, message)
				}
			}
		}
	}

	return json.Marshal(payload)
}

func fixChatChoice(choice map[string]interface{}) {
	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return
	}

	promoteReasoningField(message)
	extractThinkTagsFromMessage(message)
	convertLegacyFunctionCall(message, choice)
	fixToolCalls(message, choice)
	rewriteFinishReason(choice)
}

func promoteReasoningField(container map[string]interface{}) {
	if _, ok := container["reasoning_content"]; ok {
		return
	}
	if value, ok := container["reasoningContent"]; ok {
		container["reasoning_content"] = value
		delete(container, "reasoningContent")
	}
}

func extractThinkTagsFromMessage(message map[string]interface{}) {
	content, ok := message["content"].(string)
	if !ok || content == "" {
		return
	}
	if _, exists := message["reasoning_content"]; exists {
		return
	}

	cleaned, reasoning := extractThinkTags(content)
	if reasoning == "" {
		return
	}
	message["reasoning_content"] = reasoning
	message["content"] = cleaned
}

func convertLegacyFunctionCall(message map[string]interface{}, choice map[string]interface{}) {
	if _, ok := message["tool_calls"]; ok {
		return
	}
	functionCall, ok := message["function_call"].(map[string]interface{})
	if !ok {
		return
	}

	message["tool_calls"] = []interface{}{
		map[string]interface{}{
			"id":   newToolCallID(),
			"type": "function",
			"function": map[string]interface{}{
				"name":      stringValue(functionCall["name"]),
				"arguments": stringValue(functionCall["arguments"]),
			},
		},
	}
	delete(message, "function_call")
	rewriteFinishReason(choice)
}

func fixToolCalls(message map[string]interface{}, choice map[string]interface{}) {
	toolCalls, ok := message["tool_calls"].([]interface{})
	if !ok || len(toolCalls) == 0 {
		return
	}

	for index, toolCallValue := range toolCalls {
		toolCall, ok := toolCallValue.(map[string]interface{})
		if !ok {
			continue
		}
		if stringValue(toolCall["id"]) == "" {
			toolCall["id"] = newToolCallID()
		}
		if _, ok := toolCall["index"]; !ok {
			toolCall["index"] = index
		}
		if stringValue(toolCall["type"]) != "function" {
			toolCall["type"] = "function"
		}
		functionData, _ := toolCall["function"].(map[string]interface{})
		if functionData == nil {
			functionData = map[string]interface{}{}
			toolCall["function"] = functionData
		}
		switch arguments := functionData["arguments"].(type) {
		case map[string]interface{}, []interface{}:
			encoded, _ := json.Marshal(arguments)
			functionData["arguments"] = string(encoded)
		case nil:
			functionData["arguments"] = "{}"
		}
		normalizeToolArguments(functionData)
	}

	if finishReason := stringValue(choice["finish_reason"]); finishReason != "tool_calls" {
		choice["finish_reason"] = "tool_calls"
	}
}

func FixToolCallsCompat(message map[string]interface{}, choice map[string]interface{}) {
	fixToolCalls(message, choice)
}

func rewriteFinishReason(choice map[string]interface{}) {
	if stringValue(choice["finish_reason"]) == "function_call" {
		choice["finish_reason"] = "tool_calls"
	}
}

func normalizeToolArguments(functionData map[string]interface{}) {
	rawArgs := functionData["arguments"]
	if rawArgs == nil {
		return
	}

	argsStr, ok := rawArgs.(string)
	if !ok {
		return
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return
	}

	args = applyToolArgFixes(stringValue(functionData["name"]), args)
	encoded, err := json.Marshal(args)
	if err != nil {
		return
	}
	functionData["arguments"] = string(encoded)
}

func applyToolArgFixes(toolName string, args map[string]interface{}) map[string]interface{} {
	args = normalizeArgs(args)
	args = repairStrReplaceArgs(toolName, args)
	return args
}

func ApplyToolArgFixesCompat(toolName string, args map[string]interface{}) map[string]interface{} {
	return applyToolArgFixes(toolName, args)
}

func normalizeArgs(args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return args
	}
	if _, ok := args["path"]; !ok {
		if filePath, ok := args["file_path"]; ok {
			args["path"] = filePath
			delete(args, "file_path")
		}
	}
	return args
}

var (
	smartDouble = []rune{'«', '»', '\u201c', '\u201d', '\u275e', '\u201f', '\u201e', '\u275d'}
	smartSingle = []rune{'\u2018', '\u2019', '\u201a', '\u201b'}
)

func repairStrReplaceArgs(toolName string, args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return args
	}
	nameLower := strings.ToLower(strings.TrimSpace(toolName))
	if !strings.Contains(nameLower, "str_replace") && !strings.Contains(nameLower, "search_replace") {
		return args
	}

	oldValue, _ := args["old_string"].(string)
	if oldValue == "" {
		oldValue, _ = args["old_str"].(string)
	}
	if oldValue == "" {
		return args
	}

	filePath, _ := args["path"].(string)
	if filePath == "" {
		filePath, _ = args["file_path"].(string)
	}
	if filePath == "" {
		return args
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return args
	}
	contentStr := string(content)
	if strings.Contains(contentStr, oldValue) {
		return args
	}
	normalizedOld := replaceSmartQuotes(oldValue)
	if normalizedOld != oldValue && strings.Contains(contentStr, normalizedOld) {
		if _, ok := args["old_string"]; ok {
			args["old_string"] = normalizedOld
		}
		if _, ok := args["old_str"]; ok {
			args["old_str"] = normalizedOld
		}
		if newValue, ok := args["new_string"].(string); ok {
			args["new_string"] = replaceSmartQuotes(newValue)
		}
		if newValue, ok := args["new_str"].(string); ok {
			args["new_str"] = replaceSmartQuotes(newValue)
		}
		return args
	}

	pattern, err := regexp.Compile(buildFuzzyPattern(oldValue))
	if err != nil {
		return args
	}
	matches := pattern.FindAllString(contentStr, -1)
	if len(matches) != 1 {
		return args
	}

	if _, ok := args["old_string"]; ok {
		args["old_string"] = matches[0]
	}
	if _, ok := args["old_str"]; ok {
		args["old_str"] = matches[0]
	}
	if newValue, ok := args["new_string"].(string); ok {
		args["new_string"] = replaceSmartQuotes(newValue)
	}
	if newValue, ok := args["new_str"].(string); ok {
		args["new_str"] = replaceSmartQuotes(newValue)
	}
	return args
}

func buildFuzzyPattern(text string) string {
	var builder strings.Builder
	for _, ch := range text {
		switch {
		case containsRune(smartDouble, ch) || ch == '"':
			builder.WriteString(`["\u00ab\u201c\u201d\u275e\u201f\u201e\u275d\u00bb]`)
		case containsRune(smartSingle, ch) || ch == '\'':
			builder.WriteString(`['\u2018\u2019\u201a\u201b]`)
		case ch == ' ' || ch == '\t':
			builder.WriteString(`\s+`)
		case ch == '\\':
			builder.WriteString(`\\{1,2}`)
		default:
			builder.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	return builder.String()
}

func replaceSmartQuotes(text string) string {
	var builder strings.Builder
	for _, ch := range text {
		switch {
		case containsRune(smartDouble, ch):
			builder.WriteRune('"')
		case containsRune(smartSingle, ch):
			builder.WriteRune('\'')
		default:
			builder.WriteRune(ch)
		}
	}
	return builder.String()
}

func containsRune(values []rune, target rune) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func extractThinkTags(text string) (string, string) {
	cleaned := text
	reasoningParts := make([]string, 0)

	for {
		start := strings.Index(cleaned, "<think>")
		if start < 0 {
			break
		}
		endOffset := strings.Index(cleaned[start+len("<think>"):], "</think>")
		if endOffset < 0 {
			break
		}
		end := start + len("<think>") + endOffset
		reasoningParts = append(reasoningParts, cleaned[start+len("<think>"):end])
		cleaned = cleaned[:start] + cleaned[end+len("</think>"):]
	}

	return strings.TrimSpace(cleaned), strings.TrimSpace(strings.Join(reasoningParts, "\n"))
}

func newToolCallID() string {
	return "call_" + uuid.NewString()
}

func NewToolCallIDCompat() string {
	return newToolCallID()
}

func ConvertAssistantToolUseMessageCompat(content []interface{}) map[string]interface{} {
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
			inputData = applyToolArgFixes(toolNameForCompat(block), inputData)
			inputJSON, _ := json.Marshal(inputData)
			callID := stringValue(block["id"])
			if callID == "" {
				callID = newToolCallID()
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

func ConvertToolResultMessageCompat(role string, content []interface{}) []interface{} {
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
				"content":      StringifyToolResultContentCompat(block["content"]),
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

func StringifyToolResultContentCompat(content interface{}) string {
	switch value := content.(type) {
	case string:
		return value
	case []interface{}:
		parts := make([]string, 0, len(value))
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

func toolNameForCompat(block map[string]interface{}) string {
	return stringValue(block["name"])
}
