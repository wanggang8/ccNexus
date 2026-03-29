// Package server provides a standalone HTTP server for the Augment plugin integration.
// It listens on a dedicated port (default 2346) and handles encrypted/plaintext requests
// from the VSCode Augment plugin, converting them to Claude/OpenAI format and proxying
// to the configured upstream endpoint.
package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lich0821/ccNexus/internal/augment/decrypt"
	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/proxy"
	"github.com/lich0821/ccNexus/internal/transformer/augment"
	"github.com/lich0821/ccNexus/internal/transformer/convert"
)

// Server is the standalone Augment HTTP server.
type Server struct {
	config          *config.Config
	proxy           *proxy.Proxy
	decryptor       *decrypt.Decryptor
	trafficRecorder *proxy.TrafficRecorder
	stats           *proxy.Stats
	httpServer      *http.Server
	httpClient      *http.Client
	mu              sync.RWMutex
	running         bool
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher interface
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// limitedBuffer captures at most limit bytes (best-effort) to avoid unbounded memory usage.
type limitedBuffer struct {
	data  []byte
	limit int
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{
		data:  make([]byte, 0, min(limit, 16*1024)),
		limit: limit,
	}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 || len(p) == 0 {
		return len(p), nil
	}

	remaining := b.limit - len(b.data)
	if remaining > 0 {
		if len(p) <= remaining {
			b.data = append(b.data, p...)
		} else {
			b.data = append(b.data, p[:remaining]...)
		}
	}
	// Pretend we consumed the whole input to keep streaming conversion flowing.
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte {
	return b.data
}

type teeCapture struct {
	w   io.Writer
	cap *limitedBuffer
}

func (t *teeCapture) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 {
		_, _ = t.cap.Write(p[:n])
	}
	return n, err
}

// Flush delegates to underlying response writer so streaming behaves as before.
func (t *teeCapture) Flush() {
	if f, ok := t.w.(interface{ Flush() }); ok {
		f.Flush()
	}
}

// New creates a new Augment server instance.
func New(cfg *config.Config, p *proxy.Proxy, privateKeyPath string, trafficRecorder *proxy.TrafficRecorder, stats *proxy.Stats) (*Server, error) {
	dec, err := decrypt.New(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("augment server: failed to create decryptor: %w", err)
	}

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
			WriteBufferSize:        128 * 1024,
			ReadBufferSize:         128 * 1024,
			MaxResponseHeaderBytes: 64 * 1024,
		},
	}

	return &Server{
		config:          cfg,
		proxy:           p,
		decryptor:       dec,
		trafficRecorder: trafficRecorder,
		stats:           stats,
		httpClient:      httpClient,
	}, nil
}

// Start starts the Augment server on the configured port.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("augment server already running")
	}

	if !s.config.AugmentEnabled {
		return fmt.Errorf("augment server is disabled in config")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/v1/messages", s.handleRequest)
	mux.HandleFunc("/v1/chat/completions", s.handleRequest)
	mux.HandleFunc("/v1/responses", s.handleRequest)
	mux.HandleFunc("/chat-stream", s.handleRequest)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/models", s.handleGetModels)
	mux.HandleFunc("/usage/api/get-models", s.handleGetModels)
	mux.HandleFunc("/usage/api/balance", s.handleGetBalance)
	mux.HandleFunc("/usage/api/getLoginToken", s.handleGetLoginToken)

	// Add logging middleware that logs all requests and catches 404s
	loggedMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("Augment: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

		// Create a response writer wrapper to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		mux.ServeHTTP(rw, r)

		// Log 404s with more details
		if rw.statusCode == http.StatusNotFound {
			logger.Warn("Augment: 404 Not Found - %s %s (headers: %v)", r.Method, r.URL.Path, r.Header)
		}
	})

	addr := fmt.Sprintf(":%d", s.config.AugmentPort)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      loggedMux,
		ReadTimeout:  300 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	s.running = true
	logger.Info("Augment server starting on %s", addr)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Augment server error: %v", err)
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}
	}()

	return nil
}

// Stop stops the Augment server gracefully.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	logger.Info("Stopping Augment server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		logger.Error("Augment server shutdown error: %v", err)
		return err
	}

	s.running = false
	logger.Info("Augment server stopped")
	return nil
}

// IsRunning returns whether the server is currently running.
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// handleHealth handles health check requests.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"service": "augment-proxy",
	})
}

