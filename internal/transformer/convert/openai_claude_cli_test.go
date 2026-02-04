package convert

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAIReqToClaudeCLI_Basic(t *testing.T) {
	openaiReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Hello, Claude!"}
		],
		"stream": true
	}`

	body, headers, err := OpenAIReqToClaudeCLI([]byte(openaiReq), "claude-sonnet-4-20250514", "test-api-key")
	if err != nil {
		t.Fatalf("OpenAIReqToClaudeCLI failed: %v", err)
	}

	// 验证 body
	var cliReq map[string]interface{}
	if err := json.Unmarshal(body, &cliReq); err != nil {
		t.Fatalf("Failed to unmarshal CLI request: %v", err)
	}

	// 验证 model
	if cliReq["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("Expected model 'claude-sonnet-4-20250514', got %v", cliReq["model"])
	}

	// 验证 messages
	messages, ok := cliReq["messages"].([]interface{})
	if !ok || len(messages) != 1 {
		t.Errorf("Expected 1 message, got %v", cliReq["messages"])
	}

	// 验证 system prompt 包含 CLI 身份声明
	system, ok := cliReq["system"].([]interface{})
	if !ok || len(system) < 1 {
		t.Fatalf("Expected system to be an array with at least 1 element")
	}
	firstSystem := system[0].(map[string]interface{})
	text, _ := firstSystem["text"].(string)
	if !strings.Contains(text, "Claude Code") {
		t.Errorf("Expected system prompt to contain 'Claude Code', got %v", text)
	}

	// 验证 headers
	if headers["x-api-key"] != "test-api-key" {
		t.Errorf("Expected x-api-key 'test-api-key', got %v", headers["x-api-key"])
	}
	if headers["anthropic-version"] != AnthropicVersion {
		t.Errorf("Expected anthropic-version '%s', got %v", AnthropicVersion, headers["anthropic-version"])
	}
	if !strings.Contains(headers["anthropic-beta"], "claude-code-20250219") {
		t.Errorf("Expected anthropic-beta to contain 'claude-code-20250219', got %v", headers["anthropic-beta"])
	}
}

func TestOpenAIReqToClaudeCLI_WithSystemPrompt(t *testing.T) {
	openaiReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Hello"}
		],
		"stream": true
	}`

	body, _, err := OpenAIReqToClaudeCLI([]byte(openaiReq), "claude-sonnet-4-20250514", "test-api-key")
	if err != nil {
		t.Fatalf("OpenAIReqToClaudeCLI failed: %v", err)
	}

	var cliReq map[string]interface{}
	if err := json.Unmarshal(body, &cliReq); err != nil {
		t.Fatalf("Failed to unmarshal CLI request: %v", err)
	}

	// 验证 system prompt 包含 CLI 身份声明 + 用户自定义
	system, ok := cliReq["system"].([]interface{})
	if !ok || len(system) != 2 {
		t.Fatalf("Expected 2 system elements, got %d", len(system))
	}

	// 第一个是 CLI 身份声明
	firstSystem := system[0].(map[string]interface{})
	if !strings.Contains(firstSystem["text"].(string), "Claude Code") {
		t.Errorf("First system element should be CLI identity")
	}

	// 第二个是用户自定义
	secondSystem := system[1].(map[string]interface{})
	if secondSystem["text"] != "You are a helpful assistant." {
		t.Errorf("Second system element should be user custom, got %v", secondSystem["text"])
	}

	// 验证 messages 不包含 system
	messages, _ := cliReq["messages"].([]interface{})
	for _, msg := range messages {
		m := msg.(map[string]interface{})
		if m["role"] == "system" {
			t.Errorf("Messages should not contain system role")
		}
	}
}

