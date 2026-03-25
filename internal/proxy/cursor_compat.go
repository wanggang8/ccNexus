package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/lich0821/ccNexus/internal/transformer/convert"
)

type proxyRequestMeta struct {
	CursorMode    bool
	OriginalPath  string
	EffectivePath string
	ClientFormat  ClientFormat
	ClientModel   string
	CursorState   *cursorCompatState
}

type cursorCompatState struct {
	InThinkingTag         bool
	ToolCallsSeen         bool
	MessagesReasoningBuf  string
	MessagesThinkingShown bool
	MessagesIndexOffset   int
}

func prepareProxyRequest(r *http.Request, body []byte) (*http.Request, []byte, proxyRequestMeta, error) {
	meta := proxyRequestMeta{
		OriginalPath:  r.URL.Path,
		EffectivePath: r.URL.Path,
	}

	if strippedPath, ok := stripCursorPrefix(r.URL.Path); ok {
		meta.CursorMode = true
		meta.EffectivePath = strippedPath
		meta.CursorState = &cursorCompatState{}
	}
	meta.ClientFormat = detectClientFormat(meta.EffectivePath)

	normalizedBody := body
	var err error
	if meta.CursorMode {
		normalizedBody, err = normalizeCursorRequestBody(meta.EffectivePath, body)
		if err != nil {
			return nil, nil, meta, err
		}
	}
	meta.ClientModel = extractModelFromPayload(normalizedBody)

	return cloneRequestWithPath(r, meta.EffectivePath), normalizedBody, meta, nil
}

func stripCursorPrefix(path string) (string, bool) {
	trimmed := strings.TrimSpace(path)
	switch {
	case trimmed == "/cursor":
		return "/", true
	case strings.HasPrefix(trimmed, "/cursor/"):
		stripped := strings.TrimPrefix(trimmed, "/cursor")
		if stripped == "" {
			return "/", true
		}
		return stripped, true
	default:
		return path, false
	}
}

func withCursorPathStripped(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		strippedPath, ok := stripCursorPrefix(r.URL.Path)
		if !ok {
			handler(w, r)
			return
		}
		handler(w, cloneRequestWithPath(r, strippedPath))
	}
}

func cloneRequestWithPath(r *http.Request, path string) *http.Request {
	cloned := r.Clone(r.Context())
	if r.URL != nil {
		copiedURL := *r.URL
		cloned.URL = &copiedURL
	} else {
		cloned.URL = &url.URL{}
	}
	cloned.URL.Path = path
	cloned.URL.RawPath = path
	cloned.RequestURI = ""
	return cloned
}

func normalizeCursorRequestBody(path string, body []byte) ([]byte, error) {
	clientFormat := detectClientFormat(path)
	model := extractModelFromPayload(body)
	normalized := body

	switch clientFormat {
	case ClientFormatOpenAIChat:
		if isResponsesLikePayload(body) && !hasMessagesPayload(body) {
			converted, err := convert.OpenAI2ReqToOpenAI(body, model)
			if err == nil {
				normalized = mergeCursorChatFields(body, converted)
			}
		}
		normalized = normalizeCursorChatRequest(normalized)
	case ClientFormatOpenAIResponses:
		if hasMessagesPayload(body) && !isResponsesLikePayload(body) {
			converted, err := convert.OpenAIReqToOpenAI2(body, model)
			if err == nil {
				normalized = converted
			}
		}
	}

	return normalized, nil
}

func mergeCursorChatFields(sourceBody, targetBody []byte) []byte {
	source, ok := decodeJSONObject(sourceBody)
	if !ok {
		return targetBody
	}
	target, ok := decodeJSONObject(targetBody)
	if !ok {
		return targetBody
	}

	if _, exists := target["tool_choice"]; !exists {
		if toolChoice, exists := source["tool_choice"]; exists {
			target["tool_choice"] = toolChoice
		}
	}
	if _, exists := target["tools"]; !exists {
		if tools, exists := source["tools"]; exists {
			target["tools"] = tools
		}
	}

	merged, err := json.Marshal(target)
	if err != nil {
		return targetBody
	}
	return merged
}

func hasMessagesPayload(body []byte) bool {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return false
	}
	messages, ok := payload["messages"].([]interface{})
	return ok && len(messages) > 0
}

func isResponsesLikePayload(body []byte) bool {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return false
	}
	_, hasInput := payload["input"]
	return hasInput
}

func extractModelFromPayload(body []byte) string {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return ""
	}
	return stringValue(payload["model"])
}

