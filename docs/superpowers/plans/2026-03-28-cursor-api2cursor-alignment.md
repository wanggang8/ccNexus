# Cursor api2cursor Alignment Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/cursor/v1/chat/completions`, `/cursor/v1/responses`, `/cursor/v1/messages` data semantics match `/tmp/api2cursor` for request/response/stream/tools/think/cache while preserving ccNexus config and normal `/v1/*` behavior.

**Architecture:** Keep Cursor-only behavior inside `internal/cursor/*` and use `internal/proxy/*` only as thin wiring. For Cursor chat + responses backend, force explicit CC→Responses→CC bridging at the Cursor layer to mirror api2cursor event semantics.

**Tech Stack:** Go, existing `internal/cursor`, `internal/proxy`, `internal/transformer/convert`.

---

## File Structure

- Modify: `internal/cursor/request/normalize.go` — ensure input/messages coercion and tool_choice normalization remain api2cursor-aligned.
- Modify: `internal/cursor/request/compat.go` — ensure Claude cache_control and max_tokens floor use api2cursor-equivalent behavior.
- Modify: `internal/cursor/response/chat.go` — align Chat response fixes with api2cursor (reasoning/tool_calls).
- Modify: `internal/cursor/response/responses.go` — align Responses output/tool args fixes with api2cursor.
- Modify: `internal/cursor/response/messages.go` — align Messages reasoning_content → thinking block injection.
- Modify: `internal/cursor/stream/chat.go` — align chat SSE split rules and `[DONE]` emission.
- Modify: `internal/cursor/stream/responses.go` — align Responses SSE event ordering and output reconstruction.
- Modify: `internal/cursor/stream/messages.go` — align Messages SSE thinking injection and index offsets.
- Modify: `internal/cursor/route/target.go` — ensure Cursor path resolves correct upstream paths when bridging.
- Modify: `internal/proxy/request.go` — ensure Cursor-mode routing applies proper bridge behavior.
- Modify: `internal/transformer/convert/openai_openai2.go` — align Responses ↔ Chat conversion semantics with api2cursor.
- Modify: `internal/transformer/convert/anthropic_common.go` — validate cache_control algorithm parity with api2cursor.

---

## Chunk 1: Validate Existing Behaviors vs api2cursor (No Code Changes Yet)

### Task 1: Document semantic equivalence and differences

**Files:**
- Modify: `docs/superpowers/plans/2026-03-28-cursor-api2cursor-alignment.md` (this plan, keep notes inline)

- [ ] **Step 1: Verify existing Chat normalization parity**

Check that `NormalizeRequestBody` for `/v1/chat/completions`:
- Converts `input` → `messages`
- Normalizes `tool_choice` object → string
- Normalizes tools to function schema

Expected: behavior already matches api2cursor.

- [ ] **Step 2: Verify existing Responses normalization parity**

Check that `/v1/responses`:
- Converts `messages` → `input`
- Preserves `tool_choice`, `tools`, `temperature`, `top_p`

Expected: behavior mostly matches, but must confirm OpenAI2 conversion matches api2cursor adapter rules.

- [ ] **Step 3: Verify `[DONE]` behavior**

Check that:
- Chat streams emit `[DONE]`
- Responses streams suppress `[DONE]`

Expected: already true. Any discrepancy → record.

---

## Chunk 2: Chat Path (CC) + Responses Backend Bridge

### Task 2: Force CC→Responses→CC bridging for Cursor chat + openai2 backend

**Files:**
- Modify: `internal/proxy/request.go`
- Modify: `internal/cursor/stream/chat.go`
- Modify: `internal/cursor/stream/responses.go`
- Test: `internal/proxy/cursor_roundtrip_test.go`

**Goal:** Ensure `/cursor/v1/chat/completions` routed to responses backend behaves exactly like api2cursor’s `cc_to_responses_request` + `responses_to_cc_response` + `ResponsesToCCStreamConverter` chain.

- [ ] **Step 1: Add regression test for chat→responses bridge semantics**

Test should assert:
- tool_calls are reconstructed per api2cursor
- reasoning_content from Responses turns into Chat chunk reasoning_content
- final stream includes `[DONE]`

- [ ] **Step 2: Run test and confirm current failure**

Run: `go test ./internal/proxy -run TestCursorChatResponsesBackendUsesResponsesBridge`
Expected: FAIL (if bridge semantics differ).

- [ ] **Step 3: Implement explicit bridge**

Implementation idea:
- When CursorMode + ClientFormatOpenAIChat + transformer `cx_chat_openai2`,
  bypass standard transformer stream rewrite and instead:
  - Convert request to Responses shape (OpenAIReqToOpenAI2)
  - For non-stream: convert Responses response back to Chat (OpenAI2RespToOpenAI)
  - For stream: use OpenAI2StreamToOpenAI to emit Chat chunks

- [ ] **Step 4: Re-run test**

Run: `go test ./internal/proxy -run TestCursorChatResponsesBackendUsesResponsesBridge`
Expected: PASS

---

## Chunk 3: Responses Path (OpenAI Responses) Semantics

