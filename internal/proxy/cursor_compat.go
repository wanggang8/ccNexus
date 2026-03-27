package proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/lich0821/ccNexus/internal/transformer/convert"
)

type proxyRequestMeta struct {
	CursorMode        bool
	OriginalPath      string
	EffectivePath     string
	ClientFormat      ClientFormat
	ClientModel       string
	CursorState       *cursorCompatState
	CacheMessages     []map[string]interface{}
	TransformerName   string
	Degraded          bool
	DegradedReason    []string
	CursorRequestBody []byte
}

type cursorCompatState struct {
	InThinkingTag         bool
	ToolCallsSeen         bool
	MessagesReasoningBuf  string
	MessagesThinkingShown bool
	MessagesIndexOffset   int
	ResponsesResponseID   string
	ResponsesReasoningID  string
	ResponsesReasoningBuf string
	ResponsesReasoningOn  bool
	ResponsesMessageID    string
	ResponsesMessageText  string
	ResponsesMessageOn    bool
	ResponsesTools        map[int]*cursorResponsesToolState
	ResponsesOutput       []map[string]interface{}
}

type cursorResponsesToolState struct {
	ID        string
	CallID    string
	Name      string
	Arguments string
	Active    bool
}

type cursorThinkingCache struct {
	store map[string]cursorThinkingCacheEntry
}

type cursorThinkingCacheEntry struct {
	Reasoning string
	StoredAt  time.Time
}

const cursorThinkingCacheTTL = 24 * time.Hour

var globalCursorThinkingCache = &cursorThinkingCache{
	store: make(map[string]cursorThinkingCacheEntry),
}

func prepareProxyRequest(r *http.Request, body []byte) (*http.Request, []byte, proxyRequestMeta, error) {
	meta := proxyRequestMeta{
		OriginalPath:  r.URL.Path,
		EffectivePath: r.URL.Path,
	}

	if strippedPath, ok := stripCursorPrefix(r.URL.Path); ok {
		meta.CursorMode = true
		meta.EffectivePath = strippedPath
		meta.CursorState = &cursorCompatState{
			ResponsesTools:  make(map[int]*cursorResponsesToolState),
			ResponsesOutput: make([]map[string]interface{}, 0),
		}
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
	if meta.CursorMode {
		meta.CursorRequestBody = normalizedBody
	}
	meta.ClientModel = extractModelFromPayload(normalizedBody)
	if meta.CursorMode && meta.ClientFormat == ClientFormatOpenAIChat {
		meta.CacheMessages = extractCursorCacheMessages(normalizedBody)
		if len(meta.CacheMessages) > 0 {
			meta.CacheMessages = globalCursorThinkingCache.Inject(meta.CacheMessages)
			if rewrittenBody, err := rewriteCursorChatMessages(normalizedBody, meta.CacheMessages); err == nil {
				normalizedBody = rewrittenBody
			}
		}
	} else if meta.CursorMode && meta.ClientFormat == ClientFormatOpenAIResponses {
		meta.CacheMessages = extractCursorResponsesCacheMessages(normalizedBody, meta.ClientModel)
	}

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
			inputData, _ := block["input"].(map[string]interface{})
			inputData = applyCursorToolArgFixes(stringValue(block["name"]), inputData)
			inputJSON, _ := json.Marshal(inputData)
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
	compact := compactCursorSchemaSignature(parameters)

	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": stringValue(toolMap["description"]),
			"parameters":  compact,
		},
	}
}

