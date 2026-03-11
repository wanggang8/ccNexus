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
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			logger.Warn("[CLI] Failed to generate random user ID: %v", err)
		}
		cliUserID = hex.EncodeToString(b)
	})
	return cliUserID
}

func generateCliUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		logger.Warn("[CLI] Failed to generate random UUID: %v", err)
	}
	h := hex.EncodeToString(b)
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

	// 2. 转换 Messages（排除 system），并合并连续 tool 消息（并行工具调用）
	var messages []map[string]interface{}
	i := 0
	for i < len(req.Messages) {
		msg := req.Messages[i]
		if msg.Role == "system" {
			i++
			continue
		}
		if msg.Role == "tool" {
			// 收集所有连续的 tool 消息，合并到同一个 user 消息中
			// Claude API 要求：多个并行工具调用的结果必须在同一条 user 消息内
			var toolResults []map[string]interface{}
			for i < len(req.Messages) && req.Messages[i].Role == "tool" {
				toolResults = append(toolResults, map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": req.Messages[i].ToolCallID,
					"content":     req.Messages[i].Content,
				})
				i++
			}
			messages = append(messages, map[string]interface{}{
				"role":    "user",
				"content": toolResults,
			})
		} else {
			messages = append(messages, convertOpenAIMessageToClaudeCLI(msg))
			i++
		}
	}

	// 过滤后消息为空（如请求仅含 system 角色）则拒绝
	if len(messages) == 0 {
		return nil, nil, fmt.Errorf("CLI: no non-system messages to send")
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

	// 转发 temperature（与 OpenAIReqToClaude 保持一致）
	if req.Temperature != nil {
		cliReq["temperature"] = *req.Temperature
	}

	// 转发 tool_choice（OpenAI → Claude 格式转换）
	if req.ToolChoice != nil && len(tools) > 0 {
		switch tc := req.ToolChoice.(type) {
		case string:
			switch tc {
			case "required":
				cliReq["tool_choice"] = map[string]interface{}{"type": "any"}
			case "auto":
				cliReq["tool_choice"] = map[string]interface{}{"type": "auto"}
			case "none":
				// Claude 无 none 选项，不设置（工具已在 tools 中，不传 tool_choice 默认 auto）
			}
		case map[string]interface{}:
			if tc["type"] == "function" {
				if fn, ok := tc["function"].(map[string]interface{}); ok {
					if name, ok := fn["name"].(string); ok && name != "" {
						cliReq["tool_choice"] = map[string]interface{}{"type": "tool", "name": name}
					}
				}
			}
		}
	}

	// 7. thinking 参数支持（配合 interleaved-thinking beta）
	// Claude API 要求 budget_tokens < max_tokens
	if req.EnableThinking {
		budgetTokens := 10000
		if budgetTokens >= maxTokens {
			budgetTokens = maxTokens - 1
		}
		if budgetTokens > 0 {
			cliReq["thinking"] = map[string]interface{}{
				"type":          "enabled",
				"budget_tokens": budgetTokens,
			}
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
	logger.Debug("[CLI] 请求已转换: 模型=%s, 消息数=%d, 工具数=%d, 流式=%v, 思考=%v",
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
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
				logger.Warn("Failed to unmarshal tool arguments: %v, using empty object", err)
				input = map[string]interface{}{}
			}
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

	// 普通消息：对多部分内容进行格式转换（兼容 OpenAI image_url 和 Claude image 两种格式）
	if arr, ok := msg.Content.([]interface{}); ok {
		result["content"] = convertMixedContentForCLI(arr)
	} else if msg.Content != nil {
		result["content"] = msg.Content
	} else {
		// nil content → 空字符串，避免 JSON 序列化为 null 被 Claude API 拒绝
		result["content"] = ""
	}
	return result
}

// convertMixedContentForCLI 转换多部分内容，兼容 OpenAI 和 Claude 两种格式
// - OpenAI image_url 格式 → Claude image 格式
// - Claude image 格式 → 直接透传
// - text、tool_result 等 → 直接透传
func convertMixedContentForCLI(arr []interface{}) []map[string]interface{} {
	var result []map[string]interface{}
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		switch m["type"] {
		case "text":
			result = append(result, map[string]interface{}{"type": "text", "text": m["text"]})
		case "image_url":
			// OpenAI 格式 → Claude 格式
			if urlObj, ok := m["image_url"].(map[string]interface{}); ok {
				if url, ok := urlObj["url"].(string); ok {
					if strings.HasPrefix(url, "data:") {
						// base64 data URL → Claude base64 source
						parts := strings.SplitN(url, ",", 2)
						if len(parts) == 2 {
							mediaType := strings.TrimPrefix(strings.Split(parts[0], ";")[0], "data:")
							result = append(result, map[string]interface{}{
								"type": "image",
								"source": map[string]interface{}{
									"type":       "base64",
									"media_type": mediaType,
									"data":       parts[1],
								},
							})
						}
					} else if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
						// External URL → Claude url source
						result = append(result, map[string]interface{}{
							"type": "image",
							"source": map[string]interface{}{
								"type": "url",
								"url":  url,
							},
						})
					}
				}
			}
		case "image":
			// 已是 Claude 格式，直接透传
			result = append(result, m)
		case "tool_result":
			// Claude 格式 tool_result，直接透传
			result = append(result, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": m["tool_use_id"],
				"content":     m["content"],
			})
		default:
			// 其他类型直接透传
			result = append(result, m)
		}
	}
	return result
}
