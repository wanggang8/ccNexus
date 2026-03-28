package augment

func attachToolUseMCPMetadata(toolUse map[string]interface{}, toolName string, toolCtx map[string]*ToolContext) {
	if toolUse == nil || toolCtx == nil {
		return
	}
	ctx, ok := toolCtx[toolName]
	if !ok || ctx == nil {
		return
	}
	if ctx.McpServerName != "" {
		toolUse["mcp_server_name"] = ctx.McpServerName
	}
	if ctx.McpToolName != "" {
		toolUse["mcp_tool_name"] = ctx.McpToolName
	}
}
