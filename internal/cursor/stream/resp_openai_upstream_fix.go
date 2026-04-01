package stream

import "bytes"

// FixRespOpenAIUpstreamChatSSE applies api2cursor-style OpenAI chat chunk
// compat fixes before the Cursor /responses -> openai bridge expands think tags
// and converts the chunk into Responses SSE events.
func FixRespOpenAIUpstreamChatSSE(eventData []byte) []byte {
	if len(eventData) == 0 {
		return eventData
	}
	eventName, data, ok := parseSSEChunk(eventData)
	if !ok || data == "" || data == "[DONE]" {
		return eventData
	}

	payload, ok := decodeJSONObject([]byte(data))
	if !ok {
		return eventData
	}

	fixed := fixChatChunkPayload(payload, "")
	choices, _ := fixed["choices"].([]interface{})
	if len(choices) == 0 {
		var output bytes.Buffer
		writeSSEChunk(&output, eventName, fixed)
		return output.Bytes()
	}
	firstChoice, ok := choices[0].(map[string]interface{})
	if !ok {
		var output bytes.Buffer
		writeSSEChunk(&output, eventName, fixed)
		return output.Bytes()
	}
	delta, _ := firstChoice["delta"].(map[string]interface{})
	if delta == nil {
		var output bytes.Buffer
		writeSSEChunk(&output, eventName, fixed)
		return output.Bytes()
	}
	content, hasContent := delta["content"].(string)
	toolCalls, hasToolCalls := delta["tool_calls"].([]interface{})
	if !hasContent || content == "" || !hasToolCalls || len(toolCalls) == 0 {
		var output bytes.Buffer
		writeSSEChunk(&output, eventName, fixed)
		return output.Bytes()
	}

	var output bytes.Buffer
	contentPayload := cloneChatChunk(fixed, map[string]interface{}{"content": content}, nil)
	writeSSEChunk(&output, eventName, contentPayload)
	toolPayload := cloneChatChunk(fixed, map[string]interface{}{"tool_calls": toolCalls}, firstChoice["finish_reason"])
	writeSSEChunk(&output, eventName, toolPayload)
	return output.Bytes()
}
