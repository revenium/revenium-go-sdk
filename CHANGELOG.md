# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.9] - 2026-10-23

### Fixed

- **Cost controls (all providers)**: a department cost cap scoped to part of a person's calls, such as
  `PROVIDER IS openai`, blocked every call that person made once it was over, including Anthropic calls the cap
  never counted. When the platform lists the caps a person is over or near (`departmentBudgetCandidates`), the SDK
  now applies the first one whose filters cover the call, comparing values without regard to letter case and
  treating a filter it cannot evaluate, or a credential filter on a call that names its credential only by raw value, as covering. A near-limit cap only logs. A payload without the list keeps
  the previous behaviour. A cap whose filter names a dimension the call does not carry, such as `PRODUCT IS checkout`
  on a call sent without a product, does not cover that call and so does not block it; the platform, which knows the
  dimension for every call, still counts it. A listed cap is read whether its rule id arrives as a number or a string,
  and one with an id of zero or less is ignored, as the platform ignores it
- **Cost controls (OpenAI, Anthropic, Grok, LiteLLM)**: a model cap written with the dated name Revenium shows, such as
  `MODEL IS gpt-4.1-nano-2025-04-14`, never blocked a `gpt-4.1-nano` call, and `MODEL IS_NOT gpt-4.1-nano`
  never blocked one either, while the Python SDK blocks both because the platform meters the dated name.
  Model filters are now judged against the names the platform may meter for the call (the dated form, a
  `provider/` route, a Vertex `@` snapshot, a `-latest` alias), as in the Python SDK. `IS_NOT` exempts only
  a call that names its own snapshot. This applies only to calls to models the provider can serve under a dated snapshot
  (OpenAI chat, responses, text completions and embeddings, Anthropic messages, Grok, and LiteLLM chat routes to OpenAI, Azure,
  Anthropic, xAI and any route no provider pattern recognises, such as a bare `o3` or an alias the proxy's model list did
  not resolve); Azure chat and text completions (recorded under the deployment name), LiteLLM routes to providers
  known to serve the exact name, LiteLLM embeddings, fal, Runway,
  Ollama, Groq, Perplexity, Google, Bedrock and OpenAI image and audio calls are served and recorded under exactly the
  requested name and keep exact model comparison, so an `IS_NOT` cap naming such a model still exempts it
- **All providers**: `REVENIUM_DEBUG=true` and `ReveniumConfig.Debug` did nothing for clients built with the
  standalone `NewRevenium*` constructors; they worked only through `Initialize()`. They now turn on debug
  logging, including the outgoing payload, on every constructor. A second `Initialize()` with a different
  `REVENIUM_ENFORCEMENT_API_KEY` or `REVENIUM_ENFORCEMENT_BASE_URL` was silently ignored and kept reading
  cost controls with the first key and address; it now returns an error naming the setting, as a changed
  metering key already did
- **LiteLLM**: a person the proxy names only in its `X-Revenium-Subscriber-*` response headers was recorded
  under that person but never checked against cost controls. The client now remembers the person the proxy
  named and checks the next call with the same request `user` value as that person; the first call from
  such a person still cannot be checked, because the proxy names the person only after the call
- **OpenAI and Azure**: three cases left image and audio calls unpriced or unreachable. An Azure
  endpoint or `BaseURL` on the v1 API (`https://<resource>.openai.azure.com/openai/v1`) was sent to
  `/openai/v1/openai/deployments/...`, which Azure refuses; the SDK now calls the v1 paths under that URL
  with the Azure key, and accepts a trailing `/openai` on the dated form. An OpenAI image generated,
  edited or varied without a model was recorded with no model; a client calling OpenAI itself now records
  OpenAI's documented default (`dall-e-2` for generations and variations, `gpt-image-1.5` for edits, and
  `gpt-image-1` for a generation that sets a GPT-image-only size or option) and sends the request
  unchanged. A call through an OpenAI-compatible proxy, Azure or xAI still records no model, because the
  default it gets is not documented. Azure image and audio records carried the deployment name,
  so a deployment not named after its model cost $0; map it with `AZURE_OPENAI_DEPLOYMENT_MODELS` or
  `WithAzureDeploymentModels` and the record names the catalog model
- **OpenAI and Azure**: streamed chat and legacy Completions records reported first-token times of a few
  milliseconds and durations that left out the wait for the first response, because the clock started only
  after the provider had answered and counted the role-only opening chunk as the first token; a caller that
  closed the stream late also stretched the duration to the `Close()` call. The clock now starts before the
  request is sent, as the Responses API path already did, the first token is the first chunk that carries
  output, and the duration and response time end once every choice has its finish reason, or at the end
  of the stream when they do not. Streamed Responses API records had the same late-`Close()` inflation; they now end at
  the `response.completed`, `response.incomplete` or `response.failed` event, and their first token is the
  first output delta, including refusals and tool-call arguments
- **OpenAI**: a Responses call that OpenAI cut short with its content filter (`incomplete_details.reason`
  `content_filter`) was recorded with stop reason `TOKEN_LIMIT`, the same as an answer that ran out of
  room. It is now recorded as `ERROR`, the stop reason chat calls and the other providers use for a content
  filter, streamed or not. An answer stopped at `max_output_tokens` stays `TOKEN_LIMIT`
- **Runway**: customer, product, organization, agent, trace and job fields set on the request context with
  `core.WithUsageMetadata`, as the README shows for every provider, were missing from `ImageToVideo`,
  `VideoToVideo` and `UpscaleVideo` records, so Runway spend showed as unattributed, and cost controls on
  those fields did not apply to Runway calls. Records and cost controls now read the context; when the
  context and the `metadata` argument set the same field, the context value wins, as it does for fal
 
- **Runway and fal**: when the caller's context ended while a Runway video or a fal queued job
  (`Subscribe`) was still running, the call returned an error and no usage record was sent, although
  the provider went on to finish and bill the work. The Runway wait also ran past the caller's deadline
  by up to one poll pause (as much as 10 seconds). The call now returns at the deadline, and the SDK
  keeps polling the accepted task in the background, retrying temporary failures, and records it with
  its real outcome. A fal queued job whose status request fails while the caller is waiting is followed
  up the same way. A task whose outcome the SDK still cannot learn when it stops polling (its polling
  limit, or `Close()`) is recorded with stop reason `ERROR`, the requested length or image count and an
  error reason that names the task id and says the outcome is unknown. `Close()` now waits for those background polls to end
- **fal**: models published outside fal's own namespace, such as `bria/embed-product`, were recorded
  under an invented name (`fal_ai/fal-ai/bria/embed-product`) that no price matches, and every call
  logged a warning that the model id was wrong. They are now recorded as `fal_ai/` plus the id you
  called (`fal_ai/bria/embed-product`), the form the price catalog uses for partner models, with no
  warning. `fal-ai/...` ids are recorded exactly as before; the warning now fires only for an id with no
  namespace at all, such as `flux`
- **fal**: chat models run through fal were always recorded as finished normally, even when the
  answer was cut off at the token limit or ended on a tool call, and cached input and reasoning tokens
  were dropped, so cached input was billed at the full input rate. Records now carry the stop reason
  from `finish_reason` (`TOKEN_LIMIT` for `length`, `END_SEQUENCE` for tool calls, `ERROR` for a content
  filter), `cacheReadTokenCount` from `prompt_tokens_details.cached_tokens`, and `reasoningTokenCount`
  from `completion_tokens_details.reasoning_tokens`. `inputTokenCount` is now the uncached input,
  because Revenium does not remove cached tokens from fal's input count itself
- **fal and Runway**: with prompt capture on, API keys, tokens and passwords in a prompt were stored in
  Revenium as typed, `REVENIUM_MAX_PROMPT_SIZE` was ignored (prompts were sent up to 50,000 characters)
  and the captured answer was never trimmed. An image fal returned inline (`sync_mode`) was copied whole
  into the record, about 1.5 MB per image, and up to 1,000 such records could be held in memory while
  Revenium was unreachable. Captured prompts and answers now have credentials redacted, as the README
  promises, and are cut to `REVENIUM_MAX_PROMPT_SIZE` characters, and inline images, videos and audio
  (`data:` URIs) are left out of the captured answer; links are still captured
