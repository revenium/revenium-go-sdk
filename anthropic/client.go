package anthropic

import (
	"sync"
)

// ClientManager manages thread-safe access to Revenium and Bedrock clients
type ClientManager struct {
	mu              sync.RWMutex
	reveniumClients map[string]*ReveniumAnthropic
	bedrockClients  map[string]interface{}
}

// NewClientManager creates a new client manager
func NewClientManager() *ClientManager {
	return &ClientManager{
		reveniumClients: make(map[string]*ReveniumAnthropic),
		bedrockClients:  make(map[string]interface{}),
	}
}

// GetReveniumClient retrieves or creates a Revenium client for the given key
func (cm *ClientManager) GetReveniumClient(key string, cfg *Config) (*ReveniumAnthropic, error) {
	cm.mu.RLock()
	if client, exists := cm.reveniumClients[key]; exists {
		cm.mu.RUnlock()
		return client, nil
	}
	cm.mu.RUnlock()

	// Create new client
	client, err := NewReveniumAnthropic(cfg)
	if err != nil {
		return nil, err
	}

	// Store in cache
	cm.mu.Lock()
	cm.reveniumClients[key] = client
	cm.mu.Unlock()

	return client, nil
}

// GetBedrockClient retrieves or creates a Bedrock client for the given key
func (cm *ClientManager) GetBedrockClient(key string, cfg *Config) (interface{}, error) {
	cm.mu.RLock()
	if client, exists := cm.bedrockClients[key]; exists {
		cm.mu.RUnlock()
		return client, nil
	}
	cm.mu.RUnlock()

	// TODO: Create Bedrock client when implementation is ready
	// For now, return nil
	return nil, nil
}

// RemoveReveniumClient removes a Revenium client from the cache
func (cm *ClientManager) RemoveReveniumClient(key string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.reveniumClients, key)
}

// RemoveBedrockClient removes a Bedrock client from the cache
func (cm *ClientManager) RemoveBedrockClient(key string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.bedrockClients, key)
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
	cm.reveniumClients = make(map[string]*ReveniumAnthropic)
	cm.bedrockClients = make(map[string]interface{})

	return nil
}

// GetClientCount returns the number of cached clients
func (cm *ClientManager) GetClientCount() (int, int) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.reveniumClients), len(cm.bedrockClients)
}
