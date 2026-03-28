package shared

type ClientFormat string

const (
	ClientFormatUnknown         ClientFormat = ""
	ClientFormatClaude          ClientFormat = "claude"
	ClientFormatOpenAIChat      ClientFormat = "openai_chat"
	ClientFormatOpenAIResponses ClientFormat = "openai_responses"
)

type Backend string

const (
	BackendUnknown   Backend = ""
	BackendAnthropic Backend = "claude"
	BackendOpenAI    Backend = "openai"
	BackendOpenAI2   Backend = "openai2"
	BackendGemini    Backend = "gemini"
	BackendCLI       Backend = "cli"
)

type RequestMeta struct {
	CursorMode    bool
	OriginalPath  string
	EffectivePath string
	ClientFormat  ClientFormat
	ClientModel   string
	Stream        bool
}

type PreparedRequest struct {
	Meta RequestMeta
	Body []byte
}