func compactCursorSchemaSignature(schema map[string]interface{}) string {
	if schema == nil {
		return "{}"
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok || len(props) == 0 {
		return "{}"
	}
	requiredSet := map[string]bool{}
	if required, ok := schema["required"].([]interface{}); ok {
		for _, value := range required {
			if name, ok := value.(string); ok {
				requiredSet[name] = true
			}
		}
	}
	parts := make([]string, 0, len(props))
	for name, raw := range props {
		prop, _ := raw.(map[string]interface{})
		typeStr := "any"
		if propType, ok := prop["type"].(string); ok && propType != "" {
			typeStr = propType
		}
		if enumValues, ok := prop["enum"].([]interface{}); ok && len(enumValues) > 0 {
			values := make([]string, 0, len(enumValues))
			for _, v := range enumValues {
				values = append(values, fmt.Sprint(v))
			}
			typeStr = strings.Join(values, "|")
		}
		if typeStr == "array" {
			if items, ok := prop["items"].(map[string]interface{}); ok {
				if itemType, ok := items["type"].(string); ok && itemType != "" {
					typeStr = itemType + "[]"
				}
			}
		}
		if typeStr == "object" {
			if subProps, ok := prop["properties"].(map[string]interface{}); ok && len(subProps) > 0 {
				typeStr = compactCursorSchemaSignature(prop)
			}
		}
		marker := "?"
		if requiredSet[name] {
			marker = "!"
		}
		parts = append(parts, fmt.Sprintf("%s%s: %s", name, marker, typeStr))
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, ", ") + "}"
}

func isCursorResponseTruncated(text string) bool {
	trimmed := strings.TrimRightFunc(text, unicode.IsSpace)
	if trimmed == "" {
		return false
	}
	jsonActionOpens := len(regexp.MustCompile("```json\\s+action").FindAllStringIndex(trimmed, -1))
	if jsonActionOpens > 0 {
		jsonActionBlocks := regexp.MustCompile("```json\\s+action[\\s\\S]*?```").FindAllStringIndex(trimmed, -1)
		if jsonActionOpens > len(jsonActionBlocks) {
			return true
		}
		return false
	}
	lineStartCodeBlocks := len(regexp.MustCompile("(?m)^```").FindAllStringIndex(trimmed, -1))
	if lineStartCodeBlocks%2 != 0 {
		return true
	}
	openTags := len(regexp.MustCompile("(?m)^<[^/][a-zA-Z]").FindAllStringIndex(trimmed, -1))
	closeTags := len(regexp.MustCompile("(?m)^</[a-zA-Z]").FindAllStringIndex(trimmed, -1))
	if openTags > closeTags+1 {
		return true
	}
	if regexp.MustCompile(`[,;:\[{(]\s*$`).MatchString(trimmed) {
		return true
	}
	if len(trimmed) > 2000 && regexp.MustCompile(`\\n?\s*$`).MatchString(trimmed) && !strings.HasSuffix(trimmed, "```") {
		return true
	}
	if len(trimmed) < 500 && regexp.MustCompile(`[a-z]$`).MatchString(trimmed) {
		return false
	}
	return false
}

func hasCursorToolCalls(text string) bool {
	return strings.Contains(text, "```json")
}

type cursorParsedToolCall struct {
	name      string
	arguments map[string]interface{}
}

func parseCursorToolCalls(responseText string) []cursorParsedToolCall {
	toolCalls := make([]cursorParsedToolCall, 0)
	openPattern := regexp.MustCompile("```json(?:\\s+action)?")
	indices := openPattern.FindAllStringIndex(responseText, -1)
	for _, match := range indices {
		contentStart := match[1]
		pos := contentStart
		inJSON := false
		closingPos := -1
		for pos < len(responseText)-2 {
			char := responseText[pos]
			if char == '"' {
				backslashCount := 0
				for j := pos - 1; j >= contentStart && responseText[j] == '\\'; j-- {
					backslashCount++
				}
				if backslashCount%2 == 0 {
					inJSON = !inJSON
				}
				pos++
				continue
			}
			if !inJSON && pos+3 <= len(responseText) && responseText[pos:pos+3] == "```" {
				closingPos = pos
				break
			}
			pos++
		}
		jsonContent := ""
		if closingPos >= 0 {
			jsonContent = strings.TrimSpace(responseText[contentStart:closingPos])
		} else {
			jsonContent = strings.TrimSpace(responseText[contentStart:])
		}
		if len(jsonContent) < 10 {
			continue
		}
		parsed, ok := cursorTolerantParse(jsonContent)
		if !ok {
			continue
		}
		name := stringValue(parsed["tool"])
		if name == "" {
			name = stringValue(parsed["name"])
		}
		if name == "" {
			continue
		}
		args := map[string]interface{}{}
		if params, ok := parsed["parameters"].(map[string]interface{}); ok {
			args = params
		} else if params, ok := parsed["arguments"].(map[string]interface{}); ok {
			args = params
		} else if params, ok := parsed["input"].(map[string]interface{}); ok {
			args = params
		}
		args = applyCursorToolArgFixes(name, args)
		toolCalls = append(toolCalls, cursorParsedToolCall{name: name, arguments: args})
	}
	return toolCalls
}

func shouldAutoContinueTruncatedToolResponse(text string, hasTools bool) bool {
	if !hasTools {
		return false
	}
	if !isCursorResponseTruncated(text) {
		if !hasCursorToolCalls(text) {
			return false
		}
		toolCalls := parseCursorToolCalls(text)
		if len(toolCalls) == 0 {
			return false
		}
		return cursorToolCallLooksIncomplete(toolCalls)
	}

	hasUnclosedActionBlock := strings.Count(text, "```json action") > 0
	if !hasUnclosedActionBlock && strings.TrimSpace(text) != "" && len(strings.TrimSpace(text)) < 200 {
		return false
	}
	if !hasCursorToolCalls(text) {
		return true
	}
	toolCalls := parseCursorToolCalls(text)
	if len(toolCalls) == 0 {
		return true
	}
	return cursorToolCallNeedsContinuation(toolCalls)
}

func cursorToolCallNeedsContinuation(toolCalls []cursorParsedToolCall) bool {
	for _, toolCall := range toolCalls {
		if cursorToolCallNeedsMoreContinuation(toolCall) {
			return true
		}
	}
	return false
}

func cursorToolCallLooksIncomplete(toolCalls []cursorParsedToolCall) bool {
	for _, toolCall := range toolCalls {
		if !cursorToolCallNeedsMoreContinuation(toolCall) {
			continue
		}
		if cursorPayloadLooksSemanticallyIncomplete(cursorLargePayloadText(toolCall)) {
			return true
		}
	}
	return false
}

func cursorToolCallNeedsMoreContinuation(toolCall cursorParsedToolCall) bool {
	name := strings.ToLower(toolCall.name)
	if name == "write" || name == "edit" || name == "multiedit" || name == "editnotebook" || name == "notebookedit" {
		return true
	}
	for key, value := range toolCall.arguments {
		if str, ok := value.(string); ok {
			if key == "content" || key == "text" || key == "command" || key == "new_string" || key == "new_str" || key == "file_text" || key == "code" {
				return true
			}
			if len(str) >= 1500 {
				return true
			}
		}
	}
	return false
}

func cursorLargePayloadText(toolCall cursorParsedToolCall) string {
	for _, key := range []string{"content", "new_string", "new_str", "text", "file_text", "code", "command"} {
		if value, ok := toolCall.arguments[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func cursorPayloadLooksSemanticallyIncomplete(payload string) bool {
	trimmed := strings.TrimRightFunc(payload, unicode.IsSpace)
	if len(trimmed) < 1200 {
		return false
	}
	fenceCount := len(regexp.MustCompile("(?m)^```").FindAllStringIndex(trimmed, -1))
	if fenceCount%2 != 0 {
		return true
	}
	lines := strings.Split(trimmed, "\n")
	lastNonEmpty := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			lastNonEmpty = strings.TrimSpace(lines[i])
			break
		}
	}
	if lastNonEmpty == "" {
		return false
	}
	if lastNonEmpty == "|" {
		return true
	}
	if strings.HasPrefix(lastNonEmpty, "|") {
		if strings.Count(lastNonEmpty, "|") < 3 {
			return true
		}
	}
	if regexp.MustCompile(`^([-*+]|\d+\.)\s*$`).MatchString(lastNonEmpty) {
		return true
	}
	if regexp.MustCompile(`[,;:\[{(]\s*$`).MatchString(lastNonEmpty) {
		return true
	}
	likelyDangling := regexp.MustCompile("^[\\p{L}\\p{N}_./`-]{1,16}$").MatchString(lastNonEmpty)
	if likelyDangling && !regexp.MustCompile("[.!?;:。！？`\"'”’\\)\\]\\}]$").MatchString(lastNonEmpty) {
		return true
	}
	return false
}

func cursorTolerantParse(jsonStr string) (map[string]interface{}, bool) {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
		return parsed, true
	}
	fixed, ok := cursorFixJSON(jsonStr)
	if ok {
		if err := json.Unmarshal([]byte(fixed), &parsed); err == nil {
			return parsed, true
		}
	}
	return nil, false
}

func cursorFixJSON(jsonStr string) (string, bool) {
	inString := false
	var builder strings.Builder
	bracketStack := make([]rune, 0)
	for i := 0; i < len(jsonStr); i++ {
		char := jsonStr[i]
		if char == '"' {
			backslashCount := 0
			for j := i - 1; j >= 0 && jsonStr[j] == '\\'; j-- {
				backslashCount++
			}
			if backslashCount%2 == 0 {
				inString = !inString
			}
			builder.WriteByte(char)
			continue
		}
		if inString {
			switch char {
			case '\n':
				builder.WriteString("\\n")
			case '\r':
				builder.WriteString("\\r")
			case '\t':
				builder.WriteString("\\t")
			default:
				builder.WriteByte(char)
			}
			continue
		}
		switch char {
		case '{':
			bracketStack = append(bracketStack, '}')
		case '[':
			bracketStack = append(bracketStack, ']')
		case '}', ']':
			if len(bracketStack) > 0 {
				bracketStack = bracketStack[:len(bracketStack)-1]
			}
		}
		builder.WriteByte(char)
	}
	if inString {
		builder.WriteByte('"')
	}
	for len(bracketStack) > 0 {
		last := bracketStack[len(bracketStack)-1]
		bracketStack = bracketStack[:len(bracketStack)-1]
		builder.WriteRune(last)
	}
	fixed := strings.TrimSpace(builder.String())
	fixed = regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(fixed, "$1")
	return fixed, fixed != ""
}

func closeUnclosedThinking(text string) string {
	openCount := strings.Count(text, "<thinking>")
	closeCount := strings.Count(text, "</thinking>")
	if openCount > closeCount {
		return text + " </thinking>\n"
	}
	return text
}

func deduplicateContinuation(existing string, continuation string) string {
	if continuation == "" || existing == "" {
		return continuation
	}
	maxOverlap := minInt(500, minInt(len(existing), len(continuation)))
	if maxOverlap < 10 {
		return continuation
	}
	tail := existing[len(existing)-maxOverlap:]
	bestOverlap := 0
	for length := maxOverlap; length >= 10; length-- {
		prefix := continuation[:length]
		if strings.HasSuffix(tail, prefix) {
			bestOverlap = length
			break
		}
	}
	if bestOverlap > 0 {
		return continuation[bestOverlap:]
	}

	continuationLines := strings.Split(continuation, "\n")
	tailLines := strings.Split(tail, "\n")
	if len(continuationLines) > 0 && len(tailLines) > 0 {
		firstLine := strings.TrimSpace(continuationLines[0])
		if len(firstLine) >= 10 {
			for i := len(tailLines) - 1; i >= 0; i-- {
				if strings.TrimSpace(tailLines[i]) == firstLine {
					matched := 1
					for k := 1; k < len(continuationLines) && i+k < len(tailLines); k++ {
						if strings.TrimSpace(continuationLines[k]) == strings.TrimSpace(tailLines[i+k]) {
							matched++
						} else {
							break
						}
					}
					if matched >= 2 {
						return strings.Join(continuationLines[matched:], "\n")
					}
					break
				}
			}
		}
	}
	return continuation
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (p *Proxy) autoContinueCursorResponseFull(transformedResp []byte, originalBody []byte, meta *proxyRequestMeta) ([]byte, error) {
	if meta == nil || !meta.CursorMode {
		return transformedResp, nil
	}
	if meta.ClientFormat != ClientFormatOpenAIChat {
		return transformedResp, nil
	}
	payload, ok := decodeJSONObject(transformedResp)
	if !ok {
		return transformedResp, nil
	}
	choices, ok := payload["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return transformedResp, nil
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return transformedResp, nil
	}
	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return transformedResp, nil
	}
	content, _ := message["content"].(string)
	toolCalls, _ := message["tool_calls"].([]interface{})
	text := content
	if text == "" && len(toolCalls) > 0 {
		if args := extractCursorToolArguments(toolCalls); args != "" {
			text = args
		}
	}
	if !shouldAutoContinueTruncatedToolResponse(text, len(toolCalls) > 0) {
		return transformedResp, nil
	}

	fullText := p.autoContinueCursorText(text, originalBody, len(toolCalls) > 0)
	if fullText == "" {
		meta.Degraded = true
		meta.DegradedReason = append(meta.DegradedReason, "auto_continue_failed")
		return transformedResp, nil
	}
	message["content"] = fullText
	delete(message, "tool_calls")
	choice["finish_reason"] = "max_tokens"
	payload["choices"] = []interface{}{choice}
	updated, err := json.Marshal(payload)
	if err != nil {
		return transformedResp, nil
	}
	meta.Degraded = true
	meta.DegradedReason = append(meta.DegradedReason, "auto_continued")
	return updated, nil
}

func (p *Proxy) autoContinueCursorResponseStream(outputText string, originalBody []byte, meta *proxyRequestMeta) (string, error) {
	if meta == nil || !meta.CursorMode {
		return "", nil
	}
	if meta.ClientFormat != ClientFormatOpenAIChat {
		return "", nil
	}
	if !shouldAutoContinueTruncatedToolResponse(outputText, true) {
		return "", nil
	}
	fullText := p.autoContinueCursorText(outputText, originalBody, true)
	if fullText == "" {
		meta.Degraded = true
		meta.DegradedReason = append(meta.DegradedReason, "auto_continue_failed")
		return "", nil
	}
	meta.Degraded = true
	meta.DegradedReason = append(meta.DegradedReason, "auto_continued")
	if strings.HasPrefix(fullText, outputText) {
		return fullText[len(outputText):], nil
	}
	return "", nil
}

func (p *Proxy) autoContinueCursorText(text string, originalBody []byte, hasTools bool) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	var basePayload map[string]interface{}
	payloadSource := originalBody
	if len(payloadSource) == 0 {
		return ""
	}
	if err := json.Unmarshal(payloadSource, &basePayload); err != nil {
		return ""
	}
	basePayload["stream"] = false
	delete(basePayload, "tools")
	delete(basePayload, "tool_choice")
	delete(basePayload, "tool_config")
	delete(basePayload, "functions")
	delete(basePayload, "function_call")

	fullText := trimmed
	maxAutoContinue := 1
	if maxAutoContinue < 1 {
		maxAutoContinue = 1
	}
	continueCount := 0
	consecutiveSmallAdds := 0

	for maxAutoContinue > 0 && shouldAutoContinueTruncatedToolResponse(fullText, hasTools) && continueCount < maxAutoContinue {
		continueCount++
		anchorLength := minInt(300, len(fullText))
		anchorText := fullText[len(fullText)-anchorLength:]
		continuationPrompt := "Your previous response was cut off mid-output. The last part of your output was:\n\n```\n..." + anchorText + "\n```\n\nContinue EXACTLY from where you stopped. DO NOT repeat any content already generated. DO NOT restart the response. Output ONLY the remaining content, starting immediately from the cut-off point."

		assistantContext := closeUnclosedThinking(fullText)
		if len(assistantContext) > 2000 {
			assistantContext = "...\n" + assistantContext[len(assistantContext)-2000:]
		}

		continuationMessages := []interface{}{
			map[string]interface{}{"role": "assistant", "content": assistantContext},
			map[string]interface{}{"role": "user", "content": continuationPrompt},
		}
		basePayload["messages"] = continuationMessages

		payloadBytes, err := json.Marshal(basePayload)
		if err != nil {
			return ""
		}

		endpoints := p.config.GetEndpoints()
		if len(endpoints) == 0 {
			return ""
		}
		endpoint := endpoints[p.currentIndex%len(endpoints)]
		proxyReq, err := buildProxyRequest(&http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/v1/chat/completions"}},
			endpoint,
			endpoint.APIKey,
			payloadBytes,
			"cx_chat_openai",
			nil,
		)
		if err != nil {
			return ""
		}

		resp, err := sendRequest(context.Background(), proxyReq, p.httpClient, p.config)
		if err != nil {
			return ""
		}
		responseBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return ""
		}

		var respPayload map[string]interface{}
		if err := json.Unmarshal(responseBody, &respPayload); err != nil {
			return ""
		}
		choices, _ := respPayload["choices"].([]interface{})
		if len(choices) == 0 {
			return ""
		}
		firstChoice, _ := choices[0].(map[string]interface{})
		message, _ := firstChoice["message"].(map[string]interface{})
		if message == nil {
			return ""
		}
		content, _ := message["content"].(string)
		if strings.TrimSpace(content) == "" {
			return ""
		}

		deduped := deduplicateContinuation(fullText, content)
		fullText += deduped
		trimmedDedup := strings.TrimSpace(deduped)
		if trimmedDedup == "" {
			break
		}
		if len(trimmedDedup) < 100 {
			break
		}
		if len(trimmedDedup) < 500 {
			consecutiveSmallAdds++
			if consecutiveSmallAdds >= 2 {
				break
			}
		} else {
			consecutiveSmallAdds = 0
		}
	}

	if fullText == trimmed {
		return ""
	}
	return fullText
}

