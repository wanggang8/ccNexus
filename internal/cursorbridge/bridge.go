package cursorbridge

import cursor "github.com/lich0821/ccNexus/internal/cursor"

type ClientFormat = cursor.ClientFormat
type RequestMeta = cursor.RequestMeta
type ThinkingCache = cursor.ThinkingCache
type ThinkingCacheEntry = cursor.ThinkingCacheEntry
type ResponseToolState = cursor.ResponseToolState
type ResponseHooks = cursor.ResponseHooks
type StreamFinalizeState = cursor.StreamFinalizeState

const (
	ClientFormatUnknown         = cursor.ClientFormatUnknown
	ClientFormatClaude          = cursor.ClientFormatClaude
	ClientFormatOpenAIChat      = cursor.ClientFormatOpenAIChat
	ClientFormatOpenAIResponses = cursor.ClientFormatOpenAIResponses
)

func PrepareRequest(path string, body []byte) cursor.PreparedRequest {
	return cursor.PrepareRequest(path, body)
}

func StripCursorPrefix(path string) (string, bool) {
	return cursor.StripCursorPrefix(path)
}

func ExtractModel(body []byte) string {
	return cursor.ExtractModel(body)
}

func ExtractStream(body []byte) bool {
	return cursor.ExtractStream(body)
}

func NewThinkingCache() *ThinkingCache {
	return cursor.NewThinkingCache()
}

func DefaultThinkingCache() *ThinkingCache {
	return cursor.DefaultThinkingCache()
}

func SetDefaultThinkingCacheForTest(cache *ThinkingCache) {
	cursor.SetDefaultThinkingCacheForTest(cache)
}

func NormalizeRequestBody(path string, body []byte) ([]byte, error) {
	return cursor.NormalizeRequestBody(path, body)
}

func ValidateTransformer(meta RequestMeta, transformerName string) error {
	return cursor.ValidateTransformer(meta, transformerName)
}

func ValidateEndpointTransformer(meta RequestMeta, endpointTransformer string) error {
	return cursor.ValidateEndpointTransformer(meta, endpointTransformer)
}

func NeedPassthroughModelOverride(meta RequestMeta, transformerName string) bool {
	return cursor.NeedPassthroughModelOverride(meta, transformerName)
}

func ResponsesRouteMode(transformerName string) string {
	return cursor.ResponsesRouteMode(transformerName)
}

func ResolveTargetPath(meta RequestMeta, transformerName string, endpointModel string, transformedBody []byte) (string, bool) {
	return cursor.ResolveTargetPath(meta, transformerName, endpointModel, transformedBody)
}

func ExtractCacheMessages(body []byte, meta RequestMeta) []map[string]interface{} {
	return cursor.ExtractCacheMessages(body, meta)
}

func ApplyPreparedRequestCache(body []byte, meta RequestMeta, cacheMessages []map[string]interface{}, thinkingCache *ThinkingCache) ([]byte, []map[string]interface{}, error) {
	return cursor.ApplyPreparedRequestCache(body, meta, cacheMessages, thinkingCache)
}

func ApplyRequestCompat(body []byte, meta RequestMeta, transformerName string, cacheMessages []map[string]interface{}, thinkingCache *ThinkingCache) ([]byte, []map[string]interface{}, error) {
	return cursor.ApplyRequestCompat(body, meta, transformerName, cacheMessages, thinkingCache)
}

func ConvertAssistantToolUseMessageCompat(content []interface{}) map[string]interface{} {
	return cursor.ConvertAssistantToolUseMessageCompat(content)
}

func FixResponse(meta RequestMeta, body []byte, hooks ResponseHooks) ([]byte, error) {
	return cursor.FixResponse(meta, body, hooks)
}

func FixChatResponseBody(body []byte, clientModel string, cacheMessages []map[string]interface{}, thinkingCache *ThinkingCache) ([]byte, error) {
	return cursor.FixChatResponseBody(body, clientModel, cacheMessages, thinkingCache)
}

func FixOpenAIUpstreamChatBody(body []byte) ([]byte, error) {
	return cursor.FixOpenAIUpstreamChatBody(body)
}

func FixRawUpstreamResponseBody(meta RequestMeta, transformerName string, upstreamBody []byte, transformedBody []byte) ([]byte, error) {
	return cursor.FixRawUpstreamResponseBody(meta, transformerName, upstreamBody, transformedBody)
}

func FixResponsesResponseBody(body []byte, clientModel string, cacheMessages []map[string]interface{}, transformerName string, thinkingCache *ThinkingCache) ([]byte, error) {
	return cursor.FixResponsesResponseBody(body, clientModel, cacheMessages, transformerName, thinkingCache)
}

func FixMessagesResponseBody(body []byte) ([]byte, error) {
	return cursor.FixMessagesResponseBody(body)
}

func FixToolCallsCompat(message map[string]interface{}, choice map[string]interface{}) {
	cursor.FixToolCallsCompat(message, choice)
}

func ApplyToolArgFixesCompat(toolName string, args map[string]interface{}) map[string]interface{} {
	return cursor.ApplyToolArgFixesCompat(toolName, args)
}

func FixStream(meta RequestMeta, bundle []byte, hook func([]byte) ([]byte, error)) ([]byte, error) {
	return cursor.FixStream(meta, bundle, hook)
}

func FixChatStreamBundle(bundle []byte, clientModel string, state *StreamFinalizeState) ([]byte, error) {
	return cursor.FixChatStreamBundle(bundle, clientModel, state)
}

func FixMessagesStreamBundle(bundle []byte, state *StreamFinalizeState) ([]byte, error) {
	return cursor.FixMessagesStreamBundle(bundle, state)
}

func FixResponsesStreamBundle(bundle []byte, clientModel string, transformerName string, cacheMessages []map[string]interface{}, thinkingCache *ThinkingCache, state *StreamFinalizeState) ([]byte, error) {
	return cursor.FixResponsesStreamBundle(bundle, clientModel, transformerName, cacheMessages, thinkingCache, state)
}

func BridgeChatFromResponsesStreamBundle(bundle []byte, clientModel string, state *StreamFinalizeState) ([]byte, error) {
	return cursor.BridgeChatFromResponsesStreamBundle(bundle, clientModel, state)
}

func PrefixStream(meta RequestMeta, state *StreamFinalizeState, model string, transformerName string) []byte {
	return cursor.PrefixStream(meta, state, model, transformerName)
}

func FinalizeStream(meta RequestMeta, state *StreamFinalizeState, model string) []byte {
	return cursor.FinalizeStream(meta, state, model)
}

func TransformCursorUpstreamStreamEvent(
	meta RequestMeta,
	eventData []byte,
	transformerName string,
	clientModel string,
	state *StreamFinalizeState,
	transform func([]byte) ([]byte, error),
) ([]byte, error) {
	return cursor.TransformCursorUpstreamStreamEvent(meta, eventData, transformerName, clientModel, state, transform)
}
