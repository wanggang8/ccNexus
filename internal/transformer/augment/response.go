package augment

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/lich0821/ccNexus/internal/tokencount"
)

// Augment protocol constants (from augment-protocol.js)
const (
	// Stop reasons
	augmentStopReasonUnspecified           = 0
	augmentStopReasonEndTurn               = 1
	augmentStopReasonMaxTokens             = 2
	augmentStopReasonToolUseRequested      = 3
	augmentStopReasonSafety                = 4
	augmentStopReasonRecitation            = 5
	augmentStopReasonMalformedFunctionCall = 6

	// Response node types
	augmentNodeTypeRawResponse        = 0
	augmentNodeTypeSuggestedQuestions = 1
	augmentNodeTypeMainTextFinished   = 2
	augmentNodeTypeToolUse            = 5
	augmentNodeTypeAgentMemory        = 6
	augmentNodeTypeToolUseStart       = 7
	augmentNodeTypeThinking           = 8
	augmentNodeTypeBillingMetadata    = 9
	augmentNodeTypeTokenUsage         = 10
)

const (
	usageFallbackMinEstimatedOutputTokens = 80
	usageFallbackOutputMismatchRatio      = 3
)

// Content block delta types
const (
	deltaTypeTextDelta      = "text_delta"
	deltaTypeInputJSONDelta = "input_json_delta"
	deltaTypeThinkingDelta  = "thinking_delta"
)

// ConvertSSEToNDJSON converts an SSE stream (from Claude/OpenAI) to NDJSON format
// expected by the Augment plugin. Returns the converted bytes and any error.
func ConvertSSEToNDJSON(sseData []byte, targetType string, toolCtx map[string]*ToolContext) ([]byte, error) {
	var out strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(string(sseData)), &out, targetType, toolCtx); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// StreamConvertSSEToNDJSON converts an SSE stream to NDJSON in a streaming fashion.
// It reads from r and writes converted NDJSON lines to w.
// Returns inputTokens, outputTokens, and any error.
func StreamConvertSSEToNDJSON(r io.Reader, w io.Writer, targetType string, toolCtx map[string]*ToolContext) (inputTokens, outputTokens int, err error) {
	switch targetType {
	case "claude", "cli":
		return streamConvertClaudeSSE(r, w, toolCtx)
	case "openai", "openai2":
		return streamConvertOpenAISSE(r, w, toolCtx)
	default:
		return 0, 0, fmt.Errorf("augment response: unsupported target type %q", targetType)
	}
}

type toolUseBuffer struct {
	id      string
	name    string
	input   strings.Builder
	active  bool
	started bool // Track if TOOL_USE_START has been emitted
}

func writeChunkLine(w io.Writer, obj map[string]interface{}) {
	line, err := json.Marshal(obj)
	if err != nil {
		// Log error but continue processing
		// In production, consider using a proper logger
		fmt.Fprintf(w, "{\"error\":\"json_marshal_failed\",\"text\":\"\"}\n")
		if f, ok := w.(interface{ Flush() }); ok {
			f.Flush()
		}
		return
	}
	_, _ = w.Write(line)
	_, _ = w.Write([]byte("\n"))
	if f, ok := w.(interface{ Flush() }); ok {
		f.Flush()
	}
}

func newBaseChunk(text string) map[string]interface{} {
	return map[string]interface{}{
		"text":                  text,
		"unknown_blob_names":    []interface{}{},
		"checkpoint_not_found":  false,
		"workspace_file_chunks": []interface{}{},
		"nodes":                 []interface{}{},
	}
}