- **fal and Runway**: `capturePrompts` in a call's usage metadata, which the README says turns prompt
  capture on or off for one call on every provider, was ignored on successful fal and Runway records: a
  call that turned capture off still had its prompt and answer stored in Revenium, and a call that turned
  it on stored nothing. fal `Run` and `Subscribe` records and successful and unfinished Runway video
  records now follow the call's setting, falling back to the client's, with the same redaction and size
  limit. Failed, cancelled and refused records still carry no captured prompt or answer
- **Runway**: a video from a model outside the SDK's default-length table, such as `gen4_aleph`
  (Runway's video-to-video model), called without `Duration` was billed at 10 seconds whatever its real
  length. `VideoToVideoRequest` has a new `SourceDurationSeconds` field (never sent to Runway) that is
  billed when `Duration` is left out, since `gen4_aleph` output follows the input video. With neither
  length and no known default, the call is recorded with 0 seconds, stop reason `ERROR` and an error
  reason starting `video_length_unknown` instead of a guessed length. Models in the table keep their
  default, and an explicit `Duration` always wins
- **fal**: video calls to families outside the SDK's default-length table, such as `sora-2`, Wan 2.2
  and `hunyuan-video`, made without a `duration` were sent with no length, so Revenium rejected the
  record and the spend was lost. The SDK now reads the length fal reports (`duration`, or frame count
  over frame rate), then the request's `duration` or `num_frames` and `frames_per_second`, then the
  family default, which now covers these families. A video whose length is known nowhere is recorded
  with 0 seconds, stop reason `ERROR` and an error reason starting `video_length_unknown` instead of
  being rejected
- **fal and Runway**: a fal or Runway task the caller stopped waiting for was polled in the
  background for up to 30 minutes before being recorded as unfinished, so a process that exited sooner
  without `Close()` lost the record. The background poll now records such a task after at most 10
  minutes, or at `Close()`, whichever comes first; the README now says short-lived programs must call
  `Close()`. A fal `Subscribe` whose submit got no answer, a 5xx answer or an unreadable answer is no
  longer recorded as a failed call with no quantity, because fal may have queued and billed the job;
  it now logs a warning, as Runway task creation already did. A 4xx refusal is still recorded as a
  failed call on both

- **Google**: Gemini chat sessions were not recorded, so each turn after the first was paid to Google with
  nothing in Revenium. The new `Chats().Create()` returns a chat whose `SendMessage` and `SendMessageStream`
  turns each send one usage record and pass the team's cost controls, keeping the same history as Google's
  own chat
- **Anthropic**: Claude beta messages, including the agent tool runner, were only reachable through the raw
  client and were never recorded. The new `BetaMessages()` offers `CreateMessage`, `CreateMessageStream`,
  `NewToolRunner` and `NewToolRunnerStreaming`; each call, and each model turn of a tool runner, sends one usage
  record and passes the team's cost controls
- **OpenAI**: legacy Completions calls (`gpt-3.5-turbo-instruct`) had no recorded method. The new
  `Completions().New()` and `NewStreaming()` send one usage record per call on OpenAI and Azure and pass the
  team's cost controls
- **All providers**: the README said every API call is recorded and called the raw OpenAI client "wrapped".
  It now says that calls made through `GetOpenAIClient()`, `GetAnthropicClient()` and `GetGenaiClient()` are
  not recorded, and lists per provider the billable calls the SDK does not record, among them OpenAI Sora
  video, Batch, Realtime and streamed images, Anthropic Message Batches, and Gemini batches, context caches
  and Live

- **Cost controls**: a person over a breached BLOCK cost control is now stopped on every provider and surface (chat, responses, embeddings, images, audio, media generation, sync and streaming), not only OpenAI chat and Gemini. Rules that stop answering for ten polls (about five minutes) stop deciding calls, so a stale block cannot outlive an outage
- **OpenAI, Azure, Google**: image, speech, transcription, translation, Imagen and Veo calls were
  stored at no cost, and HD images at the standard price, because their records named the provider
  in a form the price catalog does not list and sent size, quality, character count and billing unit
  inside an `attributes` wrapper the platform ignores. Media records now use the catalog names
  (`openai`, `azure`, `gemini`, `vertex_ai-image-models` and `vertex_ai-video-models`, with Vertex
  models recorded as `vertex_ai/<model>`) and send `resolution`, `quality`, `characterCount` and
  `billingUnit` as top-level fields. Transcriptions are billed per second and token-billed ones per
  audio token, speech per character, and gpt-image records carry the token usage OpenAI reports.
  Chat records keep their provider names

- **Google**: streamed Gemini and Vertex calls were always recorded with stop reason `END`, so answers cut
  off at the token limit or stopped by a safety filter looked like normal finishes; streams now record the
  finish reason of their last chunk, and Google's new `CONTINUATION` reason maps to `TOKEN_LIMIT`
 . The weekly provider drift issue now reports the surface check result
- **Google**: Gemini thinking tokens were sent only as `reasoningTokenCount`, which the platform does not
  price, so thinking calls were recorded at a small fraction of what Google bills; `outputTokenCount` now
  includes thinking tokens, which Google bills at the output rate, and `reasoningTokenCount` still carries
  the thinking count on its own
- **Google**: Veo videos made with `GenerateVideo` or `GenerateVideoFromSource` were recorded only when
  `WaitForVideo` was used, and then with a length of 0 seconds, so they cost nothing; each finished video
  operation is now recorded once whether you wait with `WaitForVideo` or poll with Google's
  `Operations.GetVideosOperation`, with the requested clip length (Veo's 8-second default when none is
  set) times the number of videos generated. Veo submissions are gated on the team's cost controls, as Gemini content calls already were, so a person over a breached BLOCK rule cannot start a video
- **Google**: Vertex AI embeddings were recorded with zero tokens, so they showed no cost. Records now
  carry the input token count Vertex reports for each embedding, summed across a batch, as input and
  total tokens. The Gemini API reports no token count for embeddings, so its records still carry zero
  tokens
- **Google**: Gemini and Vertex calls that used built-in tools (code execution, search grounding, URL
  context) were recorded with only the prompt as input, although Google feeds the tool results back to the
  model and bills them as input, so most of the billed input could be missing from cost. Streamed and
  non-streamed records now add Google's `toolUsePromptTokenCount` to `inputTokenCount`, so input plus
  output matches the total Google reports. Calls without built-in tools are recorded as before
- **Google**: a successful Gemini or Vertex reply without usage numbers crashed the caller's
  `GenerateContent` (and chat `Send`) inside the SDK with a nil pointer error, at every log level, and a
  streamed reply without usage numbers was not recorded at all. Both now return the reply as Google sent
  it and send a usage record with zero tokens, so the call is still counted
- **Google**: `CreateEmbedding`, `GenerateImage`, `EditImage` and `UpscaleImage` were not checked against
  the team's cost controls, so a person over a breached BLOCK rule could still run them, although the README
  says every billable call is gated. They now pass the same check as `GenerateContent` and Veo, and a blocked
  call returns `ErrCostLimitExceeded` before any request reaches Google
- **Google**: a Veo video submitted in one process and polled in another with Google's own
  `Operations.GetVideosOperation()` (a web request that submits and a worker that polls, or a service that
  restarted while the video rendered) was never recorded, because only the submitting client tracked it.
  The client that sees the operation finish now records it, with the model named in the operation, the
  attribution set on the polling call's context, and the same transaction id and Idempotency-Key any
  other process would use, so the video is counted once. Those ids come from the operation name even when
  a call's usage metadata sets `transactionId` or `idempotencyKey`; a `transactionId` set there becomes the
  record's `traceId` when the call sets none. Google's operation does not carry the requested length, so such a video is
  recorded at Veo's default 8 seconds per video; `WaitForVideo()` with the original config records the
  requested length. A call the genai client refuses before sending anything, such as `GenerateImage()` on
  the Gemini API, which genai supports only on Vertex AI, was recorded as a failed call Google never
  received; it is no longer recorded, on every Google call
- **Google and Anthropic on Bedrock**: a streamed Gemini or Vertex call that you stopped reading before
  Google sent a finish reason was recorded as finished normally (`END`), while prompt capture marked its
  reply truncated; it is now recorded with stop reason `CANCELLED`. A Claude call through Bedrock
  InvokeModel that a Bedrock guardrail blocked was recorded as a normal stop, because InvokeModel keeps
  Claude's `end_turn` and reports the block only in `amazon-bedrock-guardrailAction`; it is now recorded as
  `ERROR`, streamed or not, and the returned message's stop reason is `guardrail_intervened`, as on
  Converse. The README now says that Gemini API embeddings are recorded with zero tokens and no cost,
  because the Gemini API returns no token count for them

- **fal**: upscale, background-removal and face-swap calls (endpoints that return a single `image`)
  were metered as zero images and so cost nothing; they now report one image with its width and
  height. Video calls that used the `"8s"` duration form or left the duration at the endpoint
  default were sent with no length and rejected; they now carry the requested length or the
  endpoint's published default. Speech results that report `duration_ms` now carry that length
 
- **Runway**: every video was metered as 5 seconds long whatever length was requested, so a
  10-second video was billed at half its cost. `ImageToVideo` and `VideoToVideo` now bill the
  requested `Duration`, or the model's default length when it is omitted (10 seconds for
  `gen4_turbo`, 8 for Veo). `UpscaleVideo` bills the new `VideoUpscaleRequest.SourceDurationSeconds`
  field, which is used only for metering and is not sent to Runway.
  **Impact**: `UpscaleVideo` now returns a validation error (`runway.ErrMissingSourceDuration`) and
  sends nothing to Runway when `SourceDurationSeconds` is not set, because Runway does not report
  the upscaled length and the upscale cannot be billed without it. Upscale callers must pass the
  input video's length
