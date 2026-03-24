package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/storage"
)

const testClientTimeout = 8 * time.Second

// testEndpoint tests an endpoint's connectivity using tiered strategy (matches Desktop TestEndpointLight)
func (h *Handler) testEndpoint(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Token")
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
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

	var endpoint *storage.Endpoint
	for i := range endpoints {
		if endpoints[i].Name == name {
			endpoint = &endpoints[i]
			break
		}
	}

	if endpoint == nil {
		WriteError(w, http.StatusNotFound, "Endpoint not found")
		return
	}

	transformer := endpoint.Transformer
	if transformer == "" {
		transformer = "claude"
	}
	if transformer == "passthrough" {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "passthrough transformer does not support connectivity test (API format is provider-specific)",
		})
		return
	}

	normalizedURL := normalizeAPIUrl(endpoint.APIUrl)
	if !strings.HasPrefix(normalizedURL, "http://") && !strings.HasPrefix(normalizedURL, "https://") {
		normalizedURL = "https://" + normalizedURL
	}

	start := time.Now()
	response, err := h.runTieredTest(normalizedURL, endpoint.APIKey, transformer, endpoint.Model)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"latency": latency,
			"error":   err.Error(),
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"latency":  latency,
		"response": response,
	})
}

// runTieredTest tries light methods first, then fallback to minimal chat (matches Desktop TestEndpointLight)
func (h *Handler) runTieredTest(apiUrl, apiKey, transformer, model string) (string, error) {
	client := &http.Client{Timeout: testClientTimeout}

	// Step 1: Try models API
	statusCode, err := h.testModelsAPI(client, apiUrl, apiKey, transformer)
	if err == nil {
		return "Models API accessible", nil
	}
	logger.Debug("Test step 1 (models API) failed: %v (status %d)", err, statusCode)
	if statusCode == 401 || statusCode == 403 {
		return "", fmt.Errorf("authentication failed: HTTP %d", statusCode)
	}

	// Step 2: Try token count (Claude/CLI) or billing API (OpenAI)
	if transformer == "claude" || transformer == "cli" {
		statusCode, err = h.testTokenCountAPI(client, apiUrl, apiKey)
		if err == nil {
			return "Token count API accessible", nil
		}
		logger.Debug("Test step 2 (token count) failed: %v (status %d)", err, statusCode)
		if statusCode == 401 || statusCode == 403 {
			return "", fmt.Errorf("authentication failed: HTTP %d", statusCode)
		}
	} else if transformer == "openai" || transformer == "openai2" {
		statusCode, err = h.testBillingAPI(client, apiUrl, apiKey)
		if err == nil {
			return "Billing API accessible", nil
		}
		logger.Debug("Test step 2 (billing) failed: %v (status %d)", err, statusCode)
		if statusCode == 401 || statusCode == 403 {
			return "", fmt.Errorf("authentication failed: HTTP %d", statusCode)
		}
	}

	// Step 3: Minimal request (fallback)
	statusCode, err = h.testMinimalRequest(client, apiUrl, apiKey, transformer, model)
	if err == nil {
		return "Minimal request successful", nil
	}
	logger.Debug("Test step 3 (minimal request) failed: %v (status %d)", err, statusCode)
	if statusCode == 401 || statusCode == 403 {
		return "", fmt.Errorf("authentication failed: HTTP %d", statusCode)
	}
	if statusCode == 405 {
		return "", fmt.Errorf("method not allowed (may work in real client)")
	}
	// Extract user-friendly message from JSON error body when possible
	if statusCode >= 400 && statusCode < 600 {
		if friendly := extractErrorMessage(err.Error()); friendly != "" {
			return "", fmt.Errorf("%s", friendly)
		}
	}
	return "", err
}

