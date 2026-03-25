package augment

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// toClaudeRequest converts an AugmentRequest to a Claude Messages API request body.
// The same format is used for both "claude" and "cli" target types; the server
// layer adds the extra anthropic-beta headers required by the CLI variant.
func toClaudeRequest(ar *AugmentRequest) ([]byte, error) {
	messages, currentMessageCount := buildClaudeMessagesWithCurrentCount(ar)
	tools := buildClaudeTools(ar.EffectiveTools())
	system := buildClaudeSystem(ar)
	maxTokens := effectiveMaxTokens(ar.MaxTokens)

	// Prompt Caching — three levels (tools → system → last history message).
	if len(tools) > 0 {
		setClaudeCacheControl(tools[len(tools)-1])
	}
	if len(system) > 0 {
		setClaudeCacheControlBlock(system[0])
	}
	if histEnd := len(messages) - currentMessageCount; histEnd > 0 {
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
		req["tool_choice"] = map[string]interface{}{"type": "auto"}
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
	messages, _ := buildClaudeMessagesWithCurrentCount(ar)
	return messages
}

func buildClaudeMessagesWithCurrentCount(ar *AugmentRequest) ([]map[string]interface{}, int) {
	var messages []map[string]interface{}
	history, currentNodes := preprocessHistoryForAPI(ar)
	for i := range history {
		entry := &history[i]
		appendClaudeHistoryEntry(&messages, entry)
		if i+1 < len(history) {
			appendClaudeToolResultNodes(&messages, history[i+1].EffectiveRequestNodes())
		}
	}
	currentStart := len(messages)
	appendClaudeCurrentTurn(&messages, currentNodes, ar.Message, ar.MessageSource, ar.DisableSelectedCodeDetails, ar.Images, ar.EffectiveContext())
	messages = repairClaudeToolUseMessages(messages)

	return messages, len(messages) - currentStart
}

func appendClaudeHistoryEntry(msgs *[]map[string]interface{}, entry *ChatHistoryEntry) {
	requestNodes := entry.EffectiveRequestNodes()

	// User side
	if len(requestNodes) > 0 {
		appendClaudeNodesAsUser(msgs, requestNodes, entry.RequestMessage, "", false, nil, nil, false)
	} else if entry.RequestMessage != "" {
		*msgs = append(*msgs, map[string]interface{}{"role": "user", "content": entry.RequestMessage})
	}

	// Assistant side
	appendClaudeResponseNodes(msgs, entry.ResponseText, entry.EffectiveResponseNodes())
}

func appendClaudeCurrentTurn(msgs *[]map[string]interface{}, nodes []Node, message string, messageSource string, disableSelectedCodeDetails bool, images []string, ctx *ContextBlock) {
	appendClaudeToolResultNodes(msgs, nodes)
	appendClaudeNodesAsUser(msgs, nodes, message, messageSource, disableSelectedCodeDetails, images, ctx, true)
}

func appendClaudeNodesAsUser(msgs *[]map[string]interface{}, nodes []Node, fallbackText string, messageSource string, disableSelectedCodeDetails bool, topImages []string, ctx *ContextBlock, includeContext bool) {
	nonToolNodes := excludeToolResultNodes(nodes)
	context := ctx
	if !includeContext {
		context = nil
	}
	text := buildUserPromptText(*msgs, nonToolNodes, fallbackText, context, messageSource, disableSelectedCodeDetails)
	imageBlocks := extractImageBlocks(nonToolNodes)

	imageParts := make([]map[string]interface{}, 0, len(imageBlocks)+len(topImages))
	imageParts = append(imageParts, imageBlocks...)
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

func appendClaudeToolResultNodes(msgs *[]map[string]interface{}, nodes []Node) {
	toolResults := extractToolResultNodes(nodes)
	if len(toolResults) == 0 {
		return
	}
	content := make([]map[string]interface{}, 0, len(toolResults))
	for _, tr := range toolResults {
		block := map[string]interface{}{
			"type":        "tool_result",
			"tool_use_id": tr.EffectiveToolUseID(),
			"content":     buildClaudeToolResultContent(tr),
		}
		if tr.IsError {
			block["is_error"] = true
		}
		if contentText, ok := block["content"].(string); ok && len(contentText) >= minCacheSizeBytes {
			block["cache_control"] = map[string]interface{}{"type": "ephemeral"}
		}
		content = append(content, block)
	}
	*msgs = append(*msgs, map[string]interface{}{"role": "user", "content": content})
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
	preferCompletedToolUse := hasCompletedToolUse(nodes)

	for _, n := range nodes {
		switch n.Type {
		case 0, 2:
			if n.TextNode != nil {
				if nodeText := n.TextNode.EffectiveContent(); nodeText != "" {
					content = append(content, map[string]interface{}{"type": "text", "text": nodeText})
					hasTextBlock = true
				}
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
		case 5, 7:
			if n.ToolUse != nil {
				if n.Type == 7 && preferCompletedToolUse {
					continue
				}
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
func buildClaudeSystem(ar *AugmentRequest) []map[string]interface{} {
	systemText := buildCommonSystemText(ar)
	if systemText == "" {
		return nil
	}
	return []map[string]interface{}{{"type": "text", "text": systemText}}
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

// countCurrentMessages returns the exact number of messages added by the current
// turn by reusing the same message construction path as the real request build.
func countCurrentMessages(ar *AugmentRequest) int {
	if ar == nil {
		return 0
	}
	_, currentCount := buildClaudeMessagesWithCurrentCount(ar)
	return currentCount
}

// --- shared node extraction helpers ---

// minCacheSizeBytes is the minimum content size (in bytes) to apply cache_control.
// Claude's prompt caching has a 125% write cost, requiring at least 2 requests to break even.
// We use 2048 bytes (~1024 tokens) as the threshold for economic caching.
const minCacheSizeBytes = 2048

func buildCommonSystemText(ar *AugmentRequest) string {
	if ar == nil {
		return ""
	}
	var sections []string
	if persona := personaTypeToLabel(ar.PersonaType); persona != "" && persona != "DEFAULT" {
		sections = append(sections, "Persona: "+persona)
	}
	if text := strings.TrimSpace(ar.UserGuidelines); text != "" {
		sections = append(sections, text)
	}
	if text := strings.TrimSpace(ar.WorkspaceGuidelines); text != "" {
		sections = append(sections, text)
	}
	if text := strings.TrimSpace(coerceRulesText(ar.Rules)); text != "" {
		sections = append(sections, text)
	}
	if text := strings.TrimSpace(ar.AgentMemories); text != "" {
		sections = append(sections, text)
	}
	if text := strings.TrimSpace(ar.ByokSystemPrompt); text != "" {
		sections = append(sections, text)
	}
	if mode := strings.TrimSpace(ar.Mode); strings.EqualFold(mode, "agent") {
		sections = append(sections, "You are an AI coding assistant with access to tools. Use tools when needed to complete tasks.")
	}
	if contextText := buildSystemContextText(ar.EffectiveContext()); contextText != "" {
		sections = append(sections, contextText)
	}
	return joinPromptSections(sections...)
}

func buildSystemContextText(ctx *ContextBlock) string {
	if ctx == nil {
		return ""
	}
	lines := []string{"[context]"}
	if text := strings.TrimSpace(ctx.Path); text != "" {
		lines = append(lines, fmt.Sprintf("path=%s", text))
	}
	if text := strings.TrimSpace(ctx.Lang); text != "" {
		lines = append(lines, fmt.Sprintf("lang=%s", text))
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func buildUserContextSections(ctx *ContextBlock) []string {
	if ctx == nil {
		return nil
	}
	sections := make([]string, 0, 4)
	if text := strings.TrimSpace(ctx.Prefix); text != "" {
		sections = append(sections, "[prefix]\n"+text)
	}
	if text := strings.TrimSpace(ctx.SelectedCode); text != "" {
		sections = append(sections, "[selected_code]\n"+text)
	}
	if text := strings.TrimSpace(ctx.Suffix); text != "" {
		sections = append(sections, "[suffix]\n"+text)
	}
	if text := strings.TrimSpace(ctx.Diff); text != "" {
		sections = append(sections, "[diff]\n"+text)
	}
	return sections
}

func shouldIncludeExtraUserContext(nodes []Node, messageSource string, disableSelectedCodeDetails bool) bool {
	if disableSelectedCodeDetails {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(messageSource), "prompt") {
		return false
	}
	return len(nodes) == 0
}

func buildUserPromptText(existingMessages []map[string]interface{}, nodes []Node, fallbackText string, ctx *ContextBlock, messageSource string, disableSelectedCodeDetails bool) string {
	var sections []string
	if text := strings.TrimSpace(fallbackText); text != "" {
		sections = append(sections, text)
	}
	sections = append(sections, extractRequestNodeSections(existingMessages, nodes)...)
	if shouldIncludeExtraUserContext(nodes, messageSource, disableSelectedCodeDetails) {
		sections = append(sections, buildUserContextSections(ctx)...)
	}
	return joinPromptSections(sections...)
}

func extractRequestNodeSections(existingMessages []map[string]interface{}, nodes []Node) []string {
	var out []string
	for _, n := range nodes {
		switch n.Type {
		case 0:
			if n.TextNode != nil {
				if text := strings.TrimSpace(n.TextNode.EffectiveContent()); text != "" {
					out = append(out, text)
				}
			}
		case 3:
			if text := formatImageIDPrompt(n.ImageIDNode); text != "" {
				out = append(out, text)
			}
		case 4:
			if text := formatIdeState(n.IdeStateNode); text != "" && !ideStateDuplicate(existingMessages, text) {
				out = append(out, "[IDE_STATE]\n"+text+"\n[/IDE_STATE]")
			}
		case 5:
			if text := formatEditEventsPrompt(n.EditEventsNode); text != "" {
				out = append(out, text)
			}
		case 6:
			if text := formatCheckpointRefPrompt(n.CheckpointRef); text != "" {
				out = append(out, text)
			}
		case 7:
			if text := formatChangePersonalityPrompt(n.Personality); text != "" {
				out = append(out, text)
			}
		case 8:
			if text := formatFilePrompt(n.FileNode); text != "" {
				out = append(out, text)
			}
		case 9:
			if text := formatFileIDPrompt(n.FileIDNode); text != "" {
				out = append(out, text)
			}
		case 10:
			if text := formatHistorySummaryPrompt(n.HistorySummary); text != "" {
				out = append(out, text)
			}
		}
	}
	return out
}

func extractAssistantTextSections(nodes []Node, fallbackText string) []string {
	var mainText []string
	var rawText []string
	for _, n := range nodes {
		switch n.Type {
		case 2:
			if n.TextNode != nil {
				if text := strings.TrimSpace(n.TextNode.EffectiveContent()); text != "" {
					mainText = append(mainText, text)
				}
			}
		case 0:
			if n.TextNode != nil {
				if text := strings.TrimSpace(n.TextNode.EffectiveContent()); text != "" {
					rawText = append(rawText, text)
				}
			}
		}
	}
	if len(mainText) > 0 {
		return mainText
	}
	if len(rawText) > 0 {
		return rawText
	}
	if text := strings.TrimSpace(fallbackText); text != "" {
		return []string{text}
	}
	return nil
}

func formatHistorySummaryPrompt(node *HistorySummaryNode) string {
	if node == nil {
		return ""
	}
	if rendered := renderHistorySummaryNodeValue(node, nil); rendered != "" {
		return rendered
	}
	lines := []string{"[history_summary]"}
	if text := strings.TrimSpace(node.Text); text != "" {
		lines = append(lines, text)
	}
	if text := strings.TrimSpace(node.SummaryText); text != "" {
		lines = append(lines, fmt.Sprintf("summary_text=%s", text))
	} else if text := strings.TrimSpace(node.SummaryTextAlt); text != "" {
		lines = append(lines, fmt.Sprintf("summary_text=%s", text))
	}
	if text := strings.TrimSpace(node.SummarizationRequestID); text != "" {
		lines = append(lines, fmt.Sprintf("summarization_request_id=%s", text))
	} else if text := strings.TrimSpace(node.SummarizationRequestIDAlt); text != "" {
		lines = append(lines, fmt.Sprintf("summarization_request_id=%s", text))
	}
	if n := node.HistoryBeginningDroppedNumExchanges; n > 0 {
		lines = append(lines, fmt.Sprintf("history_beginning_dropped_num_exchanges=%d", n))
	} else if n := node.HistoryBeginningDroppedNumExchangesAlt; n > 0 {
		lines = append(lines, fmt.Sprintf("history_beginning_dropped_num_exchanges=%d", n))
	}
	if text := strings.TrimSpace(node.HistoryMiddleAbridgedText); text != "" {
		lines = append(lines, fmt.Sprintf("history_middle_abridged_text=%s", text))
	} else if text := strings.TrimSpace(node.HistoryMiddleAbridgedTextAlt); text != "" {
		lines = append(lines, fmt.Sprintf("history_middle_abridged_text=%s", text))
	}
	if text := strings.TrimSpace(node.MessageTemplate); text != "" {
		lines = append(lines, fmt.Sprintf("message_template=%s", text))
	} else if text := strings.TrimSpace(node.MessageTemplateAlt); text != "" {
		lines = append(lines, fmt.Sprintf("message_template=%s", text))
	}
	if len(node.HistoryEnd) > 0 {
		lines = append(lines, formatHistoryEndLines(node.HistoryEnd)...)
	} else if len(node.HistoryEndAlt) > 0 {
		lines = append(lines, formatHistoryEndLines(node.HistoryEndAlt)...)
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func formatHistoryEndLines(historyEnd []map[string]interface{}) []string {
	if len(historyEnd) == 0 {
		return nil
	}
	lines := []string{"history_end="}
	for _, item := range historyEnd {
		lines = append(lines, "  "+stableJSON(item))
	}
	return lines
}

func joinPromptSections(sections ...string) string {
	filtered := make([]string, 0, len(sections))
	for _, section := range sections {
		if text := strings.TrimSpace(section); text != "" {
			filtered = append(filtered, text)
		}
	}
	return strings.Join(filtered, "\n\n")
}

func stableJSON(v interface{}) string {
	data, err := json.Marshal(normalizeJSONValue(v))
	if err != nil {
		return "[]"
	}
	return string(data)
}

func normalizeJSONValue(v interface{}) interface{} {
	switch value := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(value))
		for k := range value {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		normalized := make(map[string]interface{}, len(value))
		for _, k := range keys {
			normalized[k] = normalizeJSONValue(value[k])
		}
		return normalized
	case []interface{}:
		normalized := make([]interface{}, len(value))
		for i, item := range value {
			normalized[i] = normalizeJSONValue(item)
		}
		return normalized
	case []map[string]interface{}:
		normalized := make([]interface{}, len(value))
		for i, item := range value {
			normalized[i] = normalizeJSONValue(item)
		}
		return normalized
	default:
		return value
	}
}

func extractToolResultNodes(nodes []Node) []*ToolResultNode {
	var out []*ToolResultNode
	for _, n := range nodes {
		if n.Type == 1 && n.ToolResultNode != nil {
			out = append(out, n.ToolResultNode)
		}
	}
	return out
}

func buildClaudeToolResultContent(tr *ToolResultNode) interface{} {
	if tr == nil {
		return ""
	}
	content := make([]map[string]interface{}, 0, len(tr.ContentNodes))
	for _, node := range tr.ContentNodes {
		switch node.EffectiveType() {
		case "text":
			if text := strings.TrimSpace(node.EffectiveText()); text != "" {
				content = append(content, map[string]interface{}{"type": "text", "text": text})
			}
		case "image":
			if block := buildClaudeImageFromToolResultContentNode(&node); block != nil {
				content = append(content, block)
			}
		}
	}
	if len(content) > 0 {
		return content
	}
	return tr.EffectiveContent()
}

func extractText(nodes []Node) string {
	return strings.Join(extractAssistantTextSections(nodes, ""), "")
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
	if s.WorkspaceFoldersUnchanged != nil {
		parts = append(parts, fmt.Sprintf("workspace_folders_unchanged=%t", *s.WorkspaceFoldersUnchanged))
	}
	if len(s.WorkspaceFolders) > 0 {
		var folders []string
		for _, f := range s.WorkspaceFolders {
			repo := truncateInlineText(f.RepositoryRoot, 200)
			root := truncateInlineText(f.FolderRoot, 200)
			if repo != "" || root != "" {
				folders = append(folders, fmt.Sprintf("- repository_root=%s folder_root=%s", defaultPromptText(repo, "(unknown)"), defaultPromptText(root, "(unknown)")))
			}
		}
		if len(folders) > 0 {
			parts = append(parts, "workspace_folders:")
			parts = append(parts, folders...)
		}
	}
	if s.CurrentTerminal != nil {
		if s.CurrentTerminal.TerminalID > 0 || s.CurrentTerminal.CurrentWorkingDirectory != "" {
			parts = append(parts, fmt.Sprintf("current_terminal: id=%s cwd=%s", defaultPromptInt(s.CurrentTerminal.TerminalID, "?"), defaultPromptText(truncateInlineText(s.CurrentTerminal.CurrentWorkingDirectory, 200), "(unknown)")))
		}
	}
	return strings.Join(parts, "\n")
}

func formatEditEventsPrompt(node *EditEventsNode) string {
	if node == nil {
		return ""
	}
	lines := []string{"[EDIT_EVENTS]"}
	if text := truncateInlineText(node.Source, 200); text != "" {
		lines = append(lines, "source="+text)
	}
	for i, ev := range node.EditEvents {
		if i >= 6 {
			break
		}
		lines = append(lines, fmt.Sprintf("- file: %s edits=%d%s%s", defaultPromptText(truncateInlineText(ev.Path, 200), "(unknown)"), len(ev.Edits), optionalPromptKV("before", truncateInlineText(ev.BeforeBlobName, 120)), optionalPromptKV("after", truncateInlineText(ev.AfterBlobName, 120))))
		for j, edit := range ev.Edits {
			if j >= 6 {
				break
			}
			lines = append(lines, fmt.Sprintf("  - edit: after_line_start=%s before_line_start=%s before=%q after=%q", defaultPromptInt(edit.AfterLineStart, "?"), defaultPromptInt(edit.BeforeLineStart, "?"), truncateInlineText(edit.BeforeText, 200), truncateInlineText(edit.AfterText, 200)))
		}
	}
	if len(lines) == 1 {
		return ""
	}
	lines = append(lines, "[/EDIT_EVENTS]")
	return strings.Join(lines, "\n")
}

func formatCheckpointRefPrompt(node *CheckpointRefNode) string {
	if node == nil {
		return ""
	}
	lines := []string{"[CHECKPOINT_REF]"}
	if text := truncateInlineText(node.RequestID, 120); text != "" {
		lines = append(lines, "request_id="+text)
	}
	if node.FromTimestamp != 0 || node.ToTimestamp != 0 {
		lines = append(lines, fmt.Sprintf("from_timestamp=%s to_timestamp=%s", defaultPromptInt64(node.FromTimestamp, "?"), defaultPromptInt64(node.ToTimestamp, "?")))
	}
	if text := truncateInlineText(node.Source, 200); text != "" {
		lines = append(lines, "source="+text)
	}
	if len(lines) == 1 {
		return ""
	}
	lines = append(lines, "[/CHECKPOINT_REF]")
	return strings.Join(lines, "\n")
}

func formatChangePersonalityPrompt(node *ChangePersonalityNode) string {
	if node == nil {
		return ""
	}
	lines := []string{"[CHANGE_PERSONALITY]", "personality_type=" + personaTypeToLabel(node.PersonalityType)}
	if text := truncateInlineText(node.CustomInstructions, 1000); text != "" {
		lines = append(lines, "custom_instructions="+text)
	}
	lines = append(lines, "[/CHANGE_PERSONALITY]")
	return strings.Join(lines, "\n")
}

func formatImageIDPrompt(node *ImageIDNode) string {
	if node == nil {
		return ""
	}
	if id := truncateInlineText(node.ImageID, 200); id != "" {
		return fmt.Sprintf("[IMAGE_ID] image_id=%s format=%s", id, defaultPromptInt(node.Format, "?"))
	}
	return ""
}

func formatFileIDPrompt(node *FileIDNode) string {
	if node == nil {
		return ""
	}
	id := truncateInlineText(node.FileID, 200)
	name := truncateInlineText(node.FileName, 200)
	if id == "" && name == "" {
		return ""
	}
	return fmt.Sprintf("[FILE_ID] file_name=%s file_id=%s", defaultPromptText(name, "(unknown)"), defaultPromptText(id, "(unknown)"))
}

func formatFilePrompt(node *FileNode) string {
	if node == nil {
		return ""
	}
	format := strings.TrimSpace(node.Format)
	if format == "" {
		format = "application/octet-stream"
	}
	raw := strings.TrimSpace(node.FileData)
	if raw == "" {
		return "[FILE] format=" + format + " (empty)"
	}
	data := raw
	if strings.HasPrefix(data, "data:") {
		if idx := strings.Index(data, ";base64,"); idx >= 0 {
			data = data[idx+8:]
		}
	}
	approxBytes := (len(data) * 3) / 4
	if !isTextLikeFileFormat(format) {
		return fmt.Sprintf("[FILE] format=%s bytes~=%d (content omitted)", format, approxBytes)
	}
	decoded := decodeBase64ToText(data)
	if decoded == "" {
		return fmt.Sprintf("[FILE] format=%s bytes~=%d (decode failed)", format, approxBytes)
	}
	if len(decoded) > 20000 {
		decoded = decoded[:20000] + "\n\n[Content truncated due to length...]"
	}
	return fmt.Sprintf("[FILE] format=%s bytes~=%d\n\n%s", format, approxBytes, decoded)
}

func truncateInlineText(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	return text[:maxChars] + "..."
}

func defaultPromptText(text, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return fallback
	}
	return text
}

func defaultPromptInt(v int, fallback string) string {
	if v <= 0 {
		return fallback
	}
	return fmt.Sprintf("%d", v)
}

func defaultPromptInt64(v int64, fallback string) string {
	if v == 0 {
		return fallback
	}
	return fmt.Sprintf("%d", v)
}

func optionalPromptKV(key, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return " " + key + "=" + value
}

func personaTypeToLabel(v int) string {
	switch v {
	case 1:
		return "PROTOTYPER"
	case 2:
		return "BRAINSTORM"
	case 3:
		return "REVIEWER"
	default:
		return "DEFAULT"
	}
}

func isTextLikeFileFormat(format string) bool {
	format = strings.ToLower(strings.TrimSpace(format))
	return strings.HasPrefix(format, "text/") ||
		format == "application/json" ||
		format == "application/xml" ||
		format == "application/yaml" ||
		format == "application/x-yaml" ||
		format == "application/markdown"
}

func decodeBase64ToText(data string) string {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return ""
	}
	return string(decoded)
}

// ideStateDuplicate checks if the ide_state text already appears in the last 3 messages.
func ideStateDuplicate(msgs []map[string]interface{}, ideState string) bool {
	needles := []string{
		"[ide_state]\n" + ideState,
		"[IDE_STATE]\n" + ideState + "\n[/IDE_STATE]",
	}
	start := len(msgs) - 3
	if start < 0 {
		start = 0
	}
	for i := start; i < len(msgs); i++ {
		switch c := msgs[i]["content"].(type) {
		case string:
			for _, needle := range needles {
				if strings.Contains(c, needle) {
					return true
				}
			}
		case []map[string]interface{}:
			for _, block := range c {
				if text, _ := block["text"].(string); text != "" {
					for _, needle := range needles {
						if strings.Contains(text, needle) {
							return true
						}
					}
				}
			}
		case []interface{}:
			for _, raw := range c {
				block, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				if text, _ := block["text"].(string); text != "" {
					for _, needle := range needles {
						if strings.Contains(text, needle) {
							return true
						}
					}
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
	case "openai":
		return "/v1/chat/completions"
	case "openai2":
		return "/v1/responses"
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
