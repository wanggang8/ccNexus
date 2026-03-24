package storage

import "github.com/lich0821/ccNexus/internal/config"

// ConfigStorageAdapter adapts SQLiteStorage to config.StorageAdapter interface
type ConfigStorageAdapter struct {
	storage *SQLiteStorage
	userID  int64
}

// NewConfigStorageAdapter creates a new adapter
func NewConfigStorageAdapter(storage *SQLiteStorage) *ConfigStorageAdapter {
	return &ConfigStorageAdapter{storage: storage, userID: 1}
}

// NewConfigStorageAdapterForUser creates a scoped adapter for a specific user.
func NewConfigStorageAdapterForUser(storage *SQLiteStorage, userID int64) *ConfigStorageAdapter {
	return &ConfigStorageAdapter{storage: storage, userID: userID}
}

// GetEndpoints returns endpoints in config format
func (a *ConfigStorageAdapter) GetEndpoints() ([]config.StorageEndpoint, error) {
	endpoints, err := a.storage.GetEndpointsByUser(a.userID)
	if err != nil {
		return nil, err
	}

	result := make([]config.StorageEndpoint, len(endpoints))
	for i, ep := range endpoints {
		result[i] = config.StorageEndpoint{
			ID:               ep.ID,
			Name:             ep.Name,
			APIUrl:           ep.APIUrl,
			APIKey:           ep.APIKey,
			AuthMode:         ep.AuthMode,
			Enabled:          ep.Enabled,
			Transformer:      ep.Transformer,
			Model:            ep.Model,
			Remark:           ep.Remark,
			RequestOverrides: ep.RequestOverrides,
			SortOrder:        ep.SortOrder,
		}
	}
	return result, nil
}

// SaveEndpoint saves an endpoint
func (a *ConfigStorageAdapter) SaveEndpoint(ep *config.StorageEndpoint) error {
	endpoint := &Endpoint{
		Name:             ep.Name,
		APIUrl:           ep.APIUrl,
		APIKey:           ep.APIKey,
		AuthMode:         ep.AuthMode,
		Enabled:          ep.Enabled,
		Transformer:      ep.Transformer,
		Model:            ep.Model,
		Remark:           ep.Remark,
		RequestOverrides: ep.RequestOverrides,
		SortOrder:        ep.SortOrder,
	}
	return a.storage.SaveEndpointForUser(a.userID, endpoint)
}

// UpdateEndpoint updates an endpoint
func (a *ConfigStorageAdapter) UpdateEndpoint(ep *config.StorageEndpoint) error {
	endpoint := &Endpoint{
		ID:               ep.ID,
		Name:             ep.Name,
		APIUrl:           ep.APIUrl,
		APIKey:           ep.APIKey,
		AuthMode:         ep.AuthMode,
		Enabled:          ep.Enabled,
		Transformer:      ep.Transformer,
		Model:            ep.Model,
		Remark:           ep.Remark,
		RequestOverrides: ep.RequestOverrides,
		SortOrder:        ep.SortOrder,
	}
	return a.storage.UpdateEndpointForUser(a.userID, endpoint)
}

// DeleteEndpoint deletes an endpoint
func (a *ConfigStorageAdapter) DeleteEndpoint(name string) error {
	return a.storage.DeleteEndpointForUser(a.userID, name)
}

// GetConfig gets a config value
func (a *ConfigStorageAdapter) GetConfig(key string) (string, error) {
	return a.storage.GetConfig(key)
}

// SetConfig sets a config value
func (a *ConfigStorageAdapter) SetConfig(key, value string) error {
	return a.storage.SetConfig(key, value)
}
