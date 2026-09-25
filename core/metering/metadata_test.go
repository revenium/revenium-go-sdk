package metering

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyMetadata_StringFields(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, map[string]interface{}{
		"organizationId":      "org-123",
		"productId":           "prod-456",
		"taskType":            "summarize",
		"agent":               "my-agent",
		"subscriptionId":      "sub-789",
		"traceId":             "trace-1",
		"parentTransactionId": "parent-1",
		"traceType":           "chain",
		"traceName":           "my-chain",
		"ticketId":            "FRONT-1544",
		"environment":         "production",
		"region":              "us-east-1",
		"credentialAlias":     "default",
		"taskId":              "task-1",
		"modelSource":         "CUSTOM",
		"transactionId":       "custom-tx",
	})

	assert.Equal(t, "org-123", p.OrganizationName)
	assert.Equal(t, "prod-456", p.ProductName)
	assert.Equal(t, "summarize", p.TaskType)
	assert.Equal(t, "my-agent", p.Agent)
	assert.Equal(t, "sub-789", p.SubscriptionID)
	assert.Equal(t, "trace-1", p.TraceID)
	assert.Equal(t, "parent-1", p.ParentTransactionID)
	assert.Equal(t, "chain", p.TraceType)
	assert.Equal(t, "my-chain", p.TraceName)
	assert.Equal(t, "FRONT-1544", p.TicketID)
	assert.Equal(t, "production", p.Environment)
	assert.Equal(t, "us-east-1", p.Region)
	assert.Equal(t, "default", p.CredentialAlias)
	assert.Equal(t, "task-1", p.TaskID)
	assert.Equal(t, "CUSTOM", p.ModelSource)
	assert.Equal(t, "custom-tx", p.TransactionID)
}

func TestApplyMetadata_FloatFields(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, map[string]interface{}{
		"temperature":          0.7,
		"responseQualityScore": 0.95,
		"inputTokenCost":       0.001,
		"outputTokenCost":      0.002,
		"totalCost":            0.5,
	})

	assert.Equal(t, 0.7, *p.Temperature)
	assert.Equal(t, 0.95, *p.ResponseQualityScore)
	assert.Equal(t, 0.001, *p.InputTokenCost)
	assert.Equal(t, 0.002, *p.OutputTokenCost)
	assert.Equal(t, 0.5, *p.TotalCost)
}

func TestApplyMetadata_IntToFloat(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, map[string]interface{}{
		"totalCost": 5,
	})
	assert.Equal(t, 5.0, *p.TotalCost)
}

func TestApplyMetadata_ErrorReasonOverridesStopReason(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	assert.Equal(t, "END", p.StopReason)

	ApplyMetadata(p, map[string]interface{}{
		"errorReason": "rate_limit_exceeded",
	})

	assert.Equal(t, "ERROR", p.StopReason)
	assert.Equal(t, "rate_limit_exceeded", p.ErrorReason)
}

func TestApplyMetadata_RetryNumber(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, map[string]interface{}{
		"retryNumber": 3,
	})
	assert.Equal(t, 3, *p.RetryNumber)
}

func TestApplyMetadata_Subscriber(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	sub := map[string]interface{}{"id": "user-1", "plan": "pro"}
	ApplyMetadata(p, map[string]interface{}{
		"subscriber": sub,
	})
	assert.Equal(t, sub, p.Subscriber)
}

func TestApplyMetadata_CanonicalOverridesDeprecated(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, map[string]interface{}{
		"organizationId":   "old-org",
		"organizationName": "new-org",
		"productId":        "old-prod",
		"productName":      "new-prod",
	})

	assert.Equal(t, "new-org", p.OrganizationName)
	assert.Equal(t, "new-prod", p.ProductName)
}

func TestApplyMetadata_Nil(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, nil)
	assert.Equal(t, "END", p.StopReason)
}

// The provider middlewares build and send MeteringPayload internally, so
// metadata is the only attribution path available to a caller using
// core.WithUsageMetadata. These fields are worthless if ApplyMetadata drops them.
func TestApplyMetadata_AgenticJobFields(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
		want     MeteringPayload
	}{
		{
			name: "all four applied",
			metadata: map[string]interface{}{
				"agenticJobId":      "job-abc.123_x",
				"agenticJobName":    "nightly summarization",
				"agenticJobType":    "batch-summarize",
				"agenticJobVersion": "v2.1.0",
			},
			want: MeteringPayload{
				AgenticJobID:      "job-abc.123_x",
				AgenticJobName:    "nightly summarization",
				AgenticJobType:    "batch-summarize",
				AgenticJobVersion: "v2.1.0",
			},
		},
		{
			name:     "absent keys leave fields empty",
			metadata: map[string]interface{}{"agent": "my-agent"},
			want:     MeteringPayload{},
		},
		{
			name:     "id only",
			metadata: map[string]interface{}{"agenticJobId": "job-solo"},
			want:     MeteringPayload{AgenticJobID: "job-solo"},
		},
		{
			name: "empty strings are ignored, not written through",
			metadata: map[string]interface{}{
				"agenticJobId":   "",
				"agenticJobType": "",
			},
			want: MeteringPayload{},
		},
		{
			name: "non-string values are ignored",
			metadata: map[string]interface{}{
				"agenticJobId":      42,
				"agenticJobVersion": true,
			},
			want: MeteringPayload{},
		},
		{
			name: "type is applied verbatim - the backend lowercases on ingest",
			metadata: map[string]interface{}{
				"agenticJobType": "Batch-Summarize",
			},
			want: MeteringPayload{AgenticJobType: "Batch-Summarize"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
			ApplyMetadata(p, tt.metadata)

			assert.Equal(t, tt.want.AgenticJobID, p.AgenticJobID)
			assert.Equal(t, tt.want.AgenticJobName, p.AgenticJobName)
			assert.Equal(t, tt.want.AgenticJobType, p.AgenticJobType)
			assert.Equal(t, tt.want.AgenticJobVersion, p.AgenticJobVersion)
		})
	}
}

