package api

import (
	"encoding/json"
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
		if name := r.URL.Query().Get("name"); name != "" {
			h.getEndpoint(w, r, name)
			return
		}
		h.listEndpoints(w, r)
	case http.MethodPost:
		h.createEndpoint(w, r)
	case http.MethodPut:
		name := r.URL.Query().Get("name")
		if name == "" {
			WriteError(w, http.StatusBadRequest, "Endpoint name required")
			return
		}
		h.updateEndpoint(w, r, name)
	case http.MethodDelete:
		// Names may contain "http://..."; path segments are unreliable after http.ServeMux path cleaning.
		name := r.URL.Query().Get("name")
		if name == "" {
			WriteError(w, http.StatusBadRequest, "Endpoint name required")
			return
		}
		h.deleteEndpoint(w, r, name)
	default:
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *Handler) handleEndpointsToggleByQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		WriteError(w, http.StatusBadRequest, "Endpoint name required")
		return
	}
	h.toggleEndpoint(w, r, name)
}

func (h *Handler) handleEndpointsTestByQuery(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		WriteError(w, http.StatusBadRequest, "Endpoint name required")
		return
	}
	h.testEndpoint(w, r, name)
}

func (h *Handler) handleEndpointsRevealKeyByQuery(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		WriteError(w, http.StatusBadRequest, "Endpoint name required")
		return
	}
	h.revealEndpointKey(w, r, name)
}

// dispatch* avoids shadowing endpoint names that equal reserved path segments (toggle, test, …).
func (h *Handler) dispatchEndpointsTogglePath(w http.ResponseWriter, r *http.Request) {
	if (r.Method == http.MethodPatch || r.Method == http.MethodPost) && r.URL.Query().Get("name") != "" {
		h.handleEndpointsToggleByQuery(w, r)
		return
	}
	h.handleEndpointByName(w, r)
}

func (h *Handler) dispatchEndpointsTestPath(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("name") != "" {
		h.handleEndpointsTestByQuery(w, r)
		return
	}
	h.handleEndpointByName(w, r)
}

func (h *Handler) dispatchEndpointsRevealKeyPath(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Query().Get("name") != "" {
		h.handleEndpointsRevealKeyByQuery(w, r)
		return
	}
	h.handleEndpointByName(w, r)
}

func (h *Handler) dispatchEndpointsCredentialsPath(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("name") != "" {
		h.handleEndpointsCredentialsByQuery(w, r)
		return
	}
	h.handleEndpointByName(w, r)
}

func (h *Handler) dispatchEndpointsCredentialsImportPath(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Query().Get("name") != "" {
		h.handleEndpointsCredentialsImportByQuery(w, r)
		return
	}
	h.handleEndpointByName(w, r)
}

func (h *Handler) dispatchEndpointsCredentialsStatsPath(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Query().Get("name") != "" {
		h.handleEndpointsCredentialsStatsByQuery(w, r)
		return
	}
	h.handleEndpointByName(w, r)
}

