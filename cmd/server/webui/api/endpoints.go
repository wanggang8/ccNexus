package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/storage"
)

// handleEndpoints handles GET (list) and POST (create) for endpoints
func (h *Handler) handleEndpoints(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listEndpoints(w, r)
	case http.MethodPost:
		h.createEndpoint(w, r)
	default:
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleEndpointByName handles GET, PUT, DELETE, PATCH for specific endpoint
func (h *Handler) handleEndpointByName(w http.ResponseWriter, r *http.Request) {
	// Extract endpoint name from path (name may contain "/", so use full path for CRUD)
	path := strings.TrimPrefix(r.URL.Path, "/api/endpoints/")
	path = strings.TrimSuffix(path, "/")
	parts := strings.SplitN(path, "/", 2) // at most 2: name, optional subpath
	if len(parts) == 0 || parts[0] == "" {
		WriteError(w, http.StatusBadRequest, "Endpoint name required")
		return
	}

	name := parts[0]
	subpath := ""
	if len(parts) > 1 {
		subpath = parts[1]
	}

	// Handle export/import (exact path)
	if name == "export" {
		h.exportEndpoints(w, r)
		return
	}
	if name == "import" {
		h.importEndpoints(w, r)
		return
	}

	// Handle /test and /toggle sub-paths (only when subpath is exactly "test" or "toggle")
	if subpath == "test" {
		h.testEndpoint(w, r, name)
		return
	}
	if subpath == "toggle" {
		h.toggleEndpoint(w, r, name)
		return
	}

	// CRUD: name is the full path so that names containing "/" work
	if subpath != "" {
		name = path
	}
	// Decode URL-encoded name (e.g. "https%3A%2F%2Fapi.example.com" -> "https://api.example.com")
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}

	switch r.Method {
	case http.MethodGet:
		h.getEndpoint(w, r, name)
	case http.MethodPut:
		h.updateEndpoint(w, r, name)
	case http.MethodDelete:
		h.deleteEndpoint(w, r, name)
	default:
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// listEndpoints returns all endpoints
func (h *Handler) listEndpoints(w http.ResponseWriter, r *http.Request) {
	endpoints, err := h.storage.GetEndpoints()
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
		return
	}

	// Mask API keys
	for i := range endpoints {
		endpoints[i].APIKey = maskAPIKey(endpoints[i].APIKey)
	}

	WriteSuccess(w, map[string]interface{}{
		"endpoints": endpoints,
	})
}

// getEndpoint returns a specific endpoint
func (h *Handler) getEndpoint(w http.ResponseWriter, r *http.Request, name string) {
	endpoints, err := h.storage.GetEndpoints()
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
		return
	}

	for _, ep := range endpoints {
		if ep.Name == name {
			ep.APIKey = maskAPIKey(ep.APIKey)
			WriteSuccess(w, ep)
			return
		}
	}

	WriteError(w, http.StatusNotFound, "Endpoint not found")
}

// createEndpoint creates a new endpoint
func (h *Handler) createEndpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		APIUrl      string `json:"apiUrl"`
		APIKey      string `json:"apiKey"`
		Enabled     bool   `json:"enabled"`
		Transformer string `json:"transformer"`
		Model       string `json:"model"`
		Remark      string `json:"remark"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate required fields
	if req.Name == "" || req.APIUrl == "" || req.APIKey == "" {
		WriteError(w, http.StatusBadRequest, "Name, apiUrl, and apiKey are required")
		return
	}

	// Get current endpoints to determine sort order
	endpoints, err := h.storage.GetEndpoints()
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
		return
	}

	// Check if endpoint with same name exists
	for _, ep := range endpoints {
		if ep.Name == req.Name {
			WriteError(w, http.StatusConflict, "Endpoint with this name already exists")
			return
		}
	}

	// Create new endpoint
	endpoint := &storage.Endpoint{
		Name:        req.Name,
		APIUrl:      normalizeAPIUrl(req.APIUrl),
		APIKey:      req.APIKey,
		Enabled:     req.Enabled,
		Transformer: req.Transformer,
		Model:       req.Model,
		Remark:      req.Remark,
		SortOrder:   len(endpoints),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := h.storage.SaveEndpoint(endpoint); err != nil {
		logger.Error("Failed to save endpoint: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to save endpoint")
		return
	}

	// Update proxy config
	if err := h.reloadConfig(); err != nil {
		logger.Error("Failed to reload config: %v", err)
		WriteError(w, http.StatusInternalServerError, "Endpoint created but config reload failed")
		return
	}

	endpoint.APIKey = maskAPIKey(endpoint.APIKey)
	WriteSuccess(w, endpoint)
}

// updateEndpoint updates an existing endpoint
func (h *Handler) updateEndpoint(w http.ResponseWriter, r *http.Request, name string) {
	var req struct {
		Name        string `json:"name"`
		APIUrl      string `json:"apiUrl"`
		APIKey      string `json:"apiKey"`
		Enabled     bool   `json:"enabled"`
		Transformer string `json:"transformer"`
		Model       string `json:"model"`
		Remark      string `json:"remark"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get existing endpoint
	endpoints, err := h.storage.GetEndpoints()
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
		return
	}

	var existing *storage.Endpoint
	for i := range endpoints {
		if endpoints[i].Name == name {
			existing = &endpoints[i]
			break
		}
	}

	if existing == nil {
		WriteError(w, http.StatusNotFound, "Endpoint not found")
		return
	}

	// Update fields
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.APIUrl != "" {
		existing.APIUrl = normalizeAPIUrl(req.APIUrl)
	}
	if req.APIKey != "" {
		existing.APIKey = req.APIKey
	}
	existing.Enabled = req.Enabled
	if req.Transformer != "" {
		existing.Transformer = req.Transformer
	}
	if req.Model != "" {
		existing.Model = req.Model
	}
	existing.Remark = req.Remark
	existing.UpdatedAt = time.Now()

	if err := h.storage.UpdateEndpoint(existing); err != nil {
		logger.Error("Failed to update endpoint: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to update endpoint")
		return
	}

	// Update proxy config
	if err := h.reloadConfig(); err != nil {
		logger.Error("Failed to reload config: %v", err)
		WriteError(w, http.StatusInternalServerError, "Endpoint updated but config reload failed")
		return
	}

	existing.APIKey = maskAPIKey(existing.APIKey)
	WriteSuccess(w, existing)
}

