package response

import "encoding/json"

// FixOpenAIUpstreamChatBody applies api2cursor-style OpenAI chat compat fixes
// to the raw upstream body before a Cursor /responses -> openai bridge converts
// it into Responses output items.
func FixOpenAIUpstreamChatBody(body []byte) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}

	if choices, ok := payload["choices"].([]interface{}); ok {
		for _, choiceValue := range choices {
			choice, ok := choiceValue.(map[string]interface{})
			if !ok {
				continue
			}
			fixChatChoice(choice)
		}
	}

	return json.Marshal(payload)
}
