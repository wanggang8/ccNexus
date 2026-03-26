package convert

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// OpenAIReqToOpenAI2 converts OpenAI Chat request to OpenAI Responses request
func OpenAIReqToOpenAI2(openaiReq []byte, model string) ([]byte, error) {
	var req transformer.OpenAIRequest
	if err := json.Unmarshal(openaiReq, &req); err != nil {
		return nil, err
	}

	openai2Req := map[string]interface{}{
		"model":  model,
		"stream": req.Stream,
	}

	var input []map[string]interface{}
	var instructions []string
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			if content := extractOpenAIRequestText(msg.Content); content != "" {
				instructions = append(instructions, content)
			}
			continue
		}

		if msg.Role == "tool" {
			input = append(input, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": msg.ToolCallID,
				"output":  stringifyOpenAIContent(msg.Content),
			})
			continue
		}

		item := map[string]interface{}{"type": "message", "role": msg.Role}
		contentParts := buildOpenAIRequestContentParts(msg.Content, msg.Role)

		if len(contentParts) > 0 {
			item["content"] = contentParts
			input = append(input, item)
		}
		if msg.Role == "assistant" {
			for _, tc := range msg.ToolCalls {
				input = append(input, map[string]interface{}{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				})
			}
		}
	}
	openai2Req["input"] = input
	if len(instructions) > 0 {
		openai2Req["instructions"] = strings.Join(instructions, "\n\n")
	}
	// TODO: max_output_tokens is standard OpenAI Responses API param but some
	// third-party endpoints (e.g. SiliconFlow) don't support it. Skipping for compatibility.

	if len(req.Tools) > 0 {
		var tools []map[string]interface{}
		for _, tool := range req.Tools {
			if tool.Type == "function" {
				tools = append(tools, map[string]interface{}{
					"type":        "function",
					"name":        tool.Function.Name,
					"description": tool.Function.Description,
					"parameters":  tool.Function.Parameters,
				})
			}
		}
		openai2Req["tools"] = tools

		// Preserve explicit tool routing semantics when moving to Responses API.
		if mapped := mapOpenAIToolChoiceToOpenAI2(req.ToolChoice); mapped != nil {
			openai2Req["tool_choice"] = mapped
		} else {
			// Keep explicit default for compatibility with providers that do not
			// treat omitted tool_choice as "auto".
			openai2Req["tool_choice"] = "auto"
		}
	}

	return json.Marshal(openai2Req)
}