func streamConvertClaudeSSE(r io.Reader, w io.Writer, toolCtx map[string]*ToolContext) (inputTokens, outputTokens int, err error) {
	scanner := bufio.NewScanner(r)
	var lastEventType string
	var buf toolUseBuffer
	nextNodeID := 1
	hasEmittedToolUse := false // Track if any tool_use was emitted for stop_reason fallback
	var usageAcc usageAccumulator
	var generatedText strings.Builder
	usageEmitted := false

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			lastEventType = ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			lastEventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		if _, ok := ev["type"].(string); !ok && lastEventType != "" {
			ev["type"] = lastEventType
		}
		typ, _ := ev["type"].(string)

		switch typ {
		case "content_block_delta":
			delta, _ := ev["delta"].(map[string]interface{})
			deltaType, _ := delta["type"].(string)
			switch deltaType {
			case deltaTypeTextDelta:
				text, _ := delta["text"].(string)
				if text != "" {
					generatedText.WriteString(text)
					writeChunkLine(w, newBaseChunk(text))
				}
			case deltaTypeInputJSONDelta:
				partial, _ := delta["partial_json"].(string)
				if buf.active && partial != "" {
					buf.input.WriteString(partial)
				}
			case deltaTypeThinkingDelta:
				thinking, _ := delta["thinking"].(string)
				if thinking != "" {
					thinkingNode := map[string]interface{}{
						"id":       nextNodeID,
						"type":     augmentNodeTypeThinking,
						"content":  "",
						"thinking": map[string]interface{}{"summary": thinking},
					}
					nextNodeID++
					chunk := newBaseChunk("")
					chunk["nodes"] = []interface{}{thinkingNode}
					writeChunkLine(w, chunk)
				}
			}

		case "message_start":
			if usage, ok := ev["usage"].(map[string]interface{}); ok {
				usageAcc.merge(usage)
			}
			if msg, ok := ev["message"].(map[string]interface{}); ok {
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					usageAcc.merge(usage)
				}
			}

		case "content_block_start":
			cb, _ := ev["content_block"].(map[string]interface{})
			cbType, _ := cb["type"].(string)
			if cbType == "tool_use" {
				buf = toolUseBuffer{}
				buf.active = true
				buf.id, _ = cb["id"].(string)
				buf.name, _ = cb["name"].(string)

				// Emit TOOL_USE_START node (type=7)
				toolUse := map[string]interface{}{
					"tool_name":   buf.name,
					"tool_use_id": buf.id,
					"input_json":  "",
				}
				// Add MCP fields if available
				if toolCtx != nil {
					if ctx, ok := toolCtx[buf.name]; ok {
						if ctx.McpServerName != "" {
							toolUse["mcp_server_name"] = ctx.McpServerName
						}
						if ctx.McpToolName != "" {
							toolUse["mcp_tool_name"] = ctx.McpToolName
						}
					}
				}
				startNode := map[string]interface{}{
					"id":       nextNodeID,
					"type":     augmentNodeTypeToolUseStart,
					"content":  "",
					"tool_use": toolUse,
				}
				nextNodeID++
				chunk := newBaseChunk("")
				chunk["nodes"] = []interface{}{startNode}
				writeChunkLine(w, chunk)
				buf.started = true
			}

		case "content_block_stop":
			if buf.active {
				// Emit TOOL_USE node (type=5) with complete input
				toolUse := map[string]interface{}{
					"tool_name":   buf.name,
					"tool_use_id": buf.id,
					"input_json":  buf.input.String(),
				}
				// Add MCP fields if available
				if toolCtx != nil {
					if ctx, ok := toolCtx[buf.name]; ok {
						if ctx.McpServerName != "" {
							toolUse["mcp_server_name"] = ctx.McpServerName
						}
						if ctx.McpToolName != "" {
							toolUse["mcp_tool_name"] = ctx.McpToolName
						}
					}
				}
				node := map[string]interface{}{
					"id":       nextNodeID,
					"type":     augmentNodeTypeToolUse,
					"content":  "",
					"tool_use": toolUse,
				}
				nextNodeID++
				chunk := newBaseChunk("")
				chunk["nodes"] = []interface{}{node}
				chunk["stop_reason"] = augmentStopReasonToolUseRequested
				writeChunkLine(w, chunk)
				buf.active = false
				hasEmittedToolUse = true
			}

		case "message_delta":
			delta, _ := ev["delta"].(map[string]interface{})
			if sr, ok := delta["stop_reason"].(string); ok && sr != "" {
				reason := mapClaudeStopReason(sr)
				// Fallback: if we already emitted tool_use but upstream sends end_turn/stop,
				// force TOOL_USE_REQUESTED so the Augment UI triggers tool execution.
				if hasEmittedToolUse && reason == augmentStopReasonEndTurn {
					reason = augmentStopReasonToolUseRequested
				}
				chunk := newBaseChunk("")
				chunk["stop_reason"] = reason
				writeChunkLine(w, chunk)
			}
			// Extract usage from both delta["usage"] and top-level ev["usage"]
			if usage, ok := delta["usage"].(map[string]interface{}); ok {
				usageAcc.merge(usage)
			}
			if usage, ok := ev["usage"].(map[string]interface{}); ok {
				usageAcc.merge(usage)
			}

		case "message_stop":
			if usage, ok := ev["usage"].(map[string]interface{}); ok {
				usageAcc.merge(usage)
			}
			usageEmitted = emitAggregatedTokenUsageNode(w, &usageAcc, generatedText.String(), &nextNodeID)
			// Fallback: if tool_use was emitted, final stop_reason must be TOOL_USE_REQUESTED.
			finalReason := augmentStopReasonEndTurn
			if hasEmittedToolUse {
				finalReason = augmentStopReasonToolUseRequested
			}
			chunk := newBaseChunk("")
			chunk["stop_reason"] = finalReason
			writeChunkLine(w, chunk)
		}
	}

	if !usageEmitted {
		_ = emitAggregatedTokenUsageNode(w, &usageAcc, generatedText.String(), &nextNodeID)
	}

	// Extract final token counts using buildTokenUsage to ensure consistency with emitted usage
	tokenUsage := usageAcc.buildTokenUsage(generatedText.String())
	inputTokens, outputTokens := 0, 0
	if tokenUsage != nil {
		if v, ok := tokenUsage["input_tokens"].(int); ok {
			inputTokens = v
		}
		if v, ok := tokenUsage["output_tokens"].(int); ok {
			outputTokens = v
		}
	}
	return inputTokens, outputTokens, scanner.Err()
}

