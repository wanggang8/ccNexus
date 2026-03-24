package convert

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/transformer"
)

// ClaudeReqToOpenAI converts Claude request to OpenAI Chat request
func ClaudeReqToOpenAI(claudeReq []byte, model string) ([]byte, error) {
	var req transformer.ClaudeRequest
	if err := json.Unmarshal(claudeReq, &req); err != nil {
		return nil, err
	}

	// Parse raw map to detect explicit temperature (including 0)
	var rawReq map[string]interface{}
	json.Unmarshal(claudeReq, &rawReq)
	var messages []transformer.OpenAIMessage


	// Convert system prompt
	if req.System != nil {
		systemText := extractSystemText(req.System)
		if systemText != "" {
			messages = append(messages, transformer.OpenAIMessage{
				Role:    "system",
				Content: systemText,
			})
		}
	}

	// Convert messages
	for _, msg := range req.Messages {
		switch content := msg.Content.(type) {
		case string:
			messages = append(messages, transformer.OpenAIMessage{Role: msg.Role, Content: content})
		case []interface{}:
			// Check for tool_result blocks
			var textParts []string
			var imageParts []map[string]interface{}
			var toolCalls []transformer.OpenAIToolCall
			var toolResults []transformer.OpenAIMessage
			hasThinking := false

			for _, block := range content {
				m, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				switch m["type"] {
				case "text":
					if text, ok := m["text"].(string); ok {
						textParts = append(textParts, text)
					}
				case "thinking":
					// Skip thinking blocks - they are Claude's internal reasoning
					// and should not be forwarded to other APIs
					hasThinking = true
					continue
				case "image":
					// Convert Claude image format to OpenAI image_url format
					if source, ok := m["source"].(map[string]interface{}); ok {
						srcType, _ := source["type"].(string)
						switch srcType {
						case "base64":
							mediaType, _ := source["media_type"].(string)
							data, _ := source["data"].(string)
							if mediaType != "" && data != "" {
								imageParts = append(imageParts, map[string]interface{}{
									"type":      "image_url",
									"image_url": map[string]interface{}{"url": fmt.Sprintf("data:%s;base64,%s", mediaType, data)},
								})
							}
						case "url":
							if url, ok := source["url"].(string); ok && url != "" {
								imageParts = append(imageParts, map[string]interface{}{
									"type":      "image_url",
									"image_url": map[string]interface{}{"url": url},
								})
							}
						}
					}
				case "tool_use":
					args, _ := json.Marshal(m["input"])
					id, ok := m["id"].(string)
					if !ok || id == "" {
						continue
					}
					name, ok := m["name"].(string)
					if !ok || name == "" {
						continue
					}
					toolCalls = append(toolCalls, transformer.OpenAIToolCall{
						ID:   id,
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: name, Arguments: string(args)},
					})
				case "tool_result":
					callID, ok := m["tool_use_id"].(string)
					if !ok || callID == "" {
						continue
					}
					toolResults = append(toolResults, transformer.OpenAIMessage{
						Role:       "tool",
						Content:    extractToolResultContent(m["content"]),
						ToolCallID: callID,
					})
				}
			}

			// Add main message if has text, images, or tool_calls
			if len(textParts) > 0 || len(imageParts) > 0 || len(toolCalls) > 0 {
				openaiMsg := transformer.OpenAIMessage{Role: msg.Role}
				if len(imageParts) > 0 {
					// Use array content format when images are present
					var parts []map[string]interface{}
					if len(textParts) > 0 {
						parts = append(parts, map[string]interface{}{"type": "text", "text": joinTextParts(textParts)})
					}
					parts = append(parts, imageParts...)
					openaiMsg.Content = parts
				} else if len(textParts) > 0 {
					openaiMsg.Content = joinTextParts(textParts)
				}
				if len(toolCalls) > 0 {
					openaiMsg.ToolCalls = toolCalls
				}
				messages = append(messages, openaiMsg)
			} else if hasThinking && msg.Role == "assistant" {
				messages = append(messages, transformer.OpenAIMessage{
					Role:    "assistant",
					Content: placeholderForThinkingOnly(),
				})
			}

			// Add tool result messages
			messages = append(messages, toolResults...)
		}
	}

	openaiReq := transformer.OpenAIRequest{
		Model:    model,
		Messages: messages,
		Stream:   req.Stream,
	}

	if req.MaxTokens > 0 {
		openaiReq.MaxCompletionTokens = req.MaxTokens
	}

	// Convert tools
	if len(req.Tools) > 0 {
		for _, tool := range req.Tools {
			openaiTool := transformer.OpenAITool{Type: "function"}
			openaiTool.Function.Name = tool.Name
			openaiTool.Function.Description = tool.Description
			openaiTool.Function.Parameters = tool.InputSchema
			openaiTool.Function.Strict = tool.Strict
			openaiReq.Tools = append(openaiReq.Tools, openaiTool)
		}
		// Convert tool_choice
		if req.ToolChoice != nil {
			switch tc := req.ToolChoice.(type) {
			case map[string]interface{}:
				if choiceType, _ := tc["type"].(string); choiceType == "tool" {
					if name, ok := tc["name"].(string); ok {
						openaiReq.ToolChoice = map[string]interface{}{"type": "function", "function": map[string]string{"name": name}}
					}
				} else if choiceType == "any" {
					openaiReq.ToolChoice = "required"
				} else if choiceType == "auto" {
					openaiReq.ToolChoice = "auto"
				}
			case string:
				openaiReq.ToolChoice = tc
			}
		} else {
			openaiReq.ToolChoice = "auto"
		}
	}

	// Enable usage tracking for streaming
	if req.Stream {
		if _, ok := rawReq["stream_options"]; ok {
			openaiReq.StreamOptions = &transformer.StreamOptions{IncludeUsage: true}
		}
	}

	return json.Marshal(openaiReq)
}

