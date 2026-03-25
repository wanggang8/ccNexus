package chat

import (
	"github.com/lich0821/ccNexus/internal/transformer"
	"github.com/lich0821/ccNexus/internal/transformer/convert"
)

// CLITransformer transforms Codex Chat requests to Claude CLI format
type CLITransformer struct {
	model  string
	apiKey string
}

// NewCLITransformer creates a new transformer
func NewCLITransformer(model, apiKey string) *CLITransformer {
	return &CLITransformer{model: model, apiKey: apiKey}
}

func (t *CLITransformer) Name() string {
	return "cx_chat_cli"
}

func (t *CLITransformer) TransformRequest(req []byte) ([]byte, error) {
	body, _, err := convert.OpenAIReqToClaudeCLI(req, t.model, t.apiKey)
	return body, err
}

func (t *CLITransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
	if isStreaming {
		return nil, nil
	}
	return convert.ClaudeRespToOpenAI(resp, t.model)
}

func (t *CLITransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
	if isStreaming {
		return convert.ClaudeStreamToOpenAI(resp, ctx, t.model)
	}
	return convert.ClaudeRespToOpenAI(resp, t.model)
}
