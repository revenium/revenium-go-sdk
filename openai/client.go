package openai

import (
	"errors"
	"sync"
)

// ClientManager manages thread-safe access to Revenium and Azure clients
type ClientManager struct {
	mu              sync.RWMutex
	reveniumClients map[string]*ReveniumOpenAI
	azureClients    map[string]interface{}
}

// NewClientManager creates a new client manager
func NewClientManager() *ClientManager {
	return &ClientManager{
		reveniumClients: make(map[string]*ReveniumOpenAI),
		azureClients:    make(map[string]interface{}),
	}
}

// GetReveniumClient retrieves or creates a Revenium client for the given key
func (cm *ClientManager) GetReveniumClient(key string, cfg *Config) (*ReveniumOpenAI, error) {
	cm.mu.RLock()
	if client, exists := cm.reveniumClients[key]; exists {
		cm.mu.RUnlock()
		return client, nil
	}
	cm.mu.RUnlock()

	client, err := NewReveniumOpenAI(cfg)
	if err != nil {
		return nil, err
	}

	cm.mu.Lock()
	cm.reveniumClients[key] = client
	cm.mu.Unlock()

	return client, nil
}

// GetAzureClient retrieves or creates an Azure client for the given key
func (cm *ClientManager) GetAzureClient(key string, cfg *Config) (interface{}, error) {
	cm.mu.RLock()
	if client, exists := cm.azureClients[key]; exists {
		cm.mu.RUnlock()
		return client, nil
	}
	cm.mu.RUnlock()

	return nil, nil
}

// RemoveReveniumClient removes a Revenium client from the cache
func (cm *ClientManager) RemoveReveniumClient(key string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.reveniumClients, key)
}

// RemoveAzureClient removes an Azure client from the cache
func (cm *ClientManager) RemoveAzureClient(key string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.azureClients, key)
}

// CloseAll closes all clients and cleans up resources
func (cm *ClientManager) CloseAll() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var errs []error
	for _, client := range cm.reveniumClients {
		errs = append(errs, client.Close())
	}

	cm.reveniumClients = make(map[string]*ReveniumOpenAI)
	cm.azureClients = make(map[string]interface{})

	return errors.Join(errs...)
}

// GetClientCount returns the number of cached clients
func (cm *ClientManager) GetClientCount() (int, int) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.reveniumClients), len(cm.azureClients)
}