// OpenAIReqToClaude converts OpenAI Chat request to Claude request
func OpenAIReqToClaude(openaiReq []byte, model string) ([]byte, error) {
	// Parse as generic map first to handle both OpenAI and Claude tool formats
	var reqMap map[string]interface{}
	if err := json.Unmarshal(openaiReq, &reqMap); err != nil {
		return nil, err
	}
	var req transformer.OpenAIRequest
	if err := json.Unmarshal(openaiReq, &req); err != nil {
		return nil, err
	}

	claudeReq := map[string]interface{}{
		"model": model,
	}
	if stream, ok := reqMap["stream"]; ok {
		claudeReq["stream"] = stream
	}
	claudeMaxTokens := 0

	if stream, ok := reqMap["stream"].(bool); ok {
		claudeReq["stream"] = stream
	}
	if maxTokensValue, ok := reqMap["max_tokens"].(float64); ok && maxTokensValue > 0 {
		maxTokensInt := int(maxTokensValue)
		claudeReq["max_tokens"] = maxTokensInt
		claudeMaxTokens = maxTokensInt
	} else if maxComp, ok := reqMap["max_completion_tokens"].(float64); ok && maxComp > 0 {
		maxTokensInt := int(maxComp)
		claudeReq["max_tokens"] = maxTokensInt
		claudeMaxTokens = maxTokensInt
	}

	if temp, ok := reqMap["temperature"].(float64); ok {
		claudeReq["temperature"] = temp
	}
	if thinking := buildClaudeThinkingConfig(req.Thinking, req.EnableThinking, claudeMaxTokens); thinking != nil {
		claudeReq["thinking"] = thinking
	}

	// Convert messages
	var systemPrompt string
	var messages []map[string]interface{}

	if reqMessages, ok := reqMap["messages"].([]interface{}); ok {
		idx := 0
		for idx < len(reqMessages) {
			msgInterface := reqMessages[idx]
			msg, ok := msgInterface.(map[string]interface{})
			if !ok {
				idx++
				continue
			}

			role, _ := msg["role"].(string)
			if role == "system" {
				if content, ok := msg["content"].(string); ok {
					systemPrompt += content + "\n"
				}
				idx++
				continue
			}

			// 合并连续的 tool 消息（并行工具调用的结果）到同一 user 消息中
			// Claude API 要求：多个并行工具调用的结果必须在同一条 user 消息内
			if role == "tool" {
				var toolResults []map[string]interface{}
				for idx < len(reqMessages) {
					tm, ok := reqMessages[idx].(map[string]interface{})
					if !ok {
						break
					}
					if r, _ := tm["role"].(string); r != "tool" {
						break
					}
					toolCallID, _ := tm["tool_call_id"].(string)
					toolResults = append(toolResults, map[string]interface{}{
						"type": "tool_result", "tool_use_id": toolCallID, "content": normalizeToolResultContent(tm["content"]),
					})
					idx++
				}
				messages = append(messages, map[string]interface{}{
					"role":    "user",
					"content": toolResults,
				})
				continue
			}

			claudeMsg := map[string]interface{}{"role": role}

			// Handle content
			if content, ok := msg["content"]; ok {
				switch c := content.(type) {
				case string:
					claudeMsg["content"] = c
				case []interface{}:
					claudeMsg["content"] = convertOpenAIContentToClaude(c)
				}
			}

			// Handle tool_calls
			if toolCalls, ok := msg["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
				var blocks []map[string]interface{}
				switch existing := claudeMsg["content"].(type) {
				case string:
					if existing != "" {
						blocks = append(blocks, map[string]interface{}{"type": "text", "text": existing})
					}
				case []map[string]interface{}:
					blocks = append(blocks, existing...)
				}
				for _, tcInterface := range toolCalls {
					tc, ok := tcInterface.(map[string]interface{})
					if !ok {
						continue
					}
					funcObj, _ := tc["function"].(map[string]interface{})
					argsStr, _ := funcObj["arguments"].(string)
					args := parseJSONObjectArguments(argsStr, "Failed to unmarshal tool arguments")
					blocks = append(blocks, map[string]interface{}{
						"type":  "tool_use",
						"id":    tc["id"],
						"name":  funcObj["name"],
						"input": args,
					})
				}
				claudeMsg["content"] = blocks
			}

			messages = append(messages, claudeMsg)
			idx++
		}
	}

	if systemPrompt != "" {
		claudeReq["system"] = strings.TrimSpace(systemPrompt)
	}
	claudeReq["messages"] = messages

	// Convert tools - handle both OpenAI format and Claude format (from Cursor)
	if reqTools, ok := reqMap["tools"].([]interface{}); ok && len(reqTools) > 0 {
		var tools []map[string]interface{}

		for _, toolInterface := range reqTools {
			rawTool, ok := toolInterface.(map[string]interface{})
			if !ok {
				continue
			}

			var claudeTool map[string]interface{}

			// Check if it's already in Claude format (has "name" at top level)
			if name, hasName := rawTool["name"].(string); hasName && name != "" {
				// Claude format: {name, description, input_schema}
				claudeTool = map[string]interface{}{
					"name":         rawTool["name"],
					"description":  rawTool["description"],
					"input_schema": rawTool["input_schema"],
				}
			} else if rawTool["type"] == "function" {
				// OpenAI format: {type: "function", function: {name, description, parameters}}
				if funcObj, ok := rawTool["function"].(map[string]interface{}); ok {
					claudeTool = map[string]interface{}{
						"name":         funcObj["name"],
						"description":  funcObj["description"],
						"input_schema": funcObj["parameters"],
					}
				}
			}

			if claudeTool != nil {
				tools = append(tools, claudeTool)
			}
		}

		if len(tools) > 0 {
			claudeReq["tools"] = tools

			// D5: Convert OpenAI tool_choice to Claude format
			if tc, ok := reqMap["tool_choice"]; ok && tc != nil {
				switch v := tc.(type) {
				case string:
					switch v {
					case "required":
						claudeReq["tool_choice"] = map[string]interface{}{"type": "any"}
					case "auto":
						claudeReq["tool_choice"] = map[string]interface{}{"type": "auto"}
					case "none":
						delete(claudeReq, "tools")
					}
				case map[string]interface{}:
					if v["type"] == "function" {
						if fn, ok := v["function"].(map[string]interface{}); ok {
							if name, ok := fn["name"].(string); ok && name != "" {
								claudeReq["tool_choice"] = map[string]interface{}{"type": "tool", "name": name}
							}
						}
					}
				}
			}
		}
	}

	return json.Marshal(claudeReq)
}

