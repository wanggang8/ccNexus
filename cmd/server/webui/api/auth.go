package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleAuthStatus returns whether UI token auth is required (no auth needed for this endpoint)
func (h *Handler) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	authRequired := h.uiToken != ""
	WriteJSON(w, http.StatusOK, map[string]interface{}{"authRequired": authRequired})
}

// handleAuthVerify verifies the token and returns success (no auth needed for this endpoint)
func (h *Handler) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if h.uiToken == "" || body.Token != h.uiToken {
		WriteError(w, http.StatusUnauthorized, "Invalid token")
		return
	}
	WriteSuccess(w, map[string]interface{}{"success": true})
}

// getTokenFromRequest extracts token from Authorization header, X-API-Token header, or query param
func getTokenFromRequest(r *http.Request) string {
	if s := r.Header.Get("Authorization"); s != "" {
		if strings.HasPrefix(s, "Bearer ") {
			return strings.TrimSpace(s[7:])
		}
	}
	if s := r.Header.Get("X-API-Token"); s != "" {
		return strings.TrimSpace(s)
	}
	return r.URL.Query().Get("token")
}
