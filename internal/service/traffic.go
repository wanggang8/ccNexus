package service

import (
	"encoding/json"

	"github.com/lich0821/ccNexus/internal/proxy"
)

// TrafficService handles traffic log operations
type TrafficService struct {
	proxy *proxy.Proxy
}

// NewTrafficService creates a new traffic service
func NewTrafficService(p *proxy.Proxy) *TrafficService {
	return &TrafficService{proxy: p}
}

// SetRecording enables or disables traffic recording
func (s *TrafficService) SetRecording(enabled bool) {
	s.proxy.GetTrafficRecorder().SetRecording(enabled)
}

// IsRecording returns whether traffic recording is enabled
func (s *TrafficService) IsRecording() bool {
	return s.proxy.GetTrafficRecorder().IsRecording()
}

// GetLogs returns traffic logs matching the filter
func (s *TrafficService) GetLogs(filterJSON string) string {
	var filter *proxy.TrafficFilter
	if filterJSON != "" {
		filter = &proxy.TrafficFilter{}
		if err := json.Unmarshal([]byte(filterJSON), filter); err != nil {
			filter = nil
		}
	}

	logs := s.proxy.GetTrafficRecorder().GetLogs(filter)

	result := map[string]interface{}{
		"logs":      logs,
		"count":     len(logs),
		"total":     s.proxy.GetTrafficRecorder().GetCount(),
		"recording": s.IsRecording(),
	}

	data, err := json.Marshal(result)
	if err != nil {
		return `{"error": "failed to marshal response", "logs": [], "count": 0, "total": 0, "recording": false}`
	}
	return string(data)
}

// GetLogDetail returns a specific traffic log with full details
func (s *TrafficService) GetLogDetail(id string) string {
	detail := s.proxy.GetTrafficRecorder().GetLogByID(id)
	if detail == nil {
		return "{\"error\": \"not found\"}"
	}

	data, err := json.Marshal(detail)
	if err != nil {
		return `{"error": "failed to marshal detail"}`
	}
	return string(data)
}

// ClearLogs removes all traffic logs
func (s *TrafficService) ClearLogs() {
	s.proxy.GetTrafficRecorder().Clear()
}
