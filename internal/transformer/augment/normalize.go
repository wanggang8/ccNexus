package augment

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

func normalizeAugmentRequest(body []byte) (*AugmentRequest, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var raw map[string]interface{}
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return normalizeAugmentRequestMap(raw), nil
}

// ParseRequest normalizes an Augment request body, accepting both snake_case
// and camelCase aliases used by different Augment clients.
func ParseRequest(body []byte) (*AugmentRequest, error) {
	return normalizeAugmentRequest(body)
}

func normalizeAugmentRequestMap(raw map[string]interface{}) *AugmentRequest {
	if raw == nil {
		return &AugmentRequest{}
	}

	message, messageSource := normalizePrimaryMessageWithSource(raw)

	req := &AugmentRequest{
		Model:                      firstString(raw, "model"),
		Message:                    message,
		MessageSource:              messageSource,
		Nodes:                      normalizeNodes(firstArray(raw, "nodes")),
		StructuredRequestNodes:     normalizeNodes(firstArray(raw, "structured_request_nodes", "structuredRequestNodes")),
		RequestNodes:               normalizeNodes(firstArray(raw, "request_nodes", "requestNodes")),
		ChatHistory:                normalizeChatHistory(firstArray(raw, "chat_history", "chatHistory")),
		ToolDefinitions:            normalizeToolDefinitions(firstArray(raw, "tool_definitions", "toolDefinitions", "tools")),
		UserGuidelines:             firstString(raw, "user_guidelines", "userGuidelines"),
		WorkspaceGuidelines:        firstString(raw, "workspace_guidelines", "workspaceGuidelines"),
		AgentMemories:              normalizeAgentMemories(raw),
		Mode:                       firstString(raw, "mode"),
		Context:                    normalizeContext(firstMap(raw, "context")),
		Prefix:                     firstString(raw, "prefix"),
		SelectedCode:               firstString(raw, "selected_code", "selectedCode", "selected_text", "selectedText", "selected_code_snippet", "selectedCodeSnippet"),
		DisableSelectedCodeDetails: firstBool(raw, "disable_selected_code_details", "disableSelectedCodeDetails"),
		Suffix:                     firstString(raw, "suffix"),
		Diff:                       firstString(raw, "diff"),
		Lang:                       firstString(raw, "lang", "language"),
		Path:                       firstString(raw, "path"),
		Images:                     normalizeStringSlice(firstArray(raw, "images")),
		Thinking:                   raw["thinking"],
		EnableThinking:             firstBool(raw, "enable_thinking", "enableThinking"),
		MaxTokens:                  firstInt(raw, "max_tokens", "maxTokens", "max_output_tokens", "maxOutputTokens"),
		Metadata:                   normalizeStringMap(firstMap(raw, "metadata")),
		Rules:                      firstValue(raw, "rules"),
		FeatureDetectionFlags:      firstMap(raw, "feature_detection_flags", "featureDetectionFlags"),
		PersonaType:                firstInt(raw, "persona_type", "personaType"),
		Silent:                     firstBool(raw, "silent"),
		CanvasID:                   firstString(raw, "canvas_id", "canvasId"),
		RequestIDOverride:          firstString(raw, "request_id_override", "requestIdOverride"),
		ByokSystemPrompt:           firstString(raw, "byok_system_prompt", "byokSystemPrompt", "byok_system", "byokSystem"),
	}

	if streamRaw, ok := firstValueOK(raw, "stream"); ok {
		stream := toBool(streamRaw)
		req.Stream = &stream
	}

	if req.Context == nil {
		req.Context = req.EffectiveContext()
	}
	if req.Context != nil {
		if req.Prefix == "" {
			req.Prefix = req.Context.Prefix
		}
		if req.SelectedCode == "" {
			req.SelectedCode = req.Context.SelectedCode
		}
		if req.Suffix == "" {
			req.Suffix = req.Context.Suffix
		}
		if req.Diff == "" {
			req.Diff = req.Context.Diff
		}
		if req.Lang == "" {
			req.Lang = req.Context.Lang
		}
		if req.Path == "" {
			req.Path = req.Context.Path
		}
	}

	return req
}

func normalizePrimaryMessage(raw map[string]interface{}) string {
	message, _ := normalizePrimaryMessageWithSource(raw)
	return message
}

