package response

import "encoding/json"

func FixMessagesBody(body []byte) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}
	injectMessagesThinking(payload)
	return json.Marshal(payload)
}

func injectMessagesThinking(payload map[string]interface{}) {
	reasoning := stringValue(payload["reasoning_content"])
	if reasoning == "" {
		reasoning = stringValue(payload["reasoningContent"])
	}
	if reasoning == "" {
		return
	}
	delete(payload, "reasoning_content")
	delete(payload, "reasoningContent")

	content, ok := payload["content"].([]interface{})
	if !ok {
		content = []interface{}{}
	}
	for _, blockValue := range content {
		block, ok := blockValue.(map[string]interface{})
		if ok && stringValue(block["type"]) == "thinking" {
			return
		}
	}
	payload["content"] = append([]interface{}{
		map[string]interface{}{"type": "thinking", "thinking": reasoning},
	}, content...)
}
