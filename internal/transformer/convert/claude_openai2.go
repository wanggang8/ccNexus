package convert

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// ClaudeReqToOpenAI2 converts Claude request to OpenAI Responses API request
func ClaudeReqToOpenAI2(claudeReq []byte, model string) ([]byte, error) {
	var req transformer.ClaudeRequest
	if err := json.Unmarshal(claudeReq, &req); err != nil {
		return nil, err
	}

	openai2Req := map[string]interface{}{
		"model":  model,
		"stream": req.Stream,
	}

	// Convert system to instructions
	if req.System != nil {
		openai2Req["instructions"] = extractSystemText(req.System)
	}

	// Convert messages to input
	// tool_use blocks → top-level function_call items
	// tool_result blocks → top-level function_call_output items
	var input []map[string]interface{}
	for _, msg := range req.Messages {
		switch content := msg.Content.(type) {
		case string:
			textType := "input_text"
			if msg.Role == "assistant" {
				textType = "output_text"
			}
			input = append(input, map[string]interface{}{
				"type": "message",
				"role": msg.Role,
				"content": []map[string]interface{}{
					{
						"type": textType,
						"text": content,
					},
				},
			})
		case []interface{}:
			input = append(input, convertClaudeMessageToOpenAI2Items(content, msg.Role)...)
		}
	}
	openai2Req["input"] = input

	// TODO: max_output_tokens is standard OpenAI Responses API param but some
	// third-party endpoints (e.g. SiliconFlow) don't support it. Skipping for compatibility.

	// Convert tools
	if len(req.Tools) > 0 {
		var tools []map[string]interface{}
		for _, tool := range req.Tools {
			tools = append(tools, map[string]interface{}{
				"type":        "function",
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			})
		}
		openai2Req["tools"] = tools

		// Preserve tool forcing semantics for Responses API backends.
		if mapped := mapClaudeToolChoiceToOpenAI2(req.ToolChoice); mapped != nil {
			openai2Req["tool_choice"] = mapped
		} else {
			// For first turn, prefer required to avoid "plan-only" responses.
			// After at least one tool_result exists, switch to auto to prevent
			// forced repeated tool calls in later turns.
			if hasClaudeToolResult(req.Messages) {
				openai2Req["tool_choice"] = "auto"
			} else {
				openai2Req["tool_choice"] = "required"
			}
		}
	}

	return json.Marshal(openai2Req)
}

func mapClaudeToolChoiceToOpenAI2(toolChoice interface{}) interface{} {
	if toolChoice == nil {
		return nil
	}

	switch tc := toolChoice.(type) {
	case map[string]interface{}:
		choiceType, _ := tc["type"].(string)
		switch choiceType {
		case "tool":
			if name, ok := tc["name"].(string); ok && name != "" {
				return map[string]interface{}{
					"type": "function",
					"name": name,
				}
			}
		case "any":
			return "required"
		case "auto":
			return "auto"
		case "none":
			return "none"
		}
	case string:
		switch tc {
		case "any":
			return "required"
		default:
			return tc
		}
	}

	return nil
}

func hasClaudeToolResult(messages []transformer.ClaudeMessage) bool {
	for _, msg := range messages {
		blocks, ok := msg.Content.([]interface{})
		if !ok {
			continue
		}
		for _, block := range blocks {
			m, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); t == "tool_result" {
				return true
			}
		}
	}
	return false
}

