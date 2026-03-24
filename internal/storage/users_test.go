package storage

import (
	"testing"
)

func TestUserAdminStorageOperations(t *testing.T) {
	s, err := NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	defaultUser, err := s.GetUserByID(1)
	if err != nil {
		t.Fatalf("get default user: %v", err)
	}
	if defaultUser == nil || defaultUser.Role != "admin" {
		t.Fatalf("expected default admin user, got %+v", defaultUser)
	}
	if defaultUser.TokenHash != defaultAdminToken {
		t.Fatalf("expected built-in default admin token, got %+v", defaultUser)
	}

	created, err := s.CreateUser("alice", "token-alice", "user")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.Username != "alice" || created.Role != "user" || created.Status != "active" {
		t.Fatalf("unexpected created user: %+v", created)
	}

	list, err := s.ListUsers()
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("expected at least 2 users, got %+v", list)
	}

	rotated, err := s.RotateUserToken(created.ID, "token-alice-new")
	if err != nil {
		t.Fatalf("rotate user token: %v", err)
	}
	if rotated == nil {
		t.Fatalf("expected rotated user result")
	}
	userByToken, err := s.GetUserByToken("token-alice-new")
	if err != nil {
		t.Fatalf("get user by new token: %v", err)
	}
	if userByToken == nil || userByToken.ID != created.ID {
		t.Fatalf("expected new token to work, got %+v", userByToken)
	}

	if err := s.UpdateUserStatus(created.ID, "disabled"); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	disabledUser, err := s.GetUserByToken("token-alice-new")
	if err != nil {
		t.Fatalf("get disabled user by token: %v", err)
	}
	if disabledUser != nil {
		t.Fatalf("expected disabled user token to stop working, got %+v", disabledUser)
	}

	if err := s.UpdateUserStatus(1, "disabled"); err == nil {
		t.Fatalf("expected disabling default admin to fail")
	}

	if err := s.SyncDefaultUserToken("env-admin-token"); err != nil {
		t.Fatalf("sync default user token: %v", err)
	}
	envUser, err := s.GetUserByToken("env-admin-token")
	if err != nil {
		t.Fatalf("get env synced user: %v", err)
	}
	if envUser == nil || envUser.ID != 1 {
		t.Fatalf("expected env token to resolve default admin, got %+v", envUser)
	}

	if err := s.ClearDefaultUserToken(); err != nil {
		t.Fatalf("clear default token: %v", err)
	}
	stillPresent, err := s.GetUserByToken("env-admin-token")
	if err != nil {
		t.Fatalf("get synced token after clear: %v", err)
	}
	if stillPresent != nil {
		t.Fatalf("expected synced token to be cleared after env token cleanup, got %+v", stillPresent)
	}
	fallbackUser, err := s.GetUserByToken(defaultAdminToken)
	if err != nil {
		t.Fatalf("get built-in default token after clear: %v", err)
	}
	if fallbackUser != nil {
		t.Fatalf("expected built-in default admin token to also be cleared at storage layer, got %+v", fallbackUser)
	}
}

func TestDefaultUserDoesNotAcceptLegacyDefaultToken(t *testing.T) {
	s, err := NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	user, err := s.GetUserByToken("default-token")
	if err != nil {
		t.Fatalf("get user by legacy default token: %v", err)
	}
	if user != nil {
		t.Fatalf("expected legacy default token to be invalid, got %+v", user)
	}

	defaultUser, err := s.GetUserByToken(defaultAdminToken)
	if err != nil {
		t.Fatalf("get built-in default token: %v", err)
	}
	if defaultUser == nil || defaultUser.ID != 1 {
		t.Fatalf("expected built-in default token to resolve default admin, got %+v", defaultUser)
	}
}

func TestClearDefaultUserTokenRemovesSyncedEnvToken(t *testing.T) {
	s, err := NewSQLiteStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage: %v", err)
	}
	defer s.Close()

	if err := s.SyncDefaultUserToken("env-admin-token"); err != nil {
		t.Fatalf("sync default user token: %v", err)
	}
	user, err := s.GetUserByToken("env-admin-token")
	if err != nil {
		t.Fatalf("get synced env token: %v", err)
	}
	if user == nil || user.ID != 1 {
		t.Fatalf("expected synced env token to work before clear, got %+v", user)
	}

	if err := s.ClearDefaultUserToken(); err != nil {
		t.Fatalf("clear default user token: %v", err)
	}
	cleared, err := s.GetUserByToken("env-admin-token")
	if err != nil {
		t.Fatalf("get token after clear: %v", err)
	}
	if cleared != nil {
		t.Fatalf("expected synced env token to be cleared, got %+v", cleared)
	}
	fallbackUser, err := s.GetUserByToken(defaultAdminToken)
	if err != nil {
		t.Fatalf("get fallback token after clear: %v", err)
	}
	if fallbackUser != nil {
		t.Fatalf("expected built-in default token to be cleared at storage layer too, got %+v", fallbackUser)
	}
}
