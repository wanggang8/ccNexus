# Convert Layer Deep Audit — New Angles

Audited: message ordering, content edge cases, field passthrough, error handling, concurrency.

Previous audits covered: stop_reason mapping, tool_choice mapping, thinking/reasoning, tool call lifecycle, streaming events.

---

## Angle 1: Message Ordering and Role Constraints

### ISSUE 1 — `OpenAIReqToClaude`: consecutive same-role messages not merged

**File:** `claude_openai.go:282-327`

OpenAI's Chat Completions API allows consecutive messages with the same role (e.g., two `user` messages in a row). Claude's Messages API strictly forbids this — roles **must** alternate between `"user"` and `"assistant"`.

The converter iterates through OpenAI messages and emits Claude messages with the same role, without checking whether the previously emitted message had the same role:

```go
// line 282
claudeMsg := map[string]interface{}{"role": role}
// ...
// line 327
messages = append(messages, claudeMsg)
```

No merge or alternation check is performed. Sending two consecutive `user` messages to the Claude API returns:

> `messages: roles must alternate between "user" and "assistant", but found two consecutive "user" roles`

**Impact:** Any OpenAI client that sends consecutive same-role messages (valid per OpenAI spec) triggers a 400 error from Claude.

---

### ISSUE 2 — `OpenAIReqToGemini`: consecutive same-role messages not merged

**File:** `openai_gemini.go:38-98`

Same class of bug. After filtering `system` messages and merging consecutive `tool` messages (lines 50-68), remaining consecutive `user` or `assistant` messages are passed through unmerged:

```go
// line 96
contents = append(contents, map[string]interface{}{"role": role, "parts": parts})
```

Gemini requires `model`/`user` alternation. Two consecutive `user` contents cause a Gemini API error.

**Impact:** Same as Issue 1 but for the Gemini backend.

---

### ISSUE 3 — `OpenAIReqToClaudeCLI`: consecutive same-role messages not merged

**File:** `openai_claude_cli.go:210-237`

Same class of bug as Issue 1. Messages are converted one-by-one via `convertOpenAIMessageToClaudeCLI(msg)` and appended without role-alternation checking:

```go
// line 233-234
messages = append(messages, convertOpenAIMessageToClaudeCLI(msg))
i++
```

---

### ISSUE 4 — `ClaudeReqToOpenAI`: text placed before tool_result breaks OpenAI ordering

**File:** `claude_openai.go:120-146`

When a Claude `user` message contains **both** `text` and `tool_result` blocks (a valid Claude pattern — e.g., submitting tool results with follow-up text), the converter emits:

1. A `user` message with the text content (line 137)
2. `tool` messages for each tool_result (line 146)

```go
// line 121-137 — adds user message with text first
if len(textParts) > 0 || len(imageParts) > 0 || len(toolCalls) > 0 {
    // ...
    messages = append(messages, openaiMsg)
}
// line 146 — adds tool messages after
messages = append(messages, toolResults...)
```

Per OpenAI spec, `tool` role messages **must** immediately follow the `assistant` message that contained the corresponding `tool_calls`. The inserted `user` message between the preceding `assistant` (with `tool_calls`) and the `tool` messages violates this constraint.

**Correct order should be:** tool results first, then user text.

---

### ISSUE 5 — `augment/to_claude.go`: creates two consecutive `user` messages

**File:** `augment/to_claude.go:80-136` (`appendClaudeNodesAsUser`)

When the node set contains **both** tool_result nodes (type=1) **and** text/IDE-state content, the function creates two separate `user` messages:

1. Line 95: `*msgs = append(*msgs, map[string]interface{}{"role": "user", "content": content})` — tool_result user message
2. Line 134: `*msgs = append(*msgs, map[string]interface{}{"role": "user", "content": content})` — text user message

Both have `"role": "user"`, producing consecutive `user` messages that violate Claude's alternation requirement.

**Impact:** Augment requests containing both tool results and text in the same turn trigger a 400 from Claude.

---

## Angle 2: Content Type Handling Edge Cases

### ISSUE 6 — `OpenAIReqToClaude`: null/nil content produces message without `content` key

**File:** `claude_openai.go:285-327`

When an OpenAI message has `"content": null` (valid in OpenAI when `tool_calls` are present), the type switch at line 286 falls through — `nil` matches neither `string` nor `[]interface{}`:

```go
if content, ok := msg["content"]; ok {
    switch c := content.(type) {
    case string:
        claudeMsg["content"] = c
    case []interface{}:
        claudeMsg["content"] = convertOpenAIContentToClaude(c)
    }
    // nil falls through — claudeMsg["content"] never set
}
```

If the message also has no `tool_calls` (lines 295-325 only set content when tool_calls exist), the resulting Claude message is `{"role": "assistant"}` with no `content` key. Claude API requires `content` to be present.

**Impact:** Messages like `{"role": "assistant", "content": null}` without tool_calls produce invalid Claude requests.