// OpenAI2ReqToClaude converts OpenAI Responses API request to Claude request
func OpenAI2ReqToClaude(openai2Req []byte, model string) ([]byte, error) {
	var req transformer.OpenAI2Request
	if err := json.Unmarshal(openai2Req, &req); err != nil {
		return nil, err
	}

	claudeReq := map[string]interface{}{
		"model":      model,
		"max_tokens": DefaultMaxTokens,
		"stream":     req.Stream,
	}

	if req.Instructions != "" {
		claudeReq["system"] = req.Instructions
	}
	if req.MaxOutputTokens > 0 {
		claudeReq["max_tokens"] = req.MaxOutputTokens
	}
	if req.Temperature != nil {
		claudeReq["temperature"] = *req.Temperature
	}
	if thinking := buildClaudeThinkingConfig(req.Thinking, req.EnableThinking, req.MaxOutputTokens); thinking != nil {
		claudeReq["thinking"] = thinking
	}

	// Convert input to messages
	messages := convertOpenAI2InputToClaude(req.Input)
	claudeReq["messages"] = messages

	// Convert tools
	if len(req.Tools) > 0 {
		var tools []map[string]interface{}
		for _, tool := range req.Tools {
			var inputSchema map[string]interface{}
			switch tool.Type {
			case "function":
				inputSchema = tool.Parameters
			case "custom":
				inputSchema = map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"input": map[string]interface{}{"type": "string", "description": "The input for this tool"},
					},
					"required": []string{"input"},
				}
			default:
				continue
			}
			tools = append(tools, map[string]interface{}{
				"name":         tool.Name,
				"description":  tool.Description,
				"input_schema": inputSchema,
			})
		}
		if len(tools) > 0 {
			claudeReq["tools"] = tools
		}
	}

	return json.Marshal(claudeReq)
}

// ClaudeRespToOpenAI2 converts Claude response to OpenAI Responses API response
func ClaudeRespToOpenAI2(claudeResp []byte) ([]byte, error) {
	var resp transformer.ClaudeResponse
	if err := json.Unmarshal(claudeResp, &resp); err != nil {
		return nil, err
	}

	var output []map[string]interface{}
	var messageParts []map[string]interface{}
	flushMessage := func() {
		output = appendOpenAI2MessageItem(output, "assistant", messageParts)
		messageParts = nil
	}

	for _, block := range resp.Content {
		blockMap, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		switch blockMap["type"] {
		case "text":
			messageParts = append(messageParts, map[string]interface{}{
				"type": "output_text",
				"text": blockMap["text"],
			})
		case "thinking":
			continue
		case "tool_use":
			flushMessage()
			args, _ := json.Marshal(blockMap["input"])
			output = append(output, map[string]interface{}{
				"type":      "function_call",
				"id":        blockMap["id"],
				"call_id":   blockMap["id"],
				"name":      blockMap["name"],
				"arguments": string(args),
				"status":    "completed",
			})
		}
	}
	flushMessage()

	openai2Resp := map[string]interface{}{
		"id":     resp.ID,
		"object": "response",
		"status": "completed",
		"output": output,
		"usage": map[string]interface{}{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
			"total_tokens":  resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}

	return json.Marshal(openai2Resp)
}

// OpenAI2RespToClaude converts OpenAI Responses API response to Claude response
func OpenAI2RespToClaude(openai2Resp []byte) ([]byte, error) {
	var resp transformer.OpenAI2Response
	if err := json.Unmarshal(openai2Resp, &resp); err != nil {
		return nil, err
	}

	var content []map[string]interface{}
	stopReason := "end_turn"

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					content = append(content, splitThinkTaggedText(part.Text)...)
				}
			}
		case "function_call":
			args := parseJSONObjectArguments(item.Arguments, "Failed to unmarshal function_call arguments")
			toolID := item.CallID
			if toolID == "" {
				toolID = item.ID
			}
			content = append(content, map[string]interface{}{
				"type":  "tool_use",
				"id":    toolID,
				"name":  item.Name,
				"input": args,
			})
			stopReason = "tool_use"
		}
	}

	claudeResp := map[string]interface{}{
		"id":          resp.ID,
		"type":        "message",
		"role":        "assistant",
		"content":     content,
		"stop_reason": stopReason,
		"usage": map[string]interface{}{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
		},
	}

	return json.Marshal(claudeResp)
}

