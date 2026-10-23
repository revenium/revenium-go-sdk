# Revenium Go SDK

[![Go Reference](https://pkg.go.dev/badge/github.com/revenium/revenium-go-sdk.svg)](https://pkg.go.dev/github.com/revenium/revenium-go-sdk)
[![Go 1.21 to 1.25 by module](https://img.shields.io/badge/Go-1.21%20to%201.25%20by%20module-00ADD8)](#requirements)
[![Tests](https://github.com/revenium/revenium-go-sdk/actions/workflows/test.yml/badge.svg)](https://github.com/revenium/revenium-go-sdk/actions/workflows/test.yml)
[![Documentation](https://img.shields.io/badge/docs-revenium.io-blue)](https://docs.revenium.io)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Go SDK for automatic AI usage metering, cost tracking, and analytics across 13 providers.**

Revenium wraps your existing AI provider clients (OpenAI, Anthropic, Google, etc.) and sends metering data asynchronously to the Revenium platform, so metering never slows a call down. Calls reach the provider as you wrote them, with two exceptions: a call that breaches a cost-control block is refused before it is sent (see [Cost Controls / Enforcement](#cost-controls--enforcement)), and a Claude call on Bedrock is converted to Bedrock's request format, which leaves out `metadata` and `service_tier` and refuses request parts Bedrock cannot carry (see [Anthropic](#anthropic)). Each provider is its own Go module -- install only what you need.

## Table of Contents

- [How It Works](#how-it-works)
- [Supported Providers](#supported-providers)
- [Installation](#installation)
- [Authentication](#authentication)
- [Quick Start](#quick-start)
- [Provider Guides](#provider-guides)
- [Streaming](#streaming-example--openai)
- [Error Handling](#error-handling-pattern)
- [Usage Metadata & Context](#usage-metadata--context)
- [Shutdown and Delivery](#shutdown-and-delivery)
- [Several Revenium Accounts in One Process](#several-revenium-accounts-in-one-process)
- [Tool Metering](#tool-metering)
- [Job Outcomes](#job-outcomes)
- [Calls That Are Not Recorded](#calls-that-are-not-recorded)
- [API Reference](#api-reference)
- [Configuration Reference](#configuration-options)
- [Cost Controls / Enforcement](#cost-controls--enforcement)
- [Troubleshooting](#troubleshooting)
- [Data & Privacy](#data--privacy)
- [Versioning & Stability](#versioning--stability)
- [Architecture](#architecture)
- [Development](#development)
- [Examples](#examples)
- [Contributing](#contributing)

## How It Works

1. You call `Initialize()` once at startup to configure the middleware
2. `GetClient()` returns a Revenium client whose methods mirror the upstream SDK for the calls it meters (listed per provider in the [API Reference](#api-reference))
3. Each call made through those methods is recorded with its usage metrics (tokens, latency, model, cost). Calls made through the provider's own client are not; see [Calls That Are Not Recorded](#calls-that-are-not-recorded)
   - A call that fails (an error status from the provider, a lost connection, a stream rejected or broken off) is recorded once, with stop reason `ERROR`, the provider's error type and HTTP status as the error reason, no tokens unless the provider reported some before failing, and no billed quantity for images and video. A Runway task the provider cancels is recorded with stop reason `CANCELLED`. A Runway task request or a fal queued request (`Subscribe`) that got no answer, a 5xx answer or an answer the SDK could not read is not recorded, because the provider may have created the task and bill it; a warning says so. A call stopped by a cost control before it is sent is not recorded
4. Metrics are sent asynchronously via goroutines -- your request path is never blocked
5. If metering fails, errors are logged and never returned to your code

Clients returned by `GetClient()` are safe for concurrent use. Call `Initialize()` once; use the client from any goroutine.

## Features

- **Multi-Provider Support** — OpenAI, Azure OpenAI, Anthropic, Anthropic on Bedrock, Google GenAI, Google Vertex AI, Perplexity, LiteLLM, fal.ai, Runway, Ollama, Groq, Grok (xAI)
- **Consistent API** — Same `Initialize()` / `GetClient()` / `NewRevenium*()` pattern across all providers
- **Multi-Module Layout** — Each provider is its own Go module; pull only what you import
- **Fire-and-Forget Metering** — Async sends via goroutines; your request never waits on Revenium. With prompt capture on, the prompt and reply are redacted and cut on your goroutine before the call returns, which takes longer as the conversation grows
- **Streaming Support** — First-class streaming wrappers for OpenAI, Anthropic, Google, Perplexity, LiteLLM, and fal.ai with token accumulation and first-token timing
- **Resilience Built-in** — Circuit breaker, exponential-backoff retry with jitter, and error classification shipped in `core/resilience`
- **Tool & Job Metering** — Report custom tool calls and long-running job outcomes via `core/metering` and `core/jobs`
- **Prompt Capture** — Optional capture of the system prompt, input messages and reply of every chat call, with credentials redacted
- **.env Loading** — `Initialize()` reads `.env.local` and `.env` from the working directory only; see [.env files](#env-files)

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

## Installation

```bash
go get github.com/revenium/revenium-go-sdk/openai
go get github.com/revenium/revenium-go-sdk/anthropic
go get github.com/revenium/revenium-go-sdk/google
```

Each provider module pulls `core` and its upstream SDK transitively. Install only the providers you need.

### Requirements

Each module declares its own minimum Go version in its `go.mod`, set by the module and the dependencies it pulls in. On an older Go, `go get` either fails with `requires go >= ...` or, when `GOTOOLCHAIN` allows downloads, fetches a newer Go and raises the `go` line in your `go.mod`. Check the row of every module you install; `core` comes in with every provider.

| Module | Minimum Go |
|---|---|
| `core` | 1.21 |
| `fal` | 1.21 |
| `grok` | 1.21 |
| `groq` | 1.21 |
| `runway` | 1.21 |
| `webhooks` | 1.21 |
| `litellm` | 1.22 |
| `ollama` | 1.22 |
| `anthropic` | 1.24 |
| `google` | 1.24 |
| `openai` | 1.25 |
| `perplexity` | 1.25 |

## Authentication

The SDK requires a Revenium API key plus your provider API key(s). The recommended approach is environment variables, but you can also configure programmatically.

**Via environment variables** (recommended):

```env
REVENIUM_METERING_API_KEY=rev_mk_your_metering_key
REVENIUM_METERING_BASE_URL=https://api.revenium.ai
OPENAI_API_KEY=sk-your-openai-key
```

**Via options:**

```go
reveniumopenai.Initialize(
    reveniumopenai.WithReveniumAPIKey("rev_mk_your_metering_key"),
    reveniumopenai.WithOpenAIAPIKey("sk-your-openai-key"),
)
```

### .env files

`Initialize()` reads `.env.local` and then `.env` from the working directory (the directory the process
was started in) and copies their variables into the process environment. No other directory is read,
and a variable that is already set is never replaced. A value passed as an option always wins over the
same setting in a file. The loaded values apply to the whole process, including other libraries that
read the same variables (for example `OPENAI_BASE_URL` or `HTTPS_PROXY`).

- Set `REVENIUM_ENV_FILES_DISABLE=true` to turn file loading off, which is the safer setting in
  production and on shared build machines.
- Standalone constructors (`NewReveniumOpenAI(cfg)` and the like) never read `.env` files.

## Quick Start

### OpenAI

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

## Provider Guides

### Azure OpenAI

Azure is auto-detected when `AZURE_OPENAI_API_KEY` and `AZURE_OPENAI_ENDPOINT` are set. Same `Initialize()` / `GetClient()` API -- the model field should be the Azure deployment name.

- `AZURE_OPENAI_API_VERSION` is optional; without it the SDK calls the dated `/openai/deployments` routes with `2024-02-15-preview` (`openai.DefaultAzureAPIVersion`); a `/openai/v1` endpoint sends no `api-version` unless you set one.
- A `BaseURL` on an Azure host (one ending in `azure.com`, `azure.us`, `azure.cn` or `azure-api.net`, such as `https://my-resource.openai.azure.com`) is also treated as Azure and needs `AZURE_OPENAI_API_KEY`; the SDK refuses to start without it rather than send your OpenAI key there.
- For Azure's v1 API, set the endpoint (or `BaseURL`) to the v1 URL, such as `https://my-resource.openai.azure.com/openai/v1`. The SDK then calls OpenAI's paths under that URL with your deployment name as the model and your Azure key as the API key, and sends `api-version` only when you set `AZURE_OPENAI_API_VERSION` (for example `preview`). Any other endpoint uses the dated API routes (`/openai/deployments/<deployment>/...`); a trailing `/openai` on it is accepted.
- Azure image and audio replies do not name the model, so those records carry the deployment name. When a deployment is not named after the model it serves, map it with `AZURE_OPENAI_DEPLOYMENT_MODELS=my-dalle=dall-e-3,my-tts=tts-1` or `openai.WithAzureDeploymentModels(map[string]string{"my-dalle": "dall-e-3"})` so the record names a model the price catalog lists. Chat records keep the deployment name.
- A failed Azure call returns its error. It is not retried against OpenAI, and it produces one usage record with provider `AZURE`.
- To turn Azure detection off, so Azure variables left in your environment are ignored and calls go to OpenAI, set `REVENIUM_AZURE_DISABLE=true` or pass `openai.WithAzureDisabled(true)`.

### xAI through the OpenAI client

A client whose `BaseURL` (or `OPENAI_BASE_URL`, when `BaseURL` is empty) is on `x.ai`, for example `https://api.x.ai/v1`, is recorded with provider `XAI` and model source `XAI`, so Grok spend is not reported as OpenAI. Reasoning tokens that xAI reasoning models leave out of `completion_tokens` are billed as output, and the cost xAI reports for the call (`cost_in_usd_ticks`) is forwarded as the transaction's total cost, so a Grok model Revenium has no rate for is not stored at $0. Pass your xAI key with `WithOpenAIAPIKey`. The `grok` package remains the dedicated xAI integration.

### Anthropic

```go
package main

import (
    "context"
    "log"

    anthropic "github.com/anthropics/anthropic-sdk-go"
    reveniumanthropic "github.com/revenium/revenium-go-sdk/anthropic"
)

func main() {
    if err := reveniumanthropic.Initialize(); err != nil {
        log.Fatal(err)
    }
    client, err := reveniumanthropic.GetClient()
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    msg, err := client.Messages().CreateMessage(context.Background(), anthropic.MessageNewParams{
        Model:     "claude-sonnet-4-6",
        MaxTokens: 1024,
        Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("Hello!"))},
    })
    if err != nil {
        log.Fatal(err)
    }
    _ = msg
}
```

Bedrock is auto-detected when `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` are set, or when `BaseURL` contains `amazonaws.com`. Temporary credentials from SSO or an assumed role work when they are exported as `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` and `AWS_SESSION_TOKEN`, for example with `eval "$(aws configure export-credentials --format env)"`; the SDK sends the session token with them. A profile alone (`AWS_PROFILE` after `aws sso login`) does not select Bedrock: export its credentials as above, or set `Config.BaseURL` to any URL containing `amazonaws.com` (possible only through `NewReveniumAnthropic`, not `Initialize`), in which case the SDK signs with the profile. `BaseURL` only selects Bedrock: the SDK never sends requests to it, and the region and endpoint come from `AWS_REGION` or the profile. On a runtime that exports AWS credentials for its own role, such as AWS Lambda, those variables select Bedrock too; set `REVENIUM_BEDROCK_DISABLE=true` to keep calls on the Anthropic API.

The SDK sends your whole request to Bedrock: system prompt, tools, tool choice, temperature, top-p, top-k, thinking, images and stop sequences. InvokeModel (the default) passes the Anthropic request through with every field Bedrock's Claude body accepts; the model goes in the URL. `metadata` and `service_tier` steer Anthropic's own service and have no Bedrock counterpart, so they are left out on both APIs. To use the Converse API instead, set `REVENIUM_BEDROCK_USE_CONVERSE=true` or pass `WithBedrockUseConverse(true)`. A request part the API in use has no field for (on Converse: server tools such as web search, documents, images by URL; on either API: newer Anthropic-only fields such as `inference_geo`) is refused with an error that wraps `ErrBedrockCannotCarry` and names the part, instead of being sent without it or rejected by Bedrock.

A Bedrock call that fails returns its error. To re-send a failed Bedrock call to the Anthropic API instead, set `REVENIUM_BEDROCK_FALLBACK_TO_ANTHROPIC=true` or pass `WithBedrockFallbackToAnthropic(true)` to `Initialize`; a client built with `NewReveniumAnthropic` reads no environment variables, so set `Config.BedrockFallbackToAnthropic` to true there. The re-sent call needs `ANTHROPIC_API_KEY` and is recorded as an Anthropic call.

### Google GenAI / Vertex AI

```go
package main

import (
    "context"
    "log"

    reveniumgoogle "github.com/revenium/revenium-go-sdk/google"
    "google.golang.org/genai"
)

func main() {
    if err := reveniumgoogle.Initialize(); err != nil {
        log.Fatal(err)
    }
    client, err := reveniumgoogle.GetClient()
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    resp, err := client.Models().GenerateContent(
        context.Background(),
        "gemini-2.0-flash",
        []*genai.Content{genai.NewContentFromText("Hello!", "user")},
        nil,
    )
    if err != nil {
        log.Fatal(err)
    }
    _ = resp
}
```

Vertex AI is auto-detected when `GOOGLE_CLOUD_PROJECT` is set (uses `GOOGLE_APPLICATION_CREDENTIALS` for auth). To keep using the Gemini API with `GOOGLE_API_KEY` while `GOOGLE_CLOUD_PROJECT` is set, set `REVENIUM_VERTEX_DISABLE=true` or `VertexDisabled: true` in the Google `Config`.

### Perplexity

```go
package main

import (
    "context"
    "log"

    openai "github.com/openai/openai-go/v3"
    reveniumperplexity "github.com/revenium/revenium-go-sdk/perplexity"
)

func main() {
    if err := reveniumperplexity.Initialize(); err != nil {
        log.Fatal(err)
    }
    client, err := reveniumperplexity.GetClient()
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    resp, err := client.Chat().Completions().New(context.Background(), openai.ChatCompletionNewParams{
        Model:    "sonar",
        Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("Hello!")},
    })
    if err != nil {
        log.Fatal(err)
    }
    _ = resp
}
```

### LiteLLM

```go
package main

import (
    "context"
    "log"

    reveniumlitellm "github.com/revenium/revenium-go-sdk/litellm"
)

func main() {
    if err := reveniumlitellm.Initialize(); err != nil {
        log.Fatal(err)
    }
    client, err := reveniumlitellm.GetClient()
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    resp, err := client.Chat().Completions().New(context.Background(), reveniumlitellm.ChatCompletionRequest{
        Model: "openai/gpt-4o-mini",
        Messages: []reveniumlitellm.ChatMessage{
            {Role: "user", Content: "Hello!"},
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    _ = resp
}
```

LiteLLM also supports runtime `Enable()` / `Disable()` and `GetStatus()` for introspection.

Grok models called through the proxy (`xai/grok-4`, or a bare `grok-*` model) are recorded under provider `XAI`, not `LiteLLM`, so cost controls on provider `LiteLLM` do not cover them; add a rule on provider `XAI` (or on the model) to cap them. When the proxy reports a cost for the call in `usage.cost`, as LiteLLM does for xAI, that cost is forwarded as the transaction's total cost.

Cost controls judge a LiteLLM chat call by every model it can be recorded under, since the record names the model the proxy served. Before the call, a client with a `TeamID` reads the proxy's model list (`GET /model/info`, with the proxy key, refreshed every five minutes) and checks the requested name and each provider model the proxy routes it to: a call to an alias such as `team-fast` that the proxy serves with `xai/grok-4` is blocked by a breached rule on provider `XAI`. An alias routed to several providers is blocked when a breached rule covers any of them. If the proxy does not answer the model list, the last list read is kept until a refresh succeeds, an alias not in it is judged by its requested name, and a warning is logged. Embeddings are recorded and judged under the requested model.

A chat call to an alias is recorded under the deployment the proxy served, not the alias: LiteLLM answers with the name you requested (`team-fast`), so the record names the deployment from the proxy's `x-litellm-model-name` response header (`openai/gpt-4.1-nano` is recorded as provider `OpenAI`, model `gpt-4.1-nano`), else the deployment its `x-litellm-model-id` or `x-litellm-model-group` header names in the model list already read for cost controls (a group routed to several deployments names none), else the model in the answer. A provider model the proxy passes through in the answer, such as a dated snapshot, is kept. These are the same deployments the cost-control check judges, so a cap that blocks an alias also counts its spend. When the answer carries no `usage.cost`, the cost in the proxy's `x-litellm-response-cost` header (sent by LiteLLM on non-streamed answers) is forwarded as the transaction's total cost; a zero or unreadable header cost is not forwarded, and `usage.cost` wins when both are present.

When the proxy names the person in its `X-Revenium-Subscriber-*` response headers and the call names none of its own, the record carries that person, and the client's next call with the same request `user` value (or none) is checked against cost controls as that person. The proxy names the person only after a call, so the first call from such a person is not checked by the SDK; a person you set with `core.WithSubscriber` or the usage metadata is checked from the first call.

### fal.ai

```go
package main

import (
    "context"
    "log"

    reveniumfal "github.com/revenium/revenium-go-sdk/fal"
)

func main() {
    if err := reveniumfal.Initialize(); err != nil {
        log.Fatal(err)
    }
    client, err := reveniumfal.GetClient()
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    result, err := client.Run(context.Background(),
        "fal-ai/flux/schnell",
        map[string]interface{}{"prompt": "a futuristic cityscape at sunset"},
        nil,
    )
    if err != nil {
        log.Fatal(err)
    }
    _ = result
}
```

The fal.ai middleware automatically detects the media type (image, video, audio, chat) from the endpoint ID. Also supports `Subscribe()` for queue-based execution and `Stream()` for streaming. Accepts `FAL_KEY` or `FAL_API_KEY`.

### Streaming Example — OpenAI

OpenAI, Anthropic, Google, Perplexity, LiteLLM and fal.ai expose streaming wrappers; Grok (xAI), Groq and Ollama do not stream (see below). OpenAI example:

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

Token usage and cost are captured without any extra option. The OpenAI and Azure stream wrappers ask the provider to report usage (`stream_options.include_usage`) on your behalf, and the extra usage-only chunk that produces is not passed to your loop unless you set that option yourself.

`Close()` is safe to call more than once, so `defer stream.Close()` alongside the explicit call above still sends a single usage record.

The same `Next()` / `Current()` / `Err()` / `Close()` pattern applies to Anthropic (`Messages().CreateMessageStream()`), Google (`Models().GenerateContentStream()`), Perplexity (`Chat().Completions().NewStreaming()`), and LiteLLM (`Chat().Completions().NewStreaming()`). fal.ai streaming uses a channel: `events, err := client.Stream(ctx, endpointID, input, metadata)`.

Grok (xAI), Groq and Ollama do not stream. Their `ChatCompletions` returns `ErrStreamingNotSupported`, a validation error, when `ChatCompletionRequest.Stream` is true, and sends nothing to the provider, so no call is made that would go unrecorded.

### Error Handling Pattern

Metering errors never surface to your application — they are logged and never returned. Upstream provider errors are returned normally:

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

### Groq / Grok / Ollama / Runway

All follow the same `Initialize()` / `GetClient()` / `Close()` pattern:

```go
import reveniumgroq "github.com/revenium/revenium-go-sdk/groq"

if err := reveniumgroq.Initialize(); err != nil {
    log.Fatal(err)
}
client, err := reveniumgroq.GetClient()
if err != nil {
    log.Fatal(err)
}
defer client.Close()
```

Grok calls are metered under the model xAI reports serving (the response `model`), falling back to the
requested model only when the response omits it. xAI can answer a request for one model with another
(for example `grok-4.3` for a `grok-3-mini` request), and the served model is the one xAI billed. The
cost xAI reports for the call (`cost_in_usd_ticks`) is forwarded as the transaction's total cost, and
reasoning tokens, which xAI bills as output but reports outside `completion_tokens`, are counted in
the output token count.
#### Runway video length

Runway does not report how long a finished video is, so the SDK bills the length you asked for.
`ImageToVideo` and `VideoToVideo` bill `Duration`. When it is left out, `VideoToVideo` bills
`SourceDurationSeconds`, the length of the input video, which video-to-video models such as
`gen4_aleph` follow; otherwise both bill the model's default length. A model with no known default (anything
other than `gen4_turbo`, `gen3a_turbo`, `veo3`, `veo3.1`, `veo3.1_fast` and `grok_imagine_1_5_lite`)
called with neither length is recorded with 0 seconds, stop reason `ERROR` and an error reason that
starts with `video_length_unknown`, so set `Duration` or `SourceDurationSeconds` for those models.
`UpscaleVideo` requires `SourceDurationSeconds`. Without it, `UpscaleVideo` returns
`runway.ErrMissingSourceDuration` before calling Runway. `SourceDurationSeconds` is used only for
metering and is never sent to Runway.

#### fal and Runway tasks the caller stops waiting for

When the caller's context ends after Runway accepted a task or fal queued a `Subscribe` request, the
call returns at once and the SDK keeps polling the task in the background, then records it with the
outcome the provider reports. A task whose outcome is still unknown after 10 minutes (for fal, after
the client's `RequestTimeout` when that is shorter), or when `Close()` is called, is recorded with stop
reason `ERROR`, the requested length or image count, and an error reason that names the task id and
says its outcome is unknown. Background polls die with the process, so a
short-lived program (a CLI, a job runner, a serverless function) must call `Close()` before it exits,
or those records are lost.

```go
result, err := client.UpscaleVideo(ctx, &runway.VideoUpscaleRequest{
    PromptVideo:           videoURL,
    SourceDurationSeconds: 8,
}, nil)
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

The typed subscriber's `ID` and `Email` are sent as the record's `subscriber`. When a call carries both a typed subscriber (with an `ID` or `Email`) and a `subscriber` entry in the usage metadata map, the typed subscriber wins and the map entry is ignored. The same subscriber is used to match cost controls. The `APIKey` field is never sent.

## Shutdown and Delivery

Usage records are sent in the background so they never slow a call down. When Revenium is slow or unreachable, records that cannot be delivered are retried and then held in memory and resent later. Anything still being sent or held is lost if the process ends before `Close()` runs. `Close()` gives the records being sent up to 10 seconds (or `REVENIUM_API_TIMEOUT` when that is longer), makes a last attempt of the same length to deliver the held ones, and returns an error that says how many were still undelivered (also logged as a warning).

Delivery is bounded so a slow or failing Revenium cannot hurt the application: at most 32 records are sent at once per process (one connection each), up to 1,000 more wait their turn, and any beyond that are held in memory (up to 20,000 records and 100 MiB per client, oldest dropped first with one warning per outage). After three failed sends in a row for a Revenium key, new records for that key are held without being sent and one is tried every 5 seconds; the first success resends the held records. A `Retry-After` header on a 429 or 503 is honoured. Each Revenium key is tracked separately, so one key being throttled never holds back another key's records or job calls.

The 100 MiB limit (`REVENIUM_BUFFER_MAX_BYTES`, or `BufferMaxBytes` on `core.ReveniumConfig` or `MeteringClientConfig`) counts each held record's encoded size, together with the records waiting for their first send, whether Revenium is down, slow or healthy. Without prompt capture a record is about 1 KB, so 20,000 of them stay well inside it and the record count is the limit that applies. With prompt capture on, a record can carry three fields of up to `REVENIUM_MAX_PROMPT_SIZE` characters (150 KB or more at the default 50,000), so the byte limit applies first: about 700 such records fit. When it is reached the oldest held records are dropped to make room; when the records already being sent fill it, new records are dropped before they are sent, and a record larger than the whole limit is dropped on its own. Because records waiting for their first send count even while Revenium is healthy, a burst of records whose total size passes the limit loses the records past it; set `REVENIUM_BUFFER_MAX_BYTES` above the largest burst you expect. One warning is logged per outage. `GetBufferStats()` reports `Size` (records queued for the next resend), `InFlight` (records being resent now), `HeldBytes` (the memory those take, plus records waiting for their first send) and `MaxBytes`.

`defer client.Close()` in `main` only covers a normal return from `main`. Deferred calls do not run when:

- the program calls `os.Exit` or `log.Fatal` (which calls `os.Exit`);
- a goroutine other than the one running `main` panics;
- the process receives `SIGTERM` or `SIGINT` (a container stop, a deploy, Ctrl-C) and nothing handles the signal.

Handle the signal and close the client yourself before exiting:

```go
func main() {
    if err := reveniumopenai.Initialize(); err != nil {
        log.Fatal(err)
    }
    client, err := reveniumopenai.GetClient()
    if err != nil {
        log.Fatal(err)
    }

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    exitCode := run(ctx, client) // returns when ctx is cancelled or the work is done
    stop()

    if err := client.Close(); err != nil {
        log.Printf("revenium: %v", err)
    }
    os.Exit(exitCode)
}
```

Instead of `log.Fatal` deeper in the program, return an error to `main` so `Close()` still runs. Give the container enough stop time (for example Kubernetes `terminationGracePeriodSeconds`) for `Close()`, which can take up to about twice the larger of 10 seconds and `REVENIUM_API_TIMEOUT` (about 20 seconds by default) when Revenium is slow.

## Several Revenium Accounts in One Process

`Initialize()` sets up one shared client per module, for one Revenium account. Calling it again with the same settings, or without naming any Revenium setting, returns nil. Calling it again with a different Revenium key, base URL or team, or a different cost-control key (`REVENIUM_ENFORCEMENT_API_KEY`) or address (`REVENIUM_ENFORCEMENT_BASE_URL`), returns an error naming the setting and changes nothing, so one account's usage is never recorded under another and cost controls keep being read with the first setup's key and address.

To serve several Revenium accounts from one process (for example one per customer), build one client per account with the module's standalone constructor instead of `Initialize()`: `NewReveniumOpenAI`, `NewReveniumAnthropic`, `NewReveniumGoogle`, `NewReveniumPerplexity`, `NewReveniumLiteLLM`, `NewReveniumFal`, `NewReveniumRunway`, `NewReveniumGroq`, `NewReveniumGrok` or `NewReveniumOllama`. Each client sends its usage records with its own key, and fetches and enforces its own team's cost controls. A client whose `ReveniumConfig.BaseURL` is empty sends its records to `REVENIUM_METERING_BASE_URL`, or `https://api.revenium.ai` when that is unset, and reads its cost controls from `REVENIUM_ENFORCEMENT_BASE_URL` when set, otherwise from that same address; set `BaseURL` per client when accounts live at different addresses. Each client reads its cost controls with its own key, or with `ReveniumConfig.EnforcementAPIKey` when you set one: a process-wide `REVENIUM_ENFORCEMENT_API_KEY` never replaces a client's own key. With a `TeamID`, the constructor fetches the rules before returning, which can take up to 10 seconds.

```go
func newCustomerClient(reveniumKey, teamID string) (*reveniumopenai.ReveniumOpenAI, error) {
    return reveniumopenai.NewReveniumOpenAI(&reveniumopenai.Config{
        OpenAIAPIKey: os.Getenv("OPENAI_API_KEY"),
        Revenium: &core.ReveniumConfig{
            APIKey:  reveniumKey,
            BaseURL: core.DefaultReveniumBaseURL,
            TeamID:  teamID,
        },
    })
}
```

Keep each client for as long as you serve that account, and call its `Close()` on shutdown as described above.

## Calls That Are Not Recorded

Only the methods listed in the API Reference are recorded and checked against cost controls.
`GetOpenAIClient()`, `GetAnthropicClient()` and `GetGenaiClient()` return the provider's own client, and calls
made through it are neither recorded nor blocked by cost controls. The provider still bills them.

### OpenAI

Billable calls the SDK does not record: Sora video (`Videos`), the Batch API (`Batches`), streamed image generation and edits
(`Images.GenerateStreaming`, `Images.EditStreaming`), streamed transcription (`Audio.Transcriptions.NewStreaming`),
`Responses.Compact`, Responses over WebSocket (`Responses.Connect`), Realtime (`Realtime`), fine-tuning
(`FineTuning`), Assistants (`Beta.Assistants`, and threads and runs through `Beta.Threads`), the beta Responses
service (`Beta.Responses`), Code Interpreter containers (`Containers`) and file-search storage (`VectorStores`).

### Anthropic

Billable calls the SDK does not record: Message Batches (`Messages.Batches`, `Beta.Messages.Batches`), the legacy Text Completions API
(`Completions`), and managed agents: agent sessions (`Beta.Sessions`), agents (`Beta.Agents`) and deployments
(`Beta.Deployments`, `Beta.DeploymentRuns`). Use `BetaMessages()` for beta messages and the tool
runner; called through `GetAnthropicClient().Beta.Messages` they are not recorded.

### Google

Billable calls the SDK does not record: batch prediction (`Batches`), context caches (`Caches`), the Live API (`Live`), tuning (`Tunings`),
and `Models.RecontextImage` and `Models.SegmentImage`. Use `Chats()` for chat sessions; a chat created with
`GetGenaiClient().Chats` is not recorded. The one exception is polling a Veo operation through
`GetGenaiClient().Operations`: that video is recorded once, when its finished operation is first seen, whether it
was started with `Models().GenerateVideo()` or `Models().GenerateVideoFromSource()` in this client or in another
client or process.

### Perplexity

Only `Chat().Completions()` is recorded. Calls made through `GetOpenAIClient()` are not.

## API Reference

### OpenAI

| Function                      | Description                                   |
| ----------------------------- | --------------------------------------------- |
| `Initialize(opts ...Option)`  | Initialize global middleware from env + opts  |
| `GetClient()`                 | Return the global `*ReveniumOpenAI` instance  |
| `NewReveniumOpenAI(cfg)`      | Construct a standalone instance               |
| `IsInitialized()`             | Report global initialization state            |
| `GetOpenAIClient()`           | Return the provider's own `openai.Client`; calls made through it are not recorded |
| `GetProvider()`               | `ProviderOpenAI` / `ProviderAzure`            |
| `Chat()` / `Embeddings()` / `Images()` / `Audio()` / `Responses()` | Typed interfaces for each operation |
| `Completions().New()` / `NewStreaming()` | Legacy Completions API (`gpt-3.5-turbo-instruct`) |
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
| `BetaMessages().CreateMessage()` / `CreateMessageStream()` | Beta Messages API, one record per call |
| `BetaMessages().NewToolRunner()` / `NewToolRunnerStreaming()` | Beta tool runner, one record per model turn |
| `ReconstructResponseFromChunks()` | Rebuild `*anthropic.Message` from a streaming wrapper |
| `GetAnthropicClient()`       | Return the provider's own `anthropic.Client`; calls made through it are not recorded |

`BetaMessages()` always calls the Anthropic API, also when the client is configured for Bedrock. A tool runner turn's
recorded duration includes the time your tools took to run before that turn's request.

A streamed call you stop reading before Claude's stop event (`message_stop`, or `messageStop` on Bedrock
Converse) is recorded with stop reason `CANCELLED`, on Messages, beta Messages, streaming tool runner turns
and Bedrock. It carries the tokens reported up to that point, so its output token count is understated:
Anthropic reports the reply's output tokens only near the end of the stream. Bedrock Converse reports usage
only after `messageStop`, so such a Converse record carries the SDK's estimate of the input tokens (from the
prompt text) and zero output tokens. With prompt capture on, its reply is marked truncated.

### Google (GenAI + Vertex AI)

| Function                   | Description                                    |
| -------------------------- | ---------------------------------------------- |
| `Initialize(opts ...Option)` | Initialize global middleware                 |
| `GetClient()`              | Return the global `*ReveniumGoogle`            |
| `NewReveniumGoogle(cfg)`   | Construct standalone instance                  |
| `Reset()`                  | Reset global state                             |
| `Models().GenerateContent()` / `GenerateContentStream()` | Chat / streaming |
| `Chats().Create()`         | Chat session; each `SendMessage()` / `SendMessageStream()` turn is one record |
| `Models().CreateEmbedding()` | Embeddings                                   |
| `Models().GenerateImage()` / `EditImage()` / `UpscaleImage()` | Image gen/edit |
| `Models().GenerateVideo()` / `GenerateVideoFromSource()` | Veo video generation, returns the long-running operation |
| `Models().WaitForVideo()` | Optional helper that polls a Veo operation until it finishes |
| `ExtractConfidenceScore()` | Extract confidence from candidate logprobs    |
| `GetGenaiClient()`         | Return the provider's own `*genai.Client`; calls made through it are not recorded |

Gemini token counts are sent as follows. `inputTokenCount` is Gemini's `promptTokenCount` plus
`toolUsePromptTokenCount`, the results of built-in tools (code execution, search grounding, URL context) that Google
feeds back to the model and bills as input; it includes any cached prompt tokens. `cacheReadTokenCount` is the cached part of it (`cachedContentTokenCount`).
`outputTokenCount` is the visible answer plus thinking tokens, which Google bills at the output rate;
`reasoningTokenCount` is the thinking part of it (`thoughtsTokenCount`).

A streamed call you stop reading before Gemini sends a finish reason is recorded with stop reason
`CANCELLED`, with the tokens reported up to that point; with prompt capture on, its reply is marked
truncated.

Embeddings on the Gemini API (`GOOGLE_API_KEY`) are recorded with zero tokens, so they show no cost in
Revenium: the Gemini API returns no token count for embeddings. Vertex AI embeddings are recorded with
the token count Google reports.

Veo videos are metered once per operation, by the client that first sees the finished operation, whether
you wait with `WaitForVideo()` or poll with Google's own `GetGenaiClient().Operations.GetVideosOperation()`.
The record's `durationSeconds` is the requested clip length (Veo's default of 8 seconds when none is
requested) times the number of videos generated. The video can be submitted in one process and polled in
another, such as a web request that submits and a worker that polls, or a service that restarts while the
video renders: the polling process records it. Google's operation names the model but not the requested
length, so a video polled with `GetVideosOperation()` by a client that did not submit it is recorded at 8
seconds per video; to record the requested length there, wait with `WaitForVideo()` and pass the config
the video was submitted with. Every record of one operation carries the same transaction id and
Idempotency-Key, derived from the operation name, so a video seen finished by two processes is counted
once. Revenium keeps whichever of those records arrives first, so when both the submitting and the
polling process may see the video finish, have the polling process call `WaitForVideo()` with the
original config too; otherwise a poller's 8-second record can be the one kept. A `transactionId` or
`idempotencyKey` in usage metadata does not replace them on a Veo record; a `transactionId` you set is
recorded as the record's `traceId` when the call sets no `traceId`. An operation that finishes with an
error, or a submission that fails, is recorded once as an error. Operations that are never polled are not recorded. A call that
the genai client refuses before sending any request, such as `GenerateImage()` on the Gemini API (genai
supports Imagen only on Vertex AI), returns genai's error and is not recorded.

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

**Video length:** A fal video record bills the length fal reports for the output (`video.duration`, or `video.num_frames` over `video.fps`). fal rarely reports one, so the SDK otherwise bills the length the request asked for: `duration`, else `num_frames` over `frames_per_second` (or `fps`), each falling back to the endpoint family's published default, else the family's default length (for example 4 seconds for `sora-2`, 81 frames at 16 per second for Wan 2.2, 129 frames at 24 per second for `hunyuan-video`). A video whose length is known nowhere is recorded with 0 seconds, stop reason `ERROR` and an error reason starting `video_length_unknown`; set `duration` on the request for such an endpoint.

### Tool Metering

Report custom external tool / API calls via the `core/metering` builder. Like usage records, tool events go to `REVENIUM_METERING_BASE_URL` when it is set and to `https://api.revenium.ai` otherwise; a `BaseURL` in `MeteringClientConfig` overrides both:

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

For a tool call that failed, `WithFailure(err)` sends a short name of the error (`NETWORK_ERROR`, `json.SyntaxError`,
`timeout`) as the event's `errorMessage`, adding the error text only with
`REVENIUM_CAPTURE_ERROR_MESSAGES=true`; for an error that quotes a provider's reply, such as an AWS error
or a provider's HTTP error, `REVENIUM_CAPTURE_PROMPTS=true` is needed as well. `WithError(msg)` sends your own text. Either way credentials,
query-string values and email addresses are redacted and the message is cut to 2,048 characters.

Each tool event is one tool call: it gets its own generated `transactionId`, and its `Idempotency-Key`
header is that id, so the SDK's own retries and resends are never stored twice while two calls with the
same arguments are stored as two events. A `transactionId` in the metadata passed to
`metering.ApplyToolEventMetadata` groups several tool calls instead of naming one: it is sent as the
event's `traceId`, or as `usageMetadata.callerTransactionId` when a `traceId` is also set. An
`idempotencyKey` in that metadata is not applied to tool events, for the same reason; call
`WithIdempotencyKey` on one event's builder to set its key.

### Job Outcomes

Track and amend long-running job outcomes with ROI metrics via `core/jobs`.

> **Job and outcome calls need a write key (`rev_sk_`).** Revenium refuses a metering key (`rev_mk_`) on every job call: reporting, amending, listing and reading ROI. `NewJobClient` rejects a `rev_mk_` key with a configuration error; it also accepts an older `hak_` key, the key format that predates separate metering (`rev_mk_`) and write (`rev_sk_`) keys. Any key Revenium refuses for job calls, including a `hak_` key without job access, comes back on each call as an authorization error (`core.IsAuthError`). Keep your metering key in `REVENIUM_METERING_API_KEY` for usage records and keep the write key in its own setting, such as `REVENIUM_WRITE_API_KEY`. Do not put the write key in `REVENIUM_METERING_API_KEY`: that key travels with every usage record.

Job calls go to the same Revenium address as usage records: `REVENIUM_METERING_BASE_URL` when it is set, `https://api.revenium.ai` otherwise. A `BaseURL` in `JobClientConfig` overrides both.

```go
import (
    "errors"
    "github.com/revenium/revenium-go-sdk/core/jobs"
)

client, _ := jobs.NewJobClient(jobs.JobClientConfig{
    APIKey: os.Getenv("REVENIUM_WRITE_API_KEY"),
    TeamID: os.Getenv("REVENIUM_TEAM_ID"),
})
defer client.Close()

_, err := client.ReportJobOutcome("job-123", &jobs.JobOutcome{
    ExecutionStatus: jobs.ExecutionStatusSuccess,
    OutcomeType:     jobs.OutcomeConverted,
})

var alreadyReported *jobs.OutcomeAlreadyReportedError
if errors.As(err, &alreadyReported) {
    // Outcome was already reported, amend it instead
}
```

A job has one outcome: a second report returns `OutcomeAlreadyReportedError`. A report that times out is sent once more, and a report that fails with a 5xx or a lost connection is retried as usual. If such a retry is then refused as already reported, the client reads the job back once and returns it as a success only when Revenium holds exactly the outcome you sent, unamended. Each field is compared in the form Revenium stores it:

- `ExecutionStatus`, `OutcomeType` and `OutcomeCurrency` match regardless of letter case.
- A blank `OutcomeReason` matches no stored reason; any other reason must match exactly.
- The currency is expected only when `OutcomeValue` is sent, and defaults to USD; Revenium drops a currency sent without a value.
- `OutcomeValue` matches after rounding to 10 decimal places, half away from zero, as Revenium stores it.
- `Metadata` matches when it is the same JSON, in any key order, with numbers compared exactly.
- `ReportedBy`, when you set it, must be the reporter of the only entry in the outcome history.

A report that carried `Metrics` is never confirmed this way, because Revenium does not return per-job metrics. Amendments and `AppendOutcomeMetrics` are not resent after a timeout.

### Failure Reasons and Per-Job Metrics

Say why a job failed or was cancelled in `OutcomeReason` (keep it out of `Metadata`), and record per-job metric values with the outcome in `Metrics`. Each metric key must be declared on the job type with resolution `PER_JOB`; empty `Provenance`, `RecordedBy`, `Source` and `RecordedAt` take Revenium's defaults:

```go
_, err := client.ReportJobOutcome("loan-77", &jobs.JobOutcome{
    ExecutionStatus: jobs.ExecutionStatusFailed,
    OutcomeType:     jobs.OutcomeUnsuccessful,
    OutcomeReason:   "Customer unreachable",
    Metrics: []jobs.OutcomeMetric{
        {Key: "quality_rate", Value: 0.93, Provenance: jobs.MetricMeasured},
    },
})
```

Add a metric that arrives after the outcome with `AppendOutcomeMetrics`. A value for a key the job already has replaces it and needs a `Reason`. It is retried only on a 429; the SDK cannot read per-job metrics back, so if it fails with a 5xx or a lost connection, resend with a `Reason` on every entry: a value that was stored is then replaced, never counted twice:

```go
err = client.AppendOutcomeMetrics("loan-77", []jobs.OutcomeMetric{
    {Key: "quality_rate", Value: 0.88, Reason: "regraded by reviewer"},
})
```

### Amending an Outcome

Outcomes are mutable. Use `AmendJobOutcome` to correct or update a previously reported outcome:

```go
job, err := client.GetJob("job-123")
if err != nil {
    return err
}
reason := "Payment declined"

amended, err := client.AmendJobOutcome("job-123", &jobs.JobOutcomeAmendment{
    Reason:                "correcting outcome after manual review",
    ExpectedEntityVersion: job.EntityVersion,
    ExecutionStatus:       jobs.ExecutionStatusFailed,
    OutcomeType:           jobs.OutcomeUnsuccessful,
    OutcomeReason:         &reason,
})

var conflict *jobs.OutcomeAmendConflictError
if errors.As(err, &conflict) {
    // Someone changed the outcome since you read it: conflict.CurrentEntityVersion
    // is the version Revenium holds. Re-check, then resend with that version.
}
```

`Reason` explains the amendment itself; `OutcomeReason` is why the job failed (nil leaves the stored one, an empty string clears it). `ExpectedEntityVersion` is optional: set it to the `EntityVersion` you last read and a change made since then is refused with `OutcomeAmendConflictError` instead of overwritten. Amendments are retried only when Revenium answers 429, because a repeat after a 5xx or a lost connection could record the change twice. Before resending, call `GetJobOutcomeHistory`: a stored amendment is the latest entry, carrying your `Reason` and outcome fields.

Retrieve the full amendment history for a job:

```go
history, _ := client.GetJobOutcomeHistory("job-123")
for _, entry := range history {
    // entry.AmendmentSequence: 1 = initial report, 2+ = amendments
    // entry.Reason: nil for initial report, set for amendments
}
```

## Attaching Calls to a Job or a Ticket

A call is attached to an agentic job (for job cost and ROI views) or to a ticket (for cost by ticket) in
three ways. Each field is resolved on its own: the call's own usage metadata wins, then the job
context, then the environment, and a blank value at any step counts as not set. So a call inside a job
context that names its own `agenticJobId` keeps the context's `agenticJobType` unless it sets that too.
fal and Runway calls also take a `metadata` argument. For those, usage metadata set on the context with `core.WithUsageMetadata` wins over the argument, and the argument wins over the job context and the environment.

**Per call**, with the usage metadata keys `agenticJobId`, `agenticJobName`, `agenticJobType`,
`agenticJobVersion` and `ticketId`:

```go
ctx := core.WithUsageMetadata(ctx, map[string]interface{}{"agenticJobId": "invoice-batch-7", "ticketId": "ENG-42"})
```

**For every call made inside a job**, with the job context from `core/jobs`:

```go
result, err := jobs.RunWithJobContext(ctx, &jobs.JobContextData{AgenticJobID: "invoice-batch-7", AgenticJobName: "Invoice batch"},
    func(ctx context.Context) (*openai.ChatCompletion, error) {
        return client.Chat().Completions().New(ctx, params)
    })
```

`jobs.SetJobContext(ctx, data)` returns the same kind of context without running a function; it
replaces any job set on a parent context, and `jobs.ClearJobContext(ctx)` removes the job again. Tool events built with
`metering.ApplyToolEventMetadata(payload, core.GetUsageMetadata(ctx))` carry the job too.

**For every call in the process**, with environment variables read when the client is set up:

| Environment Variable           | Record field        |
| ------------------------------ | ------------------- |
| `REVENIUM_AGENTIC_JOB_ID`      | `agenticJobId`      |
| `REVENIUM_AGENTIC_JOB_NAME`    | `agenticJobName`    |
| `REVENIUM_AGENTIC_JOB_TYPE`    | `agenticJobType`    |
| `REVENIUM_AGENTIC_JOB_VERSION` | `agenticJobVersion` |
| `REVENIUM_TICKET_ID`           | `ticketId`          |

A job id must start with a letter or digit and hold only letters, digits, `.`, `_` and `-` (at most 255
characters; `types` and `conversion-funnel` are reserved). An id Revenium would refuse is left off and
the call is still recorded. A `ticketId` longer than 256 characters is cut to 256.

## Metadata Fields

Attached via `core.WithUsageMetadata(ctx, map[string]interface{}{...})` or via `core.WithSubscriber(ctx, ...)`.

| Field                        | Type    | Records     | Description                                                                 |
| ---------------------------- | ------- | ----------- | --------------------------------------------------------------------------- |
| `organizationName`           | string  | all         | Organization or company name (`organizationId` is accepted as an alias)     |
| `productName`                | string  | all         | Product or feature name (`productId` is accepted as an alias)               |
| `subscriptionId`             | string  | all         | Subscription plan identifier                                                |
| `taskType`                   | string  | all         | Type of AI task (e.g. "chat", "embedding")                                  |
| `agent`                      | string  | all         | AI agent / bot identifier                                                   |
| `agentVersion`               | string  | all         | Version of the agent (cut to 64 characters)                                 |
| `squadId`                    | string  | all         | Id of the team of agents the call belongs to                                |
| `squadName`                  | string  | all         | Name of that team of agents                                                 |
| `squadRole`                  | string  | all         | The agent's role in that team                                               |
| `subscriber.id`              | string  | all         | Unique user identifier                                                      |
| `subscriber.email`           | string  | all         | User email address                                                          |
| `subscriber.credential`      | object  | all         | Authentication credential (`name` and `value`)                              |
| `credentialAlias`            | string  | all         | Name of the API key used; sent as `subscriber.credential.name`              |
| `traceId`                    | string  | all         | Unique identifier for session / conversation                                |
| `parentTransactionId`        | string  | all         | Transaction id of the call that caused this one                             |
| `traceType`                  | string  | all         | Category of the trace                                                       |
| `traceName`                  | string  | all         | Human-readable label of the trace                                           |
| `transactionName`            | string  | all         | Human-readable label of the operation                                       |
| `transactionId`              | string  | all         | Replaces the generated transaction id of this one call                      |
| `idempotencyKey`             | string  | all         | Replaces the generated `Idempotency-Key` of this one call                   |
| `retryNumber`                | int     | all         | Retry attempt number (0 for the first attempt)                              |
| `environment`                | string  | all         | Deployment environment                                                      |
| `region`                     | string  | all         | Cloud region                                                                |
| `ticketId`                   | string  | all         | Ticket the call is charged to (cut to 256 characters)                       |
| `agenticJobId`               | string  | all         | Job the call belongs to (see [Attaching Calls to a Job or a Ticket](#attaching-calls-to-a-job-or-a-ticket)) |
| `agenticJobName`             | string  | all         | Name of that job                                                            |
| `agenticJobType`             | string  | all         | Type of that job                                                            |
| `agenticJobVersion`          | string  | all         | Version of that job's definition                                            |
| `videoJobId`                 | string  | video       | Provider job id of the video                                                |
| `pricingTier`                | string  | all         | `STANDARD` or `BATCH` (any letter case); `BATCH` prices batch API calls     |
| `billingSkipped`             | bool    | all         | `true` records the call at no cost (free tier, for example)                 |
| `requestedServiceTier`       | string  | all         | Service tier the call asked for (e.g. `priority`, `flex`)                   |
| `actualServiceTier`          | string  | all         | Service tier the provider used; Revenium prices the call by it              |
| `totalCost`                  | float64 | all         | Cost you computed yourself                                                  |
| `inputTokenCost`, `outputTokenCost`, `cacheCreationTokenCost`, `cacheReadTokenCost` | float64 | completions | Token costs you computed yourself |
| `responseQualityScore`       | float64 | completions | Custom quality rating (0.0–1.0)                                             |
| `temperature`, `mediationLatency` | float64 | completions | Sampling temperature; latency added by your own gateway               |
| `operationSubtype`           | string  | media       | Overrides the detected image/audio/video subtype                            |
| `modelSource`, `errorReason` | string  | all         | Overrides of what the SDK detects (`errorReason` also marks the call failed) |
| `systemFingerprint`          | string  | completions | Override of the provider's system fingerprint                               |
| `capturePrompts`             | bool    | all         | Turns prompt capture on or off for this call                                |
| `costMultiplier`             | float64 | completions | Multiplier applied to the provider cost                                     |
| `subscriptionTier`           | string  | completions | Subscription tier of a coding assistant (e.g. `pro`)                        |
| `systemPrompt`               | string  | completions | System prompt of the call; sent only with prompt capture on, with credentials redacted and cut to `REVENIUM_MAX_PROMPT_SIZE` |
| `effort`                     | string  | completions | Reasoning effort level (letters, digits, `-`, `_`; at most 16)              |
| `speed`                      | string  | completions | Speed mode, such as `fast` (letters, digits, `-`, `_`; at most 16)          |
| `querySource`, `subagentType` | string | completions | Subsystem or subagent that issued the call (at most 128 characters)         |
| `promptId`, `promptLength`, `clientSurface` | string, int, string | completions | Id and length of the prompt behind the call; the app that sent it |
| `codingAssistantAccountUuid` | string  | completions | Coding assistant account the call belongs to                                |
| `skillName`, `skillInvocationTrigger`, `skillSource`, `skillKind`, `skillPluginName`, `skillMarketplaceName` | string | completions | The skill that made the call |
| `sourceTransactionId`        | string  | image, video | Transaction id of the image or video this one edits or extends            |
| `priorityTier`               | string  | image, video | Priority tier of the request                                               |

A blank value (empty or only spaces) counts as not set. A value Revenium would refuse (a `pricingTier`
other than `STANDARD` or `BATCH`, an `effort` with spaces, a negative `promptLength`, and so on) is left
off the record with a warning, and the call is still recorded. A key the table does not list is not
recorded, and the SDK logs one warning naming it the first time it sees it.

`taskId`, `audioJobId` and `failureCode` are still accepted and sent for compatibility, but Revenium has no
field for them and does not store them. A field sent on a record type the Records column does not list
is dropped by Revenium.

`credentialAlias` names the API key a call used, so spend can be grouped by credential. It is sent as
`subscriber.credential.name`, the only place Revenium stores a credential name, and the same applies to
LiteLLM's `X-Revenium-Credential-Alias` header and Runway's `UsageMetadata.CredentialAlias`. The alias is
left off when the call's subscriber already has an `id` or `email`, or a credential with a `name` or
`value`, because the caller's own subscriber wins. Two Revenium limits apply: when Revenium already links
a credential to the subscriber, it keeps that credential and ignores the alias, and on teams with strict
ingestion the alias must be registered in Revenium before records that use it are accepted.

Accepted `operationSubtype` values, by operation type. Any other value is logged as a warning and ignored, and the detected subtype is sent instead. Chat and embedding payloads never carry a subtype:

| Operation type | Accepted values                                                          |
| -------------- | ------------------------------------------------------------------------ |
| IMAGE          | `generation`, `edit`, `variation`, `upscale`, `inpainting`               |
| AUDIO          | `transcription`, `translation`, `synthesis`, `speech`, `tts`, `realtime` |
| VIDEO          | `generation`, `upscale`, `extend`, `edit`                                |

## Trace Visualization Fields

Environment variables that set a default for every record, for distributed tracing and analytics:

| Environment Variable             | Record field          | Description                                                                |
| -------------------------------- | --------------------- | -------------------------------------------------------------------------- |
| `REVENIUM_ORGANIZATION_NAME`     | `organizationName`    | Default organization (`REVENIUM_ORGANIZATION_ID` is also read)             |
| `REVENIUM_PRODUCT_NAME`          | `productName`         | Default product (`REVENIUM_PRODUCT_ID` is also read)                       |
| `REVENIUM_ENVIRONMENT`           | `environment`         | Deployment environment; falls back to `ENVIRONMENT`, then `DEPLOYMENT_ENV` |
| `REVENIUM_REGION`                | `region`              | Cloud region; falls back to `AWS_REGION`, `AWS_DEFAULT_REGION`, `AZURE_REGION`, `GCP_REGION`, `GOOGLE_CLOUD_REGION` |
| `REVENIUM_CREDENTIAL_ALIAS`      | `subscriber.credential.name` | Name of the API key used (see `credentialAlias` above)              |
| `REVENIUM_TRACE_TYPE`            | `traceType`           | Categorical identifier (alphanumeric, hyphens, underscores, max 128 chars) |
| `REVENIUM_TRACE_NAME`            | `traceName`           | Human-readable label for trace instances (max 256 chars)                   |
| `REVENIUM_PARENT_TRANSACTION_ID` | `parentTransactionId` | Parent transaction reference for distributed tracing                       |
| `REVENIUM_TRANSACTION_NAME`      | `transactionName`     | Human-friendly operation label                                             |
| `REVENIUM_RETRY_NUMBER`          | `retryNumber`         | Retry attempt number (0 for first attempt)                                 |

The SDK reads these variables when a client is set up (`Initialize()` or a standalone constructor) and
puts each value on every usage record of that client whose usage metadata does not set the field; a
value set for the call always wins. Tool events take only `REVENIUM_AGENTIC_JOB_ID` from these defaults,
as in the Python SDK; set their organization and product on the event itself. An organization or product set in code (`ReveniumConfig.OrgID` and `ProductID`,
or fal's `WithReveniumOrgID` and `WithReveniumProductID`) wins over the two variables.

## Configuration Options

### Common Environment Variables

| Variable                     | Required | Description                                                |
| ---------------------------- | -------- | ---------------------------------------------------------- |
| `REVENIUM_METERING_API_KEY`  | Yes      | Revenium metering key (starts with `rev_mk_`; older keys start with `hak_`) |
| `REVENIUM_METERING_BASE_URL` | No       | Revenium API endpoint (default: `https://api.revenium.ai`) |
| `REVENIUM_DEBUG`             | No       | Enable debug logging (`true`/`false`)                      |
| `REVENIUM_PRINT_SUMMARY`     | No       | Terminal summary (`true`, `human`, `json`, `false`)        |
| `REVENIUM_TEAM_ID`           | No       | Team ID for cost display in terminal summary               |
| `REVENIUM_CAPTURE_PROMPTS`   | No       | Enable prompt capture (`true`/`false`)                     |
| `REVENIUM_MAX_PROMPT_SIZE`   | No       | Most characters kept in each captured field (default: 50000) |
| `REVENIUM_CAPTURE_ERROR_MESSAGES` | No  | Add the error text, redacted, to the error reason of a failed call and a failed tool event (`true`/`false`); text the provider answered with also needs `REVENIUM_CAPTURE_PROMPTS` |
| `REVENIUM_ENV_FILES_DISABLE` | No       | `true` stops `Initialize()` from reading `.env` files      |
| `REVENIUM_API_TIMEOUT`       | No       | Time one usage-record or tool-event request to Revenium may take, as `5s`, `1500ms` or a number of seconds (default: `10s`). Covers usage records and tool events only; see [Timeouts for calls to Revenium](#timeouts-for-calls-to-revenium) for the calls it does not change |
| `REVENIUM_BUFFER_MAX_BYTES`  | No       | Most bytes of usage records a client holds in memory, counting held records and records waiting for their first send, as a whole number (default: `104857600`, 100 MiB); past it the oldest held records are dropped, then new records |
| `REVENIUM_ORGANIZATION_NAME` | No       | Default organization for records (see [Trace Visualization Fields](#trace-visualization-fields)) |
| `REVENIUM_PRODUCT_NAME`      | No       | Default product for records                                |

Calls to Revenium that send usage records and job outcomes, and the cost-control rule fetch, honour the standard `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` variables, the same way the provider SDKs do.

### Timeouts for calls to Revenium

`REVENIUM_API_TIMEOUT` sets the timeout of usage-record and tool-event requests only. Raising it does not change the other calls the SDK makes to Revenium, which keep fixed timeouts:

| Call to Revenium | Timeout |
| ---------------- | ------- |
| Usage records and tool events, including held records resent later | `REVENIUM_API_TIMEOUT` per request (default: `10s`); `Close()` gives records still being sent this long, and never less than 10 seconds |
| Job calls made with `jobs.JobClient` (`ReportJobOutcome`, `AmendJobOutcome`, `AppendOutcomeMetrics`, `ListJobs`, `GetJob` and the other job reads) | Fixed 10 seconds per attempt |
| Cost-control rule fetch (with `REVENIUM_TEAM_ID` set) | Fixed 10 seconds per fetch; the first fetch runs inside `Initialize()` or a standalone constructor, and a failed fetch is tried again at the next poll, about every 30 seconds |

### Provider-Specific Variables

| Variable                         | Provider          | Description                                         |
| -------------------------------- | ----------------- | --------------------------------------------------- |
| `OPENAI_API_KEY`                 | OpenAI            | OpenAI API key                                      |
| `AZURE_OPENAI_API_KEY`           | Azure OpenAI      | Azure OpenAI API key                                |
| `AZURE_OPENAI_ENDPOINT`          | Azure OpenAI      | Azure resource endpoint URL                         |
| `AZURE_OPENAI_API_VERSION`       | Azure OpenAI      | API version (default: `2024-02-15-preview`; none on `/openai/v1` endpoints) |
| `AZURE_OPENAI_DEPLOYMENT_MODELS` | Azure OpenAI      | Deployment-to-model map for image and audio records (`dep=model,dep2=model2`) |
| `REVENIUM_AZURE_DISABLE`         | Azure OpenAI      | Turn Azure detection off (`true`)                   |
| `ANTHROPIC_API_KEY`              | Anthropic         | Anthropic API key                                   |
| `AWS_ACCESS_KEY_ID`              | Anthropic Bedrock | AWS access key (auto-enables Bedrock when paired with secret key) |
| `AWS_SECRET_ACCESS_KEY`          | Anthropic Bedrock | AWS secret key                                      |
| `AWS_SESSION_TOKEN`              | Anthropic Bedrock | Session token of temporary credentials (SSO, assumed role) |
| `REVENIUM_BEDROCK_DISABLE`      | Anthropic Bedrock | Disable Bedrock transport (`true`)                  |
| `REVENIUM_BEDROCK_USE_CONVERSE` | Anthropic Bedrock | Use Converse API instead of InvokeModel (`true`)    |
| `REVENIUM_BEDROCK_FALLBACK_TO_ANTHROPIC` | Anthropic Bedrock | Re-send a failed Bedrock call to the Anthropic API (`true`; off by default) |
| `GOOGLE_API_KEY`                 | Google GenAI      | Google AI Studio API key                            |
| `GOOGLE_CLOUD_PROJECT`           | Google Vertex     | GCP project ID (enables Vertex mode)                |
| `GOOGLE_APPLICATION_CREDENTIALS` | Google Vertex     | Path to service account key file                    |
| `GOOGLE_CLOUD_LOCATION`          | Google Vertex     | GCP region (default: `us-central1`)                 |
| `REVENIUM_VERTEX_DISABLE`        | Google Vertex     | Turn Vertex detection off (`true`)                  |
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

## Cost Controls / Enforcement

Per-call enforcement blocks outgoing provider requests when a subscriber has a breached `BLOCK` cost control configured in Revenium. Every provider middleware gates its billable calls — sync and streaming, across all surfaces (chat, responses, embeddings, images, audio, media generation) — before the request leaves the process.

Cost controls on a model match Claude calls through Bedrock by the Bedrock model id the records carry (for example `us.anthropic.claude-haiku-4-5-20251001-v1:0`), not the plain Claude name: a `MODEL` rule meant to cover them must name that id or use `CONTAINS`.

> **Terminology note:** The customer-facing entity is called a **cost control**, served by the backend at `/v2/api/ai/cost-controls`. This SDK polls a separate compiled-rules feed at `/v2/api/ai/enforcement-rules/{teamId}` and is unaffected by changes to the CRUD path — no SDK upgrade is required.

### Environment variables

| Variable                          | Required                         | Description                                                                                      |
| --------------------------------- | -------------------------------- | ------------------------------------------------------------------------------------------------ |
| `REVENIUM_METERING_API_KEY`       | Yes                              | Revenium metering key (starts with `rev_mk_`; older keys start with `hak_`). Metering scope is all it needs: do not put a write key (`rev_sk_`) here. The `_METERING_` infix is load-bearing — not `REVENIUM_API_KEY`. |
| `REVENIUM_TEAM_ID`                | Yes for enforcement              | Hashed team ID. If unset, the engine starts dormant (warns once, skips rule fetch) and all provider calls pass through. |
| `REVENIUM_ENFORCEMENT_API_KEY`    | No                               | Key used only to read the team's cost-control rules; never sent with usage records. Falls back to `REVENIUM_METERING_API_KEY`. Set a read key (`rev_rk_`) here when the rule fetch refuses your metering key with 403. |
| `REVENIUM_ENFORCEMENT_BASE_URL`   | No                               | Address the cost-control rules are read from. Falls back to `REVENIUM_METERING_BASE_URL`, which is all most setups need: the SDK adds `/profitstream/v2/api/ai/enforcement-rules/{teamId}` itself (a trailing `/profitstream` is tolerated). |

The engine refuses to start if the resolved base URL is not an absolute `http(s)://` URL with a host — a safety check so a misconfigured env var cannot ship the API key to an unexpected scheme.

When the rules cannot be loaded, the SDK logs a warning on every poll that names the setting to change: a 404 points at `REVENIUM_ENFORCEMENT_BASE_URL` / `REVENIUM_METERING_BASE_URL`, a 401 or 403 at `REVENIUM_TEAM_ID` and `REVENIUM_ENFORCEMENT_API_KEY`.

### How it works

Each provider's `Initialize()` / `NewReveniumX()` constructor calls `enforcement.StartFor(cfg.Revenium)`, which returns the engine for that client's rules address, rules key and team and, on first use, boots a background poller on `/profitstream/v2/api/ai/enforcement-rules/{teamId}` every 30s ± 5s jitter. Clients configured with the same rules address, rules key and team share one poll, which stops when the last of them is closed; clients configured for different teams each enforce only their own team's rules, so one process can serve several Revenium teams. Fetched rules are cached in memory; a `204 No Content` response caches an empty ruleset.

Before every provider call, the middleware builds an `EvalContext` with `enforcement.NewEvalContext` (subscriber id and email from `core.WithSubscriber`, or else from the `subscriber` entry of the usage metadata; product, organization, agent and task type from the usage metadata, with the default organization, product and credential alias applied when the call names none, as on its record; model and provider from the request) and checks it against the client's own engine with `Engine.Check`. Each breached rule is decided for the calling person the way the Revenium platform decides it:

- a department budget blocks only the people the platform lists as blocked for it (`departmentBudgetBlocks`), judged on their own balance;
- every other rule applies only when all of its filters cover the request (`IS`, `IS_NOT`, `IN`, `CONTAINS`, `STARTS_WITH`, `ENDS_WITH` on model, provider, product, subscriber, agent, organization, task type and credential); a rule with no filters covers every request, so a breached team-wide cap blocks everyone;
- a per-person cap (`groupBy: SUBSCRIBER`) blocks only a caller whose own `groupBreakdown` entry is breached;
- a subscriber filter compares the email when the call carries one, ignoring letter case, and the subscriber id otherwise;
- on a call to a model the provider can serve under a dated snapshot (OpenAI chat, responses, text completions and embeddings, Anthropic messages, Grok, and LiteLLM chat routes to OpenAI, Azure, Anthropic, xAI and any route no provider pattern recognises, such as a bare `o3` or an alias the proxy's model list did not resolve), a model filter is judged against the names the platform may meter for the call, as the Python SDK judges it: `MODEL IS gpt-4.1-nano-2025-04-14` also covers a `gpt-4.1-nano` call, which OpenAI serves and the OpenAI record carries under that dated name, and so do a leading `provider/` segment, a Vertex `@` snapshot and a `-latest` tail. `IS_NOT` leaves out only a call that names its own snapshot: `MODEL IS_NOT gpt-4.1-nano` still covers a `gpt-4.1-nano` call, which may be metered under a date the rule does not name, so to exempt such a model from a cap, name its dated form and call that form. Every other call (Azure chat and text completions, recorded under the deployment name, LiteLLM routes to providers known to serve the exact name, LiteLLM embeddings, fal, Runway, Ollama, Groq, Perplexity, Google, Bedrock, OpenAI images and audio) is served and metered under exactly the name it requested, so a model filter compares that name exactly and `MODEL IS_NOT fal_ai/fal-ai/flux/dev` exempts a call to that endpoint;
- rules where `shadowMode == true` or `action ∈ {WARN_ONLY, THROTTLE}` are logged and never block — these are observation-only client-side, server still enforces server-side throttling;
- `*ErrCostLimitExceeded` is returned for the first `BLOCK` rule that fires, carrying the caller's own balance for a per-person cap or department budget.

The engine fails open: fetch errors, network outages, missing team IDs, and an unreachable Revenium API all result in `Check` returning `nil` (request passes). Enforcement errors never bubble to user code as metering failures. A rules answer that arrives but cannot be read or decoded is logged at `ERROR` with the reason. A process that has not loaded the rules yet then enforces none of them; one that loaded them earlier keeps enforcing those last good rules until they are ten polls (five minutes) old, and then allows every call until a fetch succeeds. The rule fetch asks for a gzip-compressed answer, so a team whose per-person caps list tens of thousands of people still loads in full.

### Error shape

```go
type ErrCostLimitExceeded struct {
    RuleID       string       // numeric server ruleId, stringified for Node SDK parity
    RuleName     string       // server-provided human-readable rule name
    Threshold    float64      // rule limit
    CurrentValue float64      // subscriber's metered value at block time
    PeriodType   string       // DAILY / WEEKLY / MONTHLY / QUARTERLY
    Action       Action       // always BLOCK today; retained for future hard-stop actions
    Context      EvalContext  // subscriberId / productName / model / provider at call time
}
```

Recover the structured fields with `errors.As`:

```go
resp, err := client.Chat().Completions().New(ctx, params)
if err != nil {
    var ece *enforcement.ErrCostLimitExceeded
    if errors.As(err, &ece) {
        log.Printf("blocked by rule %s: $%.2f of $%.2f %s limit",
            ece.RuleID, ece.CurrentValue, ece.Threshold, ece.PeriodType)
        return // or degrade gracefully
    }
    return // not an enforcement error — handle as usual
}
```

### Rule.Action enum

```go
const (
    ActionBlock    Action = "BLOCK"     // reject the call with ErrCostLimitExceeded
    ActionThrottle Action = "THROTTLE"  // observed client-side; server applies the rate limit
    ActionWarnOnly Action = "WARN_ONLY" // observed client-side; log only
)
```

`shadowMode: true` on any rule (including `action: BLOCK`) downgrades it to observation-only for this SDK — the rule is logged but never throws. This matches the Node SDK semantics so a rule can be safely rolled out in shadow mode before flipping to enforcement.

### End-to-end example

```go
package main

import (
    "context"
    "errors"
    "log"

    oai "github.com/openai/openai-go/v3"
    "github.com/revenium/revenium-go-sdk/core"
    "github.com/revenium/revenium-go-sdk/core/enforcement"
    revopenai "github.com/revenium/revenium-go-sdk/openai"
)

func main() {
    if err := revopenai.Initialize(); err != nil {
        log.Fatal(err)
    }

    client, _ := revopenai.GetClient()

    ctx := core.WithSubscriber(context.Background(), &core.Subscriber{
        ID:    "samuel.combs@revenium.io",
        Email: "samuel.combs@revenium.io",
    })

    _, err := client.Chat().Completions().New(ctx, oai.ChatCompletionNewParams{
        Model:    "gpt-4o-mini",
        Messages: []oai.ChatCompletionMessageParamUnion{oai.UserMessage("hello")},
    })

    var ece *enforcement.ErrCostLimitExceeded
    switch {
    case errors.As(err, &ece):
        log.Printf("cost limit hit: rule=%s current=$%.2f threshold=$%.2f",
            ece.RuleID, ece.CurrentValue, ece.Threshold)
    case err != nil:
        log.Printf("provider error: %v", err)
    }
}
```

### Scope safety

A filter the SDK cannot evaluate (an unknown dimension or operator, or a department filter) never matches, so such a rule is left to the platform instead of blocking traffic it may not cover. A per-person cost cap whose payload carries no `groupBreakdown` is left to the platform for the same reason.

### End-to-end smoke test

`scripts/e2e-enforcement/` ships a 6-phase smoke that exercises live dev, exception contract, shadow mode, fail-open paths, and the wrapped provider block. Set `DEV_API_BASE_URL` / `DEV_API_KEY` / `DEV_TEAM_ID` / `DEV_USER_EMAIL` in `~/revenium/.env`, then:

```bash
set -a ; source ~/revenium/.env ; set +a
cd scripts/e2e-enforcement
go run .
```

Results land in `scripts/e2e-enforcement/.e2e-report.md`. Exit code is non-zero if any phase fails.

## Troubleshooting

### No tracking data appears

1. Verify environment variables are set correctly (exported in the shell, or in a `.env` file in the working directory; see [.env files](#env-files)).
2. Enable debug logging: `REVENIUM_DEBUG=true`.
3. Check console for `[Revenium DEBUG]` / `[Revenium INFO]` log messages.
4. Verify your `REVENIUM_METERING_API_KEY` is a valid metering key (starts with `rev_mk_`; older keys start with `hak_`).
5. Look for a `usage records are not reaching Revenium` warning. It means the Revenium API cannot be reached; behind a corporate proxy, set `HTTPS_PROXY` (and `NO_PROXY` for hosts that must bypass it).

### `middleware not initialized` error

- Make sure you call `Initialize()` before `GetClient()`.
- Check that your `.env` is in the working directory the process starts in (a `.env` in a parent directory is not read), or export the variables.
- Verify `REVENIUM_METERING_API_KEY` is set.

### Azure OpenAI not metering

- Confirm `AZURE_OPENAI_API_KEY` and `AZURE_OPENAI_ENDPOINT` are both set; `AZURE_OPENAI_API_VERSION` is optional.
- Check that `REVENIUM_AZURE_DISABLE` is not set to `true`.
- The `model` field should be the Azure **deployment name**, not the base OpenAI model name.

### fal.ai `FAL_API_KEY is required`

- fal.ai's official env var is `FAL_KEY`; this SDK accepts both `FAL_KEY` and `FAL_API_KEY`.

### Debug Mode

```env
REVENIUM_DEBUG=true
```

Then every provider module logs at debug level, including each outgoing metering payload in full, to stderr, whether it is set up with `Initialize()` or a standalone `NewRevenium*` constructor; `ReveniumConfig.Debug = true` does the same. `REVENIUM_DEBUG=true` wins over `REVENIUM_LOG_LEVEL`. Debug logging is process-wide: once one client turns it on, a client built without it does not turn it off.

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

## Data & Privacy

By default, the SDK transmits only usage metrics to Revenium:

- Provider name and model identifier
- Token counts (input, output, total)
- Request latency and timing
- Transaction identifiers and stop reasons
- For a failed call, the provider's error type or code and HTTP status, such as
  `invalid_request_error/invalid_api_key (HTTP 401)`, with the status also sent as `errorCode`

**No prompts, responses, or API keys are sent by default.** Prompt capture is opt-in via `REVENIUM_CAPTURE_PROMPTS=true`, and when enabled, credentials are automatically sanitized before transmission.

With prompt capture on, chat calls through every chat provider (OpenAI, Azure OpenAI, xAI through the OpenAI client, Anthropic, Anthropic on Bedrock, Google GenAI and Vertex AI, Perplexity, LiteLLM, Groq, Grok and Ollama), streamed or not (Groq, Grok and Ollama do not stream), add four fields to the usage record:

- `systemPrompt`: the system prompt (OpenAI-style first `system` message, Anthropic `System` blocks, Google `SystemInstruction`, Responses API `Instructions`)
- `inputMessages`: the other messages sent, as a JSON array (inline images, audio and files are replaced by `[BINARY_DATA]`)
- `outputResponse`: the reply text (Anthropic tool calls are included as JSON)
- `promptsTruncated`: `true` when one of the three was cut to `REVENIUM_MAX_PROMPT_SIZE`, or the caller stopped reading a stream before its end

Credentials in all three are redacted wherever the Python SDK redacts them: provider and Revenium API keys, tokens, passwords and secrets after a secret-named label, Bearer and Basic credentials, JSON Web Tokens, private key blocks, card numbers and signed or password-bearing URLs. Go also redacts a few shapes the Python SDK does not yet: a value assigned with `:=`, `=>` or `==` after a secret-named label, a token or key label followed by only a space, a card number stored as a JSON number, and a Basic credential cut off at the end of a reply cut short. Set `capturePrompts` in the call's usage metadata to turn capture on or off for that call alone. Only the caller decides: a LiteLLM proxy's `X-Revenium-Capture-Prompts` response header never turns capture on or off, so it cannot put a `systemPrompt` from your usage metadata on the record or make `ReconstructResponse` rebuild a stream. Records of failed calls carry no captured messages or reply, and the extra records sent for Claude advisor and compaction passes carry none of these fields.

A provider's error reply can repeat part of the API key, a key hash or the request itself, so the error
text of a failed call is not sent by default. With `REVENIUM_CAPTURE_ERROR_MESSAGES=true` it is added to
the error reason with credentials, URL user info, query-string values and email addresses redacted; text
the provider answered with is added only when `REVENIUM_CAPTURE_PROMPTS=true` as well. An error reason is
at most 255 characters, the length Revenium stores; a longer one, including an `errorReason` set in usage
metadata, is cut with a warning.

## Versioning & Stability

This SDK follows [Semantic Versioning](https://semver.org/). The API is stable and ready for production use.

- **Current version**: v1.x (stable)
- **Backward compatibility**: Guaranteed within major versions
- **Go versions**: Go 1.21 to 1.25 by module, as listed in [Requirements](#requirements); CI builds and tests every module on its own minimum Go and on Go 1.25 and 1.26
- **Upstream SDKs**: tested against `openai-go/v3` v3.66, `anthropic-sdk-go` v1.76, `google.golang.org/genai` v1.71

See [CHANGELOG.md](CHANGELOG.md) for release history.

## Examples

For complete, runnable examples for each provider, see the [examples/](examples/) directory:

- `examples/openai/` -- Chat, streaming, embeddings, metadata, prompt capture
- `examples/anthropic/` -- Chat, streaming
- `examples/google/` -- Chat, streaming
- `examples/litellm/` -- Chat via LiteLLM proxy
- `examples/perplexity/` -- Chat
- `examples/fal/` -- Image generation
- `examples/tool-metering/` -- Custom tool event reporting
- `examples/job-metering/` -- Job outcome tracking

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, how to add a new provider, and PR guidelines.

## Security

See [SECURITY.md](SECURITY.md). Report vulnerabilities to support@revenium.io -- do not create public issues.

## License

MIT -- see [LICENSE](LICENSE).

## Getting Help

- **Documentation**: [docs.revenium.io](https://docs.revenium.io)
- **Bug Reports**: [GitHub Issues](https://github.com/revenium/revenium-go-sdk/issues)
- **Email**: support@revenium.io
- **Website**: [www.revenium.ai](https://www.revenium.ai)