// ClaudeRespToOpenAI converts Claude response to OpenAI Chat response
func ClaudeRespToOpenAI(claudeResp []byte, model string) ([]byte, error) {
	var resp transformer.ClaudeResponse
	if err := json.Unmarshal(claudeResp, &resp); err != nil {
		return nil, err
	}

	var textContent string
	var toolCalls []map[string]interface{}

	for _, block := range resp.Content {
		blockMap, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		switch blockMap["type"] {
		case "text":
			if text, ok := blockMap["text"].(string); ok {
				textContent += text
			} else {
				logger.Warn("Invalid text content type: %T", blockMap["text"])
			}
		case "thinking":
			// Skip thinking blocks in response
			continue
		case "tool_use":
			args, _ := json.Marshal(blockMap["input"])
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   blockMap["id"],
				"type": "function",
				"function": map[string]interface{}{
					"name":      blockMap["name"],
					"arguments": string(args),
				},
			})
		}
	}

	message := map[string]interface{}{"role": "assistant", "content": textContent}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	finishReason := mapClaudeStopToOpenAIFinish(resp.StopReason)

	openaiResp := map[string]interface{}{
		"id":      resp.ID,
		"object":  "chat.completion",
		"model":   model,
		"choices": []map[string]interface{}{{"index": 0, "message": message, "finish_reason": finishReason}},
		"usage": map[string]interface{}{
			"prompt_tokens":     resp.Usage.InputTokens,
			"completion_tokens": resp.Usage.OutputTokens,
			"total_tokens":      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}

	return json.Marshal(openaiResp)
}