func extractCursorToolArguments(toolCalls []interface{}) string {
	for _, raw := range toolCalls {
		call, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		functionData, _ := call["function"].(map[string]interface{})
		if functionData == nil {
			continue
		}
		if args, ok := functionData["arguments"].(string); ok && args != "" {
			return args
		}
	}
	return ""
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
		return fixCursorChatResponseBody(body, meta.ClientModel, meta.CacheMessages)
	case ClientFormatOpenAIResponses:
		return fixCursorResponsesBody(body, meta.ClientModel, meta.CacheMessages, meta.TransformerName)
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
			outputEvent, outputPayload := formatCursorResponsesStreamEventWithState(eventName, payload, meta)
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
		if state.InThinkingTag {
			state.InThinkingTag = false
		}
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

func fixCursorChatResponseBody(body []byte, clientModel string, cacheMessages []map[string]interface{}) ([]byte, error) {
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
			if len(cacheMessages) > 0 {
				if message, ok := choice["message"].(map[string]interface{}); ok {
					globalCursorThinkingCache.StoreFromResponse(cacheMessages, message)
				}
			}
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

func fixCursorResponsesBody(body []byte, clientModel string, cacheMessages []map[string]interface{}, transformerName string) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}
	if clientModel != "" {
		payload["model"] = clientModel
	}
	if transformerName != "cx_resp_openai2" && len(cacheMessages) > 0 {
		globalCursorThinkingCache.StoreFromResponsesOutput(cacheMessages, payload["output"])
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
		return eventName, mergeCursorResponsesEventContext(payload, map[string]interface{}{
			"type":  "output_text",
			"delta": stringValue(payload["delta"]),
		})
	case "response.output_text.done":
		return eventName, mergeCursorResponsesEventContext(payload, map[string]interface{}{
			"type": "output_text",
			"text": stringValue(payload["text"]),
		})
	case "response.reasoning_summary_text.delta":
		return eventName, mergeCursorResponsesEventContext(payload, map[string]interface{}{
			"type":  "summary_text",
			"delta": stringValue(payload["delta"]),
		})
	case "response.reasoning_summary_text.done":
		return eventName, mergeCursorResponsesEventContext(payload, map[string]interface{}{
			"type": "summary_text",
			"text": stringValue(payload["text"]),
		})
	case "response.function_call_arguments.delta":
		return eventName, mergeCursorResponsesEventContext(payload, map[string]interface{}{
			"type":  "function_call",
			"delta": stringValue(payload["delta"]),
		})
	case "response.function_call_arguments.done":
		return eventName, mergeCursorResponsesEventContext(payload, map[string]interface{}{
			"type":      "function_call",
			"arguments": stringValue(payload["arguments"]),
		})
	}

	rewritten := cloneJSONObject(payload)
	if clientModel != "" {
		if _, ok := rewritten["model"]; ok {
			rewritten["model"] = clientModel
		}
	}
	return eventName, rewritten
}

