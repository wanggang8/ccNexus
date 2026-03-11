package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/transformer"
	"github.com/lich0821/ccNexus/internal/transformer/cc"
	"github.com/lich0821/ccNexus/internal/transformer/convert"
	"github.com/lich0821/ccNexus/internal/transformer/cx/chat"
	"github.com/lich0821/ccNexus/internal/transformer/cx/responses"
	"github.com/lich0821/ccNexus/internal/transformer/passthrough"
)

// prepareTransformerForClient creates transformer based on client format and endpoint
func prepareTransformerForClient(clientFormat ClientFormat, endpoint config.Endpoint) (transformer.Transformer, error) {
	endpointTransformer := endpoint.Transformer
	if endpointTransformer == "" {
		endpointTransformer = "claude"
	}

	var trans transformer.Transformer
	var err error

	switch clientFormat {
	case ClientFormatClaude:
		trans, err = prepareCCTransformer(endpoint, endpointTransformer)
	case ClientFormatOpenAIChat:
		trans, err = prepareCxChatTransformer(endpoint, endpointTransformer)
	case ClientFormatOpenAIResponses:
		trans, err = prepareCxRespTransformer(endpoint, endpointTransformer)
	default:
		return nil, fmt.Errorf("unsupported client format: %s", clientFormat)
	}

	if err != nil {
		return nil, err
	}

	logger.Debug("[%s] 转换器: %s (客户端: %s, 端点: %s)", endpoint.Name, trans.Name(), clientFormat, endpointTransformer)
	return trans, nil
}

// prepareCCTransformer creates transformer for Claude Code client
func prepareCCTransformer(endpoint config.Endpoint, endpointTransformer string) (transformer.Transformer, error) {
	switch endpointTransformer {
	case "claude":
		if endpoint.Model != "" {
			logger.Debug("[%s] Using cc_claude with model override: %s", endpoint.Name, endpoint.Model)
			return cc.NewClaudeTransformerWithModel(endpoint.Model), nil
		}
		return cc.NewClaudeTransformer(), nil
	case "openai":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("OpenAI transformer requires model field")
		}
		return cc.NewOpenAITransformer(endpoint.Model), nil
	case "openai2":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("OpenAI2 transformer requires model field")
		}
		return cc.NewOpenAI2Transformer(endpoint.Model), nil
	case "gemini":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("Gemini transformer requires model field")
		}
		return cc.NewGeminiTransformer(endpoint.Model), nil
	case "cli":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("CLI transformer requires model field")
		}
		return cc.NewCLITransformer(endpoint.Model, endpoint.APIKey), nil
	case "passthrough":
		return passthrough.NewPassthroughTransformer(endpoint.Model), nil
	default:
		return nil, fmt.Errorf("unsupported endpoint transformer: %s", endpointTransformer)
	}
}

// prepareCxChatTransformer creates transformer for Codex Chat API client
func prepareCxChatTransformer(endpoint config.Endpoint, endpointTransformer string) (transformer.Transformer, error) {
	switch endpointTransformer {
	case "claude":
		model := endpoint.Model
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}
		return chat.NewClaudeTransformer(model), nil
	case "openai":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("OpenAI transformer requires model field")
		}
		return chat.NewOpenAITransformer(endpoint.Model), nil
	case "openai2":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("OpenAI2 transformer requires model field")
		}
		return chat.NewOpenAI2Transformer(endpoint.Model), nil
	case "gemini":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("Gemini transformer requires model field")
		}
		return chat.NewGeminiTransformer(endpoint.Model), nil
	case "cli":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("CLI transformer requires model field")
		}
		return chat.NewCLITransformer(endpoint.Model, endpoint.APIKey), nil
	case "passthrough":
		return passthrough.NewPassthroughTransformer(endpoint.Model), nil
	default:
		return nil, fmt.Errorf("unsupported endpoint transformer for Codex Chat: %s", endpointTransformer)
	}
}

// prepareCxRespTransformer creates transformer for Codex Responses API client
func prepareCxRespTransformer(endpoint config.Endpoint, endpointTransformer string) (transformer.Transformer, error) {
	switch endpointTransformer {
	case "claude":
		model := endpoint.Model
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}
		return responses.NewClaudeTransformer(model), nil
	case "openai":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("OpenAI transformer requires model field")
		}
		return responses.NewOpenAITransformer(endpoint.Model), nil
	case "openai2":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("OpenAI2 transformer requires model field")
		}
		return responses.NewOpenAI2Transformer(endpoint.Model), nil
	case "gemini":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("Gemini transformer requires model field")
		}
		return responses.NewGeminiTransformer(endpoint.Model), nil
	case "cli":
		if endpoint.Model == "" {
			return nil, fmt.Errorf("CLI transformer requires model field")
		}
		return responses.NewCLITransformer(endpoint.Model, endpoint.APIKey), nil
	case "passthrough":
		return passthrough.NewPassthroughTransformer(endpoint.Model), nil
	default:
		return nil, fmt.Errorf("unsupported endpoint transformer for Codex Responses: %s", endpointTransformer)
	}
}

