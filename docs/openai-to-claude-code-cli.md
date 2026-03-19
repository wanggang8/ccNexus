# OpenAI → Claude Code CLI 格式规范（基于当前代码）

本文档仅以本仓库当前实现为准，覆盖 `OpenAI 请求 → Claude Code CLI 请求` 的转换规则。

## 1. 适用范围

- **Codex Chat 路径（OpenAI 输入）**：
  - `internal/transformer/cx/chat/cli.go`
  - 调用 `convert.OpenAIReqToClaudeCLI(...)`
- **Claude Code 路径（Claude 输入）**：
  - `internal/transformer/cc/cli.go`
  - 不是 OpenAI→CLI 转换，而是 Claude→CLI 增强透传

## 2. 核心实现文件

- `internal/transformer/convert/openai_claude_cli.go`

## 3. 硬编码常量（当前实现）

```go
const (
	ClaudeCliVersion        = "2.1.2"
	AnthropicVersion        = "2023-06-01"
	DefaultCliMaxTokens     = 32000
	StainlessPackageVersion = "0.70.0"
	StainlessRuntimeVersion = "v24.3.0"
	StainlessLang           = "js"
	StainlessRuntime        = "node"
)
```

必需 Betas：

```go
var RequiredCliBetas = []string{
	"claude-code-20250219",
	"interleaved-thinking-2025-05-14",
}
```

CLI system prompt：

```go
var CliSystemPrompt = map[string]interface{}{
	"type":          "text",
	"text":          "You are Claude Code, Anthropic's official CLI for Claude.",
	"cache_control": map[string]string{"type": "ephemeral"},
}
```

## 4. URL 规则

```text
{baseUrl}/v1/messages?beta=true
```

由 `BuildClaudeCliURL` 构建，若 `baseUrl` 已以 `/v1` 结尾则不会重复追加。

## 5. Header 规则

由 `BuildClaudeCliHeaders` 生成，关键字段包括：

- `anthropic-version: 2023-06-01`
- `anthropic-beta: ...`
- `x-api-key`
- `x-app: cli`
- `user-agent: claude-cli/2.1.2 (external, cli)`
- `x-stainless-*` 系列字段

当 `stream=true` 时附加：

- `x-stainless-helper-method: stream`
- `accept: application/json`

## 6. Body 结构（OpenAI → CLI）

`OpenAIReqToClaudeCLI` 产出的核心字段：

```json
{
  "model": "...",
  "messages": [],
  "system": [],
  "tools": [],
  "metadata": {
    "user_id": "user_{userId}_account__session_{sessionId}"
  },
  "max_tokens": 32000,
  "stream": true
}
```

说明：

- `system` 第一个元素固定注入 `CliSystemPrompt`
- `tools` 即使为空也会输出空数组
- `max_tokens` 默认 `32000`
- `metadata.user_id` 当前实现中的 `account` 部分为空（`account__`）

## 7. 消息与工具转换要点

- `system` 角色消息会合并进 `system` 数组，不进入 `messages`
- 连续 `tool` 角色消息会合并成同一条 Claude `user` 消息中的多个 `tool_result`
- 工具定义支持两种输入：
  - OpenAI 风格：`{type:"function", function:{...}}`
  - Claude 风格：`{name, description, input_schema}`（兼容 Cursor 混合输入）

## 8. Beta 增量规则

在必需 Betas 基础上：

- 有工具时增加：
  - `tool-examples-2025-10-29`
  - `advanced-tool-use-2025-11-20`
- 若工具名包含：
  - `MCPSearch` → `tool-search-tool-2025-10-19`
  - `WebSearch` → `web-search-2025-03-05`

## 9. 响应转换

- `cx/chat/cli.go` 中：
  - 非流式：`ClaudeRespToOpenAI`
  - 流式：`ClaudeStreamToOpenAI`
- `cc/cli.go` 中：
  - 响应为透传（Claude Code 客户端本身期望 Claude 形态）
