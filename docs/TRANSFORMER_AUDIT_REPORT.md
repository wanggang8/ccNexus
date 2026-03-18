# Protocol Transformation Code Audit Report

**Date**: 2026-03-18
**Scope**: All non-test `.go` files in `internal/transformer/convert/`, `cc/`, `cx/chat/`, `cx/responses/`, `passthrough/`

---

## Executive Summary

Audited **25 source files** covering 5 API formats (Claude, Claude CLI, OpenAI Chat, OpenAI Responses, Gemini) across 3 client families (CC, CX Chat, CX Responses) plus core conversion logic. Found **28 issues**: 3 HIGH, 14 MEDIUM, 11 LOW severity.

The most critical issue is that `cc/cli.go` passes Claude-format input to a function expecting OpenAI-format input and returns OpenAI-format responses to a client expecting Claude format — both request and response conversion are wrong.

---

## Architecture Overview

```
Client Format          Transformer         Backend Format
─────────────         ───────────         ──────────────
Claude Code ──────► cc/claude.go    ───► Claude API
  (/v1/messages)   cc/openai.go    ───► OpenAI Chat
                   cc/openai2.go   ───► OpenAI Responses
                   cc/gemini.go    ───► Gemini
                   cc/cli.go       ───► Claude CLI

Codex Chat ───────► cx/chat/claude.go  ──► Claude API
  (/v1/chat/...)   cx/chat/openai.go  ──► OpenAI Chat
                   cx/chat/openai2.go ──► OpenAI Responses
                   cx/chat/gemini.go  ──► Gemini
                   cx/chat/cli.go     ──► Claude CLI

Codex Responses ──► cx/resp/claude.go  ──► Claude API
  (/v1/responses)  cx/resp/openai.go  ──► OpenAI Chat
                   cx/resp/openai2.go ──► OpenAI Responses
                   cx/resp/gemini.go  ──► Gemini
                   cx/resp/cli.go     ──► Claude CLI
```

Each transformer calls functions in `convert/` for the actual conversion logic.

---

## Issues by Severity

### HIGH Severity (3)

---

#### H1: `cc/cli.go` — Request conversion uses wrong input format

- **File**: `internal/transformer/cc/cli.go:24-26`
- **Conversion path**: CC (Claude format) → Claude CLI
- **Description**: `TransformRequest` calls `convert.OpenAIReqToClaudeCLI(req, ...)` which expects **OpenAI Chat format** input. However, CC transformers receive **Claude format** input (from `/v1/messages` endpoint). This causes:
  - System prompts (`system` field) to be ignored (Claude puts them at top level; OpenAI puts them in messages)
  - Tool use blocks (`tool_use` in content) to not be converted (OpenAI uses `tool_calls` field)
  - Tool results (`tool_result` in content) to not be converted (OpenAI uses `tool` role messages)
  - Thinking blocks to be silently lost
- **Impact**: The CC→CLI transformation path is fundamentally broken for any request with system prompts, tools, or thinking. Simple text-only messages may accidentally work because both formats use `messages` array with `role`/`content`, but the content structure diverges for complex cases.
- **Fix**: Either create a `ClaudeReqToClaudeCLI` function that accepts Claude-format input, or first convert Claude→OpenAI then pipe to `OpenAIReqToClaudeCLI`.

---

#### H2: `cc/cli.go` — Response conversion returns wrong output format

- **File**: `internal/transformer/cc/cli.go:40-53`
- **Conversion path**: Claude CLI response → CC (Claude format)
- **Description**: Both `TransformResponse` and `TransformResponseWithContext` convert CLI responses (which are in Claude format) to **OpenAI format** via `ClaudeRespToOpenAI` / `ClaudeStreamToOpenAI`. However, CC clients expect **Claude format** responses. The CLI backend returns native Claude API responses, so CC→CLI should be a near-passthrough on the response side (like `cc/claude.go`).
- **Impact**: Claude Code client receives OpenAI-format responses when using a CLI endpoint, causing parsing failures or data loss. Both streaming and non-streaming paths are affected.
- **Fix**: Response transformation should be passthrough (or use the same logic as `cc/claude.go`).

---

#### H3: `convert/openai_gemini.go` — `OpenAIStreamToGemini` drops tool calls

