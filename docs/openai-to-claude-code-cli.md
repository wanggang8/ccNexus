# Claude Code CLI 请求/响应格式规范

> 本文档完全基于 [Augment-BYOK v0.754.3](https://github.com/wanggang8/Augment-BYOK/tree/v0.754.3) 项目 `anthropic-claude-code.js` 的实现整理。

---

## 1. 硬编码常量

以下常量在项目中**硬编码**，必须保留：

```javascript
const CLAUDE_CLI_VERSION = "2.1.2";
const ANTHROPIC_VERSION = "2023-06-01";
const DEFAULT_MAX_TOKENS = 32000;

// Stainless SDK 版本信息
const STAINLESS_PACKAGE_VERSION = "0.70.0";
const STAINLESS_RUNTIME_VERSION = "v24.3.0";

// 必需的 Beta 功能
const REQUIRED_BETAS = [
  "claude-code-20250219",
  "interleaved-thinking-2025-05-14"
];

// CLI System Prompt（硬编码）
const CLI_SYSTEM_PROMPT = {
  type: "text",
  text: "You are Claude Code, Anthropic's official CLI for Claude.",
  cache_control: { type: "ephemeral" }
};
```

---

## 2. 请求 URL

```
POST {baseUrl}/messages?beta=true
```

**必须**包含 `?beta=true` 查询参数。

---

## 3. 请求 Headers

### 3.1 完整 Headers（默认）

```http
Content-Type: application/json
anthropic-version: 2023-06-01
anthropic-beta: claude-code-20250219,interleaved-thinking-2025-05-14
x-api-key: {API_KEY}
x-app: cli
user-agent: claude-cli/2.1.2 (external, cli)
x-stainless-arch: arm64
x-stainless-lang: js
x-stainless-os: MacOS
x-stainless-package-version: 0.70.0
x-stainless-retry-count: 0
x-stainless-runtime: node
x-stainless-runtime-version: v24.3.0
x-stainless-timeout: 600
connection: keep-alive
accept-encoding: gzip, deflate, br, zstd
```

### 3.2 流式请求额外 Headers

当 `stream === true` 时，添加：

```http
x-stainless-helper-method: stream
accept: application/json
```

### 3.3 Browser 模式 Header（可选）

当 `dangerouslyAllowBrowser === true` 时：

```http
anthropic-dangerous-direct-browser-access: true
```

### 3.4 User-Agent 格式

```javascript
function getClaudeCliUserAgent() {
  const entrypoint = process.env.CLAUDE_CODE_ENTRYPOINT || "cli";
  return `claude-cli/${CLAUDE_CLI_VERSION} (external, ${entrypoint})`;
}
// 输出: "claude-cli/2.1.2 (external, cli)"
```

### 3.5 操作系统映射

```javascript
// x-stainless-os 值
process.platform === "darwin"  → "MacOS"
process.platform === "win32"   → "Windows"
其他                           → "Linux"
```

---

## 4. 请求 Body

### 4.1 完整结构

```json
{
  "model": "claude-sonnet-4-20250514",
  "messages": [
    {"role": "user", "content": "Hello"}
  ],
  "system": [
    {
      "type": "text",
      "text": "You are Claude Code, Anthropic's official CLI for Claude.",
      "cache_control": { "type": "ephemeral" }
    }
  ],
  "tools": [],
  "metadata": {
    "user_id": "user_{userId}_account_{accountUuid}_session_{sessionId}"
  },
  "max_tokens": 32000,
  "stream": true
}
```

### 4.2 字段顺序

项目中按以下顺序构建 Body：

1. `model`
2. `messages`
3. `system`
4. `tools`
5. `metadata`
6. `max_tokens`
7. `stream`
8. 其他 `requestDefaults` 字段

### 4.3 System Prompt 格式

**第一个元素必须是硬编码的 CLI 身份声明**：

```json
{
  "system": [
    {
      "type": "text",
      "text": "You are Claude Code, Anthropic's official CLI for Claude.",
      "cache_control": { "type": "ephemeral" }
    },
    {
      "type": "text",
      "text": "用户自定义的 system prompt（可选）"
    }
  ]
}
```

构建逻辑：

```javascript
const cliSystem = [
  {
    type: "text",
    text: "You are Claude Code, Anthropic's official CLI for Claude.",
    cache_control: { type: "ephemeral" }
  }
];
if (system) {
  if (typeof system === "string" && system.trim()) {
    cliSystem.push({ type: "text", text: system.trim() });
  } else if (Array.isArray(system)) {
    cliSystem.push(...system);
  }
}
body.system = cliSystem;
```

### 4.4 Metadata 格式

```json
{
  "metadata": {
    "user_id": "user_{userId}_account_{accountUuid}_session_{sessionId}"
  }
}
```

生成逻辑：

```javascript
// userId: 64 位 hex，持久化存储
const userId = crypto.randomBytes(32).toString("hex");

// sessionId: UUID 格式，进程级别（每次启动生成一次）
function generateSessionId() {
  const bytes = crypto.randomBytes(16);
  const hex = bytes.toString("hex");
  return `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20,32)}`;
}

// accountUuid: 从 requestDefaults 获取
const accountUuid = requestDefaults?.metadata?.account_uuid || "";

// 最终格式
metadata.user_id = `user_${userId}_account_${accountUuid}_session_${sessionId}`;
```

