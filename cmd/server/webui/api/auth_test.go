package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/storage"
)

func TestHandleAuthStatus(t *testing.T) {
	h := 		NewHandler(&config.Config{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	w := httptest.NewRecorder()

	h.handleAuthStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp["authRequired"] {
		t.Fatalf("expected authRequired=true, got %+v", resp)
	}
}

func TestHandleAuthVerifyAndAuthWrap(t *testing.T) {
	s, err := storage.NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	user, err := s.EnsureUser("api-user", "secret-token", "user")
	if err != nil {
		t.Fatalf("ensure user: %v", err)
	}

	h := 		NewHandler(&config.Config{}, nil, s)

	t.Run("verify returns user for valid token", func(t *testing.T) {
		body := bytes.NewBufferString(`{"token":"secret-token"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/verify", body)
		w := httptest.NewRecorder()

		h.handleAuthVerify(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp struct {
			Success bool                   `json:"success"`
			Data    map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !resp.Success {
			t.Fatalf("expected success=true")
		}
		userData, ok := resp.Data["user"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected user payload, got %+v", resp.Data)
		}
		if userData["username"] != user.Username {
			t.Fatalf("expected username %s, got %+v", user.Username, userData)
		}
	})

	t.Run("verify rejects invalid token", func(t *testing.T) {
		body := bytes.NewBufferString(`{"token":"bad-token"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/verify", body)
		w := httptest.NewRecorder()

		h.handleAuthVerify(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	t.Run("verify rejects invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/verify", bytes.NewBufferString(`{"token":`))
		w := httptest.NewRecorder()

		h.handleAuthVerify(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("auth wrap resolves bearer token", func(t *testing.T) {
		var gotUser string
		wrapped := h.authWrap(func(w http.ResponseWriter, r *http.Request) {
			current := h.currentUser(r)
			if current != nil {
				gotUser = current.Username
			}
			w.WriteHeader(http.StatusNoContent)
		})

		req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
		req.Header.Set("Authorization", "Bearer secret-token")
		w := httptest.NewRecorder()

		wrapped(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", w.Code)
		}
		if gotUser != user.Username {
			t.Fatalf("expected user %s, got %s", user.Username, gotUser)
		}
	})

	t.Run("auth wrap rejects missing token", func(t *testing.T) {
		wrapped := h.authWrap(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
		w := httptest.NewRecorder()

		wrapped(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})
}
