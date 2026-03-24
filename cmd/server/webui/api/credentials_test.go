package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/storage"
)

func TestListEndpointCredentialsFiltersByCurrentUserAndMasksTokens(t *testing.T) {
	s, err := storage.NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	userA, err := s.EnsureUser("user-a", "token-a", "user")
	if err != nil {
		t.Fatalf("ensure userA: %v", err)
	}
	userB, err := s.EnsureUser("user-b", "token-b", "user")
	if err != nil {
		t.Fatalf("ensure userB: %v", err)
	}

	if err := s.SaveEndpointForUser(userA.ID, &storage.Endpoint{Name: "shared", APIUrl: "https://a", APIKey: "a", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save endpoint A: %v", err)
	}
	if err := s.SaveEndpointForUser(userB.ID, &storage.Endpoint{Name: "shared", APIUrl: "https://b", APIKey: "b", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save endpoint B: %v", err)
	}
	credA := &storage.EndpointCredential{EndpointName: "shared", ProviderType: "codex", AccessToken: "token-user-a-123456", RefreshToken: "refresh-user-a-123456", IDToken: "id-user-a-123456", Enabled: true}
	if err := s.SaveEndpointCredentialForUser(userA.ID, credA); err != nil {
		t.Fatalf("save credential A: %v", err)
	}
	credB := &storage.EndpointCredential{EndpointName: "shared", ProviderType: "codex", AccessToken: "token-user-b-123456", Enabled: true}
	if err := s.SaveEndpointCredentialForUser(userB.ID, credB); err != nil {
		t.Fatalf("save credential B: %v", err)
	}
	if err := s.UpsertCredentialRateLimits(credA.ID, &storage.CodexRateLimitsData{Source: "user-a"}, "ok", "", credA.CreatedAt); err != nil {
		t.Fatalf("upsert rate limit A: %v", err)
	}
	if err := s.UpsertCredentialRateLimits(credB.ID, &storage.CodexRateLimitsData{Source: "user-b"}, "ok", "", credB.CreatedAt); err != nil {
		t.Fatalf("upsert rate limit B: %v", err)
	}

	h := NewHandler(&config.Config{}, nil, s)
	req := httptest.NewRequest(http.MethodGet, "/api/endpoints/shared/credentials", nil)
	req = req.WithContext(context.WithValue(req.Context(), currentUserContextKey, userA))
	w := httptest.NewRecorder()

	h.listEndpointCredentials(w, req, "shared")

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
	items := resp.Data["credentials"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(items))
	}
	cred := items[0].(map[string]interface{})
	if cred["accessToken"] == "token-user-a-123456" {
		t.Fatalf("expected masked access token, got %+v", cred)
	}
	if cred["accessToken"] == "token-user-b-123456" {
		t.Fatalf("expected current user credential only, got %+v", cred)
	}
	rateLimits, ok := cred["rateLimits"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected scoped rate limits on credential, got %+v", cred)
	}
	data, ok := rateLimits["data"].(map[string]interface{})
	if !ok || data["source"] != "user-a" {
		t.Fatalf("expected userA rate limits only, got %+v", rateLimits)
	}
}

func TestImportEndpointCredentialsCreatesOnlyForCurrentUser(t *testing.T) {
	s, err := storage.NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	userA, err := s.EnsureUser("user-a", "token-a", "user")
	if err != nil {
		t.Fatalf("ensure userA: %v", err)
	}
	userB, err := s.EnsureUser("user-b", "token-b", "user")
	if err != nil {
		t.Fatalf("ensure userB: %v", err)
	}

	if err := s.SaveEndpointForUser(userA.ID, &storage.Endpoint{Name: "shared", APIUrl: "https://a", APIKey: "a", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save endpoint A: %v", err)
	}
	if err := s.SaveEndpointForUser(userB.ID, &storage.Endpoint{Name: "shared", APIUrl: "https://b", APIKey: "b", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save endpoint B: %v", err)
	}

	h := NewHandler(&config.Config{}, nil, s)
	body := bytes.NewBufferString(`[{"access_token":"new-token-a","account_id":"acct-a"}]`)
	req := httptest.NewRequest(http.MethodPost, "/api/endpoints/shared/credentials", body)
	req = req.WithContext(context.WithValue(req.Context(), currentUserContextKey, userA))
	w := httptest.NewRecorder()

	h.importEndpointCredentials(w, req, "shared")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	listA, err := s.GetEndpointCredentialsByUser(userA.ID, "shared")
	if err != nil {
		t.Fatalf("get userA credentials: %v", err)
	}
	if len(listA) != 1 || listA[0].AccessToken != "new-token-a" {
		t.Fatalf("unexpected userA credentials: %+v", listA)
	}
	listB, err := s.GetEndpointCredentialsByUser(userB.ID, "shared")
	if err != nil {
		t.Fatalf("get userB credentials: %v", err)
	}
	if len(listB) != 0 {
		t.Fatalf("expected userB credentials untouched, got %+v", listB)
	}
}

func TestDeleteEndpointCredentialDoesNotDeleteOtherUsersCredential(t *testing.T) {
	s, err := storage.NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	userA, err := s.EnsureUser("user-a", "token-a", "user")
	if err != nil {
		t.Fatalf("ensure userA: %v", err)
	}
	userB, err := s.EnsureUser("user-b", "token-b", "user")
	if err != nil {
		t.Fatalf("ensure userB: %v", err)
	}

	if err := s.SaveEndpointForUser(userA.ID, &storage.Endpoint{Name: "shared", APIUrl: "https://a", APIKey: "a", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save endpoint A: %v", err)
	}
	if err := s.SaveEndpointForUser(userB.ID, &storage.Endpoint{Name: "shared", APIUrl: "https://b", APIKey: "b", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save endpoint B: %v", err)
	}
	credB := &storage.EndpointCredential{EndpointName: "shared", ProviderType: "codex", AccessToken: "token-user-b", Enabled: true}
	if err := s.SaveEndpointCredentialForUser(userB.ID, credB); err != nil {
		t.Fatalf("save credential B: %v", err)
	}

	h := NewHandler(&config.Config{}, nil, s)
	req := httptest.NewRequest(http.MethodDelete, "/api/endpoints/shared/credentials", nil)
	req = req.WithContext(context.WithValue(req.Context(), currentUserContextKey, userA))
	w := httptest.NewRecorder()

	h.deleteEndpointCredential(w, req, "shared", credB.ID)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when deleting other user's credential, got %d body=%s", w.Code, w.Body.String())
	}
	listB, err := s.GetEndpointCredentialsByUser(userB.ID, "shared")
	if err != nil {
		t.Fatalf("get userB credentials: %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("expected userB credential preserved, got %+v", listB)
	}
}
