package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lich0821/ccNexus/internal/config"
	newcursor "github.com/lich0821/ccNexus/internal/cursorbridge"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/tokencount"
	"github.com/lich0821/ccNexus/internal/transformer"
)

// handleStreamingResponse processes streaming SSE responses.
// Returns: inputTokens, outputTokens, outputText, originalResponse, transformedResponse.
func (p *Proxy) handleStreamingResponse(w http.ResponseWriter, resp *http.Response, endpoint config.Endpoint, trans transformer.Transformer, transformerName string, thinkingEnabled bool, modelName string, bodyBytes []byte, credentialID int64, requestMeta proxyRequestMeta) (int, int, string, []byte, []byte) {
	defer resp.Body.Close()

	// Copy response headers except Content-Length and Content-Encoding
	for key, values := range resp.Header {
		if key == "Content-Length" || key == "Content-Encoding" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if strings.TrimSpace(w.Header().Get("Content-Type")) == "" {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	}
	if requestMeta.CursorMode {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
	}
	w.WriteHeader(resp.StatusCode)

	flusher, ok := w.(http.Flusher)
	if !ok {
		logger.Error("[%s] ResponseWriter does not support flushing", endpoint.Name)
		return 0, 0, "", nil, nil
	}

	// Handle gzip-encoded response body
	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			logger.Error("[%s] Failed to create gzip reader: %v", endpoint.Name, err)
			return 0, 0, "", nil, nil
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	// Create stream context for all transformers except pure passthrough
	var streamCtx *transformer.StreamContext
	switch transformerName {
	case "cx_chat_openai", "cx_resp_openai2":
		// Pure passthrough - no context needed
	default:
		// cc_claude needs context for input_tokens fallback
		streamCtx = transformer.NewStreamContext()
		streamCtx.ModelName = modelName
		// Pre-estimate input tokens for fallback
		if bodyBytes != nil {
			streamCtx.InputTokens = p.estimateInputTokens(bodyBytes)
		}
	}

	lineReader := bufio.NewReaderSize(reader, 128*1024)

	var inputTokens, outputTokens int
	var buffer bytes.Buffer
	var outputText strings.Builder
	var originalRespBuffer bytes.Buffer
	var transformedRespBuffer bytes.Buffer
	eventCount := 0
	streamDone := false
	doneSeen := false
	cursorChatMeaningfulEventWritten := false
	isRecording := p.trafficRecorder != nil && p.trafficRecorder.IsRecording()
	var readErr error

	if prefix := newcursor.PrefixStream(
		requestMeta.cursorRequestMeta(),
		requestMeta.CursorState,
		firstNonEmptyString(modelName, requestMeta.ClientModel),
		requestMeta.TransformerName,
	); len(prefix) > 0 {
		if _, writeErr := w.Write(prefix); writeErr == nil {
			flusher.Flush()
			if isRecording {
				transformedRespBuffer.Write(prefix)
			}
		}
	}

	for !streamDone {
		line, err := readStreamLine(lineReader)
		if err != nil {
			if line == "" {
				readErr = err
				break
			}
			readErr = err
		}

		if !p.isCurrentEndpoint(endpoint.Name) {
			logger.Warn("[%s] Endpoint switched during streaming, terminating stream gracefully", endpoint.Name)
			streamDone = true
			break
		}

		if strings.Contains(line, "data: [DONE]") {
			streamDone = true
			doneSeen = true

			// Token Usage Fallback: Inject message_delta with estimated output_tokens before [DONE]
			// Non-cursor fallback only: keep legacy token estimation for generic clients.
			if !requestMeta.CursorMode && outputTokens == 0 && outputText.Len() > 0 {
				outputTokens = tokencount.EstimateOutputTokens(outputText.String())
				logger.Debug("[%s] Token fallback before [DONE]: estimated output_tokens=%d", endpoint.Name, outputTokens)

				// Update stream context for transformer fallback
				if streamCtx != nil {
					streamCtx.OutputTokens = outputTokens
				}

				// Inject message_delta event with usage
				deltaEvent := fmt.Sprintf("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":%d}}\n\n", outputTokens)
				if _, writeErr := w.Write([]byte(deltaEvent)); writeErr == nil {
					flusher.Flush()
				}
			}

			buffer.WriteString(line + "\n")
			eventData := buffer.Bytes()
			logger.DebugLog("[%s] SSE Event #%d (Original): %s", endpoint.Name, eventCount+1, string(eventData))
			if isRecording {
				originalRespBuffer.Write(eventData)
			}

			transformedEvent, err := p.transformStreamEvent(eventData, trans, transformerName, streamCtx, requestMeta, firstNonEmptyString(modelName, requestMeta.ClientModel))
			if requestMeta.CursorMode && requestMeta.ClientFormat == ClientFormatOpenAIChat {
				if inputTokens == 0 {
					inputTokens = p.estimateInputTokens(bodyBytes)
				}
				if outputTokens == 0 && outputText.Len() > 0 {
					outputTokens = tokencount.EstimateOutputTokens(outputText.String())
				}
				if usageChunk := buildCursorChatUsageFallbackChunk(modelName, requestMeta.CursorState, inputTokens, outputTokens); len(usageChunk) > 0 {
					if _, err := w.Write(usageChunk); err == nil {
						flusher.Flush()
						if isRecording {
							transformedRespBuffer.Write(usageChunk)
						}
						if requestMeta.CursorState != nil {
							requestMeta.CursorState.ChatUsageSeen = true
						}
					}
				}
			}
			if err == nil && len(transformedEvent) > 0 {
				transformedEvent, err = newcursor.FixStream(
					requestMeta.cursorRequestMeta(),
					transformedEvent,
					func(bundle []byte) ([]byte, error) { return fixCursorStreamBundle(bundle, requestMeta) },
				)
			}
			if err == nil && len(transformedEvent) > 0 {
				logger.DebugLog("[%s] SSE Event #%d (Transformed): %s", endpoint.Name, eventCount+1, string(transformedEvent))
				if isRecording {
					transformedRespBuffer.Write(transformedEvent)
				}
				if requestMeta.CursorMode && requestMeta.ClientFormat == ClientFormatOpenAIChat && cursorChatBundleHasUsage(transformedEvent) && requestMeta.CursorState != nil {
					requestMeta.CursorState.ChatUsageSeen = true
				}
				if _, writeErr := w.Write(transformedEvent); writeErr == nil {
					flusher.Flush()
					if bytes.Contains(transformedEvent, []byte("data: [DONE]")) {
						doneSeen = true
					}
					if requestMeta.CursorMode && requestMeta.ClientFormat == ClientFormatOpenAIChat && cursorChatBundleHasMeaningfulPayload(transformedEvent) {
						cursorChatMeaningfulEventWritten = true
					}
				}
			}
			break
		}

		buffer.WriteString(line + "\n")

		if line == "" {
			eventCount++
			eventData := buffer.Bytes()
			logger.DebugLog("[%s] SSE Event #%d (Original): %s", endpoint.Name, eventCount, string(eventData))
			if isRecording {
				originalRespBuffer.Write(eventData)
			}

			p.captureCodexRateLimitsFromEvent(endpoint, credentialID, eventData)

			// Extract usage from original upstream events first. Some transformers may
			// not preserve usage fields in transformed events.
			p.extractTokensFromEvent(eventData, &inputTokens, &outputTokens)

			// Check if this is a message_stop event (Token Usage Fallback)
			isMessageStop := p.isMessageStopEvent(eventData)
			// Non-cursor fallback only: avoid synthetic usage events on Cursor flows.
			if !requestMeta.CursorMode && isMessageStop && outputTokens == 0 && outputText.Len() > 0 {
				outputTokens = tokencount.EstimateOutputTokens(outputText.String())
				logger.Debug("[%s] Token fallback before message_stop: estimated output_tokens=%d", endpoint.Name, outputTokens)

				// Update stream context for transformer fallback
				if streamCtx != nil {
					streamCtx.OutputTokens = outputTokens
				}

				// Inject message_delta event with usage before message_stop
				deltaEvent := fmt.Sprintf("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":%d}}\n\n", outputTokens)
				if _, writeErr := w.Write([]byte(deltaEvent)); writeErr == nil {
					flusher.Flush()
				}
			}

			transformedEvent, err := p.transformStreamEvent(eventData, trans, transformerName, streamCtx, requestMeta, firstNonEmptyString(modelName, requestMeta.ClientModel))
			if err != nil {
				logger.Error("[%s] Failed to transform SSE event: %v", endpoint.Name, err)
			} else {
				transformedEvent, err = newcursor.FixStream(
					requestMeta.cursorRequestMeta(),
					transformedEvent,
					func(bundle []byte) ([]byte, error) { return fixCursorStreamBundle(bundle, requestMeta) },
				)
				if err != nil {
					logger.Error("[%s] Failed to apply cursor SSE compatibility: %v", endpoint.Name, err)
					continue
				}
			}
			if err == nil && len(transformedEvent) > 0 {
				logger.DebugLog("[%s] SSE Event #%d (Transformed): %s", endpoint.Name, eventCount, string(transformedEvent))
				if isRecording {
					transformedRespBuffer.Write(transformedEvent)
				}

				p.extractTokensFromEvent(transformedEvent, &inputTokens, &outputTokens)
				p.extractTextFromEvent(transformedEvent, &outputText)
				if requestMeta.CursorMode && requestMeta.ClientFormat == ClientFormatOpenAIChat && cursorChatBundleHasUsage(transformedEvent) && requestMeta.CursorState != nil {
					requestMeta.CursorState.ChatUsageSeen = true
				}

				if _, writeErr := w.Write(transformedEvent); writeErr != nil {
					// Client disconnected (broken pipe) is normal for cancelled requests
					if strings.Contains(writeErr.Error(), "broken pipe") || strings.Contains(writeErr.Error(), "connection reset") {
						logger.Debug("[%s] Client disconnected: %v", endpoint.Name, writeErr)
					} else {
						logger.Error("[%s] Failed to write transformed event: %v", endpoint.Name, writeErr)
					}
					streamDone = true
					break
				}
				flusher.Flush()
				if bytes.Contains(transformedEvent, []byte("data: [DONE]")) {
					doneSeen = true
				}
				if requestMeta.CursorMode && requestMeta.ClientFormat == ClientFormatOpenAIChat && cursorChatBundleHasMeaningfulPayload(transformedEvent) {
					cursorChatMeaningfulEventWritten = true
				}
			}
			buffer.Reset()
		}

		if readErr != nil {
			break
		}
	}

	if buffer.Len() > 0 && !streamDone {
		eventCount++
		eventData := append([]byte(nil), buffer.Bytes()...)
		logger.DebugLog("[%s] SSE Event #%d (Original, partial): %s", endpoint.Name, eventCount, string(eventData))
		if isRecording {
			originalRespBuffer.Write(eventData)
		}

		p.captureCodexRateLimitsFromEvent(endpoint, credentialID, eventData)
		p.extractTokensFromEvent(eventData, &inputTokens, &outputTokens)

		transformedEvent, err := p.transformStreamEvent(eventData, trans, transformerName, streamCtx, requestMeta, firstNonEmptyString(modelName, requestMeta.ClientModel))
		if err != nil {
			logger.Error("[%s] Failed to transform partial SSE event: %v", endpoint.Name, err)
		} else {
			transformedEvent, err = newcursor.FixStream(
				requestMeta.cursorRequestMeta(),
				transformedEvent,
				func(bundle []byte) ([]byte, error) { return fixCursorStreamBundle(bundle, requestMeta) },
			)
			if err != nil {
				logger.Error("[%s] Failed to apply cursor partial SSE compatibility: %v", endpoint.Name, err)
			}
		}
		if err == nil && len(transformedEvent) > 0 {
			logger.DebugLog("[%s] SSE Event #%d (Transformed, partial): %s", endpoint.Name, eventCount, string(transformedEvent))
			if isRecording {
				transformedRespBuffer.Write(transformedEvent)
			}
			p.extractTokensFromEvent(transformedEvent, &inputTokens, &outputTokens)
			p.extractTextFromEvent(transformedEvent, &outputText)
			if requestMeta.CursorMode && requestMeta.ClientFormat == ClientFormatOpenAIChat && cursorChatBundleHasUsage(transformedEvent) && requestMeta.CursorState != nil {
				requestMeta.CursorState.ChatUsageSeen = true
			}
			if _, writeErr := w.Write(transformedEvent); writeErr != nil {
				if strings.Contains(writeErr.Error(), "broken pipe") || strings.Contains(writeErr.Error(), "connection reset") {
					logger.Debug("[%s] Client disconnected while writing partial event: %v", endpoint.Name, writeErr)
				} else {
					logger.Error("[%s] Failed to write transformed partial event: %v", endpoint.Name, writeErr)
				}
			} else {
				flusher.Flush()
				if bytes.Contains(transformedEvent, []byte("data: [DONE]")) {
					doneSeen = true
				}
				if requestMeta.CursorMode && requestMeta.ClientFormat == ClientFormatOpenAIChat && cursorChatBundleHasMeaningfulPayload(transformedEvent) {
					cursorChatMeaningfulEventWritten = true
				}
			}
		}
		buffer.Reset()
	}

	if doneSeen {
		readErr = nil
	}
	if err := readErr; err != nil {
		errMsg := err.Error()
		if isGracefulStreamReadError(err) {
			if !doneSeen {
				p.closeIdleUpstreamConnections()
			}
			logger.Warn("[%s] Upstream stream ended before clean terminator, finalizing partial response: %v", endpoint.Name, err)
		} else if strings.Contains(errMsg, "stream error") || strings.Contains(errMsg, "INTERNAL_ERROR") {
			// Check if it's an HTTP/2 stream error
			requestSize := len(bodyBytes)
			sizeStr := formatRequestSize(requestSize)
			logger.Error("[%s] HTTP/2 stream error (Request size: %s / %d bytes): %v",
				endpoint.Name, sizeStr, requestSize, err)

			// Provide context based on request size
			if requestSize > 100*1024 { // > 100KB
				logger.Warn("[%s] Large request detected (%s). Consider: 1) Reading fewer files at once, 2) Using smaller code sections, 3) Breaking task into smaller requests",
					endpoint.Name, sizeStr)
			} else {
				logger.Warn("[%s] This error may occur due to upstream server limitations or network issues.", endpoint.Name)
			}
		} else {
			logger.Error("[%s] Stream reader error: %v", endpoint.Name, err)
		}
	}

	if requestMeta.CursorMode && requestMeta.ClientFormat == ClientFormatOpenAIChat {
		if inputTokens == 0 {
			inputTokens = p.estimateInputTokens(bodyBytes)
		}
		if outputTokens == 0 && outputText.Len() > 0 {
			outputTokens = tokencount.EstimateOutputTokens(outputText.String())
		}
		if usageChunk := buildCursorChatUsageFallbackChunk(modelName, requestMeta.CursorState, inputTokens, outputTokens); len(usageChunk) > 0 {
			if _, err := w.Write(usageChunk); err == nil {
				flusher.Flush()
				if isRecording {
					transformedRespBuffer.Write(usageChunk)
				}
				if requestMeta.CursorState != nil {
					requestMeta.CursorState.ChatUsageSeen = true
				}
			}
		}
		if finalizeChunk := newcursor.FinalizeStream(
			requestMeta.cursorRequestMeta(),
			requestMeta.CursorState,
			modelName,
		); len(finalizeChunk) > 0 {
			if _, err := w.Write(finalizeChunk); err == nil {
				flusher.Flush()
				if isRecording {
					transformedRespBuffer.Write(finalizeChunk)
				}
			}
		}
		if requestMeta.CursorState != nil {
			requestMeta.CursorState.InThinkingTag = false
		}
	}
	if requestMeta.CursorMode && requestMeta.ClientFormat == ClientFormatOpenAIChat && cursorChatMeaningfulEventWritten && !doneSeen && isGracefulStreamReadError(readErr) {
		if _, err := w.Write([]byte("data: [DONE]\n\n")); err == nil {
			flusher.Flush()
			if isRecording {
				transformedRespBuffer.WriteString("data: [DONE]\n\n")
			}
		}
	}

	// Non-cursor fallback only: Cursor path aligns with api2cursor and skips auto-continue.
	if !requestMeta.CursorMode && outputText.Len() > 0 {
		reqCtx := context.Background()
		if resp.Request != nil {
			reqCtx = resp.Request.Context()
		}
		continuation, err := p.autoContinueCursorResponseStream(reqCtx, outputText.String(), bodyBytes, &requestMeta)
		if err == nil && continuation != "" {
			w.Write([]byte(continuation))
			flusher.Flush()
		}
	}

	return inputTokens, outputTokens, outputText.String(), originalRespBuffer.Bytes(), transformedRespBuffer.Bytes()
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func cursorChatBundleHasMeaningfulPayload(bundle []byte) bool {
	blocks := bytes.Split(bundle, []byte("\n\n"))
	for _, block := range blocks {
		for _, line := range bytes.Split(block, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if !bytes.HasPrefix(line, []byte("data: ")) {
				continue
			}
			payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data: ")))
			if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
				continue
			}
			if cursorChatPayloadHasMeaningfulContent(payload) {
				return true
			}
		}
	}
	return false
}

