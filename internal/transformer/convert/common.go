package convert

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// toolCallCounter is used to generate unique tool call IDs
var toolCallCounter int64

// GenerateToolCallID generates a unique tool call ID for consistent tool call tracking
func GenerateToolCallID(name string) string {
	counter := atomic.AddInt64(&toolCallCounter, 1)
	return fmt.Sprintf("toolu_%s_%d", name, counter)
}

// cleanSchemaForGemini returns a copy of the schema with fields not supported by Gemini API removed.
// The original map is NOT modified.
func cleanSchemaForGemini(schema interface{}) interface{} {
	m, ok := schema.(map[string]interface{})
	if !ok {
		return schema
	}
	cleaned := make(map[string]interface{}, len(m))
	for k, v := range m {
		if k == "additionalProperties" || k == "$schema" {
			continue
		}
		cleaned[k] = v
	}
	if props, ok := cleaned["properties"].(map[string]interface{}); ok {
		newProps := make(map[string]interface{}, len(props))
		for k, v := range props {
			newProps[k] = cleanSchemaForGemini(v)
		}
		cleaned["properties"] = newProps
	}
	if items, ok := cleaned["items"]; ok {
		cleaned["items"] = cleanSchemaForGemini(items)
	}
	return cleaned
}

// ParseSSE parses SSE event data, returning the event type and JSON data payload.
func ParseSSE(data []byte) (eventType, jsonData string) {
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
		"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
		"choices": []map[string]interface{}{{"index": 0, "delta": delta, "finish_reason": finishReason}},
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	data, _ := json.Marshal(chunk)
	return []byte(fmt.Sprintf("data: %s\n\n", data)), nil
}

// buildOpenAIChunkWithReasoning builds an OpenAI streaming chunk with reasoning_content field
func buildOpenAIChunkWithReasoning(id, model, reasoningContent string) ([]byte, error) {
	delta := map[string]interface{}{
		"reasoning_content": reasoningContent,
	}
	chunk := map[string]interface{}{
		"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
		"choices": []map[string]interface{}{{"index": 0, "delta": delta, "finish_reason": nil}},
	}
	data, _ := json.Marshal(chunk)
	return []byte(fmt.Sprintf("data: %s\n\n", data)), nil
}