// ClaudeStreamToOpenAI2 converts Claude SSE event to OpenAI Responses stream event
func ClaudeStreamToOpenAI2(event []byte, ctx *transformer.StreamContext) ([]byte, error) {
	eventType, jsonData := ParseSSE(event)
	if jsonData == "" {
		return nil, nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, nil
	}

	// Fallback: some APIs return type in JSON payload without event: line
	if eventType == "" {
		if t, ok := data["type"].(string); ok {
			eventType = t
		}
	}

	// Check for error response
	if errType, ok := data["type"].(string); ok && errType == "error" {
		if errData, ok := data["error"].(map[string]interface{}); ok {
			if msg, ok := errData["message"].(string); ok {
				return nil, fmt.Errorf("upstream error: %s", msg)
			}
		}
	}

	var result strings.Builder
	writeEvent := func(evt map[string]interface{}) {
		d, _ := json.Marshal(evt)
		result.WriteString(fmt.Sprintf("data: %s\n\n", d))
	}

	switch eventType {
	case "message_start":
		if msg, ok := data["message"].(map[string]interface{}); ok {
			ctx.MessageID, _ = msg["id"].(string)
			if usage, ok := msg["usage"].(map[string]interface{}); ok {
				if in, ok := usage["input_tokens"].(float64); ok {
					ctx.InputTokens = int(in)
				}
			}
		}
		writeEvent(map[string]interface{}{
			"type": "response.created",
			"response": map[string]interface{}{
				"id": ctx.MessageID, "object": "response", "status": "in_progress",
			},
		})

	case "content_block_start":
		block, ok := data["content_block"].(map[string]interface{})
		if !ok {
			return nil, nil
		}
		idx, _ := data["index"].(float64)
		blockIdx := int(idx)

		switch block["type"] {
		case "text":
			itemID := fmt.Sprintf("msg_%s_%d", ctx.MessageID, blockIdx)
			messageItem := &transformer.ResponseOutputItemState{
				Type:        "message",
				OutputIndex: nextResponseOutputIndex(ctx),
				ItemID:      itemID,
				Role:        "assistant",
				Status:      "in_progress",
			}
			registerResponseOutputItem(ctx, messageItem)
			bindResponseOutputItemAlias(ctx, fmt.Sprintf("claude-block:%d", blockIdx), messageItem)
			ctx.ContentBlockStarted = true
			ctx.ContentIndex = messageItem.OutputIndex
			writeEvent(map[string]interface{}{
				"type": "response.output_item.added", "output_index": messageItem.OutputIndex,
				"item": map[string]interface{}{
					"type": "message", "id": itemID,
					"role": "assistant", "status": "in_progress", "content": []interface{}{},
				},
			})
			writeEvent(map[string]interface{}{
				"type": "response.content_part.added", "output_index": messageItem.OutputIndex, "content_index": 0,
				"part": map[string]interface{}{"type": "output_text", "text": ""},
			})
		case "tool_use":
			callID, _ := block["id"].(string)
			name, _ := block["name"].(string)
			toolItem := resolveOrCreateResponseFunctionItem(ctx, nil, callID, callID, name)
			toolItem.Status = "in_progress"
			bindResponseOutputItemAlias(ctx, fmt.Sprintf("claude-block:%d", blockIdx), toolItem)
			ctx.ToolBlockStarted = true
			ctx.ToolIndex = toolItem.OutputIndex
			ctx.CurrentToolID = toolItem.CallID
			ctx.CurrentToolName = toolItem.Name
			ctx.ToolArguments = toolItem.Arguments
			writeEvent(map[string]interface{}{
				"type": "response.output_item.added", "output_index": toolItem.OutputIndex,
				"item": map[string]interface{}{
					"type": "function_call", "id": toolItem.ItemID,
					"call_id": toolItem.CallID, "name": toolItem.Name,
					"arguments": "", "status": "in_progress",
				},
			})
			updateResponseOutputItemLookup(ctx, toolItem)
		}

	case "content_block_delta":
		delta, ok := data["delta"].(map[string]interface{})
		if !ok {
			return nil, nil
		}
		switch delta["type"] {
		case "text_delta":
			text, _ := delta["text"].(string)
			ctx.ContentText += text
			writeEvent(map[string]interface{}{
				"type": "response.output_text.delta", "output_index": ctx.ContentIndex,
				"content_index": 0, "delta": text,
			})
		case "input_json_delta":
			partial, _ := delta["partial_json"].(string)
			ctx.ToolArguments += partial
			if item := resolveResponseOutputItem(ctx, nil, "", ctx.CurrentToolID); item != nil {
				item.Arguments += partial
				ctx.ToolIndex = item.OutputIndex
				updateResponseOutputItemLookup(ctx, item)
			}
			writeEvent(map[string]interface{}{
				"type":         "response.function_call_arguments.delta",
				"output_index": ctx.ToolIndex, "delta": partial,
			})
		}

	case "content_block_stop":
		idx, _ := data["index"].(float64)
		blockIdx := int(idx)
		alias := fmt.Sprintf("claude-block:%d", blockIdx)
		item := resolveResponseOutputItem(ctx, nil, "", "")
		if ctx.ResponseOutputItemLookup != nil {
			item = ctx.ResponseOutputItemLookup[alias]
		}

		if ctx.ToolBlockStarted && item != nil && item.Type == "function_call" {
			finalArgs := effectiveResponseItemArguments(item, ctx.ToolArguments)
			item.DoneArguments = finalArgs
			item.Arguments = finalArgs
			item.Status = "completed"
			item.Completed = true
			updateResponseOutputItemLookup(ctx, item)
			writeEvent(map[string]interface{}{
				"type":         "response.function_call_arguments.done",
				"output_index": item.OutputIndex, "arguments": finalArgs,
			})
			writeEvent(map[string]interface{}{
				"type": "response.output_item.done", "output_index": item.OutputIndex,
				"item": map[string]interface{}{
					"type": "function_call", "id": item.ItemID,
					"call_id": item.CallID, "name": item.Name,
					"arguments": finalArgs, "status": "completed",
				},
			})
			ctx.ToolBlockStarted = false
			ctx.ToolArguments = ""
		} else if ctx.ContentBlockStarted && item != nil && item.Type == "message" {
			accumulatedText := ctx.ContentText
			ctx.ContentText = ""
			item.Status = "completed"
			item.Completed = true
			updateResponseOutputItemLookup(ctx, item)
			writeEvent(map[string]interface{}{
				"type": "response.output_text.done", "output_index": item.OutputIndex, "content_index": 0,
				"text": accumulatedText,
			})
			writeEvent(map[string]interface{}{
				"type": "response.content_part.done", "output_index": item.OutputIndex, "content_index": 0,
				"part": map[string]interface{}{"type": "output_text", "text": accumulatedText},
			})
			writeEvent(map[string]interface{}{
				"type": "response.output_item.done", "output_index": item.OutputIndex,
				"item": map[string]interface{}{
					"type": "message", "id": item.ItemID,
					"role": "assistant", "status": "completed",
				},
			})
			ctx.ContentBlockStarted = false
		}

	case "message_delta":
		if usage, ok := data["usage"].(map[string]interface{}); ok {
			if out, ok := usage["output_tokens"].(float64); ok {
				ctx.OutputTokens = int(out)
			}
		}

	case "message_stop":
		writeEvent(map[string]interface{}{
			"type": "response.completed",
			"response": map[string]interface{}{
				"id": ctx.MessageID, "object": "response", "status": "completed",
				"usage": currentResponsesUsage(ctx),
			},
		})
		result.WriteString("data: [DONE]\n\n")
	}

	if result.Len() > 0 {
		return []byte(result.String()), nil
	}
	return nil, nil
}

