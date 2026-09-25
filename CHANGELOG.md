# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.7] - 2026-09-25

### Added

- **Core**: top-level `operationSubtype` on every multimodal metering payload — OpenAI audio and
  images, Google images and video, fal images, audio and video, Runway video. These payloads
  previously carried the subtype only inside `attributes` (fal audio carried none at all), so
  the metering API received none and logged a `*_MISSING_OPERATION_SUBTYPE` warning for each
  call, and audio priced without a resolved direction (FRONT-2796)
  - `MeteringPayload.OperationSubtype` (wire key `operationSubtype`, `omitempty`),
    `PayloadBuilder.WithOperationSubtype`, and the accepted vocabulary as `metering.Subtype*`
    constants
  - A caller-supplied `operationSubtype` in request metadata (`core.WithUsageMetadata`, or
    `runway.UsageMetadata.Custom`) takes precedence over the detected value. It is trimmed,
    lowercased and checked against the vocabulary for the payload's operation type; a value
    outside it is logged as a warning and ignored, so the metering API never answers 400 and
    drops the event. Chat and embedding payloads ignore the key

- **Core**: add agentic-job attribution fields to metering payloads, so the SDK can emit
  the job identity the metering API already accepts:
  - `MeteringPayload`: `AgenticJobID`, `AgenticJobName`, `AgenticJobType`, `AgenticJobVersion`
    (wire keys `agenticJobId`, `agenticJobName`, `agenticJobType`, `agenticJobVersion`) — all
    optional and `omitempty`, matching `AICompletionMetadataResource`
  - `ToolEventPayload`: `AgenticJobID` (wire key `agenticJobId`) — `ToolEventMetadataResource`
    defines the id only, so name/type/version are deliberately absent and would be dropped

  Both are settable through request metadata (`core.WithUsageMetadata`), which is the only
  attribution path available when a provider middleware builds the payload internally:
  `ApplyMetadata` and `ApplyToolEventMetadata` map the corresponding keys. `ToolEventBuilder`
  also gains `WithAgenticJobID`.

  `agenticJobId` alone is checked, at the metadata and builder set points, against the
  platform's published pattern (`JobValidation.AGENTIC_JOB_ID_PATTERN`): an id that would fail
  it is dropped with a debug log and the event is sent without job attribution, because the
  platform answers 400 and the whole completion or tool event — with its cost — would be lost.
  This matches the Python SDK and the Node CLI. `agenticJobName`, `agenticJobType` and
  `agenticJobVersion` have no server-side pattern and remain plain DTO fields with no
  validation, lowercasing, or length bounds; callers own their normalization. Direct struct
  assignment to any of the fields is not checked.

  `core/jobs` (`JobContextData`, `SetJobContext`/`GetJobContext`) remains
  deliberately unwired to the metering path: reading it implicitly would change behavior for
  existing users and require a precedence rule. Callers assign the fields directly.

### Changed

- Detected subtypes now use the vocabulary the metering API accepts: OpenAI speech ships as
  `tts` instead of `speech_synthesis`. fal audio is classified by endpoint as `transcription`,
  `tts` or `synthesis`; fal images as `upscale`, `inpainting`, `edit` or `generation`; fal
  video as `upscale`, `extend`, `edit` or `generation`. Runway ships `generation`
  (image-to-video), `edit` (video-to-video) and `upscale`
- `operationSubtype` removed from `attributes` on all multimodal payloads — it is now sent
  once, at the top level
- Every `Makefile` target — `test-all`, `build-all`, `lint-all`, `fmt-all`, `deps` — now runs
  `./examples` against its own module graph rather than the workspace. The examples module is
  deliberately absent from `go.work`, so running it inside the workspace failed outright and
  `make test-all` was red on `main` regardless of code state. The release workflow duplicated
  that same loop inline and would have failed the `v1.1.7` job before the GitHub Release was
  created; it now calls `make test-all`

### Fixed

- Every provider module required `core v1.1.4` while `openai`, `fal`, `google` and `runway`
  call core APIs introduced in this release, so `go get github.com/revenium/revenium-go-sdk/openai@v1.1.7`
  would not compile outside this repository's `go.work`. All ten provider modules now require
  `core v1.1.7`. The workspace resolved `core` locally and hid this from the test suite

  Each of them also gained `replace github.com/revenium/revenium-go-sdk/core => ../core`, which
  the repository's documentation already claimed was present. Go applies a `replace` only in the
  main module, so consumers still resolve `core v1.1.7` from the proxy; what it fixes is every
  build that happens inside the repository without the workspace — CI runs `go mod tidy` per
  module, and a single provider directory can now be built standalone from a clone

- **OpenAI**: `openai-go` moved from `v3.8.0` to `v3.44.0`. `AudioSpeechNewParams.Voice`
  became a union upstream, so `string(params.Voice)` failed to build for any consumer already
  on a recent `openai-go`. The `voice` attribute on speech metering now resolves the union —
  custom voice id, preset voice, or free-form string, and the code builds against every
  `openai-go` from `v3.30.0` on. `v3.44.0` is the newest release that still declares
  `go 1.22`; `v3.45.0` raises it to `go 1.25.0`, which would have forced every consumer of
  `openai` and `perplexity` onto Go 1.25. `perplexity` moves to the same version so the
  shared dependency stays aligned

- **Examples**: `job-metering` set a `JobOutcome.Status` field that does not exist (the field
  is `ExecutionStatus`) and printed the optional `JobResource.Type` pointer with `%s`

## [1.1.6] - 2026-07-31

### Added

- **Webhooks**: add HMAC signature verification helper with secret rotation support (FRONT-1690)
- **Bedrock**: capture token counts in streaming wrappers for InvokeModel and Converse (BACK-2418)

## [1.1.4] - 2026-07-23

### Fixed

- **Bedrock**: fix model-ID double-prefix, streaming wrapper, and add Converse/ConverseStream support (BACK-2360)

### Added

- **Runway**: add resolution to EditImage and UpscaleImage metering (BACK-787)
- **Core**: add ticketId field to metering payload (FRONT-1544)

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

- **Breaking** (entry added retroactively): **Core**: removed the `OrganizationID` and
  `ProductID` fields from `MeteringPayload` (FRONT-1240, #31). They serialized as
  `organizationId`/`productId`, which are not properties of any schema in the metering API —
  the backend silently dropped them. Input aliases were redirected so
  `metadata["organizationId"]` now populates `OrganizationName` and `metadata["productId"]`
  populates `ProductName`, with the canonical keys taking precedence when both are present.
  This shipped without a changelog entry and broke downstream consumers assigning the removed
  fields; they should assign `OrganizationName`/`ProductName` instead.
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

[1.1.7]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.7
[1.1.6]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.6
[1.1.4]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.4
[1.1.3]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.3
[1.1.2]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.2
[1.0.2]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.0.2
[1.0.1]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.0.1
[1.0.0]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.0.0
