package cursor

import (
	"bytes"

	cursorcache "github.com/lich0821/ccNexus/internal/cursor/cache"
	"github.com/lich0821/ccNexus/internal/cursor/entry"
	"github.com/lich0821/ccNexus/internal/cursor/request"
	"github.com/lich0821/ccNexus/internal/cursor/response"
	"github.com/lich0821/ccNexus/internal/cursor/route"
	"github.com/lich0821/ccNexus/internal/cursor/shared"
	"github.com/lich0821/ccNexus/internal/cursor/stream"
)

type ClientFormat = shared.ClientFormat
type Backend = shared.Backend
type RequestMeta = shared.RequestMeta
type PreparedRequest = shared.PreparedRequest
type ThinkingCache = cursorcache.ThinkingCache
type ThinkingCacheEntry = cursorcache.Entry
type ResponseToolState = stream.ResponseToolState

const (
	ClientFormatUnknown         = shared.ClientFormatUnknown
	ClientFormatClaude          = shared.ClientFormatClaude
	ClientFormatOpenAIChat      = shared.ClientFormatOpenAIChat
	ClientFormatOpenAIResponses = shared.ClientFormatOpenAIResponses

	BackendUnknown   = shared.BackendUnknown
	BackendAnthropic = shared.BackendAnthropic
	BackendOpenAI    = shared.BackendOpenAI
	BackendOpenAI2   = shared.BackendOpenAI2
	BackendGemini    = shared.BackendGemini
	BackendCLI       = shared.BackendCLI
)

const ThinkingCacheTTL = cursorcache.TTL

func PrepareRequest(path string, body []byte) PreparedRequest {
	return entry.Prepare(path, body)
}

func StripCursorPrefix(path string) (string, bool) {
	return entry.StripCursorPrefix(path)
}

func DetectClientFormat(path string) ClientFormat {
	return entry.DetectClientFormat(path)
}

func ExtractModel(body []byte) string {
	return entry.ExtractModel(body)
}

func ExtractStream(body []byte) bool {
	return entry.ExtractStream(body)
}

func NewThinkingCache() *ThinkingCache {
	return cursorcache.NewThinkingCache()
}

func NormalizeRequestBody(path string, body []byte) ([]byte, error) {
	return request.NormalizeRequestBody(path, body)
}

func ValidateRoute(format ClientFormat, transformerName string) error {
	return route.ValidateBackend(format, route.BackendFromTransformer(transformerName))
}

func ValidateTransformer(meta RequestMeta, transformerName string) error {
	return request.ValidateTransformer(meta, transformerName)
}

func ValidateEndpointTransformer(meta RequestMeta, endpointTransformer string) error {
	return request.ValidateEndpointTransformer(meta, endpointTransformer)
}

func ApplyStatelessRequestCompat(body []byte, meta RequestMeta, transformerName string) ([]byte, error) {
	return request.ApplyStatelessTransformedCompat(body, meta, transformerName)
}

func NeedPassthroughModelOverride(meta RequestMeta, transformerName string) bool {
	return request.NeedPassthroughModelOverride(meta, transformerName)
}

func ResponsesRouteMode(transformerName string) string {
	return route.ResponsesRouteMode(route.BackendFromTransformer(transformerName))
}

func ResolveTargetPath(meta RequestMeta, transformerName string, endpointModel string, transformedBody []byte) (string, bool) {
	return route.ResolveTargetPath(meta, transformerName, endpointModel, transformedBody)
}

func ExtractCacheMessages(body []byte, meta RequestMeta) []map[string]interface{} {
	return request.ExtractCacheMessages(body, meta)
}

func ApplyPreparedRequestCache(body []byte, meta RequestMeta, cacheMessages []map[string]interface{}, thinkingCache *ThinkingCache) ([]byte, []map[string]interface{}, error) {
	return request.ApplyPreparedCache(body, meta, cacheMessages, thinkingCache)
}

func ApplyRequestCompat(body []byte, meta RequestMeta, transformerName string, cacheMessages []map[string]interface{}, thinkingCache *ThinkingCache) ([]byte, []map[string]interface{}, error) {
	return request.ApplyTransformedCompat(body, meta, transformerName, cacheMessages, thinkingCache)
}

func ConvertAssistantToolUseMessageCompat(content []interface{}) map[string]interface{} {
	return response.ConvertAssistantToolUseMessageCompat(content)
}

func ConvertToolResultMessageCompat(role string, content []interface{}) []interface{} {
	return response.ConvertToolResultMessageCompat(role, content)
}

func StringifyToolResultContentCompat(content interface{}) string {
	return response.StringifyToolResultContentCompat(content)
}

type ResponseHooks = response.Hooks
type StreamFinalizeState = stream.FinalizeState

func FixResponse(meta RequestMeta, body []byte, hooks ResponseHooks) ([]byte, error) {
	return response.Fix(meta, body, hooks)
}

func FixChatResponseBody(body []byte, clientModel string, cacheMessages []map[string]interface{}, thinkingCache *ThinkingCache) ([]byte, error) {
	return response.FixChatBody(body, clientModel, cacheMessages, thinkingCache)
}