func normalizeCursorChatRequest(body []byte) []byte {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body
	}

	if messages, ok := payload["messages"].([]interface{}); ok {
		payload["messages"] = normalizeCursorChatMessages(convertCursorMessages(messages))
	}

	if tools, ok := payload["tools"].([]interface{}); ok {
		normalizedTools := make([]interface{}, 0, len(tools))
		for _, tool := range tools {
			normalizedTools = append(normalizedTools, normalizeCursorToolDefinition(tool))
		}
		payload["tools"] = normalizedTools
	}

	normalizeCursorToolChoice(payload)

	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

func normalizeCursorChatMessages(messages []interface{}) []interface{} {
	normalized := make([]interface{}, 0, len(messages))
	for _, messageValue := range messages {
		message, ok := messageValue.(map[string]interface{})
		if !ok {
			normalized = append(normalized, messageValue)
			continue
		}
		if toolCalls, ok := message["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
			fixCursorToolCalls(message, map[string]interface{}{})
		}
		normalized = append(normalized, message)
	}
	return normalized
}

func convertCursorMessages(messages []interface{}) []interface{} {
	converted := make([]interface{}, 0, len(messages))
	for _, messageValue := range messages {
		message, ok := messageValue.(map[string]interface{})
		if !ok {
			converted = append(converted, messageValue)
			continue
		}

		content, ok := message["content"].([]interface{})
		if !ok {
			converted = append(converted, message)
			continue
		}

		hasToolUse := false
		hasToolResult := false
		for _, blockValue := range content {
			block, ok := blockValue.(map[string]interface{})
			if !ok {
				continue
			}
			switch stringValue(block["type"]) {
			case "tool_use":
				hasToolUse = true
			case "tool_result":
				hasToolResult = true
			}
		}

		if !hasToolUse && !hasToolResult {
			converted = append(converted, message)
			continue
		}

		role := stringValue(message["role"])
		if role == "assistant" && hasToolUse {
			converted = append(converted, convertCursorAssistantToolUseMessage(content))
			continue
		}
		if hasToolResult {
			converted = append(converted, convertCursorToolResultMessage(role, content)...)
			continue
		}

		converted = append(converted, message)
	}

	return converted
}

func convertCursorAssistantToolUseMessage(content []interface{}) map[string]interface{} {
	textParts := make([]string, 0)
	toolCalls := make([]interface{}, 0)

	for _, blockValue := range content {
		block, ok := blockValue.(map[string]interface{})
		if !ok {
			continue
		}
		switch stringValue(block["type"]) {
		case "text":
			if text := stringValue(block["text"]); text != "" {
				textParts = append(textParts, text)
			}
		case "tool_use":
			inputJSON, _ := json.Marshal(block["input"])
			callID := stringValue(block["id"])
			if callID == "" {
				callID = newCursorToolCallID()
			}
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   callID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      stringValue(block["name"]),
					"arguments": string(inputJSON),
				},
			})
		}
	}

	message := map[string]interface{}{
		"role": "assistant",
	}
	if len(textParts) > 0 {
		message["content"] = strings.Join(textParts, "\n")
	} else {
		message["content"] = nil
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	return message
}

func convertCursorToolResultMessage(role string, content []interface{}) []interface{} {
	converted := make([]interface{}, 0)
	otherParts := make([]interface{}, 0)

	for _, blockValue := range content {
		block, ok := blockValue.(map[string]interface{})
		if !ok {
			continue
		}
		if stringValue(block["type"]) == "tool_result" {
			converted = append(converted, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": stringValue(block["tool_use_id"]),
				"content":      stringifyCursorToolResultContent(block["content"]),
			})
			continue
		}
		otherParts = append(otherParts, block)
	}

	if len(otherParts) > 0 {
		converted = append(converted, map[string]interface{}{
			"role":    role,
			"content": otherParts,
		})
	}

	return converted
}

func stringifyCursorToolResultContent(content interface{}) string {
	switch value := content.(type) {
	case string:
		return value
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, blockValue := range value {
			block, ok := blockValue.(map[string]interface{})
			if !ok {
				continue
			}
			if stringValue(block["type"]) == "text" {
				if text := stringValue(block["text"]); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprint(content)
	}
}

func normalizeCursorToolDefinition(tool interface{}) interface{} {
	toolMap, ok := tool.(map[string]interface{})
	if !ok {
		return tool
	}
	if stringValue(toolMap["type"]) == "function" {
		if _, ok := toolMap["function"].(map[string]interface{}); ok {
			return toolMap
		}
	}
	name := stringValue(toolMap["name"])
	if name == "" {
		return tool
	}

	parameters := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
	if inputSchema, ok := toolMap["input_schema"].(map[string]interface{}); ok {
		parameters = inputSchema
	} else if params, ok := toolMap["parameters"].(map[string]interface{}); ok {
		parameters = params
	}

	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": stringValue(toolMap["description"]),
			"parameters":  parameters,
		},
	}
}

