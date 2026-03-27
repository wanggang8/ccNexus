package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/lich0821/ccNexus/internal/logger"
)

const (
	// MaxTrafficLogs is the maximum number of traffic logs to keep in memory.
	MaxTrafficLogs = 10
	// MaxBodySize is the maximum size of request/response body to store (512KB).
	MaxBodySize = 512 * 1024

	TrafficEventTypeUnified             = "traffic"
	TrafficEventTypeTransformError      = "transform_error"
	TrafficEventTypeRequestBuildError   = "request_build_error"
	TrafficEventTypeRequestSendError    = "request_send_error"
	TrafficEventTypeStreamingSuccess    = "streaming_success"
	TrafficEventTypeStreamingError      = "streaming_error"
	TrafficEventTypeNonStreamingError   = "non_streaming_error"
	TrafficEventTypeNonStreamingSuccess = "non_streaming_success"
	TrafficEventTypeRetryError          = "retry_error"
	TrafficEventTypePassthroughResponse = "passthrough_response"
)

// TrafficLog represents a single traffic log entry.
type TrafficLog struct {
	ID              string        `json:"id"`
	RequestID       string        `json:"requestId"`
	EventType       string        `json:"eventType"`
	Timestamp       time.Time     `json:"timestamp"`
	EndpointName    string        `json:"endpointName"`
	ClientFormat    string        `json:"clientFormat"`
	TransformerName string        `json:"transformerName"`
	Method          string        `json:"method"`
	Path            string        `json:"path"`
	StatusCode      int           `json:"statusCode"`
	Duration        time.Duration `json:"duration"`
	InputTokens     int           `json:"inputTokens"`
	OutputTokens    int           `json:"outputTokens"`
	Error           string        `json:"error,omitempty"`
	IsStreaming     bool          `json:"isStreaming"`
	Truncated       bool          `json:"truncated,omitempty"`
	Degraded        bool          `json:"degraded,omitempty"`
	DegradedReason  []string      `json:"degradedReason,omitempty"`

	// Raw data (not included in summary responses).
	OriginalRequest     []byte `json:"-"`
	TransformedRequest  []byte `json:"-"`
	OriginalResponse    []byte `json:"-"`
	TransformedResponse []byte `json:"-"`
}

// TrafficLogSummary is a lightweight version of TrafficLog for list display.
type TrafficLogSummary struct {
	ID              string `json:"id"`
	RequestID       string `json:"requestId"`
	EventType       string `json:"eventType"`
	Timestamp       int64  `json:"timestamp"`
	EndpointName    string `json:"endpointName"`
	ClientFormat    string `json:"clientFormat"`
	TransformerName string `json:"transformerName"`
	Method          string `json:"method"`
	Path            string `json:"path"`
	StatusCode      int    `json:"statusCode"`
	Duration        int64  `json:"duration"`
	InputTokens     int    `json:"inputTokens"`
	OutputTokens    int    `json:"outputTokens"`
	Error           string `json:"error,omitempty"`
	IsStreaming     bool   `json:"isStreaming"`
	Truncated       bool   `json:"truncated,omitempty"`
	Degraded        bool   `json:"degraded,omitempty"`
	DegradedReason  []string `json:"degradedReason,omitempty"`
}

// TrafficLogDetail includes full request/response data.
type TrafficLogDetail struct {
	TrafficLogSummary
	OriginalRequest     string `json:"originalRequest"`
	TransformedRequest  string `json:"transformedRequest"`
	OriginalResponse    string `json:"originalResponse"`
	TransformedResponse string `json:"transformedResponse"`
}