// getTargetPath determines the target API path based on transformer name
func getTargetPath(originalPath string, endpoint config.Endpoint, transformedBody []byte, transformerName string) string {
	switch transformerName {
	case "cc_claude", "cx_chat_claude", "cx_resp_claude":
		return "/v1/messages"
	case "cc_openai", "cx_chat_openai", "cx_resp_openai":
		return "/v1/chat/completions"
	case "cc_openai2", "cx_resp_openai2", "cx_chat_openai2":
		return "/v1/responses"
	case "cc_gemini", "cx_chat_gemini", "cx_resp_gemini":
		var geminiReq struct {
			Stream bool `json:"stream"`
		}
		json.Unmarshal(transformedBody, &geminiReq)
		if geminiReq.Stream {
			return fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", endpoint.Model)
		}
		return fmt.Sprintf("/v1beta/models/%s:generateContent", endpoint.Model)
	case "cc_cli", "cx_chat_cli", "cx_resp_cli":
		return "/v1/messages?beta=true"
	}
	return originalPath
}

// buildProxyRequest creates an HTTP request for the target API
func buildProxyRequest(r *http.Request, endpoint config.Endpoint, transformedBody []byte, transformerName string) (*http.Request, error) {
	targetPath := getTargetPath(r.URL.Path, endpoint, transformedBody, transformerName)
	if targetPath == "" {
		targetPath = r.URL.Path
	}

	normalizedAPIUrl := normalizeAPIUrl(endpoint.APIUrl)
	targetURL := fmt.Sprintf("%s%s", normalizedAPIUrl, targetPath)
	if r.URL.RawQuery != "" {
		if strings.Contains(targetPath, "?") {
			targetURL += "&" + r.URL.RawQuery
		} else {
			targetURL += "?" + r.URL.RawQuery
		}
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(transformedBody))
	if err != nil {
		return nil, err
	}

	// Copy headers (except Host and Accept-Encoding)
	for key, values := range r.Header {
		if key == "Host" || key == "Accept-Encoding" {
			continue
		}
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Force gzip or no compression to avoid unsupported encodings (e.g., brotli)
	proxyReq.Header.Set("Accept-Encoding", "gzip, identity")

	// Set authentication based on transformer type
	switch transformerName {
	case "cc_openai", "cc_openai2", "cx_chat_openai", "cx_chat_openai2", "cx_resp_openai", "cx_resp_openai2":
		proxyReq.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	case "cc_gemini", "cx_chat_gemini", "cx_resp_gemini":
		q := proxyReq.URL.Query()
		q.Set("key", endpoint.APIKey)
		q.Set("alt", "sse")
		proxyReq.URL.RawQuery = q.Encode()
	case "cc_cli", "cx_chat_cli", "cx_resp_cli":
		// CLI transformer uses custom headers from convert package
		// Parse tools from request body to build dynamic betas
		var cliReq struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		json.Unmarshal(transformedBody, &cliReq)
		betas := convert.BuildClaudeCliBetas(cliReq.Tools)
		cliHeaders := convert.BuildClaudeCliHeaders(endpoint.APIKey, betas, true)
		for k, v := range cliHeaders {
			proxyReq.Header.Set(k, v)
		}
	default:
		// Claude endpoints
		proxyReq.Header.Set("x-api-key", endpoint.APIKey)
		proxyReq.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	}

	// Set Host header
	hostOnly := strings.TrimPrefix(strings.TrimPrefix(normalizedAPIUrl, "https://"), "http://")
	proxyReq.Header.Set("Host", hostOnly)

	return proxyReq, nil
}

// sendRequest sends the HTTP request and returns the response
func sendRequest(ctx context.Context, proxyReq *http.Request, httpClient *http.Client, cfg *config.Config) (*http.Response, error) {
	proxyReq = proxyReq.WithContext(ctx)

	// Apply proxy if configured
	if proxyCfg := cfg.GetProxy(); proxyCfg != nil && proxyCfg.URL != "" {
		// Clone the client and replace transport for this request
		clientWithProxy := &http.Client{
			Timeout: httpClient.Timeout,
		}

		transport, err := CreateProxyTransport(proxyCfg.URL)
		if err != nil {
			logger.Warn("Failed to create proxy transport: %v, using direct connection", err)
			clientWithProxy.Transport = httpClient.Transport
		} else {
			clientWithProxy.Transport = transport
			logger.Debug("Using proxy: %s", proxyCfg.URL)
		}

		return clientWithProxy.Do(proxyReq)
	}

	return httpClient.Do(proxyReq)
}

// CreateProxyTransport creates an http.Transport with proxy support
func CreateProxyTransport(proxyURL string) (*http.Transport, error) {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}

	transport := &http.Transport{
		TLSClientConfig:        &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:           100,
		MaxIdleConnsPerHost:    10,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    10 * time.Second,
		ExpectContinueTimeout:  1 * time.Second,
		ResponseHeaderTimeout:  30 * time.Second,
		WriteBufferSize:        128 * 1024, // 128KB write buffer for large SSE streams
		ReadBufferSize:         128 * 1024, // 128KB read buffer for large SSE streams
		MaxResponseHeaderBytes: 64 * 1024,  // 64KB max response headers
	}

	switch parsed.Scheme {
	case "socks5", "socks5h":
		auth := &proxy.Auth{}
		if parsed.User != nil {
			auth.User = parsed.User.Username()
			auth.Password, _ = parsed.User.Password()
		} else {
			auth = nil
		}
		dialer, err := proxy.SOCKS5("tcp", parsed.Host, auth, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	case "http", "https":
		transport.Proxy = http.ProxyURL(parsed)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", parsed.Scheme)
	}

	return transport, nil
}