### 4.5 Tools 字段

**必须**包含 `tools` 字段，即使为空：

```json
{
  "tools": []
}
```

### 4.6 max_tokens 默认值

```javascript
function pickMaxTokens(requestDefaults) {
  const v = requestDefaults?.max_tokens ?? requestDefaults?.maxTokens;
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? n : 32000;
}
```

---

## 5. Beta 功能列表

### 5.1 必需 Betas

```javascript
const betas = [
  "claude-code-20250219",
  "interleaved-thinking-2025-05-14"
];
```

### 5.2 条件性 Betas

| Beta | 触发条件 | 说明 |
|------|----------|------|
| `context-management-2025-06-27` | `requestDefaults.context_management` 存在 | 上下文管理 |
| `structured-outputs-2025-09-17` | `requestDefaults.output_format` 存在 | 结构化输出 |
| `tool-examples-2025-10-29` | `tools.length > 0` | 工具示例 |
| `advanced-tool-use-2025-11-20` | `tools.length > 0` | 高级工具使用 |
| `tool-search-tool-2025-10-19` | 存在名为 `MCPSearch` 的工具 | MCP 搜索工具 |
| `web-search-2025-03-05` | 存在名为 `WebSearch` 的工具 | Web 搜索 |
| `oauth-2025-04-20` | Authorization header 以 `Bearer ` 开头 | OAuth 认证 |

### 5.3 Beta 构建逻辑

```javascript
function buildClaudeCodeBetas({ model, tools, requestDefaults, extraHeaders }) {
  const betas = ["claude-code-20250219", "interleaved-thinking-2025-05-14"];
  
  if (requestDefaults?.context_management) betas.push("context-management-2025-06-27");
  if (requestDefaults?.output_format) betas.push("structured-outputs-2025-09-17");
  
  if (Array.isArray(tools) && tools.length) {
    betas.push("tool-examples-2025-10-29");
    betas.push("advanced-tool-use-2025-11-20");
  }
  
  if (tools?.some(t => t.name === "MCPSearch")) betas.push("tool-search-tool-2025-10-19");
  if (tools?.some(t => t.name === "WebSearch")) betas.push("web-search-2025-03-05");
  
  const authHeader = extraHeaders?.authorization || extraHeaders?.Authorization;
  if (authHeader?.toLowerCase().startsWith("bearer ")) betas.push("oauth-2025-04-20");
  
  return betas;
}
```

---

## 6. SSE 响应格式

### 6.1 事件类型

| 事件类型 | 说明 | 包含字段 |
|----------|------|----------|
| `message_start` | 消息开始 | `message.usage` |
| `content_block_start` | 内容块开始 | `content_block.type`, `index` |
| `content_block_delta` | 内容块增量 | `delta`, `index` |
| `content_block_stop` | 内容块结束 | `index` |
| `message_delta` | 消息增量 | `delta.stop_reason`, `usage` |
| `message_stop` | 消息结束 | - |
| `error` | 错误 | `error.message` |

### 6.2 content_block_start 类型

| `content_block.type` | 说明 | 额外字段 |
|---------------------|------|----------|
| `text` | 文本块 | - |
| `tool_use` | 工具调用 | `id`, `name` |
| `server_tool_use` | 服务端工具调用 | `id`, `name` |
| `mcp_tool_use` | MCP 工具调用 | `id`, `name` |
| `thinking` | 思考块 | - |

### 6.3 content_block_delta 类型

| `delta.type` | 说明 | 数据字段 |
|--------------|------|----------|
| `text_delta` | 文本增量 | `delta.text` |
| `input_json_delta` | 工具输入增量 | `delta.partial_json` |
| `thinking_delta` | 思考增量 | `delta.thinking` |

### 6.4 Stop Reason

| `stop_reason` | 说明 |
|---------------|------|
| `end_turn` | 正常结束 |
| `stop_sequence` | 遇到停止序列 |
| `max_tokens` | 达到 token 限制 |
| `tool_use` | 请求工具调用 |
| `server_tool_use` | 请求服务端工具调用 |
| `mcp_tool_use` | 请求 MCP 工具调用 |

### 6.5 Usage 字段

```json
{
  "usage": {
    "input_tokens": 100,
    "output_tokens": 50,
    "cache_read_input_tokens": 20,
    "cache_creation_input_tokens": 10
  }
}
```

