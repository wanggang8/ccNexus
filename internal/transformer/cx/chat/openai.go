package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/transformer"
	"github.com/lich0821/ccNexus/internal/transformer/convert"
)

// OpenAITransformer is a passthrough transformer for Codex Chat → OpenAI Chat
type OpenAITransformer struct {
	model string
}

// NewOpenAITransformer creates a new passthrough transformer
func NewOpenAITransformer(model string) *OpenAITransformer {
	return &OpenAITransformer{model: model}
}

func (t *OpenAITransformer) Name() string {
	return "cx_chat_openai"
}

func (t *OpenAITransformer) TransformRequest(req []byte) ([]byte, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(req, &data); err != nil {
		return req, nil
	}

	// Override model if specified
	if t.model != "" {
		data["model"] = t.model
	}

	// Fix Cursor's Claude-format messages (tool_result)
	if messages, ok := data["messages"].([]interface{}); ok {
		fixedMessages := make([]interface{}, 0, len(messages))
		for _, msgInterface := range messages {
			msg, ok := msgInterface.(map[string]interface{})
			if !ok {
				fixedMessages = append(fixedMessages, msgInterface)
				continue
			}

			// Check if message has Claude-format content blocks with tool_result
			if content, ok := msg["content"].([]interface{}); ok && len(content) > 0 {
				hasToolResult := false
				for _, item := range content {
					if block, ok := item.(map[string]interface{}); ok {
						if block["type"] == "tool_result" {
							hasToolResult = true
							openaiMsg := map[string]interface{}{
								"role":         "tool",
								"tool_call_id": block["tool_use_id"],
								"content":      block["content"],
							}
							fixedMessages = append(fixedMessages, openaiMsg)
						}
					}
				}
				if hasToolResult {
					continue
				}
			}

			// Keep message as-is
			fixedMessages = append(fixedMessages, msg)
		}
		data["messages"] = fixedMessages
	}

	// Fix Cursor's Claude-format tool definitions to OpenAI format
	if tools, ok := data["tools"].([]interface{}); ok && len(tools) > 0 {
		fixedTools := make([]interface{}, 0, len(tools))
		for _, toolInterface := range tools {
			tool, ok := toolInterface.(map[string]interface{})
			if !ok {
				fixedTools = append(fixedTools, toolInterface)
				continue
			}

			// Check if it's Claude format (has "name" at top level, no "type" field)
			if name, hasName := tool["name"].(string); hasName && name != "" && tool["type"] == nil {
				// Convert Claude format to OpenAI format
				// Ensure parameters is a valid object, not nil
				parameters := tool["input_schema"]
				if parameters == nil {
					parameters = map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{},
					}
				}

				openaiTool := map[string]interface{}{
					"type": "function",
					"function": map[string]interface{}{
						"name":        tool["name"],
						"description": tool["description"],
						"parameters":  parameters,
					},
				}
				fixedTools = append(fixedTools, openaiTool)
			} else {
				// Already OpenAI format or unknown format, keep as-is
				fixedTools = append(fixedTools, tool)
			}
		}

		// Log tool count for debugging
		if len(fixedTools) > 20 {
			logger.Warn("Large number of tools detected: %d (OpenAI recommends ≤20)", len(fixedTools))
		}

		data["tools"] = fixedTools
	}

	// Strip cache_control from all messages (Anthropic-specific)
	if messages, ok := data["messages"].([]interface{}); ok {
		for _, msgInterface := range messages {
			if msg, ok := msgInterface.(map[string]interface{}); ok {
				delete(msg, "cache_control")
			}
		}
	}

	// Strip cache_control from tool definitions
	if tools, ok := data["tools"].([]interface{}); ok {
		for _, toolInterface := range tools {
			if tool, ok := toolInterface.(map[string]interface{}); ok {
				delete(tool, "cache_control")
				if fn, ok := tool["function"].(map[string]interface{}); ok {
					delete(fn, "cache_control")
				}
			}
		}
	}

	// Strip Anthropic-specific top-level fields
	delete(data, "thinking")
	delete(data, "budget_tokens")
	delete(data, "reasoning_effort")
	delete(data, "metadata")

	return json.Marshal(data)
}

func (t *OpenAITransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
	if isStreaming {
		return resp, nil
	}

	// Detect response format and convert if needed
	var data map[string]interface{}
	if err := json.Unmarshal(resp, &data); err != nil {
		// Parse failed, passthrough
		return resp, nil
	}

	// Check if it's Claude format: type=="message" && content is array && has stop_reason
	if data["type"] == "message" {
		if _, hasContent := data["content"].([]interface{}); hasContent {
			if _, hasStopReason := data["stop_reason"]; hasStopReason {
				logger.Debug("[cx_chat_openai] Detected Claude format response, converting to OpenAI")
				return convert.ClaudeRespToOpenAI(resp, t.model)
			}
		}
	}

	// OpenAI format or unknown, passthrough
	return resp, nil
}

