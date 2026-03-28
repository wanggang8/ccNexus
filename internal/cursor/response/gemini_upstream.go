package response

import (
	"encoding/json"
	"strings"
)

func FixGeminiUpstreamChatBody(rawUpstream, transformedBody []byte) ([]byte, error) {
	rawIDs := extractGeminiFunctionCallIDs(rawUpstream)
	if len(rawIDs) == 0 {
		return transformedBody, nil
	}

	payload, ok := decodeJSONObject(transformedBody)
	if !ok {
		return transformedBody, nil
	}

	choices, ok := payload["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return transformedBody, nil
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return transformedBody, nil
	}
	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return transformedBody, nil
	}
	toolCalls, ok := message["tool_calls"].([]interface{})
	if !ok || len(toolCalls) == 0 {
		return transformedBody, nil
	}

	changed := false
	rawIndex := 0
	for _, rawToolCall := range toolCalls {
		toolCall, ok := rawToolCall.(map[string]interface{})
		if !ok || rawIndex >= len(rawIDs) {
			continue
		}
		rawID := strings.TrimSpace(rawIDs[rawIndex])
		rawIndex++
		if rawID == "" {
			continue
		}
		toolCall["id"] = rawID
		changed = true
	}

	if !changed {
		return transformedBody, nil
	}
	return json.Marshal(payload)
}

func FixGeminiUpstreamResponsesBody(rawUpstream, transformedBody []byte) ([]byte, error) {
	rawIDs := extractGeminiFunctionCallIDs(rawUpstream)
	if len(rawIDs) == 0 {
		return transformedBody, nil
	}

	payload, ok := decodeJSONObject(transformedBody)
	if !ok {
		return transformedBody, nil
	}

	output, ok := payload["output"].([]interface{})
	if !ok || len(output) == 0 {
		return transformedBody, nil
	}

	changed := false
	rawIndex := 0
	for _, rawItem := range output {
		item, ok := rawItem.(map[string]interface{})
		if !ok || stringValue(item["type"]) != "function_call" || rawIndex >= len(rawIDs) {
			continue
		}
		rawID := strings.TrimSpace(rawIDs[rawIndex])
		rawIndex++
		if rawID == "" {
			continue
		}
		item["call_id"] = rawID
		changed = true
	}

	if !changed {
		return transformedBody, nil
	}
	return json.Marshal(payload)
}

func extractGeminiFunctionCallIDs(rawUpstream []byte) []string {
	payload, ok := decodeJSONObject(rawUpstream)
	if !ok {
		return nil
	}

	candidates, ok := payload["candidates"].([]interface{})
	if !ok || len(candidates) == 0 {
		return nil
	}
	candidate, ok := candidates[0].(map[string]interface{})
	if !ok {
		return nil
	}
	content, ok := candidate["content"].(map[string]interface{})
	if !ok {
		return nil
	}
	parts, ok := content["parts"].([]interface{})
	if !ok || len(parts) == 0 {
		return nil
	}

	ids := make([]string, 0)
	for _, rawPart := range parts {
		part, ok := rawPart.(map[string]interface{})
		if !ok {
			continue
		}
		functionCall, ok := part["functionCall"].(map[string]interface{})
		if !ok {
			continue
		}
		ids = append(ids, stringValue(functionCall["id"]))
	}
	return ids
}
