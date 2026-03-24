package storage

import (
	"testing"
	"time"

	"github.com/lich0821/ccNexus/internal/config"
)

func TestDesktopDefaultConfigAdapterUsesUserOne(t *testing.T) {
	s, err := NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	user2, err := s.EnsureUser("user-b", "token-b", "user")
	if err != nil {
		t.Fatalf("ensure user2: %v", err)
	}

	if err := s.SaveEndpoint(&Endpoint{Name: "desktop-default", APIUrl: "https://u1", APIKey: "k1", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save user1 endpoint: %v", err)
	}
	if err := s.SaveEndpointForUser(user2.ID, &Endpoint{Name: "server-user2", APIUrl: "https://u2", APIKey: "k2", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save user2 endpoint: %v", err)
	}

	adapter := NewConfigStorageAdapter(s)
	endpoints, err := adapter.GetEndpoints()
	if err != nil {
		t.Fatalf("adapter get endpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Name != "desktop-default" {
		t.Fatalf("expected only user1 endpoint, got %+v", endpoints)
	}

	if err := adapter.SaveEndpoint(&config.StorageEndpoint{Name: "desktop-saved", APIUrl: "https://save", APIKey: "k3", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("adapter save endpoint: %v", err)
	}
	user1Endpoints, err := s.GetEndpointsByUser(1)
	if err != nil {
		t.Fatalf("get user1 endpoints: %v", err)
	}
	if len(user1Endpoints) != 2 {
		t.Fatalf("expected 2 user1 endpoints, got %+v", user1Endpoints)
	}
	user2Endpoints, err := s.GetEndpointsByUser(user2.ID)
	if err != nil {
		t.Fatalf("get user2 endpoints: %v", err)
	}
	if len(user2Endpoints) != 1 || user2Endpoints[0].Name != "server-user2" {
		t.Fatalf("expected user2 untouched, got %+v", user2Endpoints)
	}
}

func TestDesktopLegacyStorageMethodsUseUserOne(t *testing.T) {
	s, err := NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	user2, err := s.EnsureUser("user-b", "token-b", "user")
	if err != nil {
		t.Fatalf("ensure user2: %v", err)
	}

	if err := s.SaveEndpoint(&Endpoint{Name: "legacy-u1", APIUrl: "https://u1", APIKey: "k1", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save user1 endpoint: %v", err)
	}
	if err := s.SaveEndpointForUser(user2.ID, &Endpoint{Name: "legacy-u2", APIUrl: "https://u2", APIKey: "k2", Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save user2 endpoint: %v", err)
	}

	endpoints, err := s.GetEndpoints()
	if err != nil {
		t.Fatalf("get endpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Name != "legacy-u1" {
		t.Fatalf("expected only user1 endpoints, got %+v", endpoints)
	}

	now := time.Now().UTC()
	if err := s.SaveEndpointCredential(&EndpointCredential{EndpointName: "legacy-u1", ProviderType: "codex", AccessToken: "token-u1", Enabled: true}); err != nil {
		t.Fatalf("save user1 credential: %v", err)
	}
	if err := s.SaveEndpointCredentialForUser(user2.ID, &EndpointCredential{EndpointName: "legacy-u2", ProviderType: "codex", AccessToken: "token-u2", Enabled: true}); err != nil {
		t.Fatalf("save user2 credential: %v", err)
	}

	creds, err := s.GetEndpointCredentials("legacy-u1")
	if err != nil {
		t.Fatalf("get user1 credentials: %v", err)
	}
	if len(creds) != 1 || creds[0].AccessToken != "token-u1" {
		t.Fatalf("expected only user1 credential, got %+v", creds)
	}
	usable, err := s.GetUsableEndpointCredential("legacy-u1", now)
	if err != nil {
		t.Fatalf("get usable credential: %v", err)
	}
	if usable == nil || usable.AccessToken != "token-u1" {
		t.Fatalf("expected usable user1 credential, got %+v", usable)
	}

	today := now.Format("2006-01-02")
	if err := s.RecordDailyStat(&DailyStat{EndpointName: "legacy-u1", Date: today, Requests: 3, Errors: 1, InputTokens: 10, OutputTokens: 20, DeviceID: "d1"}); err != nil {
		t.Fatalf("record user1 stat: %v", err)
	}
	if err := s.RecordDailyStatForUser(user2.ID, &DailyStat{EndpointName: "legacy-u2", Date: today, Requests: 7, Errors: 2, InputTokens: 30, OutputTokens: 40, DeviceID: "d2"}); err != nil {
		t.Fatalf("record user2 stat: %v", err)
	}

	total, stats, err := s.GetTotalStats()
	if err != nil {
		t.Fatalf("get total stats: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected user1 total requests 3, got %d", total)
	}
	if len(stats) != 1 {
		t.Fatalf("expected one user1 stats entry, got %+v", stats)
	}
	if _, ok := stats["legacy-u1"]; !ok {
		t.Fatalf("expected legacy-u1 in stats, got %+v", stats)
	}

	period, err := s.GetPeriodStatsAggregated(today, today)
	if err != nil {
		t.Fatalf("get period stats: %v", err)
	}
	if len(period) != 1 {
		t.Fatalf("expected one user1 period entry, got %+v", period)
	}
	if _, ok := period["legacy-u1"]; !ok {
		t.Fatalf("expected legacy-u1 in period stats, got %+v", period)
	}

	poolStats, err := s.GetTokenPoolStats("legacy-u1")
	if err != nil {
		t.Fatalf("get token pool stats: %v", err)
	}
	if poolStats.Total != 1 || poolStats.Active != 1 {
		t.Fatalf("expected user1 token pool stats, got %+v", poolStats)
	}

	credList, err := s.GetEndpointCredentialsByUser(user2.ID, "legacy-u2")
	if err != nil {
		t.Fatalf("get user2 credentials: %v", err)
	}
	if len(credList) != 1 {
		t.Fatalf("expected user2 credential preserved, got %+v", credList)
	}
}

func TestDesktopDefaultStatsAdapterUsesUserOne(t *testing.T) {
	s, err := NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	user2, err := s.EnsureUser("user-b", "token-b", "user")
	if err != nil {
		t.Fatalf("ensure user2: %v", err)
	}

	adapter := NewStatsStorageAdapter(s)
	today := time.Now().Format("2006-01-02")

	if err := adapter.RecordDailyStat(map[string]interface{}{
		"EndpointName": "desktop-stats-u1",
		"Date":         today,
		"Requests":     2,
		"Errors":       1,
		"InputTokens":  10,
		"OutputTokens": 20,
		"DeviceID":     "d1",
	}); err != nil {
		t.Fatalf("record default user stat: %v", err)
	}
	if err := s.RecordDailyStatForUser(user2.ID, &DailyStat{EndpointName: "desktop-stats-u2", Date: today, Requests: 9, Errors: 0, InputTokens: 30, OutputTokens: 40, DeviceID: "d2"}); err != nil {
		t.Fatalf("record user2 stat: %v", err)
	}

	total, stats, err := adapter.GetTotalStats()
	if err != nil {
		t.Fatalf("get total stats: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected default user total=2, got %d", total)
	}
	if len(stats) != 1 {
		t.Fatalf("expected one default user stat entry, got %+v", stats)
	}
	if _, ok := stats["desktop-stats-u1"]; !ok {
		t.Fatalf("expected desktop-stats-u1 in stats, got %+v", stats)
	}

	period, err := adapter.GetPeriodStatsAggregated(today, today)
	if err != nil {
		t.Fatalf("get period stats: %v", err)
	}
	if len(period) != 1 {
		t.Fatalf("expected one default user period entry, got %+v", period)
	}
	if _, ok := period["desktop-stats-u1"]; !ok {
		t.Fatalf("expected desktop-stats-u1 in period stats, got %+v", period)
	}
}
