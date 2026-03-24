package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lich0821/ccNexus/internal/storage"
)

type contextKey string

const currentUserContextKey contextKey = "currentUser"

func (h *Handler) currentUser(r *http.Request) *storage.User {
	if r == nil {
		return nil
	}
	if user, ok := r.Context().Value(currentUserContextKey).(*storage.User); ok {
		return user
	}
	return nil
}

func (h *Handler) requireAdmin(r *http.Request) (*storage.User, error) {
	user := h.currentUser(r)
	if user == nil {
		return nil, http.ErrNoCookie
	}
	if user.Role != "admin" {
		return nil, http.ErrNotSupported
	}
	return user, nil
}

// handleAuthStatus returns whether token auth is required (no auth needed for this endpoint)
func (h *Handler) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{"authRequired": true})
}

// handleAuthVerify verifies the token and returns the current user (no auth needed for this endpoint)
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
	user, err := h.storage.GetUserByToken(strings.TrimSpace(body.Token))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to verify token")
		return
	}
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "Invalid token")
		return
	}
	WriteSuccess(w, map[string]interface{}{
		"success":  true,
		"user": map[string]interface{}{
			"id":                 user.ID,
			"username":           user.Username,
			"role":               user.Role,
			"currentEndpointName": user.CurrentEndpointName,
		},
	})
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

func (h *Handler) authWrap(fn func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(getTokenFromRequest(r))
		if token == "" {
			WriteError(w, http.StatusUnauthorized, "Invalid or missing API token")
			return
		}
		user, err := h.storage.GetUserByToken(token)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "Failed to resolve current user")
			return
		}
		if user == nil {
			WriteError(w, http.StatusUnauthorized, "Invalid or missing API token")
			return
		}
		ctx := context.WithValue(r.Context(), currentUserContextKey, user)
		fn(w, r.WithContext(ctx))
	}
}
