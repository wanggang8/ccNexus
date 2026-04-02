package augment

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lich0821/ccNexus/internal/tokencount"
)

// Augment protocol constants (from augment-protocol.js)
const (
	// Stop reasons
	augmentStopReasonUnspecified           = 0
	augmentStopReasonEndTurn               = 1
	augmentStopReasonMaxTokens             = 2
	augmentStopReasonToolUseRequested      = 3
	augmentStopReasonSafety                = 4
	augmentStopReasonRecitation            = 5
	augmentStopReasonMalformedFunctionCall = 6

	// Response node types
	augmentNodeTypeRawResponse        = 0
	augmentNodeTypeSuggestedQuestions = 1
	augmentNodeTypeMainTextFinished   = 2
	augmentNodeTypeToolUse            = 5
	augmentNodeTypeAgentMemory        = 6
	augmentNodeTypeToolUseStart       = 7
	augmentNodeTypeThinking           = 8
	augmentNodeTypeBillingMetadata    = 9
	augmentNodeTypeTokenUsage         = 10
)

// Content block delta types
const (
	deltaTypeTextDelta      = "text_delta"
	deltaTypeInputJSONDelta = "input_json_delta"
	deltaTypeThinkingDelta  = "thinking_delta"
	deltaTypeSignatureDelta = "signature_delta"
)

const (
	ssePreviewMaxChars = 500
)

// ConvertSSEToNDJSON converts an SSE stream (from Claude/OpenAI) to NDJSON format
// expected by the Augment plugin. Returns the converted bytes and any error.
func ConvertSSEToNDJSON(sseData []byte, targetType string, toolCtx map[string]*ToolContext) ([]byte, error) {
	var out strings.Builder
	if _, _, err := StreamConvertSSEToNDJSON(strings.NewReader(string(sseData)), &out, targetType, toolCtx); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// StreamConvertSSEToNDJSON converts an SSE stream to NDJSON in a streaming fashion.
// It reads from r and writes converted NDJSON lines to w.
// Returns inputTokens, outputTokens, and any error.
func StreamConvertSSEToNDJSON(r io.Reader, w io.Writer, targetType string, toolCtx map[string]*ToolContext) (inputTokens, outputTokens int, err error) {
	switch targetType {
	case "claude", "cli":
		return streamConvertClaudeSSE(r, w, toolCtx)
	case "openai":
		return streamConvertOpenAISSE(r, w, toolCtx)
	case "openai2":
		return streamConvertOpenAIResponsesSSE(r, w, toolCtx)
	default:
		return 0, 0, fmt.Errorf("augment response: unsupported target type %q", targetType)
	}
}

// ConvertJSONToNDJSON converts a non-streaming JSON response body into the
// NDJSON format expected by the Augment client.
func ConvertJSONToNDJSON(body []byte, targetType string, toolCtx map[string]*ToolContext) (inputTokens, outputTokens int, data []byte, err error) {
	var out strings.Builder
	switch targetType {
	case "claude", "cli":
		inputTokens, outputTokens, err = convertClaudeJSONToNDJSON(body, &out, toolCtx)
	case "openai":
		inputTokens, outputTokens, err = convertOpenAIJSONToNDJSON(body, &out, toolCtx)
	case "openai2":
		inputTokens, outputTokens, err = convertOpenAIResponsesJSONToNDJSON(body, &out, toolCtx)
	default:
		err = fmt.Errorf("augment response: unsupported target type %q", targetType)
	}
	if err != nil {
		return 0, 0, nil, err
	}
	return inputTokens, outputTokens, []byte(out.String()), nil
}

type toolUseBuffer struct {
	id      string
	name    string
	input   strings.Builder
	active  bool
	started bool // Track if TOOL_USE_START has been emitted
}

type thinkingBuffer struct {
	active    bool
	summary   strings.Builder
	signature string
}

func writeChunkLine(w io.Writer, obj map[string]interface{}) {
	line, err := json.Marshal(obj)
	if err != nil {
		// Log error but continue processing
		// In production, consider using a proper logger
		fmt.Fprintf(w, "{\"error\":\"json_marshal_failed\",\"text\":\"\"}\n")
		if f, ok := w.(interface{ Flush() }); ok {
			f.Flush()
		}
		return
	}
	_, _ = w.Write(line)
	_, _ = w.Write([]byte("\n"))
	if f, ok := w.(interface{ Flush() }); ok {
		f.Flush()
	}
}

func newBaseChunk(text string) map[string]interface{} {
	return map[string]interface{}{
		"text":                  text,
		"unknown_blob_names":    []interface{}{},
		"checkpoint_not_found":  false,
		"workspace_file_chunks": []interface{}{},
		"nodes":                 []interface{}{},
	}
}

func processSSEEvents(r io.Reader, handle func(eventType string, data string) error) error {
	reader := bufio.NewReader(r)
	var eventType string
	var dataLines []string

	flush := func() error {
		if len(dataLines) == 0 {
			eventType = ""
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		currentEventType := eventType
		eventType = ""
		return handle(currentEventType, data)
	}

	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
		} else if strings.HasPrefix(line, ":") {
			// Comment line - ignore
		} else {
			field := ""
			value := ""
			if idx := strings.Index(line, ":"); idx >= 0 {
				field = strings.TrimSpace(line[:idx])
				value = strings.TrimLeft(line[idx+1:], " \t")
			} else {
				field = strings.TrimSpace(line)
			}
			switch field {
			case "event":
				eventType = value
			case "data":
				dataLines = append(dataLines, value)
			default:
				// ignore other fields (id, retry, etc.)
			}
		}
		if readErr == io.EOF {
			break
		}
	}
	return flush()
}

func emitThinkingChunk(w io.Writer, text, signature string, nextNodeID *int) {
	if text == "" && signature == "" {
		return
	}
	emitThinkingNodeChunk(w, buildThinkingNodePayload(text, signature, "", "", nil), nextNodeID)
}

func emitThinkingNodeChunk(w io.Writer, thinking map[string]interface{}, nextNodeID *int) {
	if len(thinking) == 0 {
		return
	}
	node := map[string]interface{}{
		"id":       *nextNodeID,
		"type":     augmentNodeTypeThinking,
		"content":  "",
		"thinking": thinking,
	}
	*nextNodeID++
	chunk := newBaseChunk("")
	chunk["nodes"] = []interface{}{node}
	writeChunkLine(w, chunk)
}

func buildThinkingNodePayload(text, signature, openaiID, encryptedContent string, providerMetadata map[string]interface{}) map[string]interface{} {
	thinking := map[string]interface{}{}
	if strings.TrimSpace(text) != "" {
		thinking["summary"] = text
	}
	if strings.TrimSpace(signature) != "" {
		thinking["signature"] = signature
	}
	if strings.TrimSpace(openaiID) != "" {
		thinking["openai_id"] = openaiID
	}
	if strings.TrimSpace(encryptedContent) != "" {
		thinking["encrypted_content"] = encryptedContent
	}
	if len(providerMetadata) > 0 {
		thinking["provider_metadata"] = cloneJSONValue(providerMetadata)
	}
	return thinking
}

