package augment

import (
	"encoding/json"
	"strconv"
	"strings"
)

// AugmentRequest is the wire format sent by the VSCode Augment plugin.
// After decryption (or as-is for plaintext requests) it has this shape.
type AugmentRequest struct {
	Model                  string             `json:"model,omitempty"`
	Message                string             `json:"message,omitempty"`
	Nodes                  []Node             `json:"nodes,omitempty"`
	StructuredRequestNodes []Node             `json:"structured_request_nodes,omitempty"`
	RequestNodes           []Node             `json:"request_nodes,omitempty"`
	ChatHistory            []ChatHistoryEntry `json:"chat_history,omitempty"`
	ToolDefinitions        []ToolDefinition   `json:"tool_definitions,omitempty"`
	ToolDefinitionsAlt     []ToolDefinition   `json:"toolDefinitions,omitempty"`
	Tools                  []ToolDefinition   `json:"tools,omitempty"` // alias field
	UserGuidelines         string             `json:"user_guidelines,omitempty"`
	WorkspaceGuidelines    string             `json:"workspace_guidelines,omitempty"`
	Context                *ContextBlock      `json:"context,omitempty"`
	Prefix                 string             `json:"prefix,omitempty"`
	SelectedCode           string             `json:"selected_code,omitempty"`
	Suffix                 string             `json:"suffix,omitempty"`
	Diff                   string             `json:"diff,omitempty"`
	Lang                   string             `json:"lang,omitempty"`
	Path                   string             `json:"path,omitempty"`
	Images                 []string           `json:"images,omitempty"` // base64 image data
	Thinking               interface{}        `json:"thinking,omitempty"`
	EnableThinking         bool               `json:"enable_thinking,omitempty"`
	MaxTokens              int                `json:"max_tokens,omitempty"`
	Stream                 *bool              `json:"stream,omitempty"`
	Metadata               map[string]string  `json:"metadata,omitempty"`
}

// ContextBlock carries editor/file context from Augment requests.
type ContextBlock struct {
	Path         string `json:"path,omitempty"`
	Lang         string `json:"lang,omitempty"`
	Prefix       string `json:"prefix,omitempty"`
	SelectedCode string `json:"selected_code,omitempty"`
	Suffix       string `json:"suffix,omitempty"`
	Diff         string `json:"diff,omitempty"`
}

// EffectiveTools returns tool_definitions or toolDefinitions or tools (whichever is populated).
func (r *AugmentRequest) EffectiveTools() []ToolDefinition {
	if len(r.ToolDefinitions) > 0 {
		return r.ToolDefinitions
	}
	if len(r.ToolDefinitionsAlt) > 0 {
		return r.ToolDefinitionsAlt
	}
	return r.Tools
}

// EffectiveCurrentNodes merges all supported current-turn request node arrays.
func (r *AugmentRequest) EffectiveCurrentNodes() []Node {
	return mergeNodesDedup(r.Nodes, r.StructuredRequestNodes, r.RequestNodes)
}

// EffectiveContext returns the nested context block, falling back to legacy
// top-level context fields when present.
func (r *AugmentRequest) EffectiveContext() *ContextBlock {
	var ctx ContextBlock
	if r.Context != nil {
		ctx = *r.Context
	}
	if ctx.Path == "" {
		ctx.Path = r.Path
	}
	if ctx.Lang == "" {
		ctx.Lang = r.Lang
	}
	if ctx.Prefix == "" {
		ctx.Prefix = r.Prefix
	}
	if ctx.SelectedCode == "" {
		ctx.SelectedCode = r.SelectedCode
	}
	if ctx.Suffix == "" {
		ctx.Suffix = r.Suffix
	}
	if ctx.Diff == "" {
		ctx.Diff = r.Diff
	}
	if ctx.Path == "" && ctx.Lang == "" && ctx.Prefix == "" && ctx.SelectedCode == "" && ctx.Suffix == "" && ctx.Diff == "" {
		return nil
	}
	return &ctx
}

// IsStreaming returns the stream flag, defaulting to true when not set.
func (r *AugmentRequest) IsStreaming() bool {
	if r.Stream == nil {
		return true
	}
	return *r.Stream
}

