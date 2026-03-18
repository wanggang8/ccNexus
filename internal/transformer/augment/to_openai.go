package augment

import (
	"encoding/json"
)

// toOpenAIRequest converts an AugmentRequest to an OpenAI Chat Completions API request body.
func toOpenAIRequest(ar *AugmentRequest) ([]byte, error) {
	messages := buildOpenAIMessages(ar)
	tools := buildOpenAITools(ar.EffectiveTools())

	req := map[string]interface{}{
		"model":    ar.Model,
		"messages": messages,
		"stream":   ar.IsStreaming(),
	}
	if ar.MaxTokens > 0 {
		req["max_tokens"] = ar.MaxTokens
	}
	if len(tools) > 0 {
		req["tools"] = tools
	}
	if ar.IsStreaming() {
		req["stream_options"] = map[string]interface{}{"include_usage": true}
	}

	return json.Marshal(req)
}

// buildOpenAIMessages converts chat history + current turn to OpenAI messages.
func buildOpenAIMessages(ar *AugmentRequest) []map[string]interface{} {
	var messages []map[string]interface{}

	// System message from user_guidelines.
	if ar.UserGuidelines != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": ar.UserGuidelines,
		})
	}

	// Chat history.
	for _, entry := range ar.ChatHistory {
		appendOpenAIHistoryEntry(&messages, &entry)
	}

	// Current turn.
	appendOpenAICurrentTurn(&messages, ar.Nodes, ar.Message, ar.Images)

	return messages
}

func appendOpenAIHistoryEntry(msgs *[]map[string]interface{}, entry *ChatHistoryEntry) {
	// User side.
	if len(entry.RequestNodes) > 0 {
		appendOpenAINodesAsUser(msgs, entry.RequestNodes, entry.RequestMessage, nil)
	} else if entry.RequestMessage != "" {
		*msgs = append(*msgs, map[string]interface{}{"role": "user", "content": entry.RequestMessage})
	}

	// Assistant side.
	appendOpenAIResponseNodes(msgs, entry.ResponseText, entry.ResponseNodes)
}

func appendOpenAICurrentTurn(msgs *[]map[string]interface{}, nodes []Node, message string, images []string) {
	if len(nodes) == 0 {
		if message != "" {
			msg := map[string]interface{}{"role": "user", "content": message}
			appendOpenAITopLevelImages(msg, images)
			*msgs = append(*msgs, msg)
		}
		return
	}
	appendOpenAINodesAsUser(msgs, nodes, message, images)
}

func appendOpenAINodesAsUser(msgs *[]map[string]interface{}, nodes []Node, fallbackText string, topImages []string) {
	// Tool results (type=1) → tool role messages.
	// OpenAI requires that tool messages are preceded by an assistant message with tool_calls.
	// Check if the last message already has matching tool_calls; if not, synthesize one.
	if toolResults := extractToolResults(nodes); len(toolResults) > 0 {
		if !lastMessageHasToolCalls(*msgs, toolResults) {
			var syntheticToolCalls []map[string]interface{}
			for _, tr := range toolResults {
				syntheticToolCalls = append(syntheticToolCalls, map[string]interface{}{
					"id":   tr.ToolUseID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      "tool_" + tr.ToolUseID,
						"arguments": "{}",
					},
				})
			}
			*msgs = append(*msgs, map[string]interface{}{
				"role":       "assistant",
				"content":    "",
				"tool_calls": syntheticToolCalls,
			})
		}
		for _, tr := range toolResults {
			*msgs = append(*msgs, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": tr.ToolUseID,
				"content":      tr.Content,
			})
		}
	}

	// Text + IDE state + images → user message.
	text := fallbackText
	if text == "" {
		text = extractText(nodes)
	}
	ideState := extractIdeState(nodes)
	if ideState != "" && !ideStateDuplicate(*msgs, ideState) {
		text += "\n\n[ide_state]\n" + ideState
	}

	imageBlocks := extractImageBlocks(nodes)

	if text != "" || len(imageBlocks) > 0 || len(topImages) > 0 {
		if text == "" {
			text = ""
		}
		var content interface{} = text
		if len(imageBlocks) > 0 || len(topImages) > 0 {
			parts := []map[string]interface{}{{"type": "text", "text": text}}
			for _, img := range imageBlocks {
				parts = append(parts, convertImageBlockToOpenAI(img))
			}
			for _, raw := range topImages {
				if raw != "" {
					parts = append(parts, map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": "data:image/png;base64," + raw,
						},
					})
				}
			}
			content = parts
		}
		*msgs = append(*msgs, map[string]interface{}{"role": "user", "content": content})
	}
}

func appendOpenAITopLevelImages(msg map[string]interface{}, images []string) {
	if len(images) == 0 {
		return
	}
	text, _ := msg["content"].(string)
	parts := []map[string]interface{}{{"type": "text", "text": text}}
	for _, raw := range images {
		if raw != "" {
			parts = append(parts, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": "data:image/png;base64," + raw,
				},
			})
		}
	}
	msg["content"] = parts
}

func convertImageBlockToOpenAI(claudeBlock map[string]interface{}) map[string]interface{} {
	source, _ := claudeBlock["source"].(map[string]interface{})
	mediaType, _ := source["media_type"].(string)
	data, _ := source["data"].(string)
	if mediaType == "" {
		mediaType = "image/png"
	}
	return map[string]interface{}{
		"type": "image_url",
		"image_url": map[string]interface{}{
			"url": "data:" + mediaType + ";base64," + data,
		},
	}
}

func appendOpenAIResponseNodes(msgs *[]map[string]interface{}, text string, nodes []Node) {
	if text == "" && len(nodes) == 0 {
		return
	}
	msg := map[string]interface{}{"role": "assistant"}
	if text != "" {
		msg["content"] = text
	}
	var toolCalls []map[string]interface{}
	for _, n := range nodes {
		if n.Type == 5 && n.ToolUse != nil {
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   n.ToolUse.ToolUseID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      n.ToolUse.ToolName,
					"arguments": n.ToolUse.InputJSON,
				},
			})
		}
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	*msgs = append(*msgs, msg)
}

// buildOpenAITools converts ToolDefinitions to the OpenAI tools format.
func buildOpenAITools(defs []ToolDefinition) []map[string]interface{} {
	if len(defs) == 0 {
		return nil
	}
	tools := make([]map[string]interface{}, 0, len(defs))
	for _, d := range defs {
		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        d.Name,
				"description": d.Description,
				"parameters":  d.EffectiveInputSchema(),
			},
		})
	}
	return tools
}

// lastMessageHasToolCalls checks if the last assistant message already contains tool_calls
// matching the given tool results. This avoids inserting a duplicate synthetic assistant message.
func lastMessageHasToolCalls(msgs []map[string]interface{}, results []toolResult) bool {
	if len(msgs) == 0 {
		return false
	}
	last := msgs[len(msgs)-1]
	if last["role"] != "assistant" {
		return false
	}
	tcs, ok := last["tool_calls"].([]map[string]interface{})
	if !ok {
		return false
	}
	ids := make(map[string]bool, len(tcs))
	for _, tc := range tcs {
		if id, ok := tc["id"].(string); ok {
			ids[id] = true
		}
	}
	for _, tr := range results {
		if !ids[tr.ToolUseID] {
			return false
		}
	}
	return true
}