func formatCursorResponsesStreamEventWithState(eventName string, payload map[string]interface{}, meta proxyRequestMeta) (string, map[string]interface{}) {
	state := meta.CursorState
	clientModel := meta.ClientModel
	if eventName == "" {
		eventName = stringValue(payload["type"])
	}
	if meta.TransformerName == "cx_resp_openai2" {
		return formatCursorNativeResponsesStreamEvent(eventName, payload, clientModel)
	}
	if state == nil {
		return formatCursorResponsesStreamEvent(eventName, payload, clientModel)
	}

	switch eventName {
	case "response.created":
		if response, ok := payload["response"].(map[string]interface{}); ok {
			state.ResponsesResponseID = stringValue(response["id"])
			payload = cloneJSONObject(payload)
			rewritten := cloneJSONObject(response)
			if rewritten["id"] == nil && state.ResponsesResponseID != "" {
				rewritten["id"] = state.ResponsesResponseID
			}
			if rewritten["object"] == nil {
				rewritten["object"] = "response"
			}
			if rewritten["status"] == nil {
				rewritten["status"] = "in_progress"
			}
			if _, ok := rewritten["output"]; !ok {
				rewritten["output"] = []interface{}{}
			}
			if clientModel != "" {
				rewritten["model"] = clientModel
			}
			payload["response"] = rewritten
		}
	case "response.output_item.added":
		trackCursorResponsesAdded(state, payload)
		payload = enrichCursorResponsesStreamItemEvent(state, payload, false)
	case "response.reasoning_summary_text.delta":
		state.ResponsesReasoningBuf += stringValue(payload["delta"])
	case "response.reasoning_summary_text.done":
		payload = cloneJSONObject(payload)
		if strings.TrimSpace(stringValue(payload["text"])) == "" && state.ResponsesReasoningBuf != "" {
			payload["text"] = state.ResponsesReasoningBuf
		}
	case "response.output_text.delta":
		state.ResponsesMessageText += stringValue(payload["delta"])
	case "response.output_text.done":
		payload = cloneJSONObject(payload)
		if strings.TrimSpace(stringValue(payload["text"])) == "" && state.ResponsesMessageText != "" {
			payload["text"] = state.ResponsesMessageText
		}
	case "response.function_call_arguments.delta":
		trackCursorResponsesToolArguments(state, payload)
	case "response.function_call_arguments.done":
		trackCursorResponsesToolArgumentsDone(state, payload)
		payload = enrichCursorResponsesFunctionArgumentsDone(state, payload)
	case "response.output_item.done":
		trackCursorResponsesDone(state, payload)
		payload = enrichCursorResponsesStreamItemEvent(state, payload, true)
	case "response.completed":
		payload = injectCursorResponsesCompletedOutput(state, payload, clientModel)
		if meta.TransformerName != "cx_resp_openai2" && len(meta.CacheMessages) > 0 {
			if response, ok := payload["response"].(map[string]interface{}); ok {
				globalCursorThinkingCache.StoreFromResponsesOutput(meta.CacheMessages, response["output"])
			}
		}
	}

	return formatCursorResponsesStreamEvent(eventName, payload, clientModel)
}

func formatCursorNativeResponsesStreamEvent(eventName string, payload map[string]interface{}, clientModel string) (string, map[string]interface{}) {
	if eventName == "" {
		eventName = stringValue(payload["type"])
	}
	rewritten := cloneJSONObject(payload)
	if clientModel != "" {
		if model := stringValue(rewritten["model"]); model != "" {
			rewritten["model"] = clientModel
		}
		if response, ok := rewritten["response"].(map[string]interface{}); ok {
			responseCopy := cloneJSONObject(response)
			if model := stringValue(responseCopy["model"]); model != "" {
				responseCopy["model"] = clientModel
			}
			rewritten["response"] = responseCopy
		}
	}
	return eventName, rewritten
}

