package stream

import (
	"bytes"
	"strings"
)

func FixGeminiUpstreamChatBundle(rawEventData, transformedBundle []byte) ([]byte, error) {
	rawIDs := extractGeminiFunctionCallIDsFromEvent(rawEventData)
	if len(rawIDs) == 0 {
		return transformedBundle, nil
	}

	chunks := splitSSEBundle(transformedBundle)
	if len(chunks) == 0 {
		return transformedBundle, nil
	}

	var output bytes.Buffer
	rawIndex := 0
	for _, chunk := range chunks {
		eventName, data, ok := parseSSEChunk(chunk)
		if !ok || data == "[DONE]" {
			output.Write(chunk)
			continue
		}

		payload, ok := decodeJSONObject([]byte(data))
		if !ok {
			output.Write(chunk)
			continue
		}

		changed := false
		choices, ok := payload["choices"].([]interface{})
		if ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if delta, ok := choice["delta"].(map[string]interface{}); ok {
					if toolCalls, ok := delta["tool_calls"].([]interface{}); ok {
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
					}
				}
			}
		}

		if !changed {
			output.Write(chunk)
			continue
		}
		writeSSEChunk(&output, eventName, payload)
	}

	return output.Bytes(), nil
}

func FixGeminiUpstreamResponsesBundle(rawEventData, transformedBundle []byte) ([]byte, error) {
	rawIDs := extractGeminiFunctionCallIDsFromEvent(rawEventData)
	if len(rawIDs) == 0 {
		return transformedBundle, nil
	}

	chunks := splitSSEBundle(transformedBundle)
	if len(chunks) == 0 {
		return transformedBundle, nil
	}

	var output bytes.Buffer
	rawIndex := 0
	for _, chunk := range chunks {
		eventName, data, ok := parseSSEChunk(chunk)
		if !ok || data == "[DONE]" {
			output.Write(chunk)
			continue
		}

		payload, ok := decodeJSONObject([]byte(data))
		if !ok {
			output.Write(chunk)
			continue
		}
		if eventName == "" {
			eventName = stringValue(payload["type"])
		}

		changed := false
		switch eventName {
		case "response.output_item.added", "response.output_item.done":
			item, ok := payload["item"].(map[string]interface{})
			if !ok || stringValue(item["type"]) != "function_call" || rawIndex >= len(rawIDs) {
				break
			}
			rawID := strings.TrimSpace(rawIDs[rawIndex])
			rawIndex++
			if rawID == "" {
				break
			}
			item["call_id"] = rawID
			changed = true
		}

		if !changed {
			output.Write(chunk)
			continue
		}
		writeSSEChunk(&output, eventName, payload)
	}

	return output.Bytes(), nil
}

func extractGeminiFunctionCallIDsFromEvent(rawEventData []byte) []string {
	_, data, ok := parseSSEChunk(rawEventData)
	if !ok || strings.TrimSpace(data) == "" || strings.TrimSpace(data) == "[DONE]" {
		return nil
	}

	payload, ok := decodeJSONObject([]byte(data))
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
