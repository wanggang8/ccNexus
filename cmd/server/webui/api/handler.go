package api

import (
	"net/http"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/proxy"
	"github.com/lich0821/ccNexus/internal/storage"
)

// Handler handles API requests
type Handler struct {
	config  *config.Config
	proxy   *proxy.Proxy
	storage *storage.SQLiteStorage
	uiToken string // when set, all API/UI requests require this token
}

// NewHandler creates a new API handler
func NewHandler(cfg *config.Config, p *proxy.Proxy, s *storage.SQLiteStorage, uiToken string) *Handler {
	return &Handler{
		config:  cfg,
		proxy:   p,
		storage: s,
		uiToken: uiToken,
	}
}

// authWrap wraps a handler to require UI token when configured
func (h *Handler) authWrap(fn func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.uiToken != "" && getTokenFromRequest(r) != h.uiToken {
			WriteError(w, http.StatusUnauthorized, "Invalid or missing API token")
			return
		}
		fn(w, r)
	}
}

// RegisterRoutes registers all API routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Auth (no token required)
	mux.HandleFunc("/api/auth/status", h.handleAuthStatus)
	mux.HandleFunc("/api/auth/verify", h.handleAuthVerify)

	// Endpoint management
	mux.HandleFunc("/api/endpoints", h.authWrap(h.handleEndpoints))
	mux.HandleFunc("/api/endpoints/", h.authWrap(h.handleEndpointByName))
	mux.HandleFunc("/api/endpoints/current", h.authWrap(h.handleCurrentEndpoint))
	mux.HandleFunc("/api/endpoints/switch", h.authWrap(h.handleSwitchEndpoint))
	mux.HandleFunc("/api/endpoints/reorder", h.authWrap(h.handleReorderEndpoints))
	mux.HandleFunc("/api/endpoints/fetch-models", h.authWrap(h.handleFetchModels))

	// Statistics
	mux.HandleFunc("/api/stats/summary", h.authWrap(h.handleStatsSummary))
	mux.HandleFunc("/api/stats/daily", h.authWrap(h.handleStatsDaily))
	mux.HandleFunc("/api/stats/weekly", h.authWrap(h.handleStatsWeekly))
	mux.HandleFunc("/api/stats/monthly", h.authWrap(h.handleStatsMonthly))
	mux.HandleFunc("/api/stats/trends", h.authWrap(h.handleStatsTrends))

	// Configuration
	mux.HandleFunc("/api/config", h.authWrap(h.handleConfig))
	mux.HandleFunc("/api/config/port", h.authWrap(h.handleConfigPort))
	mux.HandleFunc("/api/config/log-level", h.authWrap(h.handleConfigLogLevel))

	// Real-time events
	mux.HandleFunc("/api/events", h.authWrap(h.handleEvents))

	// Traffic logs
	mux.HandleFunc("/api/traffic/logs", h.authWrap(h.handleTrafficLogs))
	mux.HandleFunc("/api/traffic/recording", h.authWrap(h.handleTrafficRecording))
	mux.HandleFunc("/api/traffic/clear", h.authWrap(h.handleTrafficClear))
}
