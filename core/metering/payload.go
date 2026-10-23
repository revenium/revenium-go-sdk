package metering

import (
	"encoding/json"
	"time"
)

const MiddlewareSource = "revenium-go-sdk"

type MeteringPayload struct {
	IdempotencyKey          string `json:"-"`
	StopReason              string `json:"stopReason"`
	CostType                string `json:"costType"`
	IsStreamed              bool   `json:"isStreamed"`
	OperationType           string `json:"operationType"`
	OperationSubtype        string `json:"operationSubtype,omitempty"`
	InputTokenCount         int64  `json:"inputTokenCount"`
	OutputTokenCount        int64  `json:"outputTokenCount"`
	ReasoningTokenCount     int64  `json:"reasoningTokenCount"`
	CacheCreationTokenCount int64  `json:"cacheCreationTokenCount"`
	CacheReadTokenCount     int64  `json:"cacheReadTokenCount"`
	TotalTokenCount         int64  `json:"totalTokenCount"`
	Model                   string `json:"model"`
	TransactionID           string `json:"transactionId"`
	ResponseTime            string `json:"responseTime"`
	RequestDuration         int64  `json:"requestDuration"`
	Provider                string `json:"provider"`
	RequestTime             string `json:"requestTime"`
	CompletionStartTime     string `json:"completionStartTime"`
	TimeToFirstToken        int64  `json:"timeToFirstToken"`
	MiddlewareSource        string `json:"middlewareSource"`
	ModelSource             string `json:"modelSource,omitempty"`

	CacheCreation5mTokenCount *int64 `json:"cacheCreation5mTokenCount,omitempty"`
	CacheCreation1hTokenCount *int64 `json:"cacheCreation1hTokenCount,omitempty"`

	SystemFingerprint string   `json:"systemFingerprint,omitempty"`
	Temperature       *float64 `json:"temperature,omitempty"`
	ErrorReason       string   `json:"errorReason,omitempty"`
	ErrorCode         *int     `json:"errorCode,omitempty"`
	// CompletionStatus is the outcome Revenium's video endpoint stores; that
	// endpoint keeps no stop reason.
	CompletionStatus string `json:"completionStatus,omitempty"`

	ActualImageCount         *int                   `json:"actualImageCount,omitempty"`
	RequestedImageCount      *int                   `json:"requestedImageCount,omitempty"`
	DurationSeconds          *float64               `json:"durationSeconds,omitempty"`
	RequestedDurationSeconds *float64               `json:"requestedDurationSeconds,omitempty"`
	BillingUnit              BillingUnit            `json:"billingUnit,omitempty"`
	Resolution               string                 `json:"resolution,omitempty"`
	Quality                  string                 `json:"quality,omitempty"`
	Style                    string                 `json:"style,omitempty"`
	Format                   string                 `json:"format,omitempty"`
	CharacterCount           *int64                 `json:"characterCount,omitempty"`
	InputAudioTokenCount     *int64                 `json:"inputAudioTokenCount,omitempty"`
	Language                 string                 `json:"language,omitempty"`
	TargetLanguage           string                 `json:"targetLanguage,omitempty"`
	ResponseFormat           string                 `json:"responseFormat,omitempty"`
	Voice                    string                 `json:"voice,omitempty"`
	Speed                    *float64               `json:"speed,omitempty"`
	Attributes               map[string]interface{} `json:"attributes,omitempty"`

	InputMessages    string `json:"inputMessages,omitempty"`
	OutputResponse   string `json:"outputResponse,omitempty"`
	PromptsTruncated bool   `json:"promptsTruncated,omitempty"`

	OrganizationName     string                 `json:"organizationName,omitempty"`
	ProductName          string                 `json:"productName,omitempty"`
	TaskType             string                 `json:"taskType,omitempty"`
	Agent                string                 `json:"agent,omitempty"`
	SubscriptionID       string                 `json:"subscriptionId,omitempty"`
	TraceID              string                 `json:"traceId,omitempty"`
	ParentTransactionID  string                 `json:"parentTransactionId,omitempty"`
	TraceType            string                 `json:"traceType,omitempty"`
	TraceName            string                 `json:"traceName,omitempty"`
	TransactionName      string                 `json:"transactionName,omitempty"`
	TicketID             string                 `json:"ticketId,omitempty"`
	AgenticJobID         string                 `json:"agenticJobId,omitempty"` // Direct assignment is unchecked; see sanitizeAgenticJobID.
	AgenticJobName       string                 `json:"agenticJobName,omitempty"`
	AgenticJobType       string                 `json:"agenticJobType,omitempty"`
	AgenticJobVersion    string                 `json:"agenticJobVersion,omitempty"`
	Environment          string                 `json:"environment,omitempty"`
	Region               string                 `json:"region,omitempty"`
	RetryNumber          *int                   `json:"retryNumber,omitempty"`
	CredentialAlias      string                 `json:"-"`
	Subscriber           map[string]interface{} `json:"subscriber,omitempty"`
	TaskID               string                 `json:"taskId,omitempty"`
	VideoJobID           string                 `json:"videoJobId,omitempty"`
	AudioJobID           string                 `json:"audioJobId,omitempty"`
	ResponseQualityScore *float64               `json:"responseQualityScore,omitempty"`
	MediationLatency     *float64               `json:"mediationLatency,omitempty"`

	AgentVersion               string   `json:"agentVersion,omitempty"`
	SquadID                    string   `json:"squadId,omitempty"`
	SquadName                  string   `json:"squadName,omitempty"`
	SquadRole                  string   `json:"squadRole,omitempty"`
	SystemPrompt               string   `json:"systemPrompt,omitempty"`
	BillingSkipped             *bool    `json:"billingSkipped,omitempty"`
	PricingTier                string   `json:"pricingTier,omitempty"`
	RequestedServiceTier       string   `json:"requestedServiceTier,omitempty"`
	ActualServiceTier          string   `json:"actualServiceTier,omitempty"`
	PriorityTier               string   `json:"priorityTier,omitempty"`
	SubscriptionTier           string   `json:"subscriptionTier,omitempty"`
	CostMultiplier             *float64 `json:"costMultiplier,omitempty"`
	CodingAssistantAccountUUID string   `json:"codingAssistantAccountUuid,omitempty"`
	Effort                     string   `json:"effort,omitempty"`
	SpeedMode                  string   `json:"-"`
	QuerySource                string   `json:"querySource,omitempty"`
	SubagentType               string   `json:"subagentType,omitempty"`
	PromptID                   string   `json:"promptId,omitempty"`
	PromptLength               *int64   `json:"promptLength,omitempty"`
	ClientSurface              string   `json:"clientSurface,omitempty"`
	SkillName                  string   `json:"skillName,omitempty"`
	SkillInvocationTrigger     string   `json:"skillInvocationTrigger,omitempty"`
	SkillSource                string   `json:"skillSource,omitempty"`
	SkillKind                  string   `json:"skillKind,omitempty"`
	SkillPluginName            string   `json:"skillPluginName,omitempty"`
	SkillMarketplaceName       string   `json:"skillMarketplaceName,omitempty"`
	SourceTransactionID        string   `json:"sourceTransactionId,omitempty"`

	InputTokenCost         *float64 `json:"inputTokenCost,omitempty"`
	OutputTokenCost        *float64 `json:"outputTokenCost,omitempty"`
	CacheCreationTokenCost *float64 `json:"cacheCreationTokenCost,omitempty"`
	CacheReadTokenCost     *float64 `json:"cacheReadTokenCost,omitempty"`
	TotalCost              *float64 `json:"totalCost,omitempty"`

	FailureCode string `json:"failureCode,omitempty"`
}