func emitToolUseChunks(w io.Writer, toolUseID, toolName, inputJSON string, toolCtx map[string]*ToolContext, nextNodeID *int) bool {
	if strings.TrimSpace(toolName) == "" {
		return false
	}
	if strings.TrimSpace(toolUseID) == "" {
		toolUseID = fmt.Sprintf("tool-%d", *nextNodeID)
	}
	if strings.TrimSpace(inputJSON) == "" {
		inputJSON = "{}"
	}

	toolUse := map[string]interface{}{
		"tool_name":   toolName,
		"tool_use_id": toolUseID,
		"input_json":  inputJSON,
	}
	if toolCtx != nil {
		attachToolUseMCPMetadata(toolUse, toolName, toolCtx)
	}

	startNode := map[string]interface{}{
		"id":       *nextNodeID,
		"type":     augmentNodeTypeToolUseStart,
		"content":  "",
		"tool_use": toolUse,
	}
	*nextNodeID++
	startChunk := newBaseChunk("")
	startChunk["nodes"] = []interface{}{startNode}
	writeChunkLine(w, startChunk)

	node := map[string]interface{}{
		"id":       *nextNodeID,
		"type":     augmentNodeTypeToolUse,
		"content":  "",
		"tool_use": toolUse,
	}
	*nextNodeID++
	chunk := newBaseChunk("")
	chunk["nodes"] = []interface{}{node}
	writeChunkLine(w, chunk)
	return true
}

func emitFinalStopChunk(w io.Writer, stopReasonSeen bool, stopReason int, sawToolUse bool, endedCleanly bool) {
	finalReason := augmentStopReasonUnspecified
	if endedCleanly {
		if stopReasonSeen {
			finalReason = stopReason
		} else if sawToolUse {
			finalReason = augmentStopReasonToolUseRequested
		} else {
			finalReason = augmentStopReasonEndTurn
		}
		if sawToolUse && finalReason == augmentStopReasonEndTurn {
			finalReason = augmentStopReasonToolUseRequested
		}
	}
	chunk := newBaseChunk("")
	chunk["stop_reason"] = finalReason
	writeChunkLine(w, chunk)
}

func extractUpstreamErrorMessage(obj map[string]interface{}) string {
	if obj == nil {
		return ""
	}
	if errObj := firstMap(obj, "error"); errObj != nil {
		if msg := firstString(errObj, "message"); msg != "" {
			return msg
		}
	}
	if resp := firstMap(obj, "response"); resp != nil {
		if errObj := firstMap(resp, "error"); errObj != nil {
			if msg := firstString(errObj, "message"); msg != "" {
				return msg
			}
		}
	}
	if msg := firstString(obj, "message"); msg != "" {
		return msg
	}
	return ""
}

func emitTokenUsageChunk(w io.Writer, tokenUsage map[string]interface{}, nextNodeID *int) bool {
	tokenUsage = normalizePluginFacingTokenUsage(tokenUsage)
	if len(tokenUsage) == 0 {
		return false
	}
	node := map[string]interface{}{
		"id":          *nextNodeID,
		"type":        augmentNodeTypeTokenUsage,
		"content":     "",
		"token_usage": tokenUsage,
	}
	*nextNodeID++
	chunk := newBaseChunk("")
	chunk["nodes"] = []interface{}{node}
	writeChunkLine(w, chunk)
	return true
}

func convertClaudeJSONToNDJSON(body []byte, w io.Writer, toolCtx map[string]*ToolContext) (inputTokens, outputTokens int, err error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		return 0, 0, err
	}

	msg := obj
	if nested, ok := obj["message"].(map[string]interface{}); ok && len(nested) > 0 {
		msg = nested
	}
	if firstString(msg, "type") == "error" || firstMap(msg, "error") != nil {
		if msgText := extractUpstreamErrorMessage(msg); msgText != "" {
			return 0, 0, fmt.Errorf("augment response: claude upstream error: %s", msgText)
		}
		return 0, 0, fmt.Errorf("augment response: claude upstream error")
	}

	nextNodeID := 1
	sawToolUse := false
	stopReasonSeen := false
	stopReason := augmentStopReasonEndTurn
	if sr := firstString(msg, "stop_reason", "stopReason"); sr != "" {
		stopReasonSeen = true
		stopReason = mapClaudeStopReason(sr)
	}

	var textBuf strings.Builder
	flushText := func() {
		if textBuf.Len() == 0 {
			return
		}
		writeChunkLine(w, newBaseChunk(textBuf.String()))
		textBuf.Reset()
	}

	for _, raw := range firstArray(msg, "content") {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch firstString(block, "type") {
		case "text":
			if text := firstString(block, "text"); text != "" {
				textBuf.WriteString(text)
			}
		case "thinking":
			flushText()
			emitThinkingChunk(w, firstString(block, "thinking", "summary", "text"), firstString(block, "signature"), &nextNodeID)
		case "tool_use":
			flushText()
			inputJSON := "{}"
			if input := firstValue(block, "input"); input != nil {
				inputJSON = stableJSON(input)
			}
			if emitToolUseChunks(w, firstString(block, "id"), firstString(block, "name"), inputJSON, toolCtx, &nextNodeID) {
				sawToolUse = true
			}
		}
	}
	flushText()

	usage := firstMap(msg, "usage")
	tokenUsage := map[string]interface{}{}
	if usage != nil {
		if v, ok := usageInt(usage, "input_tokens"); ok {
			inputTokens = v
			tokenUsage["input_tokens"] = v
		}
		if v, ok := usageInt(usage, "output_tokens"); ok {
			outputTokens = v
			tokenUsage["output_tokens"] = v
		}
		if v, ok := usageInt(usage, "cache_read_input_tokens"); ok {
			tokenUsage["cache_read_input_tokens"] = v
		}
		if v, ok := usageInt(usage, "cache_creation_input_tokens"); ok {
			tokenUsage["cache_creation_input_tokens"] = v
		}
	}
	_ = emitTokenUsageChunk(w, tokenUsage, &nextNodeID)
	emitFinalStopChunk(w, stopReasonSeen, stopReason, sawToolUse, true)
	return inputTokens, outputTokens, nil
}

