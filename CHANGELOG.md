# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.3] - 2026-07-09

### Added

- **Store-and-forward buffer** for metering events that exhaust retries or are rejected by the circuit breaker, preventing permanent event loss during backend outages
- **Automatic replay** of buffered events every 30 seconds (configurable) with stop-on-first-failure to avoid hammering a down backend
- **Bounded buffer** with configurable max size (default 1000), FIFO eviction, and 24-hour event TTL aligned with backend IdempotencyKey window
- **Graceful shutdown** integration: `Flush()` and `Close()` drain the buffer before returning
- **`GetBufferStats()`** exported for programmatic observability (size, capacity, events replayed, events evicted)
- **`BufferMaxSize`** and **`BufferFlushInterval`** fields on `MeteringClientConfig`

## [1.1.2] - 2026-07-06

### Added

- **Outcome amendment** via `AmendJobOutcome(jobID, amendment)` using PATCH endpoint
- **Outcome history** via `GetJobOutcomeHistory(jobID)` returning ordered amendment entries
- **Typed error `OutcomeAlreadyReportedError`** returned by `ReportJobOutcome` on 409 with structured body, exposing `JobID`, `ReportedAt`, and `AmendmentCount`
- **Typed error `OutcomeNotReportedError`** returned by `AmendJobOutcome` on 422 (job has no outcome)
- **Typed error `OutcomeAmendConflictError`** returned by `AmendJobOutcome` on 409 (concurrent amendment)
- **New fields on `JobResource`**: `OutcomeAmendmentCount`, `OutcomeUpdatedAt`, `OutcomeUpdatedBy`
- **Amend outcome example** in `examples/amend-outcome/`

### Changed

- **Breaking**: `ReportJobOutcome` now returns `*OutcomeAlreadyReportedError` on 409 responses with structured body instead of `nil, nil`. Callers relying on the previous silent `nil, nil` behavior must handle the new error type. Falls back to `nil, nil` for 409 responses without structured body (backward-compatible with older backends).

## [1.0.2] - 2026-05-08

### Added

- **Enforcement engine** with rule polling and pre-call checks
- **Filter scope derivation** from filter dimensions in enforcement rules
- **Enforcement filters test suite**

### Changed

- Renamed SDK terminology for consistency (`hak_` prefix references updated)

## [1.0.1] - 2026-04-27

### Added

- **Runway video generation example** in `examples/runway/video/`

### Changed

- API key validation now accepts both `hak_` and `rev_` prefixes

### Fixed

- Error handling in Runway example for `json.MarshalIndent`
- Assertion robustness in LiteLLM config test

## [1.0.0] - 2026-04-14

### Added

- **Multi-provider support** with consistent `Initialize()` / `GetClient()` API across all providers
- **OpenAI** module with chat completions, streaming, embeddings, images, audio, and Responses API support
- **Azure OpenAI** auto-detection when Azure environment variables are present
- **Anthropic** module with messages, streaming, and response reconstruction from chunks
- **Anthropic Bedrock** auto-detection when AWS credentials and `AWS_BEDROCK_ENABLED=true` are set
- **Google GenAI** module with content generation, streaming, embeddings, and image generation
- **Google Vertex AI** auto-detection when `GOOGLE_CLOUD_PROJECT` is set
- **Perplexity** module using OpenAI-compatible API with chat completions and streaming
- **LiteLLM** module with proxy support, runtime `Enable()` / `Disable()`, and provider detection from model IDs
- **fal.ai** module with `Run()`, `Subscribe()`, `Stream()`, and automatic media type detection (image, video, audio, chat)
- **Runway** module for video generation via Runway ML API
- **Ollama** module for local LLM inference via Ollama
- **Groq** module for Groq cloud inference
- **Grok (xAI)** module for xAI API
- **Core module** with shared utilities: config, errors, logging, context helpers
- **Metering client** with fire-and-forget async sends via goroutines
- **Tool metering** via fluent `ToolEventBuilder` API
- **Job outcomes** tracking with ROI metrics, conversion funnels, and paginated listing
- **Prompt capture** with optional credential sanitization
- **Resilience** package with circuit breaker, exponential-backoff retry with jitter, and error classification
- **Streaming support** with token accumulation and first-token timing across all chat providers
- **Usage metadata** and subscriber context via `core.WithUsageMetadata()` / `core.WithSubscriber()`
- **Trace visualization** fields for distributed tracing and analytics
- **Automatic .env loading** via `core.LoadEnvFiles()`
- **Multi-module layout** so consumers pull only the providers they need
- **CI/CD pipeline** with GitHub Actions for automated testing across all modules

[1.1.3]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.3
[1.1.2]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.2
[1.0.2]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.0.2
[1.0.1]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.0.1
[1.0.0]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.0.0
