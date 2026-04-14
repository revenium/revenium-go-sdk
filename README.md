# Revenium Middleware for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/revenium/revenium-go-sdk.svg)](https://pkg.go.dev/github.com/revenium/revenium-go-sdk)
[![Go 1.21+](https://img.shields.io/badge/Go-1.21%2B-00ADD8)](https://go.dev/)
[![Documentation](https://img.shields.io/badge/docs-revenium.io-blue)](https://docs.revenium.io)
[![Website](https://img.shields.io/badge/website-revenium.ai-blue)](https://www.revenium.ai)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Unified Go SDK for automatic AI usage tracking across multiple providers**

A production-grade Go SDK that integrates with OpenAI, Azure OpenAI, Anthropic (incl. Bedrock), Google (GenAI + Vertex AI), Perplexity, LiteLLM, fal.ai, Runway, Ollama, Groq, and Grok (xAI) to provide automatic usage tracking, billing analytics, and metadata collection. Multi-module layout so consumers pull only the providers they need. Consistent `Initialize()` / `GetClient()` API across every module.

## Features

- **Multi-Provider Support** — OpenAI, Azure OpenAI, Anthropic, Anthropic on Bedrock, Google GenAI, Google Vertex AI, Perplexity, LiteLLM, fal.ai, Runway, Ollama, Groq, Grok (xAI)
- **Consistent API** — Same `Initialize()` / `GetClient()` / `NewRevenium*()` pattern across all providers
- **Multi-Module Layout** — Each provider is its own Go module; pull only what you import
- **Fire-and-Forget Metering** — Async sends via goroutines; never blocks your request path
- **Streaming Support** — First-class streaming wrappers for OpenAI, Anthropic, Google, Perplexity, LiteLLM, and fal.ai with token accumulation and first-token timing
- **Resilience Built-in** — Circuit breaker, exponential-backoff retry with jitter, and error classification shipped in `core/resilience`
- **Tool & Job Metering** — Report custom tool calls and long-running job outcomes via `core/metering` and `core/jobs`
- **Prompt Capture** — Optional, credential-sanitizing capture of system / input / output prompts
- **Automatic .env Loading** — `core.LoadEnvFiles()` picks up `.env` automatically in local development

## Supported Providers

| Provider         | Import Path                                            | API Pattern                                               |
| ---------------- | ------------------------------------------------------ | --------------------------------------------------------- |
| OpenAI           | `github.com/revenium/revenium-go-sdk/openai`           | `Initialize(opts...)` / `GetClient()`                     |
| Azure OpenAI     | `github.com/revenium/revenium-go-sdk/openai`           | `Initialize(opts...)` / `GetClient()` (auto-detected)     |
| Anthropic        | `github.com/revenium/revenium-go-sdk/anthropic`        | `Initialize(opts...)` / `GetClient()`                     |
| Anthropic Bedrock| `github.com/revenium/revenium-go-sdk/anthropic`        | Auto-detected when AWS env vars are present               |
| Google GenAI     | `github.com/revenium/revenium-go-sdk/google`           | `Initialize(opts...)` / `GetClient()`                     |
| Google Vertex AI | `github.com/revenium/revenium-go-sdk/google`           | Auto-detected when `GOOGLE_CLOUD_PROJECT` is set          |
| Perplexity       | `github.com/revenium/revenium-go-sdk/perplexity`       | `Initialize(opts...)` / `GetClient()`                     |
| LiteLLM          | `github.com/revenium/revenium-go-sdk/litellm`          | `Initialize(opts...)` / `Enable()` / `Disable()`          |
| fal.ai           | `github.com/revenium/revenium-go-sdk/fal`              | `Initialize(opts...)` / `Run()` / `Subscribe()` / `Stream()` |
| Runway           | `github.com/revenium/revenium-go-sdk/runway`           | `Initialize(opts...)` / `GetClient()`                     |
| Ollama           | `github.com/revenium/revenium-go-sdk/ollama`           | `Initialize(opts...)` / `GetClient()`                     |
| Groq             | `github.com/revenium/revenium-go-sdk/groq`             | `Initialize(opts...)` / `GetClient()`                     |
| Grok (xAI)       | `github.com/revenium/revenium-go-sdk/grok`             | `Initialize(opts...)` / `GetClient()`                     |
| Tool Metering    | `github.com/revenium/revenium-go-sdk/core/metering`    | `ToolEventBuilder` / `MeteringClient.SendToolEvent()`     |
| Job Outcomes     | `github.com/revenium/revenium-go-sdk/core/jobs`        | `JobClient.ReportJobOutcome()` / `ListJobs()` / etc.      |

## Getting Started

### Installation

```bash
# Install only the providers you need
go get github.com/revenium/revenium-go-sdk/openai
go get github.com/revenium/revenium-go-sdk/anthropic
go get github.com/revenium/revenium-go-sdk/litellm
go get github.com/revenium/revenium-go-sdk/fal
```

Each provider module pulls its own upstream SDK (`openai-go`, `anthropic-sdk-go`, `genai`, etc.) transitively.

### Configuration

Create a `.env` file in your project root:

```env
REVENIUM_METERING_API_KEY=hak_your_revenium_api_key_here
REVENIUM_METERING_BASE_URL=https://api.revenium.ai
```

Plus the API key for your chosen provider (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, etc.).

### Quick Start — OpenAI

```go
package main

import (
    "context"
    openai "github.com/openai/openai-go/v3"
    reveniumopenai "github.com/revenium/revenium-go-sdk/openai"
)

func main() {
    if err := reveniumopenai.Initialize(); err != nil {
        panic(err)
    }
    client, err := reveniumopenai.GetClient()
    if err != nil {
        panic(err)
    }
    defer client.Close()

    resp, err := client.Chat().Completions().New(context.Background(), openai.ChatCompletionNewParams{
        Model: "gpt-4o-mini",
        Messages: []openai.ChatCompletionMessageParamUnion{
            openai.UserMessage("Hello!"),
        },
    })
    if err != nil {
        panic(err)
    }
    println(resp.Choices[0].Message.Content)
}
```

### Quick Start — Azure OpenAI

Azure is auto-detected when `AZURE_OPENAI_API_KEY` and `AZURE_OPENAI_ENDPOINT` are set. Same `Initialize()` / `GetClient()` API.

```go
reveniumopenai.Initialize()
client, _ := reveniumopenai.GetClient()
// client.Chat().Completions().New(...) — model is the Azure deployment name
```

### Quick Start — Anthropic

```go
package main

import (
    "context"
    anthropic "github.com/anthropics/anthropic-sdk-go"
    reveniumanthropic "github.com/revenium/revenium-go-sdk/anthropic"
)

func main() {
    reveniumanthropic.Initialize()
    client, _ := reveniumanthropic.GetClient()
    defer client.Close()

    msg, _ := client.Messages().CreateMessage(context.Background(), anthropic.MessageNewParams{
        Model:     "claude-sonnet-4-20250514",
        MaxTokens: 1024,
        Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("Hello!"))},
    })
    _ = msg
}
```

Bedrock is auto-detected when `AWS_BEDROCK_ENABLED=true` along with the AWS credentials.

### Quick Start — Google GenAI / Vertex AI

```go
package main

import (
    "context"
    reveniumgoogle "github.com/revenium/revenium-go-sdk/google"
    "google.golang.org/genai"
)

func main() {
    reveniumgoogle.Initialize()
    client, _ := reveniumgoogle.GetClient()
    defer client.Close()

    resp, _ := client.Models().GenerateContent(
        context.Background(),
        "gemini-2.0-flash",
        []*genai.Content{genai.NewContentFromText("Hello!", "user")},
        nil,
    )
    _ = resp
}
```

Vertex AI is auto-detected when `GOOGLE_CLOUD_PROJECT` is set (uses `GOOGLE_APPLICATION_CREDENTIALS` for auth).

### Quick Start — Perplexity

```go
package main

import (
    "context"
    openai "github.com/openai/openai-go/v3"
    reveniumperplexity "github.com/revenium/revenium-go-sdk/perplexity"
)

func main() {
    reveniumperplexity.Initialize()
    client, _ := reveniumperplexity.GetClient()
    defer client.Close()

    resp, _ := client.Chat().Completions().New(context.Background(), openai.ChatCompletionNewParams{
        Model:    "sonar",
        Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("Hello!")},
    })
    _ = resp
}
```

### Quick Start — LiteLLM

The LiteLLM middleware supports runtime `Enable()` / `Disable()` and a `GetStatus()` introspection method.

```go
package main

import (
    "context"
    reveniumlitellm "github.com/revenium/revenium-go-sdk/litellm"
)

func main() {
    reveniumlitellm.Initialize()
    client, _ := reveniumlitellm.GetClient()
    defer client.Close()

    resp, _ := client.Chat().Completions().New(context.Background(), reveniumlitellm.ChatCompletionRequest{
        Model: "openai/gpt-4o-mini",
        Messages: []reveniumlitellm.ChatMessage{
            {Role: "user", Content: "Hello!"},
        },
    })
    _ = resp

    // Toggle at runtime
    reveniumlitellm.Disable()
    reveniumlitellm.Enable()

    status := reveniumlitellm.GetStatus()
    _ = status // {Initialized, Enabled, HasConfig, ProxyURL}
}
```

### Quick Start — fal.ai

```go
package main

import (
    "context"
    reveniumfal "github.com/revenium/revenium-go-sdk/fal"
)

func main() {
    reveniumfal.Initialize()
    client, _ := reveniumfal.GetClient()
    defer client.Close()

    // Image generation (auto-detected media type)
    res, _ := client.Run(context.Background(),
        "fal-ai/flux/schnell",
        map[string]interface{}{"prompt": "a futuristic cityscape at sunset"},
        nil,
    )
    _ = res

    // Queue-based (long-running) execution
    video, _ := client.Subscribe(context.Background(),
        "fal-ai/kling-video/v1/standard/text-to-video",
        map[string]interface{}{"prompt": "ocean waves crashing on rocks", "duration": "5"},
        nil,
    )
    _ = video

    // Streaming execution
    events, _ := client.Stream(context.Background(),
        "fal-ai/openrouter/llama-3",
        map[string]interface{}{"prompt": "Explain quantum computing"},
        nil,
    )
    for ev := range events {
        _ = ev // ev.Data, ev.Partial, ev.Done, ev.Error
    }

    // Legacy typed methods still supported
    img, _ := client.GenerateImage(context.Background(), "fal-ai/flux/schnell", &reveniumfal.FalRequest{
        Prompt: "a cat in space",
    })
    _ = img
}
```

The middleware automatically detects the media type from the endpoint ID and routes metering data to the correct Revenium endpoint. Accepts `FAL_KEY` or `FAL_API_KEY` env var.

### Streaming Example — OpenAI

All chat-capable providers (OpenAI, Anthropic, Google, Perplexity, LiteLLM, fal.ai) expose streaming wrappers. OpenAI example:

```go
package main

import (
    "context"
    "fmt"

    openai "github.com/openai/openai-go/v3"
    reveniumopenai "github.com/revenium/revenium-go-sdk/openai"
)

func main() {
    if err := reveniumopenai.Initialize(); err != nil {
        panic(err)
    }
    client, err := reveniumopenai.GetClient()
    if err != nil {
        panic(err)
    }
    defer client.Close()

    stream, err := client.Chat().Completions().NewStreaming(context.Background(), openai.ChatCompletionNewParams{
        Model:    "gpt-4o-mini",
        Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("Write a haiku about Go")},
    })
    if err != nil {
        panic(err)
    }

    for stream.Next() {
        chunk := stream.Current()
        if len(chunk.Choices) > 0 {
            fmt.Print(chunk.Choices[0].Delta.Content)
        }
    }
    if err := stream.Err(); err != nil {
        panic(err)
    }
    // Close() triggers the final metering payload with isStreamed=true and timeToFirstToken.
    if err := stream.Close(); err != nil {
        panic(err)
    }
}
```

The same `Next()` / `Current()` / `Err()` / `Close()` pattern applies to Anthropic (`Messages().CreateMessageStream()`), Google (`Models().GenerateContentStream()`), Perplexity (`Chat().Completions().NewStreaming()`), and LiteLLM (`Chat().Completions().NewStreaming()`). fal.ai streaming uses a channel: `events, err := client.Stream(ctx, endpointID, input, metadata)`.

### Error Handling Pattern

Metering errors never surface to your application — they are logged and swallowed (respecting `REVENIUM_FAIL_SILENT`). Upstream provider errors are returned normally:

```go
resp, err := client.Chat().Completions().New(ctx, params)
if err != nil {
    var revErr *core.ReveniumError
    if errors.As(err, &revErr) {
        // Check the typed error category
        switch revErr.Type {
        case core.ErrorTypeNetwork:
            // retryable transport failure
        case core.ErrorTypeValidation:
            // 4xx from the provider
        case core.ErrorTypeProvider:
            // 5xx from the provider
        }
    }
    return err
}
```

The `core.ReveniumError` type wraps HTTP status, category, and an optional underlying `error`. Use `core.IsConfigError(err)`, `errors.As`, or `revErr.Type` to branch.

### Quick Start — Groq / Grok / Ollama / Runway

All follow the same `Initialize()` / `GetClient()` / `Close()` pattern:

```go
import reveniumgroq "github.com/revenium/revenium-go-sdk/groq"

reveniumgroq.Initialize()
client, _ := reveniumgroq.GetClient()
defer client.Close()
// client.Chat().Completions().New(ctx, req)
```

## Usage Metadata & Context

Attach per-request metadata via `context.Context`. Metering payloads automatically pick up these fields.

```go
import "github.com/revenium/revenium-go-sdk/core"

ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
    "traceId":     "session-123",
    "productName": "my-product",
    "taskType":    "chat",
    "agent":       "my-agent",
})

resp, _ := client.Chat().Completions().New(ctx, req)
```

Or use a typed subscriber:

```go
ctx = core.WithSubscriber(ctx, &core.Subscriber{
    ID:    "user-42",
    Email: "user@example.com",
})
```

## API Reference

### OpenAI

| Function                      | Description                                   |
| ----------------------------- | --------------------------------------------- |
| `Initialize(opts ...Option)`  | Initialize global middleware from env + opts  |
| `GetClient()`                 | Return the global `*ReveniumOpenAI` instance  |
| `NewReveniumOpenAI(cfg)`      | Construct a standalone instance               |
| `IsInitialized()`             | Report global initialization state            |
| `GetOpenAIClient()`           | Return the underlying wrapped `openai.Client` |
| `GetProvider()`               | `ProviderOpenAI` / `ProviderAzure`            |
| `Chat()` / `Embeddings()` / `Images()` / `Audio()` / `Responses()` | Typed interfaces for each operation |
| `Flush()` / `Close()`         | Flush pending metering / close client         |

### Anthropic

| Function                     | Description                                 |
| ---------------------------- | ------------------------------------------- |
| `Initialize(opts ...Option)` | Initialize global middleware                |
| `GetClient()`                | Return the global `*ReveniumAnthropic`      |
| `NewReveniumAnthropic(cfg)`  | Construct standalone instance               |
| `Reset()`                    | Reset global state                          |
| `Messages().CreateMessage()` | Non-streaming message creation              |
| `Messages().CreateMessageStream()` | Streaming wrapper                     |
| `ReconstructResponseFromChunks()` | Rebuild `*anthropic.Message` from a streaming wrapper |

### Google (GenAI + Vertex AI)

| Function                   | Description                                    |
| -------------------------- | ---------------------------------------------- |
| `Initialize(opts ...Option)` | Initialize global middleware                 |
| `GetClient()`              | Return the global `*ReveniumGoogle`            |
| `NewReveniumGoogle(cfg)`   | Construct standalone instance                  |
| `Reset()`                  | Reset global state                             |
| `Models().GenerateContent()` / `GenerateContentStream()` | Chat / streaming |
| `Models().CreateEmbedding()` | Embeddings                                   |
| `Models().GenerateImage()` / `EditImage()` / `UpscaleImage()` | Image gen/edit |
| `ExtractConfidenceScore()` | Extract confidence from candidate logprobs    |

### LiteLLM

| Function                     | Description                                 |
| ---------------------------- | ------------------------------------------- |
| `Initialize(opts ...Option)` | Initialize from env / options               |
| `GetClient()`                | Return the global `*ReveniumLiteLLM`        |
| `NewReveniumLiteLLM(cfg)`    | Construct standalone instance               |
| `ResetGlobalState()`         | Reset global state                          |
| `Enable()` / `Disable()`     | Toggle metering emission at runtime         |
| `IsEnabled()`                | Report current enable state                 |
| `GetStatus()`                | `MiddlewareStatus{Initialized, Enabled, HasConfig, ProxyURL}` |
| `ExtractProvider()` / `ExtractModelSource()` / `ExtractModelName()` | Provider detection from LiteLLM model IDs |
| `IsValidModelFormat()`       | Validate model ID format                    |

### fal.ai

| Function                     | Description                                               |
| ---------------------------- | --------------------------------------------------------- |
| `Initialize(opts ...Option)` | Initialize from env / options                             |
| `GetClient()`                | Return the global `*ReveniumFal`                          |
| `NewReveniumFal(cfg)`        | Construct standalone instance                             |
| `Reset()`                    | Reset global state                                        |
| `Enable()` / `Disable()`     | Toggle metering emission at runtime                       |
| `GetStatus()`                | `MiddlewareStatus{Initialized, Enabled, HasConfig, BaseURL}` |

**Client Methods:**

| Method                                               | Description                                         |
| ---------------------------------------------------- | --------------------------------------------------- |
| `client.Run(ctx, endpointID, input, metadata)`       | Direct execution; auto-detected media type          |
| `client.Subscribe(ctx, endpointID, input, metadata)` | Queue-based execution with polling                  |
| `client.Stream(ctx, endpointID, input, metadata)`    | Streaming execution returning `<-chan StreamEvent`  |
| `client.GenerateImage() / GenerateVideo() / GenerateAudio()` | Legacy typed helpers (delegate to `Run`)    |
| `DetectFromEndpointID()` / `CorrectFromResponse()` / `DetectMediaType()` | Media type detection helpers |

**Media Type Routing:**

| Media Type | Metering Endpoint | Detection Examples                        | Billing Metric                      |
| ---------- | ----------------- | ----------------------------------------- | ----------------------------------- |
| IMAGE      | `/ai/images`      | flux, stable-diffusion, recraft, sdxl     | Per image (+ resolution)            |
| VIDEO      | `/ai/video`       | kling-video, veo, sora, runway, luma, `\bwan-` | Seconds of video               |
| AUDIO      | `/ai/audio`       | kokoro, chatterbox, whisper, f5-tts, `\bdia\b` | Chars/minutes/seconds          |
| CHAT       | `/ai/completions` | openrouter, llm, text-generation          | Token usage                         |

Detection is two-phase: regex over the endpoint ID, then corrected by inspecting response shape (`images`, `video`, `audio_url`, `usage`). Unknown endpoints default to IMAGE.

### Tool Metering

Report custom external tool / API calls via the `core/metering` builder:

```go
import (
    "time"
    "github.com/revenium/revenium-go-sdk/core/metering"
)

mc, _ := metering.NewMeteringClient(metering.MeteringClientConfig{
    APIKey: os.Getenv("REVENIUM_METERING_API_KEY"),
})
defer mc.Close()

payload := metering.NewToolEvent("weather-api").
    WithOperation("get_forecast").
    WithDuration(245 * time.Millisecond).
    WithSuccess(true).
    Build()

mc.SendToolEvent(payload)
```

### Job Outcomes

Track long-running job outcomes with ROI metrics via `core/jobs`:

```go
import "github.com/revenium/revenium-go-sdk/core/jobs"

client, _ := jobs.NewJobClient(jobs.JobClientConfig{
    APIKey: os.Getenv("REVENIUM_METERING_API_KEY"),
})

_, _ = client.ReportJobOutcome("job-123", &jobs.JobOutcome{
    Status: "completed",
})

pagedJobs, _ := client.ListJobs(&jobs.ListJobsParams{PageSize: 20})
_ = pagedJobs
```

## Metadata Fields

Attached via `core.WithUsageMetadata(ctx, map[string]interface{}{...})` or via `core.WithSubscriber(ctx, ...)`.

| Field                   | Type      | Description                                            |
| ----------------------- | --------- | ------------------------------------------------------ |
| `traceId`               | string    | Unique identifier for session / conversation           |
| `taskType`              | string    | Type of AI task (e.g. "chat", "embedding")             |
| `agent`                 | string    | AI agent / bot identifier                              |
| `organizationName`      | string    | Organization or company name                           |
| `productName`           | string    | Product or feature name                                |
| `subscriptionId`        | string    | Subscription plan identifier                           |
| `responseQualityScore`  | float64   | Custom quality rating (0.0–1.0)                        |
| `subscriber.id`         | string    | Unique user identifier                                 |
| `subscriber.email`      | string    | User email address                                     |
| `subscriber.credential` | object    | Authentication credential (`name` and `value`)         |

## Trace Visualization Fields

Environment variables picked up automatically for distributed tracing and analytics:

| Environment Variable             | Description                                                                |
| -------------------------------- | -------------------------------------------------------------------------- |
| `REVENIUM_ENVIRONMENT`           | Deployment environment (production, staging, development)                  |
| `REVENIUM_REGION`                | Cloud region (auto-detected from AWS/Azure/GCP if not set)                 |
| `REVENIUM_CREDENTIAL_ALIAS`      | Human-readable credential name                                             |
| `REVENIUM_TRACE_TYPE`            | Categorical identifier (alphanumeric, hyphens, underscores, max 128 chars) |
| `REVENIUM_TRACE_NAME`            | Human-readable label for trace instances (max 256 chars)                   |
| `REVENIUM_PARENT_TRANSACTION_ID` | Parent transaction reference for distributed tracing                       |
| `REVENIUM_TRANSACTION_NAME`      | Human-friendly operation label                                             |
| `REVENIUM_RETRY_NUMBER`          | Retry attempt number (0 for first attempt)                                 |

## Configuration Options

### Common Environment Variables

| Variable                     | Required | Description                                                |
| ---------------------------- | -------- | ---------------------------------------------------------- |
| `REVENIUM_METERING_API_KEY`  | Yes      | Revenium API key (starts with `hak_`)                      |
| `REVENIUM_METERING_BASE_URL` | No       | Revenium API endpoint (default: `https://api.revenium.ai`) |
| `REVENIUM_DEBUG`             | No       | Enable debug logging (`true`/`false`)                      |
| `REVENIUM_PRINT_SUMMARY`     | No       | Terminal summary (`true`, `human`, `json`, `false`)        |
| `REVENIUM_TEAM_ID`           | No       | Team ID for cost display in terminal summary               |
| `REVENIUM_CAPTURE_PROMPTS`   | No       | Enable prompt capture (`true`/`false`)                     |
| `REVENIUM_MAX_PROMPT_SIZE`   | No       | Max bytes per captured prompt (default: 50000)             |
| `REVENIUM_FAIL_SILENT`       | No       | Swallow metering errors (default: `true`)                  |
| `REVENIUM_API_TIMEOUT`       | No       | Metering HTTP timeout (default: `5s`)                      |
| `REVENIUM_ORGANIZATION_NAME` | No       | Default organization name                                  |

### Provider-Specific Variables

| Variable                         | Provider          | Description                                         |
| -------------------------------- | ----------------- | --------------------------------------------------- |
| `OPENAI_API_KEY`                 | OpenAI            | OpenAI API key                                      |
| `AZURE_OPENAI_API_KEY`           | Azure OpenAI      | Azure OpenAI API key                                |
| `AZURE_OPENAI_ENDPOINT`          | Azure OpenAI      | Azure resource endpoint URL                         |
| `AZURE_OPENAI_API_VERSION`       | Azure OpenAI      | API version (default: `2024-02-15-preview`)         |
| `ANTHROPIC_API_KEY`              | Anthropic         | Anthropic API key                                   |
| `AWS_BEDROCK_ENABLED`            | Anthropic Bedrock | Enable Bedrock transport (`true`)                   |
| `GOOGLE_API_KEY`                 | Google GenAI      | Google AI Studio API key                            |
| `GOOGLE_CLOUD_PROJECT`           | Google Vertex     | GCP project ID (enables Vertex mode)                |
| `GOOGLE_APPLICATION_CREDENTIALS` | Google Vertex     | Path to service account key file                    |
| `GOOGLE_CLOUD_LOCATION`          | Google Vertex     | GCP region (default: `us-central1`)                 |
| `PERPLEXITY_API_KEY`             | Perplexity        | Perplexity API key                                  |
| `LITELLM_PROXY_URL`              | LiteLLM           | LiteLLM proxy URL (e.g. `http://localhost:4000`)    |
| `LITELLM_API_KEY`                | LiteLLM           | LiteLLM proxy API key                               |
| `FAL_KEY` / `FAL_API_KEY`        | fal.ai            | fal.ai API key (either is accepted)                 |
| `FAL_BASE_URL`                   | fal.ai            | Override fal base URL (default: `https://fal.run`)  |
| `FAL_QUEUE_BASE_URL`             | fal.ai            | Override fal queue URL (default: `https://queue.fal.run`) |
| `FAL_REQUEST_TIMEOUT`            | fal.ai            | Request timeout (default: `30m`)                    |
| `RUNWAY_API_KEY`                 | Runway            | Runway API key                                      |
| `RUNWAY_BASE_URL`                | Runway            | Runway base URL (default: `https://api.dev.runwayml.com`) |
| `RUNWAY_VERSION`                 | Runway            | Runway API version (default: `2024-11-06`)          |
| `OLLAMA_BASE_URL`                | Ollama            | Ollama base URL (default: `http://localhost:11434/v1`) |
| `GROQ_API_KEY`                   | Groq              | Groq API key                                        |
| `GROQ_BASE_URL`                  | Groq              | Groq base URL (default: `https://api.groq.com/openai/v1`) |
| `XAI_API_KEY`                    | Grok              | xAI API key                                         |
| `XAI_BASE_URL`                   | Grok              | xAI base URL (default: `https://api.x.ai/v1`)       |

## Troubleshooting

### No tracking data appears

1. Verify environment variables are set correctly (`.env` in project root or exported in shell).
2. Enable debug logging: `REVENIUM_DEBUG=true`.
3. Check console for `[Revenium DEBUG]` / `[Revenium INFO]` log messages.
4. Verify your `REVENIUM_METERING_API_KEY` is valid (starts with `hak_`).

### `middleware not initialized` error

- Make sure you call `Initialize()` before `GetClient()`.
- Check that your `.env` is readable from the working directory (or pre-export env vars).
- Verify `REVENIUM_METERING_API_KEY` is set.

### Azure OpenAI not metering

- Confirm `AZURE_OPENAI_API_KEY`, `AZURE_OPENAI_ENDPOINT`, `AZURE_OPENAI_API_VERSION` are all set.
- The `model` field should be the Azure **deployment name**, not the base OpenAI model name.

### fal.ai `FAL_API_KEY is required`

- fal.ai's official env var is `FAL_KEY`; this SDK accepts both `FAL_KEY` and `FAL_API_KEY`.

### Debug Mode

```env
REVENIUM_DEBUG=true
```

Then every outgoing metering payload is logged to stderr in full.

## Architecture

This is a **multi-module Go repository**:

- `core/` — Shared utilities: config, errors, logger, context helpers, metering client, resilience (circuit breaker, retry, error classification), prompt capture, job tracking.
- `core/testutil/` — `MockMeteringServer` for offline tests.
- `openai/`, `anthropic/`, `google/`, `litellm/`, `perplexity/`, `fal/`, `runway/`, `ollama/`, `groq/`, `grok/` — Provider-specific middleware modules.
- `go.work` — Workspace file for local development across modules.

Each provider has its own `go.mod` with a `replace` directive pointing to local `../core` during development. In production, consumers pull published versions of each module independently.

## Development

```bash
make deps         # Download all module dependencies
make build-all    # Build all modules
make test-all     # Run all tests
make lint-all     # go vet all modules
make fmt-all      # gofmt all modules

# Run tests for a single module
cd openai && go test -race -count=1 ./...

# Run with coverage
go test -cover ./...

# Sync the workspace
go work sync
```

## Requirements

- Go 1.21+
- At least one provider SDK available as a dependency of the provider module you import

## Contributing

Issues and PRs welcome. Run `make test-all` and `make lint-all` before submitting.

## Security

Report security issues to security@revenium.io.

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.

## Support

- **Website**: [www.revenium.ai](https://www.revenium.ai)
- **Documentation**: [docs.revenium.io](https://docs.revenium.io)
- **Email**: support@revenium.io

---

**Built by Revenium**
