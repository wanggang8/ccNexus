package api

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "testing"

    "github.com/lich0821/ccNexus/internal/config"
    "github.com/lich0821/ccNexus/internal/storage"
)

func setupRevealKeyHandler(t *testing.T) (*Handler, func()) {
    t.Helper()

    dbPath := filepath.Join(t.TempDir(), "test.db")
    sqliteStorage, err := storage.NewSQLiteStorage(dbPath)
    if err != nil {
        t.Fatalf("failed to create sqlite storage: %v", err)
    }

    cfg := config.DefaultConfig()
    cfg.BasicAuthEnabled = false

    handler := NewHandler(cfg, nil, sqliteStorage)
    return handler, func() {
        _ = sqliteStorage.Close()
    }
}

func TestRevealEndpointKey(t *testing.T) {
    handler, cleanup := setupRevealKeyHandler(t)
    defer cleanup()

    endpoint := &storage.Endpoint{
        Name:        "primary",
        APIUrl:      "https://example.com",
        APIKey:      "secret-key",
        AuthMode:    config.AuthModeAPIKey,
        Enabled:     true,
        Transformer: "openai",
    }

    if err := handler.storage.SaveEndpoint(endpoint); err != nil {
        t.Fatalf("failed to save endpoint: %v", err)
    }

    req := httptest.NewRequest(http.MethodPost, "/api/endpoints/primary/reveal-key", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d", rec.Code)
    }

    var response SuccessResponse
    if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
        t.Fatalf("failed to decode response: %v", err)
    }

    if !response.Success {
        t.Fatalf("expected success response")
    }

    data, ok := response.Data.(map[string]interface{})
    if !ok {
        t.Fatalf("expected data object, got %T", response.Data)
    }

    if data["apiKey"] != "secret-key" {
        t.Fatalf("expected apiKey to be secret-key, got %v", data["apiKey"])
    }
}

func TestRevealEndpointKeyNotFound(t *testing.T) {
    handler, cleanup := setupRevealKeyHandler(t)
    defer cleanup()

    req := httptest.NewRequest(http.MethodPost, "/api/endpoints/missing/reveal-key", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    if rec.Code != http.StatusNotFound {
        t.Fatalf("expected 404, got %d", rec.Code)
    }

    var response ErrorResponse
    if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
        t.Fatalf("failed to decode response: %v", err)
    }

    if response.Error == "" {
        t.Fatalf("expected error message")
    }
}