// OpenAIRespToClaude converts OpenAI Chat response to Claude response
func OpenAIRespToClaude(openaiResp []byte) ([]byte, error) {
	var resp transformer.OpenAIResponse
	if err := json.Unmarshal(openaiResp, &resp); err != nil {
		return nil, err
	}

	// T3: Parse raw JSON to extract reasoning_content (not in typed struct)
	var rawResp map[string]interface{}
	json.Unmarshal(openaiResp, &rawResp)

	content := make([]map[string]interface{}, 0) // Initialize as empty array, not nil
	stopReason := "end_turn"

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		if choice.FinishReason != "" {
			stopReason = mapOpenAIFinishToClaudeStop(choice.FinishReason)
		}

		// T3: Extract reasoning_content as thinking block
		if rawChoices, ok := rawResp["choices"].([]interface{}); ok && len(rawChoices) > 0 {
			if rawChoice, ok := rawChoices[0].(map[string]interface{}); ok {
				if rawMsg, ok := rawChoice["message"].(map[string]interface{}); ok {
					if reasoning, ok := rawMsg["reasoning_content"].(string); ok && reasoning != "" {
						content = append(content, map[string]interface{}{
							"type":     "thinking",
							"thinking": reasoning,
						})
					}
				}
			}
		}

		if choice.Message.Content != "" {
			content = append(content, splitThinkTaggedText(choice.Message.Content)...)
		}
		for _, tc := range choice.Message.ToolCalls {
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				logger.Warn("Failed to unmarshal tool arguments: %v, using empty object", err)
				args = map[string]interface{}{}
			}
			content = append(content, map[string]interface{}{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Function.Name,
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
		"model":       resp.Model,
		"stop_reason": stopReason,
		"usage": map[string]interface{}{
			"input_tokens":  resp.Usage.PromptTokens,
			"output_tokens": resp.Usage.CompletionTokens,
		},
	}

	return json.Marshal(claudeResp)
}

// ClaudeStreamToOpenAI converts Claude SSE event to OpenAI Chat stream chunk
func ClaudeStreamToOpenAI(event []byte, ctx *transformer.StreamContext, model string) ([]byte, error) {
	eventType, jsonData := ParseSSE(event)
	if jsonData == "" {
		return nil, nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		logger.Debug("ClaudeStreamToOpenAI: failed to parse event: %v", err)
		return nil, nil
	}

	// Fallback: some APIs return type in JSON payload without event: line
	if eventType == "" {
		if t, ok := data["type"].(string); ok {
			eventType = t
		}
	}

	switch eventType {
	case "message_start":
		if msg, ok := data["message"].(map[string]interface{}); ok {
			ctx.MessageID, _ = msg["id"].(string)
			if usage, ok := msg["usage"].(map[string]interface{}); ok {
				if input, ok := usage["input_tokens"].(float64); ok && int(input) > 0 {
					ctx.InputTokens = int(input)
				}
			}
		}
		// Send initial role chunk per OpenAI streaming spec
		chunk := map[string]interface{}{
			"id": ctx.MessageID, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
			"choices": []map[string]interface{}{{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": ""}, "finish_reason": nil}},
		}
		d, _ := json.Marshal(chunk)
		return []byte(fmt.Sprintf("data: %s\n\n", d)), nil

	case "content_block_start":
		if block, ok := data["content_block"].(map[string]interface{}); ok {
			switch block["type"] {
			case "tool_use":
				ctx.ToolBlockStarted = true
				ctx.ToolBlockPending = true
				ctx.CurrentToolID, _ = block["id"].(string)
				ctx.CurrentToolName, _ = block["name"].(string)
				return nil, nil
			case "thinking":
				ctx.ThinkingBlockStarted = true
			}
		}
		return nil, nil

	case "content_block_delta":
		delta, ok := data["delta"].(map[string]interface{})
		if !ok {
			return nil, nil
		}
		switch delta["type"] {
		case "text_delta":
			text, _ := delta["text"].(string)
			return buildOpenAIChunk(ctx.MessageID, model, text, nil, "")
		case "thinking_delta":
			thinking, _ := delta["thinking"].(string)
			if thinking != "" {
				return buildOpenAIChunkWithReasoning(ctx.MessageID, model, thinking)
			}
		case "input_json_delta":
			partial, _ := delta["partial_json"].(string)
			ctx.ToolArguments += partial
			// Send tool call shell on first arguments delta, then stream incremental arguments.
			if ctx.ToolBlockPending {
				chunk, err := buildOpenAIChunk(ctx.MessageID, model, "", []map[string]interface{}{
					{"index": ctx.ToolCallCounter, "id": ctx.CurrentToolID, "type": "function",
						"function": map[string]interface{}{"name": ctx.CurrentToolName, "arguments": ""}},
				}, "")
				if err != nil {
					return nil, err
				}
				deltaChunk, err := buildOpenAIChunk(ctx.MessageID, model, "", []map[string]interface{}{
					{"index": ctx.ToolCallCounter, "function": map[string]interface{}{"arguments": partial}},
				}, "")
				if err != nil {
					return nil, err
				}
				ctx.ToolBlockPending = false
				return append(chunk, deltaChunk...), nil
			}
			// Send incremental arguments delta
			return buildOpenAIChunk(ctx.MessageID, model, "", []map[string]interface{}{
				{"index": ctx.ToolCallCounter, "function": map[string]interface{}{"arguments": partial}},
			}, "")
		}
		return nil, nil

	case "content_block_stop":
		if ctx.ThinkingBlockStarted {
			ctx.ThinkingBlockStarted = false
		}
		if ctx.ToolBlockStarted {
			ctx.ToolBlockStarted = false
			ctx.ToolBlockPending = false
			ctx.ToolArguments = ""
			ctx.ToolCallCounter++
		}
		return nil, nil

	case "message_delta":
		var result []byte

		if delta, ok := data["delta"].(map[string]interface{}); ok {
			if stopReason, ok := delta["stop_reason"].(string); ok && stopReason != "" {
				finish := mapClaudeStopToOpenAIFinish(stopReason)
				finishChunk, err := buildOpenAIChunk(ctx.MessageID, model, "", nil, finish)
				if err != nil {
					return nil, err
				}
				result = append(result, finishChunk...)
			}
		}

		if ctx != nil && ctx.IncludeUsage {
			if usage, ok := data["usage"].(map[string]interface{}); ok {
				promptTokens := ctx.InputTokens
				completionTokens := ctx.OutputTokens
				if input, ok := usage["input_tokens"].(float64); ok && int(input) > 0 {
					promptTokens = int(input)
					ctx.InputTokens = int(input)
				}
				if output, ok := usage["output_tokens"].(float64); ok && int(output) >= 0 {
					completionTokens = int(output)
					ctx.OutputTokens = int(output)
				}
				usageChunk, err := buildOpenAIUsageChunk(ctx.MessageID, model, promptTokens, completionTokens)
				if err != nil {
					return nil, err
				}
				result = append(result, usageChunk...)
			}
		}

		if len(result) > 0 {
			return result, nil
		}
		return nil, nil

	case "message_stop":
		return []byte("data: [DONE]\n\n"), nil
	}

	return nil, nil
}

// OpenAIStreamToClaude converts OpenAI Chat stream chunk to Claude SSE event
func OpenAIStreamToClaude(event []byte, ctx *transformer.StreamContext) ([]byte, error) {
	_, jsonData := ParseSSE(event)
	if jsonData == "" || jsonData == "[DONE]" {
		if jsonData == "[DONE]" {
			var result []byte
			emitText, emitThinking := makeThinkEmitters(ctx, &result)
			flushThinkTaggedStream(ctx, emitText, emitThinking)
			// Close any open content blocks before message_stop
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
			// Send message_delta with stop_reason if not sent
			if !ctx.FinishReasonSent {
				result = append(result, buildClaudeEvent("message_delta", map[string]interface{}{
					"delta": map[string]interface{}{"stop_reason": "end_turn", "stop_sequence": nil},
					"usage": map[string]interface{}{"output_tokens": 0},
				})...)
			}
			result = append(result, buildClaudeEvent("message_stop", map[string]interface{}{})...)
			return result, nil
		}
		return nil, nil
	}

	var chunk transformer.OpenAIStreamChunk
	if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
		logger.Debug("OpenAIStreamToClaude: failed to parse chunk: %v", err)
		return nil, nil
	}

	var result []byte

	// message_start
	if !ctx.MessageStartSent {
		ctx.MessageStartSent = true
		ctx.MessageID = chunk.ID
		result = append(result, buildClaudeEvent("message_start", map[string]interface{}{
			"message": map[string]interface{}{
				"id": chunk.ID, "type": "message", "role": "assistant",
				"content": []interface{}{}, "model": ctx.ModelName,
				"stop_reason": nil, "stop_sequence": nil,
				"usage": map[string]interface{}{"input_tokens": 0, "output_tokens": 0},
			},
		})...)
	}

	if len(chunk.Choices) == 0 {
		if chunk.Usage != nil {
			usageObj := map[string]interface{}{
				"input_tokens":  chunk.Usage.PromptTokens,
				"output_tokens": chunk.Usage.CompletionTokens,
			}
			msgDelta := map[string]interface{}{
				"delta": map[string]interface{}{},
				"usage": usageObj,
			}
			result = append(result, buildClaudeEvent("message_delta", msgDelta)...)
		}
		// Add message_stop event to complete the stream
		result = append(result, buildClaudeEvent("message_stop", map[string]interface{}{})...)
		return result, nil
	}

	choice := chunk.Choices[0]
	delta := choice.Delta
	if chunk.Usage != nil && delta.Role == "" && delta.Content == "" && delta.ReasoningContent == "" && len(delta.ToolCalls) == 0 && choice.FinishReason == nil {
		usageObj := map[string]interface{}{
			"input_tokens":  chunk.Usage.PromptTokens,
			"output_tokens": chunk.Usage.CompletionTokens,
		}
		msgDelta := map[string]interface{}{
			"delta": map[string]interface{}{},
			"usage": usageObj,
		}
		result = append(result, buildClaudeEvent("message_delta", msgDelta)...)
		return result, nil
	}

	// Reasoning/Thinking content (before text content)
	if delta.ReasoningContent != "" {
		if !ctx.ThinkingBlockStarted {
			ctx.ThinkingBlockStarted = true
			ctx.ThinkingIndex = ctx.ContentIndex
			ctx.ContentIndex++
			result = append(result, buildClaudeEvent("content_block_start", map[string]interface{}{
				"index": ctx.ThinkingIndex, "content_block": map[string]interface{}{"type": "thinking", "thinking": ""},
			})...)
		}
		result = append(result, buildClaudeEvent("content_block_delta", map[string]interface{}{
			"index": ctx.ThinkingIndex, "delta": map[string]interface{}{"type": "thinking_delta", "thinking": delta.ReasoningContent},
		})...)
	}

	// Text content
	if delta.Content != "" {
		content := ctx.ThinkingBuffer + delta.Content
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
	}

	// Tool calls
	for _, tc := range delta.ToolCalls {
		// New tool call (has ID)
		if tc.ID != "" {
			// Close thinking block if open
			if ctx.ThinkingBlockStarted {
				result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ThinkingIndex})...)
				ctx.ThinkingBlockStarted = false
			}
			// Close text block if open
			if ctx.ContentBlockStarted {
				result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ContentIndex})...)
				ctx.ContentBlockStarted = false
				ctx.ContentIndex++
			}
			// Close previous tool block if open
			if ctx.ToolBlockStarted {
				result = append(result, buildClaudeEvent("content_block_stop", map[string]interface{}{"index": ctx.ToolIndex})...)
				ctx.ToolBlockStarted = false
			}
			ctx.ToolBlockStarted = true
			ctx.ToolIndex = ctx.ContentIndex
			ctx.ContentIndex++
			ctx.CurrentToolID = tc.ID
			ctx.CurrentToolName = tc.Function.Name
			ctx.ToolArguments = ""
			result = append(result, buildClaudeEvent("content_block_start", map[string]interface{}{
				"index": ctx.ToolIndex, "content_block": map[string]interface{}{"type": "tool_use", "id": tc.ID, "name": tc.Function.Name, "input": map[string]interface{}{}},
			})...)
		}
		// Accumulate arguments
		if tc.Function.Arguments != "" {
			ctx.ToolArguments += tc.Function.Arguments
			result = append(result, buildClaudeEvent("content_block_delta", map[string]interface{}{
				"index": ctx.ToolIndex, "delta": map[string]interface{}{"type": "input_json_delta", "partial_json": tc.Function.Arguments},
			})...)
		}
	}

	// Finish
	if choice.FinishReason != nil {
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
		stopReason := mapOpenAIFinishToClaudeStop(*choice.FinishReason)
		result = append(result, buildClaudeEvent("message_delta", map[string]interface{}{
			"delta": map[string]interface{}{"stop_reason": stopReason, "stop_sequence": nil},
			"usage": map[string]interface{}{"output_tokens": 0},
		})...)
		ctx.FinishReasonSent = true
	}

	return result, nil
}

