package server

import (
	"fmt"
	"os"
	"strings"

	"github.com/lich0821/ccNexus/internal/config"
	"github.com/lich0821/ccNexus/internal/proxy"
)

// ResolveKeyPath resolves the Augment private key path from config or fallback path.
func ResolveKeyPath(cfg *config.Config, defaultKeyPath string) string {
	if cfg == nil {
		return strings.TrimSpace(defaultKeyPath)
	}
	if keyPath := strings.TrimSpace(cfg.GetAugmentKeyPath()); keyPath != "" {
		return keyPath
	}
	return strings.TrimSpace(defaultKeyPath)
}

// StartFromConfig starts the Augment server when enabled in config.
func StartFromConfig(cfg *config.Config, defaultKeyPath string, trafficRecorder *proxy.TrafficRecorder, stats *proxy.Stats) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("augment server: config is nil")
	}
	if !cfg.GetAugmentEnabled() {
		return nil, nil
	}

	keyPath := ResolveKeyPath(cfg, defaultKeyPath)
	if keyPath == "" {
		return nil, fmt.Errorf("augment server: key path is empty")
	}
	if _, err := os.Stat(keyPath); err != nil {
		return nil, fmt.Errorf("augment server: private key not found at %s: %w", keyPath, err)
	}

	srv, err := New(cfg, keyPath, trafficRecorder, stats)
	if err != nil {
		return nil, err
	}
	if err := srv.Start(); err != nil {
		return nil, err
	}
	return srv, nil
}
