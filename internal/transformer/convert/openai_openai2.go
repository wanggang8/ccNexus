package convert

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// OpenAIReqToOpenAI2 converts OpenAI Chat request to OpenAI Responses request
func OpenAIReqToOpenAI2(openaiReq []byte, model string) ([]byte, error) {
	var rawReq map[string]interface{}
	if err := json.Unmarshal(openaiReq, &rawReq); err != nil {
		return nil, err
	}
	var req transformer.OpenAIRequest
	if err := json.Unmarshal(openaiReq, &req); err != nil {
		return nil, err
	}

	openai2Req := map[string]interface{}{
		"model": model,
	}
	if stream, ok := rawReq["stream"]; ok {
		openai2Req["stream"] = stream
	}

	var input []map[string]interface{}
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			if content, ok := msg.Content.(string); ok {
				openai2Req["instructions"] = content
			}
			continue
		}

		// tool result messages → function_call_output
		if msg.Role == "tool" {
			content, _ := msg.Content.(string)
			input = append(input, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": msg.ToolCallID,
				"output":  content,
			})
			continue
		}

		// assistant messages with tool_calls → function_call items (optionally preceded by a text message item)
		if len(msg.ToolCalls) > 0 {
			if parts := convertOpenAIChatContentToOpenAI2Parts(msg.Content, msg.Role); len(parts) > 0 {
				input = append(input, map[string]interface{}{
					"type": "message", "role": "assistant",
					"content": parts,
				})
			}
			for _, tc := range msg.ToolCalls {
				input = append(input, map[string]interface{}{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				})
			}
			continue
		}

		item := map[string]interface{}{"type": "message", "role": msg.Role}
		item["content"] = convertOpenAIChatContentToOpenAI2Parts(msg.Content, msg.Role)
		input = append(input, item)
	}
	openai2Req["input"] = input
	if req.ReasoningEffort != nil {
		openai2Req["reasoning_effort"] = req.ReasoningEffort
	}
	// TODO: max_output_tokens is standard OpenAI Responses API param but some
	// third-party endpoints (e.g. SiliconFlow) don't support it. Skipping for compatibility.

	if len(req.Tools) > 0 {
		var tools []map[string]interface{}
		for _, tool := range req.Tools {
			if tool.Type == "function" {
				toolMap := map[string]interface{}{
					"type":        "function",
					"name":        tool.Function.Name,
					"description": tool.Function.Description,
					"parameters":  tool.Function.Parameters,
				}
				if tool.Function.Strict != nil {
					toolMap["strict"] = *tool.Function.Strict
				}
				tools = append(tools, toolMap)
			}
		}
		openai2Req["tools"] = tools
	}

	if req.ToolChoice != nil {
		if mapped := mapOpenAIToolChoiceToOpenAI2(req.ToolChoice); mapped != nil {
			openai2Req["tool_choice"] = mapped
		}
	}

	return json.Marshal(openai2Req)
}