---

### ISSUE 7 — `ClaudeReqToOpenAI`: nil tool `input` marshaled as `"null"` string

**File:** `claude_openai.go:90`

```go
args, _ := json.Marshal(m["input"])
```

When a Claude `tool_use` block has `"input": null` (edge case — normally an object), `json.Marshal(nil)` returns `[]byte("null")`. The resulting OpenAI tool call gets `arguments: "null"` — a JSON null literal, not a valid JSON object string.

OpenAI expects `arguments` to be a stringified JSON object (e.g., `"{}"`). Some OpenAI-compatible backends reject `"null"` as an invalid arguments value.

**Impact:** Malformed tool calls with null input silently produce technically invalid OpenAI arguments.

---

### ISSUE 8 — `ClaudeReqToOpenAI`: messages with nil content silently dropped

**File:** `claude_openai.go:40-147`

If a Claude message has `nil` content (neither `string` nor `[]interface{}`), the type switch at line 40 falls through without adding any message or tool results. The message is silently dropped:

```go
switch content := msg.Content.(type) {
case string:
    // handled
case []interface{}:
    // handled
// nil: falls through — nothing added
}
```

If the dropped message was an `assistant` message, the next `user` message could follow a previous `user` message, breaking alternation.

---

## Angle 3: Request Field Passthrough/Loss

### ISSUE 9 — `top_p`, `top_k`, `stop_sequences`/`stop` silently dropped in ALL converters

These sampling parameters are supported by ALL target APIs but are never forwarded during conversion:

| Source → Target | `top_p` | `top_k` | `stop`/`stop_sequences` |
|---|---|---|---|
| `ClaudeReqToOpenAI` (claude_openai.go:150-161) | ❌ Dropped | ❌ Dropped | ❌ Dropped |
| `OpenAIReqToClaude` (claude_openai.go:225-232) | ❌ Dropped | N/A (OpenAI lacks) | ❌ Dropped |
| `OpenAIReqToGemini` (openai_gemini.go:108-120) | ❌ Dropped | N/A | ❌ Dropped |
| `ClaudeReqToGemini` (claude_gemini.go:52-65) | ❌ Dropped | ❌ Dropped | ❌ Dropped |
| `GeminiReqToClaude` (claude_gemini.go:169-177) | ❌ Dropped | ❌ Dropped | ❌ Dropped |
| `OpenAIReqToClaudeCLI` (openai_claude_cli.go:294-307) | ❌ Dropped | N/A | ❌ Dropped |

