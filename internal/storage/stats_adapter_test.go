package storage

import (
	"testing"
	"time"
)

func TestStatsStorageAdapterScopesStatsToUser(t *testing.T) {
	s, err := NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	user1, err := s.EnsureUser("user-a", "token-a", "user")
	if err != nil {
		t.Fatalf("ensure user1: %v", err)
	}
	user2, err := s.EnsureUser("user-b", "token-b", "user")
	if err != nil {
		t.Fatalf("ensure user2: %v", err)
	}

	adapter1 := NewStatsStorageAdapterForUser(s, user1.ID)
	adapter2 := NewStatsStorageAdapterForUser(s, user2.ID)
	today := time.Now().Format("2006-01-02")

	if err := adapter1.RecordDailyStat(map[string]interface{}{
		"UserID":       float64(user1.ID),
		"EndpointName": "ep-a",
		"Date":         today,
		"Requests":     2,
		"Errors":       1,
		"InputTokens":  10,
		"OutputTokens": 20,
		"DeviceID":     "d1",
	}); err != nil {
		t.Fatalf("record user1 stat: %v", err)
	}
	if err := adapter2.RecordDailyStat(map[string]interface{}{
		"UserID":       float64(user2.ID),
		"EndpointName": "ep-b",
		"Date":         today,
		"Requests":     5,
		"Errors":       0,
		"InputTokens":  30,
		"OutputTokens": 40,
		"DeviceID":     "d2",
	}); err != nil {
		t.Fatalf("record user2 stat: %v", err)
	}

	total1, stats1, err := adapter1.GetTotalStats()
	if err != nil {
		t.Fatalf("get user1 total stats: %v", err)
	}
	if total1 != 2 {
		t.Fatalf("expected user1 total=2, got %d", total1)
	}
	if len(stats1) != 1 {
		t.Fatalf("expected user1 one endpoint, got %+v", stats1)
	}
	if _, ok := stats1["ep-a"]; !ok {
		t.Fatalf("expected ep-a in user1 stats, got %+v", stats1)
	}
	if _, ok := stats1["ep-b"]; ok {
		t.Fatalf("did not expect ep-b in user1 stats, got %+v", stats1)
	}

	period2, err := adapter2.GetPeriodStatsAggregated(today, today)
	if err != nil {
		t.Fatalf("get user2 period stats: %v", err)
	}
	if len(period2) != 1 {
		t.Fatalf("expected user2 one endpoint, got %+v", period2)
	}
	if _, ok := period2["ep-b"]; !ok {
		t.Fatalf("expected ep-b in user2 period stats, got %+v", period2)
	}
}