// OpenAI2ReqToOpenAI converts OpenAI Responses request to OpenAI Chat request
func OpenAI2ReqToOpenAI(openai2Req []byte, model string) ([]byte, error) {
	var rawReq map[string]interface{}
	if err := json.Unmarshal(openai2Req, &rawReq); err != nil {
		return nil, err
	}

	var req transformer.OpenAI2Request
	if err := json.Unmarshal(openai2Req, &req); err != nil {
		return nil, err
	}

	var messages []transformer.OpenAIMessage

	if req.Instructions != "" {
		messages = append(messages, transformer.OpenAIMessage{Role: "system", Content: req.Instructions})
	}

	switch v := req.Input.(type) {
	case string:
		messages = append(messages, transformer.OpenAIMessage{Role: "user", Content: v})
	case []interface{}:
		var pendingToolCalls []transformer.OpenAIToolCall

		for _, item := range v {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			itemType, _ := itemMap["type"].(string)
			if itemType == "" {
				if _, hasRole := itemMap["role"].(string); hasRole {
					itemType = "message"
				}
			}
			switch itemType {
			case "message":
				if len(pendingToolCalls) > 0 {
					messages = append(messages, transformer.OpenAIMessage{Role: "assistant", ToolCalls: pendingToolCalls})
					pendingToolCalls = nil
				}
				role, _ := itemMap["role"].(string)
				content := convertOpenAI2ContentToOpenAIChat(itemMap["content"], role)
				if content == nil {
					content = ""
				}
				messages = append(messages, transformer.OpenAIMessage{Role: role, Content: content})

			case "function_call":
				callID, _ := itemMap["call_id"].(string)
				name, _ := itemMap["name"].(string)
				args, _ := itemMap["arguments"].(string)
				pendingToolCalls = append(pendingToolCalls, transformer.OpenAIToolCall{
					ID:   callID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: name, Arguments: args},
				})

			case "function_call_output":
				if len(pendingToolCalls) > 0 {
					messages = append(messages, transformer.OpenAIMessage{Role: "assistant", ToolCalls: pendingToolCalls})
					pendingToolCalls = nil
				}
				callID, _ := itemMap["call_id"].(string)
				output, _ := itemMap["output"].(string)
				messages = append(messages, transformer.OpenAIMessage{Role: "tool", Content: output, ToolCallID: callID})
			}
		}

		if len(pendingToolCalls) > 0 {
			messages = append(messages, transformer.OpenAIMessage{Role: "assistant", ToolCalls: pendingToolCalls})
		}
	}

	openaiReq := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   req.Stream,
	}

	if req.MaxOutputTokens > 0 {
		openaiReq["max_completion_tokens"] = req.MaxOutputTokens
	}
	if req.Temperature != nil {
		openaiReq["temperature"] = req.Temperature
	}
	if req.ReasoningEffort != nil {
		openaiReq["reasoning_effort"] = req.ReasoningEffort
	}

	if len(req.Tools) > 0 {
		var tools []map[string]interface{}
		for _, tool := range req.Tools {
			params, description, ok := buildOpenAIChatToolDefinition(tool)
			if !ok {
				continue
			}
			fn := map[string]interface{}{
				"name":       tool.Name,
				"parameters": params,
			}
			if description != "" {
				fn["description"] = description
			}
			if tool.Strict != nil {
				fn["strict"] = *tool.Strict
			}
			tools = append(tools, map[string]interface{}{
				"type":     "function",
				"function": fn,
			})
		}
		if len(tools) > 0 {
			openaiReq["tools"] = tools
		}
	}

	if req.ToolChoice != nil {
		if mapped := mapOpenAI2ToolChoiceToOpenAI(req.ToolChoice); mapped != nil {
			openaiReq["tool_choice"] = mapped
		}
	}

	for _, key := range []string{"metadata", "stream_options", "user", "prompt_cache_retention"} {
		if value, ok := rawReq[key]; ok {
			openaiReq[key] = value
		}
	}

	return json.Marshal(openaiReq)
}

func buildOpenAIChatToolDefinition(tool transformer.OpenAI2Tool) (map[string]interface{}, string, bool) {
	description := strings.TrimSpace(tool.Description)
	switch tool.Type {
	case "function":
		params := tool.Parameters
		if len(params) == 0 {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		return params, description, true
	case "custom":
		params := map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"input": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"input"},
		}
		inputDesc := buildCustomToolInputDescription(tool)
		params["properties"].(map[string]interface{})["input"].(map[string]interface{})["description"] = inputDesc
		if formatHint := buildCustomToolFormatHint(tool.Format); formatHint != "" {
			if description != "" {
				description += "\n\n"
			}
			description += formatHint
		}
		return params, description, true
	default:
		return nil, "", false
	}
}

func buildCustomToolInputDescription(tool transformer.OpenAI2Tool) string {
	base := "The input for this tool."
	if desc := strings.TrimSpace(tool.Description); desc != "" {
		base = desc
	}
	if formatHint := buildCustomToolFormatHint(tool.Format); formatHint != "" {
		return base + "\n\n" + formatHint
	}
	return base
}

func buildCustomToolFormatHint(format map[string]interface{}) string {
	if len(format) == 0 {
		return ""
	}
	parts := []string{"Custom tool format constraints:"}
	if formatType, _ := format["type"].(string); formatType != "" {
		parts = append(parts, fmt.Sprintf("type=%s", formatType))
	}
	if syntax, _ := format["syntax"].(string); syntax != "" {
		parts = append(parts, fmt.Sprintf("syntax=%s", syntax))
	}
	if definition, _ := format["definition"].(string); definition != "" {
		parts = append(parts, fmt.Sprintf("definition=%s", definition))
	}
	return strings.Join(parts, "\n")
}

