package fal

func (r *ReveniumFal) GetStatus() MiddlewareStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	status := MiddlewareStatus{
		Initialized: r.config != nil,
		Enabled:     r.enabled.Load(),
		HasConfig:   r.config != nil,
	}
	if r.config != nil {
		status.BaseURL = r.config.FalBaseURL
	}
	return status
}

func (r *ReveniumFal) Enable() {
	r.enabled.Store(true)
}

func (r *ReveniumFal) Disable() {
	r.enabled.Store(false)
}

func (r *ReveniumFal) IsEnabled() bool {
	return r.enabled.Load()
}

func GetStatus() MiddlewareStatus {
	client, ok := sharedClient.Current()
	if !ok {
		return MiddlewareStatus{}
	}
	return client.GetStatus()
}

func Enable() {
	if client, ok := sharedClient.Current(); ok {
		client.Enable()
	}
}

func Disable() {
	if client, ok := sharedClient.Current(); ok {
		client.Disable()
	}
}
