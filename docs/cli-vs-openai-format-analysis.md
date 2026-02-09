# CLI vs OpenAI 端点格式差异分析

## 问题描述

**现象**：
- ✅ OpenAI 请求 → CLI 端点：**正常工作**
- ❌ OpenAI 请求 → OpenAI 端点：**无法使用**

## 根本原因分析

### 1. 转换流程对比

#### 场景 A：OpenAI 请求 → CLI 端点（✅ 工作）

```
客户端 (OpenAI格式)
    ↓
[cx_chat_cli Transformer]
    ↓ TransformRequest: OpenAIReqToClaudeCLI()
Claude CLI 格式请求 → Claude API
    ↓
Claude 响应
    ↓ TransformResponse: ClaudeRespToOpenAI()
OpenAI 格式响应
    ↓
客户端接收
```

**关键转换**：
- 请求：`OpenAIReqToClaudeCLI()` - 完整转换
- 响应：`ClaudeRespToOpenAI()` - 完整转换

#### 场景 B：OpenAI 请求 → OpenAI 端点（❌ 失败）

```
客户端 (OpenAI格式)
    ↓
[cx_chat_openai Transformer]
    ↓ TransformRequest: 仅修复 model + 工具格式
OpenAI 格式请求 → OpenAI API
    ↓
OpenAI 响应
    ↓ TransformResponse: 直接透传 (return resp, nil)
OpenAI 格式响应
    ↓
客户端接收
```

**关键问题**：
- 请求：只做轻量级修复（model 覆盖、工具格式转换）
- 响应：**完全透传，不做任何转换**

---

## 2. 详细代码对比

### cx_chat_cli (cli.go)

```go
func (t *CLITransformer) TransformRequest(req []byte) ([]byte, error) {
    // 完整转换：OpenAI → Claude CLI
    body, _, err := convert.OpenAIReqToClaudeCLI(req, t.model, t.apiKey)
    return body, err
}

func (t *CLITransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
    if isStreaming {
        return nil, nil
    }
    // 完整转换：Claude → OpenAI
    return convert.ClaudeRespToOpenAI(resp, t.model)
}

func (t *CLITransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
    if isStreaming {
        // 流式转换：Claude SSE → OpenAI SSE
        return convert.ClaudeStreamToOpenAI(resp, ctx, t.model)
    }
    return convert.ClaudeRespToOpenAI(resp, t.model)
}
```

### cx_chat_openai (openai.go)

```go
func (t *OpenAITransformer) TransformRequest(req []byte) ([]byte, error) {
    var data map[string]interface{}
    if err := json.Unmarshal(req, &data); err != nil {
        return req, nil
    }

    // 1. 覆盖 model
    if t.model != "" {
        data["model"] = t.model
    }

    // 2. 修复 Cursor 的 Claude 格式消息（tool_result）
    // 3. 修复 Cursor 的 Claude 格式工具定义

    return json.Marshal(data)
}

func (t *OpenAITransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
    // ❌ 直接透传，不做任何转换
    return resp, nil
}

func (t *OpenAITransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
    // ❌ 直接透传，不做任何转换
    return resp, nil
}
```

---

## 3. 问题根源

### 客户端期望的格式

根据代码路径 `ClientFormatOpenAIChat` → `prepareCxChatTransformer`，客户端是 **Cursor 的 OpenAI Chat API 客户端**。

**Cursor 的特殊性**：
- Cursor 可能发送 **混合格式**（OpenAI + Claude 特性）
- 例如：Claude 格式的工具定义、tool_result 消息块

### OpenAI 端点的问题

`cx_chat_openai` 转换器假设：
1. **请求**：只需要轻微修复（工具格式、model）
2. **响应**：OpenAI API 返回的格式客户端可以直接理解

**但实际情况可能是**：
- Cursor 客户端期望特定的响应格式
- OpenAI API 的原始响应可能与 Cursor 期望不完全匹配
- 缺少某些字段或格式细节

---

## 4. CLI 端点为什么能工作？

CLI 转换器做了 **完整的双向转换**：