// MarshalJSON sends SpeedMode, the speed mode of a completion ("fast" or
// "normal"), under the record's "speed" key, which audio records use for the
// numeric text-to-speech rate held in Speed.
func (p MeteringPayload) MarshalJSON() ([]byte, error) {
	type wireRecord MeteringPayload
	if p.SpeedMode == "" || p.Speed != nil {
		return json.Marshal(wireRecord(p))
	}
	return json.Marshal(struct {
		wireRecord
		Speed string `json:"speed"`
	}{wireRecord(p), p.SpeedMode})
}

type PayloadBuilder struct {
	payload *MeteringPayload
}

func NewPayload(op OperationType, model, provider string) *PayloadBuilder {
	return &PayloadBuilder{
		payload: &MeteringPayload{
			IdempotencyKey:   GenerateTransactionID(),
			StopReason:       "END",
			CostType:         "AI",
			OperationType:    string(op),
			Model:            model,
			Provider:         provider,
			TransactionID:    GenerateTransactionID(),
			MiddlewareSource: MiddlewareSource,
		},
	}
}

func (b *PayloadBuilder) WithTiming(requestTime time.Time, duration time.Duration) *PayloadBuilder {
	b.payload.RequestTime = requestTime.UTC().Format(time.RFC3339)
	b.payload.ResponseTime = requestTime.Add(duration).UTC().Format(time.RFC3339)
	b.payload.RequestDuration = duration.Milliseconds()
	b.payload.CompletionStartTime = requestTime.UTC().Format(time.RFC3339)
	return b
}

