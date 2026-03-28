package stream

import (
	"encoding/json"
	"strings"
)

// Tags match internal/transformer/convert/think_tags.go (XML-style).
const (
	respOpenAIThinkOpen  = "<think>"
	respOpenAIThinkClose = "</think>"
)

// ExpandRespOpenAIUpstreamChatSSE splits one upstream OpenAI Chat completion SSE event into
// one or more events so </think> / tool_calls+content match api2cursor ThinkTagExtractor behavior.
// State is stored on FinalizeState for cross-chunk tags. If no Cursor-specific shaping applies,
// returns [][]byte{eventData}.
func ExpandRespOpenAIUpstreamChatSSE(eventData []byte, state *FinalizeState) [][]byte {
	if len(eventData) == 0 {
		return [][]byte{eventData}
	}
	_, data, ok := parseSSEChunk(eventData)
	if !ok {
		return [][]byte{eventData}
	}
	data = strings.TrimSpace(data)
	if data == "" {
		return [][]byte{eventData}
	}
	if data == "[DONE]" {
		return expandRespOpenAIDone(eventData, state)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return [][]byte{eventData}
	}
	choices, _ := payload["choices"].([]interface{})
	if len(choices) == 0 {
		return [][]byte{eventData}
	}
	choice0, ok := choices[0].(map[string]interface{})
	if !ok {
		return [][]byte{eventData}
	}
	delta, ok := choice0["delta"].(map[string]interface{})
	if !ok {
		return [][]byte{eventData}
	}

	if rc, ok := delta["reasoning_content"].(string); ok && strings.TrimSpace(rc) != "" {
		// Upstream already emits reasoning in-band; do not re-parse content for think tags.
		return [][]byte{eventData}
	}

	if !needsRespOpenAIExpand(delta, state) {
		return [][]byte{eventData}
	}

	finishReason := choice0["finish_reason"]
	template := cloneChatStreamPayload(payload)
	content := stringValue(delta["content"])
	if state != nil && state.RespOpenAIUpstreamThinkBuf != "" {
		content = state.RespOpenAIUpstreamThinkBuf + content
		state.RespOpenAIUpstreamThinkBuf = ""
	}

	var out [][]byte
	hasTools := toolCallsInDelta(delta) != nil

	if content != "" {
		deltas := deltasFromRespOpenAIThinkContent(content, state)
		for i, d := range deltas {
			fr := interface{}(nil)
			if !hasTools && i == len(deltas)-1 {
				fr = finishReason
			}
			out = append(out, packOpenAIChatSSE(template, d, fr))
		}
	}

	if hasTools {
		toolDelta := map[string]interface{}{
			"tool_calls": toolCallsInDelta(delta),
		}
		if prefix := respOpenAIToolPrefixSSE(template, state); len(prefix) > 0 {
			out = append(out, prefix)
		}
		out = append(out, packOpenAIChatSSE(template, toolDelta, finishReason))
	}

	if len(out) == 0 {
		return [][]byte{eventData}
	}
	return out
}

func expandRespOpenAIDone(eventData []byte, state *FinalizeState) [][]byte {
	if state == nil || !state.RespOpenAIUpstreamThinkInTag {
		return [][]byte{eventData}
	}
	state.RespOpenAIUpstreamThinkInTag = false
	state.RespOpenAIUpstreamThinkBuf = ""
	syn := syntheticRespOpenAIThinkCloseSSE()
	return [][]byte{syn, eventData}
}

func needsRespOpenAIExpand(delta map[string]interface{}, state *FinalizeState) bool {
	if state == nil {
		return false
	}
	if state.RespOpenAIUpstreamThinkBuf != "" || state.RespOpenAIUpstreamThinkInTag {
		return true
	}
	c, _ := delta["content"].(string)
	if strings.Contains(c, respOpenAIThinkOpen) {
		return true
	}
	if tc, ok := delta["tool_calls"].([]interface{}); ok && len(tc) > 0 {
		return true
	}
	return false
}

func toolCallsInDelta(delta map[string]interface{}) []interface{} {
	tc, _ := delta["tool_calls"].([]interface{})
	return tc
}

func cloneChatStreamPayload(payload map[string]interface{}) map[string]interface{} {
	raw, err := json.Marshal(payload)
	if err != nil {
		return payload
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return payload
	}
	return out
}

