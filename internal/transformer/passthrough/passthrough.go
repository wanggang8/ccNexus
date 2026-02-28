package passthrough

import (
	"encoding/json"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// PassthroughTransformer is a universal passthrough transformer that works with any format
type PassthroughTransformer struct {
	model string
}

// NewPassthroughTransformer creates a new passthrough transformer
func NewPassthroughTransformer(model string) transformer.Transformer {
	return &PassthroughTransformer{
		model: model,
	}
}

// Name returns the transformer name
func (t *PassthroughTransformer) Name() string {
	return "passthrough"
}

// TransformRequest passes through the request, optionally overriding the model field
func (t *PassthroughTransformer) TransformRequest(req []byte) ([]byte, error) {
	if t.model == "" {
		return req, nil // Complete passthrough
	}

	// Only override model field
	var data map[string]interface{}
	if err := json.Unmarshal(req, &data); err != nil {
		return req, nil // Return original on error
	}

	data["model"] = t.model
	return json.Marshal(data)
}

// TransformResponse passes through the response without modification
func (t *PassthroughTransformer) TransformResponse(resp []byte, isStreaming bool) ([]byte, error) {
	return resp, nil
}