func normalizeCursorToolChoice(payload map[string]interface{}) {
	toolChoice, ok := payload["tool_choice"].(map[string]interface{})
	if !ok {
		return
	}

	switch stringValue(toolChoice["type"]) {
	case "auto":
		payload["tool_choice"] = "auto"
	case "any":
		payload["tool_choice"] = "required"
	case "function":
		if fn, ok := toolChoice["function"].(map[string]interface{}); ok {
			if name := stringValue(fn["name"]); name != "" {
				payload["tool_choice"] = map[string]interface{}{
					"type": "function",
					"function": map[string]interface{}{
						"name": name,
					},
				}
				return
			}
		}
		if name := stringValue(toolChoice["name"]); name != "" {
			payload["tool_choice"] = map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": name,
				},
			}
		}
	}
}

func fixCursorResponseBody(body []byte, meta proxyRequestMeta) ([]byte, error) {
	if !meta.CursorMode {
		return body, nil
	}

	switch meta.ClientFormat {
	case ClientFormatOpenAIChat:
		return fixCursorChatResponseBody(body, meta.ClientModel)
	case ClientFormatOpenAIResponses:
		return fixCursorResponsesBody(body, meta.ClientModel)
	case ClientFormatClaude:
		return fixCursorMessagesResponseBody(body)
	default:
		return body, nil
	}
}

func fixCursorStreamBundle(bundle []byte, meta proxyRequestMeta) ([]byte, error) {
	if !meta.CursorMode {
		return bundle, nil
	}

	chunks := splitSSEBundle(bundle)
	if len(chunks) == 0 {
		return bundle, nil
	}

	var output bytes.Buffer
	for _, chunk := range chunks {
		eventName, data, ok := parseSSEChunk(chunk)
		if !ok {
			output.Write(chunk)
			if !bytes.HasSuffix(chunk, []byte("\n\n")) {
				output.WriteString("\n\n")
			}
			continue
		}

		if data == "[DONE]" {
			output.WriteString("data: [DONE]\n\n")
			continue
		}

		payload, ok := decodeJSONObject([]byte(data))
		if !ok {
			output.Write(chunk)
			if !bytes.HasSuffix(chunk, []byte("\n\n")) {
				output.WriteString("\n\n")
			}
			continue
		}

		switch meta.ClientFormat {
		case ClientFormatOpenAIChat:
			for _, item := range formatCursorChatStreamEvent(eventName, payload, meta) {
				writeSSEChunk(&output, item.eventName, item.payload)
			}
		case ClientFormatOpenAIResponses:
			outputEvent, outputPayload := formatCursorResponsesStreamEvent(eventName, payload, meta.ClientModel)
			writeSSEChunk(&output, outputEvent, outputPayload)
		case ClientFormatClaude:
			for _, item := range formatCursorMessagesStreamEvent(eventName, payload, meta) {
				writeSSEChunk(&output, item.eventName, item.payload)
			}
		default:
			writeSSEChunk(&output, eventName, payload)
		}
	}

	return output.Bytes(), nil
}

type cursorSSEItem struct {
	eventName string
	payload   map[string]interface{}
}

func formatCursorChatStreamEvent(eventName string, payload map[string]interface{}, meta proxyRequestMeta) []cursorSSEItem {
	payload = fixCursorChatChunkPayload(payload, meta.ClientModel)
	state := meta.CursorState
	if state == nil {
		return []cursorSSEItem{{eventName: eventName, payload: payload}}
	}

	choices, ok := payload["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return []cursorSSEItem{{eventName: eventName, payload: payload}}
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return []cursorSSEItem{{eventName: eventName, payload: payload}}
	}
	delta, ok := choice["delta"].(map[string]interface{})
	if !ok {
		return []cursorSSEItem{{eventName: eventName, payload: payload}}
	}

	results := make([]cursorSSEItem, 0)
	finishReason := choice["finish_reason"]
	toolCalls, hasToolCalls := delta["tool_calls"].([]interface{})
	content, hasContent := delta["content"].(string)
	reasoning, hasReasoning := delta["reasoning_content"].(string)

	if hasContent && content != "" {
		results = append(results, splitCursorChatContentIntoItems(payload, eventName, content, finishReason, hasToolCalls, state)...)
		delete(delta, "content")
	}
	if hasReasoning && reasoning != "" {
		results = append(results, cursorSSEItem{eventName: eventName, payload: cloneCursorChatChunk(payload, map[string]interface{}{
			"reasoning_content": reasoning,
		}, nil)})
		delete(delta, "reasoning_content")
	}
	if hasToolCalls && len(toolCalls) > 0 {
		if !state.ToolCallsSeen {
			state.ToolCallsSeen = true
			results = append(results, cursorSSEItem{eventName: eventName, payload: cloneCursorChatChunk(payload, map[string]interface{}{
				"content": "\n",
			}, nil)})
		}
		results = append(results, cursorSSEItem{eventName: eventName, payload: cloneCursorChatChunk(payload, map[string]interface{}{
			"tool_calls": toolCalls,
		}, finishReason)})
		delete(delta, "tool_calls")
	}

	if len(delta) > 0 || finishReason != nil && len(results) == 0 {
		results = append(results, cursorSSEItem{eventName: eventName, payload: payload})
	}
	return results
}

