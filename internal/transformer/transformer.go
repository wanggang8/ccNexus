package transformer

// Transformer defines the interface for API format transformation
type Transformer interface {
	// TransformRequest converts source format request to target API format
	// For cc/* transformers: source is Claude format
	// For cx/* transformers: source is OpenAI format
	TransformRequest(req []byte) (targetReq []byte, err error)

	// TransformResponse converts target API format response to source format
	TransformResponse(targetResp []byte, isStreaming bool) (sourceResp []byte, err error)

	// Name returns the transformer name
	Name() string
}

// StreamingTransformer extends Transformer with context-aware streaming support
// Implementations should use this interface when handling streaming responses
// that require state tracking across multiple chunks
type StreamingTransformer interface {
	Transformer
	// TransformResponseWithContext converts target API response with streaming context
	// The context maintains state across streaming chunks for proper event sequencing
	TransformResponseWithContext(resp []byte, isStreaming bool, ctx *StreamContext) ([]byte, error)
}