- **File**: `internal/transformer/convert/openai_gemini.go:275-306`
- **Conversion path**: OpenAI Chat stream → Gemini stream
- **Description**: The function only handles `delta.Content` (text). Tool call deltas (`delta.ToolCalls`) are completely ignored — no `functionCall` parts are emitted. Finish reason is also not forwarded.
- **Impact**: Any streaming response containing tool calls from an OpenAI backend being converted to Gemini format will lose all tool call data silently. This affects the `cx_chat_gemini` path when the Gemini backend actually proxies to an OpenAI endpoint.
- **Fix**: Add tool call handling similar to `GeminiStreamToOpenAI` (in reverse).

---

### MEDIUM Severity (14)

---

#### M1: `convert/claude_openai.go` — `OpenAIReqToClaude` missing `tool_choice` conversion

- **File**: `internal/transformer/convert/claude_openai.go:204-374`
- **Conversion path**: OpenAI Chat → Claude
- **Description**: The function converts tools but never maps `tool_choice`. OpenAI `"required"` should map to Claude `{type: "any"}`, OpenAI `"none"` to not sending tools, and `{type: "function", function: {name: "X"}}` to `{type: "tool", name: "X"}`.
- **Impact**: Tool choice preferences from the client are silently discarded. The model may not force tool use when intended by the client.

---

#### M2: `convert/claude_openai.go` — Incomplete `stop_reason`↔`finish_reason` mapping

- **File**: `internal/transformer/convert/claude_openai.go:419-421` and `467`
- **Conversion path**: Claude↔OpenAI (both directions, non-streaming)
- **Description**: Only `tool_use`↔`tool_calls` is mapped. Missing: Claude `max_tokens` → OpenAI `length`, OpenAI `length` → Claude `max_tokens`, OpenAI `content_filter` → Claude `end_turn` (fallback).
- **Impact**: When the model stops due to token limits, the reason is misreported as `stop`/`end_turn` instead of `length`/`max_tokens`.

---

#### M3: `convert/claude_openai.go` — Temperature=0 is silently dropped

- **File**: `internal/transformer/convert/claude_openai.go:152-153`
- **Conversion path**: Claude → OpenAI
- **Description**: `if req.Temperature > 0` excludes `Temperature == 0`. Since `ClaudeRequest.Temperature` is `float64` (zero value = 0.0), an explicitly set `temperature: 0` (deterministic output) is indistinguishable from "not set" and is dropped.
- **Impact**: Requests explicitly requesting deterministic output (`temperature: 0`) will use the model's default temperature instead.

---

#### M4: `convert/claude_openai2.go` — Missing `message_delta` before `message_stop` in `OpenAI2StreamToClaude` [DONE] handler

- **File**: `internal/transformer/convert/claude_openai2.go:504-530`
- **Conversion path**: OpenAI Responses stream → Claude stream
- **Description**: When `[DONE]` is received without a prior `response.completed`, the handler only emits `message_stop` but not `message_delta` with `stop_reason`. The Claude streaming protocol requires `message_delta` with `stop_reason` before `message_stop`. Compare with `OpenAIStreamToClaude` which correctly emits both.
- **Impact**: If `response.completed` is not sent (e.g., early termination), Claude Code won't receive a stop_reason, potentially causing parsing issues.

---

#### M5: `convert/claude_gemini.go` — `GeminiStreamToClaude` doesn't handle Gemini thought parts

- **File**: `internal/transformer/convert/claude_gemini.go:407-515`
- **Conversion path**: Gemini stream → Claude stream
- **Description**: The streaming handler checks `part.Text != ""` but never checks `part.Thought`. A Gemini thought part (with `thought: true`) would be emitted as a regular text content block instead of a Claude `thinking` block. The non-streaming `GeminiRespToClaude` correctly handles this.
- **Impact**: Gemini thinking/reasoning content is presented as regular text in streaming mode, confusing the Claude Code client.

---

#### M6: `convert/claude_gemini.go` — URL-based images silently dropped

- **File**: `internal/transformer/convert/claude_gemini.go:556-569`
- **Conversion path**: Claude → Gemini (request)
- **Description**: `convertClaudeContentToGeminiParts` only handles `base64` image sources. Images with `type: "url"` are silently dropped.
- **Impact**: URL-referenced images from Claude requests are lost during conversion to Gemini.

---

#### M7: `convert/claude_gemini.go` — `ClaudeReqToGemini` ignores `tool_choice`

