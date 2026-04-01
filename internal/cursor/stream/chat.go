package stream

import (
	"bytes"
	"strings"
)

func FixChatBundle(bundle []byte, clientModel string, state *FinalizeState) ([]byte, error) {
	chunks := splitSSEBundle(bundle)
	if len(chunks) == 0 {
		return bundle, nil
	}

	var output bytes.Buffer
	for _, chunk := range chunks {
		eventName, data, ok := parseSSEChunk(chunk)
		if !ok {
			output.Write(chunk)
			if !bytes.HasSuffix(chunk, []byte("\n\n")) {
				output.WriteString("\n\n")
			}
			continue
		}
		if data == "[DONE]" {
			if closeChunk := finalizeChatThinkingChunk(state, clientModel); len(closeChunk) > 0 {
				output.Write(closeChunk)
			}
			output.WriteString("data: [DONE]\n\n")
			continue
		}

		payload, ok := decodeJSONObject([]byte(data))
		if !ok {
			output.Write(chunk)
			if !bytes.HasSuffix(chunk, []byte("\n\n")) {
				output.WriteString("\n\n")
			}
			continue
		}

		for _, item := range formatChatStreamEvent(eventName, payload, clientModel, state) {
			writeSSEChunk(&output, item.eventName, item.payload)
		}
	}

	return output.Bytes(), nil
}

func formatChatStreamEvent(eventName string, payload map[string]interface{}, clientModel string, state *FinalizeState) []sseItem {
	payload = fixChatChunkPayload(payload, clientModel)
	if state == nil {
		return []sseItem{{eventName: eventName, payload: payload}}
	}

	choices, ok := payload["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return []sseItem{{eventName: eventName, payload: payload}}
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return []sseItem{{eventName: eventName, payload: payload}}
	}
	delta, ok := choice["delta"].(map[string]interface{})
	if !ok {
		return []sseItem{{eventName: eventName, payload: payload}}
	}

	results := make([]sseItem, 0)
	finishReason := choice["finish_reason"]
	if payload["usage"] != nil {
		state.ChatUsageSeen = true
	}
	toolCalls, hasToolCalls := delta["tool_calls"].([]interface{})
	content, hasContent := delta["content"].(string)
	reasoning, hasReasoning := delta["reasoning_content"].(string)

	if hasContent && content != "" {
		results = append(results, splitChatContentIntoItems(payload, eventName, content, finishReason, hasToolCalls, state)...)
		delete(delta, "content")
	}
	if hasReasoning && reasoning != "" {
		results = append(results, sseItem{eventName: eventName, payload: cloneChatChunk(payload, map[string]interface{}{
			"reasoning_content": reasoning,
		}, nil)})
		delete(delta, "reasoning_content")
	}
	if hasToolCalls && len(toolCalls) > 0 {
		if !state.ChatToolCallsSeen {
			state.ChatToolCallsSeen = true
			if state.InThinkingTag {
				results = append(results, sseItem{eventName: eventName, payload: cloneChatChunk(payload, map[string]interface{}{
					"content": "\n</think>\n\n",
				}, nil)})
				state.InThinkingTag = false
			} else if hasContent {
				results = append(results, sseItem{eventName: eventName, payload: cloneChatChunk(payload, map[string]interface{}{
					"content": "\n",
				}, nil)})
			}
		} else if state.InThinkingTag {
			results = append(results, sseItem{eventName: eventName, payload: cloneChatChunk(payload, map[string]interface{}{
				"content": "\n</think>\n\n",
			}, nil)})
			state.InThinkingTag = false
		}
		results = append(results, sseItem{eventName: eventName, payload: cloneChatChunk(payload, map[string]interface{}{
			"tool_calls": toolCalls,
		}, finishReason)})
		delete(delta, "tool_calls")
	}

	if len(delta) > 0 || finishReason != nil && len(results) == 0 {
		results = append(results, sseItem{eventName: eventName, payload: payload})
	}
	return results
}

func splitChatContentIntoItems(template map[string]interface{}, eventName, content string, finishReason interface{}, hasToolCalls bool, state *FinalizeState) []sseItem {
	if state == nil {
		return []sseItem{{eventName: eventName, payload: cloneChatChunk(template, map[string]interface{}{"content": content}, finishReason)}}
	}

	items := make([]sseItem, 0)
	appendContent := func(text string) {
		if text == "" {
			return
		}
		items = append(items, sseItem{
			eventName: eventName,
			payload:   cloneChatChunk(template, map[string]interface{}{"content": text}, nil),
		})
	}
	appendReasoning := func(text string) {
		if text == "" {
			return
		}
		items = append(items, sseItem{
			eventName: eventName,
			payload:   cloneChatChunk(template, map[string]interface{}{"reasoning_content": text}, nil),
		})
	}

	remaining := content
	for len(remaining) > 0 {
		if state.InThinkingTag {
			closeIdx := strings.Index(remaining, "</think>")
			if closeIdx == -1 {
				appendReasoning(remaining)
				remaining = ""
				break
			}
			appendReasoning(remaining[:closeIdx])
			remaining = strings.TrimLeft(remaining[closeIdx+len("</think>"):], "\n")
			state.InThinkingTag = false
			continue
		}

		openIdx := strings.Index(remaining, "<think>")
		if openIdx == -1 {
			appendContent(remaining)
			remaining = ""
			break
		}
		if openIdx > 0 {
			appendContent(remaining[:openIdx])
		}
		remaining = remaining[openIdx+len("<think>"):]
		closeIdx := strings.Index(remaining, "</think>")
		if closeIdx == -1 {
			state.InThinkingTag = true
			appendReasoning(remaining)
			remaining = ""
			break
		}
		appendReasoning(remaining[:closeIdx])
		remaining = strings.TrimLeft(remaining[closeIdx+len("</think>"):], "\n")
	}

	if len(items) == 0 {
		items = append(items, sseItem{
			eventName: eventName,
			payload:   cloneChatChunk(template, map[string]interface{}{"content": content}, nil),
		})
	}
	if finishReason != nil && !hasToolCalls {
		last := &items[len(items)-1]
		last.payload = cloneChatChunk(last.payload, extractDelta(last.payload), finishReason)
	}
	return items
}