// mapClaudeStopReason maps Claude stop_reason to Augment stop_reason constants.
// Based on mapAnthropicStopReasonToAugment from augment-protocol.js
func mapClaudeStopReason(sr string) int {
	r := strings.ToLower(strings.TrimSpace(sr))
	switch r {
	case "end_turn":
		return augmentStopReasonEndTurn
	case "max_tokens":
		return augmentStopReasonMaxTokens
	case "tool_use":
		return augmentStopReasonToolUseRequested
	case "stop_sequence":
		return augmentStopReasonEndTurn
	case "safety":
		return augmentStopReasonSafety
	case "recitation":
		return augmentStopReasonRecitation
	default:
		return augmentStopReasonEndTurn
	}
}

// mapOpenAIFinishReason maps OpenAI finish_reason to Augment stop_reason constants.
// Based on mapOpenAiFinishReasonToAugment from augment-protocol.js
func mapOpenAIFinishReason(fr string) int {
	r := strings.ToLower(strings.TrimSpace(fr))
	switch r {
	case "stop":
		return augmentStopReasonEndTurn
	case "length":
		return augmentStopReasonMaxTokens
	case "tool_calls":
		return augmentStopReasonToolUseRequested
	case "function_call":
		return augmentStopReasonToolUseRequested
	case "content_filter":
		return augmentStopReasonSafety
	default:
		return augmentStopReasonEndTurn
	}
}

type usageAccumulator struct {
	inputTokens              int
	outputTokens             int
	totalTokens              int
	cacheReadInputTokens     int
	cacheCreationInputTokens int

	hasInputTokens              bool
	hasOutputTokens             bool
	hasTotalTokens              bool
	hasCacheReadInputTokens     bool
	hasCacheCreationInputTokens bool
}

