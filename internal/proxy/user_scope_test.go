package proxy

import (
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
)

func TestRotateEndpointForUser(t *testing.T) {
	cfg := &config.Config{
		Endpoints: []config.Endpoint{
			{Name: "global-a", Enabled: true},
			{Name: "global-b", Enabled: true},
		},
	}
	proxy := New(cfg, &mockStatsStorage{}, nil, "test-device")
	proxy.userCurrentIndex[42] = 0

	next := proxy.rotateEndpointForUser(42)
	if next.Name != "global-b" {
		t.Fatalf("expected global-b, got %s", next.Name)
	}

	next = proxy.rotateEndpointForUser(42)
	if next.Name != "global-a" {
		t.Fatalf("expected global-a, got %s", next.Name)
	}
}

func TestRotateEndpointForUserDoesNotAffectOtherUsers(t *testing.T) {
	cfg := &config.Config{
		Endpoints: []config.Endpoint{
			{Name: "global-a", Enabled: true},
			{Name: "global-b", Enabled: true},
		},
	}
	proxy := New(cfg, &mockStatsStorage{}, nil, "test-device")
	proxy.userCurrentIndex[1] = 0
	proxy.userCurrentIndex[2] = 1

	next1 := proxy.rotateEndpointForUser(1)
	if next1.Name != "global-b" {
		t.Fatalf("expected user1 -> global-b, got %s", next1.Name)
	}
	current2 := proxy.getCurrentEndpointForUser(2)
	if current2.Name != "global-b" {
		t.Fatalf("expected user2 to remain on global-b, got %s", current2.Name)
	}
	if proxy.userCurrentIndex[2] != 1 {
		t.Fatalf("expected user2 index unchanged, got %d", proxy.userCurrentIndex[2])
	}
}