- **File**: `internal/transformer/convert/claude_gemini.go:13-84`
- **Conversion path**: Claude → Gemini (request)
- **Description**: Gemini's `toolConfig.functionCallingConfig.mode` is hardcoded to `"AUTO"`. Claude's `tool_choice` is not mapped to Gemini's `"ANY"`, `"NONE"`, or specific tool forcing mode.
- **Impact**: Claude's `tool_choice: {type: "any"}` (required) or `{type: "tool", name: "X"}` (forced) is ignored; Gemini always uses AUTO mode.

---

#### M8: `convert/openai_gemini.go` — `OpenAIReqToGemini` ignores `tool_choice`

- **File**: `internal/transformer/convert/openai_gemini.go:13-146`
- **Conversion path**: OpenAI Chat → Gemini (request)
- **Description**: Same issue as M7 — Gemini's toolConfig is hardcoded to `"AUTO"`. OpenAI's `tool_choice` is not forwarded.
- **Impact**: OpenAI `tool_choice: "required"` or `{type: "function", function: {name: "X"}}` is ignored.

---

#### M9: `convert/openai_openai2.go` — `OpenAI2ReqToOpenAI` doesn't forward temperature

- **File**: `internal/transformer/convert/openai_openai2.go:122-238`
- **Conversion path**: OpenAI Responses → OpenAI Chat (request)
- **Description**: Temperature from `OpenAI2Request.Temperature` is not forwarded to the constructed `OpenAIRequest`. Other fields like `MaxOutputTokens` and `ToolChoice` are correctly forwarded.
- **Impact**: Temperature preference from Responses API requests is silently dropped when proxying to Chat API.

---

#### M10: `convert/openai_openai2.go` — `OpenAI2StreamToOpenAI` missing initial role chunk

- **File**: `internal/transformer/convert/openai_openai2.go:564-639`
- **Conversion path**: OpenAI Responses stream → OpenAI Chat stream
- **Description**: OpenAI Chat streaming expects an initial chunk with `{"delta": {"role": "assistant", "content": ""}}`. `ClaudeStreamToOpenAI` correctly emits this on `message_start`, but `OpenAI2StreamToOpenAI` does not emit an initial role chunk on `response.created`.
- **Impact**: Some OpenAI Chat streaming clients may not properly initialize the assistant message without the initial role chunk.

---

#### M11: `convert/openai_openai2.go` — `OpenAI2StreamToOpenAI` doesn't increment `ToolIndex` for multiple tool calls

- **File**: `internal/transformer/convert/openai_openai2.go:589-611`
- **Conversion path**: OpenAI Responses stream → OpenAI Chat stream
- **Description**: When multiple `response.output_item.added` events arrive for different function calls, `ctx.ToolIndex` is never incremented. All tool calls are emitted with `"index": 0` in the OpenAI Chat chunk. Additionally, `ctx.ToolBlockStarted` is overwritten by each new tool call, potentially losing state.
- **Impact**: Clients that rely on tool call index for matching will see all tool calls at index 0, making multi-tool-call responses ambiguous.

---

#### M12: `convert/openai_openai2.go` — `OpenAIStreamToOpenAI2` doesn't track usage from OpenAI chunks

- **File**: `internal/transformer/convert/openai_openai2.go:399-562`
- **Conversion path**: OpenAI Chat stream → OpenAI Responses stream
- **Description**: When an OpenAI streaming chunk arrives with `Usage` data (from `stream_options.include_usage`), the usage is not stored in `ctx.InputTokens`/`ctx.OutputTokens`. This means the `response.completed` event emits zero tokens.
- **Impact**: Token usage reporting in Responses format output is always zero when proxying from OpenAI Chat streaming.

---

#### M13: `convert/openai_gemini.go` and `convert/claude_gemini.go` — Gemini `MAX_TOKENS` finish reason not mapped

- **File**: `openai_gemini.go:260-263`, `claude_gemini.go:497-510`
- **Conversion path**: Gemini → OpenAI/Claude (streaming)
- **Description**: Gemini's `MAX_TOKENS` finish reason should map to OpenAI's `length` or Claude's `max_tokens`. Currently it falls through to `stop`/`end_turn`.
- **Impact**: Token-limited responses from Gemini are reported as normal completions.

---

#### M14: `convert/claude_openai2.go` and `convert/openai2_gemini.go` — Image content types dropped in OpenAI2→Claude/Gemini conversion

- **File**: `claude_openai2.go:813-837`, `openai2_gemini.go:427-450`
- **Conversion path**: OpenAI Responses → Claude/Gemini (request)
- **Description**: `convertOpenAI2ContentToClaude` and `convertOpenAI2ContentToGeminiParts` only handle `input_text` and `output_text` content types. Image types (e.g., `input_image`) are silently dropped.
- **Impact**: Multimodal (image) content from Responses API requests is lost during conversion.