func (a *usageAccumulator) merge(raw map[string]interface{}) {
	if len(raw) == 0 {
		return
	}

	if v, ok := usageInt(raw, "input_tokens", "prompt_tokens"); ok {
		a.setInputTokens(v)
	}
	if v, ok := usageInt(raw, "output_tokens", "completion_tokens"); ok {
		a.setOutputTokens(v)
	}
	if v, ok := usageInt(raw, "total_tokens"); ok {
		a.setTotalTokens(v)
	}
	if v, ok := usageInt(raw, "cache_read_input_tokens"); ok {
		a.setCacheReadInputTokens(v)
	}
	if v, ok := usageInt(raw, "cache_creation_input_tokens"); ok {
		a.setCacheCreationInputTokens(v)
	}

	if details, ok := raw["prompt_tokens_details"].(map[string]interface{}); ok {
		if v, ok := usageInt(details, "cached_tokens"); ok {
			a.setCacheReadInputTokens(v)
		}
		if v, ok := usageInt(details, "cache_creation_tokens"); ok {
			a.setCacheCreationInputTokens(v)
		}
	}
}

func (a *usageAccumulator) setInputTokens(v int) {
	if v < 0 {
		return
	}
	if !a.hasInputTokens || v > a.inputTokens {
		a.inputTokens = v
		a.hasInputTokens = true
	}
}

func (a *usageAccumulator) setOutputTokens(v int) {
	if v < 0 {
		return
	}
	if !a.hasOutputTokens || v > a.outputTokens {
		a.outputTokens = v
		a.hasOutputTokens = true
	}
}

func (a *usageAccumulator) setTotalTokens(v int) {
	if v < 0 {
		return
	}
	if !a.hasTotalTokens || v > a.totalTokens {
		a.totalTokens = v
		a.hasTotalTokens = true
	}
}

func (a *usageAccumulator) setCacheReadInputTokens(v int) {
	if v < 0 {
		return
	}
	if !a.hasCacheReadInputTokens || v > a.cacheReadInputTokens {
		a.cacheReadInputTokens = v
		a.hasCacheReadInputTokens = true
	}
}

func (a *usageAccumulator) setCacheCreationInputTokens(v int) {
	if v < 0 {
		return
	}
	if !a.hasCacheCreationInputTokens || v > a.cacheCreationInputTokens {
		a.cacheCreationInputTokens = v
		a.hasCacheCreationInputTokens = true
	}
}

func (a *usageAccumulator) buildTokenUsage(outputText string) map[string]interface{} {
	if a == nil {
		return nil
	}

	inputTokens := a.inputTokens
	outputTokens := a.outputTokens
	hasInputTokens := a.hasInputTokens
	hasOutputTokens := a.hasOutputTokens

	// If only one side exists but total_tokens is present, derive the other side.
	if a.hasTotalTokens && hasOutputTokens && !hasInputTokens && a.totalTokens >= outputTokens {
		inputTokens = a.totalTokens - outputTokens
		hasInputTokens = true
	}
	if a.hasTotalTokens && hasInputTokens && !hasOutputTokens && a.totalTokens >= inputTokens {
		outputTokens = a.totalTokens - inputTokens
		hasOutputTokens = true
	}

	estimatedOutputTokens := tokencount.EstimateOutputTokens(outputText)
	if estimatedOutputTokens > 0 {
		if !hasOutputTokens || outputTokens <= 0 {
			outputTokens = estimatedOutputTokens
			hasOutputTokens = true
		} else if estimatedOutputTokens >= usageFallbackMinEstimatedOutputTokens &&
			estimatedOutputTokens >= outputTokens*usageFallbackOutputMismatchRatio {
			outputTokens = estimatedOutputTokens
		}
	}

	tokenUsage := make(map[string]interface{})
	if hasInputTokens {
		tokenUsage["input_tokens"] = inputTokens
	}
	if hasOutputTokens {
		tokenUsage["output_tokens"] = outputTokens
	}
	if a.hasCacheReadInputTokens {
		tokenUsage["cache_read_input_tokens"] = a.cacheReadInputTokens
	}
	if a.hasCacheCreationInputTokens {
		tokenUsage["cache_creation_input_tokens"] = a.cacheCreationInputTokens
	}

	if len(tokenUsage) == 0 {
		return nil
	}
	return tokenUsage
}

func usageInt(m map[string]interface{}, keys ...string) (int, bool) {
	for _, key := range keys {
		if raw, exists := m[key]; exists {
			if v, ok := toTokenInt(raw); ok {
				return v, true
			}
		}
	}
	return 0, false
}

func toTokenInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		if n < 0 {
			return 0, false
		}
		return n, true
	case int32:
		if n < 0 {
			return 0, false
		}
		return int(n), true
	case int64:
		if n < 0 {
			return 0, false
		}
		return int(n), true
	case float32:
		if n < 0 {
			return 0, false
		}
		return int(n), true
	case float64:
		if n < 0 {
			return 0, false
		}
		return int(n), true
	case json.Number:
		if iv, err := n.Int64(); err == nil && iv >= 0 {
			return int(iv), true
		}
		if fv, err := n.Float64(); err == nil && fv >= 0 {
			return int(fv), true
		}
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, false
		}
		if iv, err := strconv.Atoi(s); err == nil && iv >= 0 {
			return iv, true
		}
		if fv, err := strconv.ParseFloat(s, 64); err == nil && fv >= 0 {
			return int(fv), true
		}
	}
	return 0, false
}

func emitAggregatedTokenUsageNode(w io.Writer, usageAcc *usageAccumulator, outputText string, nextNodeID *int) bool {
	tokenUsage := usageAcc.buildTokenUsage(outputText)
	if len(tokenUsage) == 0 {
		return false
	}

	node := map[string]interface{}{
		"id":          *nextNodeID,
		"type":        augmentNodeTypeTokenUsage,
		"content":     "",
		"token_usage": tokenUsage,
	}
	*nextNodeID++

	chunk := newBaseChunk("")
	chunk["nodes"] = []interface{}{node}
	writeChunkLine(w, chunk)
	return true
}

func emitThinkingChunk(w io.Writer, text string, nextNodeID *int) {
	if text == "" {
		return
	}
	node := map[string]interface{}{
		"id":       *nextNodeID,
		"type":     augmentNodeTypeThinking,
		"content":  "",
		"thinking": map[string]interface{}{"summary": text},
	}
	*nextNodeID++
	chunk := newBaseChunk("")
	chunk["nodes"] = []interface{}{node}
	writeChunkLine(w, chunk)
}

type openAIToolCallAccum struct {
	id      string
	name    string
	args    strings.Builder
	started bool // Track if TOOL_USE_START has been emitted
}

