package augment

import "strings"

const defaultClaudeThinkingBudgetTokens = 10000

// buildClaudeThinkingConfig normalizes request-side thinking settings into the
// Claude Messages API format.
//
// Precedence:
// 1. Explicit thinking payload from the request, if present.
// 2. `enable_thinking=true`, if present.
// 3. Model-based fallback for CLI compatibility, if enabled by caller.
func buildClaudeThinkingConfig(raw interface{}, enableThinking bool, model string, maxTokens int, autoEnableByModel bool) map[string]interface{} {
	if cfg := normalizeClaudeThinkingConfig(raw, maxTokens); cfg != nil {
		return cfg
	}
	if enableThinking {
		return enabledClaudeThinkingConfig(maxTokens)
	}
	if autoEnableByModel && supportsThinking(model) {
		return enabledClaudeThinkingConfig(maxTokens)
	}
	return nil
}

func normalizeClaudeThinkingConfig(raw interface{}, maxTokens int) map[string]interface{} {
	if raw == nil {
		return nil
	}

	switch v := raw.(type) {
	case bool:
		if !v {
			return nil
		}
		return enabledClaudeThinkingConfig(maxTokens)
	case map[string]interface{}:
		if len(v) == 0 {
			return nil
		}
		cfg := make(map[string]interface{}, len(v))
		for k, val := range v {
			cfg[k] = val
		}

		thinkingType, _ := cfg["type"].(string)
		switch strings.ToLower(strings.TrimSpace(thinkingType)) {
		case "", "enabled":
			cfg["type"] = "enabled"
			budget := configuredThinkingBudget(cfg["budget_tokens"], maxTokens)
			if budget <= 0 {
				return nil
			}
			cfg["budget_tokens"] = budget
			return cfg
		case "adaptive":
			return cfg
		case "disabled":
			return nil
		default:
			return cfg
		}
	default:
		return enabledClaudeThinkingConfig(maxTokens)
	}
}

func enabledClaudeThinkingConfig(maxTokens int) map[string]interface{} {
	budget := configuredThinkingBudget(defaultClaudeThinkingBudgetTokens, maxTokens)
	if budget <= 0 {
		return nil
	}
	return map[string]interface{}{
		"type":          "enabled",
		"budget_tokens": budget,
	}
}

func configuredThinkingBudget(raw interface{}, maxTokens int) int {
	budget, ok := toTokenInt(raw)
	if !ok {
		budget = defaultClaudeThinkingBudgetTokens
	}

	if maxTokens > 0 {
		if maxTokens <= 1024 {
			return 0
		}
		if budget >= maxTokens {
			budget = maxTokens - 1
		}
	}

	if budget < 1024 {
		budget = 1024
	}

	if maxTokens > 0 && budget >= maxTokens {
		return 0
	}

	return budget
}