---

### LOW Severity (11)

---

#### L1: `convert/claude_gemini.go` — `GeminiRespToClaude` hardcoded response ID

- **File**: `internal/transformer/convert/claude_gemini.go:303`
- **Description**: All Gemini→Claude responses have `"id": "gemini-resp"`. Not unique per response.
- **Impact**: Response deduplication or tracking by ID may be unreliable.

---

#### L2: `convert/openai_gemini.go` — Hardcoded chunk IDs in `GeminiStreamToOpenAI`

- **File**: `internal/transformer/convert/openai_gemini.go:237`
- **Description**: All streaming chunks have `"id": "gemini-chunk"`. OpenAI streaming protocol expects a consistent ID across chunks of the same response.
- **Impact**: Cosmetic; most clients don't use the chunk ID.

---

#### L3: `convert/openai2_gemini.go` — Hardcoded `"call_0"`, `"call_1"` tool call IDs

- **File**: `internal/transformer/convert/openai2_gemini.go:106-107`
- **Description**: `GeminiRespToOpenAI2` uses sequential `call_N` IDs instead of globally unique IDs.
- **Impact**: Could cause ID collisions across multiple responses if client tracks call IDs globally.

---

#### L4: `convert/claude_openai2.go` — `ClaudeReqToOpenAI2` defaults to `tool_choice: "required"` on first turn

- **File**: `internal/transformer/convert/claude_openai2.go:78-84`
- **Description**: When no explicit `tool_choice` is set and no tool results exist in history, defaults to `"required"`. This forces tool use even when the model might want to respond with text.
- **Impact**: Overly aggressive tool forcing on first turn; may cause unexpected behavior with some backends.

---

#### L5: `convert/claude_openai2.go` — `max_output_tokens` intentionally skipped

- **File**: `internal/transformer/convert/claude_openai2.go:56-57`
- **Description**: Documented with TODO: skipped for third-party endpoint compatibility.
- **Impact**: Output length is unconstrained when proxying Claude→OpenAI2.

---

#### L6: `convert/openai_openai2.go` — `max_output_tokens` intentionally skipped

- **File**: `internal/transformer/convert/openai_openai2.go:91-92`
- **Description**: Same as L5, for OpenAI→OpenAI2 path.
- **Impact**: Output length is unconstrained.

---

#### L7: `convert/openai_openai2.go` — `OpenAI2StreamToOpenAI` emits complete tool call in one chunk

- **File**: `internal/transformer/convert/openai_openai2.go:604-611`
- **Description**: Instead of streaming tool call arguments incrementally, the full tool call is emitted at once in `response.output_item.done`. The `response.function_call_arguments.delta` events are accumulated but not forwarded.
- **Impact**: Technically valid but doesn't match expected incremental streaming behavior.

---

#### L8: `cc/claude.go` — `TransformResponseWithContext` has output_tokens fallback race

- **File**: `internal/transformer/cc/claude.go:87-88`
- **Description**: The fallback fills `output_tokens: 0` from `ctx.OutputTokens`, but `ctx.OutputTokens` may also be 0 (not yet populated), leading to no actual fallback.
- **Impact**: Cosmetic; token counts may show 0 in edge cases.

---

#### L9: `convert/claude_gemini.go` — `GeminiReqToClaude` doesn't set `stream` flag

- **File**: `internal/transformer/convert/claude_gemini.go:87-196`
- **Description**: The constructed Claude request doesn't include a `stream` field. Gemini uses URL-based streaming, so there's no `stream` field in the request body to forward.
- **Impact**: Negligible; the proxy layer handles streaming via separate URL paths.

---

#### L10: `convert/openai_gemini.go` — URL images dropped in `convertOpenAIContentToGeminiParts`

- **File**: `internal/transformer/convert/openai_gemini.go:309-334`
- **Description**: Only `data:` URL images are converted to Gemini `inlineData`. External URL images are silently dropped.
- **Impact**: External URL-referenced images from OpenAI requests are lost. Gemini supports URL images via `fileData` but that's not implemented.

---

#### L11: `passthrough/passthrough.go` — No streaming context support

- **File**: `internal/transformer/passthrough/passthrough.go:48-50`
- **Description**: `TransformResponseWithContext` delegates to `TransformResponse`, ignoring the context. This is correct for passthrough but means no token tracking.
- **Impact**: Negligible; passthrough by definition doesn't transform.

