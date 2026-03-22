package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/google/uuid"
	"github.com/lich0821/ccNexus/internal/storage"
)

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	Event string
	Data  string
}

// Usage represents token usage information from API response
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// APIResponse represents the structure of API responses to extract usage
type APIResponse struct {
	Usage Usage `json:"usage"`
}

// Proxy represents the proxy server
type Proxy struct {
	config            *config.Config
	storage           *storage.SQLiteStorage
	stats             *Stats
	trafficRecorder   *TrafficRecorder // traffic log recorder
	currentIndex      int
	mu                sync.RWMutex
	server            *http.Server
	httpClient        *http.Client                  // Reusable HTTP client with connection pool
	activeRequests    map[string]int                // tracks active request count by endpoint name
	activeRequestsMu  sync.RWMutex                  // protects activeRequests map
	endpointCtx       map[string]context.Context    // context per endpoint for cancellation
	endpointCancel    map[string]context.CancelFunc // cancel functions per endpoint
	ctxMu             sync.RWMutex                  // protects context maps
	onEndpointSuccess func(endpointName string)     // callback when endpoint request succeeds
}

// New creates a new Proxy instance
func New(cfg *config.Config, statsStorage StatsStorage, sqliteStorage *storage.SQLiteStorage, deviceID string) *Proxy {
	stats := NewStats(statsStorage, deviceID)

	// Create a reusable HTTP client with connection pool
	// Enhanced configuration for large SSE streaming and HTTP/2 support
	httpClient := &http.Client{
		Timeout: 300 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:        &tls.Config{InsecureSkipVerify: true},
			MaxIdleConns:           100,
			MaxIdleConnsPerHost:    10,
			IdleConnTimeout:        90 * time.Second,
			TLSHandshakeTimeout:    10 * time.Second,
			ExpectContinueTimeout:  1 * time.Second,
			ResponseHeaderTimeout:  90 * time.Second,
			WriteBufferSize:        128 * 1024, // 128KB write buffer for large SSE streams
			ReadBufferSize:         128 * 1024, // 128KB read buffer for large SSE streams
			MaxResponseHeaderBytes: 64 * 1024,  // 64KB max response headers
		},
	}

	return &Proxy{
		config:          cfg,
		storage:         sqliteStorage,
		stats:           stats,
		trafficRecorder: NewTrafficRecorder(),
		currentIndex:    0,
		httpClient:      httpClient,
		activeRequests:  make(map[string]int),
		endpointCtx:     make(map[string]context.Context),
		endpointCancel:  make(map[string]context.CancelFunc),
	}
}

// SetOnEndpointSuccess sets the callback for successful endpoint requests
func (p *Proxy) SetOnEndpointSuccess(callback func(endpointName string)) {
	p.onEndpointSuccess = callback
}

// GetTrafficRecorder returns the traffic recorder
func (p *Proxy) GetTrafficRecorder() *TrafficRecorder {
	return p.trafficRecorder
}

func (p *Proxy) recordTrafficLog(requestID string, log *TrafficLog) {
	if log == nil {
		return
	}
	log.RequestID = requestID
	if log.EventType == "" {
		log.EventType = TrafficEventTypeUnified
	}
	p.trafficRecorder.Record(log)
}

// Start starts the proxy server
func (p *Proxy) Start() error {
	return p.StartWithMux(nil, nil)
}

// StartWithMux starts the proxy server with an optional custom mux.
// handlerWrapper, when non-nil, wraps the mux (e.g. for CORS middleware).
func (p *Proxy) StartWithMux(customMux *http.ServeMux, handlerWrapper func(http.Handler) http.Handler) error {
	port := p.config.GetPort()

	var mux *http.ServeMux
	if customMux != nil {
		mux = customMux
	} else {
		mux = http.NewServeMux()
	}

	// Register proxy routes
	mux.HandleFunc("/", p.handleProxy)
	mux.HandleFunc("/v1/messages/count_tokens", p.handleCountTokens)
	mux.HandleFunc("/health", p.handleHealth)
	mux.HandleFunc("/stats", p.handleStats)

	handler := http.Handler(mux)
	if handlerWrapper != nil {
		handler = handlerWrapper(mux)
	}

	p.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
	}

	logger.Info("ccNexus 启动于端口 %d", port)
	logger.Info("已配置 %d 个端点", len(p.config.GetEndpoints()))

	return p.server.ListenAndServe()
}

// Stop stops the proxy server and closes idle connections
func (p *Proxy) Stop() error {
	if p.httpClient != nil {
		p.httpClient.CloseIdleConnections()
	}
	if p.server != nil {
		return p.server.Close()
	}
	return nil
}

// getEnabledEndpoints returns only the enabled endpoints
func (p *Proxy) getEnabledEndpoints() []config.Endpoint {
	allEndpoints := p.config.GetEndpoints()
	enabled := make([]config.Endpoint, 0)
	for _, ep := range allEndpoints {
		if ep.Enabled {
			enabled = append(enabled, ep)
		}
	}
	return enabled
}