func splitCursorChatContentIntoItems(template map[string]interface{}, eventName, content string, finishReason interface{}, hasToolCalls bool, state *cursorCompatState) []cursorSSEItem {
	if state == nil {
		return []cursorSSEItem{{eventName: eventName, payload: cloneCursorChatChunk(template, map[string]interface{}{"content": content}, finishReason)}}
	}

	items := make([]cursorSSEItem, 0)
	appendContent := func(text string) {
		if text == "" {
			return
		}
		items = append(items, cursorSSEItem{
			eventName: eventName,
			payload:   cloneCursorChatChunk(template, map[string]interface{}{"content": text}, nil),
		})
	}
	appendReasoning := func(text string) {
		if text == "" {
			return
		}
		items = append(items, cursorSSEItem{
			eventName: eventName,
			payload:   cloneCursorChatChunk(template, map[string]interface{}{"reasoning_content": text}, nil),
		})
	}

	remaining := content
	for len(remaining) > 0 {
		if state.InThinkingTag {
			closeIdx := strings.Index(remaining, "</think>")
			if closeIdx == -1 {
				appendReasoning(remaining)
				remaining = ""
				break
			}
			appendReasoning(remaining[:closeIdx])
			remaining = strings.TrimLeft(remaining[closeIdx+len("</think>"):], "\n")
			state.InThinkingTag = false
			continue
		}

		openIdx := strings.Index(remaining, "<think>")
		if openIdx == -1 {
			appendContent(remaining)
			remaining = ""
			break
		}
		if openIdx > 0 {
			appendContent(remaining[:openIdx])
		}
		remaining = remaining[openIdx+len("<think>"):]
		closeIdx := strings.Index(remaining, "</think>")
		if closeIdx == -1 {
			state.InThinkingTag = true
			appendReasoning(remaining)
			remaining = ""
			break
		}
		appendReasoning(remaining[:closeIdx])
		remaining = strings.TrimLeft(remaining[closeIdx+len("</think>"):], "\n")
	}

	if len(items) == 0 {
		items = append(items, cursorSSEItem{
			eventName: eventName,
			payload:   cloneCursorChatChunk(template, map[string]interface{}{"content": content}, nil),
		})
	}
	if finishReason != nil && !hasToolCalls {
		last := &items[len(items)-1]
		last.payload = cloneCursorChatChunk(last.payload, extractCursorDelta(last.payload), finishReason)
	}
	return items
}

func formatCursorMessagesStreamEvent(eventName string, payload map[string]interface{}, meta proxyRequestMeta) []cursorSSEItem {
	state := meta.CursorState
	if state == nil {
		return []cursorSSEItem{{eventName: eventName, payload: payload}}
	}

	modified := cloneJSONObject(payload)
	reasoning := ""
	for _, key := range []string{"message", "delta"} {
		container, ok := modified[key].(map[string]interface{})
		if !ok {
			continue
		}
		if rc := stringValue(container["reasoning_content"]); rc != "" {
			reasoning += rc
			delete(container, "reasoning_content")
		}
		if rc := stringValue(container["reasoningContent"]); rc != "" {
			reasoning += rc
			delete(container, "reasoningContent")
		}
	}
	if reasoning != "" {
		state.MessagesReasoningBuf += reasoning
	}

	results := make([]cursorSSEItem, 0)
	if state.MessagesReasoningBuf != "" && !state.MessagesThinkingShown && isCursorMessagesTextDelta(modified) {
		state.MessagesThinkingShown = true
		state.MessagesIndexOffset = 1
		results = append(results, emitCursorMessagesThinking(state.MessagesReasoningBuf)...)
		state.MessagesReasoningBuf = ""
	}

	if state.MessagesIndexOffset > 0 {
		if index, ok := modified["index"].(float64); ok {
			modified["index"] = index + float64(state.MessagesIndexOffset)
		}
	}

	results = append(results, cursorSSEItem{eventName: eventName, payload: modified})
	return results
}