func convertOpenAIJSONToNDJSON(body []byte, w io.Writer, toolCtx map[string]*ToolContext) (inputTokens, outputTokens int, err error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		return 0, 0, err
	}
	if firstMap(obj, "error") != nil {
		if msgText := extractUpstreamErrorMessage(obj); msgText != "" {
			return 0, 0, fmt.Errorf("augment response: openai upstream error: %s", msgText)
		}
		return 0, 0, fmt.Errorf("augment response: openai upstream error")
	}

	nextNodeID := 1
	sawToolUse := false

	text := extractOpenAIJSONText(obj)
	if text != "" {
		writeChunkLine(w, newBaseChunk(text))
	}

	if thinking := extractOpenAIJSONThinking(obj); thinking != "" {
		emitThinkingChunk(w, thinking, "", &nextNodeID)
	}

	for _, tc := range extractOpenAIJSONToolCalls(obj) {
		if emitToolUseChunks(w, tc.id, tc.name, tc.args, toolCtx, &nextNodeID) {
			sawToolUse = true
		}
	}

	var usageAcc usageAccumulator
	if usage := firstMap(obj, "usage"); usage != nil {
		usageAcc.merge(usage)
	}
	applyEstimatedOutputTokens(&usageAcc, text)
	tokenUsage := usageAcc.buildTokenUsage()
	if v, ok := tokenUsage["input_tokens"].(int); ok {
		inputTokens = v
	}
	if v, ok := tokenUsage["output_tokens"].(int); ok {
		outputTokens = v
	}
	_ = emitTokenUsageChunk(w, tokenUsage, &nextNodeID)

	stopReasonSeen := false
	stopReason := augmentStopReasonEndTurn
	if choices, ok := obj["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if fr := firstString(choice, "finish_reason"); fr != "" {
				stopReasonSeen = true
				stopReason = mapOpenAIFinishReason(fr)
			}
		}
	}
	emitFinalStopChunk(w, stopReasonSeen, stopReason, sawToolUse, true)
	return inputTokens, outputTokens, nil
}

func convertOpenAIResponsesJSONToNDJSON(body []byte, w io.Writer, toolCtx map[string]*ToolContext) (inputTokens, outputTokens int, err error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		return 0, 0, err
	}

	resp := obj
	if nested, ok := obj["response"].(map[string]interface{}); ok && len(nested) > 0 {
		resp = nested
	}
	if firstMap(resp, "error") != nil || firstMap(obj, "error") != nil {
		if msgText := extractUpstreamErrorMessage(resp); msgText != "" {
			return 0, 0, fmt.Errorf("augment response: openai responses upstream error: %s", msgText)
		}
		if msgText := extractUpstreamErrorMessage(obj); msgText != "" {
			return 0, 0, fmt.Errorf("augment response: openai responses upstream error: %s", msgText)
		}
		return 0, 0, fmt.Errorf("augment response: openai responses upstream error")
	}

	nextNodeID := 1
	sawToolUse := false

	text := firstString(resp, "output_text", "outputText", "text")
	if text == "" {
		text = extractResponsesTextFromOutput(resp["output"])
	}
	if text != "" {
		writeChunkLine(w, newBaseChunk(text))
	}

	if summary := extractResponsesReasoningSummaryFromOutput(resp["output"]); summary != "" {
		reasoningItem := extractResponsesReasoningItemFromOutput(resp["output"])
		emitThinkingNodeChunk(w, buildThinkingNodePayload(summary, "", firstString(reasoningItem, "id"), firstString(reasoningItem, "encrypted_content", "encryptedContent"), cloneResponsesReasoningMetadata(reasoningItem)), &nextNodeID)
	}

	for _, tc := range extractResponsesToolCalls(resp["output"]) {
		if emitToolUseChunks(w, tc.callID, tc.name, tc.arguments, toolCtx, &nextNodeID) {
			sawToolUse = true
		}
	}

	var usageAcc usageAccumulator
	usageAcc.merge(extractResponsesUsage(resp))
	applyEstimatedOutputTokens(&usageAcc, text)
	tokenUsage := usageAcc.buildTokenUsage()
	if v, ok := tokenUsage["input_tokens"].(int); ok {
		inputTokens = v
	}
	if v, ok := tokenUsage["output_tokens"].(int); ok {
		outputTokens = v
	}
	_ = emitTokenUsageChunk(w, tokenUsage, &nextNodeID)

	stopReasonSeen, stopReason := extractResponsesStopReason(resp)
	emitFinalStopChunk(w, stopReasonSeen, stopReason, sawToolUse, true)
	return inputTokens, outputTokens, nil
}

type openAIJSONToolCall struct {
	id   string
	name string
	args string
}

func extractOpenAIJSONText(obj map[string]interface{}) string {
	choices, _ := obj["choices"].([]interface{})
	if len(choices) == 0 {
		return ""
	}
	choice, _ := choices[0].(map[string]interface{})
	if msg, ok := choice["message"].(map[string]interface{}); ok {
		switch content := msg["content"].(type) {
		case string:
			return strings.TrimSpace(content)
		case []interface{}:
			var parts []string
			for _, raw := range content {
				block, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				if text := firstString(block, "text"); text != "" {
					parts = append(parts, text)
				}
			}
			return strings.TrimSpace(strings.Join(parts, ""))
		}
	}
	return firstString(choice, "text")
}

func extractOpenAIJSONThinking(obj map[string]interface{}) string {
	choices, _ := obj["choices"].([]interface{})
	if len(choices) == 0 {
		return ""
	}
	choice, _ := choices[0].(map[string]interface{})
	msg, _ := choice["message"].(map[string]interface{})
	return firstString(msg, "reasoning", "reasoning_content", "thinking", "thinking_content")
}

func startsWithWhitespace(text string) bool {
	if text == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(text)
	return unicode.IsSpace(r)
}

func endsWithWhitespace(text string) bool {
	if text == "" {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(text)
	return unicode.IsSpace(r)
}

func extractOpenAIJSONToolCalls(obj map[string]interface{}) []openAIJSONToolCall {
	choices, _ := obj["choices"].([]interface{})
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]interface{})
	msg, _ := choice["message"].(map[string]interface{})
	var out []openAIJSONToolCall
	if toolCalls, ok := msg["tool_calls"].([]interface{}); ok {
		for _, raw := range toolCalls {
			tc, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			fn, _ := tc["function"].(map[string]interface{})
			name := firstString(fn, "name")
			if name == "" {
				continue
			}
			args := firstString(fn, "arguments")
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			out = append(out, openAIJSONToolCall{id: firstString(tc, "id"), name: name, args: args})
		}
	}
	if functionCall, ok := msg["function_call"].(map[string]interface{}); ok {
		name := firstString(functionCall, "name")
		if name != "" {
			args := firstString(functionCall, "arguments")
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			out = append(out, openAIJSONToolCall{name: name, args: args})
		}
	}
	return out
}

