package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestTrafficRecorderKeepsNewestLogsAndReturnsDetails(t *testing.T) {
	t.Parallel()

	recorder := NewTrafficRecorder()
	recorder.SetRecording(true)

	for i := 0; i < MaxTrafficLogs+2; i++ {
		payload := fmt.Sprintf(`{"index":%d}`, i)
		recorder.Record(&TrafficLog{
			ID:                  time.Unix(int64(i+1), 0).UTC().Format(time.RFC3339),
			RequestID:           "req-shared",
			EventType:           TrafficEventTypeUnified,
			Timestamp:           time.Unix(int64(i+1), 0).UTC(),
			EndpointName:        "endpoint",
			ClientFormat:        "claude",
			TransformerName:     "cc_claude",
			Method:              httpMethodPost,
			Path:                "/v1/messages",
			StatusCode:          200,
			OriginalRequest:     []byte(payload),
			TransformedRequest:  []byte(payload),
			OriginalResponse:    []byte(`{"id":"upstream"}`),
			TransformedResponse: []byte(`{"id":"client"}`),
		})
	}

	if got := recorder.GetCount(); got != MaxTrafficLogs {
		t.Fatalf("GetCount() = %d, want %d", got, MaxTrafficLogs)
	}

	logs := recorder.GetLogs(nil)
	if len(logs) != MaxTrafficLogs {
		t.Fatalf("GetLogs() returned %d items, want %d", len(logs), MaxTrafficLogs)
	}

	if logs[0].ID != time.Unix(int64(MaxTrafficLogs+2), 0).UTC().Format(time.RFC3339) {
		t.Fatalf("newest log ID = %q, want newest entry", logs[0].ID)
	}
	if logs[len(logs)-1].ID != time.Unix(3, 0).UTC().Format(time.RFC3339) {
		t.Fatalf("oldest retained log ID = %q, want rollover entry", logs[len(logs)-1].ID)
	}

	detail := recorder.GetLogByID(logs[0].ID)
	if detail == nil {
		t.Fatal("GetLogByID() returned nil")
	}
	if detail.RequestID != "req-shared" {
		t.Fatalf("detail.RequestID = %q, want req-shared", detail.RequestID)
	}
	if detail.TransformedResponse != `{"id":"client"}` {
		t.Fatalf("detail.TransformedResponse = %q, want transformed payload", detail.TransformedResponse)
	}
}

func TestTrafficRecorderClearRemovesAllLogs(t *testing.T) {
	t.Parallel()

	recorder := NewTrafficRecorder()
	recorder.SetRecording(true)

	recorder.Record(&TrafficLog{
		ID:               "log-1",
		RequestID:        "req-1",
		EventType:        TrafficEventTypeUnified,
		Timestamp:        time.Unix(1710000000, 0).UTC(),
		EndpointName:     "endpoint",
		ClientFormat:     "augment",
		TransformerName:  "openai",
		Method:           httpMethodPost,
		Path:             "/v1/chat/completions",
		StatusCode:       200,
		OriginalRequest:  []byte(`{"hello":"world"}`),
		OriginalResponse: []byte(`{"ok":true}`),
	})

	if got := recorder.GetCount(); got != 1 {
		t.Fatalf("GetCount() before clear = %d, want 1", got)
	}

	recorder.Clear()

	if got := recorder.GetCount(); got != 0 {
		t.Fatalf("GetCount() after clear = %d, want 0", got)
	}
	if logs := recorder.GetLogs(nil); len(logs) != 0 {
		t.Fatalf("GetLogs() after clear returned %d items, want 0", len(logs))
	}
	if detail := recorder.GetLogByID("log-1"); detail != nil {
		t.Fatal("GetLogByID() after clear returned non-nil detail")
	}
}