func fixCursorChatResponseBody(body []byte, clientModel string) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}

	if clientModel != "" {
		payload["model"] = clientModel
	}

	if choices, ok := payload["choices"].([]interface{}); ok {
		for _, choiceValue := range choices {
			choice, ok := choiceValue.(map[string]interface{})
			if !ok {
				continue
			}
			fixCursorChatChoice(choice)
		}
	}

	return json.Marshal(payload)
}

func fixCursorChatChunkPayload(payload map[string]interface{}, clientModel string) map[string]interface{} {
	if clientModel != "" {
		payload["model"] = clientModel
	}

	if choices, ok := payload["choices"].([]interface{}); ok {
		for _, choiceValue := range choices {
			choice, ok := choiceValue.(map[string]interface{})
			if !ok {
				continue
			}
			fixCursorChatStreamChoice(choice)
		}
	}

	return payload
}

func fixCursorChatChoice(choice map[string]interface{}) {
	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return
	}

	promoteCursorReasoningField(message)
	extractCursorThinkTags(message)
	convertLegacyCursorFunctionCall(message, choice)
	fixCursorToolCalls(message, choice)
	rewriteCursorFinishReason(choice)
}

func fixCursorChatStreamChoice(choice map[string]interface{}) {
	delta, ok := choice["delta"].(map[string]interface{})
	if !ok {
		rewriteCursorFinishReason(choice)
		return
	}

	promoteCursorReasoningField(delta)
	convertLegacyCursorStreamFunctionCall(delta, choice)
	sanitizeCursorToolCallDeltas(delta)
	ensureCursorStreamToolCalls(delta)
	rewriteCursorFinishReason(choice)
}

func promoteCursorReasoningField(container map[string]interface{}) {
	if _, ok := container["reasoning_content"]; ok {
		return
	}
	if value, ok := container["reasoningContent"]; ok {
		container["reasoning_content"] = value
		delete(container, "reasoningContent")
	}
}

func extractCursorThinkTags(message map[string]interface{}) {
	content, ok := message["content"].(string)
	if !ok || content == "" {
		return
	}
	if _, exists := message["reasoning_content"]; exists {
		return
	}

	cleaned, reasoning := extractThinkTags(content)
	if reasoning == "" {
		return
	}
	message["reasoning_content"] = reasoning
	message["content"] = cleaned
}

func convertLegacyCursorFunctionCall(message map[string]interface{}, choice map[string]interface{}) {
	if _, ok := message["tool_calls"]; ok {
		return
	}
	functionCall, ok := message["function_call"].(map[string]interface{})
	if !ok {
		return
	}

	message["tool_calls"] = []interface{}{
		map[string]interface{}{
			"id":   newCursorToolCallID(),
			"type": "function",
			"function": map[string]interface{}{
				"name":      stringValue(functionCall["name"]),
				"arguments": stringValue(functionCall["arguments"]),
			},
		},
	}
	delete(message, "function_call")
	rewriteCursorFinishReason(choice)
}

func convertLegacyCursorStreamFunctionCall(delta map[string]interface{}, choice map[string]interface{}) {
	if _, ok := delta["tool_calls"]; ok {
		return
	}
	functionCall, ok := delta["function_call"].(map[string]interface{})
	if !ok {
		return
	}

	toolCall := map[string]interface{}{
		"index":    0,
		"type":     "function",
		"function": map[string]interface{}{},
	}
	functionMap := toolCall["function"].(map[string]interface{})
	if name := stringValue(functionCall["name"]); name != "" {
		toolCall["id"] = newCursorToolCallID()
		functionMap["name"] = name
	}
	if arguments := stringValue(functionCall["arguments"]); arguments != "" {
		functionMap["arguments"] = arguments
	}

	delta["tool_calls"] = []interface{}{toolCall}
	delete(delta, "function_call")
	rewriteCursorFinishReason(choice)
}

func fixCursorToolCalls(message map[string]interface{}, choice map[string]interface{}) {
	toolCalls, ok := message["tool_calls"].([]interface{})
	if !ok || len(toolCalls) == 0 {
		return
	}

	for index, toolCallValue := range toolCalls {
		toolCall, ok := toolCallValue.(map[string]interface{})
		if !ok {
			continue
		}
		if stringValue(toolCall["id"]) == "" {
			toolCall["id"] = newCursorToolCallID()
		}
		if _, ok := toolCall["index"]; !ok {
			toolCall["index"] = index
		}
		if stringValue(toolCall["type"]) != "function" {
			toolCall["type"] = "function"
		}
		functionData, _ := toolCall["function"].(map[string]interface{})
		if functionData == nil {
			functionData = map[string]interface{}{}
			toolCall["function"] = functionData
		}
		switch arguments := functionData["arguments"].(type) {
		case map[string]interface{}, []interface{}:
			encoded, _ := json.Marshal(arguments)
			functionData["arguments"] = string(encoded)
		case nil:
			functionData["arguments"] = "{}"
		}
		normalizeCursorToolArguments(functionData)
	}

	if finishReason := stringValue(choice["finish_reason"]); finishReason != "tool_calls" {
		choice["finish_reason"] = "tool_calls"
	}
}

