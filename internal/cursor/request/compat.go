package request

import (
	"encoding/json"
	"strings"

	"github.com/lich0821/ccNexus/internal/cursor/route"
	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func ValidateTransformer(meta shared.RequestMeta, transformerName string) error {
	if !meta.CursorMode {
		return nil
	}
	return route.ValidateBackend(meta.ClientFormat, route.BackendFromTransformer(transformerName))
}

func ValidateEndpointTransformer(meta shared.RequestMeta, endpointTransformer string) error {
	if !meta.CursorMode {
		return nil
	}
	return route.ValidateBackend(meta.ClientFormat, route.BackendFromEndpointTransformer(endpointTransformer))
}

func NeedClaudeCacheControl(meta shared.RequestMeta, transformerName string) bool {
	return route.NeedClaudeCacheControl(meta.ClientFormat, route.BackendFromTransformer(transformerName))
}

func NeedClaudeMaxTokensFloor(meta shared.RequestMeta, transformerName string) bool {
	return route.NeedClaudeMaxTokensFloor(meta.ClientFormat, route.BackendFromTransformer(transformerName))
}

func NeedPassthroughModelOverride(meta shared.RequestMeta, transformerName string) bool {
	if !meta.CursorMode {
		return false
	}
	return route.NeedPassthroughModelOverride(meta.ClientFormat, route.BackendFromTransformer(transformerName))
}

func EnsureClaudeMaxTokensFloor(payload []byte) []byte {
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		return payload
	}

	value, ok := body["max_tokens"]
	if !ok {
		body["max_tokens"] = float64(8192)
	} else if numeric, ok := value.(float64); !ok || numeric < 8192 {
		body["max_tokens"] = float64(8192)
	}

	updated, err := json.Marshal(body)
	if err != nil {
		return payload
	}
	return updated
}

func EnsureClaudeToolSchemas(payload []byte) []byte {
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		return payload
	}

	rawTools, ok := body["tools"].([]interface{})
	if !ok || len(rawTools) == 0 {
		return payload
	}

	changed := false
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]interface{})
		if !ok {
			continue
		}
		if schema, exists := tool["input_schema"]; exists && schema != nil {
			continue
		}
		tool["input_schema"] = map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
		changed = true
	}

	if !changed {
		return payload
	}

	updated, err := json.Marshal(body)
	if err != nil {
		return payload
	}
	return updated
}

func EnsureClaudeToolUseInputs(payload []byte) []byte {
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		return payload
	}

	rawMessages, ok := body["messages"].([]interface{})
	if !ok || len(rawMessages) == 0 {
		return payload
	}

	changed := false
	for _, rawMessage := range rawMessages {
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := message["content"].([]interface{})
		if !ok || len(content) == 0 {
			continue
		}
		for _, rawBlock := range content {
			block, ok := rawBlock.(map[string]interface{})
			if !ok || stringValue(block["type"]) != "tool_use" {
				continue
			}
			switch input := block["input"].(type) {
			case nil:
				block["input"] = map[string]interface{}{}
				changed = true
			case string:
				if strings.TrimSpace(input) == "" {
					block["input"] = map[string]interface{}{}
					changed = true
				}
			}
		}
	}

	if !changed {
		return payload
	}

	updated, err := json.Marshal(body)
	if err != nil {
		return payload
	}
	return updated
}

func NormalizeGeminiFunctionParts(payload []byte) ([]byte, error) {
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, err
	}

	contents, ok := body["contents"].([]interface{})
	if !ok {
		return payload, nil
	}

	changed := false
	for _, rawContent := range contents {
		content, ok := rawContent.(map[string]interface{})
		if !ok {
			continue
		}
		parts, ok := content["parts"].([]interface{})
		if !ok {
			continue
		}
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]interface{})
			if !ok {
				continue
			}
			if functionCall, ok := part["functionCall"].(map[string]interface{}); ok {
				if normalized, updated := normalizeGeminiJSONValue(functionCall["args"]); updated {
					functionCall["args"] = normalized
					changed = true
				}
			}
			if functionResponse, ok := part["functionResponse"].(map[string]interface{}); ok {
				if normalized, updated := normalizeGeminiJSONValue(functionResponse["response"]); updated {
					functionResponse["response"] = normalized
					changed = true
				}
			}
		}
	}

	if !changed {
		return payload, nil
	}
	return json.Marshal(body)
}

func NormalizeOpenAI2EasyInputMessages(payload []byte) ([]byte, error) {
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, err
	}

	rawInput, ok := body["input"].([]interface{})
	if !ok || len(rawInput) == 0 {
		return payload, nil
	}

	changed := false
	normalized := make([]interface{}, 0, len(rawInput))
	for index, rawItem := range rawInput {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			normalized = append(normalized, rawItem)
			continue
		}
		if shouldKeepStructuredOpenAI2Message(rawInput, index, item) {
			normalized = append(normalized, rawItem)
			continue
		}
		simplified, ok := simplifyOpenAI2EasyInputMessage(item)
		if !ok {
			normalized = append(normalized, rawItem)
			continue
		}
		normalized = append(normalized, simplified)
		changed = true
	}

	if !changed {
		return payload, nil
	}
	body["input"] = normalized
	return json.Marshal(body)
}

func shouldKeepStructuredOpenAI2Message(rawInput []interface{}, index int, item map[string]interface{}) bool {
	if item == nil {
		return false
	}
	if itemType, _ := item["type"].(string); itemType != "message" {
		return false
	}
	role, _ := item["role"].(string)
	if role != "assistant" {
		return false
	}
	if index+1 >= len(rawInput) {
		return false
	}
	nextItem, ok := rawInput[index+1].(map[string]interface{})
	if !ok {
		return false
	}
	nextType, _ := nextItem["type"].(string)
	return nextType == "function_call"
}

func simplifyOpenAI2EasyInputMessage(item map[string]interface{}) (map[string]interface{}, bool) {
	if item == nil {
		return nil, false
	}
	if itemType, _ := item["type"].(string); itemType != "message" {
		return nil, false
	}

	role, _ := item["role"].(string)
	if role != "user" && role != "assistant" {
		return nil, false
	}

	content, ok := item["content"].([]interface{})
	if !ok || len(content) != 1 {
		return nil, false
	}
	part, ok := content[0].(map[string]interface{})
	if !ok {
		return nil, false
	}

	partType, _ := part["type"].(string)
	switch role {
	case "user":
		if partType != "input_text" {
			return nil, false
		}
	case "assistant":
		if partType != "output_text" {
			return nil, false
		}
	}

	text, _ := part["text"].(string)
	if text == "" {
		return nil, false
	}

	return map[string]interface{}{
		"role":    role,
		"content": text,
	}, true
}

func normalizeGeminiJSONValue(value interface{}) (interface{}, bool) {
	switch typed := value.(type) {
	case nil:
		return map[string]interface{}{}, true
	case string:
		var decoded interface{}
		if err := json.Unmarshal([]byte(typed), &decoded); err == nil {
			return decoded, true
		}
		if typed == "" {
			return map[string]interface{}{}, true
		}
		return map[string]interface{}{"result": typed}, true
	default:
		return value, false
	}
}
