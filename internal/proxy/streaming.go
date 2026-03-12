package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/tokencount"
	"github.com/lich0821/ccNexus/internal/transformer"
)

func parseOpenAIIncludeUsage(bodyBytes []byte) bool {
	var req struct {
		StreamOptions *struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}

	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return false
	}

	return req.StreamOptions != nil && req.StreamOptions.IncludeUsage
}

// handleStreamingResponse processes streaming SSE responses
// Returns: inputTokens, outputTokens, outputText, originalResponse, transformedResponse
func (p *Proxy) handleStreamingResponse(w http.ResponseWriter, resp *http.Response, endpoint config.Endpoint, trans transformer.Transformer, transformerName string, thinkingEnabled bool, modelName string, bodyBytes []byte) (int, int, string, []byte, []byte) {
	// Copy response headers except Content-Length and Content-Encoding
	for key, values := range resp.Header {
		if key == "Content-Length" || key == "Content-Encoding" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, ok := w.(http.Flusher)
	if !ok {
		logger.Error("[%s] ResponseWriter does not support flushing", endpoint.Name)
		resp.Body.Close()
		return 0, 0, "", nil, nil
	}

	// Handle gzip-encoded response body
	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			logger.Error("[%s] Failed to create gzip reader: %v", endpoint.Name, err)
			resp.Body.Close()
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
		streamCtx.EnableThinking = thinkingEnabled
		if transformerName == "cx_chat_claude" {
			streamCtx.IncludeUsage = parseOpenAIIncludeUsage(bodyBytes)
		}
		// Pre-estimate input tokens for fallback
		if bodyBytes != nil {
			streamCtx.InputTokens = p.estimateInputTokens(bodyBytes)
		}
	}

	scanner := bufio.NewScanner(reader)
	// Increase buffer sizes to handle large SSE events (e.g., large file reads in tool calls)
	buf := make([]byte, 0, 128*1024) // 128KB initial buffer (was 64KB)
	scanner.Buffer(buf, 2*1024*1024) // 2MB max buffer (was 1MB)

	var inputTokens, outputTokens int
	var buffer bytes.Buffer
	var outputText strings.Builder
	var originalRespBuffer bytes.Buffer    // Accumulate original SSE events
	var transformedRespBuffer bytes.Buffer // Accumulate transformed SSE events
	isRecording := p.trafficRecorder.IsRecording()
	eventCount := 0
	streamDone := false

	for scanner.Scan() && !streamDone {
		line := scanner.Text()

		if !p.isCurrentEndpoint(endpoint.Name) {
			logger.Warn("[%s] Endpoint switched during streaming, terminating stream gracefully", endpoint.Name)
			streamDone = true
			break
		}

		if strings.Contains(line, "data: [DONE]") {
			streamDone = true

			// Token Usage Fallback: Inject message_delta with estimated output_tokens before [DONE]
			if outputTokens == 0 && outputText.Len() > 0 {
				outputTokens = tokencount.EstimateOutputTokens(outputText.String())
				logger.Debug("[%s] Token fallback before [DONE]: estimated output_tokens=%d", endpoint.Name, outputTokens)

				// Update stream context for transformer fallback
				if streamCtx != nil {
					streamCtx.OutputTokens = outputTokens
				}

				shouldInjectUsageFallback := transformerName != "cx_chat_claude" || (streamCtx != nil && streamCtx.IncludeUsage)
				if shouldInjectUsageFallback {
					// Inject message_delta event with usage
					deltaEvent := fmt.Sprintf("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":%d}}\n\n", outputTokens)
					if _, writeErr := w.Write([]byte(deltaEvent)); writeErr == nil {
						flusher.Flush()
					}
				}
			}

			buffer.WriteString(line + "\n")
			eventData := buffer.Bytes()

			// Accumulate original response (with size limit)
			if isRecording && originalRespBuffer.Len() < MaxBodySize {
				originalRespBuffer.Write(eventData)
			}

			transformedEvent, err := p.transformStreamEvent(eventData, trans, transformerName, streamCtx)
			if err == nil && len(transformedEvent) > 0 {
				// Accumulate transformed response (with size limit)
				if isRecording && transformedRespBuffer.Len() < MaxBodySize {
					transformedRespBuffer.Write(transformedEvent)
				}

				w.Write(transformedEvent)
				flusher.Flush()
			}
			break
		}

		buffer.WriteString(line + "\n")

		if line == "" {
			eventCount++
			eventData := buffer.Bytes()

			// Accumulate original response (with size limit)
			if isRecording && originalRespBuffer.Len() < MaxBodySize {
				originalRespBuffer.Write(eventData)
			}

			// Check if this is a message_stop event (Token Usage Fallback)
			isMessageStop := p.isMessageStopEvent(eventData)
			if isMessageStop && outputTokens == 0 && outputText.Len() > 0 {
				outputTokens = tokencount.EstimateOutputTokens(outputText.String())
				logger.Debug("[%s] Token fallback before message_stop: estimated output_tokens=%d", endpoint.Name, outputTokens)

				// Update stream context for transformer fallback
				if streamCtx != nil {
					streamCtx.OutputTokens = outputTokens
				}

				shouldInjectUsageFallback := transformerName != "cx_chat_claude" || (streamCtx != nil && streamCtx.IncludeUsage)
				if shouldInjectUsageFallback {
					// Inject message_delta event with usage before message_stop
					deltaEvent := fmt.Sprintf("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":%d}}\n\n", outputTokens)
					if _, writeErr := w.Write([]byte(deltaEvent)); writeErr == nil {
						flusher.Flush()
					}
				}
			}

			transformedEvent, err := p.transformStreamEvent(eventData, trans, transformerName, streamCtx)
			if err != nil {
				logger.Error("[%s] Failed to transform SSE event: %v", endpoint.Name, err)
			} else if len(transformedEvent) > 0 {
				// Accumulate transformed response (with size limit)
				if isRecording && transformedRespBuffer.Len() < MaxBodySize {
					transformedRespBuffer.Write(transformedEvent)
				}

				// Extract tokens and text from original event (upstream API response)
				// Original event contains complete token usage info from the API
				p.extractTokensFromEvent(eventData, &inputTokens, &outputTokens)
				p.extractTextFromEvent(eventData, &outputText)

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
			}
			buffer.Reset()
		}
	}

	if err := scanner.Err(); err != nil {
		errMsg := err.Error()
		// Check if it's an HTTP/2 stream error
		if strings.Contains(errMsg, "stream error") || strings.Contains(errMsg, "INTERNAL_ERROR") {
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
			logger.Error("[%s] Scanner error: %v", endpoint.Name, err)
		}

		// Send termination event to prevent client from hanging indefinitely
		if !streamDone {
			if _, writeErr := w.Write([]byte("data: [DONE]\n\n")); writeErr == nil {
				flusher.Flush()
			}
		}
	}

	resp.Body.Close()
	return inputTokens, outputTokens, outputText.String(), originalRespBuffer.Bytes(), transformedRespBuffer.Bytes()
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