func cursorChatBundleHasUsage(bundle []byte) bool {
	blocks := bytes.Split(bundle, []byte("\n\n"))
	for _, block := range blocks {
		for _, line := range bytes.Split(block, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if !bytes.HasPrefix(line, []byte("data: ")) {
				continue
			}
			payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data: ")))
			if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
				continue
			}
			var chunk map[string]interface{}
			if err := json.Unmarshal(payload, &chunk); err != nil {
				continue
			}
			if _, ok := chunk["usage"].(map[string]interface{}); ok {
				return true
			}
		}
	}
	return false
}

func cursorChatPayloadHasMeaningfulContent(payload []byte) bool {
	var chunk map[string]interface{}
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return false
	}

	choices, ok := chunk["choices"].([]interface{})
	if !ok {
		return false
	}
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]interface{})
		if !ok {
			continue
		}
		if finishReason, ok := choice["finish_reason"].(string); ok && finishReason != "" {
			return true
		}
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		if content, ok := delta["content"].(string); ok && content != "" {
			return true
		}
		if reasoning, ok := delta["reasoning_content"].(string); ok && reasoning != "" {
			return true
		}
		if toolCalls, ok := delta["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
			return true
		}
	}
	return false
}

func buildCursorChatUsageFallbackChunk(modelName string, state *newcursor.StreamFinalizeState, inputTokens, outputTokens int) []byte {
	if state != nil && state.ChatUsageSeen {
		return nil
	}
	if inputTokens <= 0 && outputTokens <= 0 {
		return nil
	}
	payload := map[string]interface{}{
		"id":     "",
		"object": "chat.completion.chunk",
		"model":  modelName,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     inputTokens,
			"completion_tokens": outputTokens,
			"total_tokens":      inputTokens + outputTokens,
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	// NOTE: Do not set state.ChatUsageSeen here.
	// Caller must set it after successful write to avoid skipping retry on write failure.
	return []byte("data: " + string(encoded) + "\n\n")
}

// handleStreamingAsNonStreaming aggregates SSE and returns a single non-stream response.
// This is used for Codex endpoints that require stream=true upstream while client requested non-stream.
func (p *Proxy) handleStreamingAsNonStreaming(w http.ResponseWriter, resp *http.Response, endpoint config.Endpoint, trans transformer.Transformer, credentialID int64, requestMeta proxyRequestMeta) (int, int, string, error) {
	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			resp.Body.Close()
			return 0, 0, "", err
		}
		defer gzipReader.Close()
		reader = gzipReader
	}
	defer resp.Body.Close()

	var completedPayload []byte
	var lastTerminalPayload []byte
	lineReader := bufio.NewReaderSize(reader, 128*1024)
	var readErr error
	for {
		line, err := readStreamLine(lineReader)
		if err != nil {
			if line == "" {
				readErr = err
				break
			}
			readErr = err
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			if readErr != nil {
				break
			}
			continue
		}
		jsonData := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if jsonData == "" || jsonData == "[DONE]" {
			if readErr != nil {
				break
			}
			continue
		}
		p.captureCodexRateLimitsFromEvent(endpoint, credentialID, []byte("data: "+jsonData+"\n\n"))

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			continue
		}
		terminalPayload, isTerminal, err := extractAggregateTerminalPayload([]byte(jsonData), event)
		if err != nil {
			return 0, 0, "", err
		}
		if isTerminal {
			lastTerminalPayload = terminalPayload
		}
		if eventType, _ := event["type"].(string); eventType != "response.completed" {
			continue
		}
		completedPayload = terminalPayload
		break
	}
	if readErr != nil && !isGracefulStreamReadError(readErr) {
		return 0, 0, "", readErr
	}
	if readErr != nil && len(completedPayload) == 0 && len(lastTerminalPayload) == 0 {
		if isGracefulStreamReadError(readErr) {
			return 0, 0, "", fmt.Errorf("stream closed before response.completed: %w", readErr)
		}
		return 0, 0, "", readErr
	}
	if err := readErr; err != nil && isGracefulStreamReadError(err) && (len(completedPayload) > 0 || len(lastTerminalPayload) > 0) {
		logger.Warn("[%s] Aggregated upstream stream ended early after completed payload: %v", endpoint.Name, err)
	}
	if len(completedPayload) == 0 {
		if len(lastTerminalPayload) == 0 {
			if readErr != nil {
				return 0, 0, "", fmt.Errorf("stream closed before response.completed: %w", readErr)
			}
			return 0, 0, "", fmt.Errorf("stream closed before response.completed")
		}
		// Fallback for providers that don't emit type=response.completed but still
		// provide final JSON payload in the stream.
		completedPayload = lastTerminalPayload
	}

	transformedResp, err := trans.TransformResponse(completedPayload, false)
	if err != nil {
		return 0, 0, "", err
	}
	transformedResp, err = fixCursorResponseBody(transformedResp, requestMeta)
	if err != nil {
		return 0, 0, "", err
	}

	for key, values := range resp.Header {
		if key == "Content-Length" || key == "Content-Encoding" || key == "Content-Type" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(transformedResp)

	inputTokens, outputTokens := extractTokenUsage(transformedResp)
	transformedInputTokens, transformedOutputTokens := inputTokens, outputTokens
	upstreamInputTokens, upstreamOutputTokens := extractTokenUsage(completedPayload)
	if inputTokens == 0 && upstreamInputTokens > 0 {
		inputTokens = upstreamInputTokens
	}
	if outputTokens == 0 && upstreamOutputTokens > 0 {
		outputTokens = upstreamOutputTokens
	}
	outputText := extractResponseOutputText(transformedResp)

	logger.Debug(
		"[%s] Aggregated usage transformed(in=%d,out=%d) upstream(in=%d,out=%d) outputTextLen=%d",
		endpoint.Name,
		transformedInputTokens, transformedOutputTokens,
		upstreamInputTokens, upstreamOutputTokens,
		len(outputText),
	)

	return inputTokens, outputTokens, outputText, nil
}

