package augment

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
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
	if err := StreamConvertSSEToNDJSON(strings.NewReader(string(sseData)), &out, targetType, toolCtx); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// StreamConvertSSEToNDJSON converts an SSE stream to NDJSON in a streaming fashion.
// It reads from r and writes converted NDJSON lines to w.
func StreamConvertSSEToNDJSON(r io.Reader, w io.Writer, targetType string, toolCtx map[string]*ToolContext) error {
	switch targetType {
	case "claude", "cli":
		return streamConvertClaudeSSE(r, w, toolCtx)
	case "openai", "openai2":
		return streamConvertOpenAISSE(r, w, toolCtx)
	default:
		return fmt.Errorf("augment response: unsupported target type %q", targetType)
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

func streamConvertClaudeSSE(r io.Reader, w io.Writer, toolCtx map[string]*ToolContext) error {
	scanner := bufio.NewScanner(r)
	var lastEventType string
	var buf toolUseBuffer
	nextNodeID := 1
	hasEmittedToolUse := false // Track if any tool_use was emitted for stop_reason fallback

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
				emitTokenUsageNode(w, usage, &nextNodeID)
			}
			if usage, ok := ev["usage"].(map[string]interface{}); ok {
				emitTokenUsageNode(w, usage, &nextNodeID)
			}

		case "message_stop":
			if usage, ok := ev["usage"].(map[string]interface{}); ok {
				emitTokenUsageNode(w, usage, &nextNodeID)
			}
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
	return scanner.Err()
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

// emitTokenUsageNode creates and writes a TOKEN_USAGE node (type=10) based on usage data.
// Based on tokenUsageNode from augment-protocol.js
func emitTokenUsageNode(w io.Writer, usage map[string]interface{}, nextNodeID *int) {
	tokenUsage := make(map[string]interface{})

	// Extract token counts (supports both Claude and OpenAI formats)
	if val, ok := usage["input_tokens"].(float64); ok {
		tokenUsage["input_tokens"] = int(val)
	} else if val, ok := usage["prompt_tokens"].(float64); ok {
		tokenUsage["input_tokens"] = int(val)
	}

	if val, ok := usage["output_tokens"].(float64); ok {
		tokenUsage["output_tokens"] = int(val)
	} else if val, ok := usage["completion_tokens"].(float64); ok {
		tokenUsage["output_tokens"] = int(val)
	}

	// Claude-specific: prompt caching tokens
	if val, ok := usage["cache_read_input_tokens"].(float64); ok {
		tokenUsage["cache_read_input_tokens"] = int(val)
	}
	if val, ok := usage["cache_creation_input_tokens"].(float64); ok {
		tokenUsage["cache_creation_input_tokens"] = int(val)
	}

	// OpenAI-specific: prompt_tokens_details
	if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
		if val, ok := details["cached_tokens"].(float64); ok {
			tokenUsage["cache_read_input_tokens"] = int(val)
		}
		if val, ok := details["cache_creation_tokens"].(float64); ok {
			tokenUsage["cache_creation_input_tokens"] = int(val)
		}
	}

	// Only emit if we have at least one token count
	if len(tokenUsage) == 0 {
		return
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

func streamConvertOpenAISSE(r io.Reader, w io.Writer, toolCtx map[string]*ToolContext) error {
	scanner := bufio.NewScanner(r)
	nextNodeID := 1

	// index -> accumulated tool call
	acc := map[int]*openAIToolCallAccum{}
	inThinkTag := false // T6: track <think> tag state

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
		choices, _ := ev["choices"].([]interface{})
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]interface{})
		delta, _ := choice["delta"].(map[string]interface{})

		// T1: Handle reasoning_content (DeepSeek, OpenAI reasoning models)
		if reasoning, ok := delta["reasoning_content"].(string); ok && reasoning != "" {
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
			// Extract usage information before processing finish_reason
			// to ensure we don't miss it after clearing accumulator
			if usage, ok := ev["usage"].(map[string]interface{}); ok {
				emitTokenUsageNode(w, usage, &nextNodeID)
			}

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
			continue // Skip the usage check below since we already handled it
		}

		// Extract usage information if present (OpenAI format)
		// This handles cases where usage arrives without finish_reason
		if usage, ok := ev["usage"].(map[string]interface{}); ok {
			emitTokenUsageNode(w, usage, &nextNodeID)
		}
	}
	return scanner.Err()
}