func mapOpenAIToolChoiceToOpenAI2(toolChoice interface{}) interface{} {
	if toolChoice == nil {
		return nil
	}

	switch tc := toolChoice.(type) {
	case string:
		return tc
	case map[string]interface{}:
		choiceType, _ := tc["type"].(string)
		if choiceType != "function" {
			return nil
		}

		// Chat Completions shape: {"type":"function","function":{"name":"..."}}
		if fn, ok := tc["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok && name != "" {
				return map[string]interface{}{"type": "function", "name": name}
			}
		}

		// Responses-compatible shape already.
		if name, ok := tc["name"].(string); ok && name != "" {
			return map[string]interface{}{"type": "function", "name": name}
		}
	}

	return nil
}

func mapOpenAI2ToolChoiceToOpenAI(toolChoice interface{}) interface{} {
	if toolChoice == nil {
		return nil
	}

	switch tc := toolChoice.(type) {
	case string:
		return tc
	case map[string]interface{}:
		choiceType, _ := tc["type"].(string)
		if choiceType == "function" {
			if name, ok := tc["name"].(string); ok && name != "" {
				return map[string]interface{}{
					"type": "function",
					"function": map[string]string{
						"name": name,
					},
				}
			}
		}
	}

	return nil
}

// OpenAIRespToOpenAI2 converts OpenAI Chat response to OpenAI Responses response
func OpenAIRespToOpenAI2(openaiResp []byte) ([]byte, error) {
	var resp transformer.OpenAIResponse
	if err := json.Unmarshal(openaiResp, &resp); err != nil {
		return nil, err
	}

	var output []map[string]interface{}

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		if choice.Message.Content != "" {
			output = append(output, map[string]interface{}{
				"type": "message",
				"role": "assistant",
				"content": []map[string]interface{}{
					{"type": "output_text", "text": choice.Message.Content},
				},
			})
		}
		for _, tc := range choice.Message.ToolCalls {
			output = append(output, map[string]interface{}{
				"type":      "function_call",
				"call_id":   tc.ID,
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
			})
		}
	}

	openai2Resp := map[string]interface{}{
		"id":     resp.ID,
		"object": "response",
		"status": "completed",
		"output": output,
		"usage": map[string]interface{}{
			"input_tokens":  resp.Usage.PromptTokens,
			"output_tokens": resp.Usage.CompletionTokens,
			"total_tokens":  resp.Usage.TotalTokens,
		},
	}

	return json.Marshal(openai2Resp)
}

// OpenAI2RespToOpenAI converts OpenAI Responses response to OpenAI Chat response
func OpenAI2RespToOpenAI(openai2Resp []byte, model string) ([]byte, error) {
	var resp transformer.OpenAI2Response
	if err := json.Unmarshal(openai2Resp, &resp); err != nil {
		return nil, err
	}

	var textContent string
	var toolCalls []map[string]interface{}

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					textContent += part.Text
				}
			}
		case "function_call":
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   item.CallID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      item.Name,
					"arguments": item.Arguments,
				},
			})
		}
	}

	message := map[string]interface{}{"role": "assistant", "content": textContent}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	openaiResp := map[string]interface{}{
		"id":      resp.ID,
		"object":  "chat.completion",
		"model":   model,
		"choices": []map[string]interface{}{{"index": 0, "message": message, "finish_reason": finishReason}},
		"usage": map[string]interface{}{
			"prompt_tokens":     resp.Usage.InputTokens,
			"completion_tokens": resp.Usage.OutputTokens,
			"total_tokens":      resp.Usage.TotalTokens,
		},
	}
	if resp.Usage.TotalTokens == 0 {
		openaiResp["usage"].(map[string]interface{})["total_tokens"] = resp.Usage.InputTokens + resp.Usage.OutputTokens
	}

	return json.Marshal(openaiResp)
}

