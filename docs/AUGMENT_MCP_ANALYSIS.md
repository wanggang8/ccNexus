# Augment MCP 工具调用问题分析报告

## 执行摘要

**问题确认：❌ 当前实现缺少 MCP 字段支持，无法正确处理 Augment 的 MCP 工具调用**

通过对比官方 Augment-BYOK 源代码和其他 Augment 项目，发现我们的实现缺少了关键的 `mcp_server_name` 和 `mcp_tool_name` 字段支持，这会导致 Augment 的 ACE MCP 工具无法正常工作。

---

## 一、问题发现

### 1.1 官方协议中的 MCP 字段

从 Augment-BYOK 的 `augment-protocol.js` 中发现：

```javascript
function makeToolUse({ toolUseId, toolName, inputJson, mcpServerName, mcpToolName }) {
 const out = { 
   tool_use_id: String(toolUseId || ""), 
   tool_name: String(toolName || ""), 
   input_json: String(inputJson || "") 
 };
 const msn = typeof mcpServerName === "string" ? mcpServerName.trim() : "";
 const mtn = typeof mcpToolName === "string" ? mcpToolName.trim() : "";
 if (msn) out.mcp_server_name = msn;  // ⭐ MCP 服务器名称
 if (mtn) out.mcp_tool_name = mtn;    // ⭐ MCP 工具名称
 return out;
}
```

**关键发现：**
- `tool_use` 对象可以包含 `mcp_server_name` 和 `mcp_tool_name` 字段
- 这两个字段用于标识 MCP 工具的来源

### 1.2 我们当前的实现

**types.go (line 86-91):**
```go
type ToolUseNode struct {
	ToolName  string `json:"tool_name"`
	ToolUseID string `json:"tool_use_id"`
	InputJSON string `json:"input_json"`
	// ❌ 缺少 McpServerName
	// ❌ 缺少 McpToolName
}
```

**response.go (line 151-155, 171-175):**
```go
"tool_use": map[string]interface{}{
	"tool_name":   buf.name,
	"tool_use_id": buf.id,
	"input_json":  "",
	// ❌ 缺少 mcp_server_name
	// ❌ 缺少 mcp_tool_name
}
```

---

## 二、影响分析

### 2.1 什么是 Augment MCP

**MCP (Model Context Protocol)** 是 Augment 用于集成外部工具和数据源的协议。

**Augment 支持的 MCP 工具类型：**
1. **ACE Context Service** - 代码库语义搜索和上下文检索
2. **VSCode MCP Server** - 文件操作、代码编辑、诊断、符号搜索
3. **第三方 MCP 服务器** - CircleCI, MongoDB, Redis 等
4. **自定义 MCP 服务器** - 用户自己开发的工具

### 2.2 MCP 字段的作用

当 Augment 调用 MCP 工具时：

1. **请求侧（Augment → 我们的 Proxy）：**
   - Augment 发送的 `tool_definitions` 可能来自 MCP 服务器
   - 工具名称可能是 `mcp_server_name::tool_name` 格式

2. **响应侧（上游 API → Augment）：**
   - Claude/OpenAI 返回的 `tool_use` 需要标记来源
   - Augment 需要知道这个工具来自哪个 MCP 服务器
   - 格式：`{ tool_name: "query_codebase", mcp_server_name: "auggie-context", mcp_tool_name: "query_codebase" }`

### 2.3 缺少 MCP 字段的后果

❌ **ACE 上下文检索失败**
- Augment 无法识别来自 ACE 的工具调用结果
- 代码库语义搜索功能不可用

❌ **MCP 工具调用链断裂**
- Augment 无法正确路由工具结果回 MCP 服务器
- 多轮工具调用失败

❌ **工具来源不明**
- Augment UI 无法显示工具来自哪个 MCP 服务器
- 调试困难

---

## 三、其他 Augment 项目调研

### 3.1 Augment-BYOK (官方参考实现)

**项目：** https://github.com/AnkRoot/Augment-BYOK
**Stars：** 328
**语言：** JavaScript (98.8%)

**关键特性：**
- ✅ 完整支持 MCP 字段（`mcp_server_name`, `mcp_tool_name`）
- ✅ 支持 11 个 LLM 数据面端点
- ✅ 支持 4 种 provider 类型（OpenAI, Anthropic, Gemini, OpenAI Responses）
- ✅ 单 VSIX 打包，无需外部依赖

