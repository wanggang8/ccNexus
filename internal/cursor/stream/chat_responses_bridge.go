package stream

import "bytes"

// BridgeChatFromResponsesBundle rewrites native OpenAI Responses SSE events into
// Chat Completions chunks using api2cursor-style event timing. This is used only
// for Cursor chat -> openai2 bridging so ordinary /v1/chat/completions traffic
// can keep the existing shared converter behavior.
func BridgeChatFromResponsesBundle(bundle []byte, clientModel string, state *FinalizeState) ([]byte, error) {
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

		for _, item := range formatChatFromResponsesEvent(eventName, payload, clientModel, state) {
			writeSSEChunk(&output, item.eventName, item.payload)
		}
	}

	return output.Bytes(), nil
}

func formatChatFromResponsesEvent(eventName string, payload map[string]interface{}, clientModel string, state *FinalizeState) []sseItem {
	if eventName == "" {
		eventName = stringValue(payload["type"])
	}
	rememberChatResponsesUsage(state, payload)

	switch eventName {
	case "response.created":
		response, ok := payload["response"].(map[string]interface{})
		if !ok {
			response = payload
		}
		id := stringValue(response["id"])
		if state != nil {
			state.OpenAI2ChatStarted = true
		}
		return []sseItem{{
			payload: buildChatChunkPayload(firstNonEmptyChatID(id, state), clientModel, map[string]interface{}{
				"role":    "assistant",
				"content": "",
			}, nil, nil),
		}}

	case "response.output_text.delta":
		return []sseItem{{
			payload: buildChatChunkPayload(firstNonEmptyChatID("", state), clientModel, map[string]interface{}{
				"content": stringValue(payload["delta"]),
			}, nil, nil),
		}}

	case "response.reasoning_summary_text.delta":
		return []sseItem{{
			payload: buildChatChunkPayload(firstNonEmptyChatID("", state), clientModel, map[string]interface{}{
				"reasoning_content": stringValue(payload["delta"]),
			}, nil, nil),
		}}

	case "response.output_item.added":
		item, ok := payload["item"].(map[string]interface{})
		if !ok || stringValue(item["type"]) != "function_call" {
			return nil
		}
		callID := firstNonEmptyString(
			stringValue(item["call_id"]),
			stringValue(item["id"]),
			newToolCallID(),
		)
		toolIndex := rememberChatToolSlot(state, callID, responsesOutputIndex(payload))
		name := stringValue(item["name"])
		return []sseItem{{
			payload: buildChatChunkPayload(firstNonEmptyChatID("", state), clientModel, map[string]interface{}{
				"tool_calls": []interface{}{
					map[string]interface{}{
						"index": toolIndex,
						"id":    callID,
						"type":  "function",
						"function": map[string]interface{}{
							"name":      name,
							"arguments": "",
						},
					},
				},
			}, nil, nil),
		}}

	case "response.function_call_arguments.delta":
		toolIndex := lookupChatToolSlot(state, "", responsesOutputIndex(payload))
		return []sseItem{{
			payload: buildChatChunkPayload(firstNonEmptyChatID("", state), clientModel, map[string]interface{}{
				"tool_calls": []interface{}{
					map[string]interface{}{
						"index": toolIndex,
						"function": map[string]interface{}{
							"arguments": stringValue(payload["delta"]),
						},
					},
				},
			}, nil, nil),
		}}

	case "response.completed":
		response, ok := payload["response"].(map[string]interface{})
		if !ok {
			response = payload
		}
		finishReason := "stop"
		if responseHasFunctionCallOutput(response) || (state != nil && state.OpenAI2ChatSawToolCall) {
			finishReason = "tool_calls"
		}

		usage := buildChatUsageFromResponsesUsage(extractResponsesUsageMap(response))
		if usage == nil && state != nil {
			usage = buildChatUsageFromResponsesUsage(state.OpenAI2ChatLastUsage)
		}

		return []sseItem{{
			payload: buildChatChunkPayload(firstNonEmptyChatID(stringValue(response["id"]), state), clientModel, map[string]interface{}{}, finishReason, usage),
		}}
	}

	return nil
}

func rememberChatResponsesUsage(state *FinalizeState, payload map[string]interface{}) {
	if state == nil {
		return
	}
	if usage := extractResponsesUsageMap(payload); usage != nil {
		state.OpenAI2ChatLastUsage = usage
	}
}

func extractResponsesUsageMap(payload map[string]interface{}) map[string]interface{} {
	if payload == nil {
		return nil
	}
	if rawUsage, ok := payload["usage"].(map[string]interface{}); ok && len(rawUsage) > 0 {
		return cloneJSONObject(rawUsage)
	}
	if response, ok := payload["response"].(map[string]interface{}); ok {
		if rawUsage, ok := response["usage"].(map[string]interface{}); ok && len(rawUsage) > 0 {
			return cloneJSONObject(rawUsage)
		}
	}
	return nil
}

