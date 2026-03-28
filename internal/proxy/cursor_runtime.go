package proxy

import (
	"net/http"
	"net/url"

	newcursor "github.com/lich0821/ccNexus/internal/cursorbridge"
)

type proxyRequestMeta struct {
	newcursor.RequestMeta
	CursorState       *newcursor.StreamFinalizeState
	CacheMessages     []map[string]interface{}
	TransformerName   string
	Degraded          bool
	DegradedReason    []string
	CursorRequestBody []byte
}

func (m proxyRequestMeta) cursorRequestMeta() newcursor.RequestMeta {
	return m.RequestMeta
}

func prepareProxyRequest(r *http.Request, body []byte) (*http.Request, []byte, proxyRequestMeta, error) {
	prepared := newcursor.PrepareRequest(r.URL.Path, body)
	meta := proxyRequestMeta{
		RequestMeta: prepared.Meta,
	}

	if meta.CursorMode {
		meta.CursorState = &newcursor.StreamFinalizeState{
			ResponsesTools:  make(map[int]*newcursor.ResponseToolState),
			ResponsesOutput: make([]map[string]interface{}, 0),
		}
	}

	normalizedBody := body
	var err error
	if meta.CursorMode {
		normalizedBody, err = newcursor.NormalizeRequestBody(meta.EffectivePath, body)
		if err != nil {
			return nil, nil, meta, err
		}
	}
	if meta.CursorMode {
		meta.CursorRequestBody = normalizedBody
	}
	meta.ClientModel = newcursor.ExtractModel(normalizedBody)
	meta.Stream = newcursor.ExtractStream(normalizedBody)
	if meta.CursorMode {
		meta.CacheMessages = newcursor.ExtractCacheMessages(normalizedBody, meta.cursorRequestMeta())
	}

	return cloneRequestWithPath(r, meta.EffectivePath), normalizedBody, meta, nil
}

func withCursorPathStripped(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		strippedPath, ok := newcursor.StripCursorPrefix(r.URL.Path)
		if !ok {
			handler(w, r)
			return
		}
		handler(w, cloneRequestWithPath(r, strippedPath))
	}
}

func cloneRequestWithPath(r *http.Request, path string) *http.Request {
	cloned := r.Clone(r.Context())
	if r.URL != nil {
		copiedURL := *r.URL
		cloned.URL = &copiedURL
	} else {
		cloned.URL = &url.URL{}
	}
	cloned.URL.Path = path
	cloned.URL.RawPath = path
	cloned.RequestURI = ""
	return cloned
}

func fixCursorResponseBody(body []byte, meta proxyRequestMeta) ([]byte, error) {
	if !meta.CursorMode {
		return body, nil
	}

	switch meta.ClientFormat {
	case ClientFormatOpenAIChat:
		return newcursor.FixChatResponseBody(body, meta.ClientModel, meta.CacheMessages, newcursor.DefaultThinkingCache())
	case ClientFormatOpenAIResponses:
		return newcursor.FixResponsesResponseBody(body, meta.ClientModel, meta.CacheMessages, meta.TransformerName, newcursor.DefaultThinkingCache())
	case ClientFormatClaude:
		return newcursor.FixMessagesResponseBody(body)
	default:
		return body, nil
	}
}

func fixCursorStreamBundle(bundle []byte, meta proxyRequestMeta) ([]byte, error) {
	if !meta.CursorMode {
		return bundle, nil
	}

	switch meta.ClientFormat {
	case ClientFormatOpenAIChat:
		return newcursor.FixChatStreamBundle(bundle, meta.ClientModel, meta.CursorState)
	case ClientFormatOpenAIResponses:
		return newcursor.FixResponsesStreamBundle(bundle, meta.ClientModel, meta.TransformerName, meta.CacheMessages, newcursor.DefaultThinkingCache(), meta.CursorState)
	case ClientFormatClaude:
		return newcursor.FixMessagesStreamBundle(bundle, meta.CursorState)
	default:
		return bundle, nil
	}
}

func applyCursorTransformedRequestCompat(body []byte, meta *proxyRequestMeta, transformerName string) ([]byte, error) {
	if meta == nil || !meta.CursorMode {
		return body, nil
	}
	updated, cacheMessages, err := newcursor.ApplyRequestCompat(body, meta.cursorRequestMeta(), transformerName, meta.CacheMessages, newcursor.DefaultThinkingCache())
	if err != nil {
		return nil, err
	}
	meta.CacheMessages = cacheMessages
	return updated, nil
}