- **OpenAI**: streamed Chat Completions on OpenAI and Azure written as the README shows were recorded with zero
  tokens and zero cost, because the provider only reports usage when asked. The SDK now asks for it on every
  stream; the extra usage-only chunk stays hidden unless you set `stream_options.include_usage` yourself
- **All streaming providers**: closing a stream more than once, for example a deferred `Close()` plus the explicit
  call the README shows, sent one usage record per call, so the request was counted and billed twice. OpenAI Chat
  Completions, OpenAI Responses, Anthropic, Perplexity and LiteLLM streams now send exactly one record
- **OpenAI**: transcriptions and translations on OpenAI and Azure were recorded with no audio length unless the
  verbose response format was used, and token-billed transcription models were recorded with zero tokens, so these
  calls cost nothing. The SDK now reads the audio length or token usage OpenAI reports in its default response,
  asks for the verbose format on JSON translations (the returned text is unchanged), and logs one warning if a
  response carries no billable quantity. Such a call is now recorded with an explicit length of zero seconds,
  because the platform drops an audio record that carries no length at all
- **OpenAI**: streamed Responses calls that stopped at the output token limit or failed on OpenAI's side were
  recorded with zero tokens, zero cost and a normal finish. They now carry the usage from the final event, with
  `TOKEN_LIMIT` for a truncated answer and `ERROR` plus OpenAI's error message for a failed one
- **OpenAI and Azure**: calls could go to the wrong service or be sent twice. A copied `.env.example` with
  only the OpenAI key filled in sent every OpenAI call to a placeholder Azure address (and a Gemini setup to
  Vertex AI); the Azure and Vertex lines are now commented out. An Azure setup without
  `AZURE_OPENAI_API_VERSION` called public OpenAI with the OpenAI key while the record said Azure; it now calls
  Azure with the documented default version, `2024-02-15-preview`. A `BaseURL` on an Azure host now reaches
  that host, and the SDK refuses to start when no Azure key is set for it. A failed Azure call was sent to
  Azure a second time and recorded again as OpenAI; it now returns its error after one call and one `AZURE`
  record. The README documents `REVENIUM_AZURE_DISABLE` and `REVENIUM_VERTEX_DISABLE`
- **OpenAI and Azure**: prompt-cache writes, which OpenAI bills at 1.25x the input rate on GPT-5.6 and later,
  were recorded as plain input, so those calls were priced low. Chat Completions, legacy Completions and
  Responses calls, sync and streamed, now record the `cache_write_tokens` OpenAI reports as cache-creation
  tokens and leave them out of the input count, so they are priced once, at the cache-write rate. Replies
  without that field are recorded as before
- **OpenAI**: Grok calls made through the OpenAI client with xAI's base URL (`https://api.x.ai/v1`) were
  recorded as OpenAI spend, so cost by provider was wrong. A base URL on `x.ai` or one of its subdomains, set
  with `WithBaseURL` or through `OPENAI_BASE_URL`, is now recorded with provider `XAI` and model source `XAI`,
  and the reasoning tokens xAI reasoning models leave out of `completion_tokens` are billed as output, as the
  `grok` package does. Azure detection still comes first and now matches only Azure domains on the host
  (`azure.com`, `azure.us`, `azure.cn`, `azure-api.net`), so a proxy whose address merely mentions Azure is
  no longer switched to Azure and refused at start

- **Cost controls**: blocking cost controls now apply to the same people as on the Revenium platform.
  A team-wide cap, a per-developer cap and a department budget no longer pass everyone; a per-developer
  cap or department budget no longer blocks every developer once one person is over it; a cap on a
  person's email now blocks them when the call also carries their id or uses different letter case;
  and "is one of", "is not", "contains", "starts with", "ends with" and agent, organization, task type
  and credential filters are honoured instead of being ignored
- **Cost controls**: clients set up for different Revenium teams in one process each enforce their own
  team's cost controls. Previously every client used the first client's rules, so a second team's caps
  never applied and the first team's caps could block the second team's calls; a first client set up
  without a team switched cost controls off for every later client. Clients with the same team still
  share one rule fetch, and closing the last client for a team stops that team's rule polling.
  `enforcement.Get` and the package-level `enforcement.Check` are deprecated in favour of the `*Engine`
  returned by `enforcement.Start` or `enforcement.StartFor`
- **Cost controls**: the cost-control rule fetch now honours `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`,
  so behind a company web proxy the rules load and blocking caps apply
- **Cost controls**: the cost-control rules can be read with their own key, `REVENIUM_ENFORCEMENT_API_KEY`,
  so a read key (`rev_rk_`) loads the rules while `REVENIUM_METERING_API_KEY` stays a metering key. Where
  the platform refused the metering key on the rule fetch, the only way to get caps enforced was a write key
  in the metering setting, which was then sent with every usage record. A refused (401/403) or not-found
  (404) rule fetch now logs which setting to change, and the docs no longer describe the metering key as
  `hak_` or send dev users to a separate rules address
- **Cost controls**: once a few thousand people had spent under one per-person cap, the team's rules answer
  grew past 1 MiB, the SDK cut it off there and failed to decode it, and every cost control of the team
  stopped loading, including unrelated model blocks; the only sign was a warning every 30 seconds. The SDK
  now reads the whole answer (a team with 30,000 people on a per-person cap loads every rule), asks for it
  gzip-compressed so each poll downloads a fraction of the bytes, and logs an answer it cannot read or
  decode at ERROR with the reason

- **All providers**: a subscriber attached with `core.WithSubscriber`, as the README shows, was never
  stored on the usage record, so that person's spend was unattributed. Its `ID` and `Email` are now
  sent on every record. When a call also carries a `subscriber` in the usage metadata map, the typed
  subscriber wins, and cost controls match the same subscriber the record carries
- **All providers**: behind a corporate web proxy, usage records and job outcomes never reached
  Revenium because the SDK's own connections ignored `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`.
  They now honour those variables. When usage records cannot be delivered because Revenium is
  unreachable, one warning is logged at the default level, and an info line when delivery recovers
 
- **All providers**: a Revenium base URL without `https://` or `http://`, or a Revenium key with a stray
  space or line break, passed setup and then no usage was ever delivered, and after five failures every
  other client in the process had its records held back too. Leading and trailing whitespace (such as the
  newline a secret file leaves behind) is now trimmed by `Initialize()`, the standalone `NewRevenium*`
  constructors and `metering.NewMeteringClient` alike, and setup returns an error naming
  `REVENIUM_METERING_BASE_URL`, `REVENIUM_ENFORCEMENT_BASE_URL` or `REVENIUM_METERING_API_KEY` for any other
  such value. A request that cannot be built is reported at once and is no longer retried, buffered or
  counted as a Revenium outage
