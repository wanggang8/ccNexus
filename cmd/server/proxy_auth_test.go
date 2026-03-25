package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
)

func TestProxyAuthMiddlewareProtectsProxyRoutes(t *testing.T) {
	cfg := &config.Config{
		BasicAuthEnabled:  true,
		BasicAuthPassword: "secret",
	}
	handler := proxyAuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}

func TestProxyAuthMiddlewareAcceptsBearerToken(t *testing.T) {
	cfg := &config.Config{
		BasicAuthEnabled:  true,
		BasicAuthPassword: "secret",
	}
	handler := proxyAuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected next handler for valid bearer token, got %d", rec.Code)
	}
}

func TestProxyAuthMiddlewareAcceptsXAPIToken(t *testing.T) {
	cfg := &config.Config{
		BasicAuthEnabled:  true,
		BasicAuthPassword: "secret",
	}
	handler := proxyAuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/responses", nil)
	req.Header.Set("X-API-Token", "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected next handler for valid X-API-Token, got %d", rec.Code)
	}
}

func TestProxyAuthMiddlewareSkipsNonProxyRoutes(t *testing.T) {
	cfg := &config.Config{
		BasicAuthEnabled:  true,
		BasicAuthPassword: "secret",
	}
	handler := proxyAuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected non-proxy routes to bypass proxy auth, got %d", rec.Code)
	}
}

func TestProxyAuthMiddlewareReadsUpdatedPasswordDynamically(t *testing.T) {
	cfg := &config.Config{
		BasicAuthEnabled:  true,
		BasicAuthPassword: "secret",
	}
	handler := proxyAuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cfg.BasicAuthPassword = "rotated"

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer rotated")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected rotated token to be accepted, got %d", rec.Code)
	}
}

func TestProxyAuthMiddlewareSkipsProxyRoutesWhenBasicAuthIsDisabled(t *testing.T) {
	cfg := &config.Config{
		BasicAuthEnabled:  false,
		BasicAuthPassword: "shared-secret",
	}
	handler := proxyAuthMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected proxy route to bypass auth when basic auth is disabled, got %d", rec.Code)
	}
}