func streamConvertClaudeSSE(r io.Reader, w io.Writer, toolCtx map[string]*ToolContext) (inputTokens, outputTokens int, err error) {
	var buf toolUseBuffer
	var thinking thinkingBuffer
	nextNodeID := 1
	hasEmittedToolUse := false
	var usageAcc usageAccumulator
	var generatedText strings.Builder
	usageEmitted := false
	stopReasonSeen := false
	stopReason := augmentStopReasonEndTurn
	sawMessageStop := false
	dataEvents := 0
	parsedChunks := 0
	sawThinking := false

	flushThinking := func() {
		if !thinking.active {
			return
		}
		sawThinking = true
		emitThinkingChunk(w, thinking.summary.String(), thinking.signature, &nextNodeID)
		thinking = thinkingBuffer{}
	}

	err = processSSEEvents(r, func(lastEventType string, data string) error {
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			return nil
		}
		dataEvents++

		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return nil
		}
		parsedChunks++
		if _, ok := ev["type"].(string); !ok && lastEventType != "" {
			ev["type"] = lastEventType
		}
		typ, _ := ev["type"].(string)
		if typ == "error" || firstMap(ev, "error") != nil {
			if msgText := extractUpstreamErrorMessage(ev); msgText != "" {
				return fmt.Errorf("augment response: claude upstream error: %s", msgText)
			}
			return fmt.Errorf("augment response: claude upstream error")
		}

		switch typ {
		case "content_block_delta":
			delta, _ := ev["delta"].(map[string]interface{})
			deltaType, _ := delta["type"].(string)
			switch deltaType {
			case deltaTypeTextDelta:
				text, _ := delta["text"].(string)
				if text != "" {
					generatedText.WriteString(text)
					writeChunkLine(w, newBaseChunk(text))
				}
			case deltaTypeInputJSONDelta:
				partial, _ := delta["partial_json"].(string)
				if buf.active && partial != "" {
					buf.input.WriteString(partial)
				}
			case deltaTypeThinkingDelta:
				thinkingText, _ := delta["thinking"].(string)
				if thinkingText != "" {
					if !thinking.active {
						thinking.active = true
					}
					thinking.summary.WriteString(thinkingText)
				}
			case deltaTypeSignatureDelta:
				signature, _ := delta["signature"].(string)
				if signature != "" {
					if !thinking.active {
						thinking.active = true
					}
					thinking.signature = signature
				}
			}

		case "message_start":
			if usage, ok := ev["usage"].(map[string]interface{}); ok {
				usageAcc.merge(usage)
			}
			if msg, ok := ev["message"].(map[string]interface{}); ok {
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					usageAcc.merge(usage)
				}
			}

		case "content_block_start":
			cb, _ := ev["content_block"].(map[string]interface{})
			cbType, _ := cb["type"].(string)
			if cbType == "tool_use" {
				buf = toolUseBuffer{}
				buf.active = true
				buf.id, _ = cb["id"].(string)
				buf.name, _ = cb["name"].(string)
			} else if cbType == "thinking" {
				thinking = thinkingBuffer{active: true}
			}

		case "content_block_stop":
			if thinking.active {
				flushThinking()
				return nil
			}
			if buf.active {
				emitToolUseChunks(w, buf.id, buf.name, buf.input.String(), toolCtx, &nextNodeID)
				buf.active = false
				hasEmittedToolUse = true
			}

		case "message_delta":
			delta, _ := ev["delta"].(map[string]interface{})
			if sr, ok := delta["stop_reason"].(string); ok && sr != "" {
				stopReasonSeen = true
				stopReason = mapClaudeStopReason(sr)
			}
			// Extract usage from both delta["usage"] and top-level ev["usage"]
			if usage, ok := delta["usage"].(map[string]interface{}); ok {
				usageAcc.merge(usage)
			}
			if usage, ok := ev["usage"].(map[string]interface{}); ok {
				usageAcc.merge(usage)
			}

		case "message_stop":
			sawMessageStop = true
			if usage, ok := ev["usage"].(map[string]interface{}); ok {
				usageAcc.merge(usage)
			}
			flushThinking()
			if !usageEmitted {
				usageEmitted = emitAggregatedTokenUsageNode(w, &usageAcc, &nextNodeID)
			}
		}
		return nil
	})

	flushThinking()

	if buf.active && (sawMessageStop || stopReasonSeen) {
		emitToolUseChunks(w, buf.id, buf.name, buf.input.String(), toolCtx, &nextNodeID)
		buf.active = false
		hasEmittedToolUse = true
	}

	if !usageEmitted {
		usageEmitted = emitAggregatedTokenUsageNode(w, &usageAcc, &nextNodeID)
	}
	if generatedText.Len() == 0 && !hasEmittedToolUse && !sawThinking && !usageEmitted {
		if dataEvents > 1 {
			return 0, 0, fmt.Errorf("augment response: claude sse produced no parseable content (data_events=%d, parsed_chunks=%d)", dataEvents, parsedChunks)
		}
	}
	emitFinalStopChunk(w, stopReasonSeen, stopReason, hasEmittedToolUse, sawMessageStop || stopReasonSeen || hasEmittedToolUse)

	// Extract final token counts using buildTokenUsage to ensure consistency with emitted usage
	tokenUsage := usageAcc.buildTokenUsage()
	if tokenUsage != nil {
		if v, ok := tokenUsage["input_tokens"].(int); ok {
			inputTokens = v
		}
		if v, ok := tokenUsage["output_tokens"].(int); ok {
			outputTokens = v
		}
	}
	return inputTokens, outputTokens, err
}

// mapClaudeStopReason maps Claude stop_reason to Augment stop_reason constants.
// Based on mapAnthropicStopReasonToAugment from augment-protocol.js
func mapClaudeStopReason(sr string) int {
	r := strings.ToLower(strings.TrimSpace(sr))
	switch r {
	case "end_turn":
		return augmentStopReasonEndTurn
	case "max_tokens":
		return augmentStopReasonMaxTokens
	case "tool_use":
		return augmentStopReasonToolUseRequested
	case "stop_sequence":
		return augmentStopReasonEndTurn
	case "safety":
		return augmentStopReasonSafety
	case "recitation":
		return augmentStopReasonRecitation
	default:
		return augmentStopReasonEndTurn
	}
}

// mapOpenAIFinishReason maps OpenAI finish_reason to Augment stop_reason constants.
// Based on mapOpenAiFinishReasonToAugment from augment-protocol.js
func mapOpenAIFinishReason(fr string) int {
	r := strings.ToLower(strings.TrimSpace(fr))
	switch r {
	case "stop":
		return augmentStopReasonEndTurn
	case "length":
		return augmentStopReasonMaxTokens
	case "tool_calls":
		return augmentStopReasonToolUseRequested
	case "function_call":
		return augmentStopReasonToolUseRequested
	case "content_filter":
		return augmentStopReasonSafety
	default:
		return augmentStopReasonEndTurn
	}
}

type usageAccumulator struct {
	inputTokens              int
	outputTokens             int
	cacheReadInputTokens     int
	cacheCreationInputTokens int

	hasInputTokens              bool
	hasOutputTokens             bool
	hasCacheReadInputTokens     bool
	hasCacheCreationInputTokens bool
}

func (a *usageAccumulator) merge(raw map[string]interface{}) {
	if len(raw) == 0 {
		return
	}

	if v, ok := usageInt(raw, "input_tokens", "prompt_tokens"); ok {
		a.setInputTokens(v)
	}
	if v, ok := usageInt(raw, "output_tokens", "completion_tokens"); ok {
		a.setOutputTokens(v)
	}
	if v, ok := usageInt(raw, "cache_read_input_tokens"); ok {
		a.setCacheReadInputTokens(v)
	}
	if v, ok := usageInt(raw, "cache_creation_input_tokens"); ok {
		a.setCacheCreationInputTokens(v)
	}

	if details, ok := raw["prompt_tokens_details"].(map[string]interface{}); ok {
		if v, ok := usageInt(details, "cached_tokens"); ok {
			a.setCacheReadInputTokens(v)
		}
		if v, ok := usageInt(details, "cache_creation_tokens"); ok {
			a.setCacheCreationInputTokens(v)
		}
	}
}