**MCP 支持：**
```javascript
// 完整的 tool_use 结构
{
  tool_use_id: "...",
  tool_name: "...",
  input_json: "...",
  mcp_server_name: "auggie-context",  // ✅ 支持
  mcp_tool_name: "query_codebase"     // ✅ 支持
}
```

### 3.2 augment-lite-mcp

**项目：** https://github.com/zoonderkins/augment-lite-mcp
**Stars：** 未知
**语言：** Python (82.6%)

**关键特性：**
- ✅ 本地优先的 AI 代码助手
- ✅ 31 个 MCP 工具
- ✅ 混合搜索（BM25 + 向量语义）
- ✅ 自动增量索引
- ✅ 隐私优先（本地 DuckDB + SQLite）

**工具类型：**
- 代码分析（Tree-sitter）
- RAG 检索
- 内存管理
- 任务处理

### 3.3 auggie-context-mcp

**项目：** https://github.com/aj47/auggie-context-mcp
**语言：** TypeScript

**关键特性：**
- ✅ 官方 Auggie CLI 的 MCP 封装
- ✅ 提供 `query_codebase` 工具
- ✅ 支持 Claude Desktop 和 Cursor
- ✅ 代码库上下文检索

**工具示例：**
```json
{
  "name": "query_codebase",
  "description": "Query the codebase for relevant context",
  "input_schema": {
    "type": "object",
    "properties": {
      "query": { "type": "string" }
    }
  }
}
```

### 3.4 vscode-mcp-server

**项目：** https://www.augmentcode.com/mcp/vscode-mcp-server
**语言：** TypeScript

**关键特性：**
- ✅ 将 VSCode 转为 MCP 服务器
- ✅ 9 个工具（文件、编辑、诊断、符号、Shell）
- ✅ 支持 Claude 等 LLM 客户端

**工具列表：**
1. `list_files_code` - 列出文件和目录
2. `read_file_code` - 读取文件内容
3. `create_file_code` - 创建新文件
4. `replace_lines_code` - 替换指定行
5. `get_diagnostics_code` - 获取错误和警告
6. `search_symbols_code` - 搜索符号
7. `get_symbol_definition_code` - 获取符号定义
8. `get_document_symbols_code` - 获取文档符号
9. `execute_shell_command_code` - 执行 Shell 命令

---

## 四、MCP 工具调用流程

### 4.1 正常的 MCP 工具调用流程

```
1. Augment 加载 MCP 服务器
   ↓
2. MCP 服务器注册工具（通过 tools/list）
   ↓
3. Augment 将工具定义发送给 Proxy
   {
     tool_definitions: [
       { name: "query_codebase", description: "...", input_schema: {...} }
     ]
   }
   ↓
4. Proxy 转换为 Claude/OpenAI 格式
   ↓
5. 上游 API 返回 tool_use
   {
     type: "tool_use",
     id: "tool_123",
     name: "query_codebase",
     input: { query: "..." }
   }
   ↓
6. Proxy 转换为 Augment 格式（⚠️ 这里需要添加 MCP 字段）
   {
     type: 5,
     tool_use: {
       tool_use_id: "tool_123",
       tool_name: "query_codebase",
       input_json: "{\"query\":\"...\"}",
       mcp_server_name: "auggie-context",  // ⭐ 关键
       mcp_tool_name: "query_codebase"     // ⭐ 关键
     }
   }
   ↓
7. Augment 识别 MCP 工具并调用对应的 MCP 服务器
   ↓
8. MCP 服务器执行工具并返回结果
   ↓
9. Augment 将结果作为 tool_result 发送回 Proxy
   {
     nodes: [{
       type: 1,
       tool_result_node: {
         tool_use_id: "tool_123",
         content: "..."
       }
     }]
   }
```

### 4.2 当前实现的问题

**步骤 6 出错：**
```go
// 当前实现（缺少 MCP 字段）
{
  type: 5,
  tool_use: {
    tool_use_id: "tool_123",
    tool_name: "query_codebase",
    input_json: "{\"query\":\"...\"}"
    // ❌ 缺少 mcp_server_name
    // ❌ 缺少 mcp_tool_name
  }
}
```

**结果：**
- Augment 无法识别这是 MCP 工具
- 无法路由到正确的 MCP 服务器
- 工具调用失败

---

## 五、解决方案

### 5.1 需要修改的文件

1. **internal/transformer/augment/types.go**
   - 在 `ToolUseNode` 中添加 `McpServerName` 和 `McpToolName` 字段

2. **internal/transformer/augment/response.go**
   - 在生成 `tool_use` 时添加 MCP 字段
   - 需要从 Claude/OpenAI 响应中提取或推断 MCP 信息

