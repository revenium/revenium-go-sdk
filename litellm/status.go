package litellm

// GetStatus reports whether the middleware has configuration and whether metering is currently enabled
func (r *ReveniumLiteLLM) GetStatus() MiddlewareStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	status := MiddlewareStatus{
		Initialized: r.config != nil,
		Enabled:     r.enabled.Load(),
		HasConfig:   r.config != nil,
	}
	if r.config != nil {
		status.ProxyURL = r.config.LiteLLMProxyURL
	}
	return status
}

// Enable turns on metering emission; when disabled, requests still reach the LiteLLM proxy but no payload is sent
func (r *ReveniumLiteLLM) Enable() {
	r.enabled.Store(true)
}

// Disable stops metering emission without tearing down the HTTP client
func (r *ReveniumLiteLLM) Disable() {
	r.enabled.Store(false)
}

// IsEnabled returns true when metering emission is active
func (r *ReveniumLiteLLM) IsEnabled() bool {
	return r.enabled.Load()
}

// GetStatus returns the global middleware status; returns zero value when Initialize has not been called
func GetStatus() MiddlewareStatus {
	client, ok := sharedClient.Current()
	if !ok {
		return MiddlewareStatus{}
	}
	return client.GetStatus()
}

// Enable toggles on metering emission for the globally initialized client
func Enable() {
	if client, ok := sharedClient.Current(); ok {
		client.Enable()
	}
}

// Disable toggles off metering emission for the globally initialized client
func Disable() {
	if client, ok := sharedClient.Current(); ok {
		client.Disable()
	}
}
