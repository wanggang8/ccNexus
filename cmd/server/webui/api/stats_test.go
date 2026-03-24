package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/proxy"
	"github.com/lich0821/ccNexus/internal/storage"
)

func TestHandleStatsSummaryFiltersByCurrentUser(t *testing.T) {
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

	if err := s.SaveEndpointForUser(userA.ID, &storage.Endpoint{Name: "ep-a", APIUrl: "https://a", APIKey: "a", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save endpoint A: %v", err)
	}
	if err := s.SaveEndpointForUser(userB.ID, &storage.Endpoint{Name: "ep-b", APIUrl: "https://b", APIKey: "b", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save endpoint B: %v", err)
	}

	cfg := &config.Config{}
	p := proxy.New(cfg, storage.NewStatsStorageAdapter(s), s, "test-device", false)
	p.GetStats().RecordRequestForUser(userA.ID, "ep-a")
	p.GetStats().RecordTokensForUser(userA.ID, "ep-a", 10, 20)
	p.GetStats().RecordRequestForUser(userB.ID, "ep-b")
	p.GetStats().RecordTokensForUser(userB.ID, "ep-b", 30, 40)

	h := NewHandler(&config.Config{}, nil, s)
	req := httptest.NewRequest(http.MethodGet, "/api/stats/summary", nil)
	req = req.WithContext(context.WithValue(req.Context(), currentUserContextKey, userA))
	w := httptest.NewRecorder()

	h.handleStatsSummary(w, req)

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
	if got := int(resp.Data["TotalRequests"].(float64)); got != 1 {
		t.Fatalf("expected total requests 1, got %d", got)
	}
	endpoints := resp.Data["Endpoints"].(map[string]interface{})
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
	if _, ok := endpoints["ep-a"]; !ok {
		t.Fatalf("expected ep-a in response, got %+v", endpoints)
	}
	if _, ok := endpoints["ep-b"]; ok {
		t.Fatalf("did not expect ep-b in response, got %+v", endpoints)
	}
}

func TestGetStatsForPeriodUsesScopedUserStats(t *testing.T) {
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
	if err := s.SaveEndpointForUser(userA.ID, &storage.Endpoint{Name: "shared-a", APIUrl: "https://a", APIKey: "a", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save endpoint A: %v", err)
	}
	if err := s.SaveEndpointForUser(userB.ID, &storage.Endpoint{Name: "shared-b", APIUrl: "https://b", APIKey: "b", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save endpoint B: %v", err)
	}

	today := time.Now().Format("2006-01-02")
	if err := s.RecordDailyStatForUser(userA.ID, &storage.DailyStat{EndpointName: "shared-a", Date: today, Requests: 2, InputTokens: 11, OutputTokens: 22, DeviceID: "d1"}); err != nil {
		t.Fatalf("record userA stat: %v", err)
	}
	if err := s.RecordDailyStatForUser(userB.ID, &storage.DailyStat{EndpointName: "shared-b", Date: today, Requests: 5, InputTokens: 33, OutputTokens: 44, DeviceID: "d2"}); err != nil {
		t.Fatalf("record userB stat: %v", err)
	}

	h := NewHandler(&config.Config{}, nil, s)
	req := httptest.NewRequest(http.MethodGet, "/api/stats/daily", nil)
	req = req.WithContext(context.WithValue(req.Context(), currentUserContextKey, userA))

	stats, err := h.getStatsForPeriod(req, today, today)
	if err != nil {
		t.Fatalf("getStatsForPeriod: %v", err)
	}
	if stats["totalRequests"].(int) != 2 {
		t.Fatalf("expected totalRequests 2, got %+v", stats)
	}
	endpoints := stats["endpoints"].(map[string]interface{})
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %+v", endpoints)
	}
	if _, ok := endpoints["shared-a"]; !ok {
		t.Fatalf("expected shared-a in endpoints, got %+v", endpoints)
	}
}