func packOpenAIChatSSE(template map[string]interface{}, delta map[string]interface{}, finishReason interface{}) []byte {
	p := cloneChatStreamPayload(template)
	choices, _ := p["choices"].([]interface{})
	ch0, _ := choices[0].(map[string]interface{})
	ch0["delta"] = delta
	ch0["finish_reason"] = finishReason
	b, _ := json.Marshal(p)
	return []byte("data: " + string(b) + "\n\n")
}

func syntheticRespOpenAIThinkCloseSSE() []byte {
	payload := map[string]interface{}{
		"id":      "",
		"object":  "chat.completion.chunk",
		"model":   "",
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         map[string]interface{}{"content": "\n" + respOpenAIThinkClose + "\n\n"},
				"finish_reason": nil,
			},
		},
	}
	b, _ := json.Marshal(payload)
	return []byte("data: " + string(b) + "\n\n")
}

func respOpenAIToolPrefixSSE(template map[string]interface{}, state *FinalizeState) []byte {
	if state == nil {
		return nil
	}
	if !state.RespOpenAIUpstreamToolSeen {
		state.RespOpenAIUpstreamToolSeen = true
		if state.RespOpenAIUpstreamThinkInTag {
			state.RespOpenAIUpstreamThinkInTag = false
			return packOpenAIChatSSE(template, map[string]interface{}{"content": "\n" + respOpenAIThinkClose + "\n\n"}, nil)
		}
		return packOpenAIChatSSE(template, map[string]interface{}{"content": "\n"}, nil)
	}
	if state.RespOpenAIUpstreamThinkInTag {
		state.RespOpenAIUpstreamThinkInTag = false
		return packOpenAIChatSSE(template, map[string]interface{}{"content": "\n" + respOpenAIThinkClose + "\n\n"}, nil)
	}
	return nil
}

func deltasFromRespOpenAIThinkContent(content string, state *FinalizeState) []map[string]interface{} {
	var deltas []map[string]interface{}
	emitText := func(s string) {
		if s == "" {
			return
		}
		deltas = append(deltas, map[string]interface{}{"content": s})
	}
	emitThinking := func(s string) {
		if s == "" {
			return
		}
		deltas = append(deltas, map[string]interface{}{"reasoning_content": s})
	}
	consumeRespOpenAIThinkStream(content, state, emitText, emitThinking)
	if len(deltas) == 0 && content != "" {
		return []map[string]interface{}{{"content": content}}
	}
	return deltas
}

func splitTrailingPartialOpenTag(s, tag string) (string, string) {
	if s == "" || tag == "" {
		return s, ""
	}
	max := len(tag) - 1
	if max > len(s) {
		max = len(s)
	}
	for i := max; i > 0; i-- {
		if strings.HasPrefix(tag, s[len(s)-i:]) {
			return s[:len(s)-i], s[len(s)-i:]
		}
	}
	return s, ""
}

func consumeRespOpenAIThinkStream(content string, state *FinalizeState, emitText, emitThinking func(string)) {
	if state == nil {
		emitText(content)
		return
	}
	for len(content) > 0 {
		if state.RespOpenAIUpstreamThinkInTag {
			closeIdx := strings.Index(content, respOpenAIThinkClose)
			if closeIdx == -1 {
				text, buf := splitTrailingPartialOpenTag(content, respOpenAIThinkClose)
				if text != "" {
					emitThinking(text)
				}
				state.RespOpenAIUpstreamThinkBuf = buf
				return
			}
			if closeIdx > 0 {
				emitThinking(content[:closeIdx])
			}
			state.RespOpenAIUpstreamThinkInTag = false
			content = content[closeIdx+len(respOpenAIThinkClose):]
			continue
		}
		openIdx := strings.Index(content, respOpenAIThinkOpen)
		if openIdx == -1 {
			text, buf := splitTrailingPartialOpenTag(content, respOpenAIThinkOpen)
			emitText(text)
			state.RespOpenAIUpstreamThinkBuf = buf
			return
		}
		if openIdx > 0 {
			emitText(content[:openIdx])
		}
		state.RespOpenAIUpstreamThinkInTag = true
		content = content[openIdx+len(respOpenAIThinkOpen):]
	}
}