// OpenAI2StreamToClaude converts OpenAI Responses stream event to Claude SSE event
func OpenAI2StreamToClaude(event []byte, ctx *transformer.StreamContext) ([]byte, error) {
	_, jsonData := ParseSSE(event)
	if jsonData == "" || jsonData == "[DONE]" {
		if jsonData == "[DONE]" {
			var result []byte
			emitText, emitThinking := makeThinkEmitters(ctx, &result)
			flushThinkTaggedStream(ctx, emitText, emitThinking)
			if ctx.ThinkingBlockStarted {
				result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ThinkingIndex})...)
				ctx.ThinkingBlockStarted = false
			}
			if ctx.ContentBlockStarted {
				result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ContentIndex})...)
				ctx.ContentBlockStarted = false
			}
			if ctx.ToolBlockStarted {
				result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ToolIndex})...)
				ctx.ToolBlockStarted = false
			}
			if !ctx.FinishReasonSent {
				stopReason := "end_turn"
				for _, item := range orderedResponseOutputItems(ctx) {
					if item != nil && item.Type == "function_call" {
						stopReason = "tool_use"
						break
					}
				}
				result = append(result, buildClaudeEvent("message_delta", map[string]interface{}{
					"delta": map[string]interface{}{"stop_reason": stopReason, "stop_sequence": nil},
					"usage": map[string]interface{}{"output_tokens": ctx.OutputTokens},
				})...)
				result = append(result, buildClaudeEvent("message_stop", map[string]interface{}{})...)
				ctx.FinishReasonSent = true
			}
			return result, nil
		}
		return nil, nil
	}

	var evt transformer.OpenAI2StreamEvent
	if err := json.Unmarshal([]byte(jsonData), &evt); err != nil {
		return nil, nil
	}

	var result []byte

	switch evt.Type {
	case "response.created":
		if evt.Response != nil {
			ctx.MessageID = evt.Response.ID
			syncOpenAIUsage(evt.Response.Usage.InputTokens, evt.Response.Usage.OutputTokens, evt.Response.Usage.TotalTokens, ctx)
		}
		result = append(result, buildClaudeEvent("message_start", map[string]interface{}{
			"message": map[string]interface{}{
				"id": ctx.MessageID, "type": "message", "role": "assistant", "content": []interface{}{},
				"model": ctx.ModelName, "stop_reason": nil, "stop_sequence": nil,
				"usage": map[string]interface{}{"input_tokens": ctx.InputTokens, "output_tokens": ctx.OutputTokens},
			},
		})...)

	case "response.output_text.delta":
		content := ctx.ThinkingBuffer + evt.Delta
		ctx.ThinkingBuffer = ""

		emitText, emitThinking := makeThinkEmitters(ctx, &result)
		emitTextWithClose := func(text string) {
			if text == "" {
				return
			}
			if ctx.ThinkingBlockStarted && !ctx.ContentBlockStarted && !ctx.InThinkingTag {
				result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ThinkingIndex})...)
				ctx.ThinkingBlockStarted = false
			}
			emitText(text)
		}
		emitThinkingWithClose := func(text string) {
			if text == "" {
				return
			}
			emitThinking(text)
			if ctx.ThinkingBlockStarted {
				result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ThinkingIndex})...)
				ctx.ThinkingBlockStarted = false
			}
		}

		consumeThinkTaggedStream(content, ctx, emitTextWithClose, emitThinkingWithClose)

	case "response.output_item.added":
		if evt.Item != nil && evt.Item.Type == "function_call" {
			if ctx.ThinkingBlockStarted {
				result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ThinkingIndex})...)
				ctx.ThinkingBlockStarted = false
			}
			if ctx.ContentBlockStarted {
				result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ContentIndex})...)
				ctx.ContentBlockStarted = false
				ctx.ContentIndex++
			}
			item := resolveOrCreateResponseFunctionItem(ctx, evt.OutputIndex, evt.ItemID, evt.Item.CallID, evt.Item.Name)
			if evt.Item.ID != "" && item.ItemID == "" {
				item.ItemID = evt.Item.ID
			}
			if evt.Item.Arguments != "" {
				item.Arguments = evt.Item.Arguments
			}
			ctx.ToolBlockStarted = true
			ctx.ToolIndex = ctx.ContentIndex
			ctx.CurrentToolID = item.CallID
			if ctx.CurrentToolID == "" {
				ctx.CurrentToolID = item.ItemID
			}
			ctx.CurrentToolName = item.Name
			ctx.ToolArguments = item.Arguments
			result = append(result, buildClaudeEvent("content_block_start", map[string]interface{}{
				"index": ctx.ToolIndex, "content_block": map[string]interface{}{
					"type": "tool_use", "id": ctx.CurrentToolID, "name": ctx.CurrentToolName, "input": map[string]interface{}{},
				},
			})...)
			updateResponseOutputItemLookup(ctx, item)
		}

	case "response.function_call_arguments.delta":
		item := resolveResponseOutputItem(ctx, evt.OutputIndex, evt.ItemID, "")
		if item == nil {
			item = currentResponseFunctionItem(ctx)
		}
		if item != nil && ctx.ToolBlockStarted {
			item.Arguments += evt.Delta
			ctx.CurrentToolID = item.CallID
			if ctx.CurrentToolID == "" {
				ctx.CurrentToolID = item.ItemID
			}
			ctx.CurrentToolName = item.Name
			ctx.ToolArguments = item.Arguments
			result = append(result, buildClaudeEvent("content_block_delta", map[string]interface{}{
				"index": ctx.ToolIndex, "delta": map[string]interface{}{"type": "input_json_delta", "partial_json": evt.Delta},
			})...)
			updateResponseOutputItemLookup(ctx, item)
		}

	case "response.function_call_arguments.done":
		item := resolveResponseOutputItem(ctx, evt.OutputIndex, evt.ItemID, "")
		if item == nil {
			item = currentResponseFunctionItem(ctx)
		}
		if item != nil {
			item.DoneArguments = evt.Arguments
			if evt.Arguments != "" {
				item.Arguments = evt.Arguments
				ctx.ToolArguments = evt.Arguments
			}
			updateResponseOutputItemLookup(ctx, item)
		}

	case "response.output_item.done":
		if evt.Item != nil && evt.Item.Type == "function_call" {
			item := resolveOrCreateResponseFunctionItem(ctx, evt.OutputIndex, evt.ItemID, evt.Item.CallID, evt.Item.Name)
			if evt.Item.ID != "" && item.ItemID == "" {
				item.ItemID = evt.Item.ID
			}
			if evt.Item.Arguments != "" && item.DoneArguments == "" {
				item.Arguments = evt.Item.Arguments
			}
			item.Completed = true
			item.Status = "completed"
			ctx.CurrentToolID = item.CallID
			if ctx.CurrentToolID == "" {
				ctx.CurrentToolID = item.ItemID
			}
			ctx.CurrentToolName = item.Name
			ctx.ToolArguments = effectiveResponseItemArguments(item, evt.Item.Arguments)
			updateResponseOutputItemLookup(ctx, item)
			if ctx.ToolBlockStarted {
				result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ToolIndex})...)
				ctx.ToolBlockStarted = false
				ctx.ContentIndex++
			}
		}

	case "response.completed":
		if evt.Response != nil {
			syncOpenAIUsage(evt.Response.Usage.InputTokens, evt.Response.Usage.OutputTokens, evt.Response.Usage.TotalTokens, ctx)
		}
		emitText, emitThinking := makeThinkEmitters(ctx, &result)
		flushThinkTaggedStream(ctx, emitText, emitThinking)
		if ctx.ThinkingBlockStarted {
			result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ThinkingIndex})...)
			ctx.ThinkingBlockStarted = false
		}
		if ctx.ContentBlockStarted {
			result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ContentIndex})...)
			ctx.ContentBlockStarted = false
		}
		stopReason := "end_turn"
		for _, item := range orderedResponseOutputItems(ctx) {
			if item != nil && item.Type == "function_call" {
				stopReason = "tool_use"
				break
			}
		}
		result = append(result, buildClaudeEvent("message_delta", map[string]interface{}{
			"delta": map[string]interface{}{"stop_reason": stopReason, "stop_sequence": nil},
			"usage": map[string]interface{}{"output_tokens": ctx.OutputTokens},
		})...)
		result = append(result, buildClaudeEvent("message_stop", map[string]interface{}{})...)
		ctx.FinishReasonSent = true
	}

	return result, nil
}