---

## Coverage Matrix

Each cell shows: `✓` = correct, `⚠` = has issues, `✗` = broken, `—` = N/A

### Request Conversion

| From \ To     | Claude | OpenAI Chat | OpenAI2 (Responses) | Gemini  | CLI     |
|---------------|--------|-------------|---------------------|---------|---------|
| **Claude**    | ✓ pass | ✓           | ⚠ M3,L4,L5         | ⚠ M7,M6| ✗ H1   |
| **OpenAI Chat**| ⚠ M1  | ✓ pass      | ⚠ L6               | ⚠ M8,L10| ✓      |
| **OpenAI2**   | ✓      | ⚠ M9        | ✓ pass              | ⚠ M14  | ✓ (chain)|

### Non-Streaming Response Conversion

| From \ To     | Claude | OpenAI Chat | OpenAI2 (Responses) | Gemini  |
|---------------|--------|-------------|---------------------|---------|
| **Claude**    | ✓ pass | ⚠ M2       | ✓                   | ✓       |
| **OpenAI Chat**| ⚠ M2  | ✓ pass     | ✓                   | ✓       |
| **OpenAI2**   | ✓      | ✓           | ✓ pass              | ✓       |
| **Gemini**    | ⚠ L1   | ⚠ L1,L2    | ⚠ L3               | —       |
| **CLI**       | ✗ H2   | ✓           | ✓                   | —       |

### Streaming Response Conversion

| From \ To     | Claude   | OpenAI Chat | OpenAI2 (Responses) | Gemini   |
|---------------|----------|-------------|---------------------|----------|
| **Claude SSE**| ✓ pass   | ✓           | ✓                   | ✓        |
| **OpenAI SSE**| ✓        | ✓ pass      | ⚠ M12              | ✗ H3     |
| **OpenAI2 SSE**| ⚠ M4   | ⚠ M10,M11  | ✓ pass              | ✓        |
| **Gemini SSE**| ⚠ M5,M13| ⚠ L2,M13   | ✓                   | —        |
| **CLI SSE**   | ✗ H2     | ✓           | ✓                   | —        |

---

## Recommendations

### Priority 1 (Fix Immediately)

1. **H1+H2**: Rewrite `cc/cli.go` to use Claude-format input/output. The simplest fix:
   - `TransformRequest`: first convert Claude→OpenAI via `ClaudeReqToOpenAI`, then pipe to `OpenAIReqToClaudeCLI`. Or create a dedicated `ClaudeReqToClaudeCLI` function.
   - `TransformResponse`: passthrough (CLI returns Claude format, CC expects Claude format).
   - `TransformResponseWithContext`: passthrough with `cc/claude.go`-style token fallback.

2. **H3**: Add tool call handling to `OpenAIStreamToGemini` — parse `delta.ToolCalls`, accumulate arguments, and emit `functionCall` parts.

### Priority 2 (Fix Soon)

3. **M1**: Add `tool_choice` mapping in `OpenAIReqToClaude`.
4. **M2**: Extend `stop_reason`↔`finish_reason` mapping tables in both directions.
5. **M4**: Add `message_delta` with `stop_reason` before `message_stop` in `OpenAI2StreamToClaude` [DONE] handler.
6. **M5**: Check `part.Thought` in `GeminiStreamToClaude` and emit `thinking` blocks.
7. **M9**: Forward `Temperature` in `OpenAI2ReqToOpenAI`.
8. **M11**: Increment `ToolIndex` in `OpenAI2StreamToOpenAI` for multiple tool calls.
9. **M12**: Track usage from OpenAI stream chunks in `OpenAIStreamToOpenAI2`.

### Priority 3 (Improve When Feasible)

10. All remaining MEDIUM and LOW items.

---

## Test Coverage Notes

Existing tests (`go test ./internal/transformer/... -v`) pass. Test coverage focuses on:
- Request conversion correctness (most conversion paths have basic tests)
- Multi-tool-call streaming (OpenAI→OpenAI2)
- Boundary conditions and edge cases

**Test gaps**:
- No tests for `cc/cli.go` with actual Claude-format input (existing test uses OpenAI-format, masking H1/H2)
- No streaming response conversion tests for most paths
- No tests for multimodal (image) conversion
- No tests for thinking/reasoning block conversion in streaming
- No tests for `OpenAIStreamToGemini` tool call handling (H3)
- No tests for `tool_choice` mapping in any direction
- `passthrough/` has no test files