// Node represents one element in the nodes or request_nodes / response_nodes arrays.
//
//	type=0  text_node
//	type=1  tool_result_node
//	type=2  image_node
//	type=4  ide_state_node
//	type=5  tool_use (response side)
//	type=8  thinking (response side)
//	type=10 history_summary
type Node struct {
	Type           int                 `json:"type"`
	TextNode       *TextNode           `json:"text_node,omitempty"`
	ToolResultNode *ToolResultNode     `json:"tool_result_node,omitempty"`
	ImageNode      *ImageNode          `json:"image_node,omitempty"`
	IdeStateNode   *IdeStateNode       `json:"ide_state_node,omitempty"`
	ToolUse        *ToolUseNode        `json:"tool_use,omitempty"`
	Thinking       *ThinkingNode       `json:"thinking,omitempty"`
	HistorySummary *HistorySummaryNode `json:"history_summary,omitempty"`
}

// TextNode holds the text content of a type=0 node.
type TextNode struct {
	Content string `json:"content,omitempty"`
	Text    string `json:"text,omitempty"`
}

// EffectiveContent returns the canonical text payload for the node.
func (t *TextNode) EffectiveContent() string {
	if t == nil {
		return ""
	}
	if t.Content != "" {
		return t.Content
	}
	return t.Text
}

// ToolResultNode holds tool execution output (type=1).
type ToolResultNode struct {
	ToolUseID    string                  `json:"tool_use_id,omitempty"`
	ToolCallID   string                  `json:"tool_call_id,omitempty"`
	Content      string                  `json:"content,omitempty"`
	ToolResult   string                  `json:"tool_result,omitempty"`
	ContentNodes []ToolResultContentNode `json:"content_nodes,omitempty"`
	IsError      bool                    `json:"is_error,omitempty"`
}

// EffectiveToolUseID returns the canonical tool call identifier.
func (t *ToolResultNode) EffectiveToolUseID() string {
	if t == nil {
		return ""
	}
	if t.ToolUseID != "" {
		return t.ToolUseID
	}
	return t.ToolCallID
}

// EffectiveContent returns the canonical string fallback content.
func (t *ToolResultNode) EffectiveContent() string {
	if t == nil {
		return ""
	}
	if t.Content != "" {
		return t.Content
	}
	return t.ToolResult
}

// ToolResultContentNode describes structured tool result content.
type ToolResultContentNode struct {
	Type         string                  `json:"type,omitempty"`
	NodeType     int                     `json:"node_type,omitempty"`
	Text         string                  `json:"text,omitempty"`
	TextContent  string                  `json:"text_content,omitempty"`
	MediaType    string                  `json:"media_type,omitempty"`
	Data         string                  `json:"data,omitempty"`
	ImageContent *ToolResultImageContent `json:"image_content,omitempty"`
}

// EffectiveType returns the canonical content node type.
func (n *ToolResultContentNode) EffectiveType() string {
	if n == nil {
		return ""
	}
	if n.Type != "" {
		return n.Type
	}
	switch n.NodeType {
	case 1:
		return "text"
	case 2:
		return "image"
	}
	if n.ImageContent != nil {
		return "image"
	}
	if n.Text != "" || n.TextContent != "" {
		return "text"
	}
	return ""
}

// EffectiveText returns the canonical text payload for the content node.
func (n *ToolResultContentNode) EffectiveText() string {
	if n == nil {
		return ""
	}
	if n.Text != "" {
		return n.Text
	}
	return n.TextContent
}

