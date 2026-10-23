package runway

import (
	"context"
	"errors"
	"sync"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

type ReveniumRunway struct {
	runwayClient *RunwayClient
	metering     *metering.MeteringClient
	config       *Config
	mu           sync.RWMutex
	enforcer     *enforcement.Engine
	polling      *PollingConfig
	abandoned    core.BackgroundWaits
}

var sharedClient = core.NewSharedClient("NewReveniumRunway", func(c *ReveniumRunway) *core.ReveniumConfig { return c.config.Revenium })

func Initialize(opts ...Option) error {
	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return sharedClient.Initialize(core.NamedAccount(cfg.Revenium), func() (*ReveniumRunway, error) {
		return buildSharedClient(cfg)
	})
}

func buildSharedClient(cfg *Config) (*ReveniumRunway, error) {
	core.InitializeLogger()
	core.Info("Initializing Revenium Runway middleware...")

	if err := cfg.LoadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	runwayClient := NewRunwayClient(cfg)

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	candidate := &ReveniumRunway{
		runwayClient: runwayClient,
		metering:     mc,
		config:       cfg,
		enforcer:     enforcement.StartFor(cfg.Revenium),
	}

	core.Info("Revenium Runway middleware initialized successfully")
	return candidate, nil
}

func IsInitialized() bool {
	return sharedClient.IsInitialized()
}

func GetClient() (*ReveniumRunway, error) {
	return sharedClient.Get()
}

func NewReveniumRunway(cfg *Config) (*ReveniumRunway, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}
	core.TrimReveniumConfig(cfg.Revenium)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	runwayClient := NewRunwayClient(cfg)

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	return &ReveniumRunway{
		runwayClient: runwayClient,
		metering:     mc,
		config:       cfg,
		enforcer:     enforcement.StartFor(cfg.Revenium),
	}, nil
}

func (r *ReveniumRunway) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

// ImageToVideo, VideoToVideo and UpscaleVideo record the usage metadata set on ctx with
// core.WithUsageMetadata together with the metadata argument; when both set a field, the
// context value wins, as it does for the fal module's Run, Subscribe and Stream.
func (r *ReveniumRunway) ImageToVideo(ctx context.Context, req *ImageToVideoRequest, metadata *UsageMetadata) (*VideoGenerationResult, error) {
	if req.Model == "" {
		req.Model = "gen3a_turbo"
	}
	videoSeconds := billedVideoSeconds(req.Model, float64(req.Duration), 0)
	callMetadata := recordMetadata(ctx, metadata)
	return r.runVideoTask(ctx, videoTask{
		name: "image-to-video",
		record: taskRecord{
			model:        req.Model,
			subtype:      metering.SubtypeGeneration,
			videoSeconds: videoSeconds,
			metadata:     callMetadata,
			resultInfo:   r.generationResultInfo(videoSeconds, req.PromptText, callMetadata),
		},
		create: func(ctx context.Context) (*TaskResponse, error) {
			return r.runwayClient.CreateImageToVideo(ctx, req)
		},
	})
}

func (r *ReveniumRunway) VideoToVideo(ctx context.Context, req *VideoToVideoRequest, metadata *UsageMetadata) (*VideoGenerationResult, error) {
	if req.Model == "" {
		req.Model = "gen3a_turbo"
	}
	videoSeconds := billedVideoSeconds(req.Model, float64(req.Duration), req.SourceDurationSeconds)
	callMetadata := recordMetadata(ctx, metadata)
	return r.runVideoTask(ctx, videoTask{
		name: "video-to-video",
		record: taskRecord{
			model:        req.Model,
			subtype:      metering.SubtypeEdit,
			videoSeconds: videoSeconds,
			metadata:     callMetadata,
			resultInfo:   r.generationResultInfo(videoSeconds, req.PromptText, callMetadata),
		},
		create: func(ctx context.Context) (*TaskResponse, error) {
			return r.runwayClient.CreateVideoToVideo(ctx, req)
		},
	})
}

func (r *ReveniumRunway) UpscaleVideo(ctx context.Context, req *VideoUpscaleRequest, metadata *UsageMetadata) (*VideoGenerationResult, error) {
	if !isBillableSeconds(req.SourceDurationSeconds) {
		return nil, ErrMissingSourceDuration
	}
	if req.Model == "" {
		req.Model = "upscale"
	}
	return r.runVideoTask(ctx, videoTask{
		name: "video upscale",
		record: taskRecord{
			model:        req.Model,
			subtype:      metering.SubtypeUpscale,
			videoSeconds: req.SourceDurationSeconds,
			metadata:     recordMetadata(ctx, metadata),
		},
		create: func(ctx context.Context) (*TaskResponse, error) {
			return r.runwayClient.CreateVideoUpscale(ctx, req)
		},
	})
}

func (r *ReveniumRunway) Flush() {
	r.metering.Flush()
}

func (r *ReveniumRunway) Close() error {
	r.abandoned.Stop()

	r.mu.Lock()
	defer r.mu.Unlock()

	r.enforcer.Release()
	return errors.Join(r.runwayClient.Close(), r.metering.Close())
}

func Reset() {
	_ = sharedClient.ResetAndClose()
}