func streamConvertOpenAISSE(r io.Reader, w io.Writer, toolCtx map[string]*ToolContext) (inputTokens, outputTokens int, err error) {
	scanner := bufio.NewScanner(r)
	nextNodeID := 1

	// index -> accumulated tool call
	acc := map[int]*openAIToolCallAccum{}
	inThinkTag := false // T6: track <think> tag state
	var usageAcc usageAccumulator
	var generatedText strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		if usage, ok := ev["usage"].(map[string]interface{}); ok {
			usageAcc.merge(usage)
		}

		choices, _ := ev["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]interface{})
		delta, _ := choice["delta"].(map[string]interface{})

		// T1: Handle reasoning_content (DeepSeek, OpenAI reasoning models)
		if reasoning, ok := delta["reasoning_content"].(string); ok && reasoning != "" {
			generatedText.WriteString(reasoning)
			thinkNode := map[string]interface{}{
				"id":       nextNodeID,
				"type":     augmentNodeTypeThinking,
				"content":  "",
				"thinking": map[string]interface{}{"summary": reasoning},
			}
			nextNodeID++
			chunk := newBaseChunk("")
			chunk["nodes"] = []interface{}{thinkNode}
			writeChunkLine(w, chunk)
			continue
		}

		// T6: Handle <think> tags in content as THINKING nodes
		if content, ok := delta["content"].(string); ok && content != "" {
			generatedText.WriteString(content)
			remaining := content
			for len(remaining) > 0 {
				if inThinkTag {
					closeIdx := strings.Index(remaining, "</think>")
					if closeIdx == -1 {
						// All remaining is thinking content
						emitThinkingChunk(w, remaining, &nextNodeID)
						remaining = ""
					} else {
						if closeIdx > 0 {
							emitThinkingChunk(w, remaining[:closeIdx], &nextNodeID)
						}
						inThinkTag = false
						remaining = remaining[closeIdx+len("</think>"):]
					}
				} else {
					openIdx := strings.Index(remaining, "<think>")
					if openIdx == -1 {
						// All remaining is normal text
						writeChunkLine(w, newBaseChunk(remaining))
						remaining = ""
					} else {
						if openIdx > 0 {
							writeChunkLine(w, newBaseChunk(remaining[:openIdx]))
						}
						inThinkTag = true
						remaining = remaining[openIdx+len("<think>"):]
					}
				}
			}
			continue
		}

		if toolCalls, ok := delta["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
			for _, raw := range toolCalls {
				tc, _ := raw.(map[string]interface{})
				idxFloat, _ := tc["index"].(float64)
				idx := int(idxFloat)

				a := acc[idx]
				if a == nil {
					a = &openAIToolCallAccum{}
					acc[idx] = a
				}
				if id, ok := tc["id"].(string); ok && id != "" {
					a.id = id
				}
				fn, _ := tc["function"].(map[string]interface{})
				if name, ok := fn["name"].(string); ok && name != "" {
					a.name = name
				}
				if args, ok := fn["arguments"].(string); ok && args != "" {
					a.args.WriteString(args)
				}

				// Emit TOOL_USE_START node (type=7) on first detection
				if !a.started && a.name != "" && a.id != "" {
					a.started = true
					toolUseStart := map[string]interface{}{
						"tool_name":   a.name,
						"tool_use_id": a.id,
						"input_json":  "",
					}
					// Add MCP fields if available
					if toolCtx != nil {
						if ctx, ok := toolCtx[a.name]; ok {
							if ctx.McpServerName != "" {
								toolUseStart["mcp_server_name"] = ctx.McpServerName
							}
							if ctx.McpToolName != "" {
								toolUseStart["mcp_tool_name"] = ctx.McpToolName
							}
						}
					}
					node := map[string]interface{}{
						"id":       nextNodeID,
						"type":     augmentNodeTypeToolUseStart,
						"content":  "",           // Fixed: content should be empty string
						"tool_use": toolUseStart, // Fixed: tool info goes in tool_use field
					}
					nextNodeID++
					chunk := newBaseChunk("")
					chunk["nodes"] = []interface{}{node}
					writeChunkLine(w, chunk)
				}
			}
		}

		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			switch fr {
			case "tool_calls":
				var nodes []interface{}
				for _, a := range acc {
					if a == nil {
						continue
					}
					toolUse := map[string]interface{}{
						"tool_name":   a.name,
						"tool_use_id": a.id,
						"input_json":  a.args.String(),
					}
					// Add MCP fields if available
					if toolCtx != nil {
						if ctx, ok := toolCtx[a.name]; ok {
							if ctx.McpServerName != "" {
								toolUse["mcp_server_name"] = ctx.McpServerName
							}
							if ctx.McpToolName != "" {
								toolUse["mcp_tool_name"] = ctx.McpToolName
							}
						}
					}
					nodes = append(nodes, map[string]interface{}{
						"id":       nextNodeID,
						"type":     augmentNodeTypeToolUse,
						"content":  "",
						"tool_use": toolUse,
					})
					nextNodeID++
				}
				chunk := newBaseChunk("")
				chunk["nodes"] = nodes
				chunk["stop_reason"] = augmentStopReasonToolUseRequested
				writeChunkLine(w, chunk)
				acc = map[int]*openAIToolCallAccum{}
			default:
				chunk := newBaseChunk("")
				chunk["stop_reason"] = mapOpenAIFinishReason(fr)
				writeChunkLine(w, chunk)
				acc = map[int]*openAIToolCallAccum{}
			}
			continue
		}
	}

	_ = emitAggregatedTokenUsageNode(w, &usageAcc, generatedText.String(), &nextNodeID)

	// Extract final token counts using buildTokenUsage to ensure consistency with emitted usage
	tokenUsage := usageAcc.buildTokenUsage(generatedText.String())
	inputTokens, outputTokens := 0, 0
	if tokenUsage != nil {
		if v, ok := tokenUsage["input_tokens"].(int); ok {
			inputTokens = v
		}
		if v, ok := tokenUsage["output_tokens"].(int); ok {
			outputTokens = v
		}
	}
	return inputTokens, outputTokens, scanner.Err()
}
