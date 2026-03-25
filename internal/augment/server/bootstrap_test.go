package server

import (
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
)

func TestResolveKeyPathPrefersConfigValue(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateAugmentConfig(true, 2346, "/tmp/custom-key.pem")

	got := ResolveKeyPath(cfg, "/tmp/default-key.pem")
	if got != "/tmp/custom-key.pem" {
		t.Fatalf("ResolveKeyPath() = %q, want config key path", got)
	}
}

func TestStartFromConfigReturnsNilWhenDisabled(t *testing.T) {
	cfg := config.DefaultConfig()

	srv, err := StartFromConfig(cfg, "/tmp/missing-key.pem", nil, nil)
	if err != nil {
		t.Fatalf("StartFromConfig() unexpected error: %v", err)
	}
	if srv != nil {
		t.Fatal("StartFromConfig() returned server while disabled")
	}
}

func TestStartFromConfigErrorsWhenKeyMissing(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UpdateAugmentConfig(true, 2346, "")

	srv, err := StartFromConfig(cfg, "/tmp/missing-key.pem", nil, nil)
	if err == nil {
		t.Fatal("StartFromConfig() expected missing key error")
	}
	if srv != nil {
		t.Fatal("StartFromConfig() returned server on missing key")
	}
}