func trackCursorResponsesAdded(state *cursorCompatState, payload map[string]interface{}) {
	if state == nil {
		return
	}
	item := payload
	if nested, ok := payload["item"].(map[string]interface{}); ok {
		item = nested
	}

	switch stringValue(item["type"]) {
	case "reasoning":
		state.ResponsesReasoningID = firstNonEmptyString(stringValue(item["id"]), "rs_"+uuid.NewString())
		state.ResponsesReasoningOn = true
		if summary := extractCursorResponsesReasoningSummary(item); summary != "" {
			state.ResponsesReasoningBuf = summary
		}
	case "message":
		state.ResponsesMessageID = firstNonEmptyString(stringValue(item["id"]), "msg_"+uuid.NewString())
		state.ResponsesMessageOn = true
		if text := extractCursorResponsesMessageText(item); text != "" {
			state.ResponsesMessageText = text
		}
	case "function_call":
		index := cursorResponsesOutputIndex(payload)
		if index < 0 {
			index = len(state.ResponsesTools)
		}
		tool := &cursorResponsesToolState{
			ID:        firstNonEmptyString(stringValue(item["id"]), "fc_"+uuid.NewString()),
			CallID:    firstNonEmptyString(stringValue(item["call_id"]), newCursorToolCallID()),
			Name:      stringValue(item["name"]),
			Arguments: stringValue(item["arguments"]),
			Active:    true,
		}
		state.ResponsesTools[index] = tool
	}
}

func trackCursorResponsesToolArguments(state *cursorCompatState, payload map[string]interface{}) {
	delta := stringValue(payload["delta"])
	if state == nil || delta == "" {
		return
	}
	tool := cursorResponsesToolFromPayload(state, payload)
	if tool == nil {
		index := len(state.ResponsesTools)
		tool = &cursorResponsesToolState{
			ID:     "fc_" + uuid.NewString(),
			CallID: newCursorToolCallID(),
			Active: true,
		}
		state.ResponsesTools[index] = tool
	}
	tool.Arguments += delta
}

func trackCursorResponsesToolArgumentsDone(state *cursorCompatState, payload map[string]interface{}) {
	arguments := stringValue(payload["arguments"])
	if state == nil || arguments == "" {
		return
	}
	tool := cursorResponsesToolFromPayload(state, payload)
	if tool == nil {
		return
	}
	tool.Arguments = arguments
}

func trackCursorResponsesDone(state *cursorCompatState, payload map[string]interface{}) {
	if state == nil {
		return
	}
	item := payload
	if nested, ok := payload["item"].(map[string]interface{}); ok {
		item = nested
	}

	switch stringValue(item["type"]) {
	case "reasoning":
		outputItem := cloneJSONObject(item)
		if len(outputItem) == 0 {
			outputItem = buildCursorResponsesReasoningItem(state)
		}
		if _, ok := outputItem["summary"]; !ok {
			outputItem["summary"] = []interface{}{
				map[string]interface{}{"type": "summary_text", "text": state.ResponsesReasoningBuf},
			}
		}
		appendCursorResponsesOutput(state, outputItem)
		state.ResponsesReasoningOn = false
	case "message":
		outputItem := cloneJSONObject(item)
		if len(outputItem) == 0 {
			outputItem = buildCursorResponsesMessageItem(state)
		}
		if _, ok := outputItem["content"]; !ok {
			outputItem["content"] = []interface{}{
				map[string]interface{}{"type": "output_text", "text": state.ResponsesMessageText},
			}
		}
		appendCursorResponsesOutput(state, outputItem)
		state.ResponsesMessageOn = false
	case "function_call":
		outputItem, tool := buildCursorResponsesToolItemFromPayload(state, payload)
		appendCursorResponsesOutput(state, outputItem)
		if tool != nil {
			tool.Active = false
		}
	}
}

func injectCursorResponsesCompletedOutput(state *cursorCompatState, payload map[string]interface{}, clientModel string) map[string]interface{} {
	if state == nil {
		return payload
	}

	if state.ResponsesReasoningOn && state.ResponsesReasoningBuf != "" {
		appendCursorResponsesOutput(state, buildCursorResponsesReasoningItem(state))
		state.ResponsesReasoningOn = false
	}
	if state.ResponsesMessageOn && state.ResponsesMessageText != "" {
		appendCursorResponsesOutput(state, buildCursorResponsesMessageItem(state))
		state.ResponsesMessageOn = false
	}
	for _, index := range sortedCursorResponsesToolIndexes(state) {
		tool := state.ResponsesTools[index]
		if tool == nil || !tool.Active {
			continue
		}
		appendCursorResponsesOutput(state, buildCursorResponsesToolItem(tool))
		tool.Active = false
	}

	rewritten := cloneJSONObject(payload)
	response, ok := payload["response"].(map[string]interface{})
	if !ok {
		response = map[string]interface{}{}
	}
	rewrittenResponse := cloneJSONObject(response)
	if rewrittenResponse["id"] == nil && state.ResponsesResponseID != "" {
		rewrittenResponse["id"] = state.ResponsesResponseID
	}
	if clientModel != "" {
		rewrittenResponse["model"] = clientModel
	}

	output, hasOutput := rewrittenResponse["output"].([]interface{})
	if !hasOutput || len(output) == 0 {
		rewrittenResponse["output"] = cursorResponsesOutputAsInterfaces(state)
	}
	rewritten["response"] = rewrittenResponse
	return rewritten
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

func buildCursorResponsesReasoningItem(state *cursorCompatState) map[string]interface{} {
	return map[string]interface{}{
		"type": "reasoning",
		"id":   firstNonEmptyString(state.ResponsesReasoningID, "rs_"+uuid.NewString()),
		"summary": []interface{}{
			map[string]interface{}{"type": "summary_text", "text": state.ResponsesReasoningBuf},
		},
	}
}

func buildCursorResponsesReasoningAddedItem(state *cursorCompatState) map[string]interface{} {
	return map[string]interface{}{
		"type":    "reasoning",
		"id":      firstNonEmptyString(state.ResponsesReasoningID, "rs_"+uuid.NewString()),
		"summary": []interface{}{},
	}
}

func buildCursorResponsesMessageAddedItem(state *cursorCompatState) map[string]interface{} {
	return map[string]interface{}{
		"type":    "message",
		"id":      firstNonEmptyString(state.ResponsesMessageID, "msg_"+uuid.NewString()),
		"status":  "in_progress",
		"role":    "assistant",
		"content": []interface{}{},
	}
}

func enrichCursorResponsesStreamItemEvent(state *cursorCompatState, payload map[string]interface{}, done bool) map[string]interface{} {
	if state == nil {
		return payload
	}
	rewritten := cloneJSONObject(payload)
	item, ok := payload["item"].(map[string]interface{})
	if !ok {
		item = payload
	}
	itemType := stringValue(item["type"])
	var enriched map[string]interface{}

	switch itemType {
	case "reasoning":
		if done {
			enriched = buildCursorResponsesReasoningItem(state)
		} else {
			enriched = buildCursorResponsesReasoningAddedItem(state)
		}
	case "message":
		if done {
			enriched = buildCursorResponsesMessageItem(state)
		} else {
			enriched = buildCursorResponsesMessageAddedItem(state)
		}
	case "function_call":
		enriched, _ = buildCursorResponsesToolItemFromPayload(state, payload)
		if !done {
			enriched["status"] = "in_progress"
			if _, ok := enriched["arguments"]; !ok {
				enriched["arguments"] = ""
			}
		}
	default:
		return payload
	}

	for key, value := range item {
		enriched[key] = value
	}
	rewritten["item"] = enriched
	return rewritten
}

func enrichCursorResponsesFunctionArgumentsDone(state *cursorCompatState, payload map[string]interface{}) map[string]interface{} {
	if state == nil {
		return payload
	}
	rewritten := cloneJSONObject(payload)
	if strings.TrimSpace(stringValue(rewritten["arguments"])) != "" {
		if _, ok := rewritten["type"]; !ok {
			rewritten["type"] = "function_call"
		}
		return rewritten
	}
	tool := cursorResponsesToolFromPayload(state, rewritten)
	if tool == nil || strings.TrimSpace(tool.Arguments) == "" {
		return rewritten
	}
	rewritten["arguments"] = tool.Arguments
	if _, ok := rewritten["type"]; !ok {
		rewritten["type"] = "function_call"
	}
	return rewritten
}

func extractCursorCacheMessages(body []byte) []map[string]interface{} {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return nil
	}
	rawMessages, ok := payload["messages"].([]interface{})
	if !ok || len(rawMessages) == 0 {
		return nil
	}
	messages := make([]map[string]interface{}, 0, len(rawMessages))
	for _, rawMessage := range rawMessages {
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			continue
		}
		messages = append(messages, cloneJSONObject(message))
	}
	return messages
}

