# Cursor api2cursor 对齐设计

日期: 2026-03-28

## 背景与目标

ccNexus 需要在 `/cursor/*` 路径上对齐 api2cursor 的数据面语义，保证以下三条路径在请求、响应、流式 SSE、工具调用、thinking 与 cache 行为上与 api2cursor 一致:

- `/cursor/v1/chat/completions`
- `/cursor/v1/responses`
- `/cursor/v1/messages`

同时保留 ccNexus 的配置面与运行时能力，包括端点选择、鉴权与模型覆盖、日志与统计、通用代理与上游转发。普通 `/v1/*` 请求不受影响。

## 非目标

- 不改动 ccNexus 的 endpoint 管理与配置面
- 不变更普通 `/v1/*` 非 Cursor 请求的协议与响应
- 不要求代码实现与 api2cursor 相同，只要求数据语义一致

## 对齐基线

以 `/tmp/api2cursor` 的实际行为为基准，语义一致性优先于实现细节。

## 设计原则

- Cursor 语义集中在 `internal/cursor/*`，`internal/proxy/*` 只保留薄接线
- 保留 ccNexus 的端点选择、鉴权、模型覆盖、统计与日志
- 对齐只触发于 `CursorMode == true`
- 请求与响应的“数据格式一致”优先于结构实现一致

## 路径语义对齐

### 1) /cursor/v1/chat/completions

**api2cursor 行为**

- 入口视为 OpenAI Chat Completions
- 若误传 Responses 格式（含 `input`），先降级转换为 Chat
- 根据后端类型做桥接:
  - openai: 保持 CC 语义，标准化 tools / tool_choice，修复 tool_calls / reasoning_content
  - anthropic: 转 Messages，补 max_tokens floor 与 cache_control
  - responses: CC → Responses → CC
  - gemini: CC → Gemini → CC
- thinking_cache:
  - openai / anthropic / gemini 注入
  - responses 后端不注入
- SSE:
  - responses 后端需转换为 Chat chunk，并补 `[DONE]`

**ccNexus 对齐策略**

- 在 Cursor chat 路径中识别 responses 后端（openai2 transformer），强制走 CC → Responses → CC 语义链路
- 统一 tool_choice 与 tool schema 标准化
- 保留 think tag 提取与 tool_calls 修补
- claude cache_control 与 max_tokens floor 对齐 api2cursor

### 2) /cursor/v1/responses

**api2cursor 行为**

- 入口视为 OpenAI Responses
- 若误传 Chat `messages`，先转 Responses
- 对非 responses 后端: Responses → Chat (中间表示) → 对应后端 → Responses
- 对 responses 后端: 原生 Responses 透传
- thinking_cache:
  - 在 responses 入口的 chat 中间层注入
  - 在 response.completed 或非流式响应写回
- SSE:
  - 按 Responses 事件序列重建 `response.created` / `response.completed`
  - 不输出 `[DONE]`

**ccNexus 对齐策略**

- 在 Cursor responses 路径中明确区分 native responses 与 bridge responses
- 复用 ccNexus 的 responses 事件修补状态机，但对齐 api2cursor 的事件序列与 output 重建
- 保持 `[DONE]` 抑制逻辑

### 3) /cursor/v1/messages

**api2cursor 行为**

- 只允许 Anthropic Messages passthrough
- 请求体保持 Messages 原样，不做 Chat/Responses 兼容重写
- 响应补 reasoning_content → thinking block
- SSE 注入 thinking 事件，并做 index 偏移

**ccNexus 对齐策略**

- 保持现有 passthrough
- 细节对齐: reasoning 字段、index 偏移、SSE 事件结构

## 核心不一致点与修复策略（概要）

1. **chat → responses 后端桥接**
   - 需要确保 ccNexus 在 `/cursor/v1/chat/completions` + responses 后端时，执行 CC → Responses → CC 的完整链路（含 SSE 与 usage）。

2. **responses 中间桥接语义**
   - 保证 `/cursor/v1/responses` 对 openai/anthropic/gemini 后端时与 api2cursor 一致的 Responses ↔ Chat ↔ 后端转换路径。

3. **cache_control 断点注入**
   - claude 转换在请求侧注入 cache_control 规则对齐 api2cursor。

4. **SSE 事件序列与终止规则**
   - chat path: responses 后端必须输出 `[DONE]`
   - responses path: 禁止输出 `[DONE]`，依赖 `response.completed`

5. **工具与 reasoning 字段统一**
   - tool_choice / tools / tool_calls / tool_result 统一规范化
   - reasoning_content 与 `<think>` 标签一致化

## 实现范围

- `internal/cursor/request/*`
- `internal/cursor/response/*`
- `internal/cursor/stream/*`
- `internal/cursor/route/*`
- `internal/proxy/*` 中 Cursor 接线
- `internal/transformer/convert` 若需补充对齐逻辑

## 风险与回归

- 风险集中在 SSE 事件序列与工具调用结构
- 回归测试需覆盖三条路径的非流式与流式场景

## 验证建议

- `go test ./internal/proxy ./internal/cursor/... ./internal/transformer/convert ./internal/transformer/cx/chat/... ./internal/transformer/cx/responses/...`
- 补充 Cursor 路径的 responses/bridge SSE 断言

