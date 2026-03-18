package augment

// AugmentRequest is the wire format sent by the VSCode Augment plugin.
// After decryption (or as-is for plaintext requests) it has this shape.
type AugmentRequest struct {
	Model           string             `json:"model,omitempty"`
	Message         string             `json:"message,omitempty"`
	Nodes           []Node             `json:"nodes,omitempty"`
	ChatHistory     []ChatHistoryEntry `json:"chat_history,omitempty"`
	ToolDefinitions []ToolDefinition   `json:"tool_definitions,omitempty"`
	Tools           []ToolDefinition   `json:"tools,omitempty"` // alias field
	UserGuidelines  string             `json:"user_guidelines,omitempty"`
	Images          []string           `json:"images,omitempty"` // base64 image data
	MaxTokens       int                `json:"max_tokens,omitempty"`
	Stream          *bool              `json:"stream,omitempty"`
	Metadata        map[string]string  `json:"metadata,omitempty"`
}

// EffectiveTools returns tool_definitions or tools (whichever is populated).
func (r *AugmentRequest) EffectiveTools() []ToolDefinition {
	if len(r.ToolDefinitions) > 0 {
		return r.ToolDefinitions
	}
	return r.Tools
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
type Node struct {
	Type           int             `json:"type"`
	TextNode       *TextNode       `json:"text_node,omitempty"`
	ToolResultNode *ToolResultNode `json:"tool_result_node,omitempty"`
	ImageNode      *ImageNode      `json:"image_node,omitempty"`
	IdeStateNode   *IdeStateNode   `json:"ide_state_node,omitempty"`
	ToolUse        *ToolUseNode    `json:"tool_use,omitempty"`
}

// TextNode holds the text content of a type=0 node.
type TextNode struct {
	Content string `json:"content"`
}

// ToolResultNode holds tool execution output (type=1).
type ToolResultNode struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
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

// ChatHistoryEntry is one turn in the conversation history.
type ChatHistoryEntry struct {
	RequestMessage string `json:"request_message,omitempty"`
	RequestNodes   []Node `json:"request_nodes,omitempty"`
	ResponseText   string `json:"response_text,omitempty"`
	ResponseNodes  []Node `json:"response_nodes,omitempty"`
}

// ToolDefinition describes a tool available to the model.
type ToolDefinition struct {
	Name            string                 `json:"name"`
	Description     string                 `json:"description,omitempty"`
	InputSchema     map[string]interface{} `json:"input_schema,omitempty"`
	InputSchemaJSON string                 `json:"input_schema_json,omitempty"` // JSON-encoded schema string
	Parameters      map[string]interface{} `json:"parameters,omitempty"`        // OpenAI-style alias
	McpServerName   string                 `json:"mcp_server_name,omitempty"`   // MCP server identifier
	McpToolName     string                 `json:"mcp_tool_name,omitempty"`     // MCP tool name
}

// EffectiveInputSchema returns the parsed input schema, preferring the
// already-decoded InputSchema over InputSchemaJSON.
func (t *ToolDefinition) EffectiveInputSchema() map[string]interface{} {
	if len(t.InputSchema) > 0 {
		return t.InputSchema
	}
	if len(t.Parameters) > 0 {
		return t.Parameters
	}
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}