func fixChatChunkPayload(payload map[string]interface{}, clientModel string) map[string]interface{} {
	if clientModel != "" {
		payload["model"] = clientModel
	}

	if choices, ok := payload["choices"].([]interface{}); ok {
		for _, choiceValue := range choices {
			choice, ok := choiceValue.(map[string]interface{})
			if !ok {
				continue
			}
			fixChatStreamChoice(choice)
		}
	}

	return payload
}

func fixChatStreamChoice(choice map[string]interface{}) {
	delta, ok := choice["delta"].(map[string]interface{})
	if !ok {
		rewriteFinishReason(choice)
		return
	}

	promoteReasoningField(delta)
	convertLegacyStreamFunctionCall(delta, choice)
	sanitizeToolCallDeltas(delta)
	ensureStreamToolCalls(delta, choice)
	rewriteFinishReason(choice)
}

func convertLegacyStreamFunctionCall(delta map[string]interface{}, choice map[string]interface{}) {
	if _, ok := delta["tool_calls"]; ok {
		return
	}
	functionCall, ok := delta["function_call"].(map[string]interface{})
	if !ok {
		return
	}

	toolCall := map[string]interface{}{
		"index":    0,
		"type":     "function",
		"function": map[string]interface{}{},
	}
	functionMap := toolCall["function"].(map[string]interface{})
	if name := stringValue(functionCall["name"]); name != "" {
		toolCall["id"] = newToolCallID()
		functionMap["name"] = name
	}
	if arguments := stringValue(functionCall["arguments"]); arguments != "" {
		functionMap["arguments"] = arguments
	}

	delta["tool_calls"] = []interface{}{toolCall}
	delete(delta, "function_call")
	rewriteFinishReason(choice)
}

func sanitizeToolCallDeltas(delta map[string]interface{}) {
	toolCalls, ok := delta["tool_calls"].([]interface{})
	if !ok {
		return
	}
	for _, toolCallValue := range toolCalls {
		toolCall, ok := toolCallValue.(map[string]interface{})
		if !ok {
			continue
		}
		if strings.TrimSpace(stringValue(toolCall["id"])) == "" {
			delete(toolCall, "id")
		}
		if strings.TrimSpace(stringValue(toolCall["type"])) == "" {
			delete(toolCall, "type")
		}
		functionData, ok := toolCall["function"].(map[string]interface{})
		if !ok {
			continue
		}
		if strings.TrimSpace(stringValue(functionData["name"])) == "" {
			delete(functionData, "name")
		}
	}
}

func ensureStreamToolCalls(delta map[string]interface{}, choice map[string]interface{}) {
	toolCalls, ok := delta["tool_calls"].([]interface{})
	if !ok {
		return
	}

	finishReason := strings.TrimSpace(firstNonEmptyString(
		stringValue(choice["finish_reason"]),
		stringValue(choice["finishReason"]),
	))

	for _, toolCallValue := range toolCalls {
		toolCall, ok := toolCallValue.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := toolCall["index"]; !ok {
			toolCall["index"] = 0
		}

		hasIdentity := toolCallHasResolvableIdentity(toolCall)
		hasID := strings.TrimSpace(stringValue(toolCall["id"])) != ""
		if !hasID && finishReason == "tool_calls" && hasIdentity {
			toolCall["id"] = newToolCallID()
			hasID = true
		}
		if hasID || hasIdentity {
			if strings.TrimSpace(stringValue(toolCall["type"])) == "" {
				toolCall["type"] = "function"
			}
		}
	}
}

func rewriteFinishReason(choice map[string]interface{}) {
	if stringValue(choice["finish_reason"]) == "function_call" {
		choice["finish_reason"] = "tool_calls"
	}
}

func promoteReasoningField(container map[string]interface{}) {
	if _, ok := container["reasoning_content"]; ok {
		return
	}
	if value, ok := container["reasoningContent"]; ok {
		container["reasoning_content"] = value
		delete(container, "reasoningContent")
	}
}

func cloneChatChunk(template map[string]interface{}, delta map[string]interface{}, finishReason interface{}) map[string]interface{} {
	cloned := cloneJSONObject(template)
	choices, ok := cloned["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return cloned
	}
	firstChoice, ok := choices[0].(map[string]interface{})
	if !ok {
		return cloned
	}
	newChoice := cloneJSONObject(firstChoice)
	newChoice["delta"] = delta
	newChoice["finish_reason"] = finishReason
	cloned["choices"] = []interface{}{newChoice}
	return cloned
}

func extractDelta(payload map[string]interface{}) map[string]interface{} {
	choices, ok := payload["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return map[string]interface{}{}
	}
	firstChoice, ok := choices[0].(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	delta, ok := firstChoice["delta"].(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	return delta
}
