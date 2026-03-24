package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/lich0821/ccNexus/internal/logger"
)

func generateUserToken() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "ccnx_" + hex.EncodeToString(buf), nil
}

func (h *Handler) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listUsers(w, r)
	case http.MethodPost:
		h.createUser(w, r)
	default:
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *Handler) handleUserByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/users/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		WriteError(w, http.StatusBadRequest, "User id required")
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	subpath := ""
	if len(parts) > 1 {
		subpath = parts[1]
	}

	if subpath == "reset-token" {
		if r.Method != http.MethodPost {
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		h.resetUserToken(w, r, id)
		return
	}

	if subpath == "status" || subpath == "" {
		if r.Method != http.MethodPatch {
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		h.updateUserStatus(w, r, id)
		return
	}

	WriteError(w, http.StatusNotFound, "Not found")
}

type userResponse struct {
	ID                  int64  `json:"id"`
	Username            string `json:"username"`
	Role                string `json:"role"`
	Status              string `json:"status"`
	CurrentEndpointName string `json:"currentEndpointName,omitempty"`
	LastUsedAt          string `json:"lastUsedAt,omitempty"`
	CreatedAt           string `json:"createdAt,omitempty"`
	UpdatedAt           string `json:"updatedAt,omitempty"`
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	_, err := h.requireAdmin(r)
	if err != nil {
		if err == http.ErrNoCookie {
			WriteError(w, http.StatusUnauthorized, "Current user not found")
			return
		}
		WriteError(w, http.StatusForbidden, "Admin access required")
		return
	}
	users, err := h.storage.ListUsers()
	if err != nil {
		logger.Error("Failed to list users: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to list users")
		return
	}
	resp := make([]userResponse, 0, len(users))
	for _, user := range users {
		item := userResponse{ID: user.ID, Username: user.Username, Role: user.Role, Status: user.Status, CurrentEndpointName: user.CurrentEndpointName}
		if !user.LastUsedAt.IsZero() {
			item.LastUsedAt = user.LastUsedAt.UTC().Format(http.TimeFormat)
		}
		if !user.CreatedAt.IsZero() {
			item.CreatedAt = user.CreatedAt.UTC().Format(http.TimeFormat)
		}
		if !user.UpdatedAt.IsZero() {
			item.UpdatedAt = user.UpdatedAt.UTC().Format(http.TimeFormat)
		}
		resp = append(resp, item)
	}
	WriteSuccess(w, map[string]interface{}{"users": resp})
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	_, err := h.requireAdmin(r)
	if err != nil {
		if err == http.ErrNoCookie {
			WriteError(w, http.StatusUnauthorized, "Current user not found")
			return
		}
		WriteError(w, http.StatusForbidden, "Admin access required")
		return
	}
	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		WriteError(w, http.StatusBadRequest, "username is required")
		return
	}
	token, err := generateUserToken()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}
	user, err := h.storage.CreateUser(req.Username, token, req.Role)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "exists") {
			WriteError(w, http.StatusConflict, err.Error())
			return
		}
		logger.Error("Failed to create user: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}
	item := userResponse{ID: user.ID, Username: user.Username, Role: user.Role, Status: user.Status, CurrentEndpointName: user.CurrentEndpointName}
	if !user.CreatedAt.IsZero() {
		item.CreatedAt = user.CreatedAt.UTC().Format(http.TimeFormat)
	}
	if !user.UpdatedAt.IsZero() {
		item.UpdatedAt = user.UpdatedAt.UTC().Format(http.TimeFormat)
	}
	WriteSuccess(w, map[string]interface{}{"user": item, "token": token})
}

func (h *Handler) resetUserToken(w http.ResponseWriter, r *http.Request, id int64) {
	_, err := h.requireAdmin(r)
	if err != nil {
		if err == http.ErrNoCookie {
			WriteError(w, http.StatusUnauthorized, "Current user not found")
			return
		}
		WriteError(w, http.StatusForbidden, "Admin access required")
		return
	}
	token, err := generateUserToken()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}
	user, err := h.storage.RotateUserToken(id, token)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			WriteError(w, http.StatusNotFound, "User not found")
			return
		}
		logger.Error("Failed to rotate user token: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to reset user token")
		return
	}
	item := userResponse{ID: user.ID, Username: user.Username, Role: user.Role, Status: user.Status, CurrentEndpointName: user.CurrentEndpointName}
	if !user.UpdatedAt.IsZero() {
		item.UpdatedAt = user.UpdatedAt.UTC().Format(http.TimeFormat)
	}
	WriteSuccess(w, map[string]interface{}{"user": item, "token": token})
}

func (h *Handler) updateUserStatus(w http.ResponseWriter, r *http.Request, id int64) {
	_, err := h.requireAdmin(r)
	if err != nil {
		if err == http.ErrNoCookie {
			WriteError(w, http.StatusUnauthorized, "Current user not found")
			return
		}
		WriteError(w, http.StatusForbidden, "Admin access required")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := h.storage.UpdateUserStatus(id, req.Status); err != nil {
		lower := strings.ToLower(err.Error())
		switch {
		case strings.Contains(lower, "cannot be disabled"):
			WriteError(w, http.StatusBadRequest, err.Error())
		case strings.Contains(lower, "not found"):
			WriteError(w, http.StatusNotFound, "User not found")
		default:
			logger.Error("Failed to update user status: %v", err)
			WriteError(w, http.StatusInternalServerError, "Failed to update user status")
		}
		return
	}
	user, err := h.storage.GetUserByID(id)
	if err != nil {
		logger.Error("Failed to reload user: %v", err)
		WriteError(w, http.StatusInternalServerError, "Failed to reload user")
		return
	}
	if user == nil {
		WriteError(w, http.StatusNotFound, "User not found")
		return
	}
	item := userResponse{ID: user.ID, Username: user.Username, Role: user.Role, Status: user.Status, CurrentEndpointName: user.CurrentEndpointName}
	if !user.LastUsedAt.IsZero() {
		item.LastUsedAt = user.LastUsedAt.UTC().Format(http.TimeFormat)
	}
	if !user.CreatedAt.IsZero() {
		item.CreatedAt = user.CreatedAt.UTC().Format(http.TimeFormat)
	}
	if !user.UpdatedAt.IsZero() {
		item.UpdatedAt = user.UpdatedAt.UTC().Format(http.TimeFormat)
	}
	WriteSuccess(w, map[string]interface{}{"user": item})
}
