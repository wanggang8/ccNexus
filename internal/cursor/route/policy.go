package route

import "github.com/lich0821/ccNexus/internal/cursor/shared"

func NeedClaudeMaxTokensFloor(format shared.ClientFormat, backend shared.Backend) bool {
	switch format {
	case shared.ClientFormatOpenAIChat, shared.ClientFormatOpenAIResponses:
		return backend == shared.BackendAnthropic
	default:
		return false
	}
}

func NeedClaudeCacheControl(format shared.ClientFormat, backend shared.Backend) bool {
	switch format {
	case shared.ClientFormatOpenAIChat, shared.ClientFormatOpenAIResponses:
		return backend == shared.BackendAnthropic
	default:
		return false
	}
}

func NeedPassthroughModelOverride(format shared.ClientFormat, backend shared.Backend) bool {
	switch format {
	case shared.ClientFormatOpenAIChat:
		return backend == shared.BackendOpenAI
	case shared.ClientFormatOpenAIResponses:
		return backend == shared.BackendOpenAI2
	default:
		return false
	}
}

func ResponsesRouteMode(backend shared.Backend) string {
	switch backend {
	case shared.BackendOpenAI2:
		return "native_responses"
	case shared.BackendOpenAI, shared.BackendAnthropic, shared.BackendGemini, shared.BackendCLI:
		return "responses_to_chat_bridge"
	default:
		return "unknown"
	}
}