// handleRequest handles incoming Augment plugin requests.
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientFormat := "augment"

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("Augment: failed to read request body: %v", err)
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		if s.trafficRecorder != nil {
			s.trafficRecorder.Record(&proxy.TrafficLog{
				Timestamp:    startTime,
				ClientFormat: clientFormat,
				Method:       r.Method,
				Path:         r.URL.Path,
				StatusCode:   http.StatusBadRequest,
				Duration:     time.Since(startTime),
				Error:        err.Error(),
			})
		}
		return
	}
	defer r.Body.Close()

	ar, decrypted, err := s.parseAugmentRequest(body)
	if err != nil {
		logger.Error("Augment: failed to parse request: %v", err)
		s.writeErrorResponse(w, err.Error(), false)
		if s.trafficRecorder != nil {
			s.trafficRecorder.Record(&proxy.TrafficLog{
				Timestamp:       startTime,
				ClientFormat:    clientFormat,
				Method:          r.Method,
				Path:            r.URL.Path,
				StatusCode:      http.StatusInternalServerError,
				Duration:        time.Since(startTime),
				Error:           err.Error(),
				OriginalRequest: body,
			})
		}
		return
	}

	// Determine target type and endpoint.
	targetType, endpoint := s.selectTarget()
	if endpoint == nil {
		logger.Error("Augment: no available endpoint")
		s.writeErrorResponse(w, "No available endpoint", ar.IsStreaming())
		if s.trafficRecorder != nil {
			statusCode := http.StatusInternalServerError
			if ar.IsStreaming() {
				statusCode = http.StatusOK
			}
			s.trafficRecorder.Record(&proxy.TrafficLog{
				Timestamp:       startTime,
				ClientFormat:    clientFormat,
				Method:          r.Method,
				Path:            r.URL.Path,
				StatusCode:      statusCode,
				Duration:        time.Since(startTime),
				IsStreaming:     ar.IsStreaming(),
				Error:           "No available endpoint",
				OriginalRequest: decrypted,
			})
		}
		return
	}

	// Create transformer.
	transformer, err := augment.New(targetType, endpoint.Model)
	if err != nil {
		logger.Error("Augment: failed to create transformer: %v", err)
		s.writeErrorResponse(w, "Internal error", ar.IsStreaming())
		if s.trafficRecorder != nil {
			statusCode := http.StatusInternalServerError
			if ar.IsStreaming() {
				statusCode = http.StatusOK
			}
			s.trafficRecorder.Record(&proxy.TrafficLog{
				Timestamp:       startTime,
				EndpointName:    endpoint.Name,
				ClientFormat:    clientFormat,
				Method:          r.Method,
				Path:            r.URL.Path,
				StatusCode:      statusCode,
				Duration:        time.Since(startTime),
				IsStreaming:     ar.IsStreaming(),
				Error:           err.Error(),
				OriginalRequest: decrypted,
			})
		}
		return
	}

	// Transform request.
	transformed, err := transformer.TransformRequest(decrypted)
	if err != nil {
		logger.Error("Augment: failed to transform request: %v", err)
		s.writeErrorResponse(w, "Request transformation failed", ar.IsStreaming())
		if s.trafficRecorder != nil {
			statusCode := http.StatusInternalServerError
			if ar.IsStreaming() {
				statusCode = http.StatusOK
			}
			s.trafficRecorder.Record(&proxy.TrafficLog{
				Timestamp:       startTime,
				EndpointName:    endpoint.Name,
				ClientFormat:    clientFormat,
				TransformerName: transformer.Name(),
				Method:          r.Method,
				Path:            r.URL.Path,
				StatusCode:      statusCode,
				Duration:        time.Since(startTime),
				IsStreaming:     ar.IsStreaming(),
				Error:           err.Error(),
				OriginalRequest: decrypted,
			})
		}
		return
	}

	// Extract tool context for response transformation.
	toolContext := transformer.GetToolContext()
	transformerName := transformer.Name()

	// Proxy to upstream.
	s.proxyToUpstream(
		w,
		r,
		transformed,
		endpoint,
		targetType,
		ar.IsStreaming(),
		toolContext,
		decrypted,
		transformerName,
		startTime,
		clientFormat,
	)
}

// parseAugmentRequest returns the parsed AugmentRequest and the JSON bytes that
// should be used as transformer input (decrypted or reconstructed).
func (s *Server) parseAugmentRequest(body []byte) (*augment.AugmentRequest, []byte, error) {
	var input []byte

	// Encrypted wire format: {encrypted_data, iv}
	if decrypt.IsEncrypted(body) {
		decrypted, err := s.decryptor.Decrypt(body)
		if err != nil {
			return nil, nil, fmt.Errorf("Decryption failed")
		}
		input = decrypted
	} else {
		input = body
	}

	if ar, err := augment.ParseRequest(input); err == nil {
		if augmentLooksStructured(ar) {
			return ar, input, nil
		}
	}

	// Plaintext fallback: some clients send {data:"..."} instead of full AugmentRequest.
	reconstructed, err := decrypt.ReconstructFromPlaintext(body)
	if err != nil {
		return nil, nil, fmt.Errorf("Invalid request format")
	}
	ar, err := augment.ParseRequest(reconstructed)
	if err != nil {
		return nil, nil, fmt.Errorf("Invalid request format")
	}
	return ar, reconstructed, nil
}

func augmentLooksStructured(ar *augment.AugmentRequest) bool {
	if ar == nil {
		return false
	}
	if strings.TrimSpace(ar.Message) != "" {
		return true
	}
	if len(ar.EffectiveCurrentNodes()) > 0 || len(ar.ChatHistory) > 0 || len(ar.EffectiveTools()) > 0 {
		return true
	}
	if ctx := ar.EffectiveContext(); ctx != nil {
		return true
	}
	return false
}

// selectTarget selects the target type and endpoint based on config.
func (s *Server) selectTarget() (string, *config.Endpoint) {
	endpoints := s.config.GetEndpoints()
	if len(endpoints) == 0 {
		return "", nil
	}

	if endpoint := s.getCurrentEndpoint(endpoints); endpoint != nil {
		targetType, ok := mapEndpointTransformerToTargetType(endpoint.Transformer)
		if !ok {
			return "", nil
		}
		return targetType, endpoint
	}

	// Use first enabled endpoint, mapped strictly by endpoint.Transformer.
	for i := range endpoints {
		ep := endpoints[i]
		if !ep.Enabled {
			continue
		}
		targetType, ok := mapEndpointTransformerToTargetType(ep.Transformer)
		if !ok {
			// Unsupported transformer for Augment integration.
			return "", nil
		}
		return targetType, &ep
	}

	return "", nil
}

