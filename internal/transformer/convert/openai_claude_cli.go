package convert

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/transformer"
)

// ========== 硬编码常量（必须与 Augment-BYOK v0.754.3 保持一致）==========
const (
	ClaudeCliVersion        = "2.1.2"
	AnthropicVersion        = "2023-06-01"
	DefaultCliMaxTokens     = 32000
	StainlessPackageVersion = "0.70.0"
	StainlessRuntimeVersion = "v24.3.0"
	StainlessLang           = "js"
	StainlessRuntime        = "node"
)

// RequiredCliBetas 必需的 Beta 功能
var RequiredCliBetas = []string{
	"claude-code-20250219",
	"interleaved-thinking-2025-05-14",
}

// CliSystemPrompt 硬编码的 CLI 身份声明
var CliSystemPrompt = map[string]interface{}{
	"type":          "text",
	"text":          "You are Claude Code, Anthropic's official CLI for Claude.",
	"cache_control": map[string]string{"type": "ephemeral"},
}

// ========== Session/User ID 管理 ==========
var (
	cliSessionID   string
	cliSessionOnce sync.Once
	cliUserID      string
	cliUserOnce    sync.Once
)

func getCliSessionID() string {
	cliSessionOnce.Do(func() {
		cliSessionID = generateCliUUID()
	})
	return cliSessionID
}

func getCliUserID() string {
	cliUserOnce.Do(func() {
		bytes := make([]byte, 32)
		rand.Read(bytes)
		cliUserID = hex.EncodeToString(bytes)
	})
	return cliUserID
}

func generateCliUUID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	h := hex.EncodeToString(bytes)
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

// ========== URL 构建 ==========

// BuildClaudeCliURL 构建 CLI 请求 URL（必须包含 ?beta=true）
func BuildClaudeCliURL(baseURL string) string {
	base := strings.TrimSuffix(baseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages?beta=true"
	}
	return base + "/v1/messages?beta=true"
}

// ========== Headers 构建 ==========

// BuildClaudeCliHeaders 构建 CLI 请求 Headers
func BuildClaudeCliHeaders(apiKey string, betas []string, stream bool) map[string]string {
	headers := map[string]string{
		"Content-Type":                "application/json",
		"anthropic-version":           AnthropicVersion,
		"anthropic-beta":              strings.Join(betas, ","),
		"x-api-key":                   apiKey,
		"x-app":                       "cli",
		"user-agent":                  fmt.Sprintf("claude-cli/%s (external, cli)", ClaudeCliVersion),
		"x-stainless-arch":            runtime.GOARCH,
		"x-stainless-lang":            StainlessLang,
		"x-stainless-os":              getCliOSName(),
		"x-stainless-package-version": StainlessPackageVersion,
		"x-stainless-retry-count":     "0",
		"x-stainless-runtime":         StainlessRuntime,
		"x-stainless-runtime-version": StainlessRuntimeVersion,
		"x-stainless-timeout":         "600",
		"connection":                  "keep-alive",
		"accept-encoding":             "gzip, deflate, br, zstd",
	}

	if stream {
		headers["x-stainless-helper-method"] = "stream"
		headers["accept"] = "application/json"
	}

	return headers
}

func getCliOSName() string {
	switch runtime.GOOS {
	case "darwin":
		return "MacOS"
	case "windows":
		return "Windows"
	default:
		return "Linux"
	}
}

// ========== Betas 构建 ==========

// BuildClaudeCliBetas 构建 CLI Betas 列表
func BuildClaudeCliBetas(tools []map[string]interface{}) []string {
	betas := make([]string, len(RequiredCliBetas))
	copy(betas, RequiredCliBetas)

	if len(tools) > 0 {
		betas = append(betas, "tool-examples-2025-10-29", "advanced-tool-use-2025-11-20")
	}

	for _, tool := range tools {
		name, _ := tool["name"].(string)
		switch name {
		case "MCPSearch":
			if !containsBeta(betas, "tool-search-tool-2025-10-19") {
				betas = append(betas, "tool-search-tool-2025-10-19")
			}
		case "WebSearch":
			if !containsBeta(betas, "web-search-2025-03-05") {
				betas = append(betas, "web-search-2025-03-05")
			}
		}
	}

	return betas
}

func containsBeta(betas []string, target string) bool {
	for _, b := range betas {
		if b == target {
			return true
		}
	}
	return false
}

// ========== 请求转换 ==========