func TestOpenAIReqToClaudeCLI_WithTools(t *testing.T) {
	openaiReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Read file /tmp/test.txt"}
		],
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "read_file",
					"description": "Read a file",
					"parameters": {"type": "object", "properties": {"path": {"type": "string"}}}
				}
			}
		],
		"stream": true
	}`

	body, headers, err := OpenAIReqToClaudeCLI([]byte(openaiReq), "claude-sonnet-4-20250514", "test-api-key")
	if err != nil {
		t.Fatalf("OpenAIReqToClaudeCLI failed: %v", err)
	}

	var cliReq map[string]interface{}
	if err := json.Unmarshal(body, &cliReq); err != nil {
		t.Fatalf("Failed to unmarshal CLI request: %v", err)
	}

	// 验证 tools 转换
	tools, ok := cliReq["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %v", cliReq["tools"])
	}

	tool := tools[0].(map[string]interface{})
	if tool["name"] != "read_file" {
		t.Errorf("Expected tool name 'read_file', got %v", tool["name"])
	}
	if tool["input_schema"] == nil {
		t.Errorf("Expected tool to have input_schema")
	}

	// 验证 betas 包含 tool 相关 beta
	beta := headers["anthropic-beta"]
	if !strings.Contains(beta, "tool-examples-2025-10-29") {
		t.Errorf("Expected beta to contain 'tool-examples-2025-10-29', got %v", beta)
	}
	if !strings.Contains(beta, "advanced-tool-use-2025-11-20") {
		t.Errorf("Expected beta to contain 'advanced-tool-use-2025-11-20', got %v", beta)
	}
}

func TestOpenAIReqToClaudeCLI_WithToolCall(t *testing.T) {
	openaiReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Read file"},
			{"role": "assistant", "content": "", "tool_calls": [
				{"id": "call_123", "type": "function", "function": {"name": "read_file", "arguments": "{\"path\":\"/tmp/test.txt\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_123", "content": "file content here"}
		],
		"stream": true
	}`

	body, _, err := OpenAIReqToClaudeCLI([]byte(openaiReq), "claude-sonnet-4-20250514", "test-api-key")
	if err != nil {
		t.Fatalf("OpenAIReqToClaudeCLI failed: %v", err)
	}

	var cliReq map[string]interface{}
	if err := json.Unmarshal(body, &cliReq); err != nil {
		t.Fatalf("Failed to unmarshal CLI request: %v", err)
	}

	messages, _ := cliReq["messages"].([]interface{})
	if len(messages) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(messages))
	}

	// 验证 assistant 消息包含 tool_use
	assistantMsg := messages[1].(map[string]interface{})
	content, _ := assistantMsg["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("Expected 1 content block in assistant message, got %d", len(content))
	}
	toolUse := content[0].(map[string]interface{})
	if toolUse["type"] != "tool_use" {
		t.Errorf("Expected tool_use type, got %v", toolUse["type"])
	}

	// 验证 tool 消息转换为 user + tool_result
	toolMsg := messages[2].(map[string]interface{})
	if toolMsg["role"] != "user" {
		t.Errorf("Expected tool message to be converted to user role, got %v", toolMsg["role"])
	}
	toolContent, _ := toolMsg["content"].([]interface{})
	if len(toolContent) != 1 {
		t.Fatalf("Expected 1 content block in tool message, got %d", len(toolContent))
	}
	toolResult := toolContent[0].(map[string]interface{})
	if toolResult["type"] != "tool_result" {
		t.Errorf("Expected tool_result type, got %v", toolResult["type"])
	}
}

func TestOpenAIReqToClaudeCLI_WithThinking(t *testing.T) {
	openaiReq := `{
		"model": "claude-sonnet-4-20250514",
		"messages": [
			{"role": "user", "content": "Think about this"}
		],
		"enable_thinking": true,
		"stream": true
	}`

	body, _, err := OpenAIReqToClaudeCLI([]byte(openaiReq), "claude-sonnet-4-20250514", "test-api-key")
	if err != nil {
		t.Fatalf("OpenAIReqToClaudeCLI failed: %v", err)
	}

	var cliReq map[string]interface{}
	if err := json.Unmarshal(body, &cliReq); err != nil {
		t.Fatalf("Failed to unmarshal CLI request: %v", err)
	}

	// 验证 thinking 参数
	thinking, ok := cliReq["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected thinking to be a map")
	}
	if thinking["type"] != "enabled" {
		t.Errorf("Expected thinking type 'enabled', got %v", thinking["type"])
	}
	if thinking["budget_tokens"] != float64(10000) {
		t.Errorf("Expected budget_tokens 10000, got %v", thinking["budget_tokens"])
	}
}