func (s *Server) getCurrentEndpoint(endpoints []config.Endpoint) *config.Endpoint {
	if s.proxy == nil {
		return nil
	}

	currentName := strings.TrimSpace(s.proxy.GetCurrentEndpointName())
	if currentName == "" {
		return nil
	}

	for i := range endpoints {
		ep := endpoints[i]
		if ep.Enabled && ep.Name == currentName {
			return &ep
		}
	}

	return nil
}

func mapEndpointTransformerToTargetType(transformerName string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(transformerName)) {
	case "", "claude":
		return "claude", true
	case "cli", "cc_cli":
		return "cli", true
	case "openai":
		return "openai", true
	case "openai2":
		return "openai2", true
	case "gemini":
		// Gemini endpoints use OpenAI-compatible format for Augment integration.
		// The Augment request is converted to OpenAI Chat format, which most
		// Gemini-compatible providers accept at /v1/chat/completions.
		return "openai", true
	default:
		return "", false
	}
}

// proxyToUpstream proxies the transformed request to the upstream API.
func (s *Server) proxyToUpstream(
	w http.ResponseWriter,
	r *http.Request,
	transformedRequest []byte,
	endpoint *config.Endpoint,
	targetType string,
	isStreaming bool,
	toolContext map[string]*augment.ToolContext,
	originalRequest []byte,
	transformerName string,
	startTime time.Time,
	clientFormat string,
) {
	// Build upstream URL.
	path := augment.TargetPath(targetType)
	upstreamURL := strings.TrimSuffix(endpoint.APIUrl, "/") + path

	// Build fallback payloads up front (independent from original, per BYOK)
	fallbackPayloads := augment.BuildRequestFallbackPayloads(targetType, transformedRequest)
	currentRequestBody := transformedRequest
	currentFallbackName := "original"
	fallbackIndex := -1

	// Log request details for debugging
	logger.Debug("Augment: sending request to %s (%.1f KB, fallbacks=%d)", upstreamURL, float64(len(currentRequestBody))/1024, len(fallbackPayloads))

	maxRetries := 2
	var lastErr error
	var resp *http.Response

	for {
		lastErr = nil
		resp = nil

		for attempt := 0; attempt <= maxRetries; attempt++ {
			var attemptReq *http.Request
			var reqErr error
			if attempt == 0 {
				attemptReq, reqErr = s.createUpstreamRequest(r.Context(), http.MethodPost, upstreamURL, currentRequestBody, targetType, endpoint)
			} else {
				logger.Warn("Augment: retrying request to %s (attempt %d/%d)", upstreamURL, attempt, maxRetries)
				time.Sleep(time.Duration(attempt) * time.Second)
				retryCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				attemptReq, reqErr = s.createUpstreamRequest(retryCtx, http.MethodPost, upstreamURL, currentRequestBody, targetType, endpoint)
				if reqErr != nil {
					cancel()
					lastErr = reqErr
					logger.Error("Augment: failed to create upstream request: %v", reqErr)
					break
				}
				requestStart := time.Now()
				resp, lastErr = s.httpClient.Do(attemptReq)
				cancel()
				requestDuration := time.Since(requestStart)

				if lastErr == nil {
					logger.Debug("Augment: request succeeded in %v (fallback=%s)", requestDuration, currentFallbackName)
					break
				}

				if !isRetryableError(lastErr) {
					logger.Error("Augment: non-retryable error to %s: %v", upstreamURL, lastErr)
					break
				}
				logger.Warn("Augment: retryable error to %s (attempt %d/%d): %v", upstreamURL, attempt, maxRetries, lastErr)
				continue
			}
			if reqErr != nil {
				lastErr = reqErr
				logger.Error("Augment: failed to create upstream request: %v", reqErr)
				break
			}

			requestStart := time.Now()
			resp, lastErr = s.httpClient.Do(attemptReq)
			requestDuration := time.Since(requestStart)

			if lastErr == nil {
				logger.Debug("Augment: request succeeded in %v (fallback=%s)", requestDuration, currentFallbackName)
				break
			}

			if !isRetryableError(lastErr) {
				logger.Error("Augment: non-retryable error to %s: %v", upstreamURL, lastErr)
				break
			}
			logger.Warn("Augment: retryable error to %s (attempt %d/%d): %v", upstreamURL, attempt, maxRetries, lastErr)
		}

		if lastErr != nil {
			break
		}

		if !shouldRetryWithFallback(targetType, resp) || fallbackIndex+1 >= len(fallbackPayloads) {
			break
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			break
		}

		nextFallbackIndex, nextFallback := selectFallbackPayload(targetType, body, fallbackPayloads, fallbackIndex)
		if nextFallback == nil {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			break
		}

		fallbackIndex = nextFallbackIndex
		currentRequestBody = nextFallback.Body
		currentFallbackName = nextFallback.Name
		logger.Warn("Augment: retrying with request fallback=%s target=%s status=%d", currentFallbackName, targetType, resp.StatusCode)
	}

	if lastErr != nil {
		logger.Error("Augment: upstream request failed after %d attempts: %v", maxRetries+1, lastErr)
		s.writeErrorResponse(w, "Upstream request failed", isStreaming)
		if s.trafficRecorder != nil {
			statusCode := http.StatusInternalServerError
			if isStreaming {
				statusCode = http.StatusOK
			}
			s.trafficRecorder.Record(&proxy.TrafficLog{
				Timestamp:          startTime,
				EndpointName:       endpoint.Name,
				ClientFormat:       clientFormat,
				TransformerName:    transformerName,
				Method:             r.Method,
				Path:               r.URL.Path,
				StatusCode:         statusCode,
				Duration:           time.Since(startTime),
				IsStreaming:        isStreaming,
				Error:              lastErr.Error(),
				OriginalRequest:    originalRequest,
				TransformedRequest: transformedRequest,
			})
		}
		return
	}
	defer resp.Body.Close()

	// Log successful response details
	logger.Debug("Augment: received response from %s: status=%d", upstreamURL, resp.StatusCode)

	recordEnabled := s.trafficRecorder != nil && s.trafficRecorder.IsRecording()
	// Handle response.
	if isStreaming {
		inputTokens, outputTokens, ndjson, convErr := s.handleStreamingResponse(w, resp, targetType, toolContext, recordEnabled)
		if s.stats != nil && convErr == nil {
			s.stats.RecordRequest(endpoint.Name)
			s.stats.RecordTokens(endpoint.Name, inputTokens, outputTokens)
		} else if s.stats != nil && convErr != nil {
			s.stats.RecordError(endpoint.Name)
		}
		if recordEnabled {
			log := &proxy.TrafficLog{
				Timestamp:           startTime,
				EndpointName:        endpoint.Name,
				ClientFormat:        clientFormat,
				TransformerName:     transformerName,
				Method:              r.Method,
				Path:                r.URL.Path,
				StatusCode:          resp.StatusCode,
				Duration:            time.Since(startTime),
				InputTokens:         inputTokens,
				OutputTokens:        outputTokens,
				IsStreaming:         true,
				OriginalRequest:     originalRequest,
				TransformedRequest:  transformedRequest,
				OriginalResponse:    ndjson,
				TransformedResponse: ndjson,
			}
			// Only set error if the HTTP request actually failed
			if resp.StatusCode >= 400 {
				log.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
			} else if convErr != nil {
				// Log conversion errors but don't mark the request as failed
				logger.Warn("Augment: SSE conversion warning: %v", convErr)
			}
			s.trafficRecorder.Record(log)
		} else if convErr != nil {
			logger.Error("Augment: SSE conversion error: %v", convErr)
		}
	} else {
		inputTokens, outputTokens, originalResp, transformedResp, convErr := s.handleNonStreamingResponse(w, resp, targetType, toolContext)
		if s.stats != nil && convErr == nil {
			s.stats.RecordRequest(endpoint.Name)
			s.stats.RecordTokens(endpoint.Name, inputTokens, outputTokens)
		} else if s.stats != nil && convErr != nil {
			s.stats.RecordError(endpoint.Name)
		}
		if recordEnabled {
			log := &proxy.TrafficLog{
				Timestamp:           startTime,
				EndpointName:        endpoint.Name,
				ClientFormat:        clientFormat,
				TransformerName:     transformerName,
				Method:              r.Method,
				Path:                r.URL.Path,
				StatusCode:          resp.StatusCode,
				Duration:            time.Since(startTime),
				InputTokens:         inputTokens,
				OutputTokens:        outputTokens,
				IsStreaming:         false,
				OriginalRequest:     originalRequest,
				TransformedRequest:  transformedRequest,
				OriginalResponse:    originalResp,
				TransformedResponse: transformedResp,
			}
			// Only set error if the HTTP request actually failed
			if resp.StatusCode >= 400 {
				log.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
			} else if convErr != nil {
				// Log conversion errors but don't mark the request as failed
				logger.Warn("Augment: response conversion warning: %v", convErr)
			}
			s.trafficRecorder.Record(log)
		} else if convErr != nil {
			logger.Error("Augment: response conversion error: %v", convErr)
		}
	}
}