func normalizePrimaryMessageWithSource(raw map[string]interface{}) (string, string) {
	for _, key := range []string{"message", "prompt", "instruction"} {
		if text := firstString(raw, key); text != "" && !isPlaceholderMessage(text) {
			return text, key
		}
	}
	return "", ""
}

func normalizeAgentMemories(raw map[string]interface{}) string {
	if text := firstString(raw, "agent_memories", "agentMemories"); text != "" {
		return text
	}
	info := firstMap(raw, "memories_info", "memoriesInfo")
	if info == nil {
		return ""
	}
	if text := firstString(info, "agent_memories", "agentMemories", "memories", "memory", "text", "content"); text != "" {
		return text
	}
	items := normalizeStringSlice(firstArray(info, "items", "memories", "memory"))
	return strings.Join(items, "\n")
}

func normalizeChatHistory(items []interface{}) []ChatHistoryEntry {
	if len(items) == 0 {
		return nil
	}
	out := make([]ChatHistoryEntry, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		out = append(out, ChatHistoryEntry{
			RequestMessage:         firstString(raw, "request_message", "requestMessage", "message"),
			RequestNodes:           normalizeNodes(firstArray(raw, "request_nodes", "requestNodes")),
			StructuredRequestNodes: normalizeNodes(firstArray(raw, "structured_request_nodes", "structuredRequestNodes")),
			Nodes:                  normalizeNodes(firstArray(raw, "nodes")),
			ResponseText:           firstString(raw, "response_text", "responseText", "response", "text"),
			ResponseNodes:          normalizeNodes(firstArray(raw, "response_nodes", "responseNodes")),
			StructuredOutputNodes:  normalizeNodes(firstArray(raw, "structured_output_nodes", "structuredOutputNodes")),
		})
	}
	return out
}