// handleEndpointByName handles GET, PUT, DELETE, PATCH for specific endpoint
func (h *Handler) handleEndpointByName(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/endpoints/")
	if path == "" {
		WriteError(w, http.StatusBadRequest, "Endpoint name required")
		return
	}

	decode := func(v string) string {
		if d, err := url.PathUnescape(v); err == nil {
			return d
		}
		return v
	}

	dispatchExactEndpoint := func(name string) bool {
		if name == "" {
			return false
		}
		switch r.Method {
		case http.MethodGet, http.MethodPut, http.MethodDelete:
		default:
			return false
		}

		endpoint, err := h.getEndpointByName(name)
		if err != nil {
			logger.Error("Failed to get endpoint: %v", err)
			WriteError(w, http.StatusInternalServerError, "Failed to get endpoint")
			return true
		}
		if endpoint == nil {
			return false
		}

		switch r.Method {
		case http.MethodGet:
			h.getEndpoint(w, r, name)
		case http.MethodPut:
			h.updateEndpoint(w, r, name)
		case http.MethodDelete:
			h.deleteEndpoint(w, r, name)
		}
		return true
	}

	name := decode(path)
	if dispatchExactEndpoint(name) {
		return
	}

	// Prefer exact endpoint matches for resource methods before interpreting reserved suffixes.
	if strings.HasSuffix(path, "/test") {
		name = strings.TrimSuffix(path, "/test")
		name = strings.TrimSuffix(name, "/")
		name = decode(name)
		if name == "" {
			WriteError(w, http.StatusBadRequest, "Endpoint name required")
			return
		}
		h.testEndpoint(w, r, name)
		return
	}

	if strings.HasSuffix(path, "/toggle") {
		name = strings.TrimSuffix(path, "/toggle")
		name = strings.TrimSuffix(name, "/")
		name = decode(name)
		if name == "" {
			WriteError(w, http.StatusBadRequest, "Endpoint name required")
			return
		}
		h.toggleEndpoint(w, r, name)
		return
	}

	if strings.HasSuffix(path, "/reveal-key") {
		name = strings.TrimSuffix(path, "/reveal-key")
		name = strings.TrimSuffix(name, "/")
		name = decode(name)
		if name == "" {
			WriteError(w, http.StatusBadRequest, "Endpoint name required")
			return
		}
		h.revealEndpointKey(w, r, name)
		return
	}

	if idx := strings.Index(path, "/credentials"); idx >= 0 {
		name = strings.TrimSuffix(path[:idx], "/")
		name = decode(name)
		if name == "" {
			WriteError(w, http.StatusBadRequest, "Endpoint name required")
			return
		}

		rest := strings.TrimPrefix(path[idx:], "/credentials")
		rest = strings.TrimPrefix(rest, "/")
		parts := []string{}
		if rest != "" {
			parts = strings.Split(rest, "/")
		}
		h.handleEndpointCredentials(w, r, name, parts)
		return
	}

	if name == "" {
		WriteError(w, http.StatusBadRequest, "Endpoint name required")
		return
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

	tokenPools, err := h.storage.GetAllTokenPoolStats()
	if err != nil {
		logger.Warn("Failed to get token pool stats: %v", err)
		tokenPools = map[string]storage.TokenPoolStats{}
	}

	WriteSuccess(w, map[string]interface{}{
		"endpoints":       endpoints,
		"tokenPools":      tokenPools,
		"currentEndpoint": h.proxy.GetCurrentEndpointName(),
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
		AuthMode    string `json:"authMode"`
		Enabled     bool   `json:"enabled"`
		Transformer string `json:"transformer"`
		Model       string `json:"model"`
		Remark      string `json:"remark"`
		CloneFrom   string `json:"cloneFrom"` // Clone from existing endpoint name
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// If cloning, get API key from source endpoint
	if req.CloneFrom != "" && req.APIKey == "" {
		endpoints, err := h.storage.GetEndpoints()
		if err == nil {
			for _, ep := range endpoints {
				if ep.Name == req.CloneFrom {
					req.APIKey = ep.APIKey
					break
				}
			}
		}
	}

	authMode := config.NormalizeAuthMode(req.AuthMode)
	normalizedEndpoint := config.Endpoint{
		APIUrl:      normalizeAPIUrl(req.APIUrl),
		APIKey:      req.APIKey,
		AuthMode:    authMode,
		Transformer: req.Transformer,
		Model:       req.Model,
		Remark:      req.Remark,
	}
	if normalizedEndpoint.Transformer == "" {
		normalizedEndpoint.Transformer = "claude"
	}
	config.ApplyEndpointAuthModeRules(&normalizedEndpoint)
	authMode = normalizedEndpoint.AuthMode
	req.APIUrl = normalizedEndpoint.APIUrl
	req.APIKey = normalizedEndpoint.APIKey
	req.Transformer = normalizedEndpoint.Transformer

	// Validate required fields
	if req.Name == "" || req.APIUrl == "" {
		WriteError(w, http.StatusBadRequest, "Name and apiUrl are required")
		return
	}
	if authMode == config.AuthModeAPIKey && req.APIKey == "" {
		WriteError(w, http.StatusBadRequest, "apiKey is required in api_key mode")
		return
	}
	if config.IsTokenPoolAuthMode(authMode) {
		req.APIKey = ""
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
		AuthMode:    authMode,
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
		AuthMode    string `json:"authMode"`
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
	if req.AuthMode != "" {
		existing.AuthMode = config.NormalizeAuthMode(req.AuthMode)
	}
	if existing.AuthMode == "" {
		existing.AuthMode = config.AuthModeAPIKey
	}
	normalizedEndpoint := config.Endpoint{
		Name:        existing.Name,
		APIUrl:      existing.APIUrl,
		APIKey:      existing.APIKey,
		AuthMode:    existing.AuthMode,
		Enabled:     existing.Enabled,
		Transformer: existing.Transformer,
		Model:       existing.Model,
		Remark:      existing.Remark,
	}
	if normalizedEndpoint.Transformer == "" {
		normalizedEndpoint.Transformer = "claude"
	}
	config.ApplyEndpointAuthModeRules(&normalizedEndpoint)
	existing.APIUrl = normalizedEndpoint.APIUrl
	existing.APIKey = normalizedEndpoint.APIKey
	existing.AuthMode = normalizedEndpoint.AuthMode
	existing.Transformer = normalizedEndpoint.Transformer
	if existing.AuthMode == config.AuthModeAPIKey && existing.APIKey == "" {
		WriteError(w, http.StatusBadRequest, "apiKey is required in api_key mode")
		return
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
	}

	existing.APIKey = maskAPIKey(existing.APIKey)
	WriteSuccess(w, existing)
}

// deleteEndpoint deletes an endpoint
func (h *Handler) deleteEndpoint(w http.ResponseWriter, r *http.Request, name string) {
	if err := h.storage.DeleteEndpoint(name); err != nil {
		logger.Error("Failed to delete endpoint: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to delete endpoint")
		return
	}

	// Update proxy config
	if err := h.reloadConfig(); err != nil {
		logger.Error("Failed to reload config: %v", err)
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
	}

	WriteSuccess(w, map[string]interface{}{
		"enabled": existing.Enabled,
	})
}

func (h *Handler) revealEndpointKey(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	endpoint, err := h.getEndpointByName(name)
	if err != nil {
		logger.Error("Failed to get endpoint: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoint")
		return
	}
	if endpoint == nil {
		WriteError(w, http.StatusNotFound, "Endpoint not found")
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"apiKey": endpoint.APIKey,
	})
}

// handleCurrentEndpoint returns the current active endpoint
func (h *Handler) handleCurrentEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	current := h.proxy.GetCurrentEndpointName()
	if current == "" {
		WriteError(w, http.StatusNotFound, "No enabled endpoints")
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"name": current,
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
		logger.Error("Failed to switch endpoint: %v", err)
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
	}

	WriteSuccess(w, map[string]interface{}{
		"message": "Endpoints reordered successfully",
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
	if h.proxy == nil {
		return nil
	}
	return h.proxy.UpdateConfig(cfg)
}

// maskAPIKey masks an API key, showing only the last 4 characters
func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

// normalizeAPIUrl ensures the API URL has the correct format
func normalizeAPIUrl(apiUrl string) string {
	return strings.TrimSuffix(apiUrl, "/")
}