// Helper functions

func convertClaudeMessageToOpenAI2Items(content []interface{}, role string) []map[string]interface{} {
	var items []map[string]interface{}
	var messageParts []map[string]interface{}
	textType := "input_text"
	if role == "assistant" {
		textType = "output_text"
	}

	flushMessage := func() {
		if len(messageParts) == 0 {
			return
		}
		items = append(items, map[string]interface{}{
			"type":    "message",
			"role":    role,
			"content": messageParts,
		})
		messageParts = nil
	}

	for _, block := range content {
		m, ok := block.(map[string]interface{})
		if !ok {
			continue
		}

		blockType, _ := m["type"].(string)
		switch blockType {
		case "text":
			text, _ := m["text"].(string)
			messageParts = append(messageParts, map[string]interface{}{"type": textType, "text": text})
		case "image":
			if source, ok := m["source"].(map[string]interface{}); ok {
				if url, ok := source["url"].(string); ok && strings.TrimSpace(url) != "" {
					messageParts = append(messageParts, openAI2ImagePartFromURL(url))
					continue
				}
				if sourceType, _ := source["type"].(string); strings.ToLower(strings.TrimSpace(sourceType)) == "base64" {
					mediaType, _ := source["media_type"].(string)
					data, _ := source["data"].(string)
					mediaType = strings.TrimSpace(mediaType)
					if mediaType == "" {
						mediaType = "image/png"
					}
					if data != "" {
						messageParts = append(messageParts, openAI2ImagePartFromURL(fmt.Sprintf("data:%s;base64,%s", mediaType, data)))
					}
				}
			}
		case "thinking":
			// Skip thinking blocks - they are Claude's internal reasoning
			continue
		case "tool_use":
			flushMessage()
			callID, _ := m["id"].(string)
			name, _ := m["name"].(string)
			args, _ := json.Marshal(m["input"])
			items = append(items, map[string]interface{}{
				"type":      "function_call",
				"call_id":   callID,
				"name":      name,
				"arguments": string(args),
			})
		case "tool_result":
			flushMessage()
			callID, _ := m["tool_use_id"].(string)
			items = append(items, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  toolResultToString(m["content"]),
			})
		}
	}
	flushMessage()

	return items
}

