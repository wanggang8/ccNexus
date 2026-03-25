package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
}

func prepareProxyRequest(r *http.Request, body []byte) (*http.Request, []byte, proxyRequestMeta, error) {
	meta := proxyRequestMeta{
		OriginalPath:  r.URL.Path,
		EffectivePath: r.URL.Path,
	}

	if strippedPath, ok := stripCursorPrefix(r.URL.Path); ok {
		meta.CursorMode = true
		meta.EffectivePath = strippedPath
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
		payload["messages"] = convertCursorMessages(messages)
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
			payload = fixCursorChatChunkPayload(payload, meta.ClientModel)
			writeSSEChunk(&output, eventName, payload)
		case ClientFormatOpenAIResponses:
			outputEvent, outputPayload := formatCursorResponsesStreamEvent(eventName, payload, meta.ClientModel)
			writeSSEChunk(&output, outputEvent, outputPayload)
		default:
			writeSSEChunk(&output, eventName, payload)
		}
	}

	return output.Bytes(), nil
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
			rewritten["type"] = eventName
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
	}

	rewritten := cloneJSONObject(payload)
	if clientModel != "" {
		rewritten["model"] = clientModel
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

func stringValue(value interface{}) string {
	stringValue, _ := value.(string)
	return stringValue
}

func newCursorToolCallID() string {
	return "call_" + uuid.NewString()
}