// formatRequestSize formats byte size into human-readable string
func formatRequestSize(bytes int) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// transformStreamEvent transforms a single SSE event.
func (p *Proxy) transformStreamEvent(eventData []byte, trans transformer.Transformer, transformerName string, streamCtx *transformer.StreamContext, requestMeta proxyRequestMeta, clientModel string) ([]byte, error) {
	if requestMeta.CursorMode {
		return newcursor.TransformCursorUpstreamStreamEvent(
			requestMeta.cursorRequestMeta(),
			eventData,
			transformerName,
			clientModel,
			requestMeta.CursorState,
			func(b []byte) ([]byte, error) {
				return trans.TransformResponseWithContext(b, true, streamCtx)
			},
		)
	}
	return trans.TransformResponseWithContext(eventData, true, streamCtx)
}

func forEachSSEDataLine(eventData []byte, fn func(jsonData []byte) bool) {
	for _, line := range bytes.Split(eventData, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}

		jsonData := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(jsonData) == 0 || bytes.Equal(jsonData, []byte("[DONE]")) {
			continue
		}
		if !fn(jsonData) {
			return
		}
	}
}

// extractTokensFromEvent extracts token counts from SSE event
func (p *Proxy) extractTokensFromEvent(eventData []byte, inputTokens, outputTokens *int) {
	forEachSSEDataLine(eventData, func(jsonData []byte) bool {
		var event map[string]interface{}
		if err := json.Unmarshal(jsonData, &event); err != nil {
			return true
		}

		applyUsage := func(usage map[string]interface{}) {
			in, out := extractInputOutputTokens(usage)
			if in > 0 {
				*inputTokens = in
			}
			if out > 0 {
				*outputTokens = out
			}
		}

		// Claude-style events
		eventType, _ := event["type"].(string)
		if eventType == "message_start" {
			if message, ok := event["message"].(map[string]interface{}); ok {
				if usage, ok := message["usage"].(map[string]interface{}); ok {
					applyUsage(usage)
				}
			}
		} else if eventType == "message_delta" {
			if usage, ok := event["usage"].(map[string]interface{}); ok {
				applyUsage(usage)
			}
		}

		// OpenAI Responses-style events
		if response, ok := event["response"].(map[string]interface{}); ok {
			if usage, ok := response["usage"].(map[string]interface{}); ok {
				applyUsage(usage)
			}
		}

		// OpenAI Chat chunk-style usage (top-level)
		if usage, ok := event["usage"].(map[string]interface{}); ok {
			applyUsage(usage)
		}

		// Some providers wrap payloads with object=...
		if obj, ok := event["object"].(string); ok && strings.Contains(obj, "chat.completion") {
			if usage, ok := event["usage"].(map[string]interface{}); ok {
				applyUsage(usage)
			}
		}
		return true
	})
}