func normalizeNodes(items []interface{}) []Node {
	if len(items) == 0 {
		return nil
	}
	out := make([]Node, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		nodeType := firstInt(raw, "type", "node_type", "nodeType")
		node := Node{Type: nodeType}

		switch nodeType {
		case 0:
			payload := firstMap(raw, "text_node", "textNode")
			if payload == nil {
				payload = raw
			}
			node.TextNode = &TextNode{
				Content: firstString(payload, "content"),
				Text:    firstString(payload, "text"),
			}
		case 1:
			payload := firstMap(raw, "tool_result_node", "toolResultNode")
			if payload == nil {
				payload = raw
			}
			node.ToolResultNode = &ToolResultNode{
				ToolUseID:    firstString(payload, "tool_use_id", "toolUseId"),
				ToolCallID:   firstString(payload, "tool_call_id", "toolCallId"),
				Content:      firstString(payload, "content"),
				ToolResult:   firstString(payload, "tool_result", "toolResult"),
				ContentNodes: normalizeToolResultContentNodes(firstArray(payload, "content_nodes", "contentNodes")),
				IsError:      firstBool(payload, "is_error", "isError"),
			}
		case 2:
			if payload := firstMap(raw, "text_node", "textNode"); payload != nil {
				node.TextNode = &TextNode{
					Content: firstString(payload, "content"),
					Text:    firstString(payload, "text"),
				}
				break
			}
			if payload := firstMap(raw, "image_node", "imageNode"); payload != nil || hasImageShape(raw) {
				if payload == nil {
					payload = raw
				}
				node.ImageNode = &ImageNode{
					ImageData: firstString(payload, "image_data", "imageData", "data"),
					Format:    firstInt(payload, "format"),
				}
				break
			}
			payload := raw
			node.TextNode = &TextNode{
				Content: firstString(payload, "content"),
				Text:    firstString(payload, "text"),
			}
		case 3:
			payload := firstMap(raw, "image_id_node", "imageIdNode")
			if payload == nil {
				payload = raw
			}
			node.ImageIDNode = &ImageIDNode{
				ImageID: firstString(payload, "image_id", "imageId"),
				Format:  firstInt(payload, "format"),
			}
		case 4:
			payload := firstMap(raw, "ide_state_node", "ideStateNode")
			if payload == nil {
				payload = raw
			}
			node.IdeStateNode = normalizeIdeStateNode(payload)
		case 5:
			if payload := firstMap(raw, "tool_use", "toolUse"); payload != nil || hasToolUseShape(raw) {
				if payload == nil {
					payload = raw
				}
				node.ToolUse = &ToolUseNode{
					ToolName:      firstString(payload, "tool_name", "toolName"),
					ToolUseID:     firstString(payload, "tool_use_id", "toolUseId"),
					InputJSON:     firstString(payload, "input_json", "inputJson"),
					McpServerName: firstString(payload, "mcp_server_name", "mcpServerName"),
					McpToolName:   firstString(payload, "mcp_tool_name", "mcpToolName"),
				}
				break
			}
			payload := firstMap(raw, "edit_events_node", "editEventsNode")
			if payload == nil {
				payload = raw
			}
			node.EditEventsNode = normalizeEditEventsNode(payload)
		case 6:
			payload := firstMap(raw, "checkpoint_ref_node", "checkpointRefNode")
			if payload == nil {
				payload = raw
			}
			node.CheckpointRef = normalizeCheckpointRefNode(payload)
		case 7:
			if payload := firstMap(raw, "tool_use", "toolUse"); payload != nil || hasToolUseShape(raw) {
				if payload == nil {
					payload = raw
				}
				node.ToolUse = &ToolUseNode{
					ToolName:      firstString(payload, "tool_name", "toolName"),
					ToolUseID:     firstString(payload, "tool_use_id", "toolUseId"),
					InputJSON:     firstString(payload, "input_json", "inputJson"),
					McpServerName: firstString(payload, "mcp_server_name", "mcpServerName"),
					McpToolName:   firstString(payload, "mcp_tool_name", "mcpToolName"),
				}
				break
			}
			payload := firstMap(raw, "change_personality_node", "changePersonalityNode")
			if payload == nil {
				payload = raw
			}
			node.Personality = normalizeChangePersonalityNode(payload)
		case 8:
			if payload := firstMap(raw, "thinking"); payload != nil || hasThinkingShape(raw) {
				if payload == nil {
					payload = raw
				}
				node.Thinking = &ThinkingNode{
					Summary:   firstString(payload, "summary", "thinking"),
					Signature: firstString(payload, "signature"),
				}
				break
			}
			payload := firstMap(raw, "file_node", "fileNode")
			if payload == nil {
				payload = raw
			}
			node.FileNode = &FileNode{
				FileData: firstString(payload, "file_data", "fileData"),
				Format:   firstString(payload, "format"),
			}
		case 9:
			payload := firstMap(raw, "file_id_node", "fileIdNode")
			if payload == nil {
				payload = raw
			}
			node.FileIDNode = &FileIDNode{
				FileID:   firstString(payload, "file_id", "fileId"),
				FileName: firstString(payload, "file_name", "fileName"),
			}
		case 10:
			payload := firstMap(raw, "history_summary", "historySummary", "history_summary_node", "historySummaryNode")
			if payload == nil {
				payload = raw
			}
			node.HistorySummary = normalizeHistorySummaryNode(payload)
		}

		out = append(out, node)
	}
	return out
}

func normalizeHistorySummaryNode(raw map[string]interface{}) *HistorySummaryNode {
	if raw == nil {
		return nil
	}
	return &HistorySummaryNode{
		Text:                                   firstString(raw, "text"),
		SummaryText:                            firstString(raw, "summary_text"),
		SummaryTextAlt:                         firstString(raw, "summaryText"),
		SummarizationRequestID:                 firstString(raw, "summarization_request_id"),
		SummarizationRequestIDAlt:              firstString(raw, "summarizationRequestId"),
		HistoryBeginningDroppedNumExchanges:    firstInt(raw, "history_beginning_dropped_num_exchanges"),
		HistoryBeginningDroppedNumExchangesAlt: firstInt(raw, "historyBeginningDroppedNumExchanges"),
		HistoryMiddleAbridgedText:              firstString(raw, "history_middle_abridged_text"),
		HistoryMiddleAbridgedTextAlt:           firstString(raw, "historyMiddleAbridgedText"),
		MessageTemplate:                        firstString(raw, "message_template"),
		MessageTemplateAlt:                     firstString(raw, "messageTemplate"),
		EndPartFullMaxChars:                    firstInt(raw, "end_part_full_max_chars"),
		EndPartFullMaxCharsAlt:                 firstInt(raw, "endPartFullMaxChars"),
		EndPartFullTailChars:                   firstInt(raw, "end_part_full_tail_chars"),
		EndPartFullTailCharsAlt:                firstInt(raw, "endPartFullTailChars"),
		HistoryEnd:                             normalizeMapSlice(firstArray(raw, "history_end")),
		HistoryEndAlt:                          normalizeMapSlice(firstArray(raw, "historyEnd")),
	}
}

