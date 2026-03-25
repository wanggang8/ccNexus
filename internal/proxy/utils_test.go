package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
)

func TestCleanIncompleteToolCallsIgnoresTruncatedJSON(t *testing.T) {
	body := []byte(`{"messages":[`)

	cleaned, err := cleanIncompleteToolCalls(body)
	if err != nil {
		t.Fatalf("expected truncated JSON to be ignored, got error: %v", err)
	}
	if string(cleaned) != string(body) {
		t.Fatalf("expected original body to pass through unchanged")
	}
}

func TestHandleProxyRejectsEmptyBodyEarly(t *testing.T) {
	p := New(config.DefaultConfig(), nil, nil, "test-device")

	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/chat/completions", strings.NewReader(""))
	rec := httptest.NewRecorder()
	p.handleProxy(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty body, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Empty request body") {
		t.Fatalf("expected empty body message, got %s", rec.Body.String())
	}
}

func TestHandleProxyAnswersOptionsEarly(t *testing.T) {
	p := New(config.DefaultConfig(), nil, nil, "test-device")

	req := httptest.NewRequest(http.MethodOptions, "http://localhost/cursor/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	p.handleProxy(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS, got %d", rec.Code)
	}
	if allowMethods := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(allowMethods, "OPTIONS") {
		t.Fatalf("expected CORS headers on OPTIONS response, got %q", allowMethods)
	}
}