func rewriteCursorChatMessages(body []byte, messages []map[string]interface{}) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}
	rewritten := make([]interface{}, 0, len(messages))
	for _, message := range messages {
		rewritten = append(rewritten, cloneJSONObject(message))
	}
	payload["messages"] = rewritten
	return json.Marshal(payload)
}

func extractCursorResponsesCacheMessages(body []byte, model string) []map[string]interface{} {
	converted, err := convert.OpenAI2ReqToOpenAI(body, model)
	if err != nil {
		return nil
	}
	return extractCursorCacheMessages(converted)
}

func rewriteCursorResponsesMessages(body []byte, messages []map[string]interface{}, model string) ([]byte, error) {
	converted, err := convert.OpenAI2ReqToOpenAI(body, model)
	if err != nil {
		return body, err
	}
	rewrittenChat, err := rewriteCursorChatMessages(converted, messages)
	if err != nil {
		return body, err
	}
	rewrittenResponses, err := convert.OpenAIReqToOpenAI2(rewrittenChat, model)
	if err != nil {
		return body, err
	}
	return rewrittenResponses, nil
}

func rewriteCursorClaudeMessages(body []byte, cacheMessages []map[string]interface{}) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}
	rawMessages, ok := payload["messages"].([]interface{})
	if !ok || len(rawMessages) == 0 {
		return body, nil
	}

	assistantReasoning := make([]string, 0)
	for _, message := range cacheMessages {
		if stringValue(message["role"]) != "assistant" {
			continue
		}
		reasoning := stringValue(message["reasoning_content"])
		if reasoning == "" {
			continue
		}
		assistantReasoning = append(assistantReasoning, reasoning)
	}
	if len(assistantReasoning) == 0 {
		return body, nil
	}

	reasoningIndex := 0
	rewrittenMessages := make([]interface{}, 0, len(rawMessages))
	for _, rawMessage := range rawMessages {
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			rewrittenMessages = append(rewrittenMessages, rawMessage)
			continue
		}
		cloned := cloneJSONObject(message)
		if stringValue(cloned["role"]) == "assistant" && reasoningIndex < len(assistantReasoning) {
			injectCursorClaudeThinking(cloned, assistantReasoning[reasoningIndex])
			reasoningIndex++
		}
		rewrittenMessages = append(rewrittenMessages, cloned)
	}
	payload["messages"] = rewrittenMessages
	return json.Marshal(payload)
}

func rewriteCursorGeminiContents(body []byte, cacheMessages []map[string]interface{}) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}
	rawContents, ok := payload["contents"].([]interface{})
	if !ok || len(rawContents) == 0 {
		return body, nil
	}

	assistantReasoning := make([]string, 0)
	for _, message := range cacheMessages {
		if stringValue(message["role"]) != "assistant" {
			continue
		}
		reasoning := stringValue(message["reasoning_content"])
		if reasoning == "" {
			continue
		}
		assistantReasoning = append(assistantReasoning, reasoning)
	}
	if len(assistantReasoning) == 0 {
		return body, nil
	}

	reasoningIndex := 0
	rewrittenContents := make([]interface{}, 0, len(rawContents))
	for _, rawContent := range rawContents {
		content, ok := rawContent.(map[string]interface{})
		if !ok {
			rewrittenContents = append(rewrittenContents, rawContent)
			continue
		}
		cloned := cloneJSONObject(content)
		if stringValue(cloned["role"]) == "model" && reasoningIndex < len(assistantReasoning) {
			injectCursorGeminiThought(cloned, assistantReasoning[reasoningIndex])
			reasoningIndex++
		}
		rewrittenContents = append(rewrittenContents, cloned)
	}
	payload["contents"] = rewrittenContents
	return json.Marshal(payload)
}

func buildCursorResponsesMessageItem(state *cursorCompatState) map[string]interface{} {
	return map[string]interface{}{
		"type":   "message",
		"id":     firstNonEmptyString(state.ResponsesMessageID, "msg_"+uuid.NewString()),
		"status": "completed",
		"role":   "assistant",
		"content": []interface{}{
			map[string]interface{}{"type": "output_text", "text": state.ResponsesMessageText},
		},
	}
}

func buildCursorResponsesToolItem(tool *cursorResponsesToolState) map[string]interface{} {
	if tool == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"type":      "function_call",
		"id":        firstNonEmptyString(tool.ID, "fc_"+uuid.NewString()),
		"status":    "completed",
		"call_id":   firstNonEmptyString(tool.CallID, newCursorToolCallID()),
		"name":      tool.Name,
		"arguments": tool.Arguments,
	}
}

