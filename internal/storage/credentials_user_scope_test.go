package storage

import (
	"testing"
	"time"
)

func TestCredentialIsolationByUser(t *testing.T) {
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

	ep := &Endpoint{Name: "shared", APIUrl: "https://example.com/v1/messages", APIKey: "k", Enabled: true, Transformer: "claude"}
	if err := s.SaveEndpointForUser(user1.ID, ep); err != nil {
		t.Fatalf("save endpoint user1: %v", err)
	}
	if err := s.SaveEndpointForUser(user2.ID, ep); err != nil {
		t.Fatalf("save endpoint user2: %v", err)
	}

	cred1 := &EndpointCredential{EndpointName: "shared", ProviderType: "codex", AccessToken: "token-1", Enabled: true}
	if err := s.SaveEndpointCredentialForUser(user1.ID, cred1); err != nil {
		t.Fatalf("save credential user1: %v", err)
	}
	cred2 := &EndpointCredential{EndpointName: "shared", ProviderType: "codex", AccessToken: "token-2", Enabled: true}
	if err := s.SaveEndpointCredentialForUser(user2.ID, cred2); err != nil {
		t.Fatalf("save credential user2: %v", err)
	}

	list1, err := s.GetEndpointCredentialsByUser(user1.ID, "shared")
	if err != nil {
		t.Fatalf("list user1 credentials: %v", err)
	}
	if len(list1) != 1 || list1[0].AccessToken != "token-1" {
		t.Fatalf("unexpected user1 credentials: %+v", list1)
	}

	list2, err := s.GetEndpointCredentialsByUser(user2.ID, "shared")
	if err != nil {
		t.Fatalf("list user2 credentials: %v", err)
	}
	if len(list2) != 1 || list2[0].AccessToken != "token-2" {
		t.Fatalf("unexpected user2 credentials: %+v", list2)
	}

	usable1, err := s.GetUsableEndpointCredentialForUser(user1.ID, "shared", time.Now().UTC())
	if err != nil {
		t.Fatalf("usable user1 credential: %v", err)
	}
	if usable1 == nil || usable1.AccessToken != "token-1" {
		t.Fatalf("unexpected user1 usable credential: %+v", usable1)
	}
}
