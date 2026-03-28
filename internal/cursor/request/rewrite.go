package request

import (
	"encoding/json"

	cursorcache "github.com/lich0821/ccNexus/internal/cursor/cache"
	"github.com/lich0821/ccNexus/internal/cursor/shared"
	"github.com/lich0821/ccNexus/internal/transformer/convert"
)

// ApplyStatelessTransformedCompat applies the request-side compat rules that do
// not require Cursor thinking-cache state. Cache-driven rewrites will be added
// in a later stage under the dedicated cache/rewrite layer.
func ApplyStatelessTransformedCompat(body []byte, meta shared.RequestMeta, transformerName string) ([]byte, error) {
	if !meta.CursorMode {
		return body, nil
	}

	switch meta.ClientFormat {
	case shared.ClientFormatClaude:
		// Align with api2cursor messages route behavior: keep request passthrough.
		return body, nil
	case shared.ClientFormatOpenAIChat:
		switch transformerName {
		case "cx_chat_claude":
			return applyClaudeCompat(body, meta, transformerName)
		case "cx_chat_openai2":
			return NormalizeOpenAI2EasyInputMessages(body)
		case "cx_chat_gemini":
			return NormalizeGeminiFunctionParts(body)
		default:
			return body, nil
		}
	}

	switch transformerName {
	case "cx_resp_openai2":
		return body, nil
	case "cx_resp_openai", "cx_resp_cli":
		return NormalizeOpenAIChatBody(body), nil
	case "cx_resp_claude":
		return applyClaudeCompat(body, meta, transformerName)
	case "cx_resp_gemini":
		return NormalizeGeminiFunctionParts(body)
	default:
		return body, nil
	}
}

func ExtractCacheMessages(body []byte, meta shared.RequestMeta) []map[string]interface{} {
	if !meta.CursorMode {
		return nil
	}
	switch meta.ClientFormat {
	case shared.ClientFormatOpenAIChat:
		return extractChatCacheMessages(body)
	case shared.ClientFormatOpenAIResponses:
		return extractResponsesCacheMessages(body, meta.ClientModel)
	default:
		return nil
	}
}

func ApplyPreparedCache(body []byte, meta shared.RequestMeta, cacheMessages []map[string]interface{}, thinkingCache *cursorcache.ThinkingCache) ([]byte, []map[string]interface{}, error) {
	if !meta.CursorMode || meta.ClientFormat != shared.ClientFormatOpenAIChat || len(cacheMessages) == 0 || thinkingCache == nil {
		return body, cacheMessages, nil
	}

	injected := thinkingCache.Inject(cacheMessages)
	rewritten, err := rewriteChatMessages(body, injected)
	if err != nil {
		return body, injected, nil
	}
	return rewritten, injected, nil
}

func ApplyTransformedCompat(body []byte, meta shared.RequestMeta, transformerName string, cacheMessages []map[string]interface{}, thinkingCache *cursorcache.ThinkingCache) ([]byte, []map[string]interface{}, error) {
	if !meta.CursorMode {
		return body, cacheMessages, nil
	}
	if meta.ClientFormat == shared.ClientFormatOpenAIChat {
		return applyChatTransformedCompat(body, meta, transformerName, cacheMessages, thinkingCache)
	}
	if meta.ClientFormat != shared.ClientFormatOpenAIResponses {
		updated, err := ApplyStatelessTransformedCompat(body, meta, transformerName)
		return updated, cacheMessages, err
	}
	if transformerName == "cx_resp_openai2" {
		return body, cacheMessages, nil
	}

	injected := cacheMessages
	if len(cacheMessages) > 0 && thinkingCache != nil {
		injected = thinkingCache.Inject(cacheMessages)
	}

	switch transformerName {
	case "cx_resp_openai":
		rewritten := body
		if len(injected) > 0 {
			var err error
			rewritten, err = rewriteChatMessages(body, injected)
			if err != nil {
				return nil, injected, err
			}
		}
		return NormalizeOpenAIChatBody(rewritten), injected, nil
	case "cx_resp_cli":
		return NormalizeOpenAIChatBody(body), injected, nil
	case "cx_resp_claude":
		rewritten := body
		if len(injected) > 0 {
			var err error
			rewritten, err = rewriteClaudeMessages(body, injected)
			if err != nil {
				return nil, injected, err
			}
		}
		updated, err := applyClaudeCompat(rewritten, meta, transformerName)
		return updated, injected, err
	case "cx_resp_gemini":
		updated := body
		if len(injected) > 0 {
			var err error
			updated, err = rewriteGeminiContents(body, injected)
			if err != nil {
				return nil, injected, err
			}
		}
		updated, err := NormalizeGeminiFunctionParts(updated)
		return updated, injected, err
	default:
		return body, injected, nil
	}
}

func applyChatTransformedCompat(body []byte, meta shared.RequestMeta, transformerName string, cacheMessages []map[string]interface{}, thinkingCache *cursorcache.ThinkingCache) ([]byte, []map[string]interface{}, error) {
	injected := cacheMessages
	if transformerName != "cx_chat_openai2" && len(cacheMessages) > 0 && thinkingCache != nil {
		injected = thinkingCache.Inject(cacheMessages)
	}

	rewritten := body
	switch transformerName {
	case "cx_chat_openai":
		if len(injected) > 0 {
			var err error
			rewritten, err = rewriteChatMessages(body, injected)
			if err != nil {
				return nil, injected, err
			}
		}
	case "cx_chat_claude":
		if len(injected) > 0 {
			var err error
			rewritten, err = rewriteClaudeMessages(body, injected)
			if err != nil {
				return nil, injected, err
			}
		}
	case "cx_chat_gemini":
		if len(injected) > 0 {
			var err error
			rewritten, err = rewriteGeminiContents(body, injected)
			if err != nil {
				return nil, injected, err
			}
		}
	}

	updated, err := ApplyStatelessTransformedCompat(rewritten, meta, transformerName)
	return updated, injected, err
}