// deleteEndpoint deletes an endpoint
func (h *Handler) deleteEndpoint(w http.ResponseWriter, r *http.Request, name string) {
	if err := h.storage.DeleteEndpoint(name); err != nil {
		if errors.Is(err, storage.ErrEndpointNotFound) {
			WriteError(w, http.StatusNotFound, "Endpoint not found")
			return
		}
		logger.Error("Failed to delete endpoint: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to delete endpoint")
		return
	}

	// Update proxy config
	if err := h.reloadConfig(); err != nil {
		logger.Error("Failed to reload config: %v", err)
		WriteError(w, http.StatusInternalServerError, "Endpoint deleted but config reload failed")
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"message": "Endpoint deleted successfully",
	})
}

// toggleEndpoint enables or disables an endpoint
func (h *Handler) toggleEndpoint(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get existing endpoint
	endpoints, err := h.storage.GetEndpoints()
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
		return
	}

	var existing *storage.Endpoint
	for i := range endpoints {
		if endpoints[i].Name == name {
			existing = &endpoints[i]
			break
		}
	}

	if existing == nil {
		WriteError(w, http.StatusNotFound, "Endpoint not found")
		return
	}

	existing.Enabled = req.Enabled
	existing.UpdatedAt = time.Now()

	if err := h.storage.UpdateEndpoint(existing); err != nil {
		logger.Error("Failed to update endpoint: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to update endpoint")
		return
	}

	// Update proxy config
	if err := h.reloadConfig(); err != nil {
		logger.Error("Failed to reload config: %v", err)
		WriteError(w, http.StatusInternalServerError, "Endpoint toggled but config reload failed")
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"enabled": existing.Enabled,
	})
}

// handleCurrentEndpoint returns the current active endpoint
func (h *Handler) handleCurrentEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	endpoints := h.config.GetEndpoints()
	if len(endpoints) == 0 {
		WriteError(w, http.StatusNotFound, "No endpoints configured")
		return
	}

	// Get enabled endpoints
	var enabledEndpoints []config.Endpoint
	for _, ep := range endpoints {
		if ep.Enabled {
			enabledEndpoints = append(enabledEndpoints, ep)
		}
	}

	if len(enabledEndpoints) == 0 {
		WriteError(w, http.StatusNotFound, "No enabled endpoints")
		return
	}

	name := h.proxy.GetCurrentEndpointName()
	if name == "" {
		name = enabledEndpoints[0].Name
	}
	WriteSuccess(w, map[string]interface{}{
		"name": name,
	})
}

// handleSwitchEndpoint switches to a specific endpoint
func (h *Handler) handleSwitchEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Verify endpoint exists
	endpoints := h.config.GetEndpoints()
	found := false
	for _, ep := range endpoints {
		if ep.Name == req.Name && ep.Enabled {
			found = true
			break
		}
	}

	if !found {
		WriteError(w, http.StatusNotFound, "Endpoint not found or not enabled")
		return
	}

	if err := h.proxy.SetCurrentEndpoint(req.Name); err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"message": "Endpoint switched successfully",
		"name":    req.Name,
	})
}

