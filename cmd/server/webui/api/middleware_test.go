package api

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
)

func TestDynamicBasicAuthMiddlewareUsesUpdatedPassword(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.BasicAuthEnabled = true
	cfg.BasicAuthUsername = "admin"
	cfg.BasicAuthPassword = "oldpass"

	handler := DynamicBasicAuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cfg.BasicAuthPassword = "newpass"

	req := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("admin:newpass")))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected dynamic middleware to accept updated password, got %d", rec.Code)
	}
}
