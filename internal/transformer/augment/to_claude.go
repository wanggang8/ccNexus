package augment

import (
	"encoding/json"
	"fmt"
	"strings"
)

// toClaudeRequest converts an AugmentRequest to a Claude Messages API request body.
// The same format is used for both "claude" and "cli" target types; the server
// layer adds the extra anthropic-beta headers required by the CLI variant.
func toClaudeRequest(ar *AugmentRequest) ([]byte, error) {
	messages := buildClaudeMessages(ar)
	tools := buildClaudeTools(ar.EffectiveTools())
	system := buildClaudeSystem(ar.UserGuidelines)
	maxTokens := effectiveMaxTokens(ar.MaxTokens)

	// Prompt Caching — three levels (tools → system → last history message).
	if len(tools) > 0 {
		setClaudeCacheControl(tools[len(tools)-1])
	}
	if len(system) > 0 {
		setClaudeCacheControlBlock(system[0])
	}
	if histEnd := len(messages) - countCurrentMessages(ar); histEnd > 0 {
		addCacheControlToMessage(messages[histEnd-1])
	}

	req := map[string]interface{}{
		"model":      ar.Model,
		"max_tokens": maxTokens,
		"stream":     ar.IsStreaming(),
		"messages":   messages,
	}
	if len(tools) > 0 {
		req["tools"] = tools
	}
	if len(system) > 0 {
		req["system"] = system
	}
	if thinking := buildClaudeThinkingConfig(ar.Thinking, ar.EnableThinking, ar.Model, maxTokens, false); thinking != nil {
		req["thinking"] = thinking
	}

	return json.Marshal(req)
}

// buildClaudeMessages converts chat history + current turn to Claude messages.
func buildClaudeMessages(ar *AugmentRequest) []map[string]interface{} {
	var messages []map[string]interface{}

	for _, entry := range ar.ChatHistory {
		appendClaudeHistoryEntry(&messages, &entry)
	}
	appendClaudeCurrentTurn(&messages, ar.Nodes, ar.Message, ar.Images)

	return messages
}

func appendClaudeHistoryEntry(msgs *[]map[string]interface{}, entry *ChatHistoryEntry) {
	// User side
	if len(entry.RequestNodes) > 0 {
		appendClaudeNodesAsUser(msgs, entry.RequestNodes, entry.RequestMessage, nil)
	} else if entry.RequestMessage != "" {
		*msgs = append(*msgs, map[string]interface{}{"role": "user", "content": entry.RequestMessage})
	}

	// Assistant side
	appendClaudeResponseNodes(msgs, entry.ResponseText, entry.ResponseNodes)
}

func appendClaudeCurrentTurn(msgs *[]map[string]interface{}, nodes []Node, message string, images []string) {
	if len(nodes) == 0 {
		if message != "" || len(images) > 0 {
			msg := map[string]interface{}{"role": "user", "content": message}
			appendTopLevelImages(msg, images)
			*msgs = append(*msgs, msg)
		}
		return
	}
	appendClaudeNodesAsUser(msgs, nodes, message, images)
}

func appendClaudeNodesAsUser(msgs *[]map[string]interface{}, nodes []Node, fallbackText string, topImages []string) {
	// tool_result (type=1) must be a separate user message.
	if toolResults := extractToolResults(nodes); len(toolResults) > 0 {
		content := make([]map[string]interface{}, 0, len(toolResults))
		for _, tr := range toolResults {
			block := map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": tr.ToolUseID,
				"content":     tr.Content,
			}
			if len(tr.Content) >= minCacheSizeBytes {
				block["cache_control"] = map[string]interface{}{"type": "ephemeral"}
			}
			content = append(content, block)
		}
		*msgs = append(*msgs, map[string]interface{}{"role": "user", "content": content})
	}

	// Text + IDE state + images.
	// Prefer text extracted from nodes; fall back to the plain-text message field.
	text := extractText(nodes)
	if text == "" {
		text = fallbackText
	}
	ideState := extractIdeState(nodes)
	if ideState != "" && !ideStateDuplicate(*msgs, ideState) {
		text += "\n\n[ide_state]\n" + ideState
	}

	imageBlocks := extractImageBlocks(nodes)

	imageParts := make([]map[string]interface{}, 0, len(imageBlocks)+len(topImages))
	for _, img := range imageBlocks {
		imageParts = append(imageParts, img)
	}
	for _, raw := range topImages {
		if block := buildClaudeImageBlock(raw, defaultImageMediaType); block != nil {
			imageParts = append(imageParts, block)
		}
	}

	if text != "" || len(imageParts) > 0 {
		content := buildTextImageContent(text, imageParts)
		*msgs = append(*msgs, map[string]interface{}{"role": "user", "content": content})
	}
}

