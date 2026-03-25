// Package augment provides transformers that convert between the Augment plugin
// wire format and standard API formats (Claude Messages, OpenAI Chat, CLI).
package augment

import (
	"fmt"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// Transformer dispatches Augment requests to the correct target format based
// on the configured targetType ("claude", "cli", "openai", "openai2").
type Transformer struct {
	targetType  string
	model       string
	toolContext map[string]*ToolContext // MCP tool context cache
}

// ToolContext stores MCP metadata for a tool.
type ToolContext struct {
	McpServerName string
	McpToolName   string
}

// New creates a new AugmentTransformer.
//   - targetType: "claude" | "cli" | "openai" | "openai2"
//   - model: optional model override; when empty the model from the request is used
func New(targetType, model string) (*Transformer, error) {
	switch targetType {
	case "claude", "cli", "openai", "openai2":
	default:
		return nil, fmt.Errorf("augment transformer: unsupported target type %q (want claude/cli/openai/openai2)", targetType)
	}
	return &Transformer{targetType: targetType, model: model}, nil
}

// Name implements transformer.Transformer.
func (t *Transformer) Name() string {
	return "augment_" + t.targetType
}

// TransformRequest converts an Augment-format request body to the target API format.
func (t *Transformer) TransformRequest(req []byte) ([]byte, error) {
	ar, err := normalizeAugmentRequest(req)
	if err != nil {
		return nil, fmt.Errorf("augment transformer: unmarshal request: %w", err)
	}

	// Apply model override if configured.
	if t.model != "" {
		ar.Model = t.model
	}
	if ar.Model == "" {
		switch t.targetType {
		case "openai":
			ar.Model = "gpt-4.1"
		case "openai2":
			ar.Model = "gpt-5-codex"
		default:
			ar.Model = "claude-sonnet-4-5-20250929"
		}
	}

	// Cache MCP tool context for response transformation
	t.toolContext = make(map[string]*ToolContext)
	for _, tool := range ar.EffectiveTools() {
		if tool.McpServerName != "" || tool.McpToolName != "" {
			t.toolContext[tool.Name] = &ToolContext{
				McpServerName: tool.McpServerName,
				McpToolName:   tool.McpToolName,
			}
		}
	}

	switch t.targetType {
	case "claude":
		return toClaudeRequest(ar)
	case "cli":
		return toCliRequest(ar)
	case "openai":
		return toOpenAIRequest(ar)
	case "openai2":
		return toOpenAI2Request(ar)
	default:
		return nil, fmt.Errorf("augment transformer: unknown target type %q", t.targetType)
	}
}

// TransformResponse passes the upstream response through unchanged.
// The Augment server handles SSE→NDJSON conversion separately in the response package.
func (t *Transformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
	return resp, nil
}

// TransformResponseWithContext passes the upstream response through unchanged.
// Augment NDJSON conversion is handled by the server layer, not the transformer.
func (t *Transformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
	return resp, nil
}

// GetToolContext returns the cached MCP tool context for response conversion.
// This is used by the server layer when converting SSE to NDJSON.
func (t *Transformer) GetToolContext() map[string]*ToolContext {
	return t.toolContext
}
