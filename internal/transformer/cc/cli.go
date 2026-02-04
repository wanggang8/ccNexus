package cc

import (
	"github.com/lich0821/ccNexus/internal/transformer"
	"github.com/lich0821/ccNexus/internal/transformer/convert"
)

// CLITransformer 将 OpenAI 请求转换为 Claude Code CLI 格式
type CLITransformer struct {
	model  string
	apiKey string
}

// NewCLITransformer 创建 CLI 转换器
func NewCLITransformer(model, apiKey string) *CLITransformer {
	return &CLITransformer{model: model, apiKey: apiKey}
}

func (t *CLITransformer) Name() string {
	return "openai_to_cli"
}

// TransformRequest 转换请求（返回 body）
func (t *CLITransformer) TransformRequest(req []byte) ([]byte, error) {
	body, _, err := convert.OpenAIReqToClaudeCLI(req, t.model, t.apiKey)
	return body, err
}

// TransformRequestWithHeaders 转换请求并返回 headers
func (t *CLITransformer) TransformRequestWithHeaders(req []byte) ([]byte, map[string]string, error) {
	return convert.OpenAIReqToClaudeCLI(req, t.model, t.apiKey)
}

// GetURL 获取 CLI 请求 URL
func (t *CLITransformer) GetURL(baseURL string) string {
	return convert.BuildClaudeCliURL(baseURL)
}

// TransformResponse 转换响应（CLI → OpenAI）
func (t *CLITransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
	if isStreaming {
		return nil, nil
	}
	return convert.ClaudeRespToOpenAI(resp, t.model)
}

// TransformResponseWithContext 转换流式响应
func (t *CLITransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
	if isStreaming {
		return convert.ClaudeStreamToOpenAI(resp, ctx, t.model)
	}
	return convert.ClaudeRespToOpenAI(resp, t.model)
}
