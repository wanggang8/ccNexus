// Package server provides a standalone HTTP server for the Augment plugin integration.
// It listens on a dedicated port (default 8888) and handles encrypted/plaintext requests
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
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lich0821/ccNexus/internal/augment/decrypt"
	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/transformer/augment"
)

// Server is the standalone Augment HTTP server.
type Server struct {
	config     *config.Config
	decryptor  *decrypt.Decryptor
	httpServer *http.Server
	httpClient *http.Client
	mu         sync.RWMutex
	running    bool
}

// New creates a new Augment server instance.
func New(cfg *config.Config, privateKeyPath string) (*Server, error) {
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
		config:     cfg,
		decryptor:  dec,
		httpClient: httpClient,
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
	mux.HandleFunc("/health", s.handleHealth)

	addr := fmt.Sprintf(":%d", s.config.AugmentPort)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
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
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("Augment: failed to read request body: %v", err)
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	ar, decrypted, err := s.parseAugmentRequest(body)
	if err != nil {
		logger.Error("Augment: failed to parse request: %v", err)
		s.writeErrorResponse(w, err.Error(), false)
		return
	}

	// Determine target type and endpoint.
	targetType, endpoint := s.selectTarget()
	if endpoint == nil {
		logger.Error("Augment: no available endpoint")
		s.writeErrorResponse(w, "No available endpoint", ar.IsStreaming())
		return
	}

	// Create transformer.
	transformer, err := augment.New(targetType, "")
	if err != nil {
		logger.Error("Augment: failed to create transformer: %v", err)
		s.writeErrorResponse(w, "Internal error", ar.IsStreaming())
		return
	}

	// Transform request.
	transformed, err := transformer.TransformRequest(decrypted)
	if err != nil {
		logger.Error("Augment: failed to transform request: %v", err)
		s.writeErrorResponse(w, "Request transformation failed", ar.IsStreaming())
		return
	}

	// Extract tool context for response transformation.
	toolContext := transformer.GetToolContext()

	// Proxy to upstream.
	s.proxyToUpstream(w, r, transformed, endpoint, targetType, ar.IsStreaming(), toolContext)
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

	var ar augment.AugmentRequest
	if err := json.Unmarshal(input, &ar); err == nil {
		// Some clients send plaintext wrapper: {"data":"..."} which still unmarshals
		// but does not populate AugmentRequest.Message. Detect and reconstruct.
		if strings.TrimSpace(ar.Message) != "" {
			return &ar, input, nil
		}
		var probe struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(input, &probe); err == nil && strings.TrimSpace(probe.Data) == "" {
			return &ar, input, nil
		}
	}

	// Plaintext fallback: some clients send {data:"..."} instead of full AugmentRequest.
	reconstructed, err := decrypt.ReconstructFromPlaintext(body)
	if err != nil {
		return nil, nil, fmt.Errorf("Invalid request format")
	}
	if err := json.Unmarshal(reconstructed, &ar); err != nil {
		return nil, nil, fmt.Errorf("Invalid request format")
	}
	return &ar, reconstructed, nil
}

// selectTarget selects the target type and endpoint based on config.
func (s *Server) selectTarget() (string, *config.Endpoint) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.config.Endpoints) == 0 {
		return "", nil
	}

	// Use first enabled endpoint, mapped strictly by endpoint.Transformer.
	for i := range s.config.Endpoints {
		ep := &s.config.Endpoints[i]
		if !ep.Enabled {
			continue
		}
		targetType, ok := mapEndpointTransformerToTargetType(ep.Transformer)
		if !ok {
			// Unsupported transformer for Augment integration.
			return "", nil
		}
		return targetType, ep
	}

	return "", nil
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
func (s *Server) proxyToUpstream(w http.ResponseWriter, r *http.Request, body []byte, endpoint *config.Endpoint, targetType string, isStreaming bool, toolContext map[string]*augment.ToolContext) {
	// Build upstream URL.
	path := augment.TargetPath(targetType)
	upstreamURL := strings.TrimSuffix(endpoint.APIUrl, "/") + path

	// Create upstream request.
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		logger.Error("Augment: failed to create upstream request: %v", err)
		s.writeErrorResponse(w, "Failed to create upstream request", isStreaming)
		return
	}

	// Set headers.
	req.Header.Set("Content-Type", "application/json")
	authHeaders := augment.BuildAuthHeaders(targetType, endpoint.APIKey)
	for k, v := range authHeaders {
		req.Header.Set(k, v)
	}
	if targetType == "cli" {
		req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	}

	// Execute request.
	resp, err := s.httpClient.Do(req)
	if err != nil {
		logger.Error("Augment: upstream request failed: %v", err)
		s.writeErrorResponse(w, "Upstream request failed", isStreaming)
		return
	}
	defer resp.Body.Close()

	// Handle response.
	if isStreaming {
		s.handleStreamingResponse(w, resp, targetType, toolContext)
	} else {
		s.handleNonStreamingResponse(w, resp)
	}
}

// handleStreamingResponse handles SSE streaming responses and converts to NDJSON.
func (s *Server) handleStreamingResponse(w http.ResponseWriter, resp *http.Response, targetType string, toolContext map[string]*augment.ToolContext) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	flusher, ok := w.(http.Flusher)
	if !ok {
		logger.Error("Augment: response writer does not support flushing")
		return
	}

	// Convert SSE to NDJSON on the fly.
	if err := augment.StreamConvertSSEToNDJSON(resp.Body, w, targetType, toolContext); err != nil {
		logger.Error("Augment: SSE conversion error: %v", err)
	}
	flusher.Flush()
}

// handleNonStreamingResponse converts a non-streaming upstream response to Augment NDJSON format.
// Augment clients expect NDJSON even for non-streaming responses.
func (s *Server) handleNonStreamingResponse(w http.ResponseWriter, resp *http.Response) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("Augment: failed to read upstream response: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// If upstream returned an error status, pass through as JSON.
	if resp.StatusCode >= 400 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	// Convert the JSON response to a single NDJSON chunk with text + stop_reason.
	text := extractTextFromResponse(body)

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)

	if text != "" {
		textChunk := map[string]interface{}{
			"text":                  text,
			"unknown_blob_names":    []interface{}{},
			"checkpoint_not_found":  false,
			"workspace_file_chunks": []interface{}{},
			"nodes":                 []interface{}{},
		}
		line, _ := json.Marshal(textChunk)
		w.Write(line)
		w.Write([]byte("\n"))
	}

	// Final chunk with stop_reason.
	finalChunk := map[string]interface{}{
		"text":                  "",
		"unknown_blob_names":    []interface{}{},
		"checkpoint_not_found":  false,
		"workspace_file_chunks": []interface{}{},
		"nodes":                 []interface{}{},
		"stop_reason":           1, // END_TURN
	}
	line, _ := json.Marshal(finalChunk)
	w.Write(line)
	w.Write([]byte("\n"))
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

	return ""
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
