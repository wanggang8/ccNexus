package chat

import (
	"encoding/json"
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

			// Check if message has Claude-format content blocks
			if content, ok := msg["content"].([]interface{}); ok && len(content) > 0 {
				// Check if it's a tool_result block
				if len(content) == 1 {
					if block, ok := content[0].(map[string]interface{}); ok {
						if block["type"] == "tool_result" {
							// Convert to OpenAI tool message format
							openaiMsg := map[string]interface{}{
								"role":         "tool",
								"tool_call_id": block["tool_use_id"],
								"content":      block["content"],
							}
							fixedMessages = append(fixedMessages, openaiMsg)
							continue
						}
					}
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

	// OpenAI format or unknown, passthrough
	return resp, nil
}