**Evidence:** Searched all convert/*.go files for `top_p`, `top_k`, `stop_seq`, `frequency_penalty`, `presence_penalty` — zero matches in request construction code (only found in response `stop_sequence` fields).

**Field-by-field analysis:**

- **`top_p`**: Supported by Claude (as `top_p`), OpenAI (as `top_p`), Gemini (as `topP` in generationConfig). Never forwarded in any direction.
- **`top_k`**: Supported by Claude (as `top_k`), Gemini (as `topK` in generationConfig). Never forwarded.
- **`stop` / `stop_sequences`**: Claude uses `stop_sequences` (array), OpenAI uses `stop` (string or array), Gemini uses `stopSequences` (array in generationConfig). Never forwarded.
- **`presence_penalty` / `frequency_penalty`**: Supported by OpenAI and Gemini but NOT Claude. Correctly not forwarded to Claude, but also not forwarded OpenAI↔Gemini.
- **`stream_options`**: `ClaudeReqToOpenAI` correctly adds `stream_options` (line 201-203). `toOpenAIRequest` in augment also adds it (line 24). ✅
- **`max_tokens` / `max_completion_tokens`**: Correctly mapped in all directions. ✅

**Impact:** Users setting `top_p=0.5` or `stop_sequences=["END"]` on the client side have those parameters silently ignored, potentially producing qualitatively different outputs than intended.

---

### ISSUE 10 — `GeminiReqToClaude`: `toolConfig` (tool_choice equivalent) not forwarded

**File:** `claude_gemini.go:169-197`

Gemini's `toolConfig` field (which controls tool-calling behavior, equivalent to Claude's `tool_choice`) is not read or forwarded. If a Gemini client sets `toolConfig.functionCallingConfig.mode = "ANY"`, this is silently dropped when proxying to Claude.

```go
// Lines 169-177 — only temperature, maxOutputTokens, and tools are forwarded
if req.GenerationConfig != nil {
    if req.GenerationConfig.MaxOutputTokens != nil {
        claudeReq["max_tokens"] = *req.GenerationConfig.MaxOutputTokens
    }
    if req.GenerationConfig.Temperature != nil {
        claudeReq["temperature"] = *req.GenerationConfig.Temperature
    }
}
// toolConfig is never checked
```

---

## Angle 4: Error Response Handling

### ISSUE 11 — Non-retryable error responses passed through without format transformation

**File:** `proxy.go:910-921`

When an upstream API returns a non-retryable error (400 Bad Request, 401 Unauthorized, etc. per `shouldRetry` in utils.go:21-25), the raw error response body is passed through to the client without any format transformation:

```go
// line 919-920
w.WriteHeader(resp.StatusCode)
w.Write(respBody)
```

Error response format varies by API:
- **Claude**: `{"type": "error", "error": {"type": "invalid_request_error", "message": "..."}}`
- **OpenAI**: `{"error": {"message": "...", "type": "invalid_request_error", "code": "..."}}`
- **Gemini**: `{"error": {"code": 400, "message": "...", "status": "INVALID_ARGUMENT"}}`

A Claude Code client hitting an OpenAI backend gets an OpenAI-format error, which Claude Code may not parse correctly.

**Impact:** Error messages are not consistently formatted for the client, potentially causing confusing error displays or client-side parsing failures.

---

### ISSUE 12 — Streaming error events silently ignored in most converters

Most streaming converters have no handling for upstream error events. Only `ClaudeStreamToOpenAI2` (claude_openai2.go:337-341) explicitly detects error events:

```go
// claude_openai2.go:338-341 — GOOD: detects errors
if errType, ok := data["type"].(string); ok && errType == "error" {
    if errData, ok := data["error"].(map[string]interface{}); ok {
        if msg, ok := errData["message"].(string); ok {
            return nil, fmt.Errorf("upstream error: %s", msg)
        }
    }
}
```

**Missing error detection in:**

| Converter | File:Line | Issue |
|---|---|---|
| `ClaudeStreamToOpenAI` | claude_openai.go:553 | No `"error"` case in event type switch — falls to default `return nil, nil` |
| `OpenAIStreamToClaude` | claude_openai.go:704 | No OpenAI streaming error detection |
| `GeminiStreamToOpenAI` | openai_gemini.go:222 | No Gemini streaming error detection |
| `GeminiStreamToClaude` | claude_gemini.go:434 | No Gemini streaming error detection |
| `ClaudeStreamToGemini` | claude_gemini.go:341 | No Claude error event detection |

Claude streaming emits `event: error` with `{"type": "error", "error": {...}}`. If the upstream Claude API returns an error mid-stream (e.g., overloaded), these converters silently swallow it and the client sees an incomplete response with no error indication.

**Impact:** Mid-stream errors from upstream APIs are silently dropped, producing truncated responses with no error signal to the client.

---

## Angle 5: Concurrency Safety in Streaming

### No issues found.

- **`toolCallCounter`** (`common.go:14-19`): Uses `atomic.AddInt64` — safe for concurrent access. ✅
- **`StreamContext`**: Per-request (created in `streaming.go:77` via `NewStreamContext()`), not shared across goroutines. ✅
- **`cliSessionID`/`cliUserID`** (`openai_claude_cli.go:42-46`): Protected by `sync.Once`. ✅
- **`CliSystemPrompt`** (`openai_claude_cli.go:34-38`): Package-level map, initialized once, never mutated. Callers create new slices containing a reference (line 196) but never modify the map itself. ✅
- **`buildClaudeEvent`** (`common.go:63-67`): Mutates input `data` map by adding `"type"` key, but all callers pass freshly-constructed map literals — no aliasing. ✅
- **Proxy struct fields**: Protected by dedicated mutexes (`mu`, `activeRequestsMu`, `ctxMu`). `http.Client` is documented as safe for concurrent use. ✅

---

## Summary

| # | Severity | Angle | Issue | Location |
|---|---|---|---|---|
| 1 | **High** | Ordering | Consecutive same-role messages not merged (→Claude) | claude_openai.go:282 |
| 2 | **High** | Ordering | Consecutive same-role messages not merged (→Gemini) | openai_gemini.go:96 |
| 3 | **High** | Ordering | Consecutive same-role messages not merged (→CLI) | openai_claude_cli.go:233 |
| 4 | **Medium** | Ordering | tool_result placed after text message breaks OpenAI ordering | claude_openai.go:137,146 |
| 5 | **Medium** | Ordering | Two consecutive user messages in augment path | augment/to_claude.go:95,134 |
| 6 | **Medium** | Content | Null content produces message without content key | claude_openai.go:285-292 |
| 7 | **Low** | Content | Nil tool input marshaled as "null" string | claude_openai.go:90 |
| 8 | **Medium** | Content | Messages with nil content silently dropped | claude_openai.go:40 |
| 9 | **Medium** | Fields | top_p/top_k/stop silently dropped everywhere | All convert files |
| 10 | **Low** | Fields | Gemini toolConfig not forwarded to Claude | claude_gemini.go:169 |
| 11 | **Medium** | Errors | Error responses passed without format transformation | proxy.go:919-920 |
| 12 | **Medium** | Errors | Streaming errors silently ignored in 5 of 6 converters | Multiple files |
