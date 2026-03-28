package entry

import (
	"encoding/json"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func Prepare(path string, body []byte) shared.PreparedRequest {
	effectivePath, cursorMode := StripCursorPrefix(path)
	prepared := shared.PreparedRequest{
		Meta: shared.RequestMeta{
			CursorMode:    cursorMode,
			OriginalPath:  path,
			EffectivePath: effectivePath,
			ClientFormat:  DetectClientFormat(effectivePath),
			ClientModel:   extractModel(body),
			Stream:        extractStream(body),
		},
		Body: body,
	}
	return prepared
}

func ExtractModel(body []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	model, _ := payload["model"].(string)
	return model
}

func extractModel(body []byte) string {
	return ExtractModel(body)
}

func ExtractStream(body []byte) bool {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	stream, _ := payload["stream"].(bool)
	return stream
}

func extractStream(body []byte) bool {
	return ExtractStream(body)
}
