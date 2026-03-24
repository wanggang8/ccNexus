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
	// Extract endpoint name from path (name may contain "/" or trailing "/", so preserve path as-is)
	path := strings.TrimPrefix(r.URL.Path, "/api/endpoints/")
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

	// Handle /test, /toggle, and /credentials sub-paths
	if subpath == "test" {
		h.testEndpoint(w, r, name)
		return
	}
	if subpath == "toggle" {
		h.toggleEndpoint(w, r, name)
		return
	}
	if strings.HasPrefix(subpath, "credentials") {
		credentialParts := []string{}
		if subpath == "credentials" {
			credentialParts = nil
		} else if strings.HasPrefix(subpath, "credentials/") {
			credentialParts = strings.Split(strings.TrimPrefix(subpath, "credentials/"), "/")
		}
		h.handleEndpointCredentials(w, r, name, credentialParts)
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

// listEndpoints returns all endpoints for the current user
func (h *Handler) listEndpoints(w http.ResponseWriter, r *http.Request) {
	user := h.currentUser(r)
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Current user not found")
		return
	}
	endpoints, err := h.storage.GetEndpointsByUser(user.ID)
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
		return
	}

	// Mask API keys
	for i := range endpoints {
		endpoints[i].APIKey = maskAPIKey(endpoints[i].APIKey)
	}

	tokenPools := make(map[string]storage.TokenPoolStats, len(endpoints))
	for _, ep := range endpoints {
		stats, statsErr := h.storage.GetTokenPoolStatsByUser(user.ID, ep.Name)
		if statsErr != nil {
			logger.Warn("Failed to get token pool stats for %s: %v", ep.Name, statsErr)
			continue
		}
		tokenPools[ep.Name] = stats
	}

	WriteSuccess(w, map[string]interface{}{
		"endpoints":  endpoints,
		"tokenPools": tokenPools,
	})
}

// getEndpoint returns a specific endpoint for the current user
func (h *Handler) getEndpoint(w http.ResponseWriter, r *http.Request, name string) {
	user := h.currentUser(r)
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Current user not found")
		return
	}
	endpoints, err := h.storage.GetEndpointsByUser(user.ID)
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
		Name             string `json:"name"`
		APIUrl           string `json:"apiUrl"`
		APIKey           string `json:"apiKey"`
		AuthMode         string `json:"authMode"`
		Enabled          bool   `json:"enabled"`
		Transformer      string `json:"transformer"`
		Model            string `json:"model"`
		Remark           string `json:"remark"`
		RequestOverrides string `json:"requestOverrides"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
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

	// Get current user's endpoints to determine sort order
	user := h.currentUser(r)
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Current user not found")
		return
	}
	endpoints, err := h.storage.GetEndpointsByUser(user.ID)
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
		Name:             req.Name,
		APIUrl:           normalizeAPIUrl(req.APIUrl),
		APIKey:           req.APIKey,
		AuthMode:         authMode,
		Enabled:          req.Enabled,
		Transformer:      req.Transformer,
		Model:            req.Model,
		Remark:           req.Remark,
		RequestOverrides: req.RequestOverrides,
		SortOrder:        len(endpoints),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := h.storage.SaveEndpointForUser(user.ID, endpoint); err != nil {
		logger.Error("Failed to save endpoint: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to save endpoint")
		return
	}

	// Update proxy config
	if err := h.reloadConfig(r); err != nil {
		logger.Error("Failed to reload config: %v", err)
		WriteError(w, http.StatusInternalServerError, "Endpoint created but config reload failed")
		return
	}

	endpoint.APIKey = maskAPIKey(endpoint.APIKey)
	WriteSuccess(w, endpoint)
}

// updateEndpoint updates an existing endpoint
func (h *Handler) updateEndpoint(w http.ResponseWriter, r *http.Request, name string) {
	user := h.currentUser(r)
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Current user not found")
		return
	}
	var req struct {
		Name             string `json:"name"`
		APIUrl           string `json:"apiUrl"`
		APIKey           string `json:"apiKey"`
		AuthMode         string `json:"authMode"`
		Enabled          bool   `json:"enabled"`
		Transformer      string `json:"transformer"`
		Model            string `json:"model"`
		Remark           string `json:"remark"`
		RequestOverrides string `json:"requestOverrides"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get existing endpoint for current user
	endpoints, err := h.storage.GetEndpointsByUser(user.ID)
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
	existing.RequestOverrides = req.RequestOverrides
	existing.UpdatedAt = time.Now()

	if err := h.storage.UpdateEndpointForUser(user.ID, existing); err != nil {
		logger.Error("Failed to update endpoint: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to update endpoint")
		return
	}

	// Update proxy config
	if err := h.reloadConfig(r); err != nil {
		logger.Error("Failed to reload config: %v", err)
		WriteError(w, http.StatusInternalServerError, "Endpoint updated but config reload failed")
		return
	}

	existing.APIKey = maskAPIKey(existing.APIKey)
	WriteSuccess(w, existing)
}

