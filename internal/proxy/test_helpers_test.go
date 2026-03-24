package proxy

// mockStatsStorage implements StatsStorage interface for tests.
type mockStatsStorage struct{}

func (m *mockStatsStorage) RecordDailyStat(stat interface{}) error {
	return nil
}

func (m *mockStatsStorage) GetTotalStats() (int, map[string]interface{}, error) {
	return 0, make(map[string]interface{}), nil
}

func (m *mockStatsStorage) GetDailyStats(endpointName, startDate, endDate string) ([]interface{}, error) {
	return []interface{}{}, nil
}

func (m *mockStatsStorage) GetPeriodStatsAggregated(startDate, endDate string) (map[string]interface{}, error) {
	return make(map[string]interface{}), nil
}