// transformStreamEvent transforms a single SSE event
func (p *Proxy) transformStreamEvent(eventData []byte, trans transformer.Transformer, transformerName string, streamCtx *transformer.StreamContext) ([]byte, error) {
	// Use the unified interface method instead of type assertion switch
	// All transformers now implement TransformResponseWithContext
	return trans.TransformResponseWithContext(eventData, true, streamCtx)
}

// extractTokensFromEvent extracts token counts from SSE event
// Supports multiple formats: Claude (message_start/message_delta) and OpenAI (usage in final chunk)
func (p *Proxy) extractTokensFromEvent(eventData []byte, inputTokens, outputTokens *int) {
	scanner := bufio.NewScanner(bytes.NewReader(eventData))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		jsonData := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			continue
		}

		// Claude format: message_start contains input_tokens, message_delta contains output_tokens
		eventType, _ := event["type"].(string)
		if eventType == "message_start" {
			if message, ok := event["message"].(map[string]interface{}); ok {
				if usage, ok := message["usage"].(map[string]interface{}); ok {
					if input, ok := usage["input_tokens"].(float64); ok {
						*inputTokens = int(input)
					}
				}
			}
		} else if eventType == "message_delta" {
			if usage, ok := event["usage"].(map[string]interface{}); ok {
				if output, ok := usage["output_tokens"].(float64); ok {
					*outputTokens = int(output)
				}
			}
		}

		// OpenAI format: usage in final chunk (prompt_tokens/completion_tokens or input_tokens/output_tokens)
		if usage, ok := event["usage"].(map[string]interface{}); ok {
			// OpenAI naming: prompt_tokens / completion_tokens
			if input, ok := usage["prompt_tokens"].(float64); ok && int(input) > 0 {
				*inputTokens = int(input)
			}
			if output, ok := usage["completion_tokens"].(float64); ok && int(output) > 0 {
				*outputTokens = int(output)
			}
			// Alternative naming: input_tokens / output_tokens (some providers use this)
			if input, ok := usage["input_tokens"].(float64); ok && int(input) > 0 {
				*inputTokens = int(input)
			}
			if output, ok := usage["output_tokens"].(float64); ok && int(output) > 0 {
				*outputTokens = int(output)
			}
		}
	}
}

// extractTextFromEvent extracts text content from SSE event
// Supports multiple formats:
// - Claude original: content_block_delta with delta.type="text_delta" and delta.text
// - Claude transformed: delta.text (simplified)
// - OpenAI: choices[0].delta.content
func (p *Proxy) extractTextFromEvent(eventData []byte, outputText *strings.Builder) {
	scanner := bufio.NewScanner(bytes.NewReader(eventData))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		jsonData := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			continue
		}

		// Handle delta.text format (Claude content_block_delta, third-party APIs, etc.)
		if delta, ok := event["delta"].(map[string]interface{}); ok {
			// Check for text_delta type (Claude original format)
			if deltaType, ok := delta["type"].(string); ok && deltaType == "text_delta" {
				if text, ok := delta["text"].(string); ok {
					outputText.WriteString(text)
					continue
				}
			}
			// Simplified format: direct delta.text
			if text, ok := delta["text"].(string); ok {
				outputText.WriteString(text)
				continue
			}
		}

		// OpenAI format: choices[0].delta.content
		if choices, ok := event["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if delta, ok := choice["delta"].(map[string]interface{}); ok {
					if content, ok := delta["content"].(string); ok {
						outputText.WriteString(content)
					}
				}
			}
		}
	}
}

// isMessageStopEvent checks if the event is a message_stop event
func (p *Proxy) isMessageStopEvent(eventData []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(eventData))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		jsonData := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		if eventType == "message_stop" {
			return true
		}
	}
	return false
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
