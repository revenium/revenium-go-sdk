# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[1.0.0]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.0.0
