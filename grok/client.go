package grok

import (
	"sync"
)

// ClientManager manages thread-safe access to Revenium clients
type ClientManager struct {
	mu              sync.RWMutex
	reveniumClients map[string]*ReveniumGrok
}

// NewClientManager creates a new client manager
func NewClientManager() *ClientManager {
	return &ClientManager{
		reveniumClients: make(map[string]*ReveniumGrok),
	}
}

// GetReveniumClient retrieves or creates a Revenium client for the given key
func (cm *ClientManager) GetReveniumClient(key string, cfg *Config) (*ReveniumGrok, error) {
	cm.mu.RLock()
	if client, exists := cm.reveniumClients[key]; exists {
		cm.mu.RUnlock()
		return client, nil
	}
	cm.mu.RUnlock()

	// Create new client
	client, err := NewReveniumGrok(cfg)
	if err != nil {
		return nil, err
	}

	// Store in cache
	cm.mu.Lock()
	cm.reveniumClients[key] = client
	cm.mu.Unlock()

	return client, nil
}

// RemoveReveniumClient removes a Revenium client from the cache
func (cm *ClientManager) RemoveReveniumClient(key string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.reveniumClients, key)
}

// CloseAll closes all clients and cleans up resources
func (cm *ClientManager) CloseAll() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Close all Revenium clients
	for _, client := range cm.reveniumClients {
		if err := client.Close(); err != nil {
			return err
		}
	}

	// Clear caches
	cm.reveniumClients = make(map[string]*ReveniumGrok)

	return nil
}

// GetClientCount returns the number of cached clients
func (cm *ClientManager) GetClientCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.reveniumClients)
}