func (a *usageAccumulator) setInputTokens(v int) {
	if v < 0 {
		return
	}
	if !a.hasInputTokens || v > a.inputTokens {
		a.inputTokens = v
		a.hasInputTokens = true
	}
}

func (a *usageAccumulator) setOutputTokens(v int) {
	if v < 0 {
		return
	}
	if !a.hasOutputTokens || v > a.outputTokens {
		a.outputTokens = v
		a.hasOutputTokens = true
	}
}

func (a *usageAccumulator) setCacheReadInputTokens(v int) {
	if v < 0 {
		return
	}
	if !a.hasCacheReadInputTokens || v > a.cacheReadInputTokens {
		a.cacheReadInputTokens = v
		a.hasCacheReadInputTokens = true
	}
}

func (a *usageAccumulator) setCacheCreationInputTokens(v int) {
	if v < 0 {
		return
	}
	if !a.hasCacheCreationInputTokens || v > a.cacheCreationInputTokens {
		a.cacheCreationInputTokens = v
		a.hasCacheCreationInputTokens = true
	}
}

func (a *usageAccumulator) buildTokenUsage() map[string]interface{} {
	if a == nil {
		return nil
	}

	inputTokens := a.inputTokens
	outputTokens := a.outputTokens
	hasInputTokens := a.hasInputTokens
	hasOutputTokens := a.hasOutputTokens

	tokenUsage := make(map[string]interface{})
	if hasInputTokens {
		tokenUsage["input_tokens"] = inputTokens
	}
	if hasOutputTokens {
		tokenUsage["output_tokens"] = outputTokens
	}
	if a.hasCacheReadInputTokens {
		// Augment may use this field to trigger history summarization too
		// aggressively when prompt caching is enabled, so keep the field but
		// pin it to zero in the plugin-facing NDJSON response.
		tokenUsage["cache_read_input_tokens"] = 0
	}
	if a.hasCacheCreationInputTokens {
		tokenUsage["cache_creation_input_tokens"] = a.cacheCreationInputTokens
	}

	if len(tokenUsage) == 0 {
		return nil
	}
	return tokenUsage
}

func estimateOutputTokensFallback(text string) (int, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	estimated := tokencount.EstimateOutputTokens(text)
	if estimated <= 0 {
		return 0, false
	}
	return estimated, true
}

func applyEstimatedOutputTokens(usageAcc *usageAccumulator, text string) {
	if usageAcc == nil || usageAcc.hasOutputTokens {
		return
	}
	if estimated, ok := estimateOutputTokensFallback(text); ok {
		usageAcc.setOutputTokens(estimated)
	}
}

func normalizePluginFacingTokenUsage(tokenUsage map[string]interface{}) map[string]interface{} {
	if len(tokenUsage) == 0 {
		return tokenUsage
	}
	if _, ok := tokenUsage["cache_read_input_tokens"]; ok {
		tokenUsage["cache_read_input_tokens"] = 0
	}
	return tokenUsage
}

func usageInt(m map[string]interface{}, keys ...string) (int, bool) {
	for _, key := range keys {
		if raw, exists := m[key]; exists {
			if v, ok := toTokenInt(raw); ok {
				return v, true
			}
		}
	}
	return 0, false
}

func toTokenInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		if n < 0 {
			return 0, false
		}
		return n, true
	case int32:
		if n < 0 {
			return 0, false
		}
		return int(n), true
	case int64:
		if n < 0 {
			return 0, false
		}
		return int(n), true
	case float32:
		if n < 0 {
			return 0, false
		}
		return int(n), true
	case float64:
		if n < 0 {
			return 0, false
		}
		return int(n), true
	case json.Number:
		if iv, err := n.Int64(); err == nil && iv >= 0 {
			return int(iv), true
		}
		if fv, err := n.Float64(); err == nil && fv >= 0 {
			return int(fv), true
		}
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, false
		}
		if iv, err := strconv.Atoi(s); err == nil && iv >= 0 {
			return iv, true
		}
		if fv, err := strconv.ParseFloat(s, 64); err == nil && fv >= 0 {
			return int(fv), true
		}
	}
	return 0, false
}

func emitAggregatedTokenUsageNode(w io.Writer, usageAcc *usageAccumulator, nextNodeID *int) bool {
	tokenUsage := usageAcc.buildTokenUsage()
	if len(tokenUsage) == 0 {
		return false
	}

	node := map[string]interface{}{
		"id":          *nextNodeID,
		"type":        augmentNodeTypeTokenUsage,
		"content":     "",
		"token_usage": tokenUsage,
	}
	*nextNodeID++

	chunk := newBaseChunk("")
	chunk["nodes"] = []interface{}{node}
	writeChunkLine(w, chunk)
	return true
}

type openAIToolCallAccum struct {
	id      string
	name    string
	args    strings.Builder
	started bool // Track if TOOL_USE_START has been emitted
}

