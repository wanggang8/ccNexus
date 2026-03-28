package route

import (
	"fmt"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func AllowedBackends(format shared.ClientFormat) []shared.Backend {
	switch format {
	case shared.ClientFormatOpenAIChat, shared.ClientFormatOpenAIResponses:
		return []shared.Backend{
			shared.BackendAnthropic,
			shared.BackendOpenAI,
			shared.BackendOpenAI2,
			shared.BackendGemini,
		}
	case shared.ClientFormatClaude:
		return []shared.Backend{
			shared.BackendAnthropic,
		}
	default:
		return nil
	}
}

func ValidateBackend(format shared.ClientFormat, backend shared.Backend) error {
	for _, candidate := range AllowedBackends(format) {
		if candidate == backend {
			return nil
		}
	}
	return fmt.Errorf("cursor route %s does not allow backend %s", format, backend)
}

func BackendFromTransformer(name string) shared.Backend {
	switch name {
	case "cc_claude", "cx_chat_claude", "cx_resp_claude":
		return shared.BackendAnthropic
	case "cc_openai", "cx_chat_openai", "cx_resp_openai":
		return shared.BackendOpenAI
	case "cc_openai2", "cx_chat_openai2", "cx_resp_openai2":
		return shared.BackendOpenAI2
	case "cc_gemini", "cx_chat_gemini", "cx_resp_gemini":
		return shared.BackendGemini
	case "cc_cli", "cx_chat_cli", "cx_resp_cli":
		return shared.BackendCLI
	default:
		return shared.BackendUnknown
	}
}

func BackendFromEndpointTransformer(name string) shared.Backend {
	switch name {
	case "", "claude", "cc_claude":
		return shared.BackendAnthropic
	case "openai", "cc_openai":
		return shared.BackendOpenAI
	case "openai2", "cc_openai2":
		return shared.BackendOpenAI2
	case "gemini", "cc_gemini":
		return shared.BackendGemini
	case "cli", "cc_cli":
		return shared.BackendCLI
	default:
		return shared.BackendUnknown
	}
}