func TestTrafficRecorderPreservesLargeBodiesWithoutTruncation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.log")

	recorder := NewTrafficRecorder()
	if err := recorder.EnableFileLogging(path); err != nil {
		t.Fatalf("EnableFileLogging failed: %v", err)
	}
	defer recorder.DisableFileLogging()

	recorder.SetRecording(true)

	largeBody := []byte(strings.Repeat("x", 600*1024))
	logID := "large-log"
	recorder.Record(&TrafficLog{
		ID:                  logID,
		RequestID:           "req-large",
		EventType:           TrafficEventTypeUnified,
		Timestamp:           time.Unix(1710000000, 0).UTC(),
		EndpointName:        "endpoint",
		ClientFormat:        "openai",
		TransformerName:     "openai",
		Method:              httpMethodPost,
		Path:                "/v1/chat/completions",
		StatusCode:          200,
		OriginalRequest:     largeBody,
		TransformedRequest:  largeBody,
		OriginalResponse:    largeBody,
		TransformedResponse: largeBody,
	})

	detail := recorder.GetLogByID(logID)
	if detail == nil {
		t.Fatal("GetLogByID() returned nil")
	}
	if detail.Truncated {
		t.Fatal("expected large traffic log to remain untruncated")
	}
	if detail.OriginalRequest != string(largeBody) {
		t.Fatalf("detail.OriginalRequest length = %d, want %d", len(detail.OriginalRequest), len(largeBody))
	}
	if detail.TransformedResponse != string(largeBody) {
		t.Fatalf("detail.TransformedResponse length = %d, want %d", len(detail.TransformedResponse), len(largeBody))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		t.Fatal("expected traffic file to contain data")
	}

	var entry map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(data), &entry); err != nil {
		t.Fatalf("failed to unmarshal traffic entry: %v", err)
	}
	if entry["truncated"] != false {
		t.Fatalf("expected file log truncated=false, got %#v", entry["truncated"])
	}
	if got := entry["originalRequest"].(string); got != string(largeBody) {
		t.Fatalf("file originalRequest length = %d, want %d", len(got), len(largeBody))
	}
	if got := entry["transformedResponse"].(string); got != string(largeBody) {
		t.Fatalf("file transformedResponse length = %d, want %d", len(got), len(largeBody))
	}
}

func TestTrafficRecorderClonesBodiesBeforeStoring(t *testing.T) {
	t.Parallel()

	recorder := NewTrafficRecorder()
	recorder.SetRecording(true)

	origReq := []byte(`{"request":"before"}`)
	transReq := []byte(`{"transformed":"before"}`)
	origResp := []byte(`{"response":"before"}`)
	transResp := []byte(`{"client":"before"}`)

	recorder.Record(&TrafficLog{
		ID:                  "clone-test",
		RequestID:           "req-clone",
		EventType:           TrafficEventTypeUnified,
		Timestamp:           time.Unix(1710000000, 0).UTC(),
		EndpointName:        "endpoint",
		ClientFormat:        "openai",
		TransformerName:     "openai",
		Method:              httpMethodPost,
		Path:                "/v1/chat/completions",
		StatusCode:          200,
		OriginalRequest:     origReq,
		TransformedRequest:  transReq,
		OriginalResponse:    origResp,
		TransformedResponse: transResp,
		DegradedReason:      []string{"first"},
	})

	copy(origReq, []byte(`{"request":"after "}`))
	copy(transReq, []byte(`{"transformed":"after "}`))
	copy(origResp, []byte(`{"response":"after "}`))
	copy(transResp, []byte(`{"client":"after "}`))

	detail := recorder.GetLogByID("clone-test")
	if detail == nil {
		t.Fatal("GetLogByID() returned nil")
	}
	if detail.OriginalRequest != `{"request":"before"}` {
		t.Fatalf("detail.OriginalRequest = %q, want original snapshot", detail.OriginalRequest)
	}
	if detail.TransformedRequest != `{"transformed":"before"}` {
		t.Fatalf("detail.TransformedRequest = %q, want original snapshot", detail.TransformedRequest)
	}
	if detail.OriginalResponse != `{"response":"before"}` {
		t.Fatalf("detail.OriginalResponse = %q, want original snapshot", detail.OriginalResponse)
	}
	if detail.TransformedResponse != `{"client":"before"}` {
		t.Fatalf("detail.TransformedResponse = %q, want original snapshot", detail.TransformedResponse)
	}
}

const httpMethodPost = "POST"
