package compat

import (
	"encoding/json"
)

// Audit describes a minimal compat intervention summary.
type Audit struct {
	Changed bool     `json:"changed"`
	Reason  string   `json:"reason,omitempty"`
	Summary []string `json:"summary,omitempty"`
}

func DecodeJSONMap(req []byte) (map[string]interface{}, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(req, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func MustJSON(reqMap map[string]interface{}) ([]byte, error) {
	return json.Marshal(reqMap)
}

func OverrideModel(reqMap map[string]interface{}, model string) {
	if model != "" {
		reqMap["model"] = model
	}
}

func CopyIfPresent(dst, src map[string]interface{}, keys ...string) []string {
	var changed []string
	for _, key := range keys {
		if value, ok := src[key]; ok {
			dst[key] = value
			changed = append(changed, key)
		}
	}
	return changed
}

func DeleteIfPresent(dst map[string]interface{}, keys ...string) []string {
	var changed []string
	for _, key := range keys {
		if _, ok := dst[key]; ok {
			delete(dst, key)
			changed = append(changed, key)
		}
	}
	return changed
}

func ExtractResponsesReasoningEffort(src map[string]interface{}) interface{} {
	if effort, ok := src["reasoning_effort"]; ok {
		return effort
	}
	reasoning, ok := src["reasoning"].(map[string]interface{})
	if !ok {
		return nil
	}
	if effort, ok := reasoning["effort"]; ok {
		return effort
	}
	return nil
}

func ExtractResponsesSystem(src map[string]interface{}) string {
	if instructions, ok := src["instructions"].(string); ok && instructions != "" {
		return instructions
	}
	input, ok := src["input"].([]interface{})
	if !ok {
		return ""
	}
	for _, item := range input {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := itemMap["role"].(string)
		if role != "system" {
			continue
		}
		switch content := itemMap["content"].(type) {
		case string:
			if content != "" {
				return content
			}
		case []interface{}:
			var text string
			for _, part := range content {
				partMap, ok := part.(map[string]interface{})
				if !ok {
					continue
				}
				partType, _ := partMap["type"].(string)
				if partType == "input_text" || partType == "output_text" || partType == "text" {
					if t, ok := partMap["text"].(string); ok {
						text += t
					}
				}
			}
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func BuildClaudeThinkingFromResponses(src map[string]interface{}) map[string]interface{} {
	reasoning, ok := src["reasoning"].(map[string]interface{})
	if !ok {
		return nil
	}
	effort, _ := reasoning["effort"].(string)
	if effort == "" {
		return nil
	}

	budget := 2048
	switch effort {
	case "low":
		budget = 1024
	case "medium":
		budget = 2048
	case "high":
		budget = 4096
	}

	return map[string]interface{}{
		"type":          "enabled",
		"budget_tokens": budget,
	}
}