// OpenAIStreamToOpenAI2 converts OpenAI Chat stream chunk to OpenAI Responses stream event
func OpenAIStreamToOpenAI2(event []byte, ctx *transformer.StreamContext) ([]byte, error) {
	_, jsonData := ParseSSE(event)
	if jsonData == "" || jsonData == "[DONE]" {
		if jsonData == "[DONE]" && !ctx.FinishReasonSent {
			// Handle [DONE] if finish_reason wasn't received
			var result strings.Builder
			writeEvent := func(evt map[string]interface{}) {
				d, _ := json.Marshal(evt)
				result.WriteString(fmt.Sprintf("data: %s\n\n", d))
			}
			if ctx.ContentBlockStarted {
				accText := ctx.ContentText
				ctx.ContentText = ""
				messageIndex := 0
				for _, item := range orderedResponseOutputItems(ctx) {
					if item != nil && item.Type == "message" {
						messageIndex = item.OutputIndex
						break
					}
				}
				writeEvent(map[string]interface{}{"type": "response.output_text.done", "output_index": messageIndex, "content_index": 0, "text": accText})
				writeEvent(map[string]interface{}{"type": "response.content_part.done", "output_index": messageIndex, "content_index": 0, "part": map[string]interface{}{"type": "output_text", "text": accText}})
				writeEvent(map[string]interface{}{"type": "response.output_item.done", "output_index": messageIndex, "item": map[string]interface{}{"type": "message", "role": "assistant", "status": "completed"}})
			}
			writeEvent(map[string]interface{}{
				"type": "response.completed",
				"response": map[string]interface{}{
					"id": ctx.MessageID, "object": "response", "status": "completed",
					"usage": currentResponsesUsage(ctx),
				},
			})
			result.WriteString("data: [DONE]\n\n")
			return []byte(result.String()), nil
		}
		return nil, nil
	}

	// Check for error response
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(jsonData), &errResp); err == nil && errResp.Error.Message != "" {
		return nil, fmt.Errorf("upstream error: %s", errResp.Error.Message)
	}

	var chunk transformer.OpenAIStreamChunk
	if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
		return nil, nil
	}

	if chunk.Usage != nil {
		syncOpenAIUsage(chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens, chunk.Usage.TotalTokens, ctx)
	}

	var result strings.Builder
	writeEvent := func(evt map[string]interface{}) {
		d, _ := json.Marshal(evt)
		result.WriteString(fmt.Sprintf("data: %s\n\n", d))
	}

	if !ctx.MessageStartSent {
		ctx.MessageStartSent = true
		ctx.MessageID = chunk.ID
		writeEvent(map[string]interface{}{
			"type":     "response.created",
			"response": map[string]interface{}{"id": chunk.ID, "object": "response", "status": "in_progress"},
		})
	}

	if len(chunk.Choices) > 0 {
		delta := chunk.Choices[0].Delta
		finishReason := chunk.Choices[0].FinishReason

		// Handle text content
		if delta.Content != "" {
			var messageItem *transformer.ResponseOutputItemState
			for _, item := range orderedResponseOutputItems(ctx) {
				if item != nil && item.Type == "message" {
					messageItem = item
					break
				}
			}
			if messageItem == nil {
				messageItem = registerResponseOutputItem(ctx, &transformer.ResponseOutputItemState{
					Type:        "message",
					OutputIndex: nextResponseOutputIndex(ctx),
					Role:        "assistant",
					Status:      "in_progress",
				})
			}
			if !ctx.ContentBlockStarted {
				ctx.ContentBlockStarted = true
				writeEvent(map[string]interface{}{
					"type": "response.output_item.added", "output_index": messageItem.OutputIndex,
					"item": map[string]interface{}{"type": "message", "role": "assistant", "status": "in_progress", "content": []interface{}{}},
				})
				writeEvent(map[string]interface{}{
					"type": "response.content_part.added", "output_index": messageItem.OutputIndex, "content_index": 0,
					"part": map[string]interface{}{"type": "output_text", "text": ""},
				})
			}
			ctx.ContentText += delta.Content
			writeEvent(map[string]interface{}{"type": "response.output_text.delta", "output_index": messageItem.OutputIndex, "content_index": 0, "delta": delta.Content})
		}

		// Handle tool calls
		if len(delta.ToolCalls) > 0 && ctx.ContentBlockStarted {
			accText := ctx.ContentText
			ctx.ContentText = ""
			for _, item := range orderedResponseOutputItems(ctx) {
				if item != nil && item.Type == "message" && !item.Completed {
					writeEvent(map[string]interface{}{"type": "response.output_text.done", "output_index": item.OutputIndex, "content_index": 0, "text": accText})
					writeEvent(map[string]interface{}{"type": "response.content_part.done", "output_index": item.OutputIndex, "content_index": 0, "part": map[string]interface{}{"type": "output_text", "text": accText}})
					writeEvent(map[string]interface{}{"type": "response.output_item.done", "output_index": item.OutputIndex, "item": map[string]interface{}{"type": "message", "role": "assistant", "status": "completed"}})
					item.Status = "completed"
					item.Completed = true
					updateResponseOutputItemLookup(ctx, item)
					break
				}
			}
			ctx.ContentBlockStarted = false
		}

		// Handle tool calls
		for _, tc := range delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			alias := fmt.Sprintf("openai-tool:%d", idx)
			item := resolveResponseOutputItem(ctx, nil, "", "")
			if ctx.ResponseOutputItemLookup != nil {
				item = ctx.ResponseOutputItemLookup[alias]
			}
			if item == nil {
				outputIndex := nextResponseOutputIndex(ctx)
				item = registerResponseOutputItem(ctx, &transformer.ResponseOutputItemState{
					Type:        "function_call",
					OutputIndex: outputIndex,
					CallID:      tc.ID,
					Name:        tc.Function.Name,
					Status:      "in_progress",
				})
				bindResponseOutputItemAlias(ctx, alias, item)
				ctx.ActiveToolCalls = append(ctx.ActiveToolCalls, transformer.ActiveToolCall{
					ID:          tc.ID,
					Name:        tc.Function.Name,
					Arguments:   "",
					OutputIndex: outputIndex,
				})
				writeEvent(map[string]interface{}{
					"type": "response.output_item.added", "output_index": outputIndex,
					"item": map[string]interface{}{"type": "function_call", "call_id": tc.ID, "name": tc.Function.Name, "arguments": "", "status": "in_progress"},
				})
			}
			if tc.ID != "" && item.CallID == "" {
				item.CallID = tc.ID
			}
			if tc.Function.Name != "" && item.Name == "" {
				item.Name = tc.Function.Name
			}
			ctx.CurrentToolID = item.CallID
			ctx.CurrentToolName = item.Name
			// Keep legacy fields in sync for existing tests/compatibility.
			if tc.Function.Arguments != "" {
				item.Arguments += tc.Function.Arguments
				ctx.ToolArguments = item.Arguments
				for i := range ctx.ActiveToolCalls {
					if ctx.ActiveToolCalls[i].OutputIndex == item.OutputIndex || (item.CallID != "" && ctx.ActiveToolCalls[i].ID == item.CallID) {
						ctx.ActiveToolCalls[i].ID = item.CallID
						ctx.ActiveToolCalls[i].Name = item.Name
						ctx.ActiveToolCalls[i].Arguments = item.Arguments
						ctx.ActiveToolCalls[i].OutputIndex = item.OutputIndex
						break
					}
				}
				writeEvent(map[string]interface{}{
					"type": "response.function_call_arguments.delta", "output_index": item.OutputIndex, "delta": tc.Function.Arguments,
				})
			}
			updateResponseOutputItemLookup(ctx, item)
		}

		// Handle finish
		if finishReason != nil && *finishReason != "" {
			if ctx.ContentBlockStarted {
				accText := ctx.ContentText
				ctx.ContentText = ""
				for _, item := range orderedResponseOutputItems(ctx) {
					if item != nil && item.Type == "message" {
						writeEvent(map[string]interface{}{"type": "response.output_text.done", "output_index": item.OutputIndex, "content_index": 0, "text": accText})
						writeEvent(map[string]interface{}{"type": "response.content_part.done", "output_index": item.OutputIndex, "content_index": 0, "part": map[string]interface{}{"type": "output_text", "text": accText}})
						writeEvent(map[string]interface{}{"type": "response.output_item.done", "output_index": item.OutputIndex, "item": map[string]interface{}{"type": "message", "role": "assistant", "status": "completed"}})
						item.Status = "completed"
						item.Completed = true
						updateResponseOutputItemLookup(ctx, item)
						break
					}
				}
				ctx.ContentBlockStarted = false
			}
			if *finishReason == "tool_calls" {
				for _, item := range orderedResponseOutputItems(ctx) {
					if item == nil || item.Type != "function_call" || item.Completed {
						continue
					}
					finalArgs := effectiveResponseItemArguments(item, item.Arguments)
					item.DoneArguments = finalArgs
					item.Arguments = finalArgs
					item.Status = "completed"
					item.Completed = true
					for i := range ctx.ActiveToolCalls {
						if ctx.ActiveToolCalls[i].OutputIndex == item.OutputIndex || (item.CallID != "" && ctx.ActiveToolCalls[i].ID == item.CallID) {
							ctx.ActiveToolCalls[i].ID = item.CallID
							ctx.ActiveToolCalls[i].Name = item.Name
							ctx.ActiveToolCalls[i].Arguments = finalArgs
							ctx.ActiveToolCalls[i].OutputIndex = item.OutputIndex
							break
						}
					}
					writeEvent(map[string]interface{}{"type": "response.function_call_arguments.done", "output_index": item.OutputIndex, "arguments": finalArgs})
					writeEvent(map[string]interface{}{
						"type": "response.output_item.done", "output_index": item.OutputIndex,
						"item": map[string]interface{}{"type": "function_call", "call_id": item.CallID, "name": item.Name, "arguments": finalArgs, "status": "completed"},
					})
					updateResponseOutputItemLookup(ctx, item)
				}
			}
			writeEvent(map[string]interface{}{
				"type": "response.completed",
				"response": map[string]interface{}{
					"id": ctx.MessageID, "object": "response", "status": "completed",
					"usage": currentResponsesUsage(ctx),
				},
			})
			result.WriteString("data: [DONE]\n\n")
			ctx.FinishReasonSent = true
		}
	}

	if result.Len() > 0 {
		return []byte(result.String()), nil
	}
	return nil, nil
}

