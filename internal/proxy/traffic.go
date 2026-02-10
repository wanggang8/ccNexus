package proxy

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const (
	// MaxTrafficLogs is the maximum number of traffic logs to keep in memory
	MaxTrafficLogs = 500
	// MaxBodySize is the maximum size of request/response body to store (512KB)
	MaxBodySize = 512 * 1024
)

// TrafficLog represents a single traffic log entry
type TrafficLog struct {
	ID                  string        `json:"id"`
	Timestamp           time.Time     `json:"timestamp"`
	EndpointName        string        `json:"endpointName"`
	ClientFormat        string        `json:"clientFormat"`
	TransformerName     string        `json:"transformerName"`
	Method              string        `json:"method"`
	Path                string        `json:"path"`
	StatusCode          int           `json:"statusCode"`
	Duration            time.Duration `json:"duration"`
	InputTokens         int           `json:"inputTokens"`
	OutputTokens        int           `json:"outputTokens"`
	Error               string        `json:"error,omitempty"`
	IsStreaming         bool          `json:"isStreaming"`
	Truncated           bool          `json:"truncated,omitempty"`

	// Raw data (not included in summary responses)
	OriginalRequest     []byte `json:"-"`
	TransformedRequest  []byte `json:"-"`
	OriginalResponse    []byte `json:"-"`
	TransformedResponse []byte `json:"-"`
}

// TrafficLogSummary is a lightweight version of TrafficLog for list display
type TrafficLogSummary struct {
	ID              string `json:"id"`
	Timestamp       int64  `json:"timestamp"` // Unix milliseconds
	EndpointName    string `json:"endpointName"`
	ClientFormat    string `json:"clientFormat"`
	TransformerName string `json:"transformerName"`
	Method          string `json:"method"`
	Path            string `json:"path"`
	StatusCode      int    `json:"statusCode"`
	Duration        int64  `json:"duration"` // Milliseconds
	InputTokens     int    `json:"inputTokens"`
	OutputTokens    int    `json:"outputTokens"`
	Error           string `json:"error,omitempty"`
	IsStreaming     bool   `json:"isStreaming"`
	Truncated       bool   `json:"truncated,omitempty"`
}

// TrafficLogDetail includes full request/response data
type TrafficLogDetail struct {
	TrafficLogSummary
	OriginalRequest     string `json:"originalRequest"`
	TransformedRequest  string `json:"transformedRequest"`
	OriginalResponse    string `json:"originalResponse"`
	TransformedResponse string `json:"transformedResponse"`
}

// TrafficFilter defines filter options for traffic logs
type TrafficFilter struct {
	EndpointName string `json:"endpointName,omitempty"`
	ClientFormat string `json:"clientFormat,omitempty"`
	StatusCode   int    `json:"statusCode,omitempty"`
	HasError     *bool  `json:"hasError,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

// TrafficRecorder manages traffic log recording with a ring buffer
type TrafficRecorder struct {
	mu        sync.RWMutex
	logs      []*TrafficLog
	head      int // Next write position
	count     int // Current number of logs
	recording atomic.Bool
}

// NewTrafficRecorder creates a new TrafficRecorder
func NewTrafficRecorder() *TrafficRecorder {
	tr := &TrafficRecorder{
		logs: make([]*TrafficLog, MaxTrafficLogs),
	}
	tr.recording.Store(false)
	return tr
}

// SetRecording enables or disables traffic recording
func (tr *TrafficRecorder) SetRecording(enabled bool) {
	tr.recording.Store(enabled)
}

// IsRecording returns whether traffic recording is enabled
func (tr *TrafficRecorder) IsRecording() bool {
	return tr.recording.Load()
}

// Record adds a new traffic log entry
// Note: The caller must pass a newly created TrafficLog object (not shared across goroutines)
func (tr *TrafficRecorder) Record(log *TrafficLog) {
	if !tr.IsRecording() {
		return
	}

	// Generate ID if not set
	if log.ID == "" {
		log.ID = uuid.New().String()
	}

	tr.mu.Lock()
	defer tr.mu.Unlock()

	// Truncate large bodies (inside lock for safety)
	log.OriginalRequest = truncateBody(log.OriginalRequest, &log.Truncated)
	log.TransformedRequest = truncateBody(log.TransformedRequest, &log.Truncated)
	log.OriginalResponse = truncateBody(log.OriginalResponse, &log.Truncated)
	log.TransformedResponse = truncateBody(log.TransformedResponse, &log.Truncated)

	// Release old reference before overwriting (helps GC)
	tr.logs[tr.head] = nil
	tr.logs[tr.head] = log
	tr.head = (tr.head + 1) % MaxTrafficLogs
	if tr.count < MaxTrafficLogs {
		tr.count++
	}
}

// GetLogs returns traffic logs matching the filter (newest first)
func (tr *TrafficRecorder) GetLogs(filter *TrafficFilter) []TrafficLogSummary {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	limit := MaxTrafficLogs
	if filter != nil && filter.Limit > 0 && filter.Limit < MaxTrafficLogs {
		limit = filter.Limit
	}

	result := make([]TrafficLogSummary, 0, tr.count)

	// Iterate from newest to oldest
	for i := 0; i < tr.count && len(result) < limit; i++ {
		idx := (tr.head - 1 - i + MaxTrafficLogs) % MaxTrafficLogs
		log := tr.logs[idx]
		if log == nil {
			continue
		}

		// Apply filters
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

// GetLogByID returns a specific traffic log with full details
func (tr *TrafficRecorder) GetLogByID(id string) *TrafficLogDetail {
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

// Clear removes all traffic logs
func (tr *TrafficRecorder) Clear() {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	for i := range tr.logs {
		tr.logs[i] = nil
	}
	tr.head = 0
	tr.count = 0
}

// GetCount returns the current number of logs
func (tr *TrafficRecorder) GetCount() int {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return tr.count
}

// truncateBody truncates body if it exceeds MaxBodySize
func truncateBody(body []byte, truncated *bool) []byte {
	if len(body) > MaxBodySize {
		*truncated = true
		return body[:MaxBodySize]
	}
	return body
}

// toSummary converts TrafficLog to TrafficLogSummary
func toSummary(log *TrafficLog) TrafficLogSummary {
	return TrafficLogSummary{
		ID:              log.ID,
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
	}
}

// toDetail converts TrafficLog to TrafficLogDetail
func toDetail(log *TrafficLog) *TrafficLogDetail {
	return &TrafficLogDetail{
		TrafficLogSummary:   toSummary(log),
		OriginalRequest:     string(log.OriginalRequest),
		TransformedRequest:  string(log.TransformedRequest),
		OriginalResponse:    string(log.OriginalResponse),
		TransformedResponse: string(log.TransformedResponse),
	}
}