// OpenAIReqToClaudeCLI 将 OpenAI 请求转换为 Claude CLI 格式
func OpenAIReqToClaudeCLI(openaiReq []byte, model, apiKey string) ([]byte, map[string]string, error) {
	// 错误处理：参数校验
	if model == "" {
		return nil, nil, fmt.Errorf("CLI: model is required")
	}
	if apiKey == "" {
		return nil, nil, fmt.Errorf("CLI: apiKey is required")
	}

	// Parse as map first to handle Cursor's Claude-format tools
	var reqMap map[string]interface{}
	if err := json.Unmarshal(openaiReq, &reqMap); err != nil {
		return nil, nil, fmt.Errorf("CLI: failed to parse request: %w", err)
	}

	// Also parse as struct for convenience
	var req transformer.OpenAIRequest
	if err := json.Unmarshal(openaiReq, &req); err != nil {
		return nil, nil, fmt.Errorf("CLI: failed to parse request: %w", err)
	}

	// 错误处理：消息校验
	if len(req.Messages) == 0 {
		return nil, nil, fmt.Errorf("CLI: messages cannot be empty")
	}

	// 1. 构建 System Prompt（硬编码 CLI 身份 + 用户自定义）
	system := []map[string]interface{}{CliSystemPrompt}
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			if content, ok := msg.Content.(string); ok && content != "" {
				system = append(system, map[string]interface{}{
					"type": "text",
					"text": content,
				})
			}
		}
	}

	// 2. 转换 Messages（排除 system）
	var messages []map[string]interface{}
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			continue
		}
		messages = append(messages, convertOpenAIMessageToClaudeCLI(msg))
	}

	// 3. 转换 Tools - handle both OpenAI format and Cursor's Claude format
	tools := []map[string]interface{}{} // 初始化为空数组，避免 json.Marshal 后变成 null
	if reqTools, ok := reqMap["tools"].([]interface{}); ok {
		for _, toolInterface := range reqTools {
			rawTool, ok := toolInterface.(map[string]interface{})
			if !ok {
				continue
			}

			var claudeTool map[string]interface{}

			// Check if it's already in Claude format (has "name" at top level)
			if name, hasName := rawTool["name"].(string); hasName && name != "" {
				// Claude format: {name, description, input_schema}
				claudeTool = map[string]interface{}{
					"name":         rawTool["name"],
					"description":  rawTool["description"],
					"input_schema": rawTool["input_schema"],
				}
			} else if rawTool["type"] == "function" {
				// OpenAI format: {type: "function", function: {name, description, parameters}}
				if funcObj, ok := rawTool["function"].(map[string]interface{}); ok {
					claudeTool = map[string]interface{}{
						"name":         funcObj["name"],
						"description":  funcObj["description"],
						"input_schema": funcObj["parameters"],
					}
				}
			}

			if claudeTool != nil {
				tools = append(tools, claudeTool)
			}
		}
	}

	// 4. 构建 Metadata
	metadata := map[string]interface{}{
		"user_id": fmt.Sprintf("user_%s_account__session_%s", getCliUserID(), getCliSessionID()),
	}

	// 5. 确定 max_tokens
	maxTokens := DefaultCliMaxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	} else if req.MaxCompletionTokens > 0 {
		maxTokens = req.MaxCompletionTokens
	}

	// 6. 构建 Body（按 CLI 字段顺序）
	cliReq := map[string]interface{}{
		"model":      model,
		"messages":   messages,
		"system":     system,
		"tools":      tools,
		"metadata":   metadata,
		"max_tokens": maxTokens,
		"stream":     req.Stream,
	}

	// 7. thinking 参数支持（配合 interleaved-thinking beta）
	if req.EnableThinking {
		cliReq["thinking"] = map[string]interface{}{
			"type":          "enabled",
			"budget_tokens": 10000,
		}
	}

	body, err := json.Marshal(cliReq)
	if err != nil {
		return nil, nil, err
	}

	// 8. 构建 Headers
	betas := BuildClaudeCliBetas(tools)
	headers := BuildClaudeCliHeaders(apiKey, betas, req.Stream)

	// 日志记录：请求转换完成
	logger.Debug("[CLI] Request transformed: model=%s, messages=%d, tools=%d, stream=%v, thinking=%v",
		model, len(messages), len(tools), req.Stream, req.EnableThinking)

	return body, headers, nil
}

// convertOpenAIMessageToClaudeCLI 转换单条消息
func convertOpenAIMessageToClaudeCLI(msg transformer.OpenAIMessage) map[string]interface{} {
	result := map[string]interface{}{"role": msg.Role}

	// 处理 tool 消息 → user + tool_result
	if msg.Role == "tool" {
		result["role"] = "user"
		result["content"] = []map[string]interface{}{
			{
				"type":        "tool_result",
				"tool_use_id": msg.ToolCallID,
				"content":     msg.Content,
			},
		}
		return result
	}

	// 处理 assistant 的 tool_calls → tool_use
	if len(msg.ToolCalls) > 0 {
		var content []map[string]interface{}
		if text, ok := msg.Content.(string); ok && text != "" {
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": text,
			})
		}
		for _, tc := range msg.ToolCalls {
			var input interface{}
			json.Unmarshal([]byte(tc.Function.Arguments), &input)
			content = append(content, map[string]interface{}{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Function.Name,
				"input": input,
			})
		}
		result["content"] = content
		return result
	}

	// 普通消息
	result["content"] = msg.Content
	return result
}