// buildOpenAIUsageChunk builds an OpenAI streaming usage chunk
func buildOpenAIUsageChunk(id, model string, promptTokens, completionTokens int) ([]byte, error) {
	chunk := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{},
		"usage": map[string]interface{}{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
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
	if resp.UsageMetadata.CandidatesTokenCount > 0 {
		ctx.OutputTokens = resp.UsageMetadata.CandidatesTokenCount
	}
	if resp.UsageMetadata.TotalTokenCount > 0 {
		ctx.TotalTokens = resp.UsageMetadata.TotalTokenCount
		ctx.HasAuthoritativeTotalTokens = true
	}
}

func currentOpenAIUsage(ctx *transformer.StreamContext) map[string]interface{} {
	if ctx == nil || (ctx.InputTokens == 0 && ctx.OutputTokens == 0 && ctx.TotalTokens == 0) {
		return nil
	}
	total := ctx.InputTokens + ctx.OutputTokens
	if ctx.HasAuthoritativeTotalTokens {
		total = ctx.TotalTokens
	}
	return map[string]interface{}{
		"prompt_tokens":     ctx.InputTokens,
		"completion_tokens": ctx.OutputTokens,
		"total_tokens":      total,
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

func syncOpenAIUsage(promptTokens, completionTokens, totalTokens int, ctx *transformer.StreamContext) {
	if ctx == nil {
		return
	}
	if promptTokens > 0 {
		ctx.InputTokens = promptTokens
	}
	if completionTokens > 0 {
		ctx.OutputTokens = completionTokens
	}
	if totalTokens > 0 {
		ctx.TotalTokens = totalTokens
		ctx.HasAuthoritativeTotalTokens = true
	}
}

func currentResponsesUsage(ctx *transformer.StreamContext) map[string]interface{} {
	if ctx == nil {
		return map[string]interface{}{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}
	}
	total := ctx.InputTokens + ctx.OutputTokens
	if ctx.HasAuthoritativeTotalTokens {
		total = ctx.TotalTokens
	}
	return map[string]interface{}{
		"input_tokens":  ctx.InputTokens,
		"output_tokens": ctx.OutputTokens,
		"total_tokens":  total,
	}
}

func ensureResponseOutputItemMaps(ctx *transformer.StreamContext) {
	if ctx == nil {
		return
	}
	if ctx.ResponseOutputItems == nil {
		ctx.ResponseOutputItems = make(map[int]*transformer.ResponseOutputItemState)
	}
	if ctx.ResponseOutputItemLookup == nil {
		ctx.ResponseOutputItemLookup = make(map[string]*transformer.ResponseOutputItemState)
	}
}

func responseItemLookupKeys(item *transformer.ResponseOutputItemState) []string {
	if item == nil {
		return nil
	}
	var keys []string
	if item.ItemID != "" {
		keys = append(keys, "item:"+item.ItemID)
	}
	if item.CallID != "" {
		keys = append(keys, "call:"+item.CallID)
	}
	if item.OutputIndex >= 0 {
		keys = append(keys, fmt.Sprintf("idx:%d", item.OutputIndex))
	}
	return keys
}

func registerResponseOutputItem(ctx *transformer.StreamContext, item *transformer.ResponseOutputItemState) *transformer.ResponseOutputItemState {
	if ctx == nil || item == nil {
		return item
	}
	ensureResponseOutputItemMaps(ctx)
	ctx.ResponseOutputItems[item.OutputIndex] = item
	for _, key := range responseItemLookupKeys(item) {
		ctx.ResponseOutputItemLookup[key] = item
	}
	if item.OutputIndex >= ctx.NextResponseOutputIndex {
		ctx.NextResponseOutputIndex = item.OutputIndex + 1
	}
	return item
}

func updateResponseOutputItemLookup(ctx *transformer.StreamContext, item *transformer.ResponseOutputItemState) {
	if ctx == nil || item == nil {
		return
	}
	ensureResponseOutputItemMaps(ctx)
	ctx.ResponseOutputItems[item.OutputIndex] = item
	for _, key := range responseItemLookupKeys(item) {
		ctx.ResponseOutputItemLookup[key] = item
	}
}

func bindResponseOutputItemAlias(ctx *transformer.StreamContext, alias string, item *transformer.ResponseOutputItemState) {
	if ctx == nil || item == nil || alias == "" {
		return
	}
	ensureResponseOutputItemMaps(ctx)
	ctx.ResponseOutputItemLookup[alias] = item
}

func resolveResponseOutputItem(ctx *transformer.StreamContext, outputIndex *int, itemID, callID string) *transformer.ResponseOutputItemState {
	if ctx == nil {
		return nil
	}
	ensureResponseOutputItemMaps(ctx)
	if itemID != "" {
		if item, ok := ctx.ResponseOutputItemLookup["item:"+itemID]; ok {
			return item
		}
	}
	if callID != "" {
		if item, ok := ctx.ResponseOutputItemLookup["call:"+callID]; ok {
			return item
		}
	}
	if outputIndex != nil {
		if item, ok := ctx.ResponseOutputItems[*outputIndex]; ok {
			return item
		}
		if item, ok := ctx.ResponseOutputItemLookup[fmt.Sprintf("idx:%d", *outputIndex)]; ok {
			return item
		}
	}
	return nil
}

func nextResponseOutputIndex(ctx *transformer.StreamContext) int {
	ensureResponseOutputItemMaps(ctx)
	idx := ctx.NextResponseOutputIndex
	ctx.NextResponseOutputIndex++
	return idx
}

func orderedResponseOutputItems(ctx *transformer.StreamContext) []*transformer.ResponseOutputItemState {
	if ctx == nil {
		return nil
	}
	ensureResponseOutputItemMaps(ctx)
	items := make([]*transformer.ResponseOutputItemState, 0, len(ctx.ResponseOutputItems))
	for idx := 0; idx < ctx.NextResponseOutputIndex; idx++ {
		if item, ok := ctx.ResponseOutputItems[idx]; ok {
			items = append(items, item)
		}
	}
	return items
}

func effectiveResponseItemArguments(item *transformer.ResponseOutputItemState, fallback string) string {
	if item != nil {
		if item.DoneArguments != "" {
			return item.DoneArguments
		}
		if item.Arguments != "" {
			return item.Arguments
		}
	}
	return fallback
}

func resolveOrCreateResponseFunctionItem(ctx *transformer.StreamContext, outputIndex *int, itemID, callID, name string) *transformer.ResponseOutputItemState {
	item := resolveResponseOutputItem(ctx, outputIndex, itemID, callID)
	if item != nil {
		if itemID != "" && item.ItemID == "" {
			item.ItemID = itemID
		}
		if callID != "" && item.CallID == "" {
			item.CallID = callID
		}
		if name != "" && item.Name == "" {
			item.Name = name
		}
		if item.Type == "" {
			item.Type = "function_call"
		}
		updateResponseOutputItemLookup(ctx, item)
		return item
	}
	idx := nextResponseOutputIndex(ctx)
	if outputIndex != nil {
		idx = *outputIndex
		if idx >= ctx.NextResponseOutputIndex {
			ctx.NextResponseOutputIndex = idx + 1
		}
	}
	item = &transformer.ResponseOutputItemState{
		Type:        "function_call",
		OutputIndex: idx,
		ItemID:      itemID,
		CallID:      callID,
		Name:        name,
		Status:      "in_progress",
	}
	return registerResponseOutputItem(ctx, item)
}

func currentResponseFunctionItem(ctx *transformer.StreamContext) *transformer.ResponseOutputItemState {
	if ctx == nil {
		return nil
	}
	if item := resolveResponseOutputItem(ctx, nil, "", ctx.CurrentToolID); item != nil {
		return item
	}
	for _, item := range orderedResponseOutputItems(ctx) {
		if item != nil && item.Type == "function_call" && !item.Completed {
			return item
		}
	}
	return nil
}

func flattenOpenAI2ResponseOutput(parts []map[string]interface{}) []map[string]interface{} {
	output := make([]map[string]interface{}, 0, len(parts))
	for _, part := range parts {
		if part == nil {
			continue
		}
		output = append(output, part)
	}
	return output
}

func appendOpenAI2MessageItem(output []map[string]interface{}, role string, parts []map[string]interface{}) []map[string]interface{} {
	if len(parts) == 0 {
		return output
	}
	return append(output, map[string]interface{}{
		"type":    "message",
		"role":    role,
		"status":  "completed",
		"content": parts,
	})
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