func buildChatUsageFromResponsesUsage(rawUsage map[string]interface{}) map[string]interface{} {
	if len(rawUsage) == 0 {
		return nil
	}
	total := rawUsage["total_tokens"]
	if total == nil {
		total = addUsageCounts(rawUsage["input_tokens"], rawUsage["output_tokens"])
	}
	return map[string]interface{}{
		"prompt_tokens":     rawUsage["input_tokens"],
		"completion_tokens": rawUsage["output_tokens"],
		"total_tokens":      total,
	}
}

func buildChatChunkPayload(id, model string, delta map[string]interface{}, finishReason interface{}, usage map[string]interface{}) map[string]interface{} {
	choice := map[string]interface{}{
		"index": 0,
		"delta": delta,
	}
	if finishReason != nil {
		choice["finish_reason"] = finishReason
	} else {
		choice["finish_reason"] = nil
	}

	payload := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"model":   model,
		"choices": []interface{}{choice},
	}
	if usage != nil {
		payload["usage"] = usage
	}
	return payload
}

func rememberChatToolSlot(state *FinalizeState, callID string, outputIndex int) int {
	if state == nil {
		if callID != "" {
			return 0
		}
		if outputIndex >= 0 {
			return outputIndex
		}
		return 0
	}
	if callID != "" {
		if state.OpenAI2CallIDToSlot == nil {
			state.OpenAI2CallIDToSlot = make(map[string]int)
		}
		if slot, ok := state.OpenAI2CallIDToSlot[callID]; ok {
			if outputIndex >= 0 {
				if state.OpenAI2ChatToolSlots == nil {
					state.OpenAI2ChatToolSlots = make(map[int]int)
				}
				state.OpenAI2ChatToolSlots[outputIndex] = slot
			}
			return slot
		}
		slot := state.OpenAI2ChatNextToolSlot
		state.OpenAI2ChatNextToolSlot++
		state.OpenAI2CallIDToSlot[callID] = slot
		if outputIndex >= 0 {
			if state.OpenAI2ChatToolSlots == nil {
				state.OpenAI2ChatToolSlots = make(map[int]int)
			}
			state.OpenAI2ChatToolSlots[outputIndex] = slot
		}
		state.OpenAI2ChatSawToolCall = true
		return slot
	}
	if state.OpenAI2ChatToolSlots == nil {
		state.OpenAI2ChatToolSlots = make(map[int]int)
	}
	if outputIndex >= 0 {
		if slot, ok := state.OpenAI2ChatToolSlots[outputIndex]; ok {
			return slot
		}
	}
	slot := state.OpenAI2ChatNextToolSlot
	state.OpenAI2ChatNextToolSlot++
	if outputIndex >= 0 {
		state.OpenAI2ChatToolSlots[outputIndex] = slot
	}
	state.OpenAI2ChatSawToolCall = true
	return slot
}

func lookupChatToolSlot(state *FinalizeState, callID string, outputIndex int) int {
	if state == nil {
		if outputIndex >= 0 {
			return outputIndex
		}
		return 0
	}
	if callID != "" && state.OpenAI2CallIDToSlot != nil {
		if slot, ok := state.OpenAI2CallIDToSlot[callID]; ok {
			return slot
		}
	}
	if state.OpenAI2ChatToolSlots == nil {
		state.OpenAI2ChatToolSlots = make(map[int]int)
	}
	if outputIndex >= 0 {
		if slot, ok := state.OpenAI2ChatToolSlots[outputIndex]; ok {
			return slot
		}
	}
	return rememberChatToolSlot(state, callID, outputIndex)
}

func responseHasFunctionCallOutput(response map[string]interface{}) bool {
	if response == nil {
		return false
	}
	output, ok := response["output"].([]interface{})
	if !ok {
		return false
	}
	for _, rawItem := range output {
		item, ok := rawItem.(map[string]interface{})
		if ok && stringValue(item["type"]) == "function_call" {
			return true
		}
	}
	return false
}

func addUsageCounts(left, right interface{}) int {
	return intValue(left) + intValue(right)
}

func intValue(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return 0
	}
}

func firstNonEmptyChatID(candidate string, state *FinalizeState) string {
	if state != nil && state.OpenAI2ChatResponseID != "" {
		return state.OpenAI2ChatResponseID
	}
	id := candidate
	if id == "" || bytes.HasPrefix([]byte(id), []byte("resp_")) {
		id = newChatCompletionID()
	}
	if state != nil {
		state.OpenAI2ChatResponseID = id
	}
	return id
}
