package augment

import (
	"encoding/json"
	"strings"
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

	// System message from workspace_guidelines -> user_guidelines -> context(lang/path).
	if systemText := buildCommonSystemText(ar); systemText != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": systemText,
		})
	}

	// Chat history.
	for _, entry := range ar.ChatHistory {
		appendOpenAIHistoryEntry(&messages, &entry)
	}

	// Current turn.
	appendOpenAICurrentTurn(&messages, ar.EffectiveCurrentNodes(), ar.Message, ar.Images, ar.EffectiveContext())

	return messages
}

func appendOpenAIHistoryEntry(msgs *[]map[string]interface{}, entry *ChatHistoryEntry) {
	requestNodes := entry.EffectiveRequestNodes()

	// User side.
	if len(requestNodes) > 0 {
		appendOpenAINodesAsUser(msgs, requestNodes, entry.RequestMessage, nil, nil)
	} else if entry.RequestMessage != "" {
		*msgs = append(*msgs, map[string]interface{}{"role": "user", "content": entry.RequestMessage})
	}

	// Assistant side.
	appendOpenAIResponseNodes(msgs, entry.ResponseText, entry.ResponseNodes)
}

func appendOpenAICurrentTurn(msgs *[]map[string]interface{}, nodes []Node, message string, images []string, ctx *ContextBlock) {
	appendOpenAINodesAsUser(msgs, nodes, message, images, ctx)
}

func appendOpenAINodesAsUser(msgs *[]map[string]interface{}, nodes []Node, fallbackText string, topImages []string, ctx *ContextBlock) {
	// Tool results (type=1) → tool role messages.
	// OpenAI requires that tool messages are preceded by an assistant message with tool_calls.
	// Check if the last message already has matching tool_calls; if not, synthesize one.
	if toolResults := extractToolResultNodes(nodes); len(toolResults) > 0 {
		if !lastMessageHasToolCalls(*msgs, toolResults) {
			// Build tool_use_id→name map from response nodes (type=5) in the same node set.
			toolIDToName := make(map[string]string)
			for _, n := range nodes {
				if n.Type == 5 && n.ToolUse != nil {
					toolIDToName[n.ToolUse.ToolUseID] = n.ToolUse.ToolName
				}
			}
			var syntheticToolCalls []map[string]interface{}
			for _, tr := range toolResults {
				toolID := tr.EffectiveToolUseID()
				funcName := toolIDToName[toolID]
				if funcName == "" {
					funcName = "unknown_tool"
				}
				syntheticToolCalls = append(syntheticToolCalls, map[string]interface{}{
					"id":   toolID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      funcName,
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
				"tool_call_id": tr.EffectiveToolUseID(),
				"content":      buildOpenAIToolResultContent(tr),
			})
		}
	}

	// Text + IDE state + images → user message.
	text := buildUserPromptText(*msgs, nodes, fallbackText, ctx)
	imageBlocks := extractImageBlocks(nodes)

	if text != "" || len(imageBlocks) > 0 || len(topImages) > 0 {
		imageParts := make([]map[string]interface{}, 0, len(imageBlocks)+len(topImages))
		for _, img := range imageBlocks {
			imageParts = append(imageParts, convertImageBlockToOpenAI(img))
		}
		for _, raw := range topImages {
			if block := buildOpenAIImagePart(raw, defaultImageMediaType); block != nil {
				imageParts = append(imageParts, block)
			}
		}
		content := buildTextImageContent(text, imageParts)
		*msgs = append(*msgs, map[string]interface{}{"role": "user", "content": content})
	}
}

func appendOpenAITopLevelImages(msg map[string]interface{}, images []string) {
	if len(images) == 0 {
		return
	}
	text, _ := msg["content"].(string)
	imageParts := make([]map[string]interface{}, 0, len(images))
	for _, raw := range images {
		if block := buildOpenAIImagePart(raw, defaultImageMediaType); block != nil {
			imageParts = append(imageParts, block)
		}
	}
	if len(imageParts) > 0 {
		msg["content"] = buildTextImageContent(text, imageParts)
	}
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
	msg := map[string]interface{}{"role": "assistant", "content": nil}

	// Use ResponseText if present; otherwise extract text from type=0 nodes.
	responseText := text
	if responseText == "" {
		responseText = extractText(nodes)
	}
	if responseText != "" {
		msg["content"] = responseText
	}

	// Add tool_calls from type=5 nodes.
	var toolCalls []map[string]interface{}
	hasThinking := false
	for _, n := range nodes {
		if n.Type == 8 && n.Thinking != nil {
			hasThinking = true
			continue
		}
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
	if responseText == "" && len(toolCalls) == 0 && hasThinking {
		msg["content"] = "(thinking...)"
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

func buildOpenAIToolResultContent(tr *ToolResultNode) interface{} {
	if tr == nil {
		return ""
	}
	parts := make([]string, 0, len(tr.ContentNodes))
	for _, node := range tr.ContentNodes {
		switch node.EffectiveType() {
		case "text":
			if text := strings.TrimSpace(node.EffectiveText()); text != "" {
				parts = append(parts, text)
			}
		case "image":
			parts = append(parts, buildOpenAIToolResultImageFallbackText(&node))
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n\n")
	}
	return tr.EffectiveContent()
}

// lastMessageHasToolCalls checks if the last assistant message already contains tool_calls
// matching the given tool results. This avoids inserting a duplicate synthetic assistant message.
func lastMessageHasToolCalls(msgs []map[string]interface{}, results []*ToolResultNode) bool {
	if len(msgs) == 0 {
		return false
	}
	last := msgs[len(msgs)-1]
	if last["role"] != "assistant" {
		return false
	}
	rawToolCalls, ok := last["tool_calls"].([]interface{})
	if !ok {
		if typedToolCalls, ok := last["tool_calls"].([]map[string]interface{}); ok {
			ids := make(map[string]bool, len(typedToolCalls))
			for _, tc := range typedToolCalls {
				if id, ok := tc["id"].(string); ok {
					ids[id] = true
				}
			}
			for _, tr := range results {
				if !ids[tr.EffectiveToolUseID()] {
					return false
				}
			}
			return true
		}
		return false
	}
	ids := make(map[string]bool, len(rawToolCalls))
	for _, raw := range rawToolCalls {
		tc, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if id, ok := tc["id"].(string); ok {
			ids[id] = true
		}
	}
	for _, tr := range results {
		if !ids[tr.EffectiveToolUseID()] {
			return false
		}
	}
	return true
}