func (t *OpenAITransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
	if !isStreaming {
		return t.TransformResponse(resp, false)
	}

	// Detect SSE event format
	// Claude SSE events: message_start, content_block_start, content_block_delta, content_block_stop, message_delta, message_stop
	// OpenAI SSE events: data: {"object": "chat.completion.chunk", ...}

	respStr := string(resp)

	// Check for Claude SSE event types
	claudeEvents := []string{
		"event: message_start",
		"event: content_block_start",
		"event: content_block_delta",
		"event: content_block_stop",
		"event: message_delta",
		"event: message_stop",
	}

	for _, event := range claudeEvents {
		if strings.Contains(respStr, event) {
			logger.Debug("[cx_chat_openai] Detected Claude SSE event, converting to OpenAI")
			return convert.ClaudeStreamToOpenAI(resp, ctx, t.model)
		}
	}

	// OpenAI format or unknown: keep normal chunks as-is, repair malformed OpenAI SSE when needed.
	return normalizeOpenAISSE(resp, ctx)
}

func normalizeOpenAISSE(resp []byte, ctx *transformer.StreamContext) ([]byte, error) {
	if ctx == nil {
		return resp, nil
	}

	_, payload := convert.ParseSSE(resp)
	if payload == "" {
		return resp, nil
	}

	if payload == "[DONE]" {
		if ctx.OpenAIStreamDone {
			return nil, nil
		}
		ctx.OpenAIStreamDone = true
		return []byte("data: [DONE]\n\n"), nil
	}

	if ctx.OpenAIStreamDone {
		return nil, nil
	}

	var chunk map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return resp, nil
	}

	choices, ok := chunk["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return resp, nil
	}

	changed := false
	for _, choiceAny := range choices {
		choice, ok := choiceAny.(map[string]interface{})
		if !ok {
			continue
		}

		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}

		toolCalls, ok := delta["tool_calls"].([]interface{})
		if !ok || len(toolCalls) == 0 {
			continue
		}

		normalizedToolCalls, toolChanged := normalizeToolCallDeltas(toolCalls, ctx)
		if toolChanged {
			changed = true
		}
		if len(normalizedToolCalls) == 0 {
			delete(delta, "tool_calls")
			continue
		}
		delta["tool_calls"] = normalizedToolCalls
	}

	if !changed {
		return resp, nil
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("data: %s\n\n", data)), nil
}

func normalizeToolCallDeltas(toolCalls []interface{}, ctx *transformer.StreamContext) ([]interface{}, bool) {
	if ctx.OpenAIToolCalls == nil {
		ctx.OpenAIToolCalls = make(map[int]*transformer.OpenAIToolCall)
	}

	normalized := make([]interface{}, 0, len(toolCalls))
	changed := false

	for _, rawToolCall := range toolCalls {
		tcMap, ok := rawToolCall.(map[string]interface{})
		if !ok {
			normalized = append(normalized, rawToolCall)
			continue
		}

		idx := 0
		if idxValue, ok := tcMap["index"].(float64); ok {
			idx = int(idxValue)
		}

		state, exists := ctx.OpenAIToolCalls[idx]
		if !exists {
			state = &transformer.OpenAIToolCall{}
			indexCopy := idx
			state.Index = &indexCopy
			ctx.OpenAIToolCalls[idx] = state
		}

		currentID, _ := tcMap["id"].(string)
		if currentID != "" {
			if state.ID == "" {
				state.ID = currentID
			} else if state.ID != currentID {
				changed = true
			}
		}

		currentType, _ := tcMap["type"].(string)
		if currentType != "" {
			if state.Type == "" {
				state.Type = currentType
			} else if state.Type != currentType {
				changed = true
			}
		}
		if state.Type == "" {
			state.Type = "function"
		}

		var argumentFragment string
		if functionMap, ok := tcMap["function"].(map[string]interface{}); ok {
			if name, ok := functionMap["name"].(string); ok && name != "" {
				if state.Function.Name == "" {
					state.Function.Name = name
				} else if state.Function.Name != name {
					changed = true
				}
			} else if functionMap["name"] == nil && state.Function.Name != "" {
				changed = true
			}

			if args, ok := functionMap["arguments"].(string); ok {
				argumentFragment = args
				state.Function.Arguments += args
			}
		}

		functionOut := map[string]interface{}{}
		if state.Function.Name != "" {
			functionOut["name"] = state.Function.Name
		}
		if argumentFragment != "" || hasFunctionArguments(tcMap) {
			functionOut["arguments"] = argumentFragment
		}

		out := map[string]interface{}{
			"index": idx,
			"type":  state.Type,
		}
		if state.ID != "" {
			out["id"] = state.ID
		}
		if len(functionOut) > 0 {
			out["function"] = functionOut
		}

		if !deepEqualToolCallShape(tcMap, out) {
			changed = true
		}
		normalized = append(normalized, out)
	}

	return normalized, changed
}

func hasFunctionArguments(tcMap map[string]interface{}) bool {
	functionMap, ok := tcMap["function"].(map[string]interface{})
	if !ok {
		return false
	}
	_, exists := functionMap["arguments"]
	return exists
}

func deepEqualToolCallShape(a, b map[string]interface{}) bool {
	aJSON, aErr := json.Marshal(a)
	bJSON, bErr := json.Marshal(b)
	if aErr != nil || bErr != nil {
		return false
	}
	return string(aJSON) == string(bJSON)
}
