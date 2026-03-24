package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/storage"
)

func TestUsersAPIRequiresAdminAndManagesUsers(t *testing.T) {
	s, err := storage.NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	if err := s.SyncDefaultUserToken("admin-token"); err != nil {
		t.Fatalf("sync admin token: %v", err)
	}
	admin, err := s.GetUserByToken("admin-token")
	if err != nil {
		t.Fatalf("get admin by token: %v", err)
	}
	member, err := s.CreateUser("member", "member-token", "user")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	h := 		NewHandler(&config.Config{}, nil, s)

	t.Run("non-admin forbidden on list users", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		req = req.WithContext(context.WithValue(req.Context(), currentUserContextKey, member))
		w := httptest.NewRecorder()

		h.handleUsers(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("admin can list users", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		req = req.WithContext(context.WithValue(req.Context(), currentUserContextKey, admin))
		w := httptest.NewRecorder()

		h.handleUsers(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Success bool                   `json:"success"`
			Data    map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		users := resp.Data["users"].([]interface{})
		if len(users) < 2 {
			t.Fatalf("expected at least 2 users, got %+v", users)
		}
	})

	t.Run("admin can create user", func(t *testing.T) {
		body := bytes.NewBufferString(`{"username":"alice","role":"user"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/users", body)
		req = req.WithContext(context.WithValue(req.Context(), currentUserContextKey, admin))
		w := httptest.NewRecorder()

		h.handleUsers(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Success bool                   `json:"success"`
			Data    map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		token, ok := resp.Data["token"].(string)
		if !ok || token == "" {
			t.Fatalf("expected token in response, got %+v", resp.Data)
		}
		resolved, err := s.GetUserByToken(token)
		if err != nil {
			t.Fatalf("get created user by token: %v", err)
		}
		if resolved == nil || resolved.Username != "alice" {
			t.Fatalf("expected created user alice, got %+v", resolved)
		}
	})

	t.Run("admin can reset token and disable user", func(t *testing.T) {
		resetReq := httptest.NewRequest(http.MethodPost, "/api/users/2/reset-token", nil)
		resetReq = resetReq.WithContext(context.WithValue(resetReq.Context(), currentUserContextKey, admin))
		resetW := httptest.NewRecorder()

		h.handleUserByID(resetW, resetReq)

		if resetW.Code != http.StatusOK {
			t.Fatalf("expected 200 on reset token, got %d body=%s", resetW.Code, resetW.Body.String())
		}
		var resetResp struct {
			Success bool                   `json:"success"`
			Data    map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(resetW.Body).Decode(&resetResp); err != nil {
			t.Fatalf("decode reset response: %v", err)
		}
		newToken := resetResp.Data["token"].(string)
		userByToken, err := s.GetUserByToken(newToken)
		if err != nil {
			t.Fatalf("get user by reset token: %v", err)
		}
		if userByToken == nil || userByToken.ID != member.ID {
			t.Fatalf("expected reset token to resolve member, got %+v", userByToken)
		}

		statusBody := bytes.NewBufferString(`{"status":"disabled"}`)
		statusReq := httptest.NewRequest(http.MethodPatch, "/api/users/2/status", statusBody)
		statusReq = statusReq.WithContext(context.WithValue(statusReq.Context(), currentUserContextKey, admin))
		statusW := httptest.NewRecorder()

		h.handleUserByID(statusW, statusReq)

		if statusW.Code != http.StatusOK {
			t.Fatalf("expected 200 on disable, got %d body=%s", statusW.Code, statusW.Body.String())
		}
		disabled, err := s.GetUserByToken(newToken)
		if err != nil {
			t.Fatalf("get disabled user by token: %v", err)
		}
		if disabled != nil {
			t.Fatalf("expected disabled user token invalid, got %+v", disabled)
		}
	})
}

func TestHandleAuthStatusAlwaysRequiresToken(t *testing.T) {
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

func TestHandleEventsUsesScopedStats(t *testing.T) {
	s, err := storage.NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	admin, err := s.EnsureUser("admin2", "admin2-token", "admin")
	if err != nil {
		t.Fatalf("ensure admin: %v", err)
	}
	other, err := s.EnsureUser("other", "other-token", "user")
	if err != nil {
		t.Fatalf("ensure other: %v", err)
	}
	if err := s.RecordDailyStatForUser(admin.ID, &storage.DailyStat{EndpointName: "admin-ep", Date: "2026-03-24", Requests: 3, Errors: 1, InputTokens: 10, OutputTokens: 20, DeviceID: "d1"}); err != nil {
		t.Fatalf("record admin stats: %v", err)
	}
	if err := s.RecordDailyStatForUser(other.ID, &storage.DailyStat{EndpointName: "other-ep", Date: "2026-03-24", Requests: 9, Errors: 4, InputTokens: 30, OutputTokens: 40, DeviceID: "d2"}); err != nil {
		t.Fatalf("record other stats: %v", err)
	}

	h := 		NewHandler(&config.Config{}, nil, s)
	baseReq := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	ctx, cancel := context.WithTimeout(baseReq.Context(), 5200*time.Millisecond)
	defer cancel()
	req := baseReq.WithContext(context.WithValue(ctx, currentUserContextKey, admin))
	w := httptest.NewRecorder()

	h.handleEvents(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "admin-ep") {
		t.Fatalf("expected scoped event payload to include admin endpoint, body=%s", body)
	}
	if strings.Contains(body, "other-ep") {
		t.Fatalf("expected scoped event payload to exclude other user endpoint, body=%s", body)
	}
}
