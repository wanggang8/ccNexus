package chat

import (
	"encoding/json"

	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/transformer"
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
	return resp, nil
}

func (t *OpenAITransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
	return resp, nil
}