func streamConvertOpenAISSE(r io.Reader, w io.Writer, toolCtx map[string]*ToolContext) (inputTokens, outputTokens int, err error) {
	nextNodeID := 1

	// index -> accumulated tool call
	acc := map[int]*openAIToolCallAccum{}
	inThinkTag := false // T6: track <think> tag state
	var usageAcc usageAccumulator
	var generatedText strings.Builder
	var pendingReasoning strings.Builder
	var pendingThinkTag strings.Builder
	sawToolUse := false
	stopReasonSeen := false
	stopReason := augmentStopReasonEndTurn
	sawDone := false
	dataEvents := 0
	parsedChunks := 0
	sawThinking := false
	sawVisibleText := false

	flushReasoning := func() {
		if pendingReasoning.Len() == 0 {
			return
		}
		sawThinking = true
		emitThinkingChunk(w, pendingReasoning.String(), "", &nextNodeID)
		pendingReasoning.Reset()
	}

	flushThinkTag := func() {
		if pendingThinkTag.Len() == 0 {
			inThinkTag = false
			return
		}
		sawThinking = true
		emitThinkingChunk(w, pendingThinkTag.String(), "", &nextNodeID)
		pendingThinkTag.Reset()
		inThinkTag = false
	}

	flushAllThinking := func() {
		flushReasoning()
		flushThinkTag()
	}

	err = processSSEEvents(r, func(_ string, data string) error {
		data = strings.TrimSpace(data)
		if data == "" {
			return nil
		}
		if data == "[DONE]" {
			sawDone = true
			return nil
		}
		dataEvents++

		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return nil
		}
		parsedChunks++
		if msgText := extractUpstreamErrorMessage(ev); msgText != "" {
			return fmt.Errorf("augment response: openai upstream error: %s", msgText)
		}
		if usage, ok := ev["usage"].(map[string]interface{}); ok {
			usageAcc.merge(usage)
		}

		choices, _ := ev["choices"].([]interface{})
		if len(choices) == 0 {
			return nil
		}
		choice, _ := choices[0].(map[string]interface{})
		delta, _ := choice["delta"].(map[string]interface{})

		// Handle reasoning/thinking side channels used by different OpenAI-compatible gateways.
		reasoning := ""
		for _, key := range []string{"reasoning", "reasoning_content", "thinking", "thinking_content"} {
			if value, ok := delta[key].(string); ok && value != "" {
				reasoning = value
				break
			}
		}
		if reasoning != "" {
			if pendingReasoning.Len() > 0 && !startsWithWhitespace(reasoning) && !endsWithWhitespace(pendingReasoning.String()) {
				pendingReasoning.WriteByte(' ')
			}
			generatedText.WriteString(reasoning)
			pendingReasoning.WriteString(reasoning)
		}

		// T6: Handle <think> tags in content as THINKING nodes
		if content, ok := delta["content"].(string); ok && content != "" {
			generatedText.WriteString(content)
			flushReasoning()
			remaining := content
			for len(remaining) > 0 {
				if inThinkTag {
					closeIdx := strings.Index(remaining, "</think>")
					if closeIdx == -1 {
						// All remaining is thinking content
						pendingThinkTag.WriteString(remaining)
						remaining = ""
					} else {
						if closeIdx > 0 {
							pendingThinkTag.WriteString(remaining[:closeIdx])
						}
						flushThinkTag()
						inThinkTag = false
						remaining = remaining[closeIdx+len("</think>"):]
					}
				} else {
					openIdx := strings.Index(remaining, "<think>")
					if openIdx == -1 {
						// All remaining is normal text
						sawVisibleText = true
						writeChunkLine(w, newBaseChunk(remaining))
						remaining = ""
					} else {
						if openIdx > 0 {
							sawVisibleText = true
							writeChunkLine(w, newBaseChunk(remaining[:openIdx]))
						}
						inThinkTag = true
						remaining = remaining[openIdx+len("<think>"):]
					}
				}
			}
			return nil
		}

		if toolCalls, ok := delta["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
			flushAllThinking()
			for _, raw := range toolCalls {
				tc, _ := raw.(map[string]interface{})
				idxFloat, _ := tc["index"].(float64)
				idx := int(idxFloat)

				a := acc[idx]
				if a == nil {
					a = &openAIToolCallAccum{}
					acc[idx] = a
				}
				if id, ok := tc["id"].(string); ok && id != "" {
					a.id = id
				}
				fn, _ := tc["function"].(map[string]interface{})
				if name, ok := fn["name"].(string); ok && name != "" {
					a.name = name
				}
				if args, ok := fn["arguments"].(string); ok && args != "" {
					a.args.WriteString(args)
				}
			}
		}
		if functionCall, ok := delta["function_call"].(map[string]interface{}); ok {
			flushAllThinking()
			a := acc[0]
			if a == nil {
				a = &openAIToolCallAccum{}
				acc[0] = a
			}
			if name, ok := functionCall["name"].(string); ok && name != "" {
				a.name = name
			}
			if args, ok := functionCall["arguments"].(string); ok && args != "" {
				a.args.WriteString(args)
			}
		}

		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			flushAllThinking()
			stopReasonSeen = true
			stopReason = mapOpenAIFinishReason(fr)
			if fr == "tool_calls" || fr == "function_call" {
				indexes := make([]int, 0, len(acc))
				for idx := range acc {
					indexes = append(indexes, idx)
				}
				sort.Ints(indexes)
				for _, idx := range indexes {
					a := acc[idx]
					if a == nil {
						continue
					}
					if emitToolUseChunks(w, a.id, a.name, a.args.String(), toolCtx, &nextNodeID) {
						sawToolUse = true
					}
				}
			}
			acc = map[int]*openAIToolCallAccum{}
			return nil
		}
		return nil
	})

	flushAllThinking()

	if len(acc) > 0 {
		indexes := make([]int, 0, len(acc))
		for idx := range acc {
			indexes = append(indexes, idx)
		}
		sort.Ints(indexes)
		for _, idx := range indexes {
			a := acc[idx]
			if a == nil {
				continue
			}
			if emitToolUseChunks(w, a.id, a.name, a.args.String(), toolCtx, &nextNodeID) {
				sawToolUse = true
			}
		}
		acc = map[int]*openAIToolCallAccum{}
	}

	applyEstimatedOutputTokens(&usageAcc, generatedText.String())
	usageEmitted := emitAggregatedTokenUsageNode(w, &usageAcc, &nextNodeID)
	if !sawVisibleText && !sawThinking && !sawToolUse && !usageEmitted && !stopReasonSeen {
		return 0, 0, fmt.Errorf("augment response: openai sse produced no parseable content (data_events=%d, parsed_chunks=%d)", dataEvents, parsedChunks)
	}
	emitFinalStopChunk(w, stopReasonSeen, stopReason, sawToolUse, sawDone || stopReasonSeen || sawToolUse)

	// Extract final token counts using buildTokenUsage to ensure consistency with emitted usage
	tokenUsage := usageAcc.buildTokenUsage()
	if tokenUsage != nil {
		if v, ok := tokenUsage["input_tokens"].(int); ok {
			inputTokens = v
		}
		if v, ok := tokenUsage["output_tokens"].(int); ok {
			outputTokens = v
		}
	}
	return inputTokens, outputTokens, err
}

type openAIResponsesToolCallAccum struct {
	callID    string
	name      string
	arguments strings.Builder
}

type openAIResponsesReasoningAccum struct {
	summary          strings.Builder
	openaiID         string
	encryptedContent string
	metadata         map[string]interface{}
}