func (b *PayloadBuilder) WithTokens(input, output, total int64) *PayloadBuilder {
	b.payload.InputTokenCount = input
	b.payload.OutputTokenCount = output
	b.payload.TotalTokenCount = total
	return b
}

func (b *PayloadBuilder) WithReasoningTokens(reasoning, cacheCreation, cacheRead int64) *PayloadBuilder {
	b.payload.ReasoningTokenCount = reasoning
	b.payload.CacheCreationTokenCount = cacheCreation
	b.payload.CacheReadTokenCount = cacheRead
	return b
}

func (b *PayloadBuilder) WithCacheCreationByTTL(fiveMinute, oneHour *int64) *PayloadBuilder {
	b.payload.CacheCreation5mTokenCount = fiveMinute
	b.payload.CacheCreation1hTokenCount = oneHour
	return b
}

func (b *PayloadBuilder) WithStreaming(isStreamed bool, timeToFirstToken int64, completionStartTime *time.Time) *PayloadBuilder {
	b.payload.IsStreamed = isStreamed
	b.payload.TimeToFirstToken = timeToFirstToken
	if completionStartTime != nil {
		b.payload.CompletionStartTime = completionStartTime.UTC().Format(time.RFC3339)
	}
	return b
}

func (b *PayloadBuilder) WithStopReason(reason string) *PayloadBuilder {
	b.payload.StopReason = reason
	return b
}

func (b *PayloadBuilder) WithModelSource(source string) *PayloadBuilder {
	b.payload.ModelSource = source
	return b
}

func (b *PayloadBuilder) WithSystemFingerprint(fp string) *PayloadBuilder {
	if fp != "" {
		b.payload.SystemFingerprint = fp
	}
	return b
}

func (b *PayloadBuilder) WithError(errorReason string) *PayloadBuilder {
	b.payload.ErrorReason = errorReason
	b.payload.StopReason = "ERROR"
	return b
}

func (b *PayloadBuilder) WithTemperature(temp float64) *PayloadBuilder {
	b.payload.Temperature = &temp
	return b
}

func (b *PayloadBuilder) WithImageBilling(actual, requested int) *PayloadBuilder {
	b.payload.ActualImageCount = &actual
	b.payload.RequestedImageCount = &requested
	return b
}

func (b *PayloadBuilder) WithVideoDuration(actual, requested float64) *PayloadBuilder {
	b.payload.DurationSeconds = &actual
	b.payload.RequestedDurationSeconds = &requested
	return b
}

func (b *PayloadBuilder) WithAudioDuration(duration float64) *PayloadBuilder {
	b.payload.DurationSeconds = &duration
	return b
}

func (b *PayloadBuilder) WithOperationSubtype(subtype string) *PayloadBuilder {
	applyOperationSubtype(b.payload, subtype)
	return b
}

func (b *PayloadBuilder) WithAttributes(attrs map[string]interface{}) *PayloadBuilder {
	b.payload.Attributes = attrs
	return b
}

// WithPromptCapture stores a prompt and reply under the rules the providers follow: nothing is
// stored unless REVENIUM_CAPTURE_PROMPTS is on, credentials are redacted, and each field is cut to
// REVENIUM_MAX_PROMPT_SIZE. input is a JSON list of chat messages or plain prompt text; truncated
// says the caller already cut what it passes.
func (b *PayloadBuilder) WithPromptCapture(input, output string, truncated bool) *PayloadBuilder {
	applyBuilderCapture(b.payload, input, output, truncated)
	return b
}

func (b *PayloadBuilder) WithTransactionID(id string) *PayloadBuilder {
	if id != "" {
		b.payload.TransactionID = id
	}
	return b
}

func (b *PayloadBuilder) WithIdempotencyKey(key string) *PayloadBuilder {
	if key != "" {
		b.payload.IdempotencyKey = key
	}
	return b
}

func (b *PayloadBuilder) WithFailureCode(code string) *PayloadBuilder {
	if code != "" {
		b.payload.FailureCode = code
	}
	return b
}

func (b *PayloadBuilder) WithCosts(inputCost, outputCost, cacheCreationCost, cacheReadCost, totalCost *float64) *PayloadBuilder {
	b.payload.InputTokenCost = inputCost
	b.payload.OutputTokenCost = outputCost
	b.payload.CacheCreationTokenCost = cacheCreationCost
	b.payload.CacheReadTokenCost = cacheReadCost
	b.payload.TotalCost = totalCost
	return b
}

func (b *PayloadBuilder) WithResponseQualityScore(score *float64) *PayloadBuilder {
	b.payload.ResponseQualityScore = score
	return b
}

func (b *PayloadBuilder) Build() *MeteringPayload {
	return b.payload
}