// handleStreamingResponse handles SSE streaming responses and converts to NDJSON.
func (s *Server) handleStreamingResponse(
	w http.ResponseWriter,
	resp *http.Response,
	targetType string,
	toolContext map[string]*augment.ToolContext,
	captureNDJSON bool,
) (inputTokens, outputTokens int, outBytes []byte, err error) {
	if responseLooksLikeJSON(resp.Header.Get("Content-Type")) {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return 0, 0, nil, readErr
		}
		if resp.StatusCode >= 400 {
			ndjson := buildStreamingErrorNDJSON(extractJSONErrorMessage(body))
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(ndjson)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			return 0, 0, ndjson, nil
		}

		inputTokens, outputTokens, ndjson, convErr := augment.ConvertJSONToNDJSON(body, targetType, toolContext)
		if convErr != nil {
			return 0, 0, nil, convErr
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(ndjson)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return inputTokens, outputTokens, ndjson, nil
	}
	if !responseLooksLikeSSE(resp.Header.Get("Content-Type")) {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return 0, 0, nil, readErr
		}
		message := buildUnexpectedStreamingContentTypeMessage(resp.Header.Get("Content-Type"), body)
		ndjson := buildStreamingErrorNDJSON(message)
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(ndjson)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return 0, 0, ndjson, nil
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	flusher, ok := w.(http.Flusher)
	if !ok {
		logger.Error("Augment: response writer does not support flushing")
		return 0, 0, nil, fmt.Errorf("augment: response writer does not support flushing")
	}

	var capture *limitedBuffer
	writer := io.Writer(w)
	if captureNDJSON {
		capture = newLimitedBuffer(proxy.MaxBodySize + 1)
		writer = &teeCapture{w: w, cap: capture}
	}

	// Convert SSE to NDJSON on the fly.
	inputTokens, outputTokens, err = augment.StreamConvertSSEToNDJSON(resp.Body, writer, targetType, toolContext)
	flusher.Flush()
	if capture != nil {
		outBytes = capture.Bytes()
	}
	return inputTokens, outputTokens, outBytes, err
}