- **All providers**: one bad tag value cost the whole usage record: a `ticketId` over 256 characters was
  rejected by Revenium, a `NaN` or infinite number (from a float or a string such as `"NaN"`) stopped the
  record from being encoded, and an `idempotencyKey` with a line break or non-ASCII character never left
  the process and held back every other record waiting to be resent. A long `ticketId` is now shortened to
  256 characters, a non-finite number is dropped from that one field, and an unusable idempotency key
  (anything but 1 to 255 printable ASCII characters without spaces) is replaced with a generated one, each
  with a warning, and the record is sent. A record that can never be sent is dropped with an error log
  instead of being retried, and no longer counts as a Revenium outage or blocks the records behind it; held
  records that are dropped or expire are now logged at warn level
 
- **All providers**: usage records were lost when Revenium answered slowly, because a send that timed out
  was neither retried nor held for resend and did not count towards the circuit breaker; a timed-out send is
  now retried with the same idempotency key, held and resent like one that could not connect, and counts
  towards the breaker. `Close()` used to return nil even when records were still held because Revenium was
  down; it now returns an error and logs a warning with the number of undelivered records. The README has a
  new "Shutdown and Delivery" section showing how to close the client on `SIGTERM` and explaining that
  `os.Exit`, `log.Fatal` and panics outside `main` skip a deferred `Close()`
- **All providers**: when Revenium was slow or throttling, the SDK opened one connection per usage record with
  no limit (enough to exhaust the process's file handles and make the application's own AI calls fail), sent
  each record about four times before backing off, and let one key's 429s stop every other key's records and
  job-outcome calls. Requests to Revenium (sends, resends and `SendSync` calls) are now capped at 32 at once
  per process, whatever the number of hosts; records beyond the queue are held and resent instead of spawning
  more work. Every attempt counts towards the circuit breaker, which opens after three failures in a row and
  lets one record through every 5 seconds, so an outage costs about one request per record. `Retry-After` is
  honoured, each Revenium key has its own breaker (job calls get a separate one per key), held records are
  resent as soon as delivery recovers, up to 16 at a time, and the in-memory buffer now holds up to 20,000
  records, matching the Python SDK. `Flush()` and `Close()` now wait at most about 10 seconds for records
  being sent and 10 more for held ones, and `Close()` counts records still waiting in its undelivered total.
  `jobs.JobClient` has a new `Close()`; a job client keeps its key's breaker, and any `Retry-After` pause,
  across calls until it is closed. `resilience.GetGlobalCircuitBreaker` is deprecated: no SDK call consults it any
  more
- **All providers**: calling `Initialize()` a second time with a different Revenium key was silently
  ignored, so an application serving several customers recorded every customer's usage under the first
  customer's account. A second `Initialize()` with a different Revenium key, base URL or team now returns an
  error naming the setting; repeating the same setup, or calling `Initialize()` again without naming any
  Revenium setting, still returns nil, including when that call races the first one; an environment value
  counts as named only when the first setup also took that setting from the environment. `REVENIUM_TEAM_ID` is now
  trimmed like the key and base URL. The README has a new "Several
  Revenium Accounts in One Process" section showing the per-account standalone constructors

- **Anthropic**: one-hour prompt-cache writes were priced at the five-minute rate (1.25x input instead
  of 2x), understating their cost by 37.5 percent. The five-minute and one-hour split that Claude
  reports is now sent as `cacheCreation5mTokenCount` / `cacheCreation1hTokenCount` on direct,
  streaming and Bedrock calls

- **Anthropic on Bedrock**: Claude calls through Bedrock did not get the request you sent. InvokeModel
  dropped the system prompt, tools, temperature and thinking settings, and Converse dropped images, tools,
  temperature and thinking, so answers ignored the system prompt and never called tools, and a Converse
  record could flag an image that was never sent. The whole request now reaches Bedrock, apart from
  `metadata` and `service_tier`, which only steer Anthropic's own service. A part the API in use has no
  field for (on Converse, server tools such as web search, documents and images by URL) now returns an
  error that wraps `ErrBedrockCannotCarry` and names it instead of being left out, and tool calls and
  redacted thinking in a Converse answer come back as `tool_use` and `redacted_thinking` blocks. Temporary AWS credentials (`aws sso login`, an assumed
  role) were sent without `AWS_SESSION_TOKEN`, so Bedrock rejected every call and the SDK re-sent it to the
  Anthropic API, recorded as Anthropic. The session token is now sent, and a failed Bedrock call returns its
  error instead of moving to the Anthropic API, unless you turn that on with
  `REVENIUM_BEDROCK_FALLBACK_TO_ANTHROPIC=true` or `WithBedrockFallbackToAnthropic(true)`

- **Anthropic on Bedrock**: Bedrock Claude calls were recorded wrong in four ways. A streamed InvokeModel
  call with prompt caching could be recorded with no cache tokens when Bedrock reported them only in
  `message_start` or in its invocation metrics, so cached input was billed at the full input rate. A call
  stopped by a Bedrock guardrail or content filter (`guardrail_intervened`, `content_filtered`, and the
  failed `malformed_model_output` and `malformed_tool_use`) was recorded as finished normally (`END`) and
  now records `ERROR`, as in the Python SDK. One model was recorded under up to three names depending on
  the mode, one of them an ARN carrying your AWS account id; every Bedrock call is now recorded under the
  Bedrock model id it was sent to (`us.anthropic.claude-haiku-4-5-20251001-v1:0`), which is how the price
  catalog names Bedrock models, with an ARN reduced to the model id it names, and cost controls on a model
  now check that same name. A cost-control rule meant to cover Bedrock calls must now name the Bedrock
  model id; a rule naming only the Claude model (`claude-haiku-4-5-20251001`) no longer matches them, and an
  `IS_NOT` rule on that name now does. Converse calls now also send
  the five-minute and one-hour cache write split from Bedrock's `cacheDetails` when the entries account for
  the whole write

- **Anthropic**: Claude calls that use the advisor tool or compaction were billed by Anthropic in full but
  recorded in part. Anthropic reports the advisor model's tokens and each compaction pass's tokens only in
  `usage.iterations`, outside the response's top-level usage, and the SDK read only the top-level usage, so
  the advisor's tokens (often at an Opus rate) and the compaction tokens never reached Revenium. Each advisor
  or compaction pass now sends its own usage record, priced at the model that ran it (the advisor's model,
  or the call's model for compaction), with `operationSubtype` set to `advisor_message` or `compaction`,
  `parentTransactionId` set to the call's transaction id, a transaction id of
  `<call's transaction id>-iteration-<n>` and an idempotency key derived from the call's, and the call's
  trace, subscriber and other fields. The call's own record is unchanged, so a call that uses these
  features now shows more than one record and a higher, correct cost; a call without them still sends one.
  This covers `Messages()` and `BetaMessages()` (including the tool runner), streamed and not, and Bedrock
  InvokeModel; Bedrock Converse does not report these passes. This is the rule the Python SDK follows.
  A streamed call whose turn runs several passes and writes to the prompt cache also no longer sends a
  five-minute/one-hour cache-write split that does not add up to its cache-write total: the split from
  the first pass was kept after the final usage raised the total, so the extra writes were priced at
  whatever the first pass's split said. The record now carries the final usage's own split, or none
  when the final usage gives none. Each pass record carries the call's attribution (agent, agent version,
  squad, trace, job, subscriber, credential alias, skill and prompt context, effort and speed mode) and leaves
  off what prices or describes the call's own response (quality score, error, cost overrides, pricing and
  service tiers, cost multiplier, `billingSkipped`, system prompt). A call recorded with a `speed` mode no
  longer loses its advisor and compaction records

- **Anthropic**: a Claude call whose tool returned an image (a screenshot or chart tool sending the
  picture back in a `tool_result`) was recorded without `hasVision`, on `Messages()`, `BetaMessages()`
  and both Bedrock APIs, so vision pricing and reporting missed it. An image inside a `tool_result` now
  sets `hasVision` the same way a top-level image does

- **Grok**: calls were stored at no cost, and reasoning models would have been priced at a fraction of
  what xAI billed even with a rate. The cost xAI reports for each call (`cost_in_usd_ticks`) is now sent
  as the transaction's total cost, and reasoning tokens, which xAI bills as output but reports outside
  `completion_tokens`, are now included in `outputTokenCount`. The record keeps the model xAI served,
  falling back to the requested model when the response omits it