func normalizeIdeStateNode(raw map[string]interface{}) *IdeStateNode {
	if raw == nil {
		return nil
	}
	node := &IdeStateNode{
		WorkspaceFolders: normalizeWorkspaceFolders(firstArray(raw, "workspace_folders", "workspaceFolders")),
	}
	if hasValue(raw, "workspace_folders_unchanged", "workspaceFoldersUnchanged") {
		unchanged := firstBool(raw, "workspace_folders_unchanged", "workspaceFoldersUnchanged")
		node.WorkspaceFoldersUnchanged = &unchanged
	}
	if terminal := firstMap(raw, "current_terminal", "currentTerminal"); terminal != nil {
		node.CurrentTerminal = &TerminalState{
			TerminalID:              firstInt(terminal, "terminal_id", "terminalId"),
			CurrentWorkingDirectory: firstString(terminal, "current_working_directory", "currentWorkingDirectory"),
		}
	}
	return node
}

func normalizeEditEventsNode(raw map[string]interface{}) *EditEventsNode {
	if raw == nil {
		return nil
	}
	node := &EditEventsNode{
		Source: firstString(raw, "source"),
	}
	for _, item := range firstArray(raw, "edit_events", "editEvents") {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		ev := FileEditEvent{
			Path:           firstString(entry, "path"),
			BeforeBlobName: firstString(entry, "before_blob_name", "beforeBlobName"),
			AfterBlobName:  firstString(entry, "after_blob_name", "afterBlobName"),
		}
		for _, rawEdit := range firstArray(entry, "edits") {
			edit, ok := rawEdit.(map[string]interface{})
			if !ok {
				continue
			}
			ev.Edits = append(ev.Edits, TextEditDiff{
				AfterLineStart:  firstInt(edit, "after_line_start", "afterLineStart"),
				BeforeLineStart: firstInt(edit, "before_line_start", "beforeLineStart"),
				BeforeText:      firstString(edit, "before_text", "beforeText"),
				AfterText:       firstString(edit, "after_text", "afterText"),
			})
		}
		node.EditEvents = append(node.EditEvents, ev)
	}
	return node
}

func normalizeCheckpointRefNode(raw map[string]interface{}) *CheckpointRefNode {
	if raw == nil {
		return nil
	}
	return &CheckpointRefNode{
		RequestID:     firstString(raw, "request_id", "requestId"),
		FromTimestamp: firstInt64(raw, "from_timestamp", "fromTimestamp"),
		ToTimestamp:   firstInt64(raw, "to_timestamp", "toTimestamp"),
		Source:        firstString(raw, "source"),
	}
}

func normalizeChangePersonalityNode(raw map[string]interface{}) *ChangePersonalityNode {
	if raw == nil {
		return nil
	}
	return &ChangePersonalityNode{
		PersonalityType:    firstInt(raw, "personality_type", "personalityType"),
		CustomInstructions: firstString(raw, "custom_instructions", "customInstructions"),
	}
}

func normalizeWorkspaceFolders(items []interface{}) []WorkspaceFolder {
	if len(items) == 0 {
		return nil
	}
	out := make([]WorkspaceFolder, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		out = append(out, WorkspaceFolder{
			FolderRoot:     firstString(raw, "folder_root", "folderRoot"),
			RepositoryRoot: firstString(raw, "repository_root", "repositoryRoot"),
		})
	}
	return out
}

func normalizeToolDefinitions(items []interface{}) []ToolDefinition {
	if len(items) == 0 {
		return nil
	}
	out := make([]ToolDefinition, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name := firstString(raw, "name")
		if name == "" {
			continue
		}
		out = append(out, ToolDefinition{
			Name:               name,
			Description:        firstString(raw, "description"),
			InputSchema:        firstMap(raw, "input_schema"),
			InputSchemaAlt:     firstMap(raw, "inputSchema"),
			InputSchemaJSON:    firstString(raw, "input_schema_json"),
			InputSchemaJSONAlt: firstString(raw, "inputSchemaJson"),
			Parameters:         firstMap(raw, "parameters"),
			McpServerName:      firstString(raw, "mcp_server_name", "mcpServerName"),
			McpToolName:        firstString(raw, "mcp_tool_name", "mcpToolName"),
		})
	}
	return out
}

