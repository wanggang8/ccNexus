package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/storage"
)

// mockStatsStorage implements StatsStorage interface for testing
type mockStatsStorage struct{}

func (m *mockStatsStorage) RecordDailyStat(stat interface{}) error {
	return nil
}

func (m *mockStatsStorage) GetTotalStats() (int, map[string]interface{}, error) {
	return 0, make(map[string]interface{}), nil
}

func (m *mockStatsStorage) GetDailyStats(endpointName, startDate, endDate string) ([]interface{}, error) {
	return []interface{}{}, nil
}

func (m *mockStatsStorage) GetPeriodStatsAggregated(startDate, endDate string) (map[string]interface{}, error) {
	return make(map[string]interface{}), nil
}

func TestHandleGetModels(t *testing.T) {
	// Create a test proxy instance
	cfg := &config.Config{
		Endpoints: []config.Endpoint{
			{
				Name:        "test-endpoint",
				APIUrl:      "https://api.anthropic.com/v1/messages",
				APIKey:      "test-key",
				Enabled:     true,
				Transformer: "claude",
			},
		},
	}

	sqliteStorage, err := storage.NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer sqliteStorage.Close()

	mockStats := &mockStatsStorage{}
	proxy := New(cfg, mockStats, sqliteStorage, "test-device")

	// Create test request
	req := httptest.NewRequest("GET", "/usage/api/get-models", nil)
	w := httptest.NewRecorder()

	// Call handler
	proxy.handleGetModels(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Parse response
	var models map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&models); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify models exist
	if len(models) == 0 {
		t.Error("Expected at least one model in response")
	}

	// Verify model structure
	for modelID, modelData := range models {
		modelMap, ok := modelData.(map[string]interface{})
		if !ok {
			t.Errorf("Model %s has invalid structure", modelID)
			continue
		}

		// Check required fields
		requiredFields := []string{"displayName", "description", "shortName", "priority", "isLegacyModel"}
		for _, field := range requiredFields {
			if _, exists := modelMap[field]; !exists {
				t.Errorf("Model %s missing required field: %s", modelID, field)
			}
		}
	}
}

func TestExtractShortName(t *testing.T) {
	cfg := &config.Config{}
	sqliteStorage, _ := storage.NewSQLiteStorage(":memory:")
	defer sqliteStorage.Close()
	mockStats := &mockStatsStorage{}
	proxy := New(cfg, mockStats, sqliteStorage, "test-device")

	tests := []struct {
		modelID  string
		expected string
	}{
		{"claude-sonnet-4-5-20250929", "sonnet-4.5"},
		{"claude-opus-4-20250514", "opus-4"},
		{"claude-3-5-sonnet-20241022", "3.5-sonnet"},
		{"claude-3-5-haiku-20241022", "3.5-haiku"},
		{"claude-3-opus-20240229", "3-opus"},
		{"unknown-model-123", "unknown-model"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := proxy.extractShortName(tt.modelID)
			if result != tt.expected {
				t.Errorf("extractShortName(%s) = %s, want %s", tt.modelID, result, tt.expected)
			}
		})
	}
}

func TestIsLegacyModel(t *testing.T) {
	cfg := &config.Config{}
	sqliteStorage, _ := storage.NewSQLiteStorage(":memory:")
	defer sqliteStorage.Close()
	mockStats := &mockStatsStorage{}
	proxy := New(cfg, mockStats, sqliteStorage, "test-device")

	tests := []struct {
		modelID  string
		expected bool
	}{
		{"claude-2-1", true},
		{"claude-2-0", true},
		{"claude-1-3", true},
		{"claude-instant-1-2", true},
		{"claude-3-opus-20240229", false},
		{"claude-3-5-sonnet-20241022", false},
		{"claude-sonnet-4-5-20250929", false},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := proxy.isLegacyModel(tt.modelID)
			if result != tt.expected {
				t.Errorf("isLegacyModel(%s) = %v, want %v", tt.modelID, result, tt.expected)
			}
		})
	}
}

func TestGetDefaultModels(t *testing.T) {
	cfg := &config.Config{}
	sqliteStorage, _ := storage.NewSQLiteStorage(":memory:")
	defer sqliteStorage.Close()
	mockStats := &mockStatsStorage{}
	proxy := New(cfg, mockStats, sqliteStorage, "test-device")

	models := proxy.getDefaultModels()

	// Check that default models are returned
	if len(models) == 0 {
		t.Error("Expected default models, got empty map")
	}

	// Verify specific default models exist
	expectedModels := []string{
		"claude-sonnet-4-5-20250929",
		"claude-opus-4-20250514",
		"claude-3-5-sonnet-20241022",
	}

	for _, modelID := range expectedModels {
		if _, exists := models[modelID]; !exists {
			t.Errorf("Expected default model %s not found", modelID)
		}
	}
}

func TestHandleGetModelsMethodNotAllowed(t *testing.T) {
	cfg := &config.Config{}
	sqliteStorage, _ := storage.NewSQLiteStorage(":memory:")
	defer sqliteStorage.Close()
	mockStats := &mockStatsStorage{}
	proxy := New(cfg, mockStats, sqliteStorage, "test-device")

	// Test with POST method (should fail)
	req := httptest.NewRequest("POST", "/usage/api/get-models", nil)
	w := httptest.NewRecorder()

	proxy.handleGetModels(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}