### Task 3: Align Responses input/output mapping with api2cursor

**Files:**
- Modify: `internal/transformer/convert/openai_openai2.go`
- Test: `internal/transformer/convert/openai_openai2_test.go`

**Target parity with api2cursor `responses_cc_adapter.py`:**
- EasyInputMessage path for user/assistant when no tool_calls
- function_call / function_call_output mapping
- instructions derived from system messages

- [ ] **Step 1: Add parity tests**

Create tests mirroring api2cursor rules for:
- assistant tool_calls → Responses function_call items
- tool results → function_call_output
- system → instructions

- [ ] **Step 2: Run tests and confirm differences**

Run: `go test ./internal/transformer/convert -run OpenAIReqToOpenAI2` (and new tests)
Expected: identify mismatches.

- [ ] **Step 3: Implement mapping fixes**

Adjust `OpenAIReqToOpenAI2` / `OpenAI2ReqToOpenAI` to match api2cursor’s `_append_responses_input_item` and `_convert_input_items` behaviors.

- [ ] **Step 4: Re-run tests**

Run: `go test ./internal/transformer/convert -run OpenAIReqToOpenAI2`
Expected: PASS

---

## Chunk 4: Responses SSE Event Ordering & Output Reconstruction

### Task 4: Ensure Responses SSE matches api2cursor

**Files:**
- Modify: `internal/cursor/stream/responses.go`
- Test: `internal/cursor/stream/responses_test.go` (create)

- [ ] **Step 1: Add SSE ordering tests**

Assertions:
- `response.created` emitted before any output
- `response.completed` emitted at end
- output reconstruction includes reasoning + text + tool calls in correct order
- no `[DONE]`

- [ ] **Step 2: Run tests and confirm differences**

Run: `go test ./internal/cursor/stream -run ResponsesStream`
Expected: FAIL if order differs.

- [ ] **Step 3: Fix ordering and reconstruction**

Align state machine to api2cursor’s `ResponsesStreamConverter` semantics:
- reasoning_summary_text.delta/done → reasoning output item
- output_text.delta/done → message output item
- function_call_arguments.* → function_call output item

- [ ] **Step 4: Re-run tests**

Run: `go test ./internal/cursor/stream -run ResponsesStream`
Expected: PASS

---

## Chunk 5: Claude cache_control & max_tokens floor

### Task 5: Verify cache_control algorithm parity

**Files:**
- Modify: `internal/transformer/convert/anthropic_common.go`
- Modify: `internal/cursor/request/compat.go`
- Test: `internal/cursor/request/compat_test.go`

- [ ] **Step 1: Add tests matching api2cursor**

Assertions based on api2cursor:
- clear existing cache_control
- inject anchors on tools[-1], system[-1], last cacheable block
- window anchor if enough blocks

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cursor/request -run CacheControl`
Expected: reveal mismatches.

- [ ] **Step 3: Adjust injection points or enable OptimizeAnthropicCacheControl**

Ensure Cursor Claude request passes through `OptimizeAnthropicCacheControl` when api2cursor would do so.

- [ ] **Step 4: Re-run tests**

Run: `go test ./internal/cursor/request -run CacheControl`
Expected: PASS

---

## Chunk 6: Messages Path Semantics

### Task 6: Verify Messages reasoning injection and stream index offset

**Files:**
- Modify: `internal/cursor/response/messages.go`
- Modify: `internal/cursor/stream/messages.go`
- Test: `internal/cursor/response/messages_test.go` (create)

- [ ] **Step 1: Add tests**

Assertions:
- reasoning_content/reasoningContent injected as thinking block at content[0]
- stream emits thinking events before first text_delta and offsets indexes

- [ ] **Step 2: Run tests**

Run: `go test ./internal/cursor/response -run Messages` and `go test ./internal/cursor/stream -run Messages`
Expected: identify mismatches if any.

- [ ] **Step 3: Fix differences**

Adjust injection ordering or offset logic to match api2cursor.

- [ ] **Step 4: Re-run tests**

Run: `go test ./internal/cursor/response -run Messages` and `go test ./internal/cursor/stream -run Messages`
Expected: PASS

---

## Chunk 7: End-to-End Cursor Matrix

### Task 7: Ensure routing matrix matches api2cursor

**Files:**
- Modify: `internal/proxy/cursor_matrix_test.go`
- Modify: `internal/proxy/cursor_roundtrip_test.go`

- [ ] **Step 1: Add coverage for all backends**

- `/cursor/v1/chat/completions` → openai/openai2/claude/gemini
- `/cursor/v1/responses` → openai/openai2/claude/gemini
- `/cursor/v1/messages` → claude only

- [ ] **Step 2: Run tests**

Run: `go test ./internal/proxy -run CursorMatrix`
Expected: PASS

---

## Verification

- [ ] Run: `go test ./internal/proxy ./internal/cursor/... ./internal/transformer/convert ./internal/transformer/cx/chat/... ./internal/transformer/cx/responses/...`
  - Expected: PASS

---

## Plan Review Loop

No plan-document-reviewer available in repo; proceed without automated review.