// handleNonStreamingResponse converts a non-streaming upstream response to Augment NDJSON format.
// Augment clients expect NDJSON even for non-streaming responses.
func (s *Server) handleNonStreamingResponse(w http.ResponseWriter, resp *http.Response, targetType string, toolContext map[string]*augment.ToolContext) (inputTokens, outputTokens int, originalResp []byte, transformedResp []byte, err error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("Augment: failed to read upstream response: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		return 0, 0, nil, nil, err
	}
	originalResp = body

	// Extract token usage from the response.
	inputTokens, outputTokens = extractTokenUsageFromResponse(body)

	// If upstream returned an error status, pass through as JSON.
	if resp.StatusCode >= 400 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return inputTokens, outputTokens, originalResp, originalResp, nil
	}

	inputTokens, outputTokens, transformedResp, err = augment.ConvertJSONToNDJSON(body, targetType, toolContext)
	if err != nil {
		return inputTokens, outputTokens, originalResp, nil, err
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	w.Write(transformedResp)
	return inputTokens, outputTokens, originalResp, transformedResp, nil
}

func responseLooksLikeJSON(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct == "" {
		return false
	}
	if strings.Contains(ct, "text/event-stream") {
		return false
	}
	return strings.Contains(ct, "json")
}

func responseLooksLikeSSE(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	return strings.Contains(ct, "text/event-stream")
}

func buildUnexpectedStreamingContentTypeMessage(contentType string, body []byte) string {
	ct := strings.TrimSpace(contentType)
	if ct == "" {
		ct = "unknown"
	}
	detail := strings.TrimSpace(string(body))
	if len(detail) > 500 {
		detail = detail[:500]
	}
	if detail == "" {
		return fmt.Sprintf("Upstream streaming response is not SSE (content-type=%s)", ct)
	}
	return fmt.Sprintf("Upstream streaming response is not SSE (content-type=%s): %s", ct, detail)
}

func extractJSONErrorMessage(body []byte) string {
	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		return "Upstream request failed"
	}
	if errObj, ok := obj["error"].(map[string]interface{}); ok {
		if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
			return msg
		}
	}
	if responseObj, ok := obj["response"].(map[string]interface{}); ok {
		if errObj, ok := responseObj["error"].(map[string]interface{}); ok {
			if msg, ok := errObj["message"].(string); ok && strings.TrimSpace(msg) != "" {
				return msg
			}
		}
	}
	if msg, ok := obj["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return msg
	}
	return "Upstream request failed"
}

func buildStreamingErrorNDJSON(message string) []byte {
	line, _ := json.Marshal(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"message": message,
		},
	})
	return append(line, '\n')
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractTextFromResponse extracts text content from Claude or OpenAI JSON responses.
func extractTextFromResponse(body []byte) string {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}

	// Claude format: content[].text
	if content, ok := resp["content"].([]interface{}); ok {
		var parts []string
		for _, block := range content {
			if b, ok := block.(map[string]interface{}); ok {
				if text, ok := b["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "")
		}
	}

	// OpenAI format: choices[].message.content
	if choices, ok := resp["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				if text, ok := msg["content"].(string); ok {
					return text
				}
			}
		}
	}

	// OpenAI Responses format: output_text or output[].message/content
	if text, ok := resp["output_text"].(string); ok && text != "" {
		return text
	}
	if output, ok := resp["output"].([]interface{}); ok {
		var parts []string
		for _, item := range output {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if itemType, _ := entry["type"].(string); itemType == "message" {
				if content, ok := entry["content"].([]interface{}); ok {
					for _, raw := range content {
						block, ok := raw.(map[string]interface{})
						if !ok {
							continue
						}
						if text, ok := block["text"].(string); ok && text != "" {
							parts = append(parts, text)
						}
					}
				}
			}
			if text, ok := entry["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "")
		}
	}

	return ""
}

// extractTokenUsageFromResponse extracts token usage from Claude or OpenAI JSON responses.
func extractTokenUsageFromResponse(body []byte) (inputTokens, outputTokens int) {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, 0
	}

	// Try Claude format: usage.input_tokens/output_tokens
	if usage, ok := resp["usage"].(map[string]interface{}); ok {
		inputTokens = parseIntFromMap(usage, "input_tokens")
		outputTokens = parseIntFromMap(usage, "output_tokens")
		if inputTokens > 0 || outputTokens > 0 {
			return inputTokens, outputTokens
		}
	}

	// Try OpenAI format: usage.prompt_tokens/completion_tokens
	if usage, ok := resp["usage"].(map[string]interface{}); ok {
		promptTokens := parseIntFromMap(usage, "prompt_tokens")
		completionTokens := parseIntFromMap(usage, "completion_tokens")
		if promptTokens > 0 || completionTokens > 0 {
			return promptTokens, completionTokens
		}
	}

	// Try OpenAI Responses format nested in response
	if responseObj, ok := resp["response"].(map[string]interface{}); ok {
		if usage, ok := responseObj["usage"].(map[string]interface{}); ok {
			inputTokens = parseIntFromMap(usage, "input_tokens")
			outputTokens = parseIntFromMap(usage, "output_tokens")
			if inputTokens > 0 || outputTokens > 0 {
				return inputTokens, outputTokens
			}
		}
	}

	return 0, 0
}

