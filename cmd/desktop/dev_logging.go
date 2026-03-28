package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/proxy"
)

const (
	wailsDevDebugLogName   = "wails-dev-debug.log"
	wailsDevTrafficLogName = "wails-dev-traffic.jsonl"
)

func isWailsDevMode() bool {
	return isWailsDevModeFromLookup(os.Getenv)
}

func isWailsDevModeFromLookup(getenv func(string) string) bool {
	if getenv == nil {
		return false
	}
	return strings.TrimSpace(getenv("devserver")) != "" ||
		strings.TrimSpace(getenv("frontenddevserverurl")) != ""
}

func wailsDevLogPaths(configDir string) (string, string) {
	logDir := filepath.Join(configDir, "logs")
	return filepath.Join(logDir, wailsDevDebugLogName), filepath.Join(logDir, wailsDevTrafficLogName)
}

func enableWailsDevLogging(configDir string, recorder *proxy.TrafficRecorder) {
	if !isWailsDevMode() {
		return
	}

	debugPath, trafficPath := wailsDevLogPaths(configDir)
	logDir := filepath.Dir(debugPath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		logger.Warn("Failed to create Wails dev log directory %s: %v", logDir, err)
		return
	}

	if err := logger.GetLogger().EnableDebugFile(debugPath); err != nil {
		logger.Warn("Failed to enable Wails dev debug log %s: %v", debugPath, err)
	} else {
		logger.Info("Wails dev debug log enabled: %s", debugPath)
	}

	if recorder == nil {
		return
	}
	if err := recorder.EnableFileLogging(trafficPath); err != nil {
		logger.Warn("Failed to enable Wails dev traffic log %s: %v", trafficPath, err)
		return
	}
	recorder.SetRecording(true)
	logger.Info("Wails dev traffic log enabled: %s", trafficPath)
}
