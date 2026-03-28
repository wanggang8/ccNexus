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

	var output bytes.Buffer
	writeSSEChunk(&output, eventName, fixChatChunkPayload(payload, ""))
	return output.Bytes()
}