// TrafficFilter defines filter options for traffic logs.
type TrafficFilter struct {
	EndpointName string `json:"endpointName,omitempty"`
	ClientFormat string `json:"clientFormat,omitempty"`
	StatusCode   int    `json:"statusCode,omitempty"`
	HasError     *bool  `json:"hasError,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

// TrafficRecorder manages traffic log recording with a ring buffer.
type TrafficRecorder struct {
	mu          sync.RWMutex
	logs        []*TrafficLog
	head        int
	count       int
	recording   atomic.Bool
	trafficMu   sync.Mutex
	trafficFile *os.File
}

// NewTrafficRecorder creates a new TrafficRecorder.
func NewTrafficRecorder() *TrafficRecorder {
	tr := &TrafficRecorder{
		logs: make([]*TrafficLog, MaxTrafficLogs),
	}
	tr.recording.Store(false)
	return tr
}

// SetRecording enables or disables in-memory traffic recording.
func (tr *TrafficRecorder) SetRecording(enabled bool) {
	tr.recording.Store(enabled)
}

// IsRecording returns whether in-memory traffic recording is enabled.
func (tr *TrafficRecorder) IsRecording() bool {
	return tr.recording.Load()
}

// EnableFileLogging enables persisted traffic logging.
func (tr *TrafficRecorder) EnableFileLogging(filepath string) error {
	tr.trafficMu.Lock()
	defer tr.trafficMu.Unlock()

	if tr.trafficFile != nil {
		_ = tr.trafficFile.Close()
	}

	f, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	tr.trafficFile = f
	return nil
}

// DisableFileLogging disables persisted traffic logging.
func (tr *TrafficRecorder) DisableFileLogging() {
	tr.trafficMu.Lock()
	defer tr.trafficMu.Unlock()

	if tr.trafficFile != nil {
		_ = tr.trafficFile.Close()
		tr.trafficFile = nil
	}
}

// Record adds a new traffic log entry.
func (tr *TrafficRecorder) Record(log *TrafficLog) {
	if tr == nil || log == nil || !tr.IsRecording() {
		return
	}

	if log.ID == "" {
		log.ID = uuid.New().String()
	}

	tr.mu.Lock()
	defer tr.mu.Unlock()

	log.OriginalRequest = truncateBody(log.OriginalRequest, &log.Truncated)
	log.TransformedRequest = truncateBody(log.TransformedRequest, &log.Truncated)
	log.OriginalResponse = truncateBody(log.OriginalResponse, &log.Truncated)
	log.TransformedResponse = truncateBody(log.TransformedResponse, &log.Truncated)

	tr.logs[tr.head] = nil
	tr.logs[tr.head] = log
	tr.head = (tr.head + 1) % MaxTrafficLogs
	if tr.count < MaxTrafficLogs {
		tr.count++
	}

	tr.writeFileLog(log)
}

func (tr *TrafficRecorder) writeFileLog(log *TrafficLog) {
	tr.trafficMu.Lock()
	defer tr.trafficMu.Unlock()

	if tr.trafficFile == nil {
		return
	}

	entry := map[string]interface{}{
		"id":                  log.ID,
		"requestId":           log.RequestID,
		"eventType":           log.EventType,
		"timestamp":           log.Timestamp.Format(time.RFC3339Nano),
		"endpointName":        log.EndpointName,
		"clientFormat":        log.ClientFormat,
		"transformerName":     log.TransformerName,
		"method":              log.Method,
		"path":                log.Path,
		"statusCode":          log.StatusCode,
		"durationMs":          log.Duration.Milliseconds(),
		"inputTokens":         log.InputTokens,
		"outputTokens":        log.OutputTokens,
		"error":               log.Error,
		"isStreaming":         log.IsStreaming,
		"truncated":           log.Truncated,
		"degraded":            log.Degraded,
		"degradedReason":      log.DegradedReason,
		"originalRequest":     string(log.OriginalRequest),
		"transformedRequest":  string(log.TransformedRequest),
		"originalResponse":    string(log.OriginalResponse),
		"transformedResponse": string(log.TransformedResponse),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		logger.Warn("Failed to marshal traffic log: %v", err)
		return
	}

	if _, err := fmt.Fprintln(tr.trafficFile, string(data)); err != nil {
		logger.Warn("Failed to write traffic log: %v", err)
	}
}

// GetLogs returns traffic logs matching the filter (newest first).
func (tr *TrafficRecorder) GetLogs(filter *TrafficFilter) []TrafficLogSummary {
	if tr == nil {
		return nil
	}

	tr.mu.RLock()
	defer tr.mu.RUnlock()

	limit := MaxTrafficLogs
	if filter != nil && filter.Limit > 0 && filter.Limit < MaxTrafficLogs {
		limit = filter.Limit
	}

	result := make([]TrafficLogSummary, 0, tr.count)
	for i := 0; i < tr.count && len(result) < limit; i++ {
		idx := (tr.head - 1 - i + MaxTrafficLogs) % MaxTrafficLogs
		log := tr.logs[idx]
		if log == nil {
			continue
		}

		if filter != nil {
			if filter.EndpointName != "" && log.EndpointName != filter.EndpointName {
				continue
			}
			if filter.ClientFormat != "" && log.ClientFormat != filter.ClientFormat {
				continue
			}
			if filter.StatusCode != 0 && log.StatusCode != filter.StatusCode {
				continue
			}
			if filter.HasError != nil {
				hasError := log.Error != "" || log.StatusCode >= 400
				if *filter.HasError != hasError {
					continue
				}
			}
		}

		result = append(result, toSummary(log))
	}

	return result
}

// GetLogByID returns a specific traffic log with full details.
func (tr *TrafficRecorder) GetLogByID(id string) *TrafficLogDetail {
	if tr == nil {
		return nil
	}

	tr.mu.RLock()
	defer tr.mu.RUnlock()

	for i := 0; i < tr.count; i++ {
		idx := (tr.head - 1 - i + MaxTrafficLogs) % MaxTrafficLogs
		log := tr.logs[idx]
		if log != nil && log.ID == id {
			return toDetail(log)
		}
	}
	return nil
}

// Clear removes all traffic logs.
func (tr *TrafficRecorder) Clear() {
	if tr == nil {
		return
	}

	tr.mu.Lock()
	defer tr.mu.Unlock()

	for i := range tr.logs {
		tr.logs[i] = nil
	}
	tr.head = 0
	tr.count = 0
}

// GetCount returns the current number of logs.
func (tr *TrafficRecorder) GetCount() int {
	if tr == nil {
		return 0
	}

	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return tr.count
}

func truncateBody(body []byte, truncated *bool) []byte {
	if len(body) > MaxBodySize {
		*truncated = true
		return body[:MaxBodySize]
	}
	return body
}

func toSummary(log *TrafficLog) TrafficLogSummary {
	return TrafficLogSummary{
		ID:              log.ID,
		RequestID:       log.RequestID,
		EventType:       log.EventType,
		Timestamp:       log.Timestamp.UnixMilli(),
		EndpointName:    log.EndpointName,
		ClientFormat:    log.ClientFormat,
		TransformerName: log.TransformerName,
		Method:          log.Method,
		Path:            log.Path,
		StatusCode:      log.StatusCode,
		Duration:        log.Duration.Milliseconds(),
		InputTokens:     log.InputTokens,
		OutputTokens:    log.OutputTokens,
		Error:           log.Error,
		IsStreaming:     log.IsStreaming,
		Truncated:       log.Truncated,
		Degraded:        log.Degraded,
		DegradedReason:  log.DegradedReason,
	}
}

func toDetail(log *TrafficLog) *TrafficLogDetail {
	if log == nil {
		return nil
	}

	return &TrafficLogDetail{
		TrafficLogSummary:   toSummary(log),
		OriginalRequest:     string(log.OriginalRequest),
		TransformedRequest:  string(log.TransformedRequest),
		OriginalResponse:    string(log.OriginalResponse),
		TransformedResponse: string(log.TransformedResponse),
	}
}