// OpenAI2StreamToOpenAI converts OpenAI Responses stream event to OpenAI Chat stream chunk
func OpenAI2StreamToOpenAI(event []byte, ctx *transformer.StreamContext, model string) ([]byte, error) {
	_, jsonData := ParseSSE(event)
	if jsonData == "" || jsonData == "[DONE]" {
		if jsonData == "[DONE]" {
			return []byte("data: [DONE]\n\n"), nil
		}
		return nil, nil
	}

	var evt transformer.OpenAI2StreamEvent
	if err := json.Unmarshal([]byte(jsonData), &evt); err != nil {
		return nil, nil
	}

	switch evt.Type {
	case "response.created":
		if evt.Response != nil {
			ctx.MessageID = evt.Response.ID
		}
		return nil, nil

	case "response.output_text.delta":
		return buildOpenAIChunk(ctx.MessageID, model, evt.Delta, nil, "")

	case "response.output_item.added":
		if evt.Item != nil && evt.Item.Type == "function_call" {
			item := resolveOrCreateResponseFunctionItem(ctx, evt.OutputIndex, evt.ItemID, evt.Item.CallID, evt.Item.Name)
			if evt.Item.ID != "" && item.ItemID == "" {
				item.ItemID = evt.Item.ID
			}
			if evt.Item.Status != "" {
				item.Status = evt.Item.Status
			}
			if evt.Item.Arguments != "" {
				item.Arguments = evt.Item.Arguments
			}
			if !item.HasLocalIndex {
				item.LocalIndex = ctx.ToolIndex
				item.HasLocalIndex = true
				ctx.ToolIndex = item.LocalIndex + 1
			}
			ctx.ToolBlockStarted = true
			ctx.CurrentToolID = item.CallID
			if ctx.CurrentToolID == "" {
				ctx.CurrentToolID = item.ItemID
			}
			ctx.CurrentToolName = item.Name
			ctx.ToolArguments = item.Arguments
			updateResponseOutputItemLookup(ctx, item)
		}
		return nil, nil

	case "response.function_call_arguments.delta":
		item := resolveResponseOutputItem(ctx, evt.OutputIndex, evt.ItemID, "")
		if item == nil {
			item = currentResponseFunctionItem(ctx)
		}
		if item != nil {
			item.Arguments += evt.Delta
			ctx.ToolArguments = item.Arguments
			updateResponseOutputItemLookup(ctx, item)
		}
		return nil, nil

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
		return nil, nil

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
			finalArgs := effectiveResponseItemArguments(item, evt.Item.Arguments)
			ctx.ToolBlockStarted = false
			ctx.CurrentToolID = item.CallID
			if ctx.CurrentToolID == "" {
				ctx.CurrentToolID = item.ItemID
			}
			ctx.CurrentToolName = item.Name
			ctx.ToolArguments = finalArgs
			updateResponseOutputItemLookup(ctx, item)
			return buildOpenAIChunk(ctx.MessageID, model, "", []map[string]interface{}{
				{"index": item.LocalIndex, "id": ctx.CurrentToolID, "type": "function",
					"function": map[string]interface{}{"name": item.Name, "arguments": finalArgs}},
			}, "")
		}
		return nil, nil

	case "response.completed":
		if evt.Response != nil {
			syncOpenAIUsage(evt.Response.Usage.InputTokens, evt.Response.Usage.OutputTokens, evt.Response.Usage.TotalTokens, ctx)
		}
		finishReason := "stop"
		for _, item := range orderedResponseOutputItems(ctx) {
			if item != nil && item.Type == "function_call" {
				finishReason = "tool_calls"
				break
			}
		}
		return buildOpenAIChunkWithUsage(ctx.MessageID, model, "", nil, finishReason, currentOpenAIUsage(ctx))
	}

	return nil, nil
}

