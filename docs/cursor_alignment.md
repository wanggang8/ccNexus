# Cursor 数据面对齐说明

本文档用于固定 `ccNexus` 当前 `/cursor/*` 数据面的设计边界与对齐基线。

目标不是把整个项目改造成 `api2cursor`，而是：

- `/cursor/*` 的路由语义、请求转换、消息结构、工具调用、工具返回、响应转换、流式 SSE 语义尽量对齐 `api2cursor`
- 同时继续保留 `ccNexus` 自己的配置面和运行时能力：
  - 端点选择
  - 鉴权与 token pool
  - 模型覆盖
  - 日志、流量记录、统计
  - 通用代理与上游请求发送

## 当前架构

- Cursor 数据面：`internal/cursor`
- 通用代理壳子：`internal/proxy`
- 通用协议转换器：`internal/transformer`

其中：

- `internal/cursor` 负责 `/cursor/*` 的入口识别、路由矩阵、请求归一化、请求兼容改写、响应修补、流式状态机和 thinking cache
- `internal/proxy` 只负责通用请求处理、端点选择、模型覆盖、认证、日志、统计、上游转发

## 对齐范围

以 `api2cursor` 的数据面行为为准，对齐以下内容：

- `/cursor/v1/chat/completions`
- `/cursor/v1/responses`
- `/cursor/v1/messages`

对齐维度包括：

- 路由矩阵
- 请求体 normalize
- `messages / chat / responses` 之间的桥接
- 工具定义
- `tool_calls / tool_use`
- `tool_result / function_call_output`
- 非流式响应修补
- 流式 SSE 事件序列
- Claude `cache_control`
- Cursor thinking cache

不对齐的部分：

- `ccNexus` 的配置面
- 端点管理
- 模型映射与覆盖
- token pool / 凭证逻辑
- traffic log / stats
- 通用代理的 URL 兼容处理

## 路由矩阵

当前 `/cursor/*` 路由矩阵按 `api2cursor` 固定为：

- `/cursor/v1/chat/completions`
  - `openai`
  - `openai2`
  - `claude`
  - `gemini`
- `/cursor/v1/responses`
  - `openai`
  - `openai2`
  - `claude`
  - `gemini`
- `/cursor/v1/messages`
  - `claude` only

对应代码：

- `internal/cursor/route/matrix.go`
- `internal/cursor/route/policy.go`

## 每条路径的对齐方式

### `/cursor/v1/chat/completions`

入口请求视为 OpenAI Chat 风格请求。

对齐点：

- 若误传 Responses 风格 `input`，先降级转换成 Chat
- 规范化 `tool_use / tool_result`
- 标准化工具定义与 `tool_choice`
- 若目标是 Claude，则转为 Anthropic Messages，并补 `max_tokens` floor 与 `cache_control`
- 若目标是 Gemini，则补 Gemini function payload 兼容
- 非流式响应按 Chat 语义修补
- 流式响应按 Chat chunk 语义修补，包括 `<think>` / `reasoning_content` / `tool_calls`

核心文件：

- `internal/cursor/request/normalize.go`
- `internal/cursor/request/rewrite.go`
- `internal/cursor/response/chat.go`
- `internal/cursor/stream/chat.go`

### `/cursor/v1/responses`

入口请求视为 OpenAI Responses 风格请求。

对齐点：

- 若误传 Chat `messages`，先桥接为 Responses
- 若目标不是原生 Responses，则先降级到 Chat 中间表示，再按目标后端转换
- 保留 Responses 风格 thinking cache 注入与回写
- 对 bridge responses 与 native responses 分别处理流式事件
- 流式事件补齐 `response.created`、`response.completed`、done 事件和输出重建

核心文件：

- `internal/cursor/request/normalize.go`
- `internal/cursor/request/rewrite.go`
- `internal/cursor/response/responses.go`
- `internal/cursor/stream/responses.go`

### `/cursor/v1/messages`

入口请求视为 Anthropic Messages 风格请求。

按 `api2cursor`，这条路径只允许 Anthropic passthrough。

对齐点：

- 请求体保持 Messages passthrough，不做 Chat/Responses 风格重写
- 非流式响应补 `reasoning_content -> thinking block`
- 流式响应补 thinking 事件注入与 index 偏移

核心文件：

- `internal/cursor/request/normalize.go`
- `internal/cursor/response/messages.go`
- `internal/cursor/stream/messages.go`

## 保留的 ccNexus 能力

即使 `/cursor/*` 数据面按 `api2cursor` 对齐，以下能力仍明确保留在 `ccNexus`：

- 当前启用端点选择
- endpoint transformer 决策
- endpoint 级 model override
- API key / token pool 鉴权
- 代理 URL、Codex backend URL 兼容
- traffic log / request recorder / stats

这些逻辑主要位于：

- `internal/proxy/proxy.go`
- `internal/proxy/request.go`
- `internal/proxy/traffic.go`

## 普通请求不受影响

`/cursor/*` 数据面和普通 `/v1/*` 已经分层。

普通请求不会进入 Cursor 路径的：

- request normalize
- transformed request compat
- response fix
- stream fix
- thinking cache

因此后续对 `internal/cursor` 的改动，不应影响普通非 Cursor 请求。

## 当前关键文件

- Cursor 入口与桥接导出：`internal/cursor/bridge.go`
- Cursor 入口识别：`internal/cursor/entry/*`
- Cursor 路由矩阵：`internal/cursor/route/*`
- Cursor 请求处理：`internal/cursor/request/*`
- Cursor 响应修补：`internal/cursor/response/*`
- Cursor 流式状态机：`internal/cursor/stream/*`
- Cursor thinking cache：`internal/cursor/cache/*`
- Proxy 侧运行时薄壳：`internal/proxy/cursor_runtime.go`

## 回归命令

每次修改 `/cursor/*` 数据面后，至少运行：

```bash
go test ./internal/proxy ./internal/cursor/... ./internal/transformer/convert ./internal/transformer/cx/chat/... ./internal/transformer/cx/responses/...
```

## 维护原则

- 新的 Cursor 语义优先落在 `internal/cursor`
- `internal/proxy` 只保留通用代理壳子和极薄的 Cursor runtime 接线
- 普通请求共用的 `internal/transformer/convert` 只放通用转换
- Cursor 专属差异优先在 `internal/cursor/request|response|stream` 做补偿
- 若后续再参考 `api2cursor` 调整行为，以本文档和对应测试为基线
