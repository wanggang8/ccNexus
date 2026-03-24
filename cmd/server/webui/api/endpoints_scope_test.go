package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/storage"
)

func TestListEndpointsUsesUserScopedTokenPoolStats(t *testing.T) {
	s, err := storage.NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	userA, err := s.EnsureUser("user-a", "token-a", "user")
	if err != nil {
		t.Fatalf("ensure user a: %v", err)
	}
	userB, err := s.EnsureUser("user-b", "token-b", "user")
	if err != nil {
		t.Fatalf("ensure user b: %v", err)
	}

	if err := s.SaveEndpointForUser(userA.ID, &storage.Endpoint{Name: "shared", APIUrl: "https://a.example.com", APIKey: "a", AuthMode: "token_pool", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save endpoint user a: %v", err)
	}
	if err := s.SaveEndpointForUser(userB.ID, &storage.Endpoint{Name: "shared", APIUrl: "https://b.example.com", APIKey: "b", AuthMode: "token_pool", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save endpoint user b: %v", err)
	}
	if err := s.SaveEndpointCredentialForUser(userA.ID, &storage.EndpointCredential{EndpointName: "shared", ProviderType: "codex", AccessToken: "a-1", Status: "active", Enabled: true}); err != nil {
		t.Fatalf("save credential user a: %v", err)
	}
	if err := s.SaveEndpointCredentialForUser(userB.ID, &storage.EndpointCredential{EndpointName: "shared", ProviderType: "codex", AccessToken: "b-1", Status: "active", Enabled: true}); err != nil {
		t.Fatalf("save credential user b first: %v", err)
	}
	if err := s.SaveEndpointCredentialForUser(userB.ID, &storage.EndpointCredential{EndpointName: "shared", ProviderType: "codex", AccessToken: "b-2", Status: "active", Enabled: true}); err != nil {
		t.Fatalf("save credential user b second: %v", err)
	}

	h := 		NewHandler(&config.Config{}, nil, s)
	req := httptest.NewRequest(http.MethodGet, "/api/endpoints", nil)
	req.Header.Set("Authorization", "Bearer token-a")
	w := httptest.NewRecorder()

	h.authWrap(h.handleEndpoints)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Data struct {
			TokenPools map[string]storage.TokenPoolStats `json:"tokenPools"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	stats := resp.Data.TokenPools["shared"]
	if stats.Total != 1 {
		t.Fatalf("expected scoped token pool total 1, got %+v", stats)
	}
}