// ToolResultImageContent carries structured image tool result data.
type ToolResultImageContent struct {
	ImageData string `json:"image_data,omitempty"`
	Format    int    `json:"format,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

// EffectiveData returns the canonical image payload.
func (i *ToolResultImageContent) EffectiveData() string {
	if i == nil {
		return ""
	}
	if i.Data != "" {
		return i.Data
	}
	return i.ImageData
}

// EffectiveMediaType returns the canonical image MIME type.
func (i *ToolResultImageContent) EffectiveMediaType() string {
	if i == nil {
		return ""
	}
	if i.MediaType != "" {
		return i.MediaType
	}
	if mediaType := imageFormatMap[i.Format]; mediaType != "" {
		return mediaType
	}
	return ""
}

// HistorySummaryNode describes Augment request node type=10.
type HistorySummaryNode struct {
	Text                                   string                   `json:"text,omitempty"`
	SummaryText                            string                   `json:"summary_text,omitempty"`
	SummaryTextAlt                         string                   `json:"summaryText,omitempty"`
	SummarizationRequestID                 string                   `json:"summarization_request_id,omitempty"`
	SummarizationRequestIDAlt              string                   `json:"summarizationRequestId,omitempty"`
	HistoryBeginningDroppedNumExchanges    int                      `json:"history_beginning_dropped_num_exchanges,omitempty"`
	HistoryBeginningDroppedNumExchangesAlt int                      `json:"historyBeginningDroppedNumExchanges,omitempty"`
	HistoryMiddleAbridgedText              string                   `json:"history_middle_abridged_text,omitempty"`
	HistoryMiddleAbridgedTextAlt           string                   `json:"historyMiddleAbridgedText,omitempty"`
	MessageTemplate                        string                   `json:"message_template,omitempty"`
	MessageTemplateAlt                     string                   `json:"messageTemplate,omitempty"`
	HistoryEnd                             []map[string]interface{} `json:"history_end,omitempty"`
	HistoryEndAlt                          []map[string]interface{} `json:"historyEnd,omitempty"`
}

// ImageNode holds base64-encoded image data (type=2).
// Format: 1=png, 2=jpeg, 3=gif, 4=webp
type ImageNode struct {
	ImageData string `json:"image_data"`
	Format    int    `json:"format,omitempty"`
}

// IdeStateNode holds IDE context (type=4).
type IdeStateNode struct {
	WorkspaceFolders []WorkspaceFolder `json:"workspace_folders,omitempty"`
	CurrentTerminal  *TerminalState    `json:"current_terminal,omitempty"`
}

// WorkspaceFolder represents an open workspace folder.
type WorkspaceFolder struct {
	FolderRoot     string `json:"folder_root,omitempty"`
	RepositoryRoot string `json:"repository_root,omitempty"`
}

// TerminalState holds terminal context within IdeStateNode.
type TerminalState struct {
	CurrentWorkingDirectory string `json:"current_working_directory,omitempty"`
}

// ToolUseNode describes a tool invocation in a response node (type=5).
type ToolUseNode struct {
	ToolName      string `json:"tool_name"`
	ToolUseID     string `json:"tool_use_id"`
	InputJSON     string `json:"input_json"`
	McpServerName string `json:"mcp_server_name,omitempty"` // MCP server identifier
	McpToolName   string `json:"mcp_tool_name,omitempty"`   // MCP tool name
}

// ThinkingNode describes extended thinking output (type=8).
type ThinkingNode struct {
	Summary   string `json:"summary,omitempty"`
	Signature string `json:"signature,omitempty"`
}

// ChatHistoryEntry is one turn in the conversation history.
type ChatHistoryEntry struct {
	RequestMessage         string `json:"request_message,omitempty"`
	RequestNodes           []Node `json:"request_nodes,omitempty"`
	StructuredRequestNodes []Node `json:"structured_request_nodes,omitempty"`
	ResponseText           string `json:"response_text,omitempty"`
	ResponseNodes          []Node `json:"response_nodes,omitempty"`
}

// EffectiveRequestNodes merges history request node variants.
func (e *ChatHistoryEntry) EffectiveRequestNodes() []Node {
	return mergeNodesDedup(e.RequestNodes, e.StructuredRequestNodes)
}

func mergeNodesDedup(groups ...[]Node) []Node {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	if total == 0 {
		return nil
	}
	out := make([]Node, 0, total)
	seen := make(map[string]struct{}, total)
	for _, group := range groups {
		for _, node := range group {
			fingerprint := nodeFingerprint(node)
			if _, ok := seen[fingerprint]; ok {
				continue
			}
			seen[fingerprint] = struct{}{}
			out = append(out, node)
		}
	}
	return out
}

func nodeFingerprint(n Node) string {
	switch n.Type {
	case 0:
		if n.TextNode != nil {
			return "0|" + strings.TrimSpace(n.TextNode.EffectiveContent())
		}
	case 1:
		if n.ToolResultNode != nil {
			return "1|" + n.ToolResultNode.EffectiveToolUseID() + "|" + strings.TrimSpace(n.ToolResultNode.EffectiveContent()) + "|" + strconv.FormatBool(n.ToolResultNode.IsError) + "|" + toolResultContentNodesFingerprint(n.ToolResultNode.ContentNodes)
		}
	case 2:
		if n.ImageNode != nil {
			return imageNodeFingerprint(n.ImageNode)
		}
	case 4:
		if n.IdeStateNode != nil {
			return "4|" + stableJSONString(n.IdeStateNode)
		}
	case 5:
		if n.ToolUse != nil {
			return "5|" + n.ToolUse.ToolUseID + "|" + n.ToolUse.ToolName + "|" + strings.TrimSpace(n.ToolUse.InputJSON)
		}
	case 8:
		if n.Thinking != nil {
			return "8|" + n.Thinking.Summary + "|" + n.Thinking.Signature
		}
	case 10:
		if n.HistorySummary != nil {
			return "10|" + stableJSONString(n.HistorySummary)
		}
	}
	return strconv.Itoa(n.Type) + "|" + stableJSONString(n)
}

func imageNodeFingerprint(node *ImageNode) string {
	if node == nil {
		return "2|"
	}
	fallbackMediaType := imageFormatMap[node.Format]
	if fallbackMediaType == "" {
		fallbackMediaType = defaultImageMediaType
	}
	data, mediaType := normalizeBase64Image(node.ImageData, fallbackMediaType)
	return "2|" + strings.TrimSpace(data) + "|" + strings.TrimSpace(mediaType)
}

func toolResultContentNodesFingerprint(nodes []ToolResultContentNode) string {
	if len(nodes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		parts = append(parts, toolResultContentNodeFingerprint(node))
	}
	return strings.Join(parts, ";")
}

func toolResultContentNodeFingerprint(node ToolResultContentNode) string {
	nodeType := node.EffectiveType()
	if nodeType == "image" {
		mediaType := strings.ToLower(strings.TrimSpace(effectiveToolResultContentNodeMediaType(&node)))
		data, normalizedMediaType := normalizeBase64Image(effectiveToolResultContentNodeData(&node), mediaType)
		return nodeType + "||" + strings.TrimSpace(normalizedMediaType) + "|" + strings.TrimSpace(data)
	}
	return nodeType + "|" + strings.TrimSpace(node.EffectiveText()) + "|" + strings.ToLower(strings.TrimSpace(node.MediaType)) + "|" + strings.TrimSpace(node.Data)
}

func stableJSONString(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// ToolDefinition describes a tool available to the model.
type ToolDefinition struct {
	Name               string                 `json:"name"`
	Description        string                 `json:"description,omitempty"`
	InputSchema        map[string]interface{} `json:"input_schema,omitempty"`
	InputSchemaAlt     map[string]interface{} `json:"inputSchema,omitempty"`
	InputSchemaJSON    string                 `json:"input_schema_json,omitempty"` // JSON-encoded schema string
	InputSchemaJSONAlt string                 `json:"inputSchemaJson,omitempty"`
	Parameters         map[string]interface{} `json:"parameters,omitempty"`      // OpenAI-style alias
	McpServerName      string                 `json:"mcp_server_name,omitempty"` // MCP server identifier
	McpToolName        string                 `json:"mcp_tool_name,omitempty"`   // MCP tool name
}

// EffectiveInputSchema returns the parsed input schema, preferring the
// already-decoded InputSchema over InputSchemaJSON over Parameters.
func (t *ToolDefinition) EffectiveInputSchema() map[string]interface{} {
	if len(t.InputSchema) > 0 {
		return t.InputSchema
	}
	if len(t.InputSchemaAlt) > 0 {
		return t.InputSchemaAlt
	}
	if t.InputSchemaJSON != "" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(t.InputSchemaJSON), &parsed); err == nil && len(parsed) > 0 {
			return parsed
		}
	}
	if t.InputSchemaJSONAlt != "" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(t.InputSchemaJSONAlt), &parsed); err == nil && len(parsed) > 0 {
			return parsed
		}
	}
	if len(t.Parameters) > 0 {
		return t.Parameters
	}
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}