func buildCursorResponsesToolItemFromPayload(state *cursorCompatState, payload map[string]interface{}) (map[string]interface{}, *cursorResponsesToolState) {
	item := payload
	if nested, ok := payload["item"].(map[string]interface{}); ok {
		item = nested
	}

	index := cursorResponsesOutputIndex(payload)
	tool := state.ResponsesTools[index]
	if tool == nil {
		tool = findCursorResponsesToolByID(state, stringValue(item["id"]))
	}
	if tool == nil {
		tool = latestActiveCursorResponsesTool(state)
	}
	if tool == nil {
		tool = &cursorResponsesToolState{
			ID:        firstNonEmptyString(stringValue(item["id"]), "fc_"+uuid.NewString()),
			CallID:    firstNonEmptyString(stringValue(item["call_id"]), newCursorToolCallID()),
			Name:      stringValue(item["name"]),
			Arguments: stringValue(item["arguments"]),
		}
	}

	if tool.ID == "" {
		tool.ID = firstNonEmptyString(stringValue(item["id"]), "fc_"+uuid.NewString())
	}
	if tool.CallID == "" {
		tool.CallID = firstNonEmptyString(stringValue(item["call_id"]), newCursorToolCallID())
	}
	if tool.Name == "" {
		tool.Name = stringValue(item["name"])
	}
	if tool.Arguments == "" {
		tool.Arguments = stringValue(item["arguments"])
	}

	outputItem := cloneJSONObject(item)
	if _, ok := outputItem["status"]; !ok {
		outputItem["status"] = "completed"
	}
	if _, ok := outputItem["id"]; !ok {
		outputItem["id"] = tool.ID
	}
	if _, ok := outputItem["call_id"]; !ok {
		outputItem["call_id"] = tool.CallID
	}
	if _, ok := outputItem["name"]; !ok {
		outputItem["name"] = tool.Name
	}
	if _, ok := outputItem["arguments"]; !ok {
		outputItem["arguments"] = tool.Arguments
	}
	if _, ok := outputItem["type"]; !ok {
		outputItem["type"] = "function_call"
	}
	return outputItem, tool
}

func appendCursorResponsesOutput(state *cursorCompatState, item map[string]interface{}) {
	if state == nil || len(item) == 0 {
		return
	}
	id := stringValue(item["id"])
	for index, existing := range state.ResponsesOutput {
		if id != "" && id == stringValue(existing["id"]) {
			state.ResponsesOutput[index] = cloneJSONObject(item)
			return
		}
	}
	state.ResponsesOutput = append(state.ResponsesOutput, cloneJSONObject(item))
}

func latestActiveCursorResponsesTool(state *cursorCompatState) *cursorResponsesToolState {
	for _, index := range reverseCursorResponsesToolIndexes(state) {
		tool := state.ResponsesTools[index]
		if tool != nil && tool.Active {
			return tool
		}
	}
	return nil
}

func cursorResponsesToolFromPayload(state *cursorCompatState, payload map[string]interface{}) *cursorResponsesToolState {
	if state == nil {
		return nil
	}
	if index := cursorResponsesOutputIndex(payload); index >= 0 {
		if tool := state.ResponsesTools[index]; tool != nil {
			return tool
		}
	}
	return latestActiveCursorResponsesTool(state)
}

func findCursorResponsesToolByID(state *cursorCompatState, id string) *cursorResponsesToolState {
	if state == nil || id == "" {
		return nil
	}
	for _, tool := range state.ResponsesTools {
		if tool != nil && tool.ID == id {
			return tool
		}
	}
	return nil
}

func cursorResponsesOutputAsInterfaces(state *cursorCompatState) []interface{} {
	if state == nil || len(state.ResponsesOutput) == 0 {
		return []interface{}{}
	}
	output := make([]interface{}, 0, len(state.ResponsesOutput))
	for _, item := range state.ResponsesOutput {
		output = append(output, cloneJSONObject(item))
	}
	return output
}

func extractCursorResponsesMessageText(item map[string]interface{}) string {
	content, ok := item["content"].([]interface{})
	if !ok {
		return ""
	}
	var builder strings.Builder
	for _, partValue := range content {
		part, ok := partValue.(map[string]interface{})
		if !ok {
			continue
		}
		if stringValue(part["type"]) != "output_text" {
			continue
		}
		builder.WriteString(stringValue(part["text"]))
	}
	return builder.String()
}

func extractCursorResponsesReasoningSummary(item map[string]interface{}) string {
	summary, ok := item["summary"].([]interface{})
	if !ok {
		return ""
	}
	var builder strings.Builder
	for _, partValue := range summary {
		part, ok := partValue.(map[string]interface{})
		if !ok {
			continue
		}
		builder.WriteString(stringValue(part["text"]))
	}
	return builder.String()
}

func cursorResponsesOutputIndex(payload map[string]interface{}) int {
	if index, ok := payload["output_index"].(float64); ok {
		return int(index)
	}
	return -1
}

func sortedCursorResponsesToolIndexes(state *cursorCompatState) []int {
	if state == nil || len(state.ResponsesTools) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(state.ResponsesTools))
	for index := range state.ResponsesTools {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}

func reverseCursorResponsesToolIndexes(state *cursorCompatState) []int {
	indexes := sortedCursorResponsesToolIndexes(state)
	if len(indexes) == 0 {
		return nil
	}
	sort.Sort(sort.Reverse(sort.IntSlice(indexes)))
	return indexes
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func applyCursorTransformerCompat(body []byte, meta *proxyRequestMeta, transformerName string) ([]byte, error) {
	if meta == nil || !meta.CursorMode {
		return body, nil
	}
	if meta.ClientFormat != ClientFormatOpenAIResponses {
		return body, nil
	}
	if transformerName == "cx_resp_openai2" || len(meta.CacheMessages) == 0 {
		return body, nil
	}

	injected := globalCursorThinkingCache.Inject(meta.CacheMessages)
	meta.CacheMessages = injected
	return body, nil
}

func applyCursorTransformedRequestCompat(body []byte, meta *proxyRequestMeta, transformerName string) ([]byte, error) {
	if meta == nil || !meta.CursorMode || meta.ClientFormat != ClientFormatOpenAIResponses {
		return body, nil
	}
	if transformerName == "cx_resp_openai2" || len(meta.CacheMessages) == 0 {
		return body, nil
	}

	injected := globalCursorThinkingCache.Inject(meta.CacheMessages)
	meta.CacheMessages = injected

	switch transformerName {
	case "cx_resp_openai":
		return rewriteCursorChatMessages(body, injected)
	case "cx_resp_claude":
		return rewriteCursorClaudeMessages(body, injected)
	case "cx_resp_gemini":
		return rewriteCursorGeminiContents(body, injected)
	default:
		return body, nil
	}
}

func (c *cursorThinkingCache) Inject(messages []map[string]interface{}) []map[string]interface{} {
	if c == nil || len(messages) == 0 {
		return messages
	}
	sessionID := cursorThinkingSessionID(messages)
	if sessionID == "" {
		return messages
	}
	now := time.Now()
	injected := make([]map[string]interface{}, 0, len(messages))
	for _, message := range messages {
		cloned := cloneJSONObject(message)
		if stringValue(cloned["role"]) == "assistant" && stringValue(cloned["reasoning_content"]) == "" {
			key := sessionID + ":" + cursorThinkingMessageHash(cloned)
			if entry, ok := c.store[key]; ok && now.Sub(entry.StoredAt) < cursorThinkingCacheTTL {
				cloned["reasoning_content"] = entry.Reasoning
			}
		}
		injected = append(injected, cloned)
	}
	return injected
}

func (c *cursorThinkingCache) StoreFromResponse(messages []map[string]interface{}, assistantMessage map[string]interface{}) {
	if c == nil || len(messages) == 0 || len(assistantMessage) == 0 {
		return
	}
	reasoning := stringValue(assistantMessage["reasoning_content"])
	if reasoning == "" {
		return
	}
	sessionID := cursorThinkingSessionID(messages)
	if sessionID == "" {
		return
	}
	key := sessionID + ":" + cursorThinkingMessageHash(map[string]interface{}{
		"role":       "assistant",
		"content":    "",
		"tool_calls": []interface{}{},
	})
	c.store[key] = cursorThinkingCacheEntry{
		Reasoning: reasoning,
		StoredAt:  time.Now(),
	}
	c.cleanup()
}

func (c *cursorThinkingCache) StoreFromResponsesOutput(messages []map[string]interface{}, output interface{}) {
	if c == nil || len(messages) == 0 {
		return
	}
	items, ok := output.([]interface{})
	if !ok {
		return
	}
	reasoning := extractCursorResponsesReasoningFromOutput(items)
	if reasoning == "" {
		return
	}
	c.StoreFromResponse(messages, map[string]interface{}{"reasoning_content": reasoning})
}

func (c *cursorThinkingCache) cleanup() {
	if c == nil || len(c.store) < 100 {
		return
	}
	now := time.Now()
	for key, entry := range c.store {
		if now.Sub(entry.StoredAt) >= cursorThinkingCacheTTL {
			delete(c.store, key)
		}
	}
}

func cursorThinkingSessionID(messages []map[string]interface{}) string {
	firstUser := ""
	firstAssistant := ""
	for _, message := range messages {
		role := stringValue(message["role"])
		if role == "system" || role == "developer" {
			continue
		}
		if role == "user" && firstUser == "" {
			firstUser = normalizeCursorThinkingContent(message["content"])
		} else if role == "assistant" && firstAssistant == "" {
			firstAssistant = normalizeCursorThinkingContent(message["content"])
		}
		if firstUser != "" && firstAssistant != "" {
			break
		}
	}
	if firstUser == "" || firstAssistant == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(firstUser + "|" + firstAssistant))
	return fmt.Sprintf("%x", sum)[:16]
}

