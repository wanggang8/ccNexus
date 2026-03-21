package compat

// ApplyOpenAIFieldPolicy keeps request fields that should survive chat-entry compat routing.
func ApplyOpenAIFieldPolicy(dst, src map[string]interface{}) Audit {
	summary := CopyIfPresent(dst, src,
		"metadata",
		"stream_options",
		"user",
	)
	if thinking, ok := src["thinking"]; ok {
		dst["thinking"] = thinking
		summary = append(summary, "thinking")
	}
	if enableThinking, ok := src["enable_thinking"]; ok {
		dst["enable_thinking"] = enableThinking
		summary = append(summary, "enable_thinking")
	}
	if budgetTokens, ok := src["budget_tokens"]; ok {
		dst["budget_tokens"] = budgetTokens
		summary = append(summary, "budget_tokens")
	}
	DeleteIfPresent(dst, "include", "store", "reasoning", "reasoning_content")
	if effort := ExtractResponsesReasoningEffort(src); effort != nil {
		dst["reasoning_effort"] = effort
		summary = append(summary, "reasoning_effort")
	}
	return Audit{Changed: len(summary) > 0, Reason: "openai_field_policy", Summary: summary}
}

// ApplyClaudeFieldPolicy keeps request fields that should survive chat-entry compat routing.
func ApplyClaudeFieldPolicy(dst, src map[string]interface{}) Audit {
	summary := CopyIfPresent(dst, src, "metadata")
	DeleteIfPresent(dst, "stream_options", "include", "user", "store", "reasoning", "reasoning_effort", "enable_thinking", "budget_tokens")
	if _, ok := dst["system"]; !ok {
		if system := ExtractResponsesSystem(src); system != "" {
			dst["system"] = system
			summary = append(summary, "system")
		}
	}
	if _, ok := dst["thinking"]; !ok {
		if thinking := BuildClaudeThinkingFromResponses(src); thinking != nil {
			dst["thinking"] = thinking
			summary = append(summary, "thinking")
		}
	}
	return Audit{Changed: len(summary) > 0, Reason: "claude_field_policy", Summary: summary}
}

// ApplyResponsesFieldPolicy keeps request fields that should survive chat-entry compat routing.
func ApplyResponsesFieldPolicy(dst, src map[string]interface{}) Audit {
	summary := CopyIfPresent(dst, src,
		"metadata",
		"include",
		"user",
		"reasoning",
		"store",
	)
	if effort, ok := src["reasoning_effort"]; ok {
		dst["reasoning_effort"] = effort
		summary = append(summary, "reasoning_effort")
	}
	if thinking, ok := src["thinking"]; ok {
		dst["thinking"] = thinking
		summary = append(summary, "thinking")
	}
	if enableThinking, ok := src["enable_thinking"]; ok {
		dst["enable_thinking"] = enableThinking
		summary = append(summary, "enable_thinking")
	}
	if budgetTokens, ok := src["budget_tokens"]; ok {
		dst["budget_tokens"] = budgetTokens
		summary = append(summary, "budget_tokens")
	}
	DeleteIfPresent(dst, "stream_options")
	return Audit{Changed: len(summary) > 0, Reason: "responses_field_policy", Summary: summary}
}
