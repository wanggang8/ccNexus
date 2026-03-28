package convert

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/lich0821/ccNexus/internal/transformer"
)

// cleanSchemaForGemini removes fields not supported by Gemini API
func cleanSchemaForGemini(schema interface{}) interface{} {
	m, ok := schema.(map[string]interface{})
	if !ok {
		return schema
	}
	// Remove unsupported fields
	delete(m, "additionalProperties")
	delete(m, "$schema")
	if props, ok := m["properties"].(map[string]interface{}); ok {
		for k, v := range props {
			props[k] = cleanSchemaForGemini(v)
		}
	}
	if items, ok := m["items"]; ok {
		m["items"] = cleanSchemaForGemini(items)
	}
	return m
}

// parseSSE parses SSE event data
func parseSSE(data []byte) (eventType, jsonData string) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			jsonData = strings.TrimPrefix(line, "data: ")
		}
	}
	return
}

func newChatCompletionID() string {
	return "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// buildClaudeEvent builds a Claude SSE event
func buildClaudeEvent(eventType string, data map[string]interface{}) []byte {
	data["type"] = eventType
	jsonData, _ := json.Marshal(data)
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, jsonData))
}

// buildOpenAIChunk builds an OpenAI streaming chunk without usage.
func buildOpenAIChunk(id, model, content string, toolCalls []map[string]interface{}, finish string) ([]byte, error) {
	return buildOpenAIChunkWithUsage(id, model, content, toolCalls, finish, nil)
}

func buildOpenAIReasoningChunk(id, model, reasoning string, finish string) ([]byte, error) {
	delta := map[string]interface{}{}
	if reasoning != "" {
		delta["reasoning_content"] = reasoning
	}

	var finishReason interface{} = nil
	if finish != "" {
		finishReason = finish
	}

	chunk := map[string]interface{}{
		"id": id, "object": "chat.completion.chunk", "model": model,
		"choices": []map[string]interface{}{{"index": 0, "delta": delta, "finish_reason": finishReason}},
	}
	data, _ := json.Marshal(chunk)
	return []byte(fmt.Sprintf("data: %s\n\n", data)), nil
}