// parseIntFromMap parses an int from a map under multiple possible keys.
func parseIntFromMap(m map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if val, ok := m[key]; ok {
			switch v := val.(type) {
			case int:
				return v
			case float64:
				return int(v)
			case json.Number:
				if i, err := v.Int64(); err == nil {
					return int(i)
				}
			}
		}
	}
	return 0
}

// writeErrorResponse writes an error response in Augment format.
func (s *Server) writeErrorResponse(w http.ResponseWriter, message string, isStreaming bool) {
	if isStreaming {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		errorLine, _ := json.Marshal(map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"message": message,
			},
		})
		w.Write(errorLine)
		w.Write([]byte("\n"))
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": message,
			},
		})
	}
}

// ModelInfo represents basic model information from various API providers.
type ModelInfo struct {
	ID          string
	DisplayName string
	CreatedAt   time.Time
}

// fetchClaudeModels fetches model list from Claude API.
func (s *Server) fetchClaudeModels(endpoint *config.Endpoint) ([]ModelInfo, error) {
	url := strings.TrimSuffix(endpoint.APIUrl, "/") + "/v1/models"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-api-key", endpoint.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID          string    `json:"id"`
			DisplayName string    `json:"display_name"`
			CreatedAt   time.Time `json:"created_at"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]ModelInfo, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, ModelInfo{
			ID:          m.ID,
			DisplayName: m.DisplayName,
			CreatedAt:   m.CreatedAt,
		})
	}

	return models, nil
}

// fetchOpenAIModels fetches model list from OpenAI-compatible API.
func (s *Server) fetchOpenAIModels(endpoint *config.Endpoint) ([]ModelInfo, error) {
	url := strings.TrimSuffix(endpoint.APIUrl, "/") + "/v1/models"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+endpoint.APIKey)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]ModelInfo, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, ModelInfo{
			ID:          m.ID,
			DisplayName: m.ID, // OpenAI doesn't provide display_name
			CreatedAt:   time.Unix(m.Created, 0),
		})
	}

	return models, nil
}

// fetchGeminiModels fetches model list from Gemini API.
func (s *Server) fetchGeminiModels(endpoint *config.Endpoint) ([]ModelInfo, error) {
	url := "https://generativelanguage.googleapis.com/v1beta/models?key=" + endpoint.APIKey

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]ModelInfo, 0, len(result.Models))
	for _, m := range result.Models {
		// Extract model ID from "models/gemini-pro" format
		modelID := strings.TrimPrefix(m.Name, "models/")

		models = append(models, ModelInfo{
			ID:          modelID,
			DisplayName: m.DisplayName,
			CreatedAt:   time.Now(), // Gemini doesn't provide creation time
		})
	}

	return models, nil
}

// convertToAugmentFormat converts model list to Augment-compatible format.
func (s *Server) convertToAugmentFormat(models []ModelInfo, targetType string) map[string]interface{} {
	result := make(map[string]interface{})

	for i, model := range models {
		// Generate short name from model ID
		shortName := model.ID
		if strings.Contains(model.ID, "-") {
			parts := strings.Split(model.ID, "-")
			if len(parts) > 0 {
				shortName = parts[0]
			}
		}

		// Determine priority (lower number = higher priority)
		priority := i + 1

		// Create model entry in Augment-compatible format
		modelEntry := map[string]interface{}{
			"displayName":   model.ID,
			"description":   model.DisplayName,
			"shortName":     shortName,
			"priority":      priority,
			"isLegacyModel": false,
		}

		result[model.ID] = modelEntry
	}

	return result
}

// fetchModelsFromEndpoint fetches models from the endpoint based on its type.
func (s *Server) fetchModelsFromEndpoint(endpoint *config.Endpoint, targetType string) (map[string]interface{}, error) {
	var models []ModelInfo
	var err error

	switch targetType {
	case "claude", "cli":
		models, err = s.fetchClaudeModels(endpoint)
	case "openai", "openai2":
		models, err = s.fetchOpenAIModels(endpoint)
	case "gemini":
		// Try Gemini API first, fallback to OpenAI-compatible
		models, err = s.fetchGeminiModels(endpoint)
		if err != nil {
			logger.Debug("Augment: Gemini API failed, trying OpenAI-compatible: %v", err)
			models, err = s.fetchOpenAIModels(endpoint)
		}
	default:
		return nil, fmt.Errorf("unsupported target type: %s", targetType)
	}

	if err != nil {
		return nil, err
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("no models returned from endpoint")
	}

	return s.convertToAugmentFormat(models, targetType), nil
}

// getDefaultModels returns the default model list as fallback.
func (s *Server) getDefaultModels() map[string]interface{} {
	return map[string]interface{}{
		"claude-sonnet-4-5-20250929": map[string]interface{}{
			"displayName":   "claude-sonnet-4-5-20250929",
			"description":   "Claude Sonnet 4.5",
			"shortName":     "sonnet-4.5",
			"priority":      10,
			"isLegacyModel": false,
		},
		"claude-opus-4-20250514": map[string]interface{}{
			"displayName":   "claude-opus-4-20250514",
			"description":   "Claude Opus 4",
			"shortName":     "opus-4",
			"priority":      9,
			"isLegacyModel": false,
		},
		"claude-3-5-sonnet-20241022": map[string]interface{}{
			"displayName":   "claude-3-5-sonnet-20241022",
			"description":   "Claude 3.5 Sonnet",
			"shortName":     "3.5-sonnet",
			"priority":      8,
			"isLegacyModel": false,
		},
	}
}

// handleGetModels handles model list requests for Augment plugin.
func (s *Server) handleGetModels(w http.ResponseWriter, r *http.Request) {
	logger.Debug("Augment: GET /usage/api/get-models from %s", r.RemoteAddr)

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Try to fetch models dynamically from endpoint
	targetType, endpoint := s.selectTarget()
	var models map[string]interface{}

	if endpoint != nil {
		logger.Debug("Augment: attempting to fetch models from endpoint (type: %s)", targetType)
		fetchedModels, err := s.fetchModelsFromEndpoint(endpoint, targetType)
		if err != nil {
			logger.Debug("Augment: failed to fetch models from endpoint: %v, using defaults", err)
			models = s.getDefaultModels()
		} else {
			logger.Debug("Augment: successfully fetched %d models from endpoint", len(fetchedModels))
			models = fetchedModels
		}
	} else {
		logger.Debug("Augment: no endpoint configured, using default models")
		models = s.getDefaultModels()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models)
	logger.Debug("Augment: returned model list with %d models", len(models))
}

// handleGetBalance handles balance query requests for Augment plugin.
func (s *Server) handleGetBalance(w http.ResponseWriter, r *http.Request) {
	logger.Debug("Augment: GET /usage/api/balance from %s", r.RemoteAddr)

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Return balance info in Augment-compatible format
	balance := map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"name":          "ccNexus Proxy",
			"remain_quota":  999999,
			"remain_amount": 999999,
			"unlimited":     false,
			"expired_time":  4102444800,
			"status":        1,
			"status_text":   "enabled",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(balance)
	logger.Debug("Augment: returned balance info")
}

// handleGetLoginToken handles login token requests for Augment plugin.
func (s *Server) handleGetLoginToken(w http.ResponseWriter, r *http.Request) {
	logger.Debug("Augment: GET /usage/api/getLoginToken from %s", r.RemoteAddr)

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get first enabled endpoint for tenantUrl and accessToken
	var tenantUrl, accessToken string
	if _, endpoint := s.selectTarget(); endpoint != nil {
		tenantUrl = strings.TrimSuffix(endpoint.APIUrl, "/")
		accessToken = endpoint.APIKey
	}

	// Fallback to default values if no endpoint found
	if tenantUrl == "" {
		tenantUrl = "https://api.anthropic.com"
	}
	if accessToken == "" {
		accessToken = "augment-proxy-token"
	}

	// Return login token in Augment-compatible format
	// Note: data is duplicated at both top level and nested for compatibility
	token := map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"tenantUrl":   tenantUrl,
			"accessToken": accessToken,
		},
		"tenantUrl":   tenantUrl,
		"accessToken": accessToken,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(token)
	logger.Debug("Augment: returned login token")
}

// isRetryableError checks if an error should trigger a retry
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Network-level errors that are typically temporary
	if strings.Contains(errStr, "EOF") {
		return true // Connection closed by remote server
	}
	if strings.Contains(errStr, "connection reset") {
		return true // Connection reset
	}
	if strings.Contains(errStr, "broken pipe") {
		return true // Broken pipe
	}
	if strings.Contains(errStr, "timeout") {
		return true // Timeout
	}
	if strings.Contains(errStr, "temporary") {
		return true // Temporary failure
	}

	// Check for specific error types
	if netErr, ok := err.(net.Error); ok {
		return netErr.Temporary() || netErr.Timeout()
	}

	return false
}

// createUpstreamRequest creates an HTTP request with proper headers for the target type
func (s *Server) createUpstreamRequest(ctx context.Context, method, url string, body []byte, targetType string, endpoint *config.Endpoint) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// Set headers based on target type
	if targetType == "cli" {
		// CLI mode: use full CLI headers (includes auth, beta, user-agent, stainless-*)
		var tools []map[string]interface{}
		var reqBody struct {
			Stream bool                     `json:"stream"`
			Tools  []map[string]interface{} `json:"tools"`
		}
		if err := json.Unmarshal(body, &reqBody); err == nil {
			tools = reqBody.Tools
		}
		betas := convert.BuildClaudeCliBetas(tools)
		cliHeaders := convert.BuildClaudeCliHeaders(endpoint.APIKey, betas, reqBody.Stream)
		for k, v := range cliHeaders {
			req.Header.Set(k, v)
		}
	} else {
		req.Header.Set("Content-Type", "application/json")
		authHeaders := augment.BuildAuthHeaders(targetType, endpoint.APIKey)
		for k, v := range authHeaders {
			req.Header.Set(k, v)
		}
		if targetType == "claude" && claudeThinkingEnabled(body) {
			req.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
		}
	}

	return req, nil
}

func claudeThinkingEnabled(body []byte) bool {
	var req struct {
		Thinking map[string]interface{} `json:"thinking"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	if len(req.Thinking) == 0 {
		return false
	}
	thinkingType, _ := req.Thinking["type"].(string)
	switch strings.ToLower(strings.TrimSpace(thinkingType)) {
	case "enabled", "adaptive":
		return true
	default:
		return false
	}
}

