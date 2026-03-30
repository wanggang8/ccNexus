package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	newcursor "github.com/lich0821/ccNexus/internal/cursorbridge"
)

// Pre-compiled regular expressions for performance
var (
	reJsonAction       = regexp.MustCompile("```json\\s+action")
	reJsonActionBlock  = regexp.MustCompile("```json\\s+action[\\s\\S]*?```")
	reLineCodeBlock    = regexp.MustCompile("(?m)^```")
	reOpenTag          = regexp.MustCompile("(?m)^<[^/][a-zA-Z]")
	reCloseTag         = regexp.MustCompile("(?m)^</[a-zA-Z]")
	reTrailingPunct    = regexp.MustCompile(`[,;:\[{(]\s*$`)
	reTrailingEscape   = regexp.MustCompile(`\\n?\s*$`)
	reTrailingLower    = regexp.MustCompile(`[a-z]$`)
	reToolCallOpen       = regexp.MustCompile("```json(?:\\s+action)?")
	reFence              = regexp.MustCompile("(?m)^```")
	reListOrNumber       = regexp.MustCompile(`^([-*+]|\d+\.)\s*$`)
	reLikelyDangling     = regexp.MustCompile("^[\\p{L}\\p{N}_./`-]{1,16}$")
	reEndPunct           = regexp.MustCompile("[.!?;:。！？`\"'\"')\\]\\}]$")
	reTrailingCommaClose = regexp.MustCompile(`,\s*([}\]])`)
)

func isCursorResponseTruncated(text string) bool {
	trimmed := strings.TrimRightFunc(text, unicode.IsSpace)
	if trimmed == "" {
		return false
	}
	jsonActionOpens := len(reJsonAction.FindAllStringIndex(trimmed, -1))
	if jsonActionOpens > 0 {
		jsonActionBlocks := reJsonActionBlock.FindAllStringIndex(trimmed, -1)
		if jsonActionOpens > len(jsonActionBlocks) {
			return true
		}
		return false
	}
	lineStartCodeBlocks := len(reLineCodeBlock.FindAllStringIndex(trimmed, -1))
	if lineStartCodeBlocks%2 != 0 {
		return true
	}
	openTags := len(reOpenTag.FindAllStringIndex(trimmed, -1))
	closeTags := len(reCloseTag.FindAllStringIndex(trimmed, -1))
	if openTags > closeTags+1 {
		return true
	}
	if reTrailingPunct.MatchString(trimmed) {
		return true
	}
	if len(trimmed) > 2000 && reTrailingEscape.MatchString(trimmed) && !strings.HasSuffix(trimmed, "```") {
		return true
	}
	if len(trimmed) < 500 && reTrailingLower.MatchString(trimmed) {
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

func decodeJSONObject(body []byte) (map[string]interface{}, bool) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func stringValue(value interface{}) string {
	stringValue, _ := value.(string)
	return stringValue
}

func parseCursorToolCalls(responseText string) []cursorParsedToolCall {
	toolCalls := make([]cursorParsedToolCall, 0)
	indices := reToolCallOpen.FindAllStringIndex(responseText, -1)
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
		args = newcursor.ApplyToolArgFixesCompat(name, args)
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
	fenceCount := len(reFence.FindAllStringIndex(trimmed, -1))
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
	if reListOrNumber.MatchString(lastNonEmpty) {
		return true
	}
	if reTrailingPunct.MatchString(lastNonEmpty) {
		return true
	}
	likelyDangling := reLikelyDangling.MatchString(lastNonEmpty)
	if likelyDangling && !reEndPunct.MatchString(lastNonEmpty) {
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
	fixed = reTrailingCommaClose.ReplaceAllString(fixed, "$1")
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

func (p *Proxy) autoContinueCursorResponseFull(ctx context.Context, transformedResp []byte, originalBody []byte, meta *proxyRequestMeta) ([]byte, error) {
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

	fullText := p.autoContinueCursorText(ctx, text, originalBody, len(toolCalls) > 0)
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

func (p *Proxy) autoContinueCursorResponseStream(ctx context.Context, outputText string, originalBody []byte, meta *proxyRequestMeta) (string, error) {
	if meta == nil || !meta.CursorMode {
		return "", nil
	}
	if meta.ClientFormat != ClientFormatOpenAIChat {
		return "", nil
	}
	if !shouldAutoContinueTruncatedToolResponse(outputText, true) {
		return "", nil
	}
	fullText := p.autoContinueCursorText(ctx, outputText, originalBody, true)
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

func (p *Proxy) autoContinueCursorText(ctx context.Context, text string, originalBody []byte, hasTools bool) string {
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
	const maxAutoContinue = 1
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

		endpoint := p.getEndpointByCurrentIndex()
		if endpoint.Name == "" {
			return ""
		}
		proxyReq, err := buildProxyRequest(&http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/v1/chat/completions"}},
			endpoint,
			endpoint.APIKey,
			payloadBytes,
			"cx_chat_openai",
			nil,
			nil,
		)
		if err != nil {
			return ""
		}

		resp, err := sendRequest(ctx, proxyReq, p.httpClient, p.config, p.getOrCreateProxyClient)
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