// deleteEndpoint deletes an endpoint
func (h *Handler) deleteEndpoint(w http.ResponseWriter, r *http.Request, name string) {
	user := h.currentUser(r)
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Current user not found")
		return
	}
	if err := h.storage.DeleteEndpointForUser(user.ID, name); err != nil {
		if errors.Is(err, storage.ErrEndpointNotFound) {
			WriteError(w, http.StatusNotFound, "Endpoint not found")
			return
		}
		logger.Error("Failed to delete endpoint: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to delete endpoint")
		return
	}

	// Update proxy config
	if err := h.reloadConfig(r); err != nil {
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
	user := h.currentUser(r)
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Current user not found")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get existing endpoint for current user
	endpoints, err := h.storage.GetEndpointsByUser(user.ID)
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

	if err := h.storage.UpdateEndpointForUser(user.ID, existing); err != nil {
		logger.Error("Failed to update endpoint: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to update endpoint")
		return
	}

	// Update proxy config
	if err := h.reloadConfig(r); err != nil {
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
	user := h.currentUser(r)
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Current user not found")
		return
	}

	endpoints, err := h.storage.GetEndpointsByUser(user.ID)
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
		return
	}
	if len(endpoints) == 0 {
		WriteError(w, http.StatusNotFound, "No endpoints configured")
		return
	}

	// Get enabled endpoints
	var enabledEndpoints []storage.Endpoint
	for _, ep := range endpoints {
		if ep.Enabled {
			enabledEndpoints = append(enabledEndpoints, ep)
		}
	}

	if len(enabledEndpoints) == 0 {
		WriteError(w, http.StatusNotFound, "No enabled endpoints")
		return
	}

	name, err := h.proxy.GetCurrentEndpointNameForUser(user.ID)
	if err != nil {
		logger.Error("Failed to get current endpoint: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get current endpoint")
		return
	}
	if name == "" && len(enabledEndpoints) > 0 {
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
	user := h.currentUser(r)
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Current user not found")
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
	endpoints, err := h.storage.GetEndpointsByUser(user.ID)
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
		return
	}
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

	if err := h.proxy.SetCurrentEndpointForUser(user.ID, req.Name); err != nil {
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
	user := h.currentUser(r)
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Current user not found")
		return
	}

	var req struct {
		Names []string `json:"names"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get all endpoints for current user
	endpoints, err := h.storage.GetEndpointsByUser(user.ID)
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
			if err := h.storage.UpdateEndpointForUser(user.ID, ep); err != nil {
				logger.Error("Failed to update endpoint sort order: %v", err)
			}
		}
	}

	// Update proxy config
	if err := h.reloadConfig(r); err != nil {
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
	user := h.currentUser(r)
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Current user not found")
		return
	}
	endpoints, err := h.storage.GetEndpointsByUser(user.ID)
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
	user := h.currentUser(r)
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Current user not found")
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

	existing, err := h.storage.GetEndpointsByUser(user.ID)
	if err != nil {
		logger.Error("Failed to get endpoints: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
		return
	}

	if mode == "replace" {
		for _, ep := range existing {
			if err := h.storage.DeleteEndpointForUser(user.ID, ep.Name); err != nil {
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
		if err := h.storage.SaveEndpointForUser(user.ID, newEp); err != nil {
			logger.Warn("Failed to save endpoint %s: %v", ep.Name, err)
			continue
		}
		imported++
		sortOrder++
	}

	if err := h.reloadConfig(r); err != nil {
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
func (h *Handler) reloadConfig(r *http.Request) error {
	user := h.currentUser(r)
	if user == nil {
		return nil
	}
	adapter := storage.NewConfigStorageAdapterForUser(h.storage, user.ID)
	cfg, err := config.LoadFromStorage(adapter)
	if err != nil {
		return err
	}

	h.config = cfg
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