func sanitizeCursorToolCallDeltas(delta map[string]interface{}) {
	toolCalls, ok := delta["tool_calls"].([]interface{})
	if !ok {
		return
	}
	for _, toolCallValue := range toolCalls {
		toolCall, ok := toolCallValue.(map[string]interface{})
		if !ok {
			continue
		}
		if strings.TrimSpace(stringValue(toolCall["id"])) == "" {
			delete(toolCall, "id")
		}
		if strings.TrimSpace(stringValue(toolCall["type"])) == "" {
			delete(toolCall, "type")
		}
		functionData, ok := toolCall["function"].(map[string]interface{})
		if !ok {
			continue
		}
		if strings.TrimSpace(stringValue(functionData["name"])) == "" {
			delete(functionData, "name")
		}
	}
}

func ensureCursorStreamToolCalls(delta map[string]interface{}) {
	toolCalls, ok := delta["tool_calls"].([]interface{})
	if !ok {
		return
	}

	for _, toolCallValue := range toolCalls {
		toolCall, ok := toolCallValue.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := toolCall["index"]; !ok {
			toolCall["index"] = 0
		}

		functionData, _ := toolCall["function"].(map[string]interface{})
		hasName := functionData != nil && strings.TrimSpace(stringValue(functionData["name"])) != ""
		hasID := strings.TrimSpace(stringValue(toolCall["id"])) != ""
		if hasID || hasName {
			if !hasID {
				toolCall["id"] = newCursorToolCallID()
			}
			if strings.TrimSpace(stringValue(toolCall["type"])) == "" {
				toolCall["type"] = "function"
			}
		}
	}
}

func rewriteCursorFinishReason(choice map[string]interface{}) {
	if stringValue(choice["finish_reason"]) == "function_call" {
		choice["finish_reason"] = "tool_calls"
	}
}

func fixCursorResponsesBody(body []byte, clientModel string) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}
	if clientModel != "" {
		payload["model"] = clientModel
	}
	return json.Marshal(payload)
}

func fixCursorMessagesResponseBody(body []byte) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}
	injectCursorMessagesThinking(payload)
	return json.Marshal(payload)
}

func formatCursorResponsesStreamEvent(eventName string, payload map[string]interface{}, clientModel string) (string, map[string]interface{}) {
	if eventName == "" {
		eventName = stringValue(payload["type"])
	}
	if eventName == "" {
		return "", payload
	}

	switch eventName {
	case "response.created", "response.completed":
		if response, ok := payload["response"].(map[string]interface{}); ok {
			rewritten := cloneJSONObject(response)
			if clientModel != "" {
				rewritten["model"] = clientModel
			}
			return eventName, rewritten
		}
	case "response.output_item.added", "response.output_item.done":
		if item, ok := payload["item"].(map[string]interface{}); ok {
			return eventName, cloneJSONObject(item)
		}
	case "response.content_part.added", "response.content_part.done":
		if part, ok := payload["part"].(map[string]interface{}); ok {
			return eventName, cloneJSONObject(part)
		}
	case "response.output_text.delta":
		return eventName, map[string]interface{}{
			"type":  "output_text",
			"delta": stringValue(payload["delta"]),
		}
	case "response.output_text.done":
		return eventName, map[string]interface{}{
			"type": "output_text",
			"text": stringValue(payload["text"]),
		}
	case "response.reasoning_summary_text.delta":
		return eventName, map[string]interface{}{
			"type":  "summary_text",
			"delta": stringValue(payload["delta"]),
		}
	case "response.reasoning_summary_text.done":
		return eventName, map[string]interface{}{
			"type": "summary_text",
			"text": stringValue(payload["text"]),
		}
	case "response.function_call_arguments.delta":
		return eventName, map[string]interface{}{
			"type":  "function_call",
			"delta": stringValue(payload["delta"]),
		}
	case "response.function_call_arguments.done":
		return eventName, map[string]interface{}{
			"type":      "function_call",
			"arguments": stringValue(payload["arguments"]),
		}
	}

	rewritten := cloneJSONObject(payload)
	if clientModel != "" {
		if _, ok := rewritten["model"]; ok {
			rewritten["model"] = clientModel
		}
	}
	return eventName, rewritten
}

