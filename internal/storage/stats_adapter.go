package storage

import (
	"github.com/lich0821/ccNexus/internal/proxy"
)

// StatsStorageAdapter adapts SQLiteStorage to be used by proxy.Stats
// It implements the proxy.StatsStorage interface
type StatsStorageAdapter struct {
	storage *SQLiteStorage
}

// NewStatsStorageAdapter creates a new adapter
func NewStatsStorageAdapter(storage *SQLiteStorage) *StatsStorageAdapter {
	return &StatsStorageAdapter{storage: storage}
}

// RecordDailyStat records a daily stat
func (a *StatsStorageAdapter) RecordDailyStat(stat *proxy.StatRecord) error {
	dailyStat := &DailyStat{
		EndpointName: stat.EndpointName,
		Date:         stat.Date,
		Requests:     stat.Requests,
		Errors:       stat.Errors,
		InputTokens:  stat.InputTokens,
		OutputTokens: stat.OutputTokens,
		DeviceID:     stat.DeviceID,
	}
	return a.storage.RecordDailyStat(dailyStat)
}

// GetTotalStats gets total stats for all endpoints
func (a *StatsStorageAdapter) GetTotalStats() (int, map[string]*proxy.StatsData, error) {
	totalRequests, endpointStats, err := a.storage.GetTotalStats()
	if err != nil {
		return 0, nil, err
	}

	result := make(map[string]*proxy.StatsData)
	for name, stats := range endpointStats {
		result[name] = &proxy.StatsData{
			Requests:     stats.Requests,
			Errors:       stats.Errors,
			InputTokens:  stats.InputTokens,
			OutputTokens: stats.OutputTokens,
		}
	}

	return totalRequests, result, nil
}

// GetDailyStats gets daily stats for an endpoint
func (a *StatsStorageAdapter) GetDailyStats(endpointName, startDate, endDate string) ([]*proxy.DailyRecord, error) {
	dailyStats, err := a.storage.GetDailyStats(endpointName, startDate, endDate)
	if err != nil {
		return nil, err
	}

	result := make([]*proxy.DailyRecord, len(dailyStats))
	for i, stat := range dailyStats {
		result[i] = &proxy.DailyRecord{
			Date:         stat.Date,
			Requests:     stat.Requests,
			Errors:       stat.Errors,
			InputTokens:  stat.InputTokens,
			OutputTokens: stat.OutputTokens,
		}
	}

	return result, nil
}