// End-to-end for the path a middleware user actually exercises: metadata in,
// wire JSON out.
func TestApplyMetadata_AgenticJobReachesWire(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, map[string]interface{}{
		"agenticJobId":      "job-wire",
		"agenticJobName":    "wire check",
		"agenticJobType":    "verify",
		"agenticJobVersion": "v1",
	})

	data, err := json.Marshal(p)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Equal(t, "job-wire", m["agenticJobId"])
	assert.Equal(t, "wire check", m["agenticJobName"])
	assert.Equal(t, "verify", m["agenticJobType"])
	assert.Equal(t, "v1", m["agenticJobVersion"])
}

// The platform validates agenticJobId on completions, so an id it would reject
// has to be dropped here: sending it turns the whole completion into a 400 and
// the cost is lost, not just the attribution.
func TestApplyMetadata_AgenticJobIDValidation(t *testing.T) {
	for _, tt := range agenticJobIDCases {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
			ApplyMetadata(p, map[string]interface{}{
				"agenticJobId":   tt.id,
				"agenticJobName": "nightly summarization",
				"agent":          "my-agent",
			})

			if tt.valid {
				assert.Equal(t, tt.id, p.AgenticJobID)
			} else {
				assert.Empty(t, p.AgenticJobID)
			}

			// Dropping the id must not cost the rest of the metadata.
			assert.Equal(t, "nightly summarization", p.AgenticJobName)
			assert.Equal(t, "my-agent", p.Agent)
		})
	}
}

// Only agenticJobId has a server-side pattern. Name, type and version are plain
// DTO fields and must reach the wire untouched, whatever they look like.
func TestApplyMetadata_AgenticJobNameTypeVersionAreNotValidated(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, map[string]interface{}{
		"agenticJobName":    "nightly / summarization run",
		"agenticJobType":    "-batch summarize",
		"agenticJobVersion": "types",
	})

	assert.Equal(t, "nightly / summarization run", p.AgenticJobName)
	assert.Equal(t, "-batch summarize", p.AgenticJobType)
	assert.Equal(t, "types", p.AgenticJobVersion)
}

func TestApplyMetadata_OperationSubtype(t *testing.T) {
	tests := []struct {
		name     string
		op       OperationType
		detected string
		metadata map[string]interface{}
		want     string
	}{
		{
			name:     "valid override replaces the detected subtype",
			op:       OperationImage,
			detected: SubtypeGeneration,
			metadata: map[string]interface{}{"operationSubtype": SubtypeInpainting},
			want:     "inpainting",
		},
		{
			name:     "override is trimmed and lowercased",
			op:       OperationAudio,
			detected: SubtypeTranscription,
			metadata: map[string]interface{}{"operationSubtype": " TTS "},
			want:     "tts",
		},
		{
			name:     "unsupported override keeps the detected subtype",
			op:       OperationAudio,
			detected: SubtypeTTS,
			metadata: map[string]interface{}{"operationSubtype": "speech_synthesis"},
			want:     "tts",
		},
		{
			name:     "speech override is accepted on audio",
			op:       OperationAudio,
			detected: SubtypeTTS,
			metadata: map[string]interface{}{"operationSubtype": SubtypeSpeech},
			want:     "speech",
		},
		{
			name:     "realtime override is accepted on audio",
			op:       OperationAudio,
			detected: SubtypeTranscription,
			metadata: map[string]interface{}{"operationSubtype": SubtypeRealtime},
			want:     "realtime",
		},
		{
			name:     "empty override is ignored",
			op:       OperationVideo,
			detected: SubtypeGeneration,
			metadata: map[string]interface{}{"operationSubtype": ""},
			want:     "generation",
		},
		{
			name:     "whitespace-only override is treated as absent",
			op:       OperationVideo,
			detected: SubtypeGeneration,
			metadata: map[string]interface{}{"operationSubtype": "   "},
			want:     "generation",
		},
		{
			name:     "non-string override is ignored",
			op:       OperationVideo,
			detected: SubtypeUpscale,
			metadata: map[string]interface{}{"operationSubtype": 42},
			want:     "upscale",
		},
		{
			name:     "absent key leaves the detected subtype",
			op:       OperationImage,
			detected: SubtypeEdit,
			metadata: map[string]interface{}{"traceId": "t-1"},
			want:     "edit",
		},
		{
			name:     "chat payload ignores the key",
			op:       OperationChat,
			detected: "",
			metadata: map[string]interface{}{"operationSubtype": SubtypeGeneration},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPayload(tt.op, "m", "PROVIDER").WithOperationSubtype(tt.detected).Build()
			ApplyMetadata(p, tt.metadata)
			assert.Equal(t, tt.want, p.OperationSubtype)
		})
	}
}
