package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrafficRecorderWritesTrafficFileWhenEnabled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.log")

	recorder := NewTrafficRecorder()
	if err := recorder.EnableFileLogging(path); err != nil {
		t.Fatalf("EnableFileLogging failed: %v", err)
	}
	defer recorder.DisableFileLogging()

	recorder.SetRecording(true)
	recorder.Record(&TrafficLog{
		Timestamp:           time.Unix(1710000000, 0).UTC(),
		EndpointName:        "test-endpoint",
		ClientFormat:        "openai",
		TransformerName:     "openai2",
		Method:              httpMethodPost,
		Path:                "/v1/chat/completions",
		StatusCode:          200,
		Duration:            125 * time.Millisecond,
		InputTokens:         12,
		OutputTokens:        34,
		IsStreaming:         false,
		OriginalRequest:     []byte(`{"model":"gpt-4.1"}`),
		TransformedRequest:  []byte(`{"model":"gpt-4.1"}`),
		OriginalResponse:    []byte(`{"usage":{"input_tokens":12,"output_tokens":34}}`),
		TransformedResponse: []byte(`{"usage":{"input_tokens":12,"output_tokens":34}}`),
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected traffic file to contain data")
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	if !scanner.Scan() {
		t.Fatal("expected at least one log line")
	}

	var entry map[string]interface{}
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		t.Fatalf("failed to unmarshal traffic entry: %v", err)
	}

	if entry["endpointName"] != "test-endpoint" {
		t.Fatalf("unexpected endpointName: %v", entry["endpointName"])
	}
	if entry["transformedResponse"] == "" {
		t.Fatal("expected transformedResponse to be recorded")
	}
}

func TestTrafficRecorderDoesNotWriteWhenNotRecording(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.log")

	recorder := NewTrafficRecorder()
	if err := recorder.EnableFileLogging(path); err != nil {
		t.Fatalf("EnableFileLogging failed: %v", err)
	}
	defer recorder.DisableFileLogging()

	recorder.Record(&TrafficLog{
		Timestamp:       time.Unix(1710000000, 0).UTC(),
		EndpointName:    "test-endpoint",
		ClientFormat:    "openai",
		TransformerName: "openai2",
		Method:          httpMethodPost,
		Path:            "/v1/chat/completions",
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty traffic file when recording disabled, got %q", string(data))
	}
}

const httpMethodPost = "POST"