func shouldRetryWithFallback(targetType string, resp *http.Response) bool {
	if resp == nil {
		return false
	}
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnprocessableEntity {
		return false
	}
	switch targetType {
	case "openai", "openai2", "claude", "cli":
		return true
	default:
		return false
	}
}

func selectFallbackPayload(targetType string, body []byte, payloads []augment.RequestFallbackPayload, lastFallbackIndex int) (int, *augment.RequestFallbackPayload) {
	if len(body) == 0 || len(payloads) == 0 || lastFallbackIndex >= len(payloads)-1 {
		return -1, nil
	}

	message := fallbackMessageFromBody(body)
	orderedNames := fallbackNamesForError(targetType, message)

	// If we have matching error-driven fallback names, select the first matching one
	if len(orderedNames) > 0 {
		for _, name := range orderedNames {
			for idx := lastFallbackIndex + 1; idx < len(payloads); idx++ {
				if payloads[idx].Name == name {
					payload := payloads[idx]
					return idx, &payload
				}
			}
		}
	}

	// BYOK-style exhaustive fallback: if no specific match, try next in sequence
	nextIndex := lastFallbackIndex + 1
	if nextIndex < len(payloads) {
		payload := payloads[nextIndex]
		return nextIndex, &payload
	}

	return -1, nil
}

