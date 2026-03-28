package response

import "github.com/lich0821/ccNexus/internal/cursor/shared"

type Hooks struct {
	FixChat      func(body []byte, clientModel string) ([]byte, error)
	FixResponses func(body []byte, clientModel string) ([]byte, error)
	FixMessages  func(body []byte) ([]byte, error)
}

func Fix(meta shared.RequestMeta, body []byte, hooks Hooks) ([]byte, error) {
	if !meta.CursorMode {
		return body, nil
	}
	switch meta.ClientFormat {
	case shared.ClientFormatOpenAIChat:
		if hooks.FixChat != nil {
			return hooks.FixChat(body, meta.ClientModel)
		}
	case shared.ClientFormatOpenAIResponses:
		if hooks.FixResponses != nil {
			return hooks.FixResponses(body, meta.ClientModel)
		}
	case shared.ClientFormatClaude:
		if hooks.FixMessages != nil {
			return hooks.FixMessages(body)
		}
	}
	return body, nil
}