3. **internal/transformer/augment/to_claude.go**
   - 在处理 `tool_result` 时保留 MCP 信息

4. **internal/transformer/augment/to_openai.go**
   - 在处理 `tool_result` 时保留 MCP 信息

### 5.2 实现难点

#### 问题 1：如何获取 MCP 信息？

Claude/OpenAI 的响应中**不包含** MCP 字段：

```json
// Claude 响应
{
  "type": "tool_use",
  "id": "tool_123",
  "name": "query_codebase",
  "input": { "query": "..." }
  // ❌ 没有 mcp_server_name
  // ❌ 没有 mcp_tool_name
}
```

**可能的解决方案：**

**方案 A：工具名称映射表**
```go
var mcpToolMapping = map[string]struct{
	ServerName string
	ToolName   string
}{
	"query_codebase": {
		ServerName: "auggie-context",
		ToolName:   "query_codebase",
	},
	"list_files_code": {
		ServerName: "vscode-mcp",
		ToolName:   "list_files_code",
	},
	// ...
}
```

**优点：** 简单直接
**缺点：** 需要维护映射表，不支持自定义 MCP 工具

**方案 B：从请求中记录工具定义**
```go
// 在 TransformRequest 时记录工具来源
type ToolContext struct {
	ToolName      string
	McpServerName string
	McpToolName   string
}

// 在响应时查找对应的 MCP 信息
```

**优点：** 支持所有 MCP 工具
**缺点：** 需要维护状态，实现复杂

**方案 C：工具名称前缀约定**
```go
// 假设 MCP 工具名称格式为 "server::tool"
func parseMcpToolName(toolName string) (serverName, toolName string) {
	parts := strings.Split(toolName, "::")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", toolName
}
```

**优点：** 无需额外状态
**缺点：** 依赖命名约定，可能不准确

#### 问题 2：Augment 如何知道工具是 MCP 工具？

从 Augment-BYOK 的代码来看，Augment 在发送请求时**已经知道**哪些工具是 MCP 工具。

**推测：**
- Augment 在 `tool_definitions` 中可能包含 MCP 元数据
- 或者 Augment 维护了一个 MCP 工具注册表

**需要验证：**
- 查看 Augment 发送的实际请求格式
- 检查 `tool_definitions` 是否包含额外字段

---

## 六、建议的实现步骤

### 步骤 1：扩展数据结构

```go
// types.go
type ToolUseNode struct {
	ToolName      string `json:"tool_name"`
	ToolUseID     string `json:"tool_use_id"`
	InputJSON     string `json:"input_json"`
	McpServerName string `json:"mcp_server_name,omitempty"` // ⭐ 新增
	McpToolName   string `json:"mcp_tool_name,omitempty"`   // ⭐ 新增
}

type ToolDefinition struct {
	Name            string                 `json:"name"`
	Description     string                 `json:"description,omitempty"`
	InputSchema     map[string]interface{} `json:"input_schema,omitempty"`
	InputSchemaJSON string                 `json:"input_schema_json,omitempty"`
	Parameters      map[string]interface{} `json:"parameters,omitempty"`
	McpServerName   string                 `json:"mcp_server_name,omitempty"` // ⭐ 新增
	McpToolName     string                 `json:"mcp_tool_name,omitempty"`   // ⭐ 新增
}
```

### 步骤 2：记录工具上下文

```go
// transformer.go
type Transformer struct {
	targetType  string
	model       string
	toolContext map[string]*ToolContext // ⭐ 新增：工具上下文缓存
}

type ToolContext struct {
	McpServerName string
	McpToolName   string
}

func (t *Transformer) TransformRequest(req []byte) ([]byte, error) {
	var ar AugmentRequest
	// ...
	
	// 记录 MCP 工具信息
	t.toolContext = make(map[string]*ToolContext)
	for _, tool := range ar.EffectiveTools() {
		if tool.McpServerName != "" {
			t.toolContext[tool.Name] = &ToolContext{
				McpServerName: tool.McpServerName,
				McpToolName:   tool.McpToolName,
			}
		}
	}
	
	// ...
}
```

### 步骤 3：在响应中添加 MCP 字段