func normalizeToolResultContentNodes(items []interface{}) []ToolResultContentNode {
	if len(items) == 0 {
		return nil
	}
	out := make([]ToolResultContentNode, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		content := ToolResultContentNode{
			Type:        firstString(raw, "type"),
			NodeType:    firstInt(raw, "node_type", "nodeType"),
			Text:        firstString(raw, "text"),
			TextContent: firstString(raw, "text_content", "textContent"),
			MediaType:   firstString(raw, "media_type", "mediaType"),
			Data:        firstString(raw, "data"),
		}
		if image := firstMap(raw, "image_content", "imageContent"); image != nil {
			content.ImageContent = &ToolResultImageContent{
				ImageData: firstString(image, "image_data", "imageData"),
				Format:    firstInt(image, "format"),
				MediaType: firstString(image, "media_type", "mediaType"),
				Data:      firstString(image, "data"),
			}
		}
		out = append(out, content)
	}
	return out
}

func normalizeContext(raw map[string]interface{}) *ContextBlock {
	if raw == nil {
		return nil
	}
	ctx := &ContextBlock{
		Path:         firstString(raw, "path"),
		Lang:         firstString(raw, "lang", "language"),
		Prefix:       firstString(raw, "prefix"),
		SelectedCode: firstString(raw, "selected_code", "selectedCode", "selected_text", "selectedText", "selected_code_snippet", "selectedCodeSnippet"),
		Suffix:       firstString(raw, "suffix"),
		Diff:         firstString(raw, "diff"),
	}
	if ctx.Path == "" && ctx.Lang == "" && ctx.Prefix == "" && ctx.SelectedCode == "" && ctx.Suffix == "" && ctx.Diff == "" {
		return nil
	}
	return ctx
}

func normalizeMapSlice(items []interface{}) []map[string]interface{} {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func normalizeStringSlice(items []interface{}) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(toString(item)); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func normalizeStringMap(raw map[string]interface{}) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out[key] = toString(value)
	}
	return out
}

func firstString(raw map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			text := strings.TrimSpace(toString(value))
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func firstInt(raw map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if n, ok := toTokenInt(value); ok {
				return n
			}
		}
	}
	return 0
}

func firstInt64(raw map[string]interface{}, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			switch n := value.(type) {
			case int64:
				return n
			case int:
				return int64(n)
			case float64:
				return int64(n)
			case json.Number:
				if v, err := n.Int64(); err == nil {
					return v
				}
			case string:
				if v, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
					return v
				}
			}
		}
	}
	return 0
}

func firstBool(raw map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			return toBool(value)
		}
	}
	return false
}

func firstArray(raw map[string]interface{}, keys ...string) []interface{} {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if items, ok := value.([]interface{}); ok {
				return items
			}
		}
	}
	return nil
}

func firstMap(raw map[string]interface{}, keys ...string) map[string]interface{} {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if item, ok := value.(map[string]interface{}); ok {
				return item
			}
		}
	}
	return nil
}

func hasValue(raw map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if _, ok := raw[key]; ok {
			return true
		}
	}
	return false
}

func hasImageShape(raw map[string]interface{}) bool {
	return hasValue(raw, "image_data", "imageData", "data") || hasValue(raw, "image_node", "imageNode")
}

func hasToolUseShape(raw map[string]interface{}) bool {
	return hasValue(raw, "tool_name", "toolName", "tool_use_id", "toolUseId", "input_json", "inputJson") ||
		hasValue(raw, "tool_use", "toolUse")
}

func hasThinkingShape(raw map[string]interface{}) bool {
	return hasValue(raw, "thinking", "summary", "signature")
}

func firstValue(raw map[string]interface{}, keys ...string) interface{} {
	value, _ := firstValueOK(raw, keys...)
	return value
}

func firstValueOK(raw map[string]interface{}, keys ...string) (interface{}, bool) {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func toBool(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "on":
			return true
		}
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i != 0
		}
	case float64:
		return v != 0
	}
	return false
}

func toString(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(data)
	}
}
