package main

import (
	"net/http"
	"strings"

	"github.com/lich0821/ccNexus/internal/logger"
)

var allowedExactPaths = map[string]struct{}{
	"/":                        {},
	"/v1/messages":             {},
	"/v1/chat/completions":      {},
	"/v1/responses":            {},
	"/chat-stream":             {},
	"/v1/messages/count_tokens": {},
	"/health":                  {},
	"/stats":                   {},

	"/admin": {},
	"/ui":    {},

	"/api/auth/status":        {},
	"/api/auth/verify":        {},
	"/api/endpoints":          {},
	"/api/endpoints/current":  {},
	"/api/endpoints/switch":   {},
	"/api/endpoints/reorder":  {},
	"/api/endpoints/fetch-models": {},
	"/api/stats/summary":      {},
	"/api/stats/daily":        {},
	"/api/stats/weekly":       {},
	"/api/stats/monthly":      {},
	"/api/stats/trends":       {},
	"/api/config":             {},
	"/api/config/port":        {},
	"/api/config/log-level":   {},
	"/api/users":              {},
	"/api/events":             {},
	"/api/traffic/logs":       {},
	"/api/traffic/recording":  {},
	"/api/traffic/clear":      {},
}

var allowedPrefixPaths = []string{
	"/ui/",
	"/api/endpoints/",
	"/api/users/",
}

// routeAllowlistMiddleware blocks unsupported paths before they can reach the proxy chain.
// It only permits routes that are explicitly registered by the main server process.
func routeAllowlistMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAllowedPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		logger.Warn("Blocked unsupported request: %s %s from %s (ua=%q)",
			r.Method, r.URL.Path, r.RemoteAddr, r.Header.Get("User-Agent"))
		http.NotFound(w, r)
	})
}

func isAllowedPath(path string) bool {
	if _, ok := allowedExactPaths[path]; ok {
		return true
	}

	for _, prefix := range allowedPrefixPaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	return false
}