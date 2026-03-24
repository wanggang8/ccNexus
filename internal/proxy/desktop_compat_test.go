package proxy

import (
	"testing"
	"time"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/storage"
)

func TestDesktopProxyDefaultMethodsUseUserOne(t *testing.T) {
	s, err := storage.NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	user2, err := s.EnsureUser("user-b", "token-b", "user")
	if err != nil {
		t.Fatalf("ensure user2: %v", err)
	}

	cfg := &config.Config{
		Endpoints: []config.Endpoint{
			{Name: "pool-default", Enabled: true, AuthMode: config.AuthModeTokenPool},
			{Name: "pool-second", Enabled: true, AuthMode: config.AuthModeTokenPool},
		},
	}
	if err := s.SaveEndpoint(&storage.Endpoint{Name: "pool-default", APIUrl: "https://u1-default", APIKey: "k1", AuthMode: config.AuthModeTokenPool, Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save user1 endpoint default: %v", err)
	}
	if err := s.SaveEndpoint(&storage.Endpoint{Name: "pool-second", APIUrl: "https://u1-second", APIKey: "k2", AuthMode: config.AuthModeTokenPool, Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save user1 endpoint second: %v", err)
	}
	if err := s.SaveEndpointForUser(user2.ID, &storage.Endpoint{Name: "pool-default", APIUrl: "https://u2-default", APIKey: "k3", AuthMode: config.AuthModeTokenPool, Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save user2 endpoint default: %v", err)
	}
	if err := s.SaveEndpointForUser(user2.ID, &storage.Endpoint{Name: "pool-second", APIUrl: "https://u2-second", APIKey: "k4", AuthMode: config.AuthModeTokenPool, Enabled: true, Transformer: "claude"}); err != nil {
		t.Fatalf("save user2 endpoint second: %v", err)
	}
	p := New(cfg, storage.NewStatsStorageAdapter(s), s, "test-device")
	p.currentIndex = 0

	if err := s.SaveEndpointCredential(&storage.EndpointCredential{EndpointName: "pool-default", ProviderType: "codex", AccessToken: "token-u1", Enabled: true}); err != nil {
		t.Fatalf("save user1 credential: %v", err)
	}
	if err := s.SaveEndpointCredentialForUser(user2.ID, &storage.EndpointCredential{EndpointName: "pool-default", ProviderType: "codex", AccessToken: "token-u2", Enabled: true}); err != nil {
		t.Fatalf("save user2 credential: %v", err)
	}
	if err := s.SaveEndpointCredential(&storage.EndpointCredential{EndpointName: "pool-second", ProviderType: "codex", AccessToken: "token-u1-second", Enabled: true}); err != nil {
		t.Fatalf("save user1 second credential: %v", err)
	}
	if err := s.SaveEndpointCredentialForUser(user2.ID, &storage.EndpointCredential{EndpointName: "pool-second", ProviderType: "codex", AccessToken: "token-u2-second", Enabled: true}); err != nil {
		t.Fatalf("save user2 second credential: %v", err)
	}

	cred, err := p.selectCredential("pool-default")
	if err != nil {
		t.Fatalf("select default credential: %v", err)
	}
	if cred == nil || cred.AccessToken != "token-u1" {
		t.Fatalf("expected user1 credential, got %+v", cred)
	}

	maxRetries := p.computeMaxRetries(cfg.Endpoints)
	if maxRetries != 4 {
		t.Fatalf("expected base retries 4 with one credential per endpoint for user1, got %d", maxRetries)
	}

	next := p.rotateEndpoint()
	if next.Name != "pool-second" {
		t.Fatalf("expected rotate to pool-second, got %s", next.Name)
	}
}

func TestDesktopProxyStatsDefaultMethodsUseUserOne(t *testing.T) {
	s, err := storage.NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	stats := NewStats(storage.NewStatsStorageAdapter(s), "desktop-device")
	stats.RecordRequest("desktop-u1")
	stats.RecordError("desktop-u1")
	stats.RecordTokens("desktop-u1", 12, 34)

	today := time.Now().Format("2006-01-02")
	period, err := s.GetPeriodStatsAggregated(today, today)
	if err != nil {
		t.Fatalf("get period stats: %v", err)
	}
	entry, ok := period["desktop-u1"]
	if !ok {
		t.Fatalf("expected desktop-u1 stats, got %+v", period)
	}
	if entry.Requests != 1 || entry.Errors != 1 || entry.InputTokens != 12 || entry.OutputTokens != 34 {
		t.Fatalf("unexpected stats entry: %+v", entry)
	}
}
