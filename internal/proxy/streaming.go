package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/transformer"
	"github.com/lich0821/ccNexus/internal/transformer/cc"
	"github.com/lich0821/ccNexus/internal/transformer/cx/chat"
	"github.com/lich0821/ccNexus/internal/transformer/cx/responses"
)

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
		// Pre-estimate input tokens for fallback
		if bodyBytes != nil {
			streamCtx.InputTokens = p.estimateInputTokens(bodyBytes)
		}
	}

	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

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
		logger.Error("[%s] Scanner error: %v", endpoint.Name, err)
	}

	resp.Body.Close()
	return inputTokens, outputTokens, outputText.String(), originalRespBuffer.Bytes(), transformedRespBuffer.Bytes()
}

// transformStreamEvent transforms a single SSE event
func (p *Proxy) transformStreamEvent(eventData []byte, trans transformer.Transformer, transformerName string, streamCtx *transformer.StreamContext) ([]byte, error) {
	var result []byte
	var err error

	switch transformerName {
	// Claude Code transformers
	case "cc_claude":
		result, err = trans.(*cc.ClaudeTransformer).TransformResponseWithContext(eventData, true, streamCtx)
	case "cc_openai":
		result, err = trans.(*cc.OpenAITransformer).TransformResponseWithContext(eventData, true, streamCtx)
	case "cc_openai2":
		result, err = trans.(*cc.OpenAI2Transformer).TransformResponseWithContext(eventData, true, streamCtx)
	case "cc_gemini":
		result, err = trans.(*cc.GeminiTransformer).TransformResponseWithContext(eventData, true, streamCtx)
	// Codex Chat transformers
	case "cx_chat_claude":
		result, err = trans.(*chat.ClaudeTransformer).TransformResponseWithContext(eventData, true, streamCtx)
	case "cx_chat_openai":
		result, err = eventData, nil // passthrough
	case "cx_chat_openai2":
		result, err = trans.(*chat.OpenAI2Transformer).TransformResponseWithContext(eventData, true, streamCtx)
	case "cx_chat_gemini":
		result, err = trans.(*chat.GeminiTransformer).TransformResponseWithContext(eventData, true, streamCtx)
	case "cx_chat_cli":
		result, err = trans.(*chat.CLITransformer).TransformResponseWithContext(eventData, true, streamCtx)
	// Codex Responses transformers
	case "cx_resp_claude":
		result, err = trans.(*responses.ClaudeTransformer).TransformResponseWithContext(eventData, true, streamCtx)
	case "cx_resp_openai":
		result, err = trans.(*responses.OpenAITransformer).TransformResponseWithContext(eventData, true, streamCtx)
	case "cx_resp_openai2":
		result, err = eventData, nil // passthrough
	case "cx_resp_gemini":
		result, err = trans.(*responses.GeminiTransformer).TransformResponseWithContext(eventData, true, streamCtx)
	case "cx_resp_cli":
		result, err = trans.(*responses.CLITransformer).TransformResponseWithContext(eventData, true, streamCtx)
	// Claude Code CLI transformer
	case "cc_cli":
		result, err = trans.(*cc.CLITransformer).TransformResponseWithContext(eventData, true, streamCtx)
	default:
		result, err = trans.TransformResponse(eventData, true)
	}

	if err != nil {
		return nil, err
	}

	return result, nil
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

		// Claude format: delta.text (works for both original content_block_delta and simplified format)
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

// decompressGzip decompresses gzip-encoded response body
func decompressGzip(body io.ReadCloser) ([]byte, error) {
	gzipReader, err := gzip.NewReader(body)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	return io.ReadAll(gzipReader)
}