// getCurrentEndpoint returns the current endpoint (thread-safe)
func (p *Proxy) getCurrentEndpoint() config.Endpoint {
	p.mu.RLock()
	defer p.mu.RUnlock()

	endpoints := p.getEnabledEndpoints()
	if len(endpoints) == 0 {
		// Return empty endpoint if no enabled endpoints
		return config.Endpoint{}
	}
	// Make sure currentIndex is within bounds
	index := p.currentIndex % len(endpoints)
	return endpoints[index]
}

// markRequestActive marks an endpoint as having active requests
func (p *Proxy) markRequestActive(endpointName string) {
	p.activeRequestsMu.Lock()
	defer p.activeRequestsMu.Unlock()
	p.activeRequests[endpointName]++
}

// markRequestInactive decrements active request count for an endpoint
func (p *Proxy) markRequestInactive(endpointName string) {
	p.activeRequestsMu.Lock()
	defer p.activeRequestsMu.Unlock()
	if p.activeRequests[endpointName] > 0 {
		p.activeRequests[endpointName]--
	}
	if p.activeRequests[endpointName] == 0 {
		delete(p.activeRequests, endpointName)
	}
}

// hasActiveRequests checks if an endpoint has active requests
func (p *Proxy) hasActiveRequests(endpointName string) bool {
	p.activeRequestsMu.RLock()
	defer p.activeRequestsMu.RUnlock()
	return p.activeRequests[endpointName] > 0
}

// isCurrentEndpoint checks if the given endpoint is still the current one
func (p *Proxy) isCurrentEndpoint(endpointName string) bool {
	current := p.getCurrentEndpoint()
	return current.Name == endpointName
}