// Helper functions

func convertClaudeContentToOpenAI(content []interface{}) (interface{}, []transformer.OpenAIToolCall) {
	var textParts []string
	var toolCalls []transformer.OpenAIToolCall

	for _, block := range content {
		m, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		switch m["type"] {
		case "text":
			if text, ok := m["text"].(string); ok {
				textParts = append(textParts, text)
			}
		case "thinking":
			// Skip thinking blocks
			continue
		case "tool_use":
			id, okID := m["id"].(string)
			name, okName := m["name"].(string)
			if !okID || !okName {
				continue
			}
			args, _ := json.Marshal(m["input"])
			toolCalls = append(toolCalls, transformer.OpenAIToolCall{
				ID:   id,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: name, Arguments: string(args)},
			})
		}
	}

	if len(textParts) > 0 {
		return strings.Join(textParts, ""), toolCalls
	}
	return "", toolCalls
}

func convertOpenAIContentToClaude(content []interface{}) []map[string]interface{} {
	var result []map[string]interface{}
	for _, item := range content {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		switch m["type"] {
		case "text":
			result = append(result, map[string]interface{}{"type": "text", "text": m["text"]})
		case "image_url":
			if urlObj, ok := m["image_url"].(map[string]interface{}); ok {
				if url, ok := urlObj["url"].(string); ok {
					if strings.HasPrefix(url, "data:") {
						parts := strings.SplitN(url, ",", 2)
						if len(parts) == 2 {
							mediaType := strings.TrimPrefix(strings.Split(parts[0], ";")[0], "data:")
							result = append(result, map[string]interface{}{
								"type":   "image",
								"source": map[string]interface{}{"type": "base64", "media_type": mediaType, "data": parts[1]},
							})
						}
					} else if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
						result = append(result, map[string]interface{}{
							"type":   "image",
							"source": map[string]interface{}{"type": "url", "url": url},
						})
					}
				}
			}
		case "tool_result":
			// Cursor may send Claude-format tool_result directly
			// Keep it as-is since it's already in Claude format
			result = append(result, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": m["tool_use_id"],
				"content":     m["content"],
			})
		}
	}
	return result
}

