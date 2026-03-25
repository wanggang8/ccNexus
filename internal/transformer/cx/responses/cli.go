package responses

import (
	"github.com/lich0821/ccNexus/internal/transformer"
	"github.com/lich0821/ccNexus/internal/transformer/convert"
)

// CLITransformer transforms Codex Responses requests to Claude CLI format
type CLITransformer struct {
	model  string
	apiKey string
}

// NewCLITransformer creates a new transformer
func NewCLITransformer(model, apiKey string) *CLITransformer {
	return &CLITransformer{model: model, apiKey: apiKey}
}

func (t *CLITransformer) Name() string {
	return "cx_resp_cli"
}

func (t *CLITransformer) TransformRequest(req []byte) ([]byte, error) {
	// First convert OpenAI2 to OpenAI format, then to CLI
	openaiReq, err := convert.OpenAI2ReqToOpenAI(req, t.model)
	if err != nil {
		return nil, err
	}
	body, _, err := convert.OpenAIReqToClaudeCLI(openaiReq, t.model, t.apiKey)
	return body, err
}

func (t *CLITransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
	if isStreaming {
		return nil, nil
	}
	return convert.ClaudeRespToOpenAI2(resp)
}

func (t *CLITransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
	if isStreaming {
		return convert.ClaudeStreamToOpenAI2(resp, ctx)
	}
	return convert.ClaudeRespToOpenAI2(resp)
}