func streamConvertOpenAIResponsesSSE(r io.Reader, w io.Writer, toolCtx map[string]*ToolContext) (inputTokens, outputTokens int, err error) {
	nextNodeID := 1
	var usageAcc usageAccumulator
	var generatedText strings.Builder
	var reasoning openAIResponsesReasoningAccum
	textByIndex := make(map[int]string)
	toolCallsByIndex := make(map[int]*openAIResponsesToolCallAccum)
	responsesToolIndexByID := make(map[string]int)
	fallbackToolIndex := 1000000
	var finalResponse map[string]interface{}
	sawToolUse := false
	stopReasonSeen := false
	stopReason := augmentStopReasonEndTurn
	sawDone := false
	dataEvents := 0
	parsedChunks := 0
	sawThinking := false
	sawVisibleText := false

	extractOutputIndex := func(raw map[string]interface{}) (int, bool) {
		if raw == nil {
			return 0, false
		}
		for _, key := range []string{"output_index", "outputIndex", "index"} {
			if _, ok := raw[key]; ok {
				return firstInt(raw, key), true
			}
		}
		return 0, false
	}

	mergeToolCallAccum := func(dst, src *openAIResponsesToolCallAccum) {
		if dst == nil || src == nil || dst == src {
			return
		}
		if dst.callID == "" && src.callID != "" {
			dst.callID = src.callID
		}
		if dst.name == "" && src.name != "" {
			dst.name = src.name
		}
		srcArgs := src.arguments.String()
		if srcArgs != "" {
			dstArgs := dst.arguments.String()
			if dstArgs == "" || len(srcArgs) > len(dstArgs) {
				dst.arguments.Reset()
				dst.arguments.WriteString(srcArgs)
			}
		}
	}

	ensureToolCall := func(idx int, hasIndex bool, callID string) *openAIResponsesToolCallAccum {
		targetIdx := idx
		if hasIndex && targetIdx < 0 {
			hasIndex = false
		}
		if callID != "" {
			if mappedIdx, ok := responsesToolIndexByID[callID]; ok {
				if hasIndex {
					if mappedIdx != targetIdx {
						if existing := toolCallsByIndex[mappedIdx]; existing != nil {
							if toolCallsByIndex[targetIdx] == nil {
								toolCallsByIndex[targetIdx] = existing
							} else {
								mergeToolCallAccum(toolCallsByIndex[targetIdx], existing)
							}
							delete(toolCallsByIndex, mappedIdx)
						}
					}
				} else {
					targetIdx = mappedIdx
					hasIndex = true
				}
			} else if !hasIndex {
				targetIdx = fallbackToolIndex
				fallbackToolIndex++
				hasIndex = true
			}
			responsesToolIndexByID[callID] = targetIdx
		}
		if !hasIndex {
			targetIdx = 0
		}
		if toolCallsByIndex[targetIdx] == nil {
			toolCallsByIndex[targetIdx] = &openAIResponsesToolCallAccum{}
		}
		return toolCallsByIndex[targetIdx]
	}

	appendRemainingText := func(idx int, full string) {
		if strings.TrimSpace(full) == "" {
			return
		}
		current := textByIndex[idx]
		rest := full
		if strings.HasPrefix(full, current) {
			rest = full[len(current):]
		} else if full == current {
			rest = ""
		}
		if rest == "" {
			return
		}
		textByIndex[idx] = current + rest
		generatedText.WriteString(rest)
		sawVisibleText = true
		writeChunkLine(w, newBaseChunk(rest))
	}

	err = processSSEEvents(r, func(lineEventType string, data string) error {
		data = strings.TrimSpace(data)
		if data == "" {
			return nil
		}
		if data == "[DONE]" {
			sawDone = true
			return nil
		}
		dataEvents++

		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return nil
		}
		parsedChunks++
		eventType, _ := ev["type"].(string)
		if eventType == "" {
			eventType = lineEventType
		}

		switch eventType {
		case "response.output_item.added", "response.output_item.done":
			item, _ := ev["item"].(map[string]interface{})
			outputIndex, hasOutputIndex := extractOutputIndex(ev)
			if !hasOutputIndex {
				outputIndex, hasOutputIndex = extractOutputIndex(item)
			}
			if itemType, _ := item["type"].(string); itemType == "function_call" {
				callID := firstString(item, "call_id", "callId", "callID")
				acc := ensureToolCall(outputIndex, hasOutputIndex, callID)
				if callID != "" {
					acc.callID = callID
				}
				if name := firstString(item, "name"); name != "" {
					acc.name = name
				}
				if args := firstString(item, "arguments"); args != "" {
					acc.arguments.Reset()
					acc.arguments.WriteString(args)
				}
			}
			if itemType, _ := item["type"].(string); itemType == "reasoning" && reasoning.summary.Len() == 0 {
				mergeResponsesReasoningMeta(&reasoning, item)
				if summary := extractResponsesReasoningSummary(item); summary != "" {
					sawThinking = true
					reasoning.summary.WriteString(summary)
				}
			}

		case "response.function_call_arguments.delta":
			outputIndex, hasOutputIndex := extractOutputIndex(ev)
			callID := firstString(ev, "call_id", "callId", "callID", "id")
			acc := ensureToolCall(outputIndex, hasOutputIndex, callID)
			if callID != "" {
				acc.callID = callID
			}
			if name := firstString(ev, "name"); name != "" {
				acc.name = name
			}
			if delta, _ := ev["delta"].(string); delta != "" {
				acc.arguments.WriteString(delta)
			}

		case "response.function_call_arguments.done":
			outputIndex, hasOutputIndex := extractOutputIndex(ev)
			callID := firstString(ev, "call_id", "callId", "callID", "id")
			acc := ensureToolCall(outputIndex, hasOutputIndex, callID)
			if callID != "" {
				acc.callID = callID
			}
			if name := firstString(ev, "name"); name != "" {
				acc.name = name
			}
			if args, _ := ev["arguments"].(string); args != "" {
				acc.arguments.Reset()
				acc.arguments.WriteString(args)
			}

		case "response.output_text.delta":
			idx := firstInt(ev, "output_index", "outputIndex", "index")
			if delta, _ := ev["delta"].(string); delta != "" {
				textByIndex[idx] += delta
				generatedText.WriteString(delta)
				sawVisibleText = true
				writeChunkLine(w, newBaseChunk(delta))
			}

		case "response.output_text.done":
			idx := firstInt(ev, "output_index", "outputIndex", "index")
			appendRemainingText(idx, firstString(ev, "text"))

		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			if delta, _ := ev["delta"].(string); delta != "" {
				sawThinking = true
				reasoning.summary.WriteString(delta)
			}
			mergeResponsesReasoningMeta(&reasoning, ev)

		case "response.reasoning_summary_text.done":
			mergeResponsesReasoningMeta(&reasoning, ev)
			if full := firstString(ev, "text"); full != "" {
				sawThinking = true
				if reasoning.summary.Len() == 0 {
					reasoning.summary.WriteString(full)
				} else if !strings.Contains(reasoning.summary.String(), full) {
					reasoning.summary.WriteString(full)
				}
			}

		case "response.incomplete":
			if resp, ok := ev["response"].(map[string]interface{}); ok {
				finalResponse = resp
				usageAcc.merge(extractResponsesUsage(resp))
				stopReasonSeen, stopReason = extractResponsesStopReason(resp)
			}

		case "response.completed":
			if resp, ok := ev["response"].(map[string]interface{}); ok {
				finalResponse = resp
				usageAcc.merge(extractResponsesUsage(resp))
				stopReasonSeen, stopReason = extractResponsesStopReason(resp)
				fullText := firstString(resp, "output_text", "outputText", "text")
				if fullText == "" {
					fullText = extractResponsesTextFromOutput(resp["output"])
				}
				appendRemainingText(0, fullText)
			}

		case "response.failed", "response.error", "error":
			if msgText := extractUpstreamErrorMessage(ev); msgText != "" {
				return fmt.Errorf("augment response: openai responses upstream error: %s", msgText)
			}
			return fmt.Errorf("augment response: openai responses upstream error")
		}
		return nil
	})

	if finalResponse != nil {
		finalText := firstString(finalResponse, "output_text", "outputText", "text")
		if finalText == "" {
			finalText = extractResponsesTextFromOutput(finalResponse["output"])
		}
		appendRemainingText(0, finalText)
		mergeResponsesReasoningMeta(&reasoning, extractResponsesReasoningItemFromOutput(finalResponse["output"]))
		if summary := extractResponsesReasoningSummaryFromOutput(finalResponse["output"]); summary != "" && reasoning.summary.Len() == 0 {
			reasoning.summary.WriteString(summary)
		}
		for seq, tc := range extractResponsesToolCalls(finalResponse["output"]) {
			idx := tc.index
			hasIndex := tc.hasIndex
			if !hasIndex {
				if mappedIdx, ok := responsesToolIndexByID[tc.callID]; ok {
					idx = mappedIdx
					hasIndex = true
				} else if existing := toolCallsByIndex[seq]; existing != nil && (existing.callID == tc.callID || existing.callID == "") {
					idx = seq
					hasIndex = true
				}
			}
			acc := ensureToolCall(idx, hasIndex, tc.callID)
			if tc.callID != "" {
				acc.callID = tc.callID
			}
			if tc.name != "" {
				acc.name = tc.name
			}
			if tc.arguments != "" {
				acc.arguments.Reset()
				acc.arguments.WriteString(tc.arguments)
			}
		}
	}

	if reasoning.summary.Len() > 0 || reasoning.openaiID != "" || reasoning.encryptedContent != "" || len(reasoning.metadata) > 0 {
		emitThinkingNodeChunk(w, buildThinkingNodePayload(reasoning.summary.String(), "", reasoning.openaiID, reasoning.encryptedContent, reasoning.metadata), &nextNodeID)
	}

	indexes := make([]int, 0, len(toolCallsByIndex))
	for idx := range toolCallsByIndex {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	for _, idx := range indexes {
		acc := toolCallsByIndex[idx]
		if acc == nil {
			continue
		}
		if emitToolUseChunks(w, acc.callID, acc.name, acc.arguments.String(), toolCtx, &nextNodeID) {
			sawToolUse = true
		}
	}

	applyEstimatedOutputTokens(&usageAcc, generatedText.String())
	usageEmitted := emitAggregatedTokenUsageNode(w, &usageAcc, &nextNodeID)
	if !sawVisibleText && !sawThinking && !sawToolUse && !usageEmitted {
		if finalResponse == nil && !sawDone && !stopReasonSeen {
			return 0, 0, fmt.Errorf("augment response: openai responses sse produced no parseable content (data_events=%d, parsed_chunks=%d)", dataEvents, parsedChunks)
		}
	}
	emitFinalStopChunk(w, stopReasonSeen, stopReason, sawToolUse, sawDone || finalResponse != nil || stopReasonSeen)

	tokenUsage := usageAcc.buildTokenUsage()
	if tokenUsage != nil {
		if v, ok := tokenUsage["input_tokens"].(int); ok {
			inputTokens = v
		}
		if v, ok := tokenUsage["output_tokens"].(int); ok {
			outputTokens = v
		}
	}
	return inputTokens, outputTokens, err
}

