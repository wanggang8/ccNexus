package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/storage"
)

func TestTestEndpointRespectsCurrentUserScope(t *testing.T) {
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

	if err := s.SaveEndpointForUser(userA.ID, &storage.Endpoint{Name: "user-a-only", APIUrl: "https://a", APIKey: "a", Enabled: true, Transformer: "passthrough"}); err != nil {
		t.Fatalf("save endpoint userA: %v", err)
	}
	if err := s.SaveEndpointForUser(userB.ID, &storage.Endpoint{Name: "user-b-only", APIUrl: "https://b", APIKey: "b", Enabled: true, Transformer: "passthrough"}); err != nil {
		t.Fatalf("save endpoint userB: %v", err)
	}

	h := 		NewHandler(&config.Config{}, nil, s)
	req := httptest.NewRequest(http.MethodPost, "/api/endpoints/user-b-only/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), currentUserContextKey, userA))
	w := httptest.NewRecorder()

	h.testEndpoint(w, req, "user-b-only")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user endpoint, got %d body=%s", w.Code, w.Body.String())
	}
}
