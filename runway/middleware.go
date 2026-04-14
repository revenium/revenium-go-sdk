package runway

import (
	"context"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

type ReveniumRunway struct {
	runwayClient *RunwayClient
	metering     *metering.MeteringClient
	config       *Config
	mu           sync.RWMutex
}

var (
	globalClient *ReveniumRunway
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
	core.Info("Initializing Revenium Runway middleware...")

	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}

	if err := cfg.LoadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	runwayClient := NewRunwayClient(cfg)

	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	if err != nil {
		return err
	}

	globalClient = &ReveniumRunway{
		runwayClient: runwayClient,
		metering:     mc,
		config:       cfg,
	}

	initialized = true
	core.Info("Revenium Runway middleware initialized successfully")
	return nil
}

func IsInitialized() bool {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return initialized
}

func GetClient() (*ReveniumRunway, error) {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if !initialized {
		return nil, core.NewConfigError("middleware not initialized, call Initialize() first", nil)
	}

	return globalClient, nil
}

func NewReveniumRunway(cfg *Config) (*ReveniumRunway, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	runwayClient := NewRunwayClient(cfg)

	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	if err != nil {
		return nil, err
	}

	return &ReveniumRunway{
		runwayClient: runwayClient,
		metering:     mc,
		config:       cfg,
	}, nil
}

func (r *ReveniumRunway) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

func (r *ReveniumRunway) ImageToVideo(ctx context.Context, req *ImageToVideoRequest, metadata *UsageMetadata) (*VideoGenerationResult, error) {
	startTime := time.Now()

	if req.Model == "" {
		req.Model = "gen3a_turbo"
	}

	core.Debug("Creating image-to-video task with model: %s", req.Model)
	taskResp, err := r.runwayClient.CreateImageToVideo(ctx, req)
	if err != nil {
		return nil, err
	}

	core.Info("Waiting for task %s to complete...", taskResp.ID)
	statusResp, err := r.runwayClient.WaitForTaskCompletion(ctx, taskResp.ID, DefaultPollingConfig())
	if err != nil {
		return nil, err
	}

	duration := time.Since(startTime)
	result := &VideoGenerationResult{
		ID:         taskResp.ID,
		Status:     statusResp.Status,
		OutputURLs: statusResp.Output,
		Duration:   duration,
		Model:      req.Model,
		Metadata:   make(map[string]interface{}),
	}

	if req.Duration > 0 {
		result.Metadata["requestedDuration"] = req.Duration
	} else {
		result.Metadata["requestedDuration"] = 5
	}

	if r.config.CapturePrompts && req.PromptText != "" {
		result.Metadata["_capturedPrompt"] = req.PromptText
	}

	if statusResp.Error != nil {
		result.Error = statusResp.Error
	}
	if statusResp.FailureCode != nil {
		result.FailureCode = statusResp.FailureCode
	}

	payload := buildVideoMeteringPayload(result, metadata, r.config.CapturePrompts, startTime)
	r.metering.Send(payload)

	return result, nil
}

func (r *ReveniumRunway) VideoToVideo(ctx context.Context, req *VideoToVideoRequest, metadata *UsageMetadata) (*VideoGenerationResult, error) {
	startTime := time.Now()

	if req.Model == "" {
		req.Model = "gen3a_turbo"
	}

	core.Debug("Creating video-to-video task with model: %s", req.Model)
	taskResp, err := r.runwayClient.CreateVideoToVideo(ctx, req)
	if err != nil {
		return nil, err
	}

	core.Info("Waiting for task %s to complete...", taskResp.ID)
	statusResp, err := r.runwayClient.WaitForTaskCompletion(ctx, taskResp.ID, DefaultPollingConfig())
	if err != nil {
		return nil, err
	}

	duration := time.Since(startTime)
	result := &VideoGenerationResult{
		ID:         taskResp.ID,
		Status:     statusResp.Status,
		OutputURLs: statusResp.Output,
		Duration:   duration,
		Model:      req.Model,
		Metadata:   make(map[string]interface{}),
	}

	if req.Duration > 0 {
		result.Metadata["requestedDuration"] = req.Duration
	} else {
		result.Metadata["requestedDuration"] = 5
	}

	if r.config.CapturePrompts && req.PromptText != "" {
		result.Metadata["_capturedPrompt"] = req.PromptText
	}

	if statusResp.Error != nil {
		result.Error = statusResp.Error
	}
	if statusResp.FailureCode != nil {
		result.FailureCode = statusResp.FailureCode
	}

	payload := buildVideoMeteringPayload(result, metadata, r.config.CapturePrompts, startTime)
	r.metering.Send(payload)

	return result, nil
}

func (r *ReveniumRunway) UpscaleVideo(ctx context.Context, req *VideoUpscaleRequest, metadata *UsageMetadata) (*VideoGenerationResult, error) {
	startTime := time.Now()

	if req.Model == "" {
		req.Model = "upscale"
	}

	core.Debug("Creating video upscale task with model: %s", req.Model)
	taskResp, err := r.runwayClient.CreateVideoUpscale(ctx, req)
	if err != nil {
		return nil, err
	}

	core.Info("Waiting for task %s to complete...", taskResp.ID)
	statusResp, err := r.runwayClient.WaitForTaskCompletion(ctx, taskResp.ID, DefaultPollingConfig())
	if err != nil {
		return nil, err
	}

	duration := time.Since(startTime)
	result := &VideoGenerationResult{
		ID:         taskResp.ID,
		Status:     statusResp.Status,
		OutputURLs: statusResp.Output,
		Duration:   duration,
		Model:      req.Model,
	}

	if statusResp.Error != nil {
		result.Error = statusResp.Error
	}
	if statusResp.FailureCode != nil {
		result.FailureCode = statusResp.FailureCode
	}

	payload := buildVideoMeteringPayload(result, metadata, r.config.CapturePrompts, startTime)
	r.metering.Send(payload)

	return result, nil
}

func (r *ReveniumRunway) Flush() {
	r.metering.Flush()
}

func (r *ReveniumRunway) Close() error {
	r.Flush()

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.runwayClient.Close(); err != nil {
		return err
	}

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
