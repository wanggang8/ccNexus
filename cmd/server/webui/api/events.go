package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/lich0821/ccNexus/internal/logger"
)

// handleEvents handles Server-Sent Events for real-time updates
func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create a flusher
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Send initial connection message
	fmt.Fprintf(w, "data: {\"type\":\"connected\",\"message\":\"Connected to ccNexus events\"}\n\n")
	flusher.Flush()

	// Create ticker for periodic updates
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Listen for client disconnect
	ctx := r.Context()

	logger.Debug("[SSE] Client connected")

	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			logger.Debug("[SSE] Client disconnected")
			return
		case <-ticker.C:
			user := h.currentUser(r)
			if user == nil {
				return
			}
			statsSummary := map[string]interface{}{
				"TotalRequests":     0,
				"TotalErrors":       0,
				"TotalInputTokens":  int64(0),
				"TotalOutputTokens": int64(0),
				"Endpoints":         map[string]interface{}{},
			}
			_, endpointStats, statsErr := h.storage.GetTotalStatsForUser(user.ID)
			if statsErr != nil {
				logger.Error("[SSE] Failed to get scoped stats: %v", statsErr)
			} else {
				endpointsPayload := make(map[string]interface{}, len(endpointStats))
				totalRequests := 0
				totalErrors := 0
				var totalInputTokens int64
				var totalOutputTokens int64
				for name, stat := range endpointStats {
					endpointsPayload[name] = stat
					totalRequests += stat.Requests
					totalErrors += stat.Errors
					totalInputTokens += stat.InputTokens
					totalOutputTokens += stat.OutputTokens
				}
				statsSummary = map[string]interface{}{
					"TotalRequests":     totalRequests,
					"TotalErrors":       totalErrors,
					"TotalInputTokens":  totalInputTokens,
					"TotalOutputTokens": totalOutputTokens,
					"Endpoints":         endpointsPayload,
				}
			}
			currentEndpoint := ""
			if h.proxy != nil {
				proxyEndpoint, proxyErr := h.proxy.GetCurrentEndpointNameForUser(user.ID)
				if proxyErr != nil {
					logger.Error("[SSE] Failed to get current endpoint: %v", proxyErr)
				} else {
					currentEndpoint = proxyEndpoint
				}
			}

			event := map[string]interface{}{
				"type":            "stats",
				"timestamp":       time.Now().Unix(),
				"stats":           statsSummary,
				"currentEndpoint": currentEndpoint,
			}

			data, err := json.Marshal(event)
			if err != nil {
				logger.Error("[SSE] Failed to marshal event: %v", err)
				continue
			}

			// Send event
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
		}
	}
}