// extractTextFromEvent extracts text content from transformed event
// Enhanced to support both delta.text and content_block_delta formats
func (p *Proxy) extractTextFromEvent(transformedEvent []byte, outputText *strings.Builder) {
	forEachSSEDataLine(transformedEvent, func(jsonData []byte) bool {
		var event map[string]interface{}
		if err := json.Unmarshal(jsonData, &event); err != nil {
			return true
		}

		eventType, _ := event["type"].(string)

		// Handle content_block_delta format (from some third-party APIs)
		if eventType == "content_block_delta" {
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				if text, ok := delta["text"].(string); ok {
					outputText.WriteString(text)
				}
			}
		} else if delta, ok := event["delta"].(map[string]interface{}); ok {
			// Handle standard delta.text format
			if text, ok := delta["text"].(string); ok {
				outputText.WriteString(text)
			}
		}

		// Handle OpenAI Responses stream text delta format
		if eventType == "response.output_text.delta" {
			if delta, ok := event["delta"].(string); ok {
				outputText.WriteString(delta)
			}
		}

		// Handle OpenAI Chat stream chunk format (choices[].delta.content)
		if choices, ok := event["choices"].([]interface{}); ok {
			for _, choice := range choices {
				choiceMap, ok := choice.(map[string]interface{})
				if !ok {
					continue
				}
				delta, ok := choiceMap["delta"].(map[string]interface{})
				if !ok {
					continue
				}
				if text, ok := delta["content"].(string); ok {
					outputText.WriteString(text)
				}
			}
		}
		return true
	})
}

