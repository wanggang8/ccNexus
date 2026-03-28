package stream

import "bytes"

func FixMessagesBundle(bundle []byte, state *FinalizeState) ([]byte, error) {
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

		for _, item := range formatMessagesStreamEvent(eventName, payload, state) {
			writeSSEChunk(&output, item.eventName, item.payload)
		}
	}

	return output.Bytes(), nil
}

func formatMessagesStreamEvent(eventName string, payload map[string]interface{}, state *FinalizeState) []sseItem {
	if state == nil {
		return []sseItem{{eventName: eventName, payload: payload}}
	}

	modified := cloneJSONObject(payload)
	reasoning := ""
	for _, key := range []string{"message", "delta"} {
		container, ok := modified[key].(map[string]interface{})
		if !ok {
			continue
		}
		if rc := stringValue(container["reasoning_content"]); rc != "" {
			reasoning += rc
			delete(container, "reasoning_content")
		}
		if rc := stringValue(container["reasoningContent"]); rc != "" {
			reasoning += rc
			delete(container, "reasoningContent")
		}
	}
	if reasoning != "" {
		state.MessagesReasoningBuf += reasoning
	}

	results := make([]sseItem, 0)
	if state.MessagesReasoningBuf != "" && !state.MessagesThinkingShown && isMessagesTextDelta(modified) {
		state.MessagesThinkingShown = true
		state.MessagesIndexOffset = 1
		results = append(results, emitMessagesThinking(state.MessagesReasoningBuf)...)
		state.MessagesReasoningBuf = ""
	}

	if state.MessagesIndexOffset > 0 {
		if index, ok := modified["index"].(float64); ok {
			modified["index"] = index + float64(state.MessagesIndexOffset)
		}
	}

	results = append(results, sseItem{eventName: eventName, payload: modified})
	return results
}

func isMessagesTextDelta(payload map[string]interface{}) bool {
	delta, ok := payload["delta"].(map[string]interface{})
	if !ok {
		return false
	}
	if stringValue(delta["type"]) != "text_delta" {
		return false
	}
	return stringValue(delta["text"]) != ""
}

func emitMessagesThinking(text string) []sseItem {
	if text == "" {
		return nil
	}
	return []sseItem{
		{
			eventName: "content_block_start",
			payload: map[string]interface{}{
				"type":          "content_block_start",
				"index":         0,
				"content_block": map[string]interface{}{"type": "thinking", "thinking": ""},
			},
		},
		{
			eventName: "content_block_delta",
			payload: map[string]interface{}{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]interface{}{
					"type":     "thinking_delta",
					"thinking": text,
				},
			},
		},
		{
			eventName: "content_block_stop",
			payload: map[string]interface{}{
				"type":  "content_block_stop",
				"index": 0,
			},
		},
	}
}