func splitSSEBundle(bundle []byte) [][]byte {
	parts := bytes.Split(bundle, []byte("\n\n"))
	events := make([][]byte, 0, len(parts))
	for _, part := range parts {
		trimmed := bytes.TrimSpace(part)
		if len(trimmed) == 0 {
			continue
		}
		events = append(events, append([]byte{}, trimmed...))
	}
	return events
}

func parseSSEChunk(chunk []byte) (string, string, bool) {
	lines := strings.Split(string(chunk), "\n")
	eventName := ""
	dataLines := make([]string, 0, len(lines))

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	if len(dataLines) == 0 {
		return "", "", false
	}
	return eventName, strings.Join(dataLines, "\n"), true
}

func writeSSEChunk(buffer *bytes.Buffer, eventName string, payload interface{}) {
	if buffer == nil {
		return
	}
	if eventName != "" {
		buffer.WriteString("event: ")
		buffer.WriteString(eventName)
		buffer.WriteByte('\n')
	}

	switch value := payload.(type) {
	case string:
		buffer.WriteString("data: ")
		buffer.WriteString(value)
	case map[string]interface{}:
		encoded, _ := json.Marshal(value)
		buffer.WriteString("data: ")
		buffer.Write(encoded)
	default:
		encoded, _ := json.Marshal(value)
		buffer.WriteString("data: ")
		buffer.Write(encoded)
	}
	buffer.WriteString("\n\n")
}