func TestOpenAIReqToClaudeCLI_ErrorCases(t *testing.T) {
	// 测试空 model
	_, _, err := OpenAIReqToClaudeCLI([]byte(`{"messages":[{"role":"user","content":"hi"}]}`), "", "test-key")
	if err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Errorf("Expected 'model is required' error, got %v", err)
	}

	// 测试空 apiKey
	_, _, err = OpenAIReqToClaudeCLI([]byte(`{"messages":[{"role":"user","content":"hi"}]}`), "model", "")
	if err == nil || !strings.Contains(err.Error(), "apiKey is required") {
		t.Errorf("Expected 'apiKey is required' error, got %v", err)
	}

	// 测试空 messages
	_, _, err = OpenAIReqToClaudeCLI([]byte(`{"messages":[]}`), "model", "key")
	if err == nil || !strings.Contains(err.Error(), "messages cannot be empty") {
		t.Errorf("Expected 'messages cannot be empty' error, got %v", err)
	}

	// 测试无效 JSON
	_, _, err = OpenAIReqToClaudeCLI([]byte(`invalid json`), "model", "key")
	if err == nil || !strings.Contains(err.Error(), "failed to parse request") {
		t.Errorf("Expected parse error, got %v", err)
	}
}

func TestBuildClaudeCliBetas(t *testing.T) {
	// 无 tools
	betas := BuildClaudeCliBetas(nil)
	if len(betas) != 2 {
		t.Errorf("Expected 2 required betas, got %d", len(betas))
	}

	// 有 tools
	tools := []map[string]interface{}{
		{"name": "read_file"},
	}
	betas = BuildClaudeCliBetas(tools)
	if len(betas) != 4 {
		t.Errorf("Expected 4 betas with tools, got %d", len(betas))
	}

	// 有 MCPSearch 工具
	tools = []map[string]interface{}{
		{"name": "MCPSearch"},
	}
	betas = BuildClaudeCliBetas(tools)
	found := false
	for _, b := range betas {
		if b == "tool-search-tool-2025-10-19" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'tool-search-tool-2025-10-19' beta for MCPSearch tool")
	}

	// 有 WebSearch 工具
	tools = []map[string]interface{}{
		{"name": "WebSearch"},
	}
	betas = BuildClaudeCliBetas(tools)
	found = false
	for _, b := range betas {
		if b == "web-search-2025-03-05" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'web-search-2025-03-05' beta for WebSearch tool")
	}
}

func TestBuildClaudeCliHeaders(t *testing.T) {
	betas := []string{"beta1", "beta2"}
	headers := BuildClaudeCliHeaders("test-key", betas, true)

	// 验证基础 headers
	if headers["x-api-key"] != "test-key" {
		t.Errorf("Expected x-api-key 'test-key', got %v", headers["x-api-key"])
	}
	if headers["anthropic-version"] != AnthropicVersion {
		t.Errorf("Expected anthropic-version '%s', got %v", AnthropicVersion, headers["anthropic-version"])
	}
	if headers["anthropic-beta"] != "beta1,beta2" {
		t.Errorf("Expected anthropic-beta 'beta1,beta2', got %v", headers["anthropic-beta"])
	}

	// 验证 stream=true 时的额外 headers
	if headers["x-stainless-helper-method"] != "stream" {
		t.Errorf("Expected x-stainless-helper-method 'stream', got %v", headers["x-stainless-helper-method"])
	}

	// 验证 CLI headers
	if headers["x-app"] != "cli" {
		t.Errorf("Expected x-app 'cli', got %v", headers["x-app"])
	}
	if !strings.Contains(headers["user-agent"], "claude-cli/") {
		t.Errorf("Expected user-agent to contain 'claude-cli/', got %v", headers["user-agent"])
	}
}