func fallbackNamesForError(targetType string, message string) []string {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return nil
	}

	switch targetType {
	case "openai", "openai2":
		return openAIFallbackNamesForError(message)
	case "claude", "cli":
		return claudeFallbackNamesForError(message)
	default:
		return nil
	}
}

func fallbackMessageFromBody(body []byte) string {
	message := strings.ToLower(extractJSONErrorMessage(body))
	if strings.TrimSpace(message) != "" && message != strings.ToLower("Upstream request failed") {
		return message
	}
	return strings.ToLower(string(body))
}

func openAIFallbackNamesForError(message string) []string {
	hasIncludeUsage := containsAny(message, "include_usage", "stream_options", "stream options")
	hasToolChoice := containsAny(message, "tool_choice", "tool choice")
	hasParallelToolCalls := containsAny(message, "parallel_tool_calls", "parallel tool calls")
	hasFunctions := containsAny(message, "functions", "function calling")
	hasToolsParam := containsAny(message, "unsupported parameter: tools", "unknown parameter: tools", "parameter \"tools\"", "field required: tools")
	hasToolCalls := containsAny(message, "tool calls") || (strings.Contains(message, "tool_calls") && !hasParallelToolCalls)
	hasUnsupported := containsAny(message, "unsupported parameter", "unknown parameter", "unrecognized request argument", "extra inputs are not permitted")
	hasInvalidValue := containsAny(message, "invalid_value", "invalid value", "is not of type", "invalid type")
	hasVision := containsAny(message, "image", "vision", "multimodal", "content type")

	var names []string
	if hasIncludeUsage {
		names = append(names, "drop_stream_include_usage")
	}
	if hasToolChoice {
		names = append(names, "drop_tool_choice")
	}
	if hasParallelToolCalls {
		names = append(names, "drop_parallel_tool_calls")
	}
	if hasFunctions || hasToolsParam || hasToolCalls {
		names = append(names, "convert_tools_to_functions", "drop_tools")
	} else if (hasUnsupported || hasInvalidValue) && !hasToolChoice && !hasParallelToolCalls && containsAny(message, "tools") {
		names = append(names, "drop_tools")
	}
	if hasVision {
		names = append(names, "strip_vision", "strip_vision_drop_tools")
	}
	if (hasUnsupported || hasInvalidValue) && !hasToolChoice && !hasParallelToolCalls && !hasFunctions && !hasToolsParam && !hasToolCalls && containsAny(message, "tool") {
		names = append(names, "drop_tool_choice", "drop_tools")
	}
	return dedupeFallbackNames(names)
}

func claudeFallbackNamesForError(message string) []string {
	var names []string
	invalidType := containsAny(message, "invalid type", "expected type", "must be an array", "must be an object")
	if containsAny(message, "tool_choice", "tool choice") {
		names = append(names, "drop_tool_choice")
	}
	if containsAny(message, "system") && invalidType {
		names = append(names, "normalize_system_blocks")
	}
	if containsAny(message, "messages", "content") && invalidType {
		names = append(names, "normalize_message_blocks", "normalize_all_blocks")
	}
	if containsAny(message, "tool_result", "tool_use") && (invalidType || containsAny(message, "missing", "orphan", "pair")) {
		names = append(names, "repair_tool_pairs")
	}
	if containsAny(message, "messages[0]", "first message") && containsAny(message, "user", "role") {
		names = append(names, "ensure_first_user")
	}
	if containsAny(message, "tools") || (containsAny(message, "tool_use", "tool use") && !containsAny(message, "tool_choice", "tool choice")) {
		names = append(names, "drop_tools")
	}
	return dedupeFallbackNames(names)
}

func containsAny(message string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(message, needle) {
			return true
		}
	}
	return false
}

func dedupeFallbackNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