func extractToolResultContent(content interface{}) string {
	if content == nil {
		return ""
	}
	if str, ok := content.(string); ok {
		return str
	}
	if arr, ok := content.([]interface{}); ok {
		var parts []string
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				if m["type"] == "text" {
					if text, ok := m["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func normalizeToolResultContent(content interface{}) interface{} {
	if content == nil {
		return ""
	}
	if str, ok := content.(string); ok {
		return str
	}
	if arr, ok := content.([]interface{}); ok {
		return convertOpenAIContentToClaude(arr)
	}
	return extractToolResultContent(content)
}

// mapClaudeStopToOpenAIFinish maps Claude stop_reason to OpenAI finish_reason.
func mapClaudeStopToOpenAIFinish(stopReason string) string {
	switch stopReason {
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	case "end_turn", "stop_sequence":
		return "stop"
	default:
		return "stop"
	}
}

// mapOpenAIFinishToClaudeStop maps OpenAI finish_reason to Claude stop_reason.
func mapOpenAIFinishToClaudeStop(finishReason string) string {
	switch finishReason {
	case "tool_calls", "function_call":
		return "tool_use"
	case "length":
		return "max_tokens"
	case "content_filter":
		return "end_turn"
	case "stop":
		return "end_turn"
	default:
		return "end_turn"
	}
}