### 请求转换 (OpenAIReqToClaudeCLI)
```go
// 1. 构建 System Prompt（硬编码 CLI 身份 + 用户自定义）
system := []map[string]interface{}{CliSystemPrompt}

// 2. 转换 Messages（排除 system）
messages := convertOpenAIMessageToClaudeCLI(msg)

// 3. 转换 Tools（OpenAI → Claude 格式）
claudeTool = map[string]interface{}{
    "name":         funcObj["name"],
    "description":  funcObj["description"],
    "input_schema": funcObj["parameters"],
}

// 4. 构建 Metadata
metadata := map[string]interface{}{
    "user_id": fmt.Sprintf("user_%s_account__session_%s", ...),
}

// 5. 添加 CLI 特定字段
cliReq := map[string]interface{}{
    "model":      model,
    "messages":   messages,
    "system":     system,
    "tools":      tools,
    "metadata":   metadata,
    "max_tokens": maxTokens,
    "stream":     req.Stream,
}
```

### 响应转换 (ClaudeRespToOpenAI)
```go
// 1. 解析 Claude 响应
var resp transformer.ClaudeResponse

// 2. 转换 content blocks
for _, block := range resp.Content {
    switch blockMap["type"] {
    case "text":
        textContent += text
    case "thinking":
        continue // 跳过 thinking blocks
    case "tool_use":
        toolCalls = append(toolCalls, ...)
    }
}

// 3. 构建标准 OpenAI 响应
openaiResp := map[string]interface{}{
    "id":      resp.ID,
    "object":  "chat.completion",
    "model":   model,
    "choices": []map[string]interface{}{
        {
            "index":         0,
            "message":       message,
            "finish_reason": finishReason,
        },
    },
    "usage": map[string]interface{}{
        "prompt_tokens":     resp.Usage.InputTokens,
        "completion_tokens": resp.Usage.OutputTokens,
        "total_tokens":      resp.Usage.InputTokens + resp.Usage.OutputTokens,
    },
}
```

**关键**：CLI 转换器确保返回的是 **标准化的 OpenAI 格式**，包含所有必需字段。

---

## 5. 可能的失败原因

### OpenAI 端点失败的可能原因：

#### A. 响应格式不完整
OpenAI API 的原始响应可能缺少 Cursor 期望的字段：
- `created` 时间戳
- `system_fingerprint`
- 特定的 `finish_reason` 值
- 工具调用的格式细节

#### B. 流式响应格式差异
```go
// cx_chat_openai: 直接透传
func (t *OpenAITransformer) TransformResponseWithContext(...) ([]byte, error) {
    return resp, nil  // ❌ 不处理 SSE 事件
}

// cx_chat_cli: 完整转换
func (t *CLITransformer) TransformResponseWithContext(...) ([]byte, error) {
    if isStreaming {
        return convert.ClaudeStreamToOpenAI(resp, ctx, t.model)  // ✅ 转换 SSE
    }
    return convert.ClaudeRespToOpenAI(resp, t.model)
}
```

#### C. 工具调用格式
OpenAI API 返回的 `tool_calls` 格式可能与 Cursor 期望不同：
```json
// OpenAI 原始格式
{
  "tool_calls": [{
    "id": "call_xxx",
    "type": "function",
    "function": {
      "name": "get_weather",
      "arguments": "{\"location\":\"SF\"}"
    }
  }]
}

// Cursor 可能期望的格式（经过标准化）
// CLI 转换器会确保格式正确
```

#### D. 错误响应处理
OpenAI API 的错误响应格式可能不同：
```json
// OpenAI 错误格式
{
  "error": {
    "message": "...",
    "type": "invalid_request_error",
    "code": "..."
  }
}

// Cursor 可能期望 Claude 风格的错误
```

---

## 6. 解决方案建议

### 方案 1：增强 cx_chat_openai 转换器（推荐）

修改 `internal/transformer/cx/chat/openai.go`：

