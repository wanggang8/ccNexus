package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/proxy"
)

// handleTrafficLogs handles GET requests for traffic logs
func (h *Handler) handleTrafficLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Check if requesting a specific log by ID
	if id := r.URL.Query().Get("id"); id != "" {
		h.handleTrafficLogDetail(w, r, id)
		return
	}

	// Parse filter parameters
	filter := &proxy.TrafficFilter{}
	if endpointName := r.URL.Query().Get("endpoint"); endpointName != "" {
		filter.EndpointName = endpointName
	}
	if clientFormat := r.URL.Query().Get("format"); clientFormat != "" {
		filter.ClientFormat = clientFormat
	}
	if statusCode := r.URL.Query().Get("status"); statusCode != "" {
		var code int
		if _, err := fmt.Sscanf(statusCode, "%d", &code); err == nil {
			filter.StatusCode = code
		}
	}
	if hasError := r.URL.Query().Get("error"); hasError != "" {
		hasErr := hasError == "true" || hasError == "1"
		filter.HasError = &hasErr
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		var l int
		if _, err := fmt.Sscanf(limit, "%d", &l); err == nil && l > 0 {
			filter.Limit = l
		}
	}

	logs := h.proxy.GetTrafficRecorder().GetLogs(filter)
	recording := h.proxy.GetTrafficRecorder().IsRecording()
	total := h.proxy.GetTrafficRecorder().GetCount()

	WriteSuccess(w, map[string]interface{}{
		"logs":      logs,
		"count":     len(logs),
		"total":     total,
		"recording": recording,
	})
}

// handleTrafficLogDetail handles GET requests for a specific traffic log detail
func (h *Handler) handleTrafficLogDetail(w http.ResponseWriter, r *http.Request, id string) {
	detail := h.proxy.GetTrafficRecorder().GetLogByID(id)
	if detail == nil {
		WriteError(w, http.StatusNotFound, "Traffic log not found")
		return
	}

	WriteSuccess(w, detail)
}

// handleTrafficRecording handles GET/POST requests for recording status
func (h *Handler) handleTrafficRecording(w http.ResponseWriter, r *http.Request) {
	recorder := h.proxy.GetTrafficRecorder()

	switch r.Method {
	case http.MethodGet:
		// Return current recording status
		WriteSuccess(w, map[string]interface{}{
			"recording": recorder.IsRecording(),
		})

	case http.MethodPost:
		// Update recording status
		var req struct {
			Recording bool `json:"recording"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		recorder.SetRecording(req.Recording)

		status := "disabled"
		if req.Recording {
			status = "enabled"
		}

		logger.Info("Traffic recording %s", status)

		WriteSuccess(w, map[string]interface{}{
			"recording": req.Recording,
			"message":   "Traffic recording " + status,
		})

	default:
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleTrafficClear handles POST/DELETE requests to clear all traffic logs
func (h *Handler) handleTrafficClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	h.proxy.GetTrafficRecorder().Clear()

	logger.Info("Traffic logs cleared")

	WriteSuccess(w, map[string]interface{}{
		"message": "Traffic logs cleared",
	})
}