// extractErrorMessage tries to extract error.message from JSON in "API returned status N: {...}" format
func extractErrorMessage(errStr string) string {
	idx := strings.Index(errStr, ": ")
	if idx < 0 {
		return ""
	}
	jsonPart := strings.TrimSpace(errStr[idx+2:])
	if len(jsonPart) == 0 || (jsonPart[0] != '{' && jsonPart[0] != '[') {
		return ""
	}
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(jsonPart), &v); err != nil {
		return ""
	}
	if errObj, ok := v["error"].(map[string]interface{}); ok {
		if msg, ok := errObj["message"].(string); ok && msg != "" {
			return msg
		}
	}
	return ""
}

func (h *Handler) testModelsAPI(client *http.Client, apiUrl, apiKey, transformer string) (int, error) {
	var url string
	if transformer == "gemini" {
		url = fmt.Sprintf("%s/v1beta/models?key=%s", apiUrl, apiKey)
	} else {
		url = fmt.Sprintf("%s/v1/models", apiUrl)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	if transformer != "gemini" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return resp.StatusCode, err
	}
	if data, ok := result["data"].([]interface{}); ok && len(data) > 0 {
		return resp.StatusCode, nil
	}
	if models, ok := result["models"].([]interface{}); ok && len(models) > 0 {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, fmt.Errorf("unexpected response format")
}

func (h *Handler) testTokenCountAPI(client *http.Client, apiUrl, apiKey string) (int, error) {
	url := fmt.Sprintf("%s/v1/messages/count_tokens", apiUrl)
	body, _ := json.Marshal(map[string]interface{}{
		"model": "claude-sonnet-4-5-20250929",
		"messages": []map[string]string{{"role": "user", "content": "Hi"}},
	})

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "token-counting-2024-11-01")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return resp.StatusCode, err
	}
	if _, ok := result["input_tokens"]; !ok {
		return resp.StatusCode, fmt.Errorf("invalid response: no input_tokens")
	}
	return resp.StatusCode, nil
}

