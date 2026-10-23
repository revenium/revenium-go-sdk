package fal

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

type ReveniumFal struct {
	config    *Config
	falClient *FalClient
	metering  *metering.MeteringClient
	enforcer  *enforcement.Engine
	mu        sync.RWMutex
	enabled   atomic.Bool
	abandoned core.BackgroundWaits
	// operations holds the operation type each endpoint's last reply
	// resolved to, so a call that gets no reply is recorded under the same
	// type as that endpoint's successful calls.
	operationsMu sync.RWMutex
	operations   map[string]metering.OperationType
}

var sharedClient = core.NewSharedClient("NewReveniumFal", func(c *ReveniumFal) *core.ReveniumConfig { return c.config.Revenium })

func Initialize(opts ...Option) error {
	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return sharedClient.Initialize(core.NamedAccount(cfg.Revenium), func() (*ReveniumFal, error) {
		return buildSharedClient(cfg)
	})
}

func buildSharedClient(cfg *Config) (*ReveniumFal, error) {
	core.InitializeLogger()
	core.Info("Initializing Revenium Fal.ai middleware...")

	if err := cfg.loadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	falClient, err := NewFalClient(cfg)
	if err != nil {
		return nil, err
	}

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	candidate := &ReveniumFal{
		config:    cfg,
		falClient: falClient,
		metering:  mc,
		enforcer:  enforcement.StartFor(cfg.Revenium),
	}
	candidate.enabled.Store(true)

	core.Info("Revenium Fal.ai middleware initialized successfully")
	return candidate, nil
}

func IsInitialized() bool {
	return sharedClient.IsInitialized()
}

func GetClient() (*ReveniumFal, error) {
	return sharedClient.Get()
}

func NewReveniumFal(cfg *Config) (*ReveniumFal, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}
	core.TrimReveniumConfig(cfg.Revenium)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	falClient, err := NewFalClient(cfg)
	if err != nil {
		return nil, err
	}

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	client := &ReveniumFal{
		config:    cfg,
		falClient: falClient,
		metering:  mc,
		enforcer:  enforcement.StartFor(cfg.Revenium),
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
	r.abandoned.Stop()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enforcer.Release()
	return r.metering.Close()
}

func Reset() {
	_ = sharedClient.ResetAndClose()
}
