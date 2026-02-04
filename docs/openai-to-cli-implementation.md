# OpenAI → Claude Code CLI 转换实现方案

> 基于 [Augment-BYOK v0.754.3](https://github.com/wanggang8/Augment-BYOK/tree/v0.754.3) 的 CLI 格式规范和本项目架构设计。

---

## 1. 文件结构

```
internal/transformer/
├── types.go                        # 可选：新增 ClaudeCLIRequest 类型
├── convert/
│   └── openai_claude_cli.go        # 核心转换逻辑（新建）
└── cc/
    └── cli.go                      # CLI Transformer 封装（新建）
```

---

## 2. 硬编码常量

**所有值必须与 Augment-BYOK 保持一致**：

```go
const (
    // CLI 版本
    ClaudeCliVersion = "2.1.2"
    
    // API 版本
    AnthropicVersion = "2023-06-01"
    
    // 默认 max_tokens
    DefaultMaxTokens = 32000
    
    // Stainless SDK 版本（硬编码，不使用 runtime.Version()）
    StainlessPackageVersion = "0.70.0"
    StainlessRuntimeVersion = "v24.3.0"
    StainlessLang           = "js"     // 硬编码，不改为 go
    StainlessRuntime        = "node"   // 硬编码，不改为 go
)

// 必需的 Beta 功能
var RequiredBetas = []string{
    "claude-code-20250219",
    "interleaved-thinking-2025-05-14",
}

// CLI System Prompt（硬编码）
var CliSystemPrompt = map[string]interface{}{
    "type": "text",
    "text": "You are Claude Code, Anthropic's official CLI for Claude.",
    "cache_control": map[string]string{"type": "ephemeral"},
}
```

---

## 3. 核心实现代码

### 3.1 `convert/openai_claude_cli.go`

```go
package convert

import (
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "runtime"
    "strings"
    "sync"

    "github.com/lich0821/ccNexus/internal/transformer"
)

// ========== 硬编码常量 ==========
const (
    ClaudeCliVersion        = "2.1.2"
    AnthropicVersion        = "2023-06-01"
    DefaultMaxTokens        = 32000
    StainlessPackageVersion = "0.70.0"
    StainlessRuntimeVersion = "v24.3.0"
    StainlessLang           = "js"
    StainlessRuntime        = "node"
)

var RequiredBetas = []string{
    "claude-code-20250219",
    "interleaved-thinking-2025-05-14",
}

var CliSystemPrompt = map[string]interface{}{
    "type": "text",
    "text": "You are Claude Code, Anthropic's official CLI for Claude.",
    "cache_control": map[string]string{"type": "ephemeral"},
}

// ========== Session/User ID 管理 ==========
var (
    sessionID   string
    sessionOnce sync.Once
    userID      string
    userOnce    sync.Once
)

func getSessionID() string {
    sessionOnce.Do(func() {
        sessionID = generateUUID()
    })
    return sessionID
}

func getUserID() string {
    userOnce.Do(func() {
        bytes := make([]byte, 32)
        rand.Read(bytes)
        userID = hex.EncodeToString(bytes)
    })
    return userID
}

func generateUUID() string {
    bytes := make([]byte, 16)
    rand.Read(bytes)
    h := hex.EncodeToString(bytes)
    return fmt.Sprintf("%s-%s-%s-%s-%s",
        h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

// ========== URL 构建 ==========
func BuildClaudeCliURL(baseURL string) string {
    base := strings.TrimSuffix(baseURL, "/")
    if !strings.HasSuffix(base, "/v1") {
        base = base + "/v1"
    }
    return base + "/messages?beta=true"
}

// ========== Headers 构建 ==========
func BuildClaudeCliHeaders(apiKey string, betas []string, stream bool) map[string]string {
    headers := map[string]string{
        "Content-Type":                "application/json",
        "anthropic-version":           AnthropicVersion,
        "anthropic-beta":              strings.Join(betas, ","),
        "x-api-key":                   apiKey,
        "x-app":                       "cli",
        "user-agent":                  fmt.Sprintf("claude-cli/%s (external, cli)", ClaudeCliVersion),
        "x-stainless-arch":            runtime.GOARCH,
        "x-stainless-lang":            StainlessLang,   // 硬编码 js
        "x-stainless-os":              getOSName(),
        "x-stainless-package-version": StainlessPackageVersion,
        "x-stainless-retry-count":     "0",
        "x-stainless-runtime":         StainlessRuntime,        // 硬编码 node
        "x-stainless-runtime-version": StainlessRuntimeVersion, // 硬编码 v24.3.0
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

func getOSName() string {
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
func BuildClaudeCliBetas(tools []map[string]interface{}) []string {
    betas := make([]string, len(RequiredBetas))
    copy(betas, RequiredBetas)

    if len(tools) > 0 {
        betas = append(betas, "tool-examples-2025-10-29", "advanced-tool-use-2025-11-20")
    }

    for _, tool := range tools {
        name, _ := tool["name"].(string)
        switch name {
        case "MCPSearch":
            betas = append(betas, "tool-search-tool-2025-10-19")
        case "WebSearch":
            betas = append(betas, "web-search-2025-03-05")
        }
    }

    return betas
}

// ========== 请求转换 ==========

// OpenAIReqToClaudeCLI 将 OpenAI 请求转换为 Claude CLI 格式
// 返回：body, headers, error
func OpenAIReqToClaudeCLI(openaiReq []byte, model, apiKey string) ([]byte, map[string]string, error) {
    var req transformer.OpenAIRequest
    if err := json.Unmarshal(openaiReq, &req); err != nil {
        return nil, nil, err
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

    // 3. 转换 Tools
    var tools []map[string]interface{}
    for _, tool := range req.Tools {
        if tool.Type == "function" {
            tools = append(tools, map[string]interface{}{
                "name":         tool.Function.Name,
                "description":  tool.Function.Description,
                "input_schema": tool.Function.Parameters,
            })
        }
    }

    // 4. 构建 Metadata
    metadata := map[string]interface{}{
        "user_id": fmt.Sprintf("user_%s_account__session_%s", getUserID(), getSessionID()),
    }

    // 5. 确定 max_tokens
    maxTokens := DefaultMaxTokens
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
        "tools":      tools, // 即使为空也包含
        "metadata":   metadata,
        "max_tokens": maxTokens,
        "stream":     req.Stream,
    }

    body, err := json.Marshal(cliReq)
    if err != nil {
        return nil, nil, err
    }

    // 7. 构建 Headers
    betas := BuildClaudeCliBetas(tools)
    headers := BuildClaudeCliHeaders(apiKey, betas, req.Stream)

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
        // 如果有文本内容，先添加
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
```

### 3.2 `cc/cli.go`

```go
package cc

import (
    "github.com/lich0821/ccNexus/internal/transformer"
    "github.com/lich0821/ccNexus/internal/transformer/convert"
)

// CLITransformer 将 OpenAI 请求转换为 Claude Code CLI 格式
type CLITransformer struct {
    model   string
    apiKey  string
}

// NewCLITransformer 创建 CLI 转换器
func NewCLITransformer(model, apiKey string) *CLITransformer {
    return &CLITransformer{model: model, apiKey: apiKey}
}

func (t *CLITransformer) Name() string {
    return "openai_to_cli"
}

// TransformRequest 转换请求（返回 body）
func (t *CLITransformer) TransformRequest(req []byte) ([]byte, error) {
    body, _, err := convert.OpenAIReqToClaudeCLI(req, t.model, t.apiKey)
    return body, err
}

// TransformRequestWithHeaders 转换请求并返回 headers
func (t *CLITransformer) TransformRequestWithHeaders(req []byte) ([]byte, map[string]string, error) {
    return convert.OpenAIReqToClaudeCLI(req, t.model, t.apiKey)
}

// GetURL 获取 CLI 请求 URL
func (t *CLITransformer) GetURL(baseURL string) string {
    return convert.BuildClaudeCliURL(baseURL)
}

// TransformResponse 转换响应（CLI → OpenAI）
func (t *CLITransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
    if isStreaming {
        return nil, nil
    }
    return convert.ClaudeRespToOpenAI(resp, t.model)
}

// TransformResponseWithContext 转换流式响应
func (t *CLITransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
    if isStreaming {
        return convert.ClaudeStreamToOpenAI(resp, ctx, t.model)
    }
    return convert.ClaudeRespToOpenAI(resp, t.model)
}
```

---

## 4. 使用示例

```go
// 创建转换器
cliTransformer := cc.NewCLITransformer("claude-sonnet-4-20250514", "sk-ant-xxx")

// 转换请求
openaiReq := []byte(`{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
}`)

body, headers, err := cliTransformer.TransformRequestWithHeaders(openaiReq)
if err != nil {
    log.Fatal(err)
}

// 获取 URL
url := cliTransformer.GetURL("https://api.anthropic.com")
// url = "https://api.anthropic.com/v1/messages?beta=true"

// 发起请求
req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
for k, v := range headers {
    req.Header.Set(k, v)
}
```

---

## 5. 关键注意事项

1. **硬编码值不可修改**：
   - `x-stainless-lang: js`
   - `x-stainless-runtime: node`
   - `x-stainless-runtime-version: v24.3.0`
   - `user-agent: claude-cli/2.1.2 (external, cli)`

2. **URL 必须包含 `?beta=true`**

3. **System Prompt 第一个元素必须是硬编码的 CLI 身份声明**

4. **`tools` 字段必须存在**（即使为空数组）

5. **`metadata.user_id` 格式**：`user_{userId}_account_{accountUuid}_session_{sessionId}`

---

## 6. 参考资料

- **CLI 格式规范**: `docs/openai-to-claude-code-cli.md`
- **Augment-BYOK 源码**: `anthropic-claude-code.js`
- **项目转换架构**: `internal/transformer/`
