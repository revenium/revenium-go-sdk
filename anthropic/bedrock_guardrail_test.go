package anthropic

import (
	"context"
	"fmt"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func invokeModelAnswerWithGuardrailAction(action string) string {
	return fmt.Sprintf(`{"id":"msg_bdrk_1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001",`+
		`"content":[{"type":"text","text":"Sorry, the model cannot answer this question."}],"stop_reason":"end_turn",`+
		`"usage":{"input_tokens":12,"output_tokens":3},"amazon-bedrock-guardrailAction":%q}`, action)
}

func invokeStreamStopWithGuardrailAction(action string) string {
	return fmt.Sprintf(`{"type":"message_stop","amazon-bedrock-invocationMetrics":{"inputTokenCount":1,"outputTokenCount":162,`+
		`"invocationLatency":2112,"firstByteLatency":412},"amazon-bedrock-guardrailAction":%q}`, action)
}

func TestBedrockInvokeModelGuardrailBlockIsRecordedAsError(t *testing.T) {
	cases := map[string]struct {
		action         string
		wantStopReason anthropicsdk.StopReason
		wantRecorded   string
	}{
		"guardrail intervened":     {action: "INTERVENED", wantStopReason: "guardrail_intervened", wantRecorded: "ERROR"},
		"guardrail let it through": {action: "NONE", wantStopReason: anthropicsdk.StopReasonEndTurn, wantRecorded: "END"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newBedrockStub(t)
			stub.respondWith(invokeModelAnswerWithGuardrailAction(tc.action))
			mock := newMeteringMock(t)
			r := newBedrockAnthropic(t, bedrockConfig(mock.URL()))

			resp, err := r.Messages().CreateMessage(context.Background(), messageParams("claude-haiku-4-5-20251001"))
			require.NoError(t, err)

			assert.Equal(t, tc.wantStopReason, resp.StopReason)
			assert.Equal(t, tc.wantRecorded, onlyRecord(t, r, mock)["stopReason"])
		})
	}
}

func TestBedrockInvokeModelStreamGuardrailBlockIsRecordedAsError(t *testing.T) {
	cases := map[string]struct {
		action       string
		wantRecorded string
	}{
		"guardrail intervened":     {action: "INTERVENED", wantRecorded: "ERROR"},
		"guardrail let it through": {action: "NONE", wantRecorded: "END"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newBedrockStub(t)
			stub.streamWith(invokeStreamChunks(
				invokeStreamMessageStartWithoutCache, invokeStreamBlockStart, invokeStreamBlockDelta, invokeStreamBlockStop,
				invokeStreamOutputOnly, invokeStreamStopWithGuardrailAction(tc.action),
			))
			mock := newMeteringMock(t)
			r := newBedrockAnthropic(t, bedrockConfig(mock.URL()))

			stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams(streamedModelID))
			require.NoError(t, err)
			drainStream(t, stream)

			assert.Equal(t, tc.wantRecorded, onlyRecord(t, r, mock)["stopReason"])
		})
	}
}
