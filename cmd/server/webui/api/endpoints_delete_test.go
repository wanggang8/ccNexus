package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/storage"
)

func setupDeleteHandler(t *testing.T) (*Handler, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqliteStorage, err := storage.NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite storage: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.BasicAuthEnabled = false
	adapter := storage.NewConfigStorageAdapter(sqliteStorage)
	if err := cfg.SaveToStorage(adapter); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	handler := NewHandler(cfg, nil, sqliteStorage)
	return handler, func() { _ = sqliteStorage.Close() }
}

func TestDeleteEndpoint_WithSlashInName(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	name := "http://8.219.58.50:18081/"

	endpoint := &storage.Endpoint{
		Name:        name,
		APIUrl:      "https://example.com",
		APIKey:      "secret-key",
		AuthMode:    config.AuthModeAPIKey,
		Enabled:     true,
		Transformer: "openai",
	}
	if err := handler.storage.SaveEndpoint(endpoint); err != nil {
		t.Fatalf("failed to save endpoint: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/endpoints/"+url.PathEscape(name), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	endpoints, err := handler.storage.GetEndpoints()
	if err != nil {
		t.Fatalf("failed to query endpoints: %v", err)
	}
	for _, ep := range endpoints {
		if ep.Name == name {
			t.Fatalf("endpoint should be deleted but still exists: %s", name)
		}
	}
}

func TestDeleteEndpoint_WithSlashInNameViaQueryOnServeMux(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	name := "http://8.219.58.50:18081/"

	endpoint := &storage.Endpoint{
		Name:        name,
		APIUrl:      "https://example.com",
		APIKey:      "secret-key",
		AuthMode:    config.AuthModeAPIKey,
		Enabled:     true,
		Transformer: "openai",
	}
	if err := handler.storage.SaveEndpoint(endpoint); err != nil {
		t.Fatalf("failed to save endpoint: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handler.ServeHTTP)

	req := httptest.NewRequest(http.MethodDelete, "/api/endpoints?name="+url.QueryEscape(name), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	endpoints, err := handler.storage.GetEndpoints()
	if err != nil {
		t.Fatalf("failed to query endpoints: %v", err)
	}
	for _, ep := range endpoints {
		if ep.Name == name {
			t.Fatalf("endpoint should be deleted but still exists: %s", name)
		}
	}
}

func TestEndpointNameQuery_PUTGETToggleViaServeMux(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	name := "http://8.219.58.50:18081/"

	endpoint := &storage.Endpoint{
		Name:        name,
		APIUrl:      "https://example.com",
		APIKey:      "secret-key",
		AuthMode:    config.AuthModeAPIKey,
		Enabled:     true,
		Transformer: "openai",
		Remark:      "a",
	}
	if err := handler.storage.SaveEndpoint(endpoint); err != nil {
		t.Fatalf("failed to save endpoint: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handler.ServeHTTP)

	q := "/api/endpoints?name=" + url.QueryEscape(name)

	putBody := `{"enabled":true,"remark":"via-query"}`
	putReq := httptest.NewRequest(http.MethodPut, q, strings.NewReader(putBody))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	mux.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d body=%s", putRec.Code, putRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, q, nil)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d body=%s", getRec.Code, getRec.Body.String())
	}

	toggleReq := httptest.NewRequest(http.MethodPatch, "/api/endpoints/toggle?name="+url.QueryEscape(name), strings.NewReader(`{"enabled":false}`))
	toggleReq.Header.Set("Content-Type", "application/json")
	toggleRec := httptest.NewRecorder()
	mux.ServeHTTP(toggleRec, toggleReq)
	if toggleRec.Code != http.StatusOK {
		t.Fatalf("toggle expected 200, got %d body=%s", toggleRec.Code, toggleRec.Body.String())
	}

	endpoints, err := handler.storage.GetEndpoints()
	if err != nil {
		t.Fatalf("get endpoints: %v", err)
	}
	var found *storage.Endpoint
	for i := range endpoints {
		if endpoints[i].Name == name {
			found = &endpoints[i]
			break
		}
	}
	if found == nil {
		t.Fatal("endpoint missing after ops")
	}
	if found.Remark != "via-query" {
		t.Fatalf("remark=%q want via-query", found.Remark)
	}
	if found.Enabled {
		t.Fatalf("expected disabled after toggle")
	}
}

// Endpoint named "toggle" must still be reachable at GET /api/endpoints/toggle (no ?name=), not shadowed by toggle API.
func TestReservedPath_EndpointNamedToggle_GETWithoutQuery(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	name := "toggle"
	ep := &storage.Endpoint{
		Name:        name,
		APIUrl:      "https://example.com",
		APIKey:      "k",
		AuthMode:    config.AuthModeAPIKey,
		Enabled:     true,
		Transformer: "openai",
	}
	if err := handler.storage.SaveEndpoint(ep); err != nil {
		t.Fatalf("save: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handler.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/api/endpoints/toggle", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/endpoints/toggle want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestQueryCredentials_MissingEndpointReturnsNotFound(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handler.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/api/endpoints/credentials?name=missing", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestQueryCredentialsImport_MissingEndpointDoesNotCreateOrphans(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handler.ServeHTTP)

	body := strings.NewReader(`{"items":[{"access_token":"tok","account_id":"acct-1"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/endpoints/credentials/import?name=missing", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}

	creds, err := handler.storage.GetEndpointCredentials("missing")
	if err != nil {
		t.Fatalf("get credentials: %v", err)
	}
	if len(creds) != 0 {
		t.Fatalf("expected no orphan credentials, got %d", len(creds))
	}
}

func TestQueryEndpointName_PreservesLeadingAndTrailingSpaces(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	name := "  spaced endpoint  "
	endpoint := &storage.Endpoint{
		Name:        name,
		APIUrl:      "https://example.com",
		APIKey:      "secret-key",
		AuthMode:    config.AuthModeAPIKey,
		Enabled:     true,
		Transformer: "openai",
	}
	if err := handler.storage.SaveEndpoint(endpoint); err != nil {
		t.Fatalf("save endpoint: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handler.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/api/endpoints?name="+url.QueryEscape(name), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEndpointNameQuery_TestRevealAndCredentialsViaServeMux(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	name := "http://8.219.58.50:18081/"
	endpoint := &storage.Endpoint{
		Name:        name,
		APIUrl:      "https://example.com",
		APIKey:      "secret-key",
		AuthMode:    config.AuthModeAPIKey,
		Enabled:     true,
		Transformer: "unknown-transformer",
	}
	if err := handler.storage.SaveEndpoint(endpoint); err != nil {
		t.Fatalf("save endpoint: %v", err)
	}
	cred := &storage.EndpointCredential{
		EndpointName: name,
		ProviderType: "codex",
		AccessToken:  "access-token",
		AccountID:    "acct-1",
		Enabled:      true,
	}
	if err := handler.storage.SaveEndpointCredential(cred); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handler.ServeHTTP)

	revealReq := httptest.NewRequest(http.MethodPost, "/api/endpoints/reveal-key?name="+url.QueryEscape(name), nil)
	revealRec := httptest.NewRecorder()
	mux.ServeHTTP(revealRec, revealReq)
	if revealRec.Code != http.StatusOK {
		t.Fatalf("reveal expected 200, got %d body=%s", revealRec.Code, revealRec.Body.String())
	}

	credListReq := httptest.NewRequest(http.MethodGet, "/api/endpoints/credentials?name="+url.QueryEscape(name), nil)
	credListRec := httptest.NewRecorder()
	mux.ServeHTTP(credListRec, credListReq)
	if credListRec.Code != http.StatusOK {
		t.Fatalf("credentials list expected 200, got %d body=%s", credListRec.Code, credListRec.Body.String())
	}

	statsReq := httptest.NewRequest(http.MethodGet, "/api/endpoints/credentials/stats?name="+url.QueryEscape(name), nil)
	statsRec := httptest.NewRecorder()
	mux.ServeHTTP(statsRec, statsReq)
	if statsRec.Code != http.StatusOK {
		t.Fatalf("credentials stats expected 200, got %d body=%s", statsRec.Code, statsRec.Body.String())
	}

	testReq := httptest.NewRequest(http.MethodPost, "/api/endpoints/test?name="+url.QueryEscape(name), nil)
	testRec := httptest.NewRecorder()
	mux.ServeHTTP(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Fatalf("test expected 200, got %d body=%s", testRec.Code, testRec.Body.String())
	}
	if !strings.Contains(testRec.Body.String(), `"success":false`) {
		t.Fatalf("test response should still be a handled API test response, body=%s", testRec.Body.String())
	}
}

func TestEndpointNameQuery_CredentialIDRoutesViaServeMux(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	name := "http://8.219.58.50:18081/"
	endpoint := &storage.Endpoint{
		Name:        name,
		APIUrl:      "https://example.com",
		APIKey:      "secret-key",
		AuthMode:    config.AuthModeAPIKey,
		Enabled:     true,
		Transformer: "openai",
	}
	if err := handler.storage.SaveEndpoint(endpoint); err != nil {
		t.Fatalf("save endpoint: %v", err)
	}
	cred := &storage.EndpointCredential{
		EndpointName: name,
		ProviderType: "codex",
		AccessToken:  "access-token",
		AccountID:    "acct-1",
		Enabled:      true,
	}
	if err := handler.storage.SaveEndpointCredential(cred); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handler.ServeHTTP)

	credID := cred.ID
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/endpoints/credentials/"+url.QueryEscape(strconv.FormatInt(credID, 10))+"?name="+url.QueryEscape(name), strings.NewReader(`{"enabled":false}`))
	patchReq.Header.Set("Content-Type", "application/json")
	patchRec := httptest.NewRecorder()
	mux.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("credential patch expected 200, got %d body=%s", patchRec.Code, patchRec.Body.String())
	}

	updated, err := handler.storage.GetCredentialByID(credID)
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	if updated == nil || updated.Enabled {
		t.Fatalf("credential should be disabled after patch")
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/endpoints/credentials/"+url.QueryEscape(strconv.FormatInt(credID, 10))+"?name="+url.QueryEscape(name), nil)
	deleteRec := httptest.NewRecorder()
	mux.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("credential delete expected 200, got %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	deleted, err := handler.storage.GetCredentialByID(credID)
	if err != nil {
		t.Fatalf("get credential after delete: %v", err)
	}
	if deleted != nil {
		t.Fatalf("credential should be deleted")
	}
}

func TestReservedPath_EndpointNamedRevealKeyAndCredentials_GETWithoutQuery(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	endpoints := []*storage.Endpoint{
		{
			Name:        "reveal-key",
			APIUrl:      "https://example.com",
			APIKey:      "key-1",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai",
		},
		{
			Name:        "credentials",
			APIUrl:      "https://example.com",
			APIKey:      "key-2",
			AuthMode:    config.AuthModeAPIKey,
			Enabled:     true,
			Transformer: "openai",
		},
	}
	for _, ep := range endpoints {
		if err := handler.storage.SaveEndpoint(ep); err != nil {
			t.Fatalf("save endpoint %s: %v", ep.Name, err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handler.ServeHTTP)

	for _, path := range []string{"/api/endpoints/reveal-key", "/api/endpoints/credentials"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s want 200, got %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestLegacyPath_EndpointNameEndingWithTestSuffix_PrefersExactEndpoint(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	name := "foo/test"
	endpoint := &storage.Endpoint{
		Name:        name,
		APIUrl:      "https://example.com",
		APIKey:      "secret-key",
		AuthMode:    config.AuthModeAPIKey,
		Enabled:     true,
		Transformer: "openai",
	}
	if err := handler.storage.SaveEndpoint(endpoint); err != nil {
		t.Fatalf("save endpoint: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handler.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/api/endpoints/"+url.PathEscape(name), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLegacyPath_EndpointNameEndingWithRevealKey_AllowsDelete(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	name := "foo/reveal-key"
	endpoint := &storage.Endpoint{
		Name:        name,
		APIUrl:      "https://example.com",
		APIKey:      "secret-key",
		AuthMode:    config.AuthModeAPIKey,
		Enabled:     true,
		Transformer: "openai",
	}
	if err := handler.storage.SaveEndpoint(endpoint); err != nil {
		t.Fatalf("save endpoint: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handler.ServeHTTP)

	req := httptest.NewRequest(http.MethodDelete, "/api/endpoints/"+url.PathEscape(name), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLegacyPath_EndpointNameContainingCredentials_PrefersExactEndpoint(t *testing.T) {
	handler, cleanup := setupDeleteHandler(t)
	defer cleanup()

	name := "foo/credentials/bar"
	endpoint := &storage.Endpoint{
		Name:        name,
		APIUrl:      "https://example.com",
		APIKey:      "secret-key",
		AuthMode:    config.AuthModeAPIKey,
		Enabled:     true,
		Transformer: "openai",
	}
	if err := handler.storage.SaveEndpoint(endpoint); err != nil {
		t.Fatalf("save endpoint: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/", handler.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/api/endpoints/"+url.PathEscape(name), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}