```go
// response.go
func streamConvertClaudeSSE(r io.Reader, w io.Writer, toolCtx map[string]*ToolContext) error {
	// ...
	
	case "content_block_stop":
		if buf.active {
			toolUse := map[string]interface{}{
				"tool_name":   buf.name,
				"tool_use_id": buf.id,
				"input_json":  buf.input.String(),
			}
			
			// ⭐ 添加 MCP 字段
			if ctx, ok := toolCtx[buf.name]; ok {
				if ctx.McpServerName != "" {
					toolUse["mcp_server_name"] = ctx.McpServerName
				}
				if ctx.McpToolName != "" {
					toolUse["mcp_tool_name"] = ctx.McpToolName
				}
			}
			
			node := map[string]interface{}{
				"id":       nextNodeID,
				"type":     augmentNodeTypeToolUse,
				"content":  "",
				"tool_use": toolUse,
			}
			// ...
		}
}
```

### 步骤 4：测试验证

1. **单元测试** - 验证 MCP 字段的序列化/反序列化
2. **集成测试** - 使用实际的 Augment 请求测试
3. **端到端测试** - 验证 ACE MCP 工具调用

---

## 七、其他发现

### 7.1 Augment 的工具调用特性

**并行工具调用：**
- Augment 支持并行执行多个工具
- 需要确保我们的实现支持多个 `tool_use` 节点

**工具调用链：**
- Augment 支持多轮工具调用
- 需要正确处理 `tool_result` → `tool_use` → `tool_result` 的循环

### 7.2 Augment 的上下文注入

从 Augment-BYOK 的 CONFIG.md 中发现：

```
官方上下文注入（仅 /chat、/chat-stream；fail-open）

BYOK chat 在构造 provider 请求前，会尝试调用官方能力把外部上下文注入到请求中：
- agents/codebase-retrieval
- get-implicit-external-sources
- search-external-sources
- context-canvas/list

前置：需要 official.completionUrl + official.apiToken
```

**这意味着：**
- Augment 有自己的上下文服务（ACE）
- 我们的 Proxy 可能需要支持这些上下文注入端点
- 或者需要配置 `official.completionUrl` 让 Augment 直接调用官方服务

---

## 八、总结与建议

### 8.1 问题确认

✅ **确认问题存在**
- 当前实现缺少 `mcp_server_name` 和 `mcp_tool_name` 字段
- 无法正确处理 Augment 的 MCP 工具调用
- ACE 上下文检索等功能不可用

### 8.2 影响范围

**高影响：**
- ❌ ACE 代码库语义搜索
- ❌ VSCode MCP 工具集成
- ❌ 第三方 MCP 服务器

**中影响：**
- ⚠️ 工具调用链可能断裂
- ⚠️ UI 显示不完整

**低影响：**
- ✅ 普通工具调用（非 MCP）仍然正常

### 8.3 优先级建议

**P0 - 立即修复：**
1. 添加 MCP 字段到数据结构
2. 实现基本的 MCP 字段传递

**P1 - 短期优化：**
3. 实现工具上下文缓存
4. 添加 MCP 工具映射表

**P2 - 长期改进：**
5. 支持官方上下文注入
6. 完整的 MCP 协议支持

### 8.4 参考资源

**官方文档：**
- https://docs.augmentcode.com/setup-augment/mcp
- https://modelcontextprotocol.io/

**参考项目：**
- https://github.com/AnkRoot/Augment-BYOK （最佳参考）
- https://github.com/zoonderkins/augment-lite-mcp
- https://github.com/aj47/auggie-context-mcp

**MCP 服务器：**
- https://www.augmentcode.com/mcp （官方 MCP 注册表）

---

## 九、下一步行动

1. **验证问题** - 使用实际的 Augment 请求测试当前实现
2. **设计方案** - 选择合适的 MCP 字段获取方案
3. **实现修复** - 按照上述步骤实现 MCP 支持
4. **测试验证** - 确保 ACE 和其他 MCP 工具正常工作
5. **文档更新** - 更新使用文档，说明 MCP 支持情况

---

## 附录：Augment-BYOK 的完整 tool_use 结构

```javascript
// 来自 augment-protocol.js
function toolUseNode({ id, toolUseId, toolName, inputJson, mcpServerName, mcpToolName }) {
 return { 
   id: Number(id) || 0, 
   type: RESPONSE_NODE_TOOL_USE,  // 5
   content: "", 
   tool_use: {
     tool_use_id: String(toolUseId || ""),
     tool_name: String(toolName || ""),
     input_json: String(inputJson || ""),
     mcp_server_name: mcpServerName || undefined,  // 可选
     mcp_tool_name: mcpToolName || undefined       // 可选
   }
 };
}
```

**关键点：**
- `mcp_server_name` 和 `mcp_tool_name` 是可选字段
- 只有 MCP 工具才需要这两个字段
- 普通工具（如 Claude 内置工具）不需要