func toolResultToString(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

func convertOpenAI2InputToClaude(input interface{}) []map[string]interface{} {
	var messages []map[string]interface{}

	switch v := input.(type) {
	case string:
		messages = append(messages, map[string]interface{}{"role": "user", "content": v})
	case []interface{}:
		var pendingToolUses []map[string]interface{}
		var pendingToolResults []map[string]interface{}

		for _, item := range v {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			itemType, _ := itemMap["type"].(string)
			switch itemType {
			case "message":
				// Flush pending tool uses before user message
				if len(pendingToolUses) > 0 {
					messages = append(messages, map[string]interface{}{"role": "assistant", "content": pendingToolUses})
					pendingToolUses = nil
				}
				// Flush pending tool results before user message
				if len(pendingToolResults) > 0 {
					messages = append(messages, map[string]interface{}{"role": "user", "content": pendingToolResults})
					pendingToolResults = nil
				}

				role, _ := itemMap["role"].(string)
				content := convertOpenAI2ContentToClaude(itemMap["content"], role)
				messages = append(messages, map[string]interface{}{"role": role, "content": content})

			case "function_call":
				// Convert to Claude tool_use
				callID, _ := itemMap["call_id"].(string)
				if callID == "" {
					callID, _ = itemMap["id"].(string)
				}
				name, _ := itemMap["name"].(string)
				argsStr, _ := itemMap["arguments"].(string)
				args := parseJSONObjectArguments(argsStr, "Failed to unmarshal tool arguments")
				pendingToolUses = append(pendingToolUses, map[string]interface{}{
					"type": "tool_use", "id": callID, "name": name, "input": args,
				})

			case "function_call_output":
				// Flush pending tool uses first
				if len(pendingToolUses) > 0 {
					messages = append(messages, map[string]interface{}{"role": "assistant", "content": pendingToolUses})
					pendingToolUses = nil
				}
				// Convert to Claude tool_result
				callID, _ := itemMap["call_id"].(string)
				output, _ := itemMap["output"].(string)
				pendingToolResults = append(pendingToolResults, map[string]interface{}{
					"type": "tool_result", "tool_use_id": callID, "content": output,
				})
			}
		}

		// Flush remaining
		if len(pendingToolUses) > 0 {
			messages = append(messages, map[string]interface{}{"role": "assistant", "content": pendingToolUses})
		}
		if len(pendingToolResults) > 0 {
			messages = append(messages, map[string]interface{}{"role": "user", "content": pendingToolResults})
		}
	}
	return messages
}

func convertOpenAI2ContentToClaude(content interface{}, role string) interface{} {
	arr, ok := content.([]interface{})
	if !ok {
		return content
	}

	var result []map[string]interface{}
	for _, part := range arr {
		partMap, ok := part.(map[string]interface{})
		if !ok {
			continue
		}
		switch partMap["type"] {
		case "input_text", "output_text":
			if text, ok := partMap["text"].(string); ok && text != "" {
				result = append(result, map[string]interface{}{"type": "text", "text": text})
			}
		case "input_image", "image_url", "image":
			url, ok := normalizeImageURL(partMap["image_url"])
			if !ok {
				url, ok = normalizeImageURL(partMap)
			}
			if ok {
				if imageBlock := claudeImageBlockFromURL(url); imageBlock != nil {
					result = append(result, imageBlock)
				}
			}
		}
	}

	if len(result) == 1 {
		if text, ok := result[0]["text"].(string); ok {
			return text
		}
	}
	return result
}
