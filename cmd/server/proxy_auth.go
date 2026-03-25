package main

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func proxyAuthMiddleware(token string) func(http.Handler) http.Handler {
	trimmedToken := strings.TrimSpace(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if trimmedToken == "" || !requiresProxyAuth(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			if !isValidProxyToken(r, trimmedToken) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"unauthorized","type":"auth_error"}}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func requiresProxyAuth(path string) bool {
	switch {
	case strings.HasPrefix(path, "/v1/"):
		return true
	case strings.HasPrefix(path, "/chat/completions"):
		return true
	case strings.HasPrefix(path, "/responses"):
		return true
	case strings.HasPrefix(path, "/cursor/"):
		return true
	default:
		return false
	}
}

func isValidProxyToken(r *http.Request, token string) bool {
	if r == nil || strings.TrimSpace(token) == "" {
		return false
	}

	if bearer := bearerTokenFromHeader(r.Header.Get("Authorization")); bearer != "" {
		return subtle.ConstantTimeCompare([]byte(token), []byte(bearer)) == 1
	}

	apiToken := strings.TrimSpace(r.Header.Get("X-API-Token"))
	if apiToken != "" {
		return subtle.ConstantTimeCompare([]byte(token), []byte(apiToken)) == 1
	}

	return false
}

func bearerTokenFromHeader(authHeader string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(authHeader, prefix))
}
