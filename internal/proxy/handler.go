package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/tokencount"
)

// handleHealth handles health check requests
func (p *Proxy) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	endpoints := p.getEnabledEndpoints()

	// Mask API keys before sending response to prevent security leak
	maskedEndpoints := make([]config.Endpoint, len(endpoints))
	for i, ep := range endpoints {
		maskedEndpoints[i] = ep
		maskedEndpoints[i].APIKey = maskAPIKey(ep.APIKey)
	}

	response := map[string]interface{}{
		"status":            "healthy",
		"enabled_endpoints": len(endpoints),
		"endpoints":         maskedEndpoints,
	}

	json.NewEncoder(w).Encode(response)
}

// maskAPIKey masks an API key for security, showing only first 4 and last 4 characters
func maskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}

// handleStats handles statistics requests
func (p *Proxy) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	stats := p.GetStats()
	json.NewEncoder(w).Encode(stats)
}

// GetStats returns current statistics
func (p *Proxy) GetStats() *Stats {
	return p.stats
}

// handleCountTokens handles token counting requests
func (p *Proxy) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Model    string                   `json:"model"`
		System   interface{}              `json:"system,omitempty"`
		Messages []map[string]interface{} `json:"messages"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Error("Failed to decode count_tokens request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	systemText := ""
	if req.System != nil {
		switch sys := req.System.(type) {
		case string:
			systemText = sys
		case []interface{}:
			for _, block := range sys {
				if blockMap, ok := block.(map[string]interface{}); ok {
					if text, ok := blockMap["text"].(string); ok {
						systemText += text + "\n"
					}
				}
			}
		}
	}

	totalTokens := 0
	if systemText != "" {
		totalTokens += tokencount.EstimateOutputTokens(systemText)
	}

	for _, msg := range req.Messages {
		content, ok := msg["content"]
		if !ok {
			continue
		}

		switch c := content.(type) {
		case string:
			totalTokens += tokencount.EstimateOutputTokens(c)
		case []interface{}:
			for _, block := range c {
				if blockMap, ok := block.(map[string]interface{}); ok {
					if text, ok := blockMap["text"].(string); ok {
						totalTokens += tokencount.EstimateOutputTokens(text)
					}
				}
			}
		}
	}

	response := map[string]interface{}{
		"input_tokens": totalTokens,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetModels handles model list requests for Augment/CLI clients
func (p *Proxy) handleGetModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Fetch real model list from current endpoint
	models, err := p.fetchModelsFromEndpoint()
	if err != nil {
		logger.Warn("获取模型列表失败，返回默认列表: %v", err)
		// Fallback to default models if fetch fails
		models = p.getDefaultModels()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models)
	logger.Debug("返回模型列表，共 %d 个模型", len(models))
}

// fetchModelsFromEndpoint fetches model list from the current endpoint
func (p *Proxy) fetchModelsFromEndpoint() (map[string]interface{}, error) {
	p.mu.RLock()
	endpoints := p.getEnabledEndpoints()
	if len(endpoints) == 0 {
		p.mu.RUnlock()
		return nil, fmt.Errorf("no enabled endpoints")
	}
	endpoint := endpoints[p.currentIndex]
	p.mu.RUnlock()

	// Create request to /v1/models endpoint
	modelsURL := strings.TrimSuffix(endpoint.APIUrl, "/messages") + "/models"
	req, err := http.NewRequest("GET", modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// Set authentication headers
	req.Header.Set("x-api-key", endpoint.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	// Make the request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API 返回错误状态 %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var apiResponse struct {
		Data []struct {
			ID          string    `json:"id"`
			DisplayName string    `json:"display_name"`
			CreatedAt   time.Time `json:"created_at"`
			Type        string    `json:"type"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// Convert to Augment-compatible format
	models := make(map[string]interface{})
	for i, model := range apiResponse.Data {
		priority := 100 - i // Higher priority for newer models
		shortName := p.extractShortName(model.ID)
		
		models[model.ID] = map[string]interface{}{
			"displayName":   model.DisplayName,
			"description":   model.DisplayName,
			"shortName":     shortName,
			"priority":      priority,
			"isLegacyModel": p.isLegacyModel(model.ID),
		}
	}

	return models, nil
}

// extractShortName extracts a short name from model ID
func (p *Proxy) extractShortName(modelID string) string {
	// Extract short name from model ID
	// e.g., "claude-sonnet-4-5-20250929" -> "sonnet-4.5"
	if strings.Contains(modelID, "sonnet-4-5") {
		return "sonnet-4.5"
	} else if strings.Contains(modelID, "opus-4") {
		return "opus-4"
	} else if strings.Contains(modelID, "3-5-sonnet") {
		return "3.5-sonnet"
	} else if strings.Contains(modelID, "3-5-haiku") {
		return "3.5-haiku"
	} else if strings.Contains(modelID, "3-opus") {
		return "3-opus"
	}
	// Default: use the part before the date
	parts := strings.Split(modelID, "-")
	if len(parts) >= 2 {
		return strings.Join(parts[:2], "-")
	}
	return modelID
}

// isLegacyModel determines if a model is legacy
func (p *Proxy) isLegacyModel(modelID string) bool {
	// Models before Claude 3 are considered legacy
	return strings.Contains(modelID, "claude-2") || 
		strings.Contains(modelID, "claude-1") ||
		strings.Contains(modelID, "claude-instant")
}

// getDefaultModels returns default model list as fallback
func (p *Proxy) getDefaultModels() map[string]interface{} {
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

// handleGetBalance handles balance query requests for Augment/CLI clients
func (p *Proxy) handleGetBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Return fake balance info
	balance := map[string]interface{}{
		"balance":     1000000,
		"currency":    "USD",
		"lastUpdated": "2024-01-01T00:00:00Z",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(balance)
	logger.Debug("返回余额信息")
}

// handleGetLoginToken handles login token requests for Augment/CLI clients
func (p *Proxy) handleGetLoginToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Return fake login token info
	token := map[string]interface{}{
		"token":     "fake-login-token",
		"expiresAt": "2099-12-31T23:59:59Z",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(token)
	logger.Debug("返回登录令牌信息")
}

// UpdateConfig updates the proxy configuration
func (p *Proxy) UpdateConfig(cfg *config.Config) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Save current endpoint name
	var currentEndpointName string
	if p.config != nil {
		endpoints := p.getEnabledEndpoints()
		if len(endpoints) > 0 && p.currentIndex < len(endpoints) {
			currentEndpointName = endpoints[p.currentIndex].Name
		}
	}

	p.config = cfg

	// Try to find the previous current endpoint in new config
	newEndpoints := p.getEnabledEndpoints()
	if currentEndpointName != "" && len(newEndpoints) > 0 {
		found := false
		for i, ep := range newEndpoints {
			if ep.Name == currentEndpointName {
				p.currentIndex = i
				found = true
				logger.Debug("[配置更新] 保留当前端点: %s (索引 %d)", currentEndpointName, i)
				break
			}
		}
		if !found {
			p.currentIndex = 0
			logger.Debug("[配置更新] 当前端点 '%s' 未找到，重置为索引 0", currentEndpointName)
		}
	} else {
		p.currentIndex = 0
	}

	logger.Info("配置已更新: 已配置 %d 个端点", len(cfg.GetEndpoints()))
	return nil
}
