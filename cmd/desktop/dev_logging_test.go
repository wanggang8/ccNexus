package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lich0821/ccNexus/internal/proxy"
)

func TestIsWailsDevModeFromLookup(t *testing.T) {
	if isWailsDevModeFromLookup(nil) {
		t.Fatal("expected nil lookup to be treated as non-dev")
	}
	if !isWailsDevModeFromLookup(func(key string) string {
		if key == "devserver" {
			return "localhost:34115"
		}
		return ""
	}) {
		t.Fatal("expected devserver env to enable Wails dev mode")
	}
	if !isWailsDevModeFromLookup(func(key string) string {
		if key == "frontenddevserverurl" {
			return "http://localhost:34115"
		}
		return ""
	}) {
		t.Fatal("expected frontenddevserverurl env to enable Wails dev mode")
	}
	if isWailsDevModeFromLookup(func(string) string { return "" }) {
		t.Fatal("expected empty env to be treated as non-dev")
	}
}

func TestWailsDevLogPaths(t *testing.T) {
	debugPath, trafficPath := wailsDevLogPaths("/tmp/ccNexus")
	if want := filepath.Join("/tmp/ccNexus", "logs", wailsDevDebugLogName); debugPath != want {
		t.Fatalf("expected debug log path %s, got %s", want, debugPath)
	}
	if want := filepath.Join("/tmp/ccNexus", "logs", wailsDevTrafficLogName); trafficPath != want {
		t.Fatalf("expected traffic log path %s, got %s", want, trafficPath)
	}
}

func TestEnableWailsDevLoggingCreatesTrafficLogAndEnablesRecording(t *testing.T) {
	t.Setenv("devserver", "localhost:34115")
	t.Setenv("frontenddevserverurl", "")

	dir := t.TempDir()
	recorder := proxy.NewTrafficRecorder()

	enableWailsDevLogging(dir, recorder)

	if !recorder.IsRecording() {
		t.Fatal("expected Wails dev logging to enable traffic recording")
	}

	_, trafficPath := wailsDevLogPaths(dir)
	if _, err := os.Stat(trafficPath); err != nil {
		t.Fatalf("expected traffic log file to be created, got %v", err)
	}

	recorder.DisableFileLogging()
}
