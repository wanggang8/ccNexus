package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/proxy"
)

func TestTrafficAPIs(t *testing.T) {
	// Create test proxy with traffic recorder
	cfg := &config.Config{}
	p := proxy.New(cfg, nil, "test-device")
	recorder := p.GetTrafficRecorder()

	// Create handler
	h := NewHandler(cfg, p, nil, "")

	t.Run("GetRecordingStatus", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/traffic/recording", nil)
		w := httptest.NewRecorder()

		h.handleTrafficRecording(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var resp struct {
			Success bool                   `json:"success"`
			Data    map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !resp.Success {
			t.Error("Expected success=true")
		}

		if _, ok := resp.Data["recording"]; !ok {
			t.Error("Response missing 'recording' field")
		}
	})

	t.Run("EnableRecording", func(t *testing.T) {
		body := strings.NewReader(`{"recording": true}`)
		req := httptest.NewRequest("POST", "/api/traffic/recording", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleTrafficRecording(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var resp struct {
			Success bool                   `json:"success"`
			Data    map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !resp.Success {
			t.Error("Expected success=true")
		}

		if !recorder.IsRecording() {
			t.Error("Recording should be enabled")
		}
	})

	t.Run("GetLogs", func(t *testing.T) {
		// Add a test log
		recorder.SetRecording(true)
		recorder.Record(&proxy.TrafficLog{
			Timestamp:    time.Now(),
			EndpointName: "test-endpoint",
			ClientFormat: "openai",
			Method:       "POST",
			Path:         "/v1/chat/completions",
			StatusCode:   200,
			Duration:     100 * time.Millisecond,
		})

		req := httptest.NewRequest("GET", "/api/traffic/logs", nil)
		w := httptest.NewRecorder()

		h.handleTrafficLogs(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var resp struct {
			Success bool                   `json:"success"`
			Data    map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !resp.Success {
			t.Error("Expected success=true")
		}

		total, ok := resp.Data["total"].(float64)
		if !ok || int(total) != 1 {
			t.Errorf("Expected 1 log, got %v", resp.Data["total"])
		}
	})

	t.Run("ClearLogs", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/traffic/clear", nil)
		w := httptest.NewRecorder()

		h.handleTrafficClear(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var resp struct {
			Success bool                   `json:"success"`
			Data    map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !resp.Success {
			t.Error("Expected success=true")
		}

		if len(recorder.GetLogs(nil)) != 0 {
			t.Error("Logs should be cleared")
		}
	})
}