func cursorThinkingMessageHash(message map[string]interface{}) string {
	content := normalizeCursorThinkingContent(message["content"])
	toolIDs := make([]string, 0)
	if toolCalls, ok := message["tool_calls"].([]interface{}); ok {
		for _, rawToolCall := range toolCalls {
			toolCall, ok := rawToolCall.(map[string]interface{})
			if !ok {
				continue
			}
			toolIDs = append(toolIDs, normalizeCursorToolID(stringValue(toolCall["id"])))
		}
	}
	sort.Strings(toolIDs)
	raw, _ := json.Marshal(map[string]interface{}{"c": content, "t": toolIDs})
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum)[:16]
}

func normalizeCursorThinkingContent(content interface{}) string {
	switch value := content.(type) {
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			switch block := item.(type) {
			case string:
				parts = append(parts, block)
			case map[string]interface{}:
				if stringValue(block["type"]) == "text" {
					parts = append(parts, stringValue(block["text"]))
				}
			}
		}
		return stripCursorThinkFragments(strings.Join(parts, "\n"))
	case string:
		return stripCursorThinkFragments(value)
	default:
		if value == nil {
			return ""
		}
		return stripCursorThinkFragments(fmt.Sprint(value))
	}
}

func stripCursorThinkFragments(text string) string {
	cleaned := text
	for {
		start := strings.Index(cleaned, "<think>")
		if start < 0 {
			break
		}
		end := strings.Index(cleaned[start+len("<think>"):], "</think>")
		if end < 0 {
			cleaned = cleaned[:start]
			break
		}
		cleaned = cleaned[:start] + cleaned[start+len("<think>")+end+len("</think>"):]
	}
	return strings.TrimSpace(cleaned)
}

func normalizeCursorToolID(id string) string {
	if id == "" {
		return ""
	}
	var builder strings.Builder
	for _, ch := range id {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			builder.WriteRune(ch)
		}
	}
	return builder.String()
}

func extractCursorResponsesReasoningFromOutput(items []interface{}) string {
	var builder strings.Builder
	for _, itemValue := range items {
		item, ok := itemValue.(map[string]interface{})
		if !ok || stringValue(item["type"]) != "reasoning" {
			continue
		}
		summary, ok := item["summary"].([]interface{})
		if !ok {
			continue
		}
		for _, partValue := range summary {
			part, ok := partValue.(map[string]interface{})
			if !ok {
				continue
			}
			builder.WriteString(stringValue(part["text"]))
		}
	}
	return builder.String()
}

func injectCursorClaudeThinking(message map[string]interface{}, reasoning string) {
	if message == nil || reasoning == "" {
		return
	}
	content := message["content"]
	switch value := content.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			message["content"] = []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": reasoning},
			}
			return
		}
		message["content"] = []interface{}{
			map[string]interface{}{"type": "thinking", "thinking": reasoning},
			map[string]interface{}{"type": "text", "text": value},
		}
	case []interface{}:
		for _, item := range value {
			block, ok := item.(map[string]interface{})
			if ok && stringValue(block["type"]) == "thinking" {
				return
			}
		}
		message["content"] = append([]interface{}{
			map[string]interface{}{"type": "thinking", "thinking": reasoning},
		}, value...)
	default:
		message["content"] = []interface{}{
			map[string]interface{}{"type": "thinking", "thinking": reasoning},
		}
	}
}

func injectCursorGeminiThought(content map[string]interface{}, reasoning string) {
	if content == nil || reasoning == "" {
		return
	}
	rawParts, ok := content["parts"].([]interface{})
	if !ok {
		content["parts"] = []interface{}{
			map[string]interface{}{"text": reasoning, "thought": true},
		}
		return
	}
	for _, rawPart := range rawParts {
		part, ok := rawPart.(map[string]interface{})
		if ok && part["thought"] == true {
			return
		}
	}
	content["parts"] = append([]interface{}{
		map[string]interface{}{"text": reasoning, "thought": true},
	}, rawParts...)
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

	args = applyCursorToolArgFixes(stringValue(functionData["name"]), args)
	encoded, err := json.Marshal(args)
	if err != nil {
		return
	}
	functionData["arguments"] = string(encoded)
}

func applyCursorToolArgFixes(toolName string, args map[string]interface{}) map[string]interface{} {
	args = normalizeCursorArgs(args)
	args = repairCursorStrReplaceArgs(toolName, args)
	return args
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

func mergeCursorResponsesEventContext(source map[string]interface{}, target map[string]interface{}) map[string]interface{} {
	if len(source) == 0 {
		return target
	}
	if target == nil {
		target = map[string]interface{}{}
	}
	for _, key := range []string{
		"output_index",
		"content_index",
		"summary_index",
		"item_id",
		"call_id",
	} {
		if value, ok := source[key]; ok {
			target[key] = value
		}
	}
	return target
}

func newCursorToolCallID() string {
	return "call_" + uuid.NewString()
}
