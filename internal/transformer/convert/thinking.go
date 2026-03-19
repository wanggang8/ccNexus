package convert

import (
	"strings"
)

func buildClaudeThinkingConfig(rawThinking interface{}, enableThinking bool, maxTokens int) map[string]interface{} {
	switch v := rawThinking.(type) {
	case nil:
	case bool:
		if v {
			return enabledClaudeThinkingConfig(maxTokens)
		}
		return nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "enabled", "adaptive", "true", "on":
			return enabledClaudeThinkingConfig(maxTokens)
		case "disabled", "off", "false", "":
			return nil
		}
	case map[string]interface{}:
		thinkingType, _ := v["type"].(string)
		switch strings.ToLower(strings.TrimSpace(thinkingType)) {
		case "enabled", "adaptive":
			return normalizeClaudeThinkingConfig(v, maxTokens)
		case "disabled", "off", "false":
			return nil
		}
	}

	if enableThinking {
		return enabledClaudeThinkingConfig(maxTokens)
	}
	return nil
}

func enabledClaudeThinkingConfig(maxTokens int) map[string]interface{} {
	budgetTokens := 10000
	if maxTokens > 0 && budgetTokens >= maxTokens {
		budgetTokens = maxTokens - 1
	}
	if budgetTokens <= 0 {
		return nil
	}
	return map[string]interface{}{
		"type":          "enabled",
		"budget_tokens": budgetTokens,
	}
}

func normalizeClaudeThinkingConfig(cfg map[string]interface{}, maxTokens int) map[string]interface{} {
	if len(cfg) == 0 {
		return nil
	}

	out := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}

	thinkingType, _ := out["type"].(string)
	switch strings.ToLower(strings.TrimSpace(thinkingType)) {
	case "adaptive":
		out["type"] = "adaptive"
		return out
	default:
		out["type"] = "enabled"
	}

	budgetTokens := thinkingBudgetFromConfig(out)
	if budgetTokens <= 0 {
		budgetTokens = 10000
	}
	if maxTokens > 0 && budgetTokens >= maxTokens {
		budgetTokens = maxTokens - 1
	}
	if budgetTokens <= 0 {
		return nil
	}
	out["budget_tokens"] = budgetTokens
	return out
}

func thinkingBudgetFromConfig(cfg map[string]interface{}) int {
	if cfg == nil {
		return 0
	}
	switch v := cfg["budget_tokens"].(type) {
	case float64:
		return int(v)
	case float32:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case uint:
		return int(v)
	case uint64:
		return int(v)
	case uint32:
		return int(v)
	}
	return 0
}
