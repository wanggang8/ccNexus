package augment

import "strings"

func sanitizeProviderRequest(targetType string, req map[string]interface{}) map[string]interface{} {
	if req == nil {
		return nil
	}
	caps := capabilitiesForTarget(targetType)
	cleaned := cloneMap(req)

	if !caps.SupportsPreviousResponseID {
		delete(cleaned, "previous_response_id")
	}
	if !caps.SupportsStore {
		delete(cleaned, "store")
	}
	if !caps.SupportsInstructions {
		delete(cleaned, "instructions")
	}
	if !caps.SupportsToolChoice {
		delete(cleaned, "tool_choice")
	}
	if !caps.SupportsThinking {
		delete(cleaned, "thinking")
	}
	if !caps.SupportsResponsesInput {
		delete(cleaned, "input")
	}
	if !caps.SupportsContextHistoryRule {
		cleaned = stripContextHistoryRules(cleaned)
	}

	dropEmptyStrings(cleaned,
		"previous_response_id",
		"instructions",
		"byok_system_prompt",
	)
	return cleaned
}

func stripContextHistoryRules(payload map[string]interface{}) map[string]interface{} {
	if payload == nil {
		return nil
	}
	cleaned := cloneMap(payload)
	if instructions, ok := cleaned["instructions"].(string); ok {
		if next := removeContextHistoryRuleText(instructions); strings.TrimSpace(next) == "" {
			delete(cleaned, "instructions")
		} else {
			cleaned["instructions"] = next
		}
	}
	return cleaned
}

func removeContextHistoryRuleText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(lower, "context-history") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func cloneMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	out := make(map[string]interface{}, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func dropEmptyStrings(payload map[string]interface{}, keys ...string) {
	for _, key := range keys {
		if v, ok := payload[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
				delete(payload, key)
			}
		}
	}
}