// handleReorderEndpoints reorders endpoints
func (h *Handler) handleReorderEndpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Names []string `json:"names"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get all endpoints
	endpoints, err := h.storage.GetEndpoints()
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
		return
	}

	// Create a map for quick lookup
	endpointMap := make(map[string]*storage.Endpoint)
	for i := range endpoints {
		endpointMap[endpoints[i].Name] = &endpoints[i]
	}

	// Update sort order
	for i, name := range req.Names {
		if ep, ok := endpointMap[name]; ok {
			ep.SortOrder = i
			ep.UpdatedAt = time.Now()
			if err := h.storage.UpdateEndpoint(ep); err != nil {
				logger.Error("Failed to update endpoint sort order: %v", err)
			}
		}
	}

	// Update proxy config
	if err := h.reloadConfig(); err != nil {
		logger.Error("Failed to reload config: %v", err)
		WriteError(w, http.StatusInternalServerError, "Endpoints reordered but config reload failed")
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"message": "Endpoints reordered successfully",
	})
}

// exportEndpoints returns all endpoints as JSON (full API keys, for backup/transfer)
func (h *Handler) exportEndpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	endpoints, err := h.storage.GetEndpoints()
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
		return
	}
	// Export format: array of endpoint objects (no ID/sortOrder for portability)
	exportList := make([]map[string]interface{}, len(endpoints))
	for i, ep := range endpoints {
		exportList[i] = map[string]interface{}{
			"name":        ep.Name,
			"apiUrl":      ep.APIUrl,
			"apiKey":      ep.APIKey,
			"enabled":     ep.Enabled,
			"transformer": ep.Transformer,
			"model":       ep.Model,
			"remark":      ep.Remark,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"endpoints": exportList})
}

// importEndpoints imports endpoints from JSON (replace or merge)
func (h *Handler) importEndpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		Endpoints []struct {
			Name        string `json:"name"`
			APIUrl      string `json:"apiUrl"`
			APIKey      string `json:"apiKey"`
			Enabled     bool   `json:"enabled"`
			Transformer string `json:"transformer"`
			Model       string `json:"model"`
			Remark      string `json:"remark"`
		} `json:"endpoints"`
		Mode string `json:"mode"` // "replace" or "merge"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if len(req.Endpoints) == 0 {
		WriteError(w, http.StatusBadRequest, "No endpoints to import")
		return
	}
	mode := req.Mode
	if mode == "" {
		mode = "merge"
	}
	if mode != "replace" && mode != "merge" {
		WriteError(w, http.StatusBadRequest, "mode must be 'replace' or 'merge'")
		return
	}

	existing, err := h.storage.GetEndpoints()
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
		return
	}

	if mode == "replace" {
		for _, ep := range existing {
			if err := h.storage.DeleteEndpoint(ep.Name); err != nil {
				logger.Warn("Failed to delete endpoint %s: %v", ep.Name, err)
			}
		}
	}

	existingNames := make(map[string]bool)
	for _, ep := range existing {
		existingNames[ep.Name] = true
	}

	sortOrder := len(existing)
	if mode == "replace" {
		sortOrder = 0
	}

	imported := 0
	for _, ep := range req.Endpoints {
		if ep.Name == "" || ep.APIUrl == "" || ep.APIKey == "" {
			continue
		}
		if mode == "merge" && existingNames[ep.Name] {
			// Update existing
			var existingEp *storage.Endpoint
			for i := range existing {
				if existing[i].Name == ep.Name {
					existingEp = &existing[i]
					break
				}
			}
			if existingEp != nil {
				existingEp.APIUrl = normalizeAPIUrl(ep.APIUrl)
				existingEp.APIKey = ep.APIKey
				existingEp.Enabled = ep.Enabled
				if ep.Transformer != "" {
					existingEp.Transformer = ep.Transformer
				}
				existingEp.Model = ep.Model
				existingEp.Remark = ep.Remark
				existingEp.UpdatedAt = time.Now()
				if err := h.storage.UpdateEndpoint(existingEp); err != nil {
					logger.Warn("Failed to update endpoint %s: %v", ep.Name, err)
					continue
				}
				imported++
			}
			continue
		}
		// Add new
		newEp := &storage.Endpoint{
			Name:        ep.Name,
			APIUrl:      normalizeAPIUrl(ep.APIUrl),
			APIKey:      ep.APIKey,
			Enabled:     ep.Enabled,
			Transformer: ep.Transformer,
			Model:       ep.Model,
			Remark:      ep.Remark,
			SortOrder:   sortOrder,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if newEp.Transformer == "" {
			newEp.Transformer = "claude"
		}
		if err := h.storage.SaveEndpoint(newEp); err != nil {
			logger.Warn("Failed to save endpoint %s: %v", ep.Name, err)
			continue
		}
		imported++
		sortOrder++
	}

	if err := h.reloadConfig(); err != nil {
		logger.Error("Failed to reload config: %v", err)
		WriteError(w, http.StatusInternalServerError, "Endpoints imported but config reload failed")
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"message":  "Import completed",
		"imported": imported,
	})
}

// reloadConfig reloads the configuration from storage and updates the proxy
func (h *Handler) reloadConfig() error {
	adapter := storage.NewConfigStorageAdapter(h.storage)
	cfg, err := config.LoadFromStorage(adapter)
	if err != nil {
		return err
	}

	h.config = cfg
	return h.proxy.UpdateConfig(cfg)
}

// maskAPIKey masks an API key, showing only the last 4 characters
func maskAPIKey(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

// normalizeAPIUrl ensures the API URL has the correct format
func normalizeAPIUrl(apiUrl string) string {
	return strings.TrimSuffix(apiUrl, "/")
}
