package cc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/lich0821/ccNexus/internal/transformer"
	"github.com/lich0821/ccNexus/internal/transformer/convert"
)

// CLITransformer converts Claude Code requests (Claude format) to Claude CLI format.
// CLI format = standard Claude Messages API + CLI-specific metadata/headers.
// Both input (from CC client) and output (to CLI backend) are Claude Messages format,
// so the request body is near-passthrough with CLI metadata injection.
// Response is full passthrough (CLI returns standard Claude SSE/JSON).
type CLITransformer struct {
	model  string
	apiKey string
}

// NewCLITransformer creates a CLI transformer
func NewCLITransformer(model, apiKey string) *CLITransformer {
	return &CLITransformer{model: model, apiKey: apiKey}
}

func (t *CLITransformer) Name() string {
	return "cc_cli"
}

// TransformRequest converts a Claude-format request to CLI format by:
// 1. Overriding model if configured
// 2. Injecting CLI system prompt
// 3. Adding metadata (user_id)
// 4. Ensuring max_tokens is set
func (t *CLITransformer) TransformRequest(req []byte) ([]byte, error) {
	body, _, err := t.transformRequestInternal(req)
	return body, err
}

// TransformRequestWithHeaders converts request and returns CLI-specific headers.
func (t *CLITransformer) TransformRequestWithHeaders(req []byte) ([]byte, map[string]string, error) {
	return t.transformRequestInternal(req)
}

func (t *CLITransformer) transformRequestInternal(req []byte) ([]byte, map[string]string, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(req, &data); err != nil {
		return nil, nil, fmt.Errorf("cc_cli: failed to parse request: %w", err)
	}

	// Override model
	if t.model != "" {
		data["model"] = t.model
	}

	// Ensure max_tokens is set
	if _, ok := data["max_tokens"]; !ok {
		data["max_tokens"] = convert.DefaultCliMaxTokens
	}

	// Inject CLI system prompt at the beginning of the system array
	existingSystem := data["system"]
	var systemBlocks []interface{}
	systemBlocks = append(systemBlocks, convert.CliSystemPrompt)
	switch s := existingSystem.(type) {
	case string:
		if s != "" {
			systemBlocks = append(systemBlocks, map[string]interface{}{"type": "text", "text": s})
		}
	case []interface{}:
		systemBlocks = append(systemBlocks, s...)
	}
	data["system"] = systemBlocks

	// Add metadata
	userID := fmt.Sprintf("user_%s_account__session_%s", getCliHexID(64), getCliHexID(36))
	data["metadata"] = map[string]interface{}{"user_id": userID}

	// Build body
	body, err := json.Marshal(data)
	if err != nil {
		return nil, nil, err
	}

	// Build headers - extract tools for beta selection
	var tools []map[string]interface{}
	if toolsRaw, ok := data["tools"].([]interface{}); ok {
		for _, t := range toolsRaw {
			if tm, ok := t.(map[string]interface{}); ok {
				tools = append(tools, tm)
			}
		}
	}

	stream, _ := data["stream"].(bool)
	betas := convert.BuildClaudeCliBetas(tools)
	headers := convert.BuildClaudeCliHeaders(t.apiKey, betas, stream)

	return body, headers, nil
}

// GetURL returns the CLI request URL
func (t *CLITransformer) GetURL(baseURL string) string {
	return convert.BuildClaudeCliURL(baseURL)
}

// TransformResponse passes the CLI response through unchanged.
// CLI backend returns standard Claude Messages format, which is what CC clients expect.
func (t *CLITransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
	return resp, nil
}

// TransformResponseWithContext passes the CLI streaming response through unchanged,
// with optional input_tokens fallback (same as cc_claude).
func (t *CLITransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
	return resp, nil
}

func getCliHexID(length int) string {
	bytes := make([]byte, length/2+1)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)[:length]
}
