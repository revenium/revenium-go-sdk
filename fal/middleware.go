package fal

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

type ReveniumFal struct {
	config    *Config
	falClient *FalClient
	metering  *metering.MeteringClient
	mu        sync.RWMutex
	enabled   atomic.Bool
}

var (
	globalClient *ReveniumFal
	globalMu     sync.RWMutex
	initialized  bool
)

func Initialize(opts ...Option) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if initialized {
		return nil
	}

	core.InitializeLogger()
	core.Info("Initializing Revenium Fal.ai middleware...")

	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}

	if err := cfg.loadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	falClient, err := NewFalClient(cfg)
	if err != nil {
		return err
	}

	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	if err != nil {
		return err
	}

	globalClient = &ReveniumFal{
		config:    cfg,
		falClient: falClient,
		metering:  mc,
	}
	globalClient.enabled.Store(true)

	initialized = true
	core.Info("Revenium Fal.ai middleware initialized successfully")
	return nil
}

func IsInitialized() bool {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return initialized
}

func GetClient() (*ReveniumFal, error) {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if !initialized {
		return nil, core.NewConfigError("middleware not initialized, call Initialize() first", nil)
	}

	return globalClient, nil
}

func NewReveniumFal(cfg *Config) (*ReveniumFal, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	falClient, err := NewFalClient(cfg)
	if err != nil {
		return nil, err
	}

	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	if err != nil {
		return nil, err
	}

	client := &ReveniumFal{
		config:    cfg,
		falClient: falClient,
		metering:  mc,
	}
	client.enabled.Store(true)
	return client, nil
}

func (r *ReveniumFal) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

func (r *ReveniumFal) GenerateImage(ctx context.Context, model string, request *FalRequest) (*FalImageResponse, error) {
	result, err := r.runTyped(ctx, model, request)
	if err != nil {
		return nil, err
	}
	return imageFromMap(result), nil
}

func (r *ReveniumFal) GenerateVideo(ctx context.Context, model string, request *FalRequest) (*FalVideoResponse, error) {
	result, err := r.runTyped(ctx, model, request)
	if err != nil {
		return nil, err
	}
	return videoFromMap(result), nil
}

func (r *ReveniumFal) GenerateAudio(ctx context.Context, model string, request *FalRequest) (*FalAudioResponse, error) {
	result, err := r.runTyped(ctx, model, request)
	if err != nil {
		return nil, err
	}
	return audioFromMap(result), nil
}

func (r *ReveniumFal) runTyped(ctx context.Context, model string, request *FalRequest) (map[string]interface{}, error) {
	endpointID := "fal-ai/" + getEndpointPath(model)
	input, err := falRequestToMap(request)
	if err != nil {
		return nil, core.NewProviderError("failed to encode request", err)
	}
	return r.Run(ctx, endpointID, input, nil)
}

func falRequestToMap(request *FalRequest) (map[string]interface{}, error) {
	if request == nil {
		return map[string]interface{}{}, nil
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	for k, v := range request.AdditionalParams {
		out[k] = v
	}
	return out, nil
}

func (r *ReveniumFal) Flush() {
	r.metering.Flush()
}

func (r *ReveniumFal) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.metering.Close()
}

func Reset() {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalClient != nil {
		globalClient.Close()
		globalClient = nil
	}

	initialized = false
}
