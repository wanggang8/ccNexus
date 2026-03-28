package route

import (
	"encoding/json"
	"fmt"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func ResolveTargetPath(meta shared.RequestMeta, transformerName string, endpointModel string, transformedBody []byte) (string, bool) {
	if !meta.CursorMode {
		return "", false
	}

	switch BackendFromTransformer(transformerName) {
	case shared.BackendAnthropic:
		return "/v1/messages", true
	case shared.BackendOpenAI:
		return "/v1/chat/completions", true
	case shared.BackendOpenAI2:
		return "/v1/responses", true
	case shared.BackendGemini:
		var req struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(transformedBody, &req)
		if meta.Stream || req.Stream {
			return fmt.Sprintf("/v1/models/%s:streamGenerateContent?alt=sse", endpointModel), true
		}
		return fmt.Sprintf("/v1/models/%s:generateContent", endpointModel), true
	default:
		return "", false
	}
}