func (h *Handler) testBillingAPI(client *http.Client, apiUrl, apiKey string) (int, error) {
	url := fmt.Sprintf("%s/v1/dashboard/billing/credit_grants", apiUrl)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func (h *Handler) testMinimalRequest(client *http.Client, apiUrl, apiKey, transformer, model string) (int, error) {
	var url string
	var body []byte

	switch transformer {
	case "claude", "cli":
		url = fmt.Sprintf("%s/v1/messages", apiUrl)
		if model == "" {
			model = "claude-sonnet-4-5-20250929"
		}
		body, _ = json.Marshal(map[string]interface{}{
			"model":      model,
			"max_tokens": 1,
			"messages":   []map[string]string{{"role": "user", "content": "Hi"}},
		})
	case "openai":
		url = fmt.Sprintf("%s/v1/chat/completions", apiUrl)
		if model == "" {
			model = "gpt-4-turbo"
		}
		body, _ = json.Marshal(map[string]interface{}{
			"model":      model,
			"max_tokens": 1,
			"messages":   []map[string]interface{}{{"role": "user", "content": "Hi"}},
		})
	case "openai2":
		url = fmt.Sprintf("%s/v1/responses", apiUrl)
		if model == "" {
			model = "gpt-4-turbo"
		}
		body, _ = json.Marshal(map[string]interface{}{
			"model": model,
			"input": []map[string]interface{}{
				{"type": "message", "role": "user", "content": []map[string]interface{}{{"type": "input_text", "text": "Hi"}}},
			},
		})
	case "gemini":
		if model == "" {
			model = "gemini-2.0-flash"
		}
		url = fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", apiUrl, model, apiKey)
		body, _ = json.Marshal(map[string]interface{}{
			"contents":         []map[string]interface{}{{"parts": []map[string]string{{"text": "Hi"}}}},
			"generationConfig": map[string]int{"maxOutputTokens": 1},
		})
	default:
		return 0, fmt.Errorf("unsupported transformer: %s", transformer)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if transformer == "claude" || transformer == "cli" {
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if transformer != "gemini" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client30 := &http.Client{Timeout: 30 * time.Second}
	resp, err := client30.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}
	return resp.StatusCode, nil
}

func (h *Handler) resolveEndpointAPIKey(r *http.Request, endpoint *storage.Endpoint) (string, error) {
	authMode := config.NormalizeAuthMode(endpoint.AuthMode)
	if config.IsTokenPoolAuthMode(authMode) {
		user := h.currentUser(r)
		if user == nil {
			return "", fmt.Errorf("current user not found")
		}
		cred, err := h.storage.GetUsableEndpointCredentialForUser(user.ID, endpoint.Name, time.Now().UTC())
		if err != nil {
			return "", fmt.Errorf("failed to get token from pool: %w", err)
		}
		if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
			return "", fmt.Errorf("no usable token in token pool")
		}
		return strings.TrimSpace(cred.AccessToken), nil
	}

	apiKey := strings.TrimSpace(endpoint.APIKey)
	if apiKey == "" {
		return "", fmt.Errorf("apiKey is empty")
	}
	return apiKey, nil
}

// handleFetchModels fetches available models from a provider
func (h *Handler) handleFetchModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		APIUrl       string `json:"apiUrl"`
		APIKey       string `json:"apiKey"`
		Transformer  string `json:"transformer"`
		EndpointName string `json:"endpointName"` // when editing with masked key, use stored credentials
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	apiUrl, apiKey := req.APIUrl, req.APIKey
	user := h.currentUser(r)
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Current user not found")
		return
	}
	if (apiKey == "" || apiKey == "****") && req.EndpointName != "" {
		// Use stored credentials when editing (frontend has masked key)
		endpoints, err := h.storage.GetEndpointsByUser(user.ID)
		if err != nil {
			logger.Error("Failed to get endpoints: %v", err)
			WriteError(w, http.StatusInternalServerError, "Failed to get endpoints")
			return
		}
		found := false
		for _, ep := range endpoints {
			if ep.Name == req.EndpointName {
				apiUrl, apiKey = ep.APIUrl, ep.APIKey
				found = true
				break
			}
		}
		if !found {
			WriteError(w, http.StatusNotFound, "Endpoint not found")
			return
		}
	}

	models, err := h.fetchModelsFromProvider(apiUrl, apiKey, req.Transformer)
	if err != nil {
		logger.Error("Failed to fetch models: %v", err)
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to fetch models: %v", err))
		return
	}

	WriteSuccess(w, map[string]interface{}{
		"models": models,
	})
}

// fetchModelsFromProvider fetches available models from a provider (matches Desktop FetchModels)
func (h *Handler) fetchModelsFromProvider(apiUrl, apiKey, transformer string) ([]string, error) {
	normalizedURL := normalizeAPIUrl(apiUrl)
	if !strings.HasPrefix(normalizedURL, "http://") && !strings.HasPrefix(normalizedURL, "https://") {
		normalizedURL = "https://" + normalizedURL
	}

	switch transformer {
	case "claude", "openai", "openai2", "cli":
		return h.fetchOpenAIModels(normalizedURL, apiKey)
	case "gemini":
		return h.fetchGeminiModels(normalizedURL, apiKey)
	default:
		return nil, fmt.Errorf("unsupported transformer: %s", transformer)
	}
}

func (h *Handler) fetchOpenAIModels(apiUrl, apiKey string) ([]string, error) {
	url := fmt.Sprintf("%s/v1/models", apiUrl)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		id := strings.TrimSpace(m.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	return models, nil
}

func (h *Handler) fetchGeminiModels(apiUrl, apiKey string) ([]string, error) {
	url := fmt.Sprintf("%s/v1beta/models?key=%s", apiUrl, apiKey)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]string, 0, len(result.Models))
	for _, m := range result.Models {
		name := strings.TrimPrefix(m.Name, "models/")
		models = append(models, name)
	}
	return models, nil
}
