package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/lich0821/ccNexus/internal/proxy"
)

func (h *Handler) handleTraffic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	filter := &proxy.TrafficFilter{}
	query := r.URL.Query()
	filter.EndpointName = strings.TrimSpace(query.Get("endpointName"))
	filter.ClientFormat = strings.TrimSpace(query.Get("clientFormat"))

	if statusCode := strings.TrimSpace(query.Get("statusCode")); statusCode != "" {
		parsed, err := strconv.Atoi(statusCode)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid statusCode")
			return
		}
		filter.StatusCode = parsed
	}

	if hasError := strings.TrimSpace(query.Get("hasError")); hasError != "" {
		parsed, err := strconv.ParseBool(hasError)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid hasError")
			return
		}
		filter.HasError = &parsed
	}

	if limit := strings.TrimSpace(query.Get("limit")); limit != "" {
		parsed, err := strconv.Atoi(limit)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid limit")
			return
		}
		filter.Limit = parsed
	}

	logs := h.proxy.GetTrafficRecorder().GetLogs(filter)
	WriteSuccess(w, map[string]interface{}{
		"logs":      logs,
		"count":     len(logs),
		"total":     h.proxy.GetTrafficRecorder().GetCount(),
		"recording": h.proxy.GetTrafficRecorder().IsRecording(),
	})
}

func (h *Handler) handleTrafficByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/traffic/"))
	if id == "" || id == "recording" || id == "clear" {
		http.NotFound(w, r)
		return
	}

	detail := h.proxy.GetTrafficRecorder().GetLogByID(id)
	if detail == nil {
		WriteError(w, http.StatusNotFound, "Traffic log not found")
		return
	}

	WriteSuccess(w, detail)
}

func (h *Handler) handleTrafficRecording(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		WriteSuccess(w, map[string]interface{}{
			"enabled": h.proxy.GetTrafficRecorder().IsRecording(),
		})
	case http.MethodPut:
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		h.proxy.GetTrafficRecorder().SetRecording(req.Enabled)
		WriteSuccess(w, map[string]interface{}{
			"enabled": req.Enabled,
		})
	default:
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *Handler) handleTrafficClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	h.proxy.GetTrafficRecorder().Clear()
	WriteSuccess(w, map[string]interface{}{
		"cleared": true,
	})
}
