package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestMigrationAssignsLegacyDataToDefaultUserOne(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}

	legacySchema := `
	CREATE TABLE endpoints (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		api_url TEXT NOT NULL,
		api_key TEXT NOT NULL,
		auth_mode TEXT NOT NULL DEFAULT 'api_key',
		enabled BOOLEAN DEFAULT TRUE,
		transformer TEXT DEFAULT 'claude',
		model TEXT,
		remark TEXT,
		request_overrides TEXT,
		sort_order INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE daily_stats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		endpoint_name TEXT NOT NULL,
		date TEXT NOT NULL,
		requests INTEGER DEFAULT 0,
		errors INTEGER DEFAULT 0,
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		device_id TEXT DEFAULT 'default',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE endpoint_credentials (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		endpoint_name TEXT NOT NULL,
		provider_type TEXT NOT NULL DEFAULT 'codex',
		account_id TEXT,
		email TEXT,
		access_token TEXT NOT NULL,
		refresh_token TEXT,
		id_token TEXT,
		last_refresh DATETIME,
		expires_at DATETIME,
		status TEXT NOT NULL DEFAULT 'active',
		enabled BOOLEAN DEFAULT TRUE,
		failure_count INTEGER DEFAULT 0,
		cooldown_until DATETIME,
		last_checked_at DATETIME,
		last_used_at DATETIME,
		last_error TEXT,
		remark TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE app_config (
		key TEXT PRIMARY KEY,
		value TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := legacyDB.Exec(legacySchema); err != nil {
		legacyDB.Close()
		t.Fatalf("create legacy schema: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO endpoints (name, api_url, api_key, enabled, transformer, sort_order) VALUES ('legacy-endpoint', 'https://legacy', 'legacy-key', 1, 'claude', 1)`); err != nil {
		legacyDB.Close()
		t.Fatalf("insert legacy endpoint: %v", err)
	}
	today := time.Now().Format("2006-01-02")
	if _, err := legacyDB.Exec(`INSERT INTO daily_stats (endpoint_name, date, requests, errors, input_tokens, output_tokens, device_id) VALUES ('legacy-endpoint', ?, 3, 1, 10, 20, 'legacy-device')`, today); err != nil {
		legacyDB.Close()
		t.Fatalf("insert legacy stats: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO endpoint_credentials (endpoint_name, provider_type, access_token, status, enabled) VALUES ('legacy-endpoint', 'codex', 'legacy-token', 'active', 1)`); err != nil {
		legacyDB.Close()
		t.Fatalf("insert legacy credential: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	s, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("open migrated storage: %v", err)
	}
	defer s.Close()

	defaultUser, err := s.GetUserByID(1)
	if err != nil {
		t.Fatalf("get default user: %v", err)
	}
	if defaultUser == nil {
		t.Fatalf("expected default user id=1 to exist")
	}
	if defaultUser.Username != "default" {
		t.Fatalf("expected default username, got %+v", defaultUser)
	}

	endpoints, err := s.GetEndpointsByUser(1)
	if err != nil {
		t.Fatalf("get migrated endpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Name != "legacy-endpoint" {
		t.Fatalf("expected legacy endpoint on user 1, got %+v", endpoints)
	}

	stats, err := s.GetPeriodStatsAggregatedForUser(1, today, today)
	if err != nil {
		t.Fatalf("get migrated stats: %v", err)
	}
	entry, ok := stats["legacy-endpoint"]
	if !ok {
		t.Fatalf("expected legacy stats on user 1, got %+v", stats)
	}
	if entry.Requests != 3 || entry.Errors != 1 {
		t.Fatalf("unexpected migrated stats entry: %+v", entry)
	}

	credentials, err := s.GetEndpointCredentialsByUser(1, "legacy-endpoint")
	if err != nil {
		t.Fatalf("get migrated credentials: %v", err)
	}
	if len(credentials) != 1 || credentials[0].AccessToken != "legacy-token" {
		t.Fatalf("expected legacy credential on user 1, got %+v", credentials)
	}
}