// buildOpenAIChunkWithUsage builds an OpenAI streaming chunk with optional usage.
func buildOpenAIChunkWithUsage(id, model, content string, toolCalls []map[string]interface{}, finish string, usage map[string]interface{}) ([]byte, error) {
	delta := map[string]interface{}{}
	if content != "" {
		delta["content"] = content
	}
	if len(toolCalls) > 0 {
		delta["tool_calls"] = toolCalls
	}

	var finishReason interface{} = nil
	if finish != "" {
		finishReason = finish
	}

	chunk := map[string]interface{}{
		"id": id, "object": "chat.completion.chunk", "model": model,
		"choices": []map[string]interface{}{{"index": 0, "delta": delta, "finish_reason": finishReason}},
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	data, _ := json.Marshal(chunk)
	return []byte(fmt.Sprintf("data: %s\n\n", data)), nil
}

// syncGeminiUsageMetadata stores Gemini usage metadata in stream context for later usage emission.
func syncGeminiUsageMetadata(resp *transformer.GeminiResponse, ctx *transformer.StreamContext) {
	if resp == nil || resp.UsageMetadata == nil || ctx == nil {
		return
	}
	if resp.UsageMetadata.PromptTokenCount > 0 {
		ctx.InputTokens = resp.UsageMetadata.PromptTokenCount
	}
	if outputTokens := geminiOutputTokens(resp.UsageMetadata.CandidatesTokenCount, resp.UsageMetadata.ThoughtsTokenCount); outputTokens > 0 {
		ctx.OutputTokens = outputTokens
	}
}

func geminiOutputTokens(candidates, thoughts int) int {
	return candidates + thoughts
}

func currentOpenAIUsage(ctx *transformer.StreamContext) map[string]interface{} {
	if ctx == nil || (ctx.InputTokens == 0 && ctx.OutputTokens == 0) {
		return nil
	}
	return map[string]interface{}{
		"prompt_tokens":     ctx.InputTokens,
		"completion_tokens": ctx.OutputTokens,
		"total_tokens":      ctx.InputTokens + ctx.OutputTokens,
	}
}

func currentClaudeUsage(ctx *transformer.StreamContext) map[string]interface{} {
	if ctx == nil {
		return map[string]interface{}{"input_tokens": 0, "output_tokens": 0}
	}
	return map[string]interface{}{
		"input_tokens":  ctx.InputTokens,
		"output_tokens": ctx.OutputTokens,
	}
}

func extractOpenAI2ReasoningText(item map[string]interface{}) string {
	summary, ok := item["summary"].([]interface{})
	if !ok {
		return ""
	}

	var builder strings.Builder
	for _, part := range summary {
		partMap, ok := part.(map[string]interface{})
		if !ok {
			continue
		}
		if partMap["type"] == "summary_text" {
			if text, ok := partMap["text"].(string); ok {
				builder.WriteString(text)
			}
		}
	}
	return builder.String()
}

func extractTypedOpenAI2ReasoningText(item transformer.OpenAI2OutputItem) string {
	var builder strings.Builder
	for _, part := range item.Summary {
		if part.Type == "summary_text" {
			builder.WriteString(part.Text)
		}
	}
	return builder.String()
}

func stringifyOpenAIContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

func startResponsesReasoningItem(ctx *transformer.StreamContext, writeEvent func(map[string]interface{})) {
	if ctx == nil || ctx.ThinkingBlockStarted {
		return
	}
	ctx.ThinkingBlockStarted = true
	writeEvent(map[string]interface{}{
		"type":         "response.output_item.added",
		"output_index": ctx.ThinkingIndex,
		"item": map[string]interface{}{
			"type":    "reasoning",
			"summary": []interface{}{},
		},
	})
}

func appendResponsesReasoningDelta(ctx *transformer.StreamContext, writeEvent func(map[string]interface{}), text string) {
	if ctx == nil || text == "" {
		return
	}
	startResponsesReasoningItem(ctx, writeEvent)
	ctx.PendingThinkingText += text
	writeEvent(map[string]interface{}{
		"type":          "response.reasoning_summary_text.delta",
		"output_index":  ctx.ThinkingIndex,
		"summary_index": 0,
		"delta":         text,
	})
}

func closeResponsesReasoningItem(ctx *transformer.StreamContext, writeEvent func(map[string]interface{})) {
	if ctx == nil || !ctx.ThinkingBlockStarted {
		return
	}
	text := ctx.PendingThinkingText
	writeEvent(map[string]interface{}{
		"type":          "response.reasoning_summary_text.done",
		"output_index":  ctx.ThinkingIndex,
		"summary_index": 0,
		"text":          text,
	})
	writeEvent(map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": ctx.ThinkingIndex,
		"item": map[string]interface{}{
			"type":    "reasoning",
			"summary": []map[string]interface{}{{"type": "summary_text", "text": text}},
		},
	})
	ctx.ThinkingBlockStarted = false
	if ctx.ContentIndex == 0 {
		ctx.ContentIndex = 1
	}
}

func responseStatusFromFinishReason(finishReason string) string {
	if finishReason == "length" {
		return "incomplete"
	}
	return "completed"
}

func buildResponsesReasoningOutputItem(text string) map[string]interface{} {
	return map[string]interface{}{
		"type": "reasoning",
		"id":   "rs_" + uuid.NewString(),
		"summary": []map[string]interface{}{
			{"type": "summary_text", "text": text},
		},
	}
}

func buildResponsesMessageOutputItem(text string) map[string]interface{} {
	return map[string]interface{}{
		"type":   "message",
		"id":     "msg_" + uuid.NewString(),
		"status": "completed",
		"role":   "assistant",
		"content": []map[string]interface{}{
			{"type": "output_text", "text": text},
		},
	}
}

func buildResponsesFunctionCallOutputItem(callID, name, arguments string) map[string]interface{} {
	if callID == "" {
		callID = "call_" + uuid.NewString()
	}
	if arguments == "" {
		arguments = "{}"
	}
	return map[string]interface{}{
		"type":      "function_call",
		"id":        "fc_" + uuid.NewString(),
		"status":    "completed",
		"call_id":   callID,
		"name":      name,
		"arguments": arguments,
	}
}

// extractSystemText extracts text from Claude system prompt
func extractSystemText(system interface{}) string {
	switch s := system.(type) {
	case string:
		return s
	case []interface{}:
		var parts []string
		for _, block := range s {
			if m, ok := block.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