func FixOpenAIUpstreamChatBody(body []byte) ([]byte, error) {
	return response.FixOpenAIUpstreamChatBody(body)
}

func FixRawUpstreamResponseBody(meta RequestMeta, transformerName string, upstreamBody []byte, transformedBody []byte) ([]byte, error) {
	if !meta.CursorMode {
		return transformedBody, nil
	}
	switch {
	case meta.ClientFormat == shared.ClientFormatOpenAIChat && transformerName == "cx_chat_gemini":
		return response.FixGeminiUpstreamChatBody(upstreamBody, transformedBody)
	case meta.ClientFormat == shared.ClientFormatOpenAIResponses && transformerName == "cx_resp_gemini":
		return response.FixGeminiUpstreamResponsesBody(upstreamBody, transformedBody)
	default:
		return transformedBody, nil
	}
}

func FixResponsesResponseBody(body []byte, clientModel string, cacheMessages []map[string]interface{}, transformerName string, thinkingCache *ThinkingCache) ([]byte, error) {
	return response.FixResponsesBody(body, clientModel, cacheMessages, transformerName, thinkingCache)
}

func FixMessagesResponseBody(body []byte) ([]byte, error) {
	return response.FixMessagesBody(body)
}

func FixToolCallsCompat(message map[string]interface{}, choice map[string]interface{}) {
	response.FixToolCallsCompat(message, choice)
}

func ApplyToolArgFixesCompat(toolName string, args map[string]interface{}) map[string]interface{} {
	return response.ApplyToolArgFixesCompat(toolName, args)
}

func NewToolCallIDCompat() string {
	return response.NewToolCallIDCompat()
}

func FixStream(meta RequestMeta, bundle []byte, hook func([]byte) ([]byte, error)) ([]byte, error) {
	return stream.Fix(meta, bundle, hook)
}

func FixChatStreamBundle(bundle []byte, clientModel string, state *StreamFinalizeState) ([]byte, error) {
	return stream.FixChatBundle(bundle, clientModel, state)
}

func FixMessagesStreamBundle(bundle []byte, state *StreamFinalizeState) ([]byte, error) {
	return stream.FixMessagesBundle(bundle, state)
}

func FixResponsesStreamBundle(bundle []byte, clientModel string, transformerName string, cacheMessages []map[string]interface{}, thinkingCache *ThinkingCache, state *StreamFinalizeState) ([]byte, error) {
	return stream.FixResponsesBundle(bundle, clientModel, transformerName, cacheMessages, thinkingCache, state)
}

func BridgeChatFromResponsesStreamBundle(bundle []byte, clientModel string, state *StreamFinalizeState) ([]byte, error) {
	return stream.BridgeChatFromResponsesBundle(bundle, clientModel, state)
}

func PrefixStream(meta RequestMeta, state *StreamFinalizeState, model string, transformerName string) []byte {
	return stream.Prefix(meta, state, model, transformerName)
}

func FinalizeStream(meta RequestMeta, state *StreamFinalizeState, model string) []byte {
	return stream.Finalize(meta, state, model)
}

// TransformCursorUpstreamStreamEvent applies Cursor-only upstream SSE handling, then transform on each piece.
// Call only when meta.CursorMode; otherwise use the transformer directly on eventData.
func TransformCursorUpstreamStreamEvent(
	meta RequestMeta,
	eventData []byte,
	transformerName string,
	clientModel string,
	state *StreamFinalizeState,
	transform func([]byte) ([]byte, error),
) ([]byte, error) {
	if !meta.CursorMode {
		return transform(eventData)
	}
	switch {
	case meta.ClientFormat == shared.ClientFormatClaude && transformerName == "cc_claude":
		return eventData, nil
	case meta.ClientFormat == shared.ClientFormatOpenAIChat && transformerName == "cx_chat_openai2":
		return stream.BridgeChatFromResponsesBundle(eventData, clientModel, state)
	case meta.ClientFormat == shared.ClientFormatOpenAIChat && transformerName == "cx_chat_gemini":
		transformed, err := transform(eventData)
		if err != nil {
			return nil, err
		}
		return stream.FixGeminiUpstreamChatBundle(eventData, transformed)
	case meta.ClientFormat == shared.ClientFormatOpenAIResponses && transformerName == "cx_resp_openai":
		fixedEvent := stream.FixRespOpenAIUpstreamChatSSE(eventData)
		parts := stream.ExpandRespOpenAIUpstreamChatSSE(fixedEvent, state)
		if len(parts) == 0 {
			return transform(fixedEvent)
		}
		var buf bytes.Buffer
		for _, p := range parts {
			b, err := transform(p)
			if err != nil {
				return nil, err
			}
			buf.Write(b)
		}
		return buf.Bytes(), nil
	case meta.ClientFormat == shared.ClientFormatOpenAIResponses && transformerName == "cx_resp_gemini":
		transformed, err := transform(eventData)
		if err != nil {
			return nil, err
		}
		return stream.FixGeminiUpstreamResponsesBundle(eventData, transformed)
	default:
		return transform(eventData)
	}
}