// isMessageStopEvent checks if the event is a message_stop event
func (p *Proxy) isMessageStopEvent(eventData []byte) bool {
	isStop := false
	forEachSSEDataLine(eventData, func(jsonData []byte) bool {
		var event map[string]interface{}
		if err := json.Unmarshal(jsonData, &event); err != nil {
			return true
		}

		eventType, _ := event["type"].(string)
		if eventType == "message_stop" {
			isStop = true
			return false
		}
		return true
	})
	return isStop
}

// decompressGzip decompresses gzip-encoded response body
func decompressGzip(body io.ReadCloser) ([]byte, error) {
	gzipReader, err := gzip.NewReader(body)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	return io.ReadAll(gzipReader)
}

func readStreamLine(reader *bufio.Reader) (string, error) {
	if reader == nil {
		return "", io.EOF
	}
	line, err := reader.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	return line, err
}

func isGracefulStreamReadError(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func extractAggregateTerminalPayload(rawJSON []byte, event map[string]interface{}) ([]byte, bool, error) {
	if len(rawJSON) == 0 || event == nil {
		return nil, false, nil
	}

	eventType, _ := event["type"].(string)
	eventType = strings.TrimSpace(eventType)
	if eventType != "" {
		if eventType != "response.completed" {
			return nil, false, nil
		}
		if responseObj, ok := event["response"]; ok {
			payload, err := json.Marshal(responseObj)
			if err != nil {
				return nil, false, err
			}
			return payload, true, nil
		}
		return append([]byte(nil), rawJSON...), true, nil
	}

	objectType, _ := event["object"].(string)
	if strings.TrimSpace(objectType) != "response" {
		return nil, false, nil
	}

	status, _ := event["status"].(string)
	status = strings.TrimSpace(status)
	switch status {
	case "", "completed", "failed", "cancelled", "canceled", "incomplete":
	default:
		return nil, false, nil
	}
	if status == "" {
		if _, hasOutput := event["output"]; !hasOutput {
			if _, hasUsage := event["usage"]; !hasUsage {
				return nil, false, nil
			}
		}
	}

	return append([]byte(nil), rawJSON...), true, nil
}
