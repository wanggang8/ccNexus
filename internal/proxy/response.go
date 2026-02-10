package proxy

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/transformer"
)

// handleNonStreamingResponse processes non-streaming responses
// Returns: inputTokens, outputTokens, originalResponse, transformedResponse, error
func (p *Proxy) handleNonStreamingResponse(w http.ResponseWriter, resp *http.Response, endpoint config.Endpoint, trans transformer.Transformer) (int, int, []byte, []byte, error) {
	var bodyBytes []byte
	var err error

	if resp.Header.Get("Content-Encoding") == "gzip" {
		bodyBytes, err = decompressGzip(resp.Body)
		if err != nil {
			logger.Error("[%s] Failed to decompress gzip response: %v", endpoint.Name, err)
			return 0, 0, nil, nil, err
		}
	} else {
		bodyBytes, err = io.ReadAll(resp.Body)
		if err != nil {
			logger.Error("[%s] Failed to read response body: %v", endpoint.Name, err)
			return 0, 0, nil, nil, err
		}
	}
	resp.Body.Close()

	logger.DebugLog("[%s] Response body: %s", endpoint.Name, string(bodyBytes))

	// Transform response back to client format
	transformedResp, err := trans.TransformResponse(bodyBytes, false)
	if err != nil {
		logger.Error("[%s] Failed to transform response: %v", endpoint.Name, err)
		return 0, 0, bodyBytes, nil, err
	}

	logger.DebugLog("[%s] Transformed response: %s", endpoint.Name, string(transformedResp))

	// Extract token usage
	inputTokens, outputTokens := extractTokenUsage(transformedResp)

	// Copy response headers
	for key, values := range resp.Header {
		if key == "Content-Length" || key == "Content-Encoding" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	w.Write(transformedResp)

	return inputTokens, outputTokens, bodyBytes, transformedResp, nil
}

// extractTokenUsage extracts token counts from response
// Supports multiple formats: Claude (input_tokens/output_tokens) and OpenAI (prompt_tokens/completion_tokens)
func extractTokenUsage(responseBody []byte) (int, int) {
	var resp map[string]interface{}
	if err := json.Unmarshal(responseBody, &resp); err != nil {
		return 0, 0
	}

	var inputTokens, outputTokens int

	if usage, ok := resp["usage"].(map[string]interface{}); ok {
		// Claude naming: input_tokens / output_tokens
		if input, ok := usage["input_tokens"].(float64); ok {
			inputTokens = int(input)
		}
		if output, ok := usage["output_tokens"].(float64); ok {
			outputTokens = int(output)
		}
		// OpenAI naming: prompt_tokens / completion_tokens
		if input, ok := usage["prompt_tokens"].(float64); ok && int(input) > 0 {
			inputTokens = int(input)
		}
		if output, ok := usage["completion_tokens"].(float64); ok && int(output) > 0 {
			outputTokens = int(output)
		}
	}

	return inputTokens, outputTokens
}