func convertOpenAIChatContentToOpenAI2Parts(content interface{}, role string) []map[string]interface{} {
	var parts []map[string]interface{}
	switch v := content.(type) {
	case string:
		if v == "" {
			return parts
		}
		textType := "input_text"
		if role == "assistant" {
			textType = "output_text"
		}
		parts = append(parts, map[string]interface{}{"type": textType, "text": v})
	case []interface{}:
		textType := "input_text"
		if role == "assistant" {
			textType = "output_text"
		}
		for _, part := range v {
			partMap, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			switch partMap["type"] {
			case "text":
				if text, ok := partMap["text"].(string); ok && text != "" {
					parts = append(parts, map[string]interface{}{"type": textType, "text": text})
				}
			case "image_url":
				url, ok := normalizeImageURL(partMap["image_url"])
				if !ok {
					url, ok = normalizeImageURL(partMap)
				}
				if ok {
					if imagePart := openAI2ImagePartFromURL(url); imagePart != nil {
						parts = append(parts, imagePart)
					}
				}
			}
		}
	}
	return parts
}

func convertOpenAI2ContentToOpenAIChat(content interface{}, role string) interface{} {
	arr, ok := content.([]interface{})
	if !ok {
		if str, ok := content.(string); ok {
			return str
		}
		return nil
	}

	textType := "input_text"
	if role == "assistant" {
		textType = "output_text"
	}

	var parts []map[string]interface{}
	var textBuffer []string
	hasImage := false

	flushTextBuffer := func() {
		for _, text := range textBuffer {
			parts = append(parts, map[string]interface{}{"type": textType, "text": text})
		}
		textBuffer = nil
	}

	for _, part := range arr {
		partMap, ok := part.(map[string]interface{})
		if !ok {
			continue
		}

		switch partMap["type"] {
		case "input_text", "output_text":
			if text, ok := partMap["text"].(string); ok && text != "" {
				if hasImage {
					parts = append(parts, map[string]interface{}{"type": textType, "text": text})
				} else {
					textBuffer = append(textBuffer, text)
				}
			}
		case "input_image", "image_url", "image":
			url, ok := normalizeImageURL(partMap["image_url"])
			if !ok {
				url, ok = normalizeImageURL(partMap)
			}
			if ok {
				hasImage = true
				if len(textBuffer) > 0 {
					flushTextBuffer()
				}
				if imagePart := openAIChatImagePartFromURL(url); imagePart != nil {
					parts = append(parts, imagePart)
				}
			}
		}
	}

	if !hasImage {
		if len(textBuffer) == 0 {
			return nil
		}
		if len(textBuffer) == 1 {
			return textBuffer[0]
		}
		return strings.Join(textBuffer, "")
	}

	if len(textBuffer) > 0 {
		flushTextBuffer()
	}
	return parts
}