// getEndpointContext returns a context for the given endpoint, creating one if needed
func (p *Proxy) getEndpointContext(endpointName string) context.Context {
	p.ctxMu.Lock()
	defer p.ctxMu.Unlock()

	if ctx, ok := p.endpointCtx[endpointName]; ok {
		return ctx
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.endpointCtx[endpointName] = ctx
	p.endpointCancel[endpointName] = cancel
	return ctx
}

// cancelEndpointRequests cancels all requests for the given endpoint
func (p *Proxy) cancelEndpointRequests(endpointName string) {
	p.ctxMu.Lock()
	defer p.ctxMu.Unlock()

	if cancel, ok := p.endpointCancel[endpointName]; ok {
		cancel()
		delete(p.endpointCtx, endpointName)
		delete(p.endpointCancel, endpointName)
	}
}

// rotateEndpoint switches to the next endpoint (thread-safe)
// waitForActive: if true, waits briefly for active requests to complete before switching
func (p *Proxy) rotateEndpoint() config.Endpoint {
	// First, check if we need to wait for active requests
	oldEndpoint := p.getCurrentEndpoint()
	if p.hasActiveRequests(oldEndpoint.Name) {
		logger.Debug("[SWITCH] Waiting for active requests on %s to complete...", oldEndpoint.Name)

		// Wait outside of the main lock to avoid blocking other operations
		for i := 0; i < 10; i++ { // Check 10 times, 50ms each = 500ms max
			time.Sleep(50 * time.Millisecond)
			if !p.hasActiveRequests(oldEndpoint.Name) {
				break
			}
		}
	}

	// Now acquire lock and perform the rotation
	p.mu.Lock()
	defer p.mu.Unlock()

	endpoints := p.getEnabledEndpoints()
	if len(endpoints) == 0 {
		return config.Endpoint{}
	}

	oldIndex := p.currentIndex % len(endpoints)
	oldEndpoint = endpoints[oldIndex]

	// Calculate next index
	p.currentIndex = (oldIndex + 1) % len(endpoints)

	newEndpoint := endpoints[p.currentIndex]
	if len(endpoints) > 1 && oldEndpoint.Name != newEndpoint.Name {
		logger.Debug("[SWITCH] %s → %s (#%d)", oldEndpoint.Name, newEndpoint.Name, p.currentIndex+1)
	}

	return newEndpoint
}

// GetCurrentEndpointName returns the current endpoint name (thread-safe)
func (p *Proxy) GetCurrentEndpointName() string {
	endpoint := p.getCurrentEndpoint()
	return endpoint.Name
}

// SetCurrentEndpoint manually switches to a specific endpoint by name
// Returns error if endpoint not found or not enabled
// Thread-safe and cancels ongoing requests on the old endpoint
func (p *Proxy) SetCurrentEndpoint(targetName string) error {
	p.mu.Lock()

	endpoints := p.getEnabledEndpoints()
	if len(endpoints) == 0 {
		p.mu.Unlock()
		return fmt.Errorf("no enabled endpoints")
	}

	// Find the endpoint by name
	for i, ep := range endpoints {
		if ep.Name == targetName {
			oldEndpoint := endpoints[p.currentIndex%len(endpoints)]
			oldEndpointName := ""
			if oldEndpoint.Name != targetName {
				oldEndpointName = oldEndpoint.Name
			}
			p.currentIndex = i
			logger.Info("[手动切换] %s → %s", oldEndpoint.Name, ep.Name)
			p.mu.Unlock()

			// Cancel requests on old endpoint after releasing mu lock to avoid deadlock
			if oldEndpointName != "" {
				p.cancelEndpointRequests(oldEndpointName)
			}
			return nil
		}
	}

	p.mu.Unlock()
	return fmt.Errorf("endpoint '%s' not found or not enabled", targetName)
}

// ClientFormat represents the API format used by the client
type ClientFormat string

const (
	ClientFormatClaude          ClientFormat = "claude"           // Claude Code: /v1/messages
	ClientFormatOpenAIChat      ClientFormat = "openai_chat"      // Codex (chat): /v1/chat/completions
	ClientFormatOpenAIResponses ClientFormat = "openai_responses" // Codex (responses): /v1/responses
)

// detectClientFormat identifies the client format based on request path.
// Uses Contains so that paths with a prefix (e.g. reverse proxy /ccnexus/v1/chat/completions) are still detected correctly.
func detectClientFormat(path string) ClientFormat {
	var format ClientFormat
	switch {
	case strings.Contains(path, "/v1/chat/completions") || strings.Contains(path, "/chat/completions"):
		format = ClientFormatOpenAIChat
	case strings.Contains(path, "/v1/responses") || strings.Contains(path, "/responses"):
		format = ClientFormatOpenAIResponses
	default:
		format = ClientFormatClaude
	}
	logger.Debug("[格式检测] 路径: %s → 客户端格式: %s", path, format)
	return format
}

// handleProxy handles the main proxy logic
func (p *Proxy) handleProxy(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := uuid.New().String()

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	requestStart := time.Now()
	reqBytes := len(bodyBytes)

	// Detect client format
	clientFormat := detectClientFormat(r.URL.Path)

	logger.Debug("代理请求: %s %s | 格式: %s | 请求体: %d 字节", r.Method, r.URL.Path, clientFormat, len(bodyBytes))

	// Write full request body to debug.log file only
	if len(bodyBytes) == 0 {
		logger.DebugLog("Request body is EMPTY")
	} else {
		logger.DebugLog("Request body: %s", string(bodyBytes))
	}

	var streamReq struct {
		Model    string      `json:"model"`
		Thinking interface{} `json:"thinking"`
		Stream   bool        `json:"stream"`
	}
	json.Unmarshal(bodyBytes, &streamReq)

	endpoints := p.getEnabledEndpoints()
	if len(endpoints) == 0 {
		logger.Error("没有可用的端点")
		http.Error(w, "No enabled endpoints configured", http.StatusServiceUnavailable)
		return
	}

	maxRetries := p.computeMaxRetries(endpoints)
	endpointAttempts := 0
	lastEndpointName := ""
	refreshedCredentialAttempts := make(map[int64]bool)

	for retry := 0; retry < maxRetries; retry++ {
		endpoint := p.getCurrentEndpoint()
		if endpoint.Name == "" {
			http.Error(w, "No enabled endpoints available", http.StatusServiceUnavailable)
			return
		}

		// Reset attempts counter if endpoint changed (e.g., manual switch)
		if lastEndpointName != "" && lastEndpointName != endpoint.Name {
			endpointAttempts = 0
		}
		lastEndpointName = endpoint.Name

		endpointAttempts++
		p.markRequestActive(endpoint.Name)

		authMode := config.NormalizeAuthMode(endpoint.AuthMode)
		apiKey := strings.TrimSpace(endpoint.APIKey)
		credentialID := int64(0)
		var selectedCredential *storage.EndpointCredential
		if config.IsTokenPoolAuthMode(authMode) {
			credential, err := p.selectCredential(endpoint.Name)
			if err != nil {
				logger.Warn("[%s] Failed to select token pool credential: %v", endpoint.Name, err)
				p.stats.RecordError(endpoint.Name)
				p.markRequestInactive(endpoint.Name)
				if endpointAttempts >= 2 {
					p.rotateEndpoint()
					endpointAttempts = 0
				}
				continue
			}
			if credential == nil || strings.TrimSpace(credential.AccessToken) == "" {
				logger.Warn("[%s] No usable token in token pool", endpoint.Name)
				p.stats.RecordError(endpoint.Name)
				p.markRequestInactive(endpoint.Name)
				if endpointAttempts >= 2 {
					p.rotateEndpoint()
					endpointAttempts = 0
				}
				continue
			}
			selectedCredential = credential
			if shouldTryCredentialRefresh(credential, time.Now().UTC()) {
				refreshed, refreshErr := p.refreshCredential(endpoint, credential)
				if refreshErr != nil {
					logger.Warn("[%s] Preflight credential refresh failed (id=%d): %v", endpoint.Name, credential.ID, refreshErr)
				} else {
					selectedCredential = refreshed
					refreshedCredentialAttempts[refreshed.ID] = true
				}
			}
			apiKey = strings.TrimSpace(credential.AccessToken)
			if selectedCredential != nil {
				apiKey = strings.TrimSpace(selectedCredential.AccessToken)
				credentialID = selectedCredential.ID
			}
		} else if apiKey == "" {
			logger.Warn("[%s] API key mode but apiKey is empty", endpoint.Name)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)
			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		trans, err := prepareTransformerForClient(clientFormat, endpoint, bodyBytes)
		if err != nil {
			logger.Error("[%s] %v", endpoint.Name, err)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)

			// Record traffic log for transformer error
			p.recordTrafficLog(requestID, &TrafficLog{
				Timestamp:       startTime,
				EndpointName:    endpoint.Name,
				ClientFormat:    string(clientFormat),
				Method:          r.Method,
				Path:            r.URL.Path,
				Duration:        time.Since(startTime),
				Error:           err.Error(),
				OriginalRequest: bodyBytes,
			})

			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		transformerName := trans.Name()

		transformedBody, err := trans.TransformRequest(bodyBytes)
		if err != nil {
			logger.Error("[%s] 请求转换失败: %v", endpoint.Name, err)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)

			// Record traffic log for transform error
			p.recordTrafficLog(requestID, &TrafficLog{
				Timestamp:       startTime,
				EndpointName:    endpoint.Name,
				ClientFormat:    string(clientFormat),
				TransformerName: transformerName,
				Method:          r.Method,
				Path:            r.URL.Path,
				Duration:        time.Since(startTime),
				Error:           err.Error(),
				OriginalRequest: bodyBytes,
			})

			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		logger.DebugLog("[%s] Transformer: %s | Transformed request: %s", endpoint.Name, transformerName, string(transformedBody))

		cleanedBody, err := cleanIncompleteToolCalls(transformedBody)
		if err != nil {
			logger.Warn("[%s] Failed to clean tool calls: %v", endpoint.Name, err)
			cleanedBody = transformedBody
		}
		transformedBody = cleanedBody
		if config.NormalizeAuthMode(endpoint.AuthMode) == config.AuthModeCodexTokenPool {
			transformedBody = overrideModelInPayload(transformedBody, endpoint.Model)
		}

		// Apply request overrides if configured
		if endpoint.RequestOverrides != "" {
			transformedBody, err = applyRequestOverrides(transformedBody, endpoint.RequestOverrides)
			if err != nil {
				logger.Warn("[%s] Failed to apply request overrides: %v", endpoint.Name, err)
			} else {
				logger.DebugLog("[%s] Applied request overrides: %s", endpoint.Name, endpoint.RequestOverrides)
			}
		}

		modelName := strings.TrimSpace(streamReq.Model)
		if modelName == "" || (authMode == config.AuthModeCodexTokenPool && strings.TrimSpace(endpoint.Model) != "") {
			modelName = endpoint.Model
		}

		var thinkingEnabled bool
		{
			var transformedReq map[string]interface{}
			if err := json.Unmarshal(transformedBody, &transformedReq); err == nil {
				// CLI/Claude 格式：检测 thinking.type == "enabled"
				if thinking, ok := transformedReq["thinking"].(map[string]interface{}); ok {
					if thinkingType, ok := thinking["type"].(string); ok {
						switch strings.ToLower(strings.TrimSpace(thinkingType)) {
						case "enabled", "adaptive":
							thinkingEnabled = true
						}
					}
				}
				// OpenAI 格式：检测 enable_thinking 字段
				if !thinkingEnabled {
					if enable, ok := transformedReq["enable_thinking"].(bool); ok {
						thinkingEnabled = enable
					}
				}
			}
		}

		proxyReq, err := buildProxyRequest(r, endpoint, apiKey, transformedBody, transformerName, selectedCredential, thinkingEnabled)
		if err != nil {
			logger.Error("[%s] Failed to create request: %v", endpoint.Name, err)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)

			// Record traffic log for request build error
			p.recordTrafficLog(requestID, &TrafficLog{
				Timestamp:          startTime,
				EndpointName:       endpoint.Name,
				ClientFormat:       string(clientFormat),
				TransformerName:    transformerName,
				Method:             r.Method,
				Path:               r.URL.Path,
				Duration:           time.Since(startTime),
				Error:              err.Error(),
				OriginalRequest:    bodyBytes,
				TransformedRequest: transformedBody,
			})

			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		proxyURL := resolveProxyURLForRequest(p.config, proxyReq.URL)
		proxyLabel := strings.TrimSpace(proxyURL)
		if streamReq.Stream {
			if proxyLabel == "" {
				logger.Debug("[%s] Streaming %s %d", endpoint.Name, modelName, reqBytes)
			} else {
				logger.Debug("[%s] Streaming %s %d %s", endpoint.Name, modelName, reqBytes, proxyLabel)
			}
		} else {
			if proxyLabel == "" {
				logger.Debug("[%s] Requesting %s %d", endpoint.Name, modelName, reqBytes)
			} else {
				logger.Debug("[%s] Requesting %s %d %s", endpoint.Name, modelName, reqBytes, proxyLabel)
			}
		}

		ctx := p.getEndpointContext(endpoint.Name)
		resp, err := sendRequest(ctx, proxyReq, p.httpClient, p.config)
		if err != nil {
			// Some HTTP errors may return non-nil resp, ensure cleanup
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			logger.Error("[%s] Request failed: %v", endpoint.Name, err)
			if isTransientNetworkError(err) {
				logger.Warn("[%s] Transient network error, retrying same endpoint: %v", endpoint.Name, err)
				p.markRequestInactive(endpoint.Name)
				time.Sleep(300 * time.Millisecond)
				endpointAttempts = 0
				continue
			}
			p.markCredentialFailure(credentialID, 0, err.Error())
			p.recordCredentialUsage(credentialID, endpoint.Name, 0, 1, 0, 0)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)

			// Record traffic log for request send error
			p.recordTrafficLog(requestID, &TrafficLog{
				Timestamp:          startTime,
				EndpointName:       endpoint.Name,
				ClientFormat:       string(clientFormat),
				TransformerName:    transformerName,
				Method:             r.Method,
				Path:               r.URL.Path,
				Duration:           time.Since(startTime),
				Error:              err.Error(),
				OriginalRequest:    bodyBytes,
				TransformedRequest: transformedBody,
			})

			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		if resp.StatusCode == http.StatusOK {
			p.captureCodexRateLimitsFromHeaders(endpoint, credentialID, resp.Header)
		}

		contentType := resp.Header.Get("Content-Type")
		isStreaming := shouldHandleAsStreamingResponse(contentType, streamReq.Stream, endpoint, transformerName)

		// Codex backend enforces stream=true upstream for /responses in some environments.
		// Bridge to non-stream client responses regardless of upstream Content-Type quirks.
		if resp.StatusCode == http.StatusOK && !streamReq.Stream && shouldAggregateCodexStreaming(endpoint, transformerName) {
			inputTokens, outputTokens, outputText, err := p.handleStreamingAsNonStreaming(w, resp, endpoint, trans, credentialID)
			if err == nil {
				// Fallback: estimate tokens when usage is missing.
				if inputTokens == 0 || outputTokens == 0 {
					inputTokens, outputTokens = p.estimateTokens(bodyBytes, outputText, inputTokens, outputTokens, endpoint.Name)
				}

				p.stats.RecordRequest(endpoint.Name)
				p.stats.RecordTokens(endpoint.Name, inputTokens, outputTokens)
				p.recordCredentialUsage(credentialID, endpoint.Name, 1, 0, inputTokens, outputTokens)
				p.markCredentialSuccess(credentialID)
				p.markRequestInactive(endpoint.Name)
				if p.onEndpointSuccess != nil {
					p.onEndpointSuccess(endpoint.Name)
				}
				totalElapsed := time.Since(requestStart).Round(time.Millisecond)
				logger.Debug("[%s] Requested tokens=%d/%d latency=%s cred_id=%d", endpoint.Name, inputTokens, outputTokens, totalElapsed, credentialID)
				return
			}
			logger.Warn("[%s] Failed to aggregate streaming response as non-stream: %v", endpoint.Name, err)
			p.markCredentialFailure(credentialID, 0, err.Error())
			p.recordCredentialUsage(credentialID, endpoint.Name, 0, 1, 0, 0)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)
			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		if resp.StatusCode == http.StatusOK && isStreaming {
			inputTokens, outputTokens, outputText, originalResp, transformedResp := p.handleStreamingResponse(w, resp, endpoint, trans, transformerName, thinkingEnabled, modelName, bodyBytes, credentialID)

			// Fallback: estimate tokens when usage is 0
			if inputTokens == 0 || outputTokens == 0 {
				inputTokens, outputTokens = p.estimateTokens(bodyBytes, outputText, inputTokens, outputTokens, endpoint.Name)
			}

			p.stats.RecordRequest(endpoint.Name)
			p.stats.RecordTokens(endpoint.Name, inputTokens, outputTokens)
			p.recordCredentialUsage(credentialID, endpoint.Name, 1, 0, inputTokens, outputTokens)
			p.markCredentialSuccess(credentialID)
			p.markRequestInactive(endpoint.Name)

			// Record traffic log for successful streaming response
			p.recordTrafficLog(requestID, &TrafficLog{
				ID:                  requestID,
				Timestamp:           startTime,
				EndpointName:        endpoint.Name,
				ClientFormat:        string(clientFormat),
				TransformerName:     transformerName,
				Method:              r.Method,
				Path:                r.URL.Path,
				StatusCode:          resp.StatusCode,
				Duration:            time.Since(startTime),
				InputTokens:         inputTokens,
				OutputTokens:        outputTokens,
				IsStreaming:         true,
				OriginalRequest:     bodyBytes,
				TransformedRequest:  transformedBody,
				OriginalResponse:    originalResp,
				TransformedResponse: transformedResp,
			})

			if p.onEndpointSuccess != nil {
				p.onEndpointSuccess(endpoint.Name)
			}
			totalElapsed := time.Since(requestStart).Round(time.Millisecond)
			logger.Debug("[%s] Requested tokens=%d/%d latency=%s cred_id=%d", endpoint.Name, inputTokens, outputTokens, totalElapsed, credentialID)
			return
		}

		if resp.StatusCode == http.StatusOK {
			inputTokens, outputTokens, originalResp, transformedResp, err := p.handleNonStreamingResponse(w, resp, endpoint, trans)
			if err != nil {
				// Transform failed: send error to client + clean up
				logger.Error("[%s] Non-streaming response transformation failed: %v", endpoint.Name, err)
				p.stats.RecordRequest(endpoint.Name)
				p.stats.RecordError(endpoint.Name)
				p.markRequestInactive(endpoint.Name)
				http.Error(w, fmt.Sprintf(`{"error":{"message":"response transformation failed: %s","type":"proxy_error"}}`, err.Error()), http.StatusBadGateway)

				// Record traffic log for failed non-streaming response
				p.recordTrafficLog(requestID, &TrafficLog{
					Timestamp:          startTime,
					EndpointName:       endpoint.Name,
					ClientFormat:       string(clientFormat),
					TransformerName:    transformerName,
					Method:             r.Method,
					Path:               r.URL.Path,
					StatusCode:         http.StatusBadGateway,
					Duration:           time.Since(startTime),
					IsStreaming:        false,
					Error:              err.Error(),
					OriginalRequest:    bodyBytes,
					TransformedRequest: transformedBody,
					OriginalResponse:   originalResp,
				})
				totalElapsed := time.Since(requestStart).Round(time.Millisecond)
				logger.Debug("[%s] Transform error latency=%s cred_id=%d", endpoint.Name, totalElapsed, credentialID)
				return
			}

			p.stats.RecordRequest(endpoint.Name)
			p.stats.RecordTokens(endpoint.Name, inputTokens, outputTokens)
			p.recordCredentialUsage(credentialID, endpoint.Name, 1, 0, inputTokens, outputTokens)
			p.markCredentialSuccess(credentialID)
			p.markRequestInactive(endpoint.Name)

			// Record traffic log for successful non-streaming response
			p.recordTrafficLog(requestID, &TrafficLog{
				Timestamp:           startTime,
				EndpointName:        endpoint.Name,
				ClientFormat:        string(clientFormat),
				TransformerName:     transformerName,
				Method:              r.Method,
				Path:                r.URL.Path,
				StatusCode:          resp.StatusCode,
				Duration:            time.Since(startTime),
				InputTokens:         inputTokens,
				OutputTokens:        outputTokens,
				IsStreaming:         false,
				OriginalRequest:     bodyBytes,
				TransformedRequest:  transformedBody,
				OriginalResponse:    originalResp,
				TransformedResponse: transformedResp,
			})

			if p.onEndpointSuccess != nil {
				p.onEndpointSuccess(endpoint.Name)
			}
			totalElapsed := time.Since(requestStart).Round(time.Millisecond)
			logger.Debug("[%s] Requested tokens=%d/%d latency=%s cred_id=%d", endpoint.Name, inputTokens, outputTokens, totalElapsed, credentialID)
			return
		}

		if shouldRetry(resp.StatusCode) {
			var errBody []byte
			if resp.Header.Get("Content-Encoding") == "gzip" {
				errBody, err = decompressGzip(resp.Body)
			} else {
				errBody, err = io.ReadAll(resp.Body)
			}
			resp.Body.Close()
			if err != nil {
				logger.Warn("[%s] Failed to read error response body: %v", endpoint.Name, err)
			}
			errMsg := string(errBody)
			if len(errMsg) > 200 {
				errMsg = errMsg[:200] + "..."
			}
			logger.Warn("[%s] Request failed %d: %s", endpoint.Name, resp.StatusCode, errMsg)
			logger.DebugLog("[%s] Request failed %d: %s", endpoint.Name, resp.StatusCode, errMsg)
			p.markCredentialFailure(credentialID, resp.StatusCode, errMsg)
			p.recordCredentialUsage(credentialID, endpoint.Name, 0, 1, 0, 0)
			p.stats.RecordError(endpoint.Name)
			p.markRequestInactive(endpoint.Name)

			// Record traffic log for retry error
			p.recordTrafficLog(requestID, &TrafficLog{
				Timestamp:          startTime,
				EndpointName:       endpoint.Name,
				ClientFormat:       string(clientFormat),
				TransformerName:    transformerName,
				Method:             r.Method,
				Path:               r.URL.Path,
				StatusCode:         resp.StatusCode,
				Duration:           time.Since(startTime),
				Error:              errMsg,
				OriginalRequest:    bodyBytes,
				TransformedRequest: transformedBody,
				OriginalResponse:   errBody,
			})

			if endpointAttempts >= 2 {
				p.rotateEndpoint()
				endpointAttempts = 0
			}
			continue
		}

		var respBody []byte
		if resp.Header.Get("Content-Encoding") == "gzip" {
			respBody, err = decompressGzip(resp.Body)
		} else {
			respBody, err = io.ReadAll(resp.Body)
		}
		resp.Body.Close()
		if err != nil {
			logger.Error("[%s] Failed to read response body: %v", endpoint.Name, err)
			p.markRequestInactive(endpoint.Name)
			http.Error(w, "Failed to read upstream response", http.StatusBadGateway)
			return
		}
		skipCredentialPenalty := false

		// Token pool mode: on 401/403, invalidate current credential and retry within the same endpoint.
		if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && credentialID > 0 {
			errMsg := string(respBody)
			if len(errMsg) > 500 {
				errMsg = errMsg[:500] + "..."
			}
			if !shouldTreatCredentialAuthFailure(resp.StatusCode, errMsg) {
				skipCredentialPenalty = true
				logger.Warn("[%s] Upstream %d looks like route/gateway denial, skipping credential invalidation", endpoint.Name, resp.StatusCode)
			}
			if skipCredentialPenalty {
				p.stats.RecordError(endpoint.Name)
				p.markRequestInactive(endpoint.Name)
			} else {
				if selectedCredential != nil &&
					isCodexProviderType(selectedCredential.ProviderType) &&
					strings.TrimSpace(selectedCredential.RefreshToken) != "" &&
					!refreshedCredentialAttempts[credentialID] {
					refreshedCredentialAttempts[credentialID] = true
					refreshed, refreshErr := p.refreshCredential(endpoint, selectedCredential)
					if refreshErr == nil {
						logger.Info("[%s] Credential refreshed after %d, retrying with updated token (id=%d)", endpoint.Name, resp.StatusCode, credentialID)
						p.markRequestInactive(endpoint.Name)
						endpointAttempts = 0
						if refreshed != nil && refreshed.ID > 0 {
							refreshedCredentialAttempts[refreshed.ID] = true
						}
						continue
					}
					logger.Warn("[%s] Credential refresh failed after %d (id=%d): %v", endpoint.Name, resp.StatusCode, credentialID, refreshErr)
				}
				p.markCredentialFailure(credentialID, resp.StatusCode, errMsg)
				p.recordCredentialUsage(credentialID, endpoint.Name, 0, 1, 0, 0)
				p.stats.RecordError(endpoint.Name)
				p.markRequestInactive(endpoint.Name)
				endpointAttempts = 0
				logger.Warn("[%s] Credential auth failed (%d), retrying with next token", endpoint.Name, resp.StatusCode)
				continue
			}
		}

		p.markRequestInactive(endpoint.Name)
		// Log non-200 responses for debugging
		if resp.StatusCode != http.StatusOK {
			errMsg := string(respBody)
			if len(errMsg) > 500 {
				errMsg = errMsg[:500] + "..."
			}
			if resp.StatusCode == http.StatusBadRequest &&
				strings.Contains(errMsg, "api.responses.write") &&
				strings.Contains(transformerName, "openai2") {
				logger.Warn("[%s] Upstream rejected /v1/responses scope (api.responses.write). Try transformer=openai (chat/completions) for this token.", endpoint.Name)
			}
			if skipCredentialPenalty {
				p.markCredentialFailure(credentialID, 0, errMsg)
				p.recordCredentialUsage(credentialID, endpoint.Name, 0, 1, 0, 0)
			} else {
				p.markCredentialFailure(credentialID, resp.StatusCode, errMsg)
				p.recordCredentialUsage(credentialID, endpoint.Name, 0, 1, 0, 0)
			}
			logger.Warn("[%s] Response %d: %s", endpoint.Name, resp.StatusCode, errMsg)
		}
		// Remove Content-Encoding header since we've decompressed
		for key, values := range resp.Header {
			if key == "Content-Encoding" || key == "Content-Length" {
				continue
			}
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)

		// Record traffic log for non-200 passthrough response
		p.recordTrafficLog(requestID, &TrafficLog{
			Timestamp:           startTime,
			EndpointName:        endpoint.Name,
			ClientFormat:        string(clientFormat),
			TransformerName:     transformerName,
			Method:              r.Method,
			Path:                r.URL.Path,
			StatusCode:          resp.StatusCode,
			Duration:            time.Since(startTime),
			OriginalRequest:     bodyBytes,
			TransformedRequest:  transformedBody,
			OriginalResponse:    respBody,
			TransformedResponse: respBody, // Passthrough, same as original
		})

		return
	}

	http.Error(w, "All endpoints failed", http.StatusServiceUnavailable)
}

func (p *Proxy) selectCredential(endpointName string) (*storage.EndpointCredential, error) {
	if p.storage == nil {
		return nil, nil
	}
	return p.storage.GetUsableEndpointCredential(endpointName, time.Now().UTC())
}

func (p *Proxy) markCredentialSuccess(credentialID int64) {
	if credentialID <= 0 || p.storage == nil {
		return
	}
	if err := p.storage.MarkCredentialSuccess(credentialID, time.Now().UTC()); err != nil {
		logger.Warn("Failed to mark credential success (id=%d): %v", credentialID, err)
	}
}

func (p *Proxy) recordCredentialUsage(credentialID int64, endpointName string, requests, errors, inputTokens, outputTokens int) {
	if credentialID <= 0 || p.storage == nil {
		return
	}
	if err := p.storage.UpsertCredentialUsage(credentialID, endpointName, requests, errors, inputTokens, outputTokens, time.Now().UTC()); err != nil {
		logger.Warn("Failed to record credential usage (id=%d): %v", credentialID, err)
	}
}

func (p *Proxy) markCredentialFailure(credentialID int64, statusCode int, errMsg string) {
	if credentialID <= 0 || p.storage == nil {
		return
	}
	if err := p.storage.MarkCredentialFailure(credentialID, statusCode, errMsg, time.Now().UTC()); err != nil {
		logger.Warn("Failed to mark credential failure (id=%d): %v", credentialID, err)
	}
}

func (p *Proxy) computeMaxRetries(endpoints []config.Endpoint) int {
	baseRetries := len(endpoints) * 2
	if p.storage == nil || len(endpoints) == 0 {
		return baseRetries
	}

	extraRetries := 0
	for _, endpoint := range endpoints {
		if !config.IsTokenPoolAuthMode(endpoint.AuthMode) {
			continue
		}

		stats, err := p.storage.GetTokenPoolStats(endpoint.Name)
		if err != nil {
			logger.Warn("[%s] Failed to load token pool stats: %v", endpoint.Name, err)
			continue
		}

		usable := stats.Active + stats.Expiring + stats.NeedRefresh
		if usable > 1 {
			extraRetries += usable - 1
		}
	}

	maxRetries := baseRetries + extraRetries
	if maxRetries < baseRetries {
		return baseRetries
	}
	return maxRetries
}

func shouldAggregateCodexStreaming(endpoint config.Endpoint, transformerName string) bool {
	if !strings.Contains(transformerName, "openai2") {
		return false
	}
	url := strings.ToLower(strings.TrimSpace(endpoint.APIUrl))
	return strings.Contains(url, "chatgpt.com/backend-api/codex")
}

// shouldHandleAsStreamingResponse determines if an upstream 200 response should be
// processed as SSE. Some Codex upstreams intermittently omit Content-Type even when
// stream=true and body is SSE.
func shouldHandleAsStreamingResponse(contentType string, clientRequestedStream bool, endpoint config.Endpoint, transformerName string) bool {
	if strings.Contains(strings.ToLower(strings.TrimSpace(contentType)), "text/event-stream") {
		return true
	}
	if !clientRequestedStream {
		return false
	}
	// Codex /responses may return SSE with an empty content-type header.
	if shouldAggregateCodexStreaming(endpoint, transformerName) {
		return true
	}
	return false
}

func shouldTreatCredentialAuthFailure(statusCode int, body string) bool {
	if statusCode == http.StatusUnauthorized {
		return true
	}
	if statusCode != http.StatusForbidden {
		return false
	}

	lower := strings.ToLower(strings.TrimSpace(body))
	if strings.HasPrefix(lower, "<!doctype html") ||
		strings.HasPrefix(lower, "<html") ||
		strings.Contains(lower, "<head>") ||
		strings.Contains(lower, "<body") {
		return false
	}
	return true
}

func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(message, "eof") {
		return true
	}
	if strings.Contains(message, "timeout awaiting response headers") {
		return true
	}
	if strings.Contains(message, "i/o timeout") {
		return true
	}
	if strings.Contains(message, "connection reset by peer") {
		return true
	}
	return false
}

