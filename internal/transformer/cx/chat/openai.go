package chat

import (
	"encoding/json"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// OpenAITransformer is a passthrough transformer for Codex Chat → OpenAI Chat
type OpenAITransformer struct {
	model string
}

// NewOpenAITransformer creates a new passthrough transformer
func NewOpenAITransformer(model string) *OpenAITransformer {
	return &OpenAITransformer{model: model}
}

func (t *OpenAITransformer) Name() string {
	return "cx_chat_openai"
}

func (t *OpenAITransformer) TransformRequest(req []byte) ([]byte, error) {
	if t.model == "" {
		return req, nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal(req, &data); err != nil {
		return req, nil
	}

	data["model"] = t.model
	return json.Marshal(data)
}

func (t *OpenAITransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
	return resp, nil
}

func (t *OpenAITransformer) TransformResponseWithContext(resp []byte, isStreaming bool, ctx *transformer.StreamContext) ([]byte, error) {
	return resp, nil
}