- **LiteLLM**: Claude calls with prompt caching were priced several times too high. LiteLLM's
  `prompt_tokens` already contains the cache-read and cache-creation tokens, and the SDK sent it as
  `inputTokenCount` beside the same tokens in `cacheReadTokenCount` and `cacheCreationTokenCount`, so
  Revenium billed every cached token at the input rate as well as at its cache rate. For Claude models
  (including Claude on Bedrock and Vertex) `inputTokenCount` is now the uncached input only, matching
  what the Anthropic module reports for the same call, on streaming and non-streaming calls alike.
  Other models keep the full prompt count, because Revenium already removes the cached tokens for
  OpenAI-style providers itself. The Claude check uses the model LiteLLM reports serving, so a
  proxy alias that routes to Claude is covered; streamed records now carry that served model, as
  non-streamed records already did. Rows already stored are not corrected
- **LiteLLM**: the `X-Revenium-Subscriber-Id`, `X-Revenium-Subscriber-Email`,
  `X-Revenium-Subscriber-Credential` and `X-Revenium-Subscriber-Credential-Name` headers returned by the
  proxy were read and then dropped, so those calls were recorded with no subscriber and per-person
  spend and cost controls missed them; they now reach the record's `subscriber` (with the credential
  as its `name` and `value`). The `X-Revenium-Agentic-Job-Id`, `-Name`, `-Type` and `-Version` headers,
  which were ignored, now set the record's agent-job fields
- **Grok, Groq, Ollama**: a request with `Stream` set to true was sent to the provider, which did (and,
  for xAI and Groq, billed) the work, and then the call failed with `failed to parse response` and nothing
  was recorded. These middlewares do not stream, so `ChatCompletions` now returns
  `ErrStreamingNotSupported` before any request is sent
- **All modules**: the README, llms.txt and llms-full.txt said every module works on Go 1.22 or later,
  but `openai` and `perplexity` need Go 1.25 and `anthropic` and `google` need Go 1.24. On an older Go,
  `go get` failed with `requires go >= ...` or quietly downloaded a newer Go and raised the `go` line in
  the caller's `go.mod`. The docs now list the minimum Go of each module, CI fails when that list and a
  module's `go.mod` disagree, and CI tests every module on the Go version its `go.mod` declares
- **All providers**: no change to what the SDK sends. The tests now read the outgoing usage record for
  the token counts (input, output, cache read, cache write, reasoning), the mapped stop reason when a
  provider stops at its token limit, the customer and trace fields set on the context, the JSON names
  Revenium reads, and the key and body of a record held while Revenium was down and sent later, so a
  release that drops or renames any of them fails CI instead of mis-billing calls. The fal tests no
  longer send their usage records to the production Revenium address
- **Job outcomes**: the README, `llms-full.txt` and the job examples built the job client with the metering
  key, which Revenium refuses on every job call, and nothing said a write key was needed; the refusal then
  surfaced as `[VALIDATION_ERROR]`, pointing at the data rather than the key. The docs and examples now use a
  separate write key (`rev_sk_`, read from `REVENIUM_WRITE_API_KEY`) and say the metering key stays in
  `REVENIUM_METERING_API_KEY`. `NewJobClient` rejects a metering key (`rev_mk_`) with a configuration
  error, and a 401 or 403 from a job call is now an authorization error (`core.IsAuthError`) whose message
  says a write key is required
- **Tool metering and job outcomes**: a `metering.MeteringClient` or `jobs.JobClient` built without a
  `BaseURL`, as the README shows, sent tool events and job outcomes to `https://api.revenium.ai` even when
  `REVENIUM_METERING_BASE_URL` pointed the SDK at another Revenium address, so they were rejected there and
  lost while AI usage beside them was recorded. Both clients now use `REVENIUM_METERING_BASE_URL` when no
  `BaseURL` is given; an explicit `BaseURL` still wins, and an invalid address is a configuration error
  naming where it came from. The standalone provider constructors (`NewReveniumOpenAI` and the others)
  follow the same rule, for usage records and for the cost-control rule fetch, so every Revenium call of
  a client goes to one address
- **Job outcomes**: Go could not say why a job failed, attach per-job metric values, add a metric that
  arrives later, or guard an amendment against overwriting someone else's change, while Python could.
  `JobOutcome` and `JobOutcomeAmendment` now carry `OutcomeReason` and `Metrics`, an amendment can carry
  `ExpectedEntityVersion` (a stale one returns `OutcomeAmendConflictError` with the version Revenium holds in
  `CurrentEntityVersion`), and the new `AppendOutcomeMetrics` adds late per-job metrics. `JobResource` now
  reads `OutcomeReason` and `EntityVersion`, and `JobOutcomeAmendmentEntry` reads `OutcomeReason`.
  `JobResource.OutcomeAmendmentCount` and `JobOutcomeAmendmentEntry.AmendmentSequence` were always empty
  because their JSON names did not match what Revenium sends; they now read `outcomeUpdateCount` and
  `sequence`. An outcome refused as already reported now carries Revenium's report time and amendment count;
  a report that timed out is sent once more, and after a timeout, 5xx or lost connection the job is read back and the call succeeds only if Revenium holds exactly
  the outcome sent (including an explicit `ReportedBy`) and the report carried no metrics, which cannot be read back. Amendments and late
  metrics are no longer resent after a 5xx or lost connection, which could record them twice; only a 429
  is retried, through the same retry and circuit breaker as every other call
- **Cost controls, Grok**: a cost control on provider `XAI` did not stop calls made with the `grok`
  package, because that package reported the provider as `xai` and the SDK compared provider names
  letter for letter, while the Revenium platform ignores case. Provider filters (`IS`, `IS_NOT`, lists
  and the text matches) now ignore case, as on the platform, so a rule on `XAI`, `xai` or `xAI` covers
  the same calls; other filters still compare exactly. The `grok` package now records and checks its
  calls as provider `XAI`, the spelling the OpenAI client's xAI path and the Python SDK use