// applyRequestOverrides applies JSON overrides to the request body.
// Supports adding, replacing, and deleting fields (null values delete fields).
// Nested objects are deep-merged recursively.
func applyRequestOverrides(requestBody []byte, overridesJSON string) ([]byte, error) {
	overridesJSON = strings.TrimSpace(overridesJSON)
	if overridesJSON == "" {
		return requestBody, nil
	}

	var requestMap map[string]interface{}
	if err := json.Unmarshal(requestBody, &requestMap); err != nil {
		return nil, fmt.Errorf("failed to parse request body: %w", err)
	}

	var overrides map[string]interface{}
	if err := json.Unmarshal([]byte(overridesJSON), &overrides); err != nil {
		return nil, fmt.Errorf("failed to parse overrides JSON: %w", err)
	}

	deepMerge(requestMap, overrides)

	modifiedBody, err := json.Marshal(requestMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal modified request: %w", err)
	}

	return modifiedBody, nil
}

// deepMerge recursively merges src into dst.
// - null values in src delete the corresponding key in dst
// - nested objects are merged recursively
// - all other values in src overwrite dst
func deepMerge(dst, src map[string]interface{}) {
	for key, srcVal := range src {
		if srcVal == nil {
			delete(dst, key)
			continue
		}
		srcMap, srcIsMap := srcVal.(map[string]interface{})
		dstVal, dstExists := dst[key]
		if srcIsMap && dstExists {
			if dstMap, dstIsMap := dstVal.(map[string]interface{}); dstIsMap {
				deepMerge(dstMap, srcMap)
				continue
			}
		}
		dst[key] = srcVal
	}
}