func decodeJSONObject(body []byte) (map[string]interface{}, bool) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func cloneJSONObject(payload map[string]interface{}) map[string]interface{} {
	if payload == nil {
		return map[string]interface{}{}
	}
	cloned := make(map[string]interface{}, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func extractThinkTags(text string) (string, string) {
	cleaned := text
	reasoningParts := make([]string, 0)

	for {
		start := strings.Index(cleaned, "<think>")
		if start < 0 {
			break
		}
		endOffset := strings.Index(cleaned[start+len("<think>"):], "</think>")
		if endOffset < 0 {
			break
		}
		end := start + len("<think>") + endOffset
		reasoningParts = append(reasoningParts, cleaned[start+len("<think>"):end])
		cleaned = cleaned[:start] + cleaned[end+len("</think>"):]
	}

	return strings.TrimSpace(cleaned), strings.TrimSpace(strings.Join(reasoningParts, "\n"))
}

func injectCursorMessagesThinking(payload map[string]interface{}) {
	reasoning := stringValue(payload["reasoning_content"])
	if reasoning == "" {
		reasoning = stringValue(payload["reasoningContent"])
	}
	if reasoning == "" {
		return
	}
	delete(payload, "reasoning_content")
	delete(payload, "reasoningContent")

	content, ok := payload["content"].([]interface{})
	if !ok {
		content = []interface{}{}
	}
	for _, blockValue := range content {
		block, ok := blockValue.(map[string]interface{})
		if ok && stringValue(block["type"]) == "thinking" {
			return
		}
	}
	payload["content"] = append([]interface{}{
		map[string]interface{}{"type": "thinking", "thinking": reasoning},
	}, content...)
}

func isCursorMessagesTextDelta(payload map[string]interface{}) bool {
	delta, ok := payload["delta"].(map[string]interface{})
	if !ok {
		return false
	}
	if stringValue(delta["type"]) != "text_delta" {
		return false
	}
	return stringValue(delta["text"]) != ""
}

func emitCursorMessagesThinking(text string) []cursorSSEItem {
	if text == "" {
		return nil
	}
	return []cursorSSEItem{
		{
			eventName: "content_block_start",
			payload: map[string]interface{}{
				"type":          "content_block_start",
				"index":         0,
				"content_block": map[string]interface{}{"type": "thinking", "thinking": ""},
			},
		},
		{
			eventName: "content_block_delta",
			payload: map[string]interface{}{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]interface{}{"type": "thinking_delta", "thinking": text},
			},
		},
		{
			eventName: "content_block_stop",
			payload: map[string]interface{}{
				"type":  "content_block_stop",
				"index": 0,
			},
		},
	}
}

func cloneCursorChatChunk(template map[string]interface{}, delta map[string]interface{}, finishReason interface{}) map[string]interface{} {
	cloned := cloneJSONObject(template)
	choices, ok := template["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return cloned
	}
	firstChoice, ok := choices[0].(map[string]interface{})
	if !ok {
		return cloned
	}

	newChoice := cloneJSONObject(firstChoice)
	newChoice["delta"] = delta
	if finishReason != nil {
		newChoice["finish_reason"] = finishReason
	} else {
		newChoice["finish_reason"] = nil
	}
	cloned["choices"] = []interface{}{newChoice}
	return cloned
}

func extractCursorDelta(payload map[string]interface{}) map[string]interface{} {
	choices, ok := payload["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return map[string]interface{}{}
	}
	firstChoice, ok := choices[0].(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	delta, ok := firstChoice["delta"].(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	return delta
}

func normalizeCursorToolArguments(functionData map[string]interface{}) {
	rawArgs := functionData["arguments"]
	if rawArgs == nil {
		return
	}

	argsStr, ok := rawArgs.(string)
	if !ok {
		return
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return
	}

	args = normalizeCursorArgs(args)
	args = repairCursorStrReplaceArgs(stringValue(functionData["name"]), args)
	encoded, err := json.Marshal(args)
	if err != nil {
		return
	}
	functionData["arguments"] = string(encoded)
}

func normalizeCursorArgs(args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return args
	}
	if _, ok := args["path"]; !ok {
		if filePath, ok := args["file_path"]; ok {
			args["path"] = filePath
			delete(args, "file_path")
		}
	}
	return args
}

var (
	cursorSmartDouble = []rune{'«', '»', '\u201c', '\u201d', '\u275e', '\u201f', '\u201e', '\u275d'}
	cursorSmartSingle = []rune{'\u2018', '\u2019', '\u201a', '\u201b'}
)

func repairCursorStrReplaceArgs(toolName string, args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return args
	}
	nameLower := strings.ToLower(strings.TrimSpace(toolName))
	if !strings.Contains(nameLower, "str_replace") && !strings.Contains(nameLower, "search_replace") {
		return args
	}

	oldValue, _ := args["old_string"].(string)
	if oldValue == "" {
		oldValue, _ = args["old_str"].(string)
	}
	if oldValue == "" {
		return args
	}

	filePath, _ := args["path"].(string)
	if filePath == "" {
		filePath, _ = args["file_path"].(string)
	}
	if filePath == "" {
		return args
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return args
	}
	contentStr := string(content)
	if strings.Contains(contentStr, oldValue) {
		return args
	}
	normalizedOld := replaceCursorSmartQuotes(oldValue)
	if normalizedOld != oldValue && strings.Contains(contentStr, normalizedOld) {
		if _, ok := args["old_string"]; ok {
			args["old_string"] = normalizedOld
		}
		if _, ok := args["old_str"]; ok {
			args["old_str"] = normalizedOld
		}
		if newValue, ok := args["new_string"].(string); ok {
			args["new_string"] = replaceCursorSmartQuotes(newValue)
		}
		if newValue, ok := args["new_str"].(string); ok {
			args["new_str"] = replaceCursorSmartQuotes(newValue)
		}
		return args
	}

	pattern, err := regexp.Compile(buildCursorFuzzyPattern(oldValue))
	if err != nil {
		return args
	}
	matches := pattern.FindAllString(contentStr, -1)
	if len(matches) != 1 {
		return args
	}

	if _, ok := args["old_string"]; ok {
		args["old_string"] = matches[0]
	}
	if _, ok := args["old_str"]; ok {
		args["old_str"] = matches[0]
	}

	if newValue, ok := args["new_string"].(string); ok {
		args["new_string"] = replaceCursorSmartQuotes(newValue)
	}
	if newValue, ok := args["new_str"].(string); ok {
		args["new_str"] = replaceCursorSmartQuotes(newValue)
	}
	return args
}

func buildCursorFuzzyPattern(text string) string {
	var builder strings.Builder
	for _, ch := range text {
		switch {
		case containsRune(cursorSmartDouble, ch) || ch == '"':
			builder.WriteString(`["\u00ab\u201c\u201d\u275e\u201f\u201e\u275d\u00bb]`)
		case containsRune(cursorSmartSingle, ch) || ch == '\'':
			builder.WriteString(`['\u2018\u2019\u201a\u201b]`)
		case ch == ' ' || ch == '\t':
			builder.WriteString(`\s+`)
		case ch == '\\':
			builder.WriteString(`\\{1,2}`)
		default:
			builder.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	return builder.String()
}

func replaceCursorSmartQuotes(text string) string {
	var builder strings.Builder
	for _, ch := range text {
		switch {
		case containsRune(cursorSmartDouble, ch):
			builder.WriteRune('"')
		case containsRune(cursorSmartSingle, ch):
			builder.WriteRune('\'')
		default:
			builder.WriteRune(ch)
		}
	}
	return builder.String()
}

func containsRune(values []rune, target rune) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func stringValue(value interface{}) string {
	stringValue, _ := value.(string)
	return stringValue
}

func newCursorToolCallID() string {
	return "call_" + uuid.NewString()
}