func applyClaudeCompat(payload []byte, meta shared.RequestMeta, transformerName string) ([]byte, error) {
	updated := payload
	updated = EnsureClaudeToolSchemas(updated)
	updated = EnsureClaudeToolUseInputs(updated)
	if NeedClaudeMaxTokensFloor(meta, transformerName) {
		updated = EnsureClaudeMaxTokensFloor(updated)
	}
	if !NeedClaudeCacheControl(meta, transformerName) {
		return updated, nil
	}
	return convert.OptimizeAnthropicCacheControl(updated)
}

func extractChatCacheMessages(body []byte) []map[string]interface{} {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return nil
	}
	rawMessages, ok := payload["messages"].([]interface{})
	if !ok || len(rawMessages) == 0 {
		return nil
	}
	messages := make([]map[string]interface{}, 0, len(rawMessages))
	for _, rawMessage := range rawMessages {
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			continue
		}
		messages = append(messages, cloneJSONObject(message))
	}
	return messages
}

func rewriteChatMessages(body []byte, messages []map[string]interface{}) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}
	rewritten := make([]interface{}, 0, len(messages))
	for _, message := range messages {
		rewritten = append(rewritten, cloneJSONObject(message))
	}
	payload["messages"] = rewritten
	return json.Marshal(payload)
}

func extractResponsesCacheMessages(body []byte, model string) []map[string]interface{} {
	converted, err := convert.OpenAI2ReqToOpenAI(body, model)
	if err != nil {
		return nil
	}
	return extractChatCacheMessages(converted)
}

func rewriteResponsesMessages(body []byte, messages []map[string]interface{}, model string) ([]byte, error) {
	converted, err := convert.OpenAI2ReqToOpenAI(body, model)
	if err != nil {
		return body, err
	}
	rewrittenChat, err := rewriteChatMessages(converted, messages)
	if err != nil {
		return body, err
	}
	rewrittenResponses, err := convert.OpenAIReqToOpenAI2(rewrittenChat, model)
	if err != nil {
		return body, err
	}
	return rewrittenResponses, nil
}

func rewriteClaudeMessages(body []byte, cacheMessages []map[string]interface{}) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}
	rawMessages, ok := payload["messages"].([]interface{})
	if !ok || len(rawMessages) == 0 {
		return body, nil
	}

	assistantReasoning := extractAssistantReasoning(cacheMessages)
	if len(assistantReasoning) == 0 {
		return body, nil
	}

	reasoningIndex := 0
	rewrittenMessages := make([]interface{}, 0, len(rawMessages))
	for _, rawMessage := range rawMessages {
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			rewrittenMessages = append(rewrittenMessages, rawMessage)
			continue
		}
		cloned := cloneJSONObject(message)
		if stringValue(cloned["role"]) == "assistant" && reasoningIndex < len(assistantReasoning) {
			cursorcache.InjectClaudeThinking(cloned, assistantReasoning[reasoningIndex])
			reasoningIndex++
		}
		rewrittenMessages = append(rewrittenMessages, cloned)
	}
	payload["messages"] = rewrittenMessages
	return json.Marshal(payload)
}

func rewriteGeminiContents(body []byte, cacheMessages []map[string]interface{}) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}
	rawContents, ok := payload["contents"].([]interface{})
	if !ok || len(rawContents) == 0 {
		return body, nil
	}

	assistantReasoning := extractAssistantReasoning(cacheMessages)
	if len(assistantReasoning) == 0 {
		return body, nil
	}

	reasoningIndex := 0
	rewrittenContents := make([]interface{}, 0, len(rawContents))
	for _, rawContent := range rawContents {
		content, ok := rawContent.(map[string]interface{})
		if !ok {
			rewrittenContents = append(rewrittenContents, rawContent)
			continue
		}
		cloned := cloneJSONObject(content)
		if stringValue(cloned["role"]) == "model" && reasoningIndex < len(assistantReasoning) {
			cursorcache.InjectGeminiThought(cloned, assistantReasoning[reasoningIndex])
			reasoningIndex++
		}
		rewrittenContents = append(rewrittenContents, cloned)
	}
	payload["contents"] = rewrittenContents
	return json.Marshal(payload)
}

func extractAssistantReasoning(messages []map[string]interface{}) []string {
	reasoning := make([]string, 0)
	for _, message := range messages {
		if stringValue(message["role"]) != "assistant" {
			continue
		}
		value := stringValue(message["reasoning_content"])
		if value == "" {
			continue
		}
		reasoning = append(reasoning, value)
	}
	return reasoning
}

func cloneJSONObject(payload map[string]interface{}) map[string]interface{} {
	if payload == nil {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		cloned := make(map[string]interface{}, len(payload))
		for key, value := range payload {
			cloned[key] = value
		}
		return cloned
	}
	var cloned map[string]interface{}
	if err := json.Unmarshal(raw, &cloned); err != nil {
		cloned = make(map[string]interface{}, len(payload))
		for key, value := range payload {
			cloned[key] = value
		}
	}
	return cloned
}