// OpenAI2ReqToOpenAI converts OpenAI Responses request to OpenAI Chat request
func OpenAI2ReqToOpenAI(openai2Req []byte, model string) ([]byte, error) {
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
		var pendingReasoning string

		flushPendingToolCalls := func() {
			if len(pendingToolCalls) == 0 {
				return
			}
			msg := transformer.OpenAIMessage{Role: "assistant", ToolCalls: pendingToolCalls}
			if pendingReasoning != "" {
				msg.ReasoningContent = pendingReasoning
				pendingReasoning = ""
			}
			messages = append(messages, msg)
			pendingToolCalls = nil
		}

		for _, item := range v {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			itemType, _ := itemMap["type"].(string)
			role, _ := itemMap["role"].(string)
			if role != "" && itemType == "" {
				flushPendingToolCalls()
				msg := transformer.OpenAIMessage{Role: role, Content: buildOpenAIMessageContent(itemMap["content"], role)}
				if role == "assistant" && pendingReasoning != "" {
					msg.ReasoningContent = pendingReasoning
					pendingReasoning = ""
				}
				messages = append(messages, msg)
				continue
			}

			switch itemType {
			case "reasoning":
				pendingReasoning += extractOpenAI2ReasoningText(itemMap)

			case "message":
				flushPendingToolCalls()
				role, _ := itemMap["role"].(string)
				msg := transformer.OpenAIMessage{Role: role, Content: buildOpenAIMessageContent(itemMap["content"], role)}
				if role == "assistant" && pendingReasoning != "" {
					msg.ReasoningContent = pendingReasoning
					pendingReasoning = ""
				}
				messages = append(messages, msg)

			case "function_call":
				if pendingReasoning != "" && len(messages) > 0 && messages[len(messages)-1].Role == "assistant" {
					if messages[len(messages)-1].ReasoningContent == "" {
						messages[len(messages)-1].ReasoningContent = pendingReasoning
						pendingReasoning = ""
					}
				}
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
				flushPendingToolCalls()
				callID, _ := itemMap["call_id"].(string)
				output, _ := itemMap["output"].(string)
				messages = append(messages, transformer.OpenAIMessage{Role: "tool", Content: output, ToolCallID: callID})
			}
		}

		flushPendingToolCalls()
	}

	openaiReq := transformer.OpenAIRequest{
		Model:    model,
		Messages: messages,
		Stream:   req.Stream,
	}

	if req.MaxOutputTokens > 0 {
		openaiReq.MaxCompletionTokens = req.MaxOutputTokens
	}

	if len(req.Tools) > 0 {
		for _, tool := range req.Tools {
			var params map[string]interface{}
			switch tool.Type {
			case "function":
				params = tool.Parameters
			case "custom":
				// Custom tools (like apply_patch) use format instead of parameters
				// Convert to a function that accepts a single string input
				params = map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"input": map[string]interface{}{"type": "string", "description": "The input for this tool"},
					},
					"required": []string{"input"},
				}
			default:
				continue
			}
			openaiReq.Tools = append(openaiReq.Tools, transformer.OpenAITool{
				Type: "function",
				Function: struct {
					Name        string                 `json:"name"`
					Description string                 `json:"description,omitempty"`
					Parameters  map[string]interface{} `json:"parameters"`
				}{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  params,
				},
			})
		}
	}

	if req.ToolChoice != nil {
		openaiReq.ToolChoice = mapOpenAI2ToolChoiceToOpenAI(req.ToolChoice)
	}

	return json.Marshal(openaiReq)
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
		if choice.Message.ReasoningContent != "" {
			output = append(output, buildResponsesReasoningOutputItem(choice.Message.ReasoningContent))
		}
		if choice.Message.Content != "" {
			output = append(output, buildResponsesMessageOutputItem(choice.Message.Content))
		}
		for _, tc := range choice.Message.ToolCalls {
			output = append(output, buildResponsesFunctionCallOutputItem(tc.ID, tc.Function.Name, tc.Function.Arguments))
		}
	}

	status := "completed"
	if len(resp.Choices) > 0 {
		status = responseStatusFromFinishReason(resp.Choices[0].FinishReason)
	}
	openai2Resp := map[string]interface{}{
		"id":     resp.ID,
		"object": "response",
		"status": status,
		"model":  resp.Model,
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
	var reasoningContent string
	var toolCalls []map[string]interface{}

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					textContent += part.Text
				}
			}
		case "reasoning":
			reasoningContent += extractTypedOpenAI2ReasoningText(item)
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
	if reasoningContent != "" {
		message["reasoning_content"] = reasoningContent
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	} else if resp.Status == "incomplete" {
		finishReason = "length"
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
	_, jsonData := parseSSE(event)
	if jsonData == "" || jsonData == "[DONE]" {
		if jsonData == "[DONE]" && !ctx.FinishReasonSent {
			// Handle [DONE] if finish_reason wasn't received
			var result strings.Builder
			writeEvent := func(evt map[string]interface{}) {
				d, _ := json.Marshal(evt)
				result.WriteString(fmt.Sprintf("data: %s\n\n", d))
			}
			if ctx.ContentBlockStarted {
				writeEvent(map[string]interface{}{"type": "response.output_text.done", "output_index": 0, "content_index": 0})
				writeEvent(map[string]interface{}{"type": "response.content_part.done", "output_index": 0, "content_index": 0, "part": map[string]interface{}{"type": "output_text"}})
				writeEvent(map[string]interface{}{"type": "response.output_item.done", "output_index": 0, "item": map[string]interface{}{"type": "message", "role": "assistant", "status": "completed"}})
			}
			writeEvent(map[string]interface{}{
				"type": "response.completed",
				"response": map[string]interface{}{
					"id": ctx.MessageID, "object": "response", "status": "completed",
					"usage": map[string]interface{}{"input_tokens": ctx.InputTokens, "output_tokens": ctx.OutputTokens, "total_tokens": ctx.InputTokens + ctx.OutputTokens},
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
		if delta.ReasoningContent != "" {
			appendResponsesReasoningDelta(ctx, writeEvent, delta.ReasoningContent)
		}
		if delta.Content != "" {
			closeResponsesReasoningItem(ctx, writeEvent)
			if !ctx.ContentBlockStarted {
				ctx.ContentBlockStarted = true
				writeEvent(map[string]interface{}{
					"type": "response.output_item.added", "output_index": ctx.ContentIndex,
					"item": map[string]interface{}{"type": "message", "role": "assistant", "status": "in_progress", "content": []interface{}{}},
				})
				writeEvent(map[string]interface{}{
					"type": "response.content_part.added", "output_index": ctx.ContentIndex, "content_index": 0,
					"part": map[string]interface{}{"type": "output_text", "text": ""},
				})
			}
			writeEvent(map[string]interface{}{"type": "response.output_text.delta", "output_index": ctx.ContentIndex, "content_index": 0, "delta": delta.Content})
		}

		// Handle tool calls
		for _, tc := range delta.ToolCalls {
			closeResponsesReasoningItem(ctx, writeEvent)
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			// New tool call (has ID)
			if tc.ID != "" {
				ctx.ToolCallCounter++
				ctx.CurrentToolID = tc.ID
				ctx.CurrentToolName = tc.Function.Name
				ctx.ToolArguments = ""
				writeEvent(map[string]interface{}{
					"type": "response.output_item.added", "output_index": idx + 1,
					"item": map[string]interface{}{"type": "function_call", "call_id": tc.ID, "name": tc.Function.Name, "arguments": "", "status": "in_progress"},
				})
			}
			// Accumulate arguments
			if tc.Function.Arguments != "" {
				ctx.ToolArguments += tc.Function.Arguments
				writeEvent(map[string]interface{}{
					"type": "response.function_call_arguments.delta", "output_index": idx + 1, "delta": tc.Function.Arguments,
				})
			}
		}

		// Handle finish
		if finishReason != nil && *finishReason != "" {
			closeResponsesReasoningItem(ctx, writeEvent)
			if ctx.ContentBlockStarted {
				writeEvent(map[string]interface{}{"type": "response.output_text.done", "output_index": ctx.ContentIndex, "content_index": 0})
				writeEvent(map[string]interface{}{"type": "response.content_part.done", "output_index": ctx.ContentIndex, "content_index": 0, "part": map[string]interface{}{"type": "output_text"}})
				writeEvent(map[string]interface{}{"type": "response.output_item.done", "output_index": ctx.ContentIndex, "item": map[string]interface{}{"type": "message", "role": "assistant", "status": "completed"}})
				ctx.ContentBlockStarted = false
			}
			if *finishReason == "tool_calls" && ctx.CurrentToolID != "" {
				writeEvent(map[string]interface{}{"type": "response.function_call_arguments.done", "output_index": 1, "arguments": ctx.ToolArguments})
				writeEvent(map[string]interface{}{
					"type": "response.output_item.done", "output_index": 1,
					"item": map[string]interface{}{"type": "function_call", "call_id": ctx.CurrentToolID, "name": ctx.CurrentToolName, "arguments": ctx.ToolArguments, "status": "completed"},
				})
			}
			writeEvent(map[string]interface{}{
				"type": "response.completed",
				"response": map[string]interface{}{
					"id": ctx.MessageID, "object": "response", "status": "completed",
					"usage": map[string]interface{}{"input_tokens": ctx.InputTokens, "output_tokens": ctx.OutputTokens, "total_tokens": ctx.InputTokens + ctx.OutputTokens},
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
	_, jsonData := parseSSE(event)
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

	case "response.reasoning_summary_text.delta":
		return buildOpenAIReasoningChunk(ctx.MessageID, model, evt.Delta, "")

	case "response.output_text.delta":
		return buildOpenAIChunk(ctx.MessageID, model, evt.Delta, nil, "")

	case "response.output_item.added":
		if evt.Item != nil && evt.Item.Type == "function_call" {
			ctx.ToolBlockStarted = true
			ctx.CurrentToolID = evt.Item.CallID
			ctx.CurrentToolName = evt.Item.Name
			ctx.ToolArguments = ""
		}
		return nil, nil

	case "response.function_call_arguments.delta":
		if ctx.ToolBlockStarted {
			ctx.ToolArguments += evt.Delta
		}
		return nil, nil

	case "response.output_item.done":
		if evt.Item != nil && evt.Item.Type == "function_call" && ctx.ToolBlockStarted {
			ctx.ToolBlockStarted = false
			return buildOpenAIChunk(ctx.MessageID, model, "", []map[string]interface{}{
				{"index": ctx.ToolIndex, "id": ctx.CurrentToolID, "type": "function",
					"function": map[string]interface{}{"name": ctx.CurrentToolName, "arguments": ctx.ToolArguments}},
			}, "")
		}
		return nil, nil

	case "response.completed":
		if evt.Response != nil {
			if evt.Response.Usage.InputTokens > 0 {
				ctx.InputTokens = evt.Response.Usage.InputTokens
			}
			if evt.Response.Usage.OutputTokens > 0 {
				ctx.OutputTokens = evt.Response.Usage.OutputTokens
			}
		}
		finishReason := "stop"
		if ctx.CurrentToolID != "" {
			finishReason = "tool_calls"
		}
		usage := map[string]interface{}{
			"prompt_tokens":     ctx.InputTokens,
			"completion_tokens": ctx.OutputTokens,
			"total_tokens":      ctx.InputTokens + ctx.OutputTokens,
		}
		if evt.Response != nil && evt.Response.Usage.TotalTokens > 0 {
			usage["total_tokens"] = evt.Response.Usage.TotalTokens
		}
		return buildOpenAIChunkWithUsage(ctx.MessageID, model, "", nil, finishReason, usage)
	}

	return nil, nil
}

func extractOpenAI2Text(content interface{}) string {
	if text, ok := content.(string); ok {
		return text
	}
	arr, ok := content.([]interface{})
	if !ok {
		return ""
	}
	var parts []string
	for _, part := range arr {
		partMap, ok := part.(map[string]interface{})
		if !ok {
			continue
		}
		if partMap["type"] == "input_text" || partMap["type"] == "output_text" {
			if text, ok := partMap["text"].(string); ok {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "")
}

func extractOpenAIRequestText(content interface{}) string {
	switch value := content.(type) {
	case string:
		return value
	case []interface{}:
		var parts []string
		for _, item := range value {
			partMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			partType, _ := partMap["type"].(string)
			if partType == "text" || partType == "input_text" || partType == "output_text" {
				if text, ok := partMap["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

func buildOpenAIRequestContentParts(content interface{}, role string) []map[string]interface{} {
	textType := "input_text"
	if role == "assistant" {
		textType = "output_text"
	}

	appendText := func(parts []map[string]interface{}, text string) []map[string]interface{} {
		if text == "" {
			return parts
		}
		if len(parts) > 0 && parts[len(parts)-1]["type"] == textType {
			if existing, ok := parts[len(parts)-1]["text"].(string); ok {
				parts[len(parts)-1]["text"] = existing + text
			} else {
				parts[len(parts)-1]["text"] = text
			}
			return parts
		}
		return append(parts, map[string]interface{}{"type": textType, "text": text})
	}

	switch value := content.(type) {
	case string:
		if value == "" {
			return nil
		}
		return []map[string]interface{}{{"type": textType, "text": value}}
	case []interface{}:
		parts := make([]map[string]interface{}, 0, len(value))
		for _, item := range value {
			partMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			partType, _ := partMap["type"].(string)
			switch partType {
			case "text", "input_text", "output_text":
				if text, ok := partMap["text"].(string); ok {
					parts = appendText(parts, text)
				}
			case "image_url":
				imagePart := buildOpenAI2ImagePart(partMap)
				if imagePart != nil {
					parts = append(parts, imagePart)
				}
			}
		}
		return parts
	default:
		return nil
	}
}

func buildOpenAI2ImagePart(partMap map[string]interface{}) map[string]interface{} {
	rawImageURL, exists := partMap["image_url"]
	if !exists {
		return nil
	}

	part := map[string]interface{}{"type": "input_image"}
	switch value := rawImageURL.(type) {
	case string:
		if value == "" {
			return nil
		}
		part["image_url"] = value
	case map[string]interface{}:
		url, _ := value["url"].(string)
		if url == "" {
			return nil
		}
		part["image_url"] = url
		if detail, ok := value["detail"].(string); ok && detail != "" {
			part["detail"] = detail
		}
	default:
		return nil
	}

	return part
}

func buildOpenAIMessageContent(content interface{}, role string) interface{} {
	switch value := content.(type) {
	case string:
		return value
	case []interface{}:
		parts := make([]interface{}, 0, len(value))
		textParts := make([]string, 0)
		flushText := func() {
			if len(textParts) == 0 {
				return
			}
			parts = append(parts, map[string]interface{}{
				"type": "text",
				"text": strings.Join(textParts, ""),
			})
			textParts = nil
		}

		for _, item := range value {
			partMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			partType, _ := partMap["type"].(string)
			switch partType {
			case "input_text", "output_text", "text":
				if text, ok := partMap["text"].(string); ok && text != "" {
					textParts = append(textParts, text)
				}
			case "input_image":
				flushText()
				imagePart := buildOpenAIImageURLPart(partMap)
				if imagePart != nil {
					parts = append(parts, imagePart)
				}
			}
		}
		flushText()

		if len(parts) == 0 {
			return ""
		}
		if len(parts) == 1 {
			if partMap, ok := parts[0].(map[string]interface{}); ok && partMap["type"] == "text" {
				if text, ok := partMap["text"].(string); ok {
					return text
				}
				return ""
			}
		}
		return parts
	default:
		return ""
	}
}

func buildOpenAIImageURLPart(partMap map[string]interface{}) map[string]interface{} {
	rawImageURL, exists := partMap["image_url"]
	if !exists {
		return nil
	}

	imageURL := map[string]interface{}{}
	switch value := rawImageURL.(type) {
	case string:
		if value == "" {
			return nil
		}
		imageURL["url"] = value
	case map[string]interface{}:
		url, _ := value["url"].(string)
		if url == "" {
			return nil
		}
		imageURL["url"] = url
	default:
		return nil
	}

	if detail, ok := partMap["detail"].(string); ok && detail != "" {
		imageURL["detail"] = detail
	}

	return map[string]interface{}{
		"type":      "image_url",
		"image_url": imageURL,
	}
}