func extractResponsesUsage(resp map[string]interface{}) map[string]interface{} {
	if resp == nil {
		return nil
	}
	usage, _ := resp["usage"].(map[string]interface{})
	return usage
}

func extractResponsesStopReason(resp map[string]interface{}) (bool, int) {
	if resp == nil {
		return false, augmentStopReasonEndTurn
	}
	status := strings.ToLower(strings.TrimSpace(firstString(resp, "status")))
	details := firstMap(resp, "incomplete_details", "incompleteDetails")
	reason := strings.ToLower(strings.TrimSpace(firstString(details, "reason")))
	if status != "incomplete" && reason == "" {
		return false, augmentStopReasonEndTurn
	}
	switch reason {
	case "max_output_tokens", "max_tokens", "length":
		return true, augmentStopReasonMaxTokens
	case "content_filter", "contentfilter", "safety":
		return true, augmentStopReasonSafety
	default:
		return true, augmentStopReasonUnspecified
	}
}

func extractResponsesReasoningSummary(item map[string]interface{}) string {
	summary, _ := item["summary"].([]interface{})
	var parts []string
	for _, raw := range summary {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if firstString(block, "type") != "summary_text" {
			continue
		}
		if text := firstString(block, "text"); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func extractResponsesReasoningSummaryFromOutput(output interface{}) string {
	items, _ := output.([]interface{})
	var parts []string
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if firstString(item, "type") != "reasoning" {
			continue
		}
		if summary := extractResponsesReasoningSummary(item); summary != "" {
			parts = append(parts, summary)
		}
	}
	return strings.Join(parts, "\n")
}

func extractResponsesReasoningItemFromOutput(output interface{}) map[string]interface{} {
	items, _ := output.([]interface{})
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if firstString(item, "type") == "reasoning" {
			return item
		}
	}
	return nil
}

func mergeResponsesReasoningMeta(acc *openAIResponsesReasoningAccum, item map[string]interface{}) {
	if acc == nil || item == nil {
		return
	}
	if id := firstString(item, "id", "item_id", "itemId"); id != "" && acc.openaiID == "" {
		acc.openaiID = id
	}
	if encrypted := firstString(item, "encrypted_content", "encryptedContent"); encrypted != "" && acc.encryptedContent == "" {
		acc.encryptedContent = encrypted
	}
	if meta := cloneResponsesReasoningMetadata(item); len(meta) > 0 {
		if acc.metadata == nil {
			acc.metadata = meta
			return
		}
		for key, value := range meta {
			if _, exists := acc.metadata[key]; !exists {
				acc.metadata[key] = value
			}
		}
	}
}

func cloneResponsesReasoningMetadata(item map[string]interface{}) map[string]interface{} {
	if len(item) == 0 {
		return nil
	}
	meta := make(map[string]interface{})
	for key, value := range item {
		switch key {
		case "type", "summary", "id", "encrypted_content", "encryptedContent":
			continue
		default:
			meta[key] = cloneJSONValue(value)
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func extractResponsesTextFromOutput(output interface{}) string {
	items, _ := output.([]interface{})
	var parts []string
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch firstString(item, "type") {
		case "message":
			content, _ := item["content"].([]interface{})
			for _, rawBlock := range content {
				block, ok := rawBlock.(map[string]interface{})
				if !ok {
					continue
				}
				switch firstString(block, "type") {
				case "output_text", "text":
					if text := firstString(block, "text"); text != "" {
						parts = append(parts, text)
					}
				}
			}
		case "output_text", "text":
			if text := firstString(item, "text"); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "")
}

type responsesToolCall struct {
	index     int
	hasIndex  bool
	callID    string
	name      string
	arguments string
}

func extractResponsesToolCalls(output interface{}) []responsesToolCall {
	items, _ := output.([]interface{})
	out := make([]responsesToolCall, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if firstString(item, "type") != "function_call" {
			continue
		}
		name := firstString(item, "name")
		if name == "" {
			continue
		}
		tc := responsesToolCall{
			callID:    firstString(item, "call_id", "callId", "callID"),
			name:      name,
			arguments: firstString(item, "arguments"),
		}
		if tc.arguments == "" {
			tc.arguments = "{}"
		}
		for _, key := range []string{"output_index", "outputIndex", "index"} {
			if _, ok := item[key]; ok {
				tc.index = firstInt(item, key)
				tc.hasIndex = true
				break
			}
		}
		out = append(out, tc)
	}
	return out
}