- **LiteLLM**: one-hour prompt-cache writes on Claude calls through LiteLLM were priced at the five-minute
  rate (1.25x input instead of 2x). The five-minute and one-hour split LiteLLM reports under
  `prompt_tokens_details.cache_creation_token_details` is now sent as `cacheCreation5mTokenCount` /
  `cacheCreation1hTokenCount` next to the unchanged total, on plain and streamed calls; a bucket LiteLLM
  does not report is left out rather than sent as 0. Separately, an attribution value made only of spaces,
  in a header map passed to `ExtractMetadataFromHeaders` or in the context metadata, was kept as a blank
  value; it now counts as not sent, so the next source applies (for example `X-Revenium-Organization-Id`
  after a blank `X-Revenium-Organization-Name`, or the proxy's header after a blank context value). Filled
  values are sent as received
- **All providers (security)**: `Initialize()` read `.env.local` and `.env` from the folder above the
  working directory as well as the working directory. A file placed one level up (a shared build
  folder, for example) could point `OPENAI_BASE_URL`, `REVENIUM_METERING_BASE_URL` or the cost-control
  address at another server, which then received the provider key and the Revenium key. Only the
  working directory is read now, a value passed as an option still wins over the file, and
  `REVENIUM_ENV_FILES_DISABLE=true` turns file loading off
- **All providers**: the credential alias (`credentialAlias` in usage metadata, LiteLLM's
  `X-Revenium-Credential-Alias` header, Runway's `CredentialAlias`) was sent as a top-level
  `credentialAlias` field, which Revenium ignores without an error, so spend was never grouped by
  credential. It is now sent as `subscriber.credential.name`, where Revenium stores it. A subscriber `id`
  or `email`, or a credential name or value the caller set, still wins over the alias
- **Tool metering**: a `transactionId` set in the metadata of several tool events was copied onto each
  event as its own id, so Revenium refused every event after the first (409) and rated only the first.
  Each tool event now keeps its own generated `transactionId`, the `Idempotency-Key` header equals it,
  and the caller's value is sent as the event's `traceId` (or `usageMetadata.callerTransactionId` when a
  trace id is also set), so every call is stored while retries still deduplicate. An `idempotencyKey`
  in the metadata of several tool events is no longer applied to them either
- **All providers**: attribution and pricing tags set in usage metadata were silently dropped: agent
  version, squad id, name and role, pricing tier, the free-tier flag (`billingSkipped`), cost multiplier,
  requested and actual service tier, effort, speed, system prompt, source transaction id, and the skill,
  prompt-context and coding-assistant fields. Agents and squads showed as unattributed, batch calls were
  priced at the full rate and free-tier calls showed a cost. They now reach the record under Revenium's
  field names on every record type that accepts them; a value Revenium would refuse is left off with a
  warning so the call is still recorded, and a key the SDK does not record logs one warning naming it.
  A value made only of spaces (for example an organization name of "  ") now counts as not set instead
  of being stored as is. The README metadata table lists every supported key
- **All providers**: documented settings did nothing. `REVENIUM_ENVIRONMENT`, `REVENIUM_REGION`,
  `REVENIUM_TRACE_TYPE`, `REVENIUM_TRACE_NAME`, `REVENIUM_PARENT_TRANSACTION_ID`,
  `REVENIUM_TRANSACTION_NAME`, `REVENIUM_RETRY_NUMBER`, `REVENIUM_CREDENTIAL_ALIAS` and the default
  organization and product (`REVENIUM_ORGANIZATION_NAME`, `REVENIUM_PRODUCT_NAME`, `ReveniumConfig.OrgID`
  and `ProductID` set in code, which win over the environment, and fal's `WithReveniumOrgID` and
  `WithReveniumProductID` options) now set those fields on every record whose
  usage metadata does not; a value set for the call still wins. `REVENIUM_REGION` falls back to the
  cloud's own region variables and `REVENIUM_ENVIRONMENT` to `ENVIRONMENT` and `DEPLOYMENT_ENV`, as in
  the Python SDK. `REVENIUM_API_TIMEOUT` now sets how long one usage-record or tool-event request to Revenium may
  take (and `Close()` waits at least that long); its
  default is 10 seconds, which the docs wrongly gave as 5. `REVENIUM_DEBUG=true` now turns on debug
  logging, including the outgoing payload, in the Anthropic, fal, Grok, Groq, Ollama and Runway modules
  too, and every `REVENIUM_*` on/off setting now accepts `true` in any letter case or `1`. `REVENIUM_FAIL_SILENT` and `AWS_BEDROCK_ENABLED` were never read and are removed from the docs:
  metering errors are always logged and never returned, and Bedrock turns on when AWS credentials are
  set. Cost controls judge a call by the same default organization, product and credential alias its
  record carries
- **All providers**: calls made inside `jobs.RunWithJobContext` (or with a context from
  `jobs.SetJobContext`) were not attached to the job, and `REVENIUM_AGENTIC_JOB_ID`, `_NAME`, `_TYPE`,
  `_VERSION` and `REVENIUM_TICKET_ID`, which attach every call in Python and Node, were ignored, so job
  cost and ROI views and cost by ticket showed nothing from Go. Records now carry the job from the job
  context and the job and ticket from those variables (tool events carry the job id too). Each field is
  resolved on its own: the call's own usage metadata (for fal and Runway, also the call's metadata
  argument) wins over the job context, which wins over the environment, and a blank value never replaces
  a set one. The README now documents how to attach calls to a job or a ticket
- **OpenAI, Anthropic, Google, Perplexity, LiteLLM, Groq, Grok and Ollama**: turning on prompt capture
  (`REVENIUM_CAPTURE_PROMPTS=true`, or `capturePrompts` in a call's usage metadata) sent no prompt or
  response for any chat call, although the README and the `examples/openai/prompt-capture` example said it
  did; only fal and Runway records carried them. Chat records, streamed or not, including Azure, xAI,
  the Responses and legacy Completions APIs, Bedrock, beta messages and tool runners, now carry
  `systemPrompt`, `inputMessages` (the other messages sent, as a JSON array), `outputResponse` and
  `promptsTruncated`, with API keys, tokens and passwords redacted and each field cut to
  `REVENIUM_MAX_PROMPT_SIZE` characters. With capture off, nothing new is sent, and the extra records for
  Claude advisor and compaction passes never carry these fields. Inline images, audio and files in the
  messages are replaced by `[BINARY_DATA]`. A stream the caller stops reading before its end records
  the reply so far, marked truncated. Cut text in any captured field now
  ends in `...[TRUNCATED]`, as in the Python SDK
- **All providers**: captured prompts and replies, including fal and Runway prompts and the `systemPrompt`
  metadata field, redacted only a handful of key shapes, so a Google, xAI, Groq, Hugging Face, Slack,
  fal or Revenium key, a short Bearer token, a Basic credential, a private key, a card number, a signed
  URL or a password under a label such as `"db_password":` was stored as written, where the Python SDK
  redacts it. Redaction now covers every shape the Python SDK redacts, keeping the label or key prefix
  and replacing the secret; captured JSON is masked value by value and stays valid. Go also redacts a
  few shapes the Python SDK does not yet: a value assigned with `:=`, `=>` or `==` after such a label,
  as in pasted Go code, a token or key label followed by only a space, a card number stored as a JSON
  number, and, in a reply cut short, a Basic credential cut at the end of the text
- **Anthropic**: a Messages stream (`CreateMessageStream`, direct or on Bedrock) read only through
  `Next()` was recorded with no usage, and one whose events were read twice through `Current()`
  repeated text in the reconstructed response. Each event is now read once in `Next()`, so usage and
  the captured reply are the same however the stream is read. With prompt capture on, a streamed
  reply's tool calls are captured as JSON, as for non-streamed calls, and a stream that fails partway,
  including beta streams and streaming tool runners, carries no captured prompt or reply. A streamed
  reply cut at `REVENIUM_MAX_PROMPT_SIZE` on any provider now always ends in `...[TRUNCATED]` with
  `promptsTruncated` set
- **Perplexity**: a stream read only through `Next()` was recorded with zero tokens, no stop reason and
  no cost, so that spend was missing from Revenium, and reading a chunk twice through `Current()`
  repeated its text in the captured reply. Each chunk is now recorded once as the stream advances,
  so the record is the same however the stream is read
- **OpenAI, Azure, Google, Perplexity and LiteLLM**: a failed call sent the provider's whole error reply
  to Revenium as the record's error reason, so anyone who can read usage records saw the last characters
  of a revoked OpenAI key, a LiteLLM proxy key and its hash, or the prompt a proxy echoed back, even with
  prompt capture off. The error reason now names the provider's error type or code and the HTTP status,
  such as `invalid_request_error/invalid_api_key (HTTP 401)`, the status is also sent as `errorCode`,
  and the reason is at most 255 characters, the length Revenium stores. The reply text is added, with
  credentials, URL user info, query-string values and email addresses redacted, only when
  `REVENIUM_CAPTURE_ERROR_MESSAGES=true` (and, for text the provider answered with,
  `REVENIUM_CAPTURE_PROMPTS=true`). Warnings about a failed Bedrock, Runway or fal request are redacted
  the same way, and tool events gain `WithFailure(err)`, which sends the kind of error rather than its
  text, while `WithError` now redacts and caps the text it is given. Error text that comes from the
  provider, such as an AWS error with its own error code or an error event sent mid-stream, is treated
  as the provider's reply and needs prompt capture as well
- **Anthropic, Bedrock, fal, Runway, xAI (Grok), Groq, Ollama and LiteLLM**: a call that failed, such
  as a rate limit, a bad request or a server error, sent no usage record from the Anthropic (including
  Bedrock, beta messages and the tool runner), fal, Runway, Grok, Groq and Ollama modules, and a LiteLLM
  stream the proxy rejected at the start or could not reach sent none either, so error rates and
  failed-call alerts were empty for those providers. Every such call now sends one record with stop
  reason `ERROR`, streamed or not. A Runway task that ends `FAILED` is recorded as `ERROR` and one that
  ends `CANCELED` as `CANCELLED`, and a fal queued job that fails is recorded as `ERROR`, all with no
  billed length or image count; a failed or cancelled video record also carries `completionStatus` `FAILED`,
  since Revenium's video records store no stop reason, and a cancelled one has the error reason `cancelled`; a call the caller cancelled has the error reason `caller_cancelled`. A fal failure is recorded under the operation type the endpoint's replies resolved to.
  An Anthropic stream that fails is now recorded as `ERROR` instead of as
  a finished call billed for an estimate of its input, and a LiteLLM stream that sends an error event
  mid-stream is recorded as `ERROR` instead of as a finished call. A Bedrock call re-sent to the
  Anthropic API is recorded by that attempt only, unless the re-send never went out (the model has no
  Anthropic name, or a cost control blocked it), in which case the failed Bedrock call is recorded, and a request the SDK refuses to send to Bedrock is
  not recorded. A Runway task request that got no answer (the caller gave up or the connection was lost)
  or a 5xx answer is not recorded, because Runway may have created the task; a warning says its usage may be missing.
  Calls stopped by a cost control are still not recorded
- **LiteLLM**: Grok calls made through a LiteLLM proxy (`xai/grok-4`, or a bare `grok-*` model) were
  recorded under provider `LiteLLM`, so xAI spend was missing from cost by provider and cost controls
  on provider `XAI` did not apply to them. They are now recorded with provider and model source `XAI`,
  as the grok package and the OpenAI client's xAI path record them, and `XAI` cost controls cover them.
  Other models keep their current provider
- **Core**: on Go 1.21 and 1.22, a request whose `http.Client` timeout expired was classified by
  `resilience.ClassifyError` as a retryable network error, while Go 1.23 and later classify it as not
  retryable, so the retry count and circuit-breaker behaviour of `resilience.WithRetry`,
  `RetryThroughBreaker` and `DefaultFailureStrategy` depended on the Go version the application was built
  with. An `http.Client` timeout is now not retried on every supported Go version; a timeout raised by the
  connection itself, such as a dial timeout, is still retried as before. Usage records and tool events
  keep their behaviour on every version (a request that runs past its deadline is retried, then held
  and resent later), and the cost-control rule fetch does not retry within a fetch; it tries again at the next
  poll. Job reads (`ListJobs`, `GetJob`, `GetJobTypes`, `GetJobROI`, `GetJobTransactions`,
  `GetConversionFunnel`, `GetJobOutcomeHistory`) are retried after an `http.Client` timeout on every Go
  version, as they were on Go 1.21 and 1.22; `ReportJobOutcome` is still resent once after a timeout, and
  `AmendJobOutcome` and `AppendOutcomeMetrics` are still not resent. An `http.Client` timeout no longer
  counts toward opening the jobs circuit breaker on Go 1.21 and 1.22, as on Go 1.23 and later, so while
  Revenium hangs each job call waits out its timeouts instead of failing fast
- **All providers**: with prompt capture on, usage records held in memory while Revenium was unreachable
  were limited only to 20,000 per client, and each could carry three captured fields of up to
  `REVENIUM_MAX_PROMPT_SIZE` characters, so a long outage could hold gigabytes. Held records are now also
  limited to 100 MiB per client (`REVENIUM_BUFFER_MAX_BYTES`, or `BufferMaxBytes` on `core.ReveniumConfig` or `MeteringClientConfig`);
  past either limit the oldest are dropped, with one warning per outage instead of one per resend
  attempt. `GetBufferStats()` reports `InFlight`, `HeldBytes` and `MaxBytes`
- **All providers**: every provider module declared core v1.1.7 as its minimum, so a project that
  upgraded a provider module without also upgrading core could build against a core that lacks code the
  provider calls. Each provider module now requires the core version it is released with, and the release
  checks refuse a provider whose core requirement is behind
- **Documentation**: README.md, llms.txt and llms-full.txt said the SDK never blocks or alters a call
  (cost-control blocks refuse calls, and Claude on Bedrock leaves out `metadata` and `service_tier`), that
  Gemini's `inputTokenCount` is `promptTokenCount` alone (it adds `toolUsePromptTokenCount`), and listed Groq,
  Grok and Ollama among streamed captures; they also left the precedence of context metadata over the fal and
  Runway `metadata` argument unclear and called `hak_` an older metering key while `NewJobClient` accepts it
  as a write key. These now match the code. llms-full.txt gains the Veo calls and metering rules, the Gemini
  token counts, the Runway `UpscaleVideo` `SourceDurationSeconds` requirement and the shutdown guidance
  (`SIGTERM`, `os.Exit`, what `Close()` reports), and the list of calls that are not recorded now names OpenAI
  Assistants, `Beta.Responses`, `Containers` and `VectorStores` and Anthropic managed agents
- **Core**: when usage records could not reach a custom Revenium address (`REVENIUM_METERING_BASE_URL`
  or `BaseURL`) that carried a user name or a query string, the default-level warning "usage records are
  not reaching Revenium", the error lines for dropped records, the error `SendSync` returns and the debug
  line naming the address each record is sent to printed the user name and the query, so a relay token in the address reached the application's logs. Only the
  password was masked. The address in those messages now shows `REDACTED` in place of the user info and
  the query
- **All providers**: a `retryNumber` that is negative, infinite, not a number, or above 2,147,483,647
  (in request metadata, set on the record, or in `REVENIUM_RETRY_NUMBER`) was sent unchanged, and
  Revenium stores it as a 32-bit whole number, so the whole usage record was rejected and its tokens and
  cost were lost. Such a value is now left off the record with a warning, and the record is sent
- **All providers**: the 100 MiB limit on usage records held in memory (`REVENIUM_BUFFER_MAX_BYTES`) did
  not count the up to 1,000 records waiting to be sent, so with prompt capture on and a Revenium that
  answers slowly rather than not at all, memory could reach about two and a half times the limit. Records
  waiting for their first send now count against the same limit, even while Revenium is healthy: when it
  is reached the oldest held records are dropped, and when the records already being sent fill it new
  records are dropped before they are sent, with one warning per outage. A burst of records larger than
  the limit, or a single record larger than it, is therefore dropped rather than sent; raise
  `REVENIUM_BUFFER_MAX_BYTES` if your bursts are bigger than that. `GetBufferStats().HeldBytes` includes
  the waiting records
- **Repository tests**: a contributor whose shell set a local forwarding proxy (`HTTPS_PROXY` on
  127.0.0.1) could run a test that sent a request to the real Revenium API through it and still see the
  suite pass, because the test sandbox trusted any local proxy for the SDK's own HTTP client. The sandbox
  now refuses such a request before a proxy is chosen and fails the package. The examples module now runs
  in CI behind the egress check, and the webhooks module, which makes no network calls and does not
  depend on `core`, has a test that fails if it ever starts to, so the sandbox is added then
- **LiteLLM and Core**: two public helpers got around the prompt capture rules. `litellm.ExtractMetadataFromHeaders`
  still returned `capturePrompts` from a proxy's `X-Revenium-Capture-Prompts` header, so passing its
  result to `core.WithUsageMetadata` let the proxy turn capture on; it no longer returns that field.
  `PayloadBuilder.WithPromptCapture` stored prompts and replies as given, unredacted, uncapped and even
  with capture off; it now stores nothing unless `REVENIUM_CAPTURE_PROMPTS` is on, redacts credentials
  and cuts each field to `REVENIUM_MAX_PROMPT_SIZE`, as the providers do. Plain prompt text passed to it
  is stored as one user message
- **OpenAI and LiteLLM**: Grok calls made through the OpenAI client pointed at `api.x.ai`, or through a
  LiteLLM proxy, dropped the cost xAI reports on every reply, so a Grok model Revenium has no rate for was
  stored at $0. The OpenAI client now forwards xAI's `cost_in_usd_ticks`, and the LiteLLM client forwards
  the proxy's `usage.cost`, as the transaction's total cost, streamed or not, as the `grok` package already
  does. Replies without a reported cost are recorded as before
- **LiteLLM**: a cost control could be passed by calling through a proxy model alias. A call to an alias
  such as `team-fast` that the proxy served with `xai/grok-4` was checked under the alias, which names no
  provider, while its record was filed under provider `XAI`, so a breached `XAI` rule let it through.
  Clients with a `TeamID` now read the proxy's model list (`/model/info`, refreshed every five minutes)
  and judge chat calls by the requested name and every provider model it routes to, streamed or not; if
  the list cannot be read, the last list read is kept and an alias not in it is judged by its own name as
  before
- **Anthropic**: a streamed Claude call you stopped reading before Anthropic sent its stop event was
  recorded as finished normally (`END`), while prompt capture marked its reply truncated, so cancelled
  traffic looked complete. Messages, beta Messages, streaming tool runner turns and Bedrock InvokeModel and
  Converse streams are now recorded with stop reason `CANCELLED` and the tokens reported up to that point;
  Converse reports usage only after its stop event, so a cancelled Converse record carries an estimated input
  count from the prompt text and zero output tokens
- **LiteLLM**: calls through a proxy model alias were recorded under provider `LiteLLM` and the alias name
  with no cost, so spend on an alias such as `team-fast` served by `openai/gpt-4.1-nano` never showed up
  under OpenAI or the model and never counted toward a cap on either. LiteLLM 1.105 answers with the alias
  and reports the price only in its `x-litellm-response-cost` header. Chat calls are now recorded under the
  deployment the proxy served (its `x-litellm-model-name` header, else the deployment its
  `x-litellm-model-id` or `x-litellm-model-group` header names in the model list already read for cost
  controls), streamed or not, and the header's cost is forwarded as the total cost when the answer's
  `usage.cost` is absent; `usage.cost` still wins when both are present

### Changed

- **Anthropic on Bedrock**: a Bedrock call that fails now returns its error instead of being re-sent to
  the Anthropic API. To restore the re-send, set `REVENIUM_BEDROCK_FALLBACK_TO_ANTHROPIC=true` or pass
  `WithBedrockFallbackToAnthropic(true)` to `Initialize`; for a client built with `NewReveniumAnthropic`,
  set `Config.BedrockFallbackToAnthropic` to true
- **Anthropic on Bedrock**: `BedrockAdapter.TransformRequestToBedrockFormat` now returns the whole
  InvokeModel body the SDK sends, decoded into Go values: `messages` and other arrays are
  `[]interface{}`, objects `map[string]interface{}`; integers that fit in int64 are `int64`, larger
  integers stay `json.Number` so they keep their exact value, and other numbers are `float64`
  (`json.Number` when outside the float64 range). It returns nil for a request Bedrock cannot carry
 
- **Anthropic on Bedrock**: Converse calls (`CreateMessage` with Converse on) now return `Model` as the
  Bedrock model id the call was sent to, with an ARN reduced to the id it names; it used to be the full
  ARN, including the AWS account id
- **Anthropic on Bedrock, upgrade note**: cost controls on a model now match Bedrock calls by the Bedrock
  model id the records carry. A `MODEL` rule meant to cover Bedrock calls must name that id, for example
  `anthropic.claude-sonnet-4-6` or `us.anthropic.claude-haiku-4-5-20251001-v1:0`, or use `CONTAINS`; a
  rule naming only the Claude model (`claude-haiku-4-5-20251001`) no longer matches Bedrock calls, and an
  `IS_NOT` rule on that name now does
- **Job outcomes**: a 409 on `ReportJobOutcome` that the SDK cannot attribute to an earlier attempt of the
  same call now always returns `OutcomeAlreadyReportedError`, never a nil job and a nil error as it did when
  the 409 body could not be parsed. After a timeout, 5xx or lost connection in the same call, a 409 whose
  read-back matches the outcome sent returns the stored job and a nil error, as the entry under
  Fixed describes
- **OpenAI, upgrade note**: chat (sync and streamed), legacy text completion and Responses records now carry
  the model OpenAI served (the dated snapshot), as the Python SDK records it, so dashboards and model rules
  see e.g. `gpt-4.1-nano-2025-04-14` instead of `gpt-4.1-nano`; rules written with the short name should use
  the dated form or a prefix operator. A response that names no model, and a failed call, keep the requested
  name; Azure chat and text completions keep the deployment name, and image and audio records are unchanged
 
- **LiteLLM, upgrade note**: Grok calls made through a LiteLLM proxy are now recorded under provider
  `XAI`, so cost controls on provider `LiteLLM` no longer cover them; add a rule on provider `XAI` (or on
  the model) to keep them capped
- **All providers, upgrade note**: a client built with a standalone constructor (`NewReveniumOpenAI` and
  the others) with a `TeamID` and no Revenium `BaseURL` now fetches and enforces its team's cost controls,
  from `REVENIUM_ENFORCEMENT_BASE_URL` when set, and otherwise from `REVENIUM_METERING_BASE_URL` (or
  `https://api.revenium.ai`). It sends its own key, or its `EnforcementAPIKey` when the config sets one; a
  process-wide `REVENIUM_ENFORCEMENT_API_KEY` never replaces a client's own key, so each account in one
  process fetches its own rules. Before, enforcement never started for such
  a client. Its constructor now makes the first rule fetch before returning, which can take up to 10
  seconds, as `Initialize` already does, and calls can be blocked by the team's BLOCK cost controls
 
- **Docs**: the README and `llms-full.txt` now list which calls to Revenium `REVENIUM_API_TIMEOUT` covers
  (usage records and tool events) and the fixed timeouts the others keep: 10 seconds per attempt for job
  calls made with `jobs.JobClient`, and 10 seconds per cost-control rule fetch. Raising the setting does
  not lengthen those

## [1.1.8] - 2026-10-08

### Fixed

- **LiteLLM**: proxied calls were metered with `cacheCreationTokenCount` 0, and cache counts carried on
  usage chunks whose prompt and completion counts were 0 were dropped on the streaming path; both
  Anthropic-native (`cache_creation_input_tokens`, `cache_read_input_tokens`) and OpenAI-shaped
  (`prompt_tokens_details.cached_tokens`) fields are now read
- **Anthropic on Bedrock**: the default `CreateMessage` path (InvokeModel) read only `input_tokens` and
  `output_tokens`, so cache creation and cache read tokens were never metered; `CreateMessageConverse`
  already reported them. The non-streaming InvokeModel path now extracts both. Streaming Bedrock usage
  is still not captured and is tracked separately
- **Anthropic**: extended-thinking output was metered with `reasoningTokenCount` 0 on every call; the
  `output_tokens_details.thinking_tokens` field shipped in `anthropic-sdk-go` v1.76 is now forwarded
  for non-streaming and streaming payloads. Bedrock does not expose thinking tokens, so Bedrock payloads still report 0
- **Google**: `TOO_MANY_TOOL_CALLS` and `LANGUAGE` finish reasons fell through to the default with a
  warning; they now map to `COMPLETION_LIMIT` and `ERROR`

### Changed

- **All providers**: tool-call stop reasons (`tool_use`, `tool_calls`, `function_call`) were metered
  as `END`, which disagreed with the Node and Python SDKs; they now map to `END_SEQUENCE` across
  Anthropic (including Bedrock InvokeModel and Converse), OpenAI (Chat Completions and Responses),
  Perplexity, LiteLLM, Grok, Groq and Ollama.
  **Impact**: saved filters, alerts and dashboards that match `stopReason = END` stop matching
  tool-call turns from this version onwards. The match is exact, so nothing errors — affected reports
  simply return fewer rows and must be widened to `END` or `END_SEQUENCE`. History is not
  backfilled, so any `stopReason` time series shows a step change at the upgrade date
- Provider modules track the current upstream SDKs: `anthropic-sdk-go` v1.76.0, `openai-go/v3`
  v3.66.0, `google.golang.org/genai` v1.71.0. `openai-go/v3` requires Go 1.25, so the `openai` and
  `perplexity` modules now declare `go 1.25`; `anthropic` and `google` move from `go 1.23` to
  `go 1.24` with their upstreams. Consumers on an older toolchain must upgrade before taking
  this release; CI tests on Go 1.25 and 1.26
- `scripts/provider-surface-check` compares the usage structs and stop-reason constants this SDK
  reads against the latest published upstream modules and fails when a consumed field disappears or
  a stop reason is unmapped; a weekly workflow runs it with the test suite against the latest versions

## [1.1.7] - 2026-09-25

### Added

- **Core**: top-level `operationSubtype` on every multimodal metering payload — OpenAI audio and
  images, Google images and video, fal images, audio and video, Runway video. These payloads
  previously carried the subtype only inside `attributes` (fal audio carried none at all), so
  the metering API received none and logged a `*_MISSING_OPERATION_SUBTYPE` warning for each
  call, and audio priced without a resolved direction
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

- **Webhooks**: add HMAC signature verification helper with secret rotation support
- **Bedrock**: capture token counts in streaming wrappers for InvokeModel and Converse

## [1.1.4] - 2026-07-23

### Fixed

- **Bedrock**: fix model-ID double-prefix, streaming wrapper, and add Converse/ConverseStream support

### Added

- **Runway**: add resolution to EditImage and UpscaleImage metering
- **Core**: add ticketId field to metering payload

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
  `ProductID` fields from `MeteringPayload` (#31). They serialized as
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

[1.1.9]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.9
[1.1.8]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.8
[1.1.7]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.7
[1.1.6]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.6
[1.1.4]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.4
[1.1.3]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.3
[1.1.2]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.1.2
[1.0.2]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.0.2
[1.0.1]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.0.1
[1.0.0]: https://github.com/revenium/revenium-go-sdk/releases/tag/v1.0.0