func appendTopLevelImages(msg map[string]interface{}, images []string) {
	if len(images) == 0 {
		return
	}
	text, _ := msg["content"].(string)
	imageParts := make([]map[string]interface{}, 0, len(images))
	for _, raw := range images {
		if block := buildClaudeImageBlock(raw, defaultImageMediaType); block != nil {
			imageParts = append(imageParts, block)
		}
	}
	if len(imageParts) > 0 {
		msg["content"] = buildTextImageContent(text, imageParts)
	}
}

func appendClaudeResponseNodes(msgs *[]map[string]interface{}, text string, nodes []Node) {
	if text == "" && len(nodes) == 0 {
		return
	}
	var content []map[string]interface{}
	hasTextBlock := false

	for _, n := range nodes {
		switch n.Type {
		case 0:
			if n.TextNode != nil && n.TextNode.Content != "" {
				content = append(content, map[string]interface{}{"type": "text", "text": n.TextNode.Content})
				hasTextBlock = true
			}
		case 8:
			if n.Thinking != nil {
				block := map[string]interface{}{
					"type":     "thinking",
					"thinking": n.Thinking.Summary,
				}
				if n.Thinking.Signature != "" {
					block["signature"] = n.Thinking.Signature
				}
				content = append(content, block)
			}
		case 5:
			if n.ToolUse != nil {
				input := parseToolInput(n.ToolUse.InputJSON)
				content = append(content, map[string]interface{}{
					"type":  "tool_use",
					"id":    n.ToolUse.ToolUseID,
					"name":  n.ToolUse.ToolName,
					"input": input,
				})
			}
		}
	}

	if len(content) == 0 && text != "" {
		content = append(content, map[string]interface{}{"type": "text", "text": text})
	} else if text != "" && !hasTextBlock {
		// Preserve node order when structured blocks are present by appending the
		// fallback response text after the node-derived content.
		content = append(content, map[string]interface{}{"type": "text", "text": text})
	}

	if len(content) > 0 {
		*msgs = append(*msgs, map[string]interface{}{"role": "assistant", "content": content})
	}
}

// buildClaudeTools converts ToolDefinitions to the Claude tools format.
func buildClaudeTools(defs []ToolDefinition) []map[string]interface{} {
	if len(defs) == 0 {
		return nil
	}
	tools := make([]map[string]interface{}, 0, len(defs))
	for _, d := range defs {
		tools = append(tools, map[string]interface{}{
			"name":         d.Name,
			"description":  d.Description,
			"input_schema": d.EffectiveInputSchema(),
		})
	}
	return tools
}

// buildClaudeSystem builds the system content array.
func buildClaudeSystem(userGuidelines string) []map[string]interface{} {
	var system []map[string]interface{}
	if userGuidelines != "" {
		system = append(system, map[string]interface{}{"type": "text", "text": userGuidelines})
	}
	return system
}

// --- cache control helpers ---

func setClaudeCacheControl(tool map[string]interface{}) {
	tool["cache_control"] = map[string]interface{}{"type": "ephemeral"}
}

func setClaudeCacheControlBlock(block map[string]interface{}) {
	block["cache_control"] = map[string]interface{}{"type": "ephemeral"}
}

func addCacheControlToMessage(msg map[string]interface{}) {
	switch c := msg["content"].(type) {
	case string:
		if len(c) >= minCacheSizeBytes {
			msg["content"] = []map[string]interface{}{
				{"type": "text", "text": c, "cache_control": map[string]interface{}{"type": "ephemeral"}},
			}
		}
	case []map[string]interface{}:
		for _, block := range c {
			if t, _ := block["type"].(string); t == "text" {
				if text, _ := block["text"].(string); len(text) >= minCacheSizeBytes {
					block["cache_control"] = map[string]interface{}{"type": "ephemeral"}
				}
			} else if t == "tool_result" {
				if content, _ := block["content"].(string); len(content) >= minCacheSizeBytes {
					block["cache_control"] = map[string]interface{}{"type": "ephemeral"}
				}
			}
		}
	}
}

// countCurrentMessages returns the number of messages added by the current turn
// (used to find the boundary between history and current turn for cache marking).
func countCurrentMessages(ar *AugmentRequest) int {
	if len(ar.Nodes) == 0 && ar.Message == "" {
		return 0
	}
	count := 0
	nodes := ar.Nodes
	if len(extractToolResults(nodes)) > 0 {
		count++ // separate tool_result message
	}
	text := ar.Message
	if text == "" {
		text = extractText(nodes)
	}
	ideState := extractIdeState(nodes)
	imageBlocks := extractImageBlocks(nodes)
	if text != "" || ideState != "" || len(imageBlocks) > 0 || len(ar.Images) > 0 {
		count++
	}
	return count
}