---

## 7. 完整请求示例

### 7.1 基础对话请求

```bash
curl -X POST "https://api.anthropic.com/v1/messages?beta=true" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -H "anthropic-beta: claude-code-20250219,interleaved-thinking-2025-05-14" \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "x-app: cli" \
  -H "user-agent: claude-cli/2.1.2 (external, cli)" \
  -H "x-stainless-arch: arm64" \
  -H "x-stainless-lang: js" \
  -H "x-stainless-os: MacOS" \
  -H "x-stainless-package-version: 0.70.0" \
  -H "x-stainless-retry-count: 0" \
  -H "x-stainless-runtime: node" \
  -H "x-stainless-runtime-version: v24.3.0" \
  -H "x-stainless-timeout: 600" \
  -H "connection: keep-alive" \
  -H "accept-encoding: gzip, deflate, br, zstd" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {"role": "user", "content": "Hello, Claude!"}
    ],
    "system": [
      {
        "type": "text",
        "text": "You are Claude Code, Anthropic'\''s official CLI for Claude.",
        "cache_control": {"type": "ephemeral"}
      }
    ],
    "tools": [],
    "metadata": {
      "user_id": "user_abc123_account_xyz789_session_550e8400-e29b-41d4-a716-446655440000"
    },
    "max_tokens": 32000,
    "stream": false
  }'
```

### 7.2 流式请求

```bash
curl -X POST "https://api.anthropic.com/v1/messages?beta=true" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -H "anthropic-beta: claude-code-20250219,interleaved-thinking-2025-05-14" \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "x-app: cli" \
  -H "user-agent: claude-cli/2.1.2 (external, cli)" \
  -H "x-stainless-arch: arm64" \
  -H "x-stainless-lang: js" \
  -H "x-stainless-os: MacOS" \
  -H "x-stainless-package-version: 0.70.0" \
  -H "x-stainless-retry-count: 0" \
  -H "x-stainless-runtime: node" \
  -H "x-stainless-runtime-version: v24.3.0" \
  -H "x-stainless-timeout: 600" \
  -H "x-stainless-helper-method: stream" \
  -H "accept: application/json" \
  -H "connection: keep-alive" \
  -H "accept-encoding: gzip, deflate, br, zstd" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {"role": "user", "content": "Tell me a story"}
    ],
    "system": [
      {
        "type": "text",
        "text": "You are Claude Code, Anthropic'\''s official CLI for Claude.",
        "cache_control": {"type": "ephemeral"}
      }
    ],
    "tools": [],
    "metadata": {
      "user_id": "user_abc123_account_xyz789_session_550e8400-e29b-41d4-a716-446655440000"
    },
    "max_tokens": 32000,
    "stream": true
  }'
```

### 7.3 带工具的请求

```bash
curl -X POST "https://api.anthropic.com/v1/messages?beta=true" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -H "anthropic-beta: claude-code-20250219,interleaved-thinking-2025-05-14,tool-examples-2025-10-29,advanced-tool-use-2025-11-20" \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "x-app: cli" \
  -H "user-agent: claude-cli/2.1.2 (external, cli)" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "messages": [
      {"role": "user", "content": "What is the weather in SF?"}
    ],
    "system": [
      {
        "type": "text",
        "text": "You are Claude Code, Anthropic'\''s official CLI for Claude.",
        "cache_control": {"type": "ephemeral"}
      }
    ],
    "tools": [
      {
        "name": "get_weather",
        "description": "Get the current weather in a location",
        "input_schema": {
          "type": "object",
          "properties": {
            "location": {
              "type": "string",
              "description": "The city and state"
            }
          },
          "required": ["location"]
        }
      }
    ],
    "metadata": {
      "user_id": "user_abc123_account_xyz789_session_550e8400-e29b-41d4-a716-446655440000"
    },
    "max_tokens": 32000,
    "stream": true
  }'
```

---

## 8. SSE 响应示例

### 8.1 文本响应

```
event: message_start
data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":50,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}

event: message_stop
data: {"type":"message_stop"}
```

### 8.2 工具调用响应

```
event: message_start
data: {"type":"message_start","message":{"id":"msg_456","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":100,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_123","name":"get_weather"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"location\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"San Francisco, CA\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":25}}

event: message_stop
data: {"type":"message_stop"}
```

### 8.3 思考块响应

```
event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think about this..."}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}
```

### 8.4 错误响应

```
event: error
data: {"type":"error","error":{"type":"invalid_request_error","message":"Invalid API key"}}
```

---

## 9. 参考资料

- **Augment-BYOK v0.754.3**: [GitHub](https://github.com/wanggang8/Augment-BYOK/tree/v0.754.3)
- **核心代码**: `payload/extension/out/byok/providers/anthropic-claude-code.js`
