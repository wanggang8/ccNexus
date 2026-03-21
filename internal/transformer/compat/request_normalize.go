package compat

func NormalizeOpenAIChatCompat(data map[string]interface{}) Audit {
	var summary []string

	if tools, changed := normalizeLegacyFunctionsToTools(data); changed {
		data["tools"] = tools
		delete(data, "functions")
		summary = append(summary, "functions->tools")
	}

	if toolChoice, changed := normalizeLegacyFunctionCallToToolChoice(data); changed {
		data["tool_choice"] = toolChoice
		delete(data, "function_call")
		summary = append(summary, "function_call->tool_choice")
	}

	if _, ok := data["reasoningContent"]; ok {
		NormalizeReasoningAlias(data)
		summary = append(summary, "reasoningContent_alias")
	}

	return Audit{Changed: len(summary) > 0, Reason: "openai_chat_normalize", Summary: summary}
}

func NormalizeReasoningAlias(data map[string]interface{}) {
	if _, ok := data["reasoning_content"]; ok {
		delete(data, "reasoningContent")
		return
	}
	if value, ok := data["reasoningContent"]; ok {
		data["reasoning_content"] = value
		delete(data, "reasoningContent")
	}
}

func normalizeLegacyFunctionsToTools(data map[string]interface{}) ([]interface{}, bool) {
	legacy, ok := data["functions"].([]interface{})
	if !ok || len(legacy) == 0 {
		return nil, false
	}

	if existing, ok := data["tools"].([]interface{}); ok && len(existing) > 0 {
		delete(data, "functions")
		return existing, false
	}

	tools := make([]interface{}, 0, len(legacy))
	for _, item := range legacy {
		fn, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		parameters := fn["parameters"]
		if parameters == nil {
			parameters = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        fn["name"],
				"description": fn["description"],
				"parameters":  parameters,
			},
		})
	}
	if len(tools) == 0 {
		return nil, false
	}
	return tools, true
}

func normalizeLegacyFunctionCallToToolChoice(data map[string]interface{}) (interface{}, bool) {
	legacy, ok := data["function_call"]
	if !ok || legacy == nil {
		return nil, false
	}

	if existing, ok := data["tool_choice"]; ok && existing != nil {
		return nil, false
	}

	switch value := legacy.(type) {
	case string:
		switch value {
		case "none", "auto", "required":
			return value, true
		default:
			if value == "" {
				return nil, false
			}
			return map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": value}}, true
		}
	case map[string]interface{}:
		name, _ := value["name"].(string)
		if name == "" {
			return nil, false
		}
		return map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": name}}, true
	default:
		return nil, false
	}
}