// --- shared node extraction helpers ---

type toolResult struct {
	ToolUseID string
	Content   string
}

func extractToolResults(nodes []Node) []toolResult {
	var out []toolResult
	for _, n := range nodes {
		if n.Type == 1 && n.ToolResultNode != nil {
			out = append(out, toolResult{
				ToolUseID: n.ToolResultNode.ToolUseID,
				Content:   n.ToolResultNode.Content,
			})
		}
	}
	return out
}

// minCacheSizeBytes is the minimum content size (in bytes) to apply cache_control.
// Claude's prompt caching has a 125% write cost, requiring at least 2 requests to break even.
// We use 2048 bytes (~1024 tokens) as the threshold for economic caching.
const minCacheSizeBytes = 2048

func extractText(nodes []Node) string {
	for _, n := range nodes {
		if n.Type == 0 && n.TextNode != nil {
			return n.TextNode.Content
		}
	}
	return ""
}

var imageFormatMap = map[int]string{
	0: "image/png", // Default format (unspecified)
	1: "image/png",
	2: "image/jpeg",
	3: "image/gif",
	4: "image/webp",
}

func extractImageBlocks(nodes []Node) []map[string]interface{} {
	var out []map[string]interface{}
	for _, n := range nodes {
		if n.Type == 2 && n.ImageNode != nil && n.ImageNode.ImageData != "" {
			mediaType := imageFormatMap[n.ImageNode.Format]
			if mediaType == "" {
				mediaType = defaultImageMediaType
			}
			if block := buildClaudeImageBlock(n.ImageNode.ImageData, mediaType); block != nil {
				out = append(out, block)
			}
		}
	}
	return out
}

func extractIdeState(nodes []Node) string {
	for _, n := range nodes {
		if n.Type == 4 && n.IdeStateNode != nil {
			return formatIdeState(n.IdeStateNode)
		}
	}
	return ""
}

func formatIdeState(s *IdeStateNode) string {
	if s == nil {
		return ""
	}
	var parts []string
	if len(s.WorkspaceFolders) > 0 {
		var folders []string
		for _, f := range s.WorkspaceFolders {
			root := f.FolderRoot
			if root == "" {
				root = f.RepositoryRoot
			}
			if root != "" {
				folders = append(folders, root)
			}
		}
		if len(folders) > 0 {
			parts = append(parts, "workspace_folders: "+strings.Join(folders, ", "))
		}
	}
	if s.CurrentTerminal != nil && s.CurrentTerminal.CurrentWorkingDirectory != "" {
		parts = append(parts, "cwd: "+s.CurrentTerminal.CurrentWorkingDirectory)
	}
	return strings.Join(parts, "; ")
}

// ideStateDuplicate checks if the ide_state text already appears in the last 3 messages.
func ideStateDuplicate(msgs []map[string]interface{}, ideState string) bool {
	needle := "[ide_state]\n" + ideState
	start := len(msgs) - 3
	if start < 0 {
		start = 0
	}
	for i := start; i < len(msgs); i++ {
		switch c := msgs[i]["content"].(type) {
		case string:
			if strings.Contains(c, needle) {
				return true
			}
		case []map[string]interface{}:
			for _, block := range c {
				if text, _ := block["text"].(string); strings.Contains(text, needle) {
					return true
				}
			}
		}
	}
	return false
}

func parseToolInput(inputJSON string) map[string]interface{} {
	if inputJSON == "" {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(inputJSON), &out); err != nil {
		return map[string]interface{}{}
	}
	return out
}

func effectiveMaxTokens(n int) int {
	if n <= 0 {
		return 32000
	}
	return n
}

// targetPath returns the URL path for the given target type.
func TargetPath(targetType string) string {
	switch targetType {
	case "cli":
		return "/v1/messages?beta=true"
	case "openai", "openai2":
		return "/v1/chat/completions"
	default:
		return "/v1/messages"
	}
}

// buildAuthHeaders returns the auth headers for the given target type and API key.
func BuildAuthHeaders(targetType, apiKey string) map[string]string {
	switch targetType {
	case "openai", "openai2":
		return map[string]string{"Authorization": fmt.Sprintf("Bearer %s", apiKey)}
	default:
		return map[string]string{
			"x-api-key":         apiKey,
			"anthropic-version": "2023-06-01",
		}
	}
}
