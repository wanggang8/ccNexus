package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxyAuthMiddlewareProtectsProxyRoutes(t *testing.T) {
	handler := proxyAuthMiddleware("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := proxyAuthMiddleware("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := proxyAuthMiddleware("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := proxyAuthMiddleware("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected non-proxy routes to bypass proxy auth, got %d", rec.Code)
	}
}