```go
func (t *OpenAITransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
    if isStreaming {
        return nil, nil
    }
    
    // 解析响应
    var openaiResp map[string]interface{}
    if err := json.Unmarshal(resp, &openaiResp); err != nil {
        return resp, nil // 解析失败，返回原始响应
    }
    
    // 标准化响应格式
    if openaiResp["object"] == nil {
        openaiResp["object"] = "chat.completion"
    }
    if openaiResp["created"] == nil {
        openaiResp["created"] = time.Now().Unix()
    }
    
    // 确保 choices 格式正确
    if choices, ok := openaiResp["choices"].([]interface{}); ok && len(choices) > 0 {
        if choice, ok := choices[0].(map[string]interface{}); ok {
            // 标准化 finish_reason
            if choice["finish_reason"] == nil {
                choice["finish_reason"] = "stop"
            }
            
            // 确保 message 格式正确
            if message, ok := choice["message"].(map[string]interface{}); ok {
                if message["role"] == nil {
                    message["role"] = "assistant"
                }
                if message["content"] == nil {
                    message["content"] = ""
                }
            }
        }
    }
    
    // 确保 usage 存在
    if openaiResp["usage"] == nil {
        openaiResp["usage"] = map[string]interface{}{
            "prompt_tokens":     0,
            "completion_tokens": 0,
            "total_tokens":      0,
        }
    }
    
    return json.Marshal(openaiResp)
}

func (t *OpenAITransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
    if !isStreaming {
        return t.TransformResponse(resp, false)
    }
    
    // 标准化流式响应
    var chunk map[string]interface{}
    if err := json.Unmarshal(resp, &chunk); err != nil {
        return resp, nil
    }
    
    // 确保流式响应格式正确
    if chunk["object"] == nil {
        chunk["object"] = "chat.completion.chunk"
    }
    if chunk["created"] == nil {
        chunk["created"] = time.Now().Unix()
    }
    
    return json.Marshal(chunk)
}
```

### 方案 2：使用 CLI 端点作为中转（临时方案）

如果 OpenAI 端点确实有问题，可以：
1. 将所有 OpenAI 端点改为 CLI 端点
2. CLI 端点会做完整的格式转换，确保兼容性

### 方案 3：添加详细日志诊断

在 `cx_chat_openai` 中添加日志：

```go
func (t *OpenAITransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
    logger.Debug("[cx_chat_openai] Raw response: %s", string(resp))
    
    // 检查是否是错误响应
    var errResp map[string]interface{}
    if err := json.Unmarshal(resp, &errResp); err == nil {
        if errResp["error"] != nil {
            logger.Warn("[cx_chat_openai] Error response detected: %v", errResp["error"])
        }
    }
    
    return resp, nil
}
```

---

## 7. 调试步骤

### 步骤 1：启用详细日志

修改 `internal/transformer/cx/chat/openai.go`：

```go
func (t *OpenAITransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
    logger.Debug("[cx_chat_openai] Response length: %d bytes", len(resp))
    logger.Debug("[cx_chat_openai] Response preview: %s", string(resp[:min(len(resp), 500)]))
    return resp, nil
}
```

### 步骤 2：对比 CLI 和 OpenAI 响应

1. 使用 CLI 端点，记录转换后的响应
2. 使用 OpenAI 端点，记录原始响应
3. 对比差异

### 步骤 3：检查客户端错误

查看 Cursor 客户端的错误信息：
- 是否有 JSON 解析错误？
- 是否缺少必需字段？
- 是否有类型不匹配？

---

## 8. 总结

| 特性 | cx_chat_cli | cx_chat_openai |
|------|-------------|----------------|
| 请求转换 | ✅ 完整转换 | ⚠️ 轻量级修复 |
| 响应转换 | ✅ 完整转换 | ❌ 直接透传 |
| 流式响应 | ✅ SSE 转换 | ❌ 直接透传 |
| 格式标准化 | ✅ 是 | ❌ 否 |
| 错误处理 | ✅ 统一格式 | ❌ 原始格式 |

**结论**：
- CLI 端点能工作是因为它做了 **完整的双向格式转换和标准化**
- OpenAI 端点失败是因为它 **假设 OpenAI API 的原始响应可以直接使用**
- 需要增强 `cx_chat_openai` 的响应处理逻辑，确保返回标准化的格式

**建议**：实施方案 1，增强 OpenAI 转换器的响应标准化能力。
