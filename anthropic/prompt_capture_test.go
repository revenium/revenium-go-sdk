package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

var promptFields = []string{"systemPrompt", "inputMessages", "outputResponse", "promptsTruncated"}

func assertNoCapturedPrompt(t *testing.T, record map[string]interface{}) {
	t.Helper()
	for _, field := range promptFields {
		assert.NotContains(t, record, field)
	}
}

func capturedInput(t *testing.T, record map[string]interface{}) []map[string]interface{} {
	t.Helper()
	text, ok := record["inputMessages"].(string)
	require.True(t, ok, "inputMessages missing from %v", record)
	var messages []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(text), &messages))
	return messages
}

func capturedMessageParams() anthropicsdk.MessageNewParams {
	params := messageParams("claude-haiku-4-5")
	params.System = []anthropicsdk.TextBlockParam{{Text: "Be brief."}, {Text: "Never share sk-ant-abcdefghij1234567890extra"}}
	return params
}

var capturingTurnSurfaces = []turnSurface{
	{"messages", false, func(t *testing.T, r *ReveniumAnthropic, ctx context.Context) {
		_, err := r.Messages().CreateMessage(ctx, capturedMessageParams())
		require.NoError(t, err)
	}},
	{"messages stream", true, func(t *testing.T, r *ReveniumAnthropic, ctx context.Context) {
		stream, err := r.Messages().CreateMessageStream(ctx, capturedMessageParams())
		require.NoError(t, err)
		drainStream(t, stream)
	}},
}

func TestPromptCapture_EveryMessagesSurfaceRecordsPromptAndReplyWhenOn(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	for _, surface := range append(capturingTurnSurfaces, turnSurfaces[2:]...) {
		t.Run(surface.name, func(t *testing.T) {
			serveTurn(t, "")
			r, mock := newMeteredAnthropic(t)

			surface.call(t, r, context.Background())

			record := flushedRecords(t, r, mock, 1)[0]
			messages := capturedInput(t, record)
			require.Len(t, messages, 1)
			assert.Equal(t, "user", messages[0]["role"])
			assert.Equal(t, "done", record["outputResponse"])
		})
	}
}

func TestPromptCapture_MessagesJoinTheSystemBlocksAndRedactCredentials(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	for _, surface := range capturingTurnSurfaces {
		t.Run(surface.name, func(t *testing.T) {
			serveTurn(t, "")
			r, mock := newMeteredAnthropic(t)

			surface.call(t, r, context.Background())

			system := flushedRecords(t, r, mock, 1)[0]["systemPrompt"]
			assert.Contains(t, system, "Be brief.\nNever share sk-ant-")
			assert.NotContains(t, system, "abcdefghij1234567890extra")
		})
	}
}

func TestPromptCapture_EveryMessagesSurfaceRecordsNoPromptWhenOff(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")
	for _, surface := range append(capturingTurnSurfaces, turnSurfaces[2:]...) {
		t.Run(surface.name, func(t *testing.T) {
			serveTurn(t, "")
			r, mock := newMeteredAnthropic(t)

			surface.call(t, r, context.Background())

			assertNoCapturedPrompt(t, flushedRecords(t, r, mock, 1)[0])
		})
	}
}

func TestPromptCapture_PerCallSettingOverridesTheEnvironment(t *testing.T) {
	for _, surface := range capturingTurnSurfaces {
		t.Run(surface.name, func(t *testing.T) {
			serveTurn(t, "")
			r, mock := newMeteredAnthropic(t)

			t.Setenv(core.EnvCapturePrompts, "true")
			surface.call(t, r, core.WithUsageMetadata(context.Background(), map[string]interface{}{"capturePrompts": false}))
			assertNoCapturedPrompt(t, flushedRecords(t, r, mock, 1)[0])

			mock.Reset()
			t.Setenv(core.EnvCapturePrompts, "false")
			surface.call(t, r, core.WithUsageMetadata(context.Background(), map[string]interface{}{"capturePrompts": true}))
			assert.Equal(t, "done", flushedRecords(t, r, mock, 1)[0]["outputResponse"])
		})
	}
}

func TestPromptCapture_PassRecordsCarryNoCapturedPrompt(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	for _, surface := range turnSurfaces {
		t.Run(surface.name, func(t *testing.T) {
			serveTurn(t, advisedIterations)
			r, mock := newMeteredAnthropic(t)

			surface.call(t, r, context.Background())

			records := recordsBySubtype(flushedRecords(t, r, mock, 3))
			assert.Equal(t, "done", records[""]["outputResponse"])
			assertNoCapturedPrompt(t, records[advisorPass])
			assertNoCapturedPrompt(t, records[compactionPass])
		})
	}
}

func TestPromptCapture_ToolRunnerRecordsEachTurnsConversationAndToolCall(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	newBetaProviderStub(t, betaToolUseMessage, betaFinalMessage)
	r, mock := newMeteredAnthropic(t)

	runner := r.BetaMessages().NewToolRunner([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams())
	_, err := runner.RunToCompletion(context.Background())
	require.NoError(t, err)

	records := recordsAfterFlush(r, mock)
	require.Len(t, records, 2)
	assert.Len(t, capturedInput(t, records[0]), 1)
	assert.JSONEq(t, `{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}`, records[0]["outputResponse"].(string))
	assert.Len(t, capturedInput(t, records[1]), 3)
	assert.Equal(t, "done", records[1]["outputResponse"])
}

func TestPromptCapture_StreamingToolRunnerRecordsEachTurnsConversationAndToolCall(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	newBetaProviderStub(t, betaToolUseMessage, betaFinalMessage)
	r, mock := newMeteredAnthropic(t)

	runner := r.BetaMessages().NewToolRunnerStreaming([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams())
	for events, err := range runner.AllStreaming(context.Background()) {
		require.NoError(t, err)
		for _, eventErr := range events {
			require.NoError(t, eventErr)
		}
	}

	records := recordsAfterFlush(r, mock)
	require.Len(t, records, 2)
	assert.Len(t, capturedInput(t, records[0]), 1)
	assert.JSONEq(t, `{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}`, records[0]["outputResponse"].(string))
	assert.Len(t, capturedInput(t, records[1]), 3)
	assert.Equal(t, "done", records[1]["outputResponse"])
}

func TestPromptCapture_MessageReplyKeepsTextAndToolUse(t *testing.T) {
	var message anthropicsdk.Message
	require.NoError(t, json.Unmarshal([]byte(`{"content":[{"type":"text","text":"Let me check."},`+
		`{"type":"tool_use","id":"toolu_9","name":"get_weather","input":{"city":"Paris"}}]}`), &message))

	reply := messageReply(message.Content)

	assert.Equal(t, `Let me check.`+"\n"+`{"id":"toolu_9","input":{"city":"Paris"},"name":"get_weather","type":"tool_use"}`, reply)
}

func TestPromptCapture_MessageReplyMasksToolInputsAfterText(t *testing.T) {
	var message anthropicsdk.Message
	require.NoError(t, json.Unmarshal([]byte(`{"content":[{"type":"text","text":"Logging you in."},`+
		`{"type":"tool_use","id":"toolu_9","name":"login","input":{"user":"ana","password":"hunter2hunter2"}}]}`), &message))

	reply := messageReply(message.Content)

	assert.NotContains(t, reply, "hunter2hunter2")
	assert.Contains(t, reply, `"user":"ana"`)
}

func TestPromptCapture_BedrockRecordsPromptAndReplyOnEveryPath(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	for _, useConverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("bedrock converse=%v", useConverse), func(t *testing.T) {
			stub := newBedrockStub(t)
			stub.respondWith(invokeModelAnswer)
			stub.streamWith(invokeStreamChunks(invokeStreamMessageStartWithoutCache, invokeStreamBlockStart, invokeStreamBlockDelta, invokeStreamBlockStop, invokeStreamOutputOnly, invokeStreamStopWithoutCache))
			plainReply, streamedReply := "Bonjour", "Hello"
			if useConverse {
				stub.respondWith(converseAnswer("end_turn", `{"inputTokens":10,"outputTokens":6,"totalTokens":16}`))
				stub.streamWith(converseStopEvents("end_turn"))
				plainReply, streamedReply = "I can't help with that.", "I can't help with that."
			}
			mock := newMeteringMock(t)
			cfg := bedrockConfig(mock.URL())
			cfg.BedrockUseConverse = useConverse
			r := newBedrockAnthropic(t, cfg)

			_, err := r.Messages().CreateMessage(context.Background(), capturedMessageParams())
			require.NoError(t, err)
			plain := onlyRecord(t, r, mock)

			mock.Reset()
			stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
			require.NoError(t, err)
			drainStream(t, stream)
			streamed := onlyRecord(t, r, mock)

			for _, record := range []map[string]interface{}{plain, streamed} {
				assert.Contains(t, record["systemPrompt"], "Be brief.")
				assert.Len(t, capturedInput(t, record), 1)
			}
			assert.Equal(t, plainReply, plain["outputResponse"])
			assert.Equal(t, streamedReply, streamed["outputResponse"])
		})
	}
}

func TestPromptCapture_BedrockRecordsNoPromptWhenOff(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")
	stub := newBedrockStub(t)
	stub.respondWith(invokeModelAnswer)
	stub.streamWith(invokeStreamChunks(invokeStreamMessageStartWithoutCache, invokeStreamBlockDelta, invokeStreamOutputOnly, invokeStreamStopWithoutCache))
	mock := newMeteringMock(t)
	r := newBedrockAnthropic(t, bedrockConfig(mock.URL()))

	_, err := r.Messages().CreateMessage(context.Background(), capturedMessageParams())
	require.NoError(t, err)
	assertNoCapturedPrompt(t, onlyRecord(t, r, mock))

	mock.Reset()
	stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
	require.NoError(t, err)
	drainStream(t, stream)
	assertNoCapturedPrompt(t, onlyRecord(t, r, mock))
}

var toolUseStreamEvents = []string{
	`{"type":"message_start","message":{"id":"msg_tool","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":12,"output_tokens":1}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Checking."}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_7","name":"get_weather","input":{}}}`,
	`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}`,
	`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"Paris\"}"}}`,
	`{"type":"content_block_stop","index":1}`,
	`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":9}}`,
	`{"type":"message_stop"}`,
}

const streamedToolReply = "Checking.\n" + `{"id":"toolu_7","input":{"city":"Paris"},"name":"get_weather","type":"tool_use"}`

func serveSSE(t *testing.T, events []string, trailer string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			var typed struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal([]byte(event), &typed)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typed.Type, event)
		}
		fmt.Fprint(w, trailer)
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
}

func TestPromptCapture_StreamedMessagesRecordToolUseWhateverTheCallerReads(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	reads := map[string]func(w *StreamingWrapper){
		"next only":     func(w *StreamingWrapper) {},
		"current once":  func(w *StreamingWrapper) { w.Current() },
		"current twice": func(w *StreamingWrapper) { w.Current(); w.Current() },
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			serveSSE(t, toolUseStreamEvents, "")
			r, mock := newMeteredAnthropic(t)

			stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
			require.NoError(t, err)
			wrapper := stream.(*StreamingWrapper)
			for wrapper.Next() {
				read(wrapper)
			}
			require.NoError(t, wrapper.Close())

			record := flushedRecords(t, r, mock, 1)[0]
			assert.Equal(t, streamedToolReply, record["outputResponse"])
			assert.Equal(t, float64(9), record["outputTokenCount"])
		})
	}
}

func TestPromptCapture_FailedStreamsCarryNoCapturedPrompt(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	failure := "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"
	surfaces := map[string]func(t *testing.T, r *ReveniumAnthropic){
		"messages stream": func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
			require.NoError(t, err)
			wrapper := stream.(*StreamingWrapper)
			for wrapper.Next() {
			}
			require.Error(t, wrapper.Err())
			_ = wrapper.Close()
		},
		"beta messages stream": func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.BetaMessages().CreateMessageStream(context.Background(), betaParams())
			require.NoError(t, err)
			for stream.Next() {
			}
			require.Error(t, stream.Err())
			_ = stream.Close()
		},
	}
	for name, call := range surfaces {
		t.Run(name, func(t *testing.T) {
			serveSSE(t, toolUseStreamEvents[:3], failure)
			r, mock := newMeteredAnthropic(t)

			call(t, r)

			records := recordsAfterFlush(r, mock)
			require.NotEmpty(t, records)
			for _, record := range records {
				assert.NotContains(t, record, "inputMessages")
				assert.NotContains(t, record, "outputResponse")
			}
		})
	}
}

func TestPromptCapture_BedrockStreamsRecordToolUse(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	converseToolEvents := []bedrockEvent{
		{eventType: "messageStart", payload: `{"role":"assistant"}`},
		{eventType: "contentBlockDelta", payload: `{"contentBlockIndex":0,"delta":{"text":"Checking."}}`},
		{eventType: "contentBlockStop", payload: `{"contentBlockIndex":0}`},
		{eventType: "contentBlockStart", payload: `{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"toolu_7","name":"get_weather"}}}`},
		{eventType: "contentBlockDelta", payload: `{"contentBlockIndex":1,"delta":{"toolUse":{"input":"{\"city\":"}}}`},
		{eventType: "contentBlockDelta", payload: `{"contentBlockIndex":1,"delta":{"toolUse":{"input":"\"Paris\"}"}}}`},
		{eventType: "contentBlockStop", payload: `{"contentBlockIndex":1}`},
		{eventType: "messageStop", payload: `{"stopReason":"tool_use"}`},
		{eventType: "metadata", payload: `{"usage":{"inputTokens":10,"outputTokens":6,"totalTokens":16},"metrics":{"latencyMs":5}}`},
	}
	for _, useConverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("bedrock converse=%v", useConverse), func(t *testing.T) {
			stub := newBedrockStub(t)
			stub.streamWith(invokeStreamChunks(toolUseStreamEvents...))
			if useConverse {
				stub.streamWith(converseToolEvents)
			}
			mock := newMeteringMock(t)
			cfg := bedrockConfig(mock.URL())
			cfg.BedrockUseConverse = useConverse
			r := newBedrockAnthropic(t, cfg)

			stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
			require.NoError(t, err)
			drainStream(t, stream)

			assert.Equal(t, streamedToolReply, onlyRecord(t, r, mock)["outputResponse"])
		})
	}
}

func secretTextDeltas(count int) []string {
	events := []string{toolUseStreamEvents[0], toolUseStreamEvents[1]}
	delta := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"sk-ant-` + strings.Repeat("k", 1000) + ` "}}`
	for i := 0; i < count; i++ {
		events = append(events, delta)
	}
	return append(events, toolUseStreamEvents[3], `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":9}}`, `{"type":"message_stop"}`)
}

func observed(t *testing.T, reply *streamedReply, events ...string) {
	t.Helper()
	for _, raw := range events {
		var event anthropicsdk.MessageStreamEventUnion
		require.NoError(t, event.UnmarshalJSON([]byte(raw)))
		reply.observe(event)
	}
}

func TestPromptCapture_StreamedReplyHoldsAtMostTheStreamedLimit(t *testing.T) {
	t.Setenv(core.EnvMaxPromptSize, "300")
	reply := newStreamedReply()
	delta := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + strings.Repeat("x", 1000) + `"}}`

	for i := 0; i < 500; i++ {
		observed(t, reply, delta)
	}

	text, cut := reply.text()
	assert.True(t, cut)
	assert.Equal(t, prompt.StreamedReplyLimit()-1, utf8.RuneCountInString(text), "the one block costs one character")
}

func TestPromptCapture_StreamedReplyCountsStartPayloadsAgainstTheLimit(t *testing.T) {
	t.Setenv(core.EnvMaxPromptSize, "300")
	reply := newStreamedReply()
	large := `{"type":"content_block_start","index":0,"content_block":{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[{"type":"web_search_result","title":"` + strings.Repeat("r", 10000) + `","url":"https://example.com"}]}}`

	observed(t, reply, large)

	text, cut := reply.text()
	assert.True(t, cut)
	assert.LessOrEqual(t, utf8.RuneCountInString(text), prompt.StreamedReplyLimit())
	assert.NotContains(t, text, "rrrr")
}

func TestPromptCapture_StreamedReplyOpensNoBlocksOnceTheLimitIsSpent(t *testing.T) {
	t.Setenv(core.EnvMaxPromptSize, "300")
	reply := newStreamedReply()
	observed(t, reply, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"`+strings.Repeat("x", prompt.StreamedReplyLimit())+`"}}`)

	for i := 1; i <= 1000; i++ {
		observed(t, reply, fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":"toolu_%d","name":"lookup","input":{}}}`, i, i))
	}

	_, cut := reply.text()
	assert.Len(t, reply.blocks, 1)
	assert.True(t, cut)
}

// Redaction shrinks what the stream kept far below the limit, so only the stream's own cut
// flag can mark this reply truncated, and only the bound keeps it that short.
func TestPromptCapture_LongStreamedRepliesAreBoundedAndMarkedOnEveryPath(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	t.Setenv(core.EnvMaxPromptSize, "300")
	assertCutByTheStream := func(t *testing.T, record map[string]interface{}) {
		t.Helper()
		reply, _ := record["outputResponse"].(string)
		assert.Less(t, utf8.RuneCountInString(reply), 200)
		assert.True(t, strings.HasSuffix(reply, "...[TRUNCATED]"), reply)
		assert.Equal(t, true, record["promptsTruncated"])
	}

	t.Run("messages stream", func(t *testing.T) {
		serveSSE(t, secretTextDeltas(6), "")
		r, mock := newMeteredAnthropic(t)
		stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
		require.NoError(t, err)
		drainStream(t, stream)
		assertCutByTheStream(t, flushedRecords(t, r, mock, 1)[0])
	})

	converseText := `{"contentBlockIndex":0,"delta":{"text":"sk-ant-` + strings.Repeat("k", 1000) + ` "}}`
	converseEvents := []bedrockEvent{{eventType: "messageStart", payload: `{"role":"assistant"}`}}
	for i := 0; i < 6; i++ {
		converseEvents = append(converseEvents, bedrockEvent{eventType: "contentBlockDelta", payload: converseText})
	}
	converseEvents = append(converseEvents, converseStopEvents("end_turn")[2:]...)
	for _, useConverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("bedrock converse=%v", useConverse), func(t *testing.T) {
			stub := newBedrockStub(t)
			stub.streamWith(invokeStreamChunks(secretTextDeltas(6)...))
			if useConverse {
				stub.streamWith(converseEvents)
			}
			mock := newMeteringMock(t)
			cfg := bedrockConfig(mock.URL())
			cfg.BedrockUseConverse = useConverse
			r := newBedrockAnthropic(t, cfg)
			stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
			require.NoError(t, err)
			drainStream(t, stream)
			assertCutByTheStream(t, onlyRecord(t, r, mock))
		})
	}
}

func TestPromptCapture_ConverseStreamThinkingIsCapturedWithoutAnEmptyBlock(t *testing.T) {
	reply := newStreamedReply()
	index := func(i int32) *int32 { return &i }
	reply.observeConverse(&brtypes.ConverseStreamOutputMemberContentBlockDelta{Value: brtypes.ContentBlockDeltaEvent{
		ContentBlockIndex: index(0),
		Delta:             &brtypes.ContentBlockDeltaMemberReasoningContent{Value: &brtypes.ReasoningContentBlockDeltaMemberText{Value: "Thinking it over."}},
	}})
	reply.observeConverse(&brtypes.ConverseStreamOutputMemberContentBlockDelta{Value: brtypes.ContentBlockDeltaEvent{
		ContentBlockIndex: index(1),
		Delta:             &brtypes.ContentBlockDeltaMemberText{Value: "Paris."},
	}})

	text, _ := reply.text()

	assert.Equal(t, `{"signature":"","thinking":"Thinking it over.","type":"thinking"}`+"\nParis.", text)
}

func TestPromptCapture_StreamedReplyDroppedWholeIsStillMarkedTruncated(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	t.Setenv(core.EnvMaxPromptSize, "300")
	large := `{"type":"content_block_start","index":0,"content_block":{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[{"type":"web_search_result","title":"` + strings.Repeat("r", 10000) + `","url":"https://example.com"}]}}`
	serveSSE(t, []string{toolUseStreamEvents[0], large, `{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":9}}`, `{"type":"message_stop"}`}, "")
	r, mock := newMeteredAnthropic(t)

	stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
	require.NoError(t, err)
	drainStream(t, stream)

	record := flushedRecords(t, r, mock, 1)[0]
	assert.NotContains(t, record, "outputResponse")
	assert.Equal(t, true, record["promptsTruncated"])
}

const betaCompactionMessage = `{"id":"msg_c","type":"message","role":"assistant","model":"claude-haiku-4-5",` +
	`"content":[{"type":"compaction","content":"summary of the lookup"}],"stop_reason":"compaction",` +
	`"usage":{"input_tokens":40,"output_tokens":5}}`

func TestPromptCapture_CompactionTurnRecordsTheConversationItSent(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	newBetaProviderStub(t, betaToolUseMessage, betaCompactionMessage, betaFinalMessage)
	r, mock := newMeteredAnthropic(t)
	runner := r.BetaMessages().NewToolRunner([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams())

	_, err := runner.NextMessage(context.Background())
	require.NoError(t, err)
	runner.CompactBeforeNextTurn(anthropicsdk.BetaCompactionConfigUnionParam{})
	_, err = runner.NextMessage(context.Background())
	require.NoError(t, err)

	records := recordsAfterFlush(r, mock)
	require.Len(t, records, 2)
	sent := capturedInput(t, records[1])
	require.Len(t, sent, 3)
	assert.Equal(t, "user", sent[2]["role"])
	assert.Contains(t, records[1]["inputMessages"], "tool_result")
	assert.Contains(t, records[1]["outputResponse"], "summary of the lookup")
}

func TestPromptCapture_StreamedReplyBoundsAStreamOfEmptyBlocks(t *testing.T) {
	t.Setenv(core.EnvMaxPromptSize, "300")
	reply := newStreamedReply()

	for i := 0; i < 3*prompt.StreamedReplyLimit(); i++ {
		observed(t, reply, fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, i))
	}

	_, cut := reply.text()
	assert.LessOrEqual(t, len(reply.blocks), prompt.StreamedReplyLimit())
	assert.True(t, cut)
}

func TestPromptCapture_RunnersBuiltFromOneOptionSliceKeepTheirOwnCapture(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	newBetaProviderStub(t, betaFinalMessage)
	r, mock := newMeteredAnthropic(t)
	shared := make([]option.RequestOption, 0, 4)
	shared = append(shared, option.WithHeader("X-Test", "shared"))

	first := r.BetaMessages().NewToolRunner([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams(), shared...)
	r.BetaMessages().NewToolRunner([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams(), shared...)
	_, err := first.NextMessage(context.Background())
	require.NoError(t, err)

	records := recordsAfterFlush(r, mock)
	require.Len(t, records, 1)
	assert.Len(t, capturedInput(t, records[0]), 1)
	assert.Len(t, shared, 1)
}

func TestPromptCapture_StreamsStoppedEarlyAreMarkedTruncated(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	surfaces := map[string]func(t *testing.T, r *ReveniumAnthropic){
		"messages stream": func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
			require.NoError(t, err)
			wrapper := stream.(*StreamingWrapper)
			for i := 0; i < 3 && wrapper.Next(); i++ {
			}
			require.NoError(t, wrapper.Close())
		},
		"beta messages stream": func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.BetaMessages().CreateMessageStream(context.Background(), betaParams())
			require.NoError(t, err)
			for i := 0; i < 3 && stream.Next(); i++ {
			}
			require.NoError(t, stream.Close())
		},
	}
	for name, call := range surfaces {
		t.Run(name, func(t *testing.T) {
			serveSSE(t, toolUseStreamEvents, "")
			r, mock := newMeteredAnthropic(t)

			call(t, r)

			record := flushedRecords(t, r, mock, 1)[0]
			assert.True(t, strings.HasSuffix(record["outputResponse"].(string), "...[TRUNCATED]"), record["outputResponse"])
			assert.Equal(t, true, record["promptsTruncated"])
		})
	}
}

func TestPromptCapture_StreamsClosedAtTheirStopEventAreComplete(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	surfaces := map[string]func(t *testing.T, r *ReveniumAnthropic){
		"messages stream": func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
			require.NoError(t, err)
			wrapper := stream.(*StreamingWrapper)
			for range toolUseStreamEvents {
				require.True(t, wrapper.Next())
			}
			require.NoError(t, wrapper.Close())
		},
		"beta messages stream": func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.BetaMessages().CreateMessageStream(context.Background(), betaParams())
			require.NoError(t, err)
			for range toolUseStreamEvents {
				require.True(t, stream.Next())
			}
			require.NoError(t, stream.Close())
		},
	}
	for name, call := range surfaces {
		t.Run(name, func(t *testing.T) {
			serveSSE(t, toolUseStreamEvents, "")
			r, mock := newMeteredAnthropic(t)

			call(t, r)

			record := flushedRecords(t, r, mock, 1)[0]
			assert.Equal(t, streamedToolReply, record["outputResponse"])
			assert.NotContains(t, record, "promptsTruncated")
		})
	}
}

func TestPromptCapture_BetaStreamStoppedInsideAToolCallKeepsIt(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	serveSSE(t, toolUseStreamEvents[:6], "")
	r, mock := newMeteredAnthropic(t)

	stream, err := r.BetaMessages().CreateMessageStream(context.Background(), betaParams())
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Close())

	reply := flushedRecords(t, r, mock, 1)[0]["outputResponse"]
	assert.Contains(t, reply, "get_weather")
	assert.Contains(t, reply, `{\"city\":`)
}

func basicCredential(passwordLength int) string {
	return base64.StdEncoding.EncodeToString([]byte("admin:" + strings.Repeat("p", passwordLength)))
}

func toolInputDelta(partialJSON string) string {
	encoded, _ := json.Marshal(partialJSON)
	return `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":` + string(encoded) + `}}`
}

func TestPromptCapture_BasicCredentialCutInAStoppedToolInputIsRedacted(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	credential := basicCredential(60)
	cut := credential[:len(credential)-3]
	events := append(append([]string{}, toolUseStreamEvents[:5]...), toolInputDelta(`{"note":"Basic `+cut))
	events = append(events, toolUseStreamEvents[7:]...)
	surfaces := map[string]func(t *testing.T, r *ReveniumAnthropic){
		"messages stream": func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
			require.NoError(t, err)
			wrapper := stream.(*StreamingWrapper)
			for i := 0; i < 6; i++ {
				require.True(t, wrapper.Next())
			}
			require.NoError(t, wrapper.Close())
		},
		"beta messages stream": func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.BetaMessages().CreateMessageStream(context.Background(), betaParams())
			require.NoError(t, err)
			for i := 0; i < 6; i++ {
				require.True(t, stream.Next())
			}
			require.NoError(t, stream.Close())
		},
	}
	for name, call := range surfaces {
		t.Run(name, func(t *testing.T) {
			serveSSE(t, events, "")
			r, mock := newMeteredAnthropic(t)

			call(t, r)

			reply := flushedRecords(t, r, mock, 1)[0]["outputResponse"].(string)
			assert.Contains(t, reply, "get_weather")
			assert.Contains(t, reply, `Basic ***REDACTED***`)
			assert.NotContains(t, reply, cut[:24])
		})
	}
}

// The padding moves where the budget cuts the credential, so every base64 length the cut
// can leave, valid or not, is covered.
func TestPromptCapture_BasicCredentialCutByTheStreamBudgetInAToolInputIsRedacted(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	t.Setenv(core.EnvMaxPromptSize, "300")
	credential := basicCredential(6000)
	for padding := 0; padding < 4; padding++ {
		t.Run(fmt.Sprintf("padding %d", padding), func(t *testing.T) {
			delta := toolInputDelta(`{"note":"Basic ` + strings.Repeat(" ", padding) + credential + `"}`)
			events := []string{toolUseStreamEvents[0], strings.Replace(toolUseStreamEvents[4], `"index":1`, `"index":0`, 1),
				strings.Replace(delta, `"index":1`, `"index":0`, 1), `{"type":"content_block_stop","index":0}`}
			serveSSE(t, append(events, toolUseStreamEvents[8:]...), "")
			r, mock := newMeteredAnthropic(t)

			stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
			require.NoError(t, err)
			drainStream(t, stream)

			reply := flushedRecords(t, r, mock, 1)[0]["outputResponse"].(string)
			assert.Contains(t, reply, `***REDACTED***`)
			assert.NotContains(t, reply, credential[:24])
		})
	}
}

func textDelta(text string) string {
	encoded, _ := json.Marshal(text)
	return `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":` + string(encoded) + `}}`
}

var textReplyStopEvents = []string{
	`{"type":"content_block_stop","index":0}`,
	`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":9}}`,
	`{"type":"message_stop"}`,
}

// Every streaming surface read to the transport's end, so only the message_stop event
// can tell a finished reply from one the connection dropped.
var streamsReadToTheEnd = map[string]func(t *testing.T, r *ReveniumAnthropic){
	"messages stream": func(t *testing.T, r *ReveniumAnthropic) {
		stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
		require.NoError(t, err)
		drainStream(t, stream)
	},
	"beta messages stream": func(t *testing.T, r *ReveniumAnthropic) {
		stream, err := r.BetaMessages().CreateMessageStream(context.Background(), betaParams())
		require.NoError(t, err)
		for stream.Next() {
		}
		require.NoError(t, stream.Err())
		require.NoError(t, stream.Close())
	},
	"streaming tool runner turn": func(t *testing.T, r *ReveniumAnthropic) {
		runner := r.BetaMessages().NewToolRunnerStreaming([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams())
		for _, err := range runner.NextStreaming(context.Background()) {
			require.NoError(t, err)
		}
	},
	"streaming tool runner all turns": func(t *testing.T, r *ReveniumAnthropic) {
		runner := r.BetaMessages().NewToolRunnerStreaming([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams())
		for events, err := range runner.AllStreaming(context.Background()) {
			require.NoError(t, err)
			for _, eventErr := range events {
				require.NoError(t, eventErr)
			}
			break
		}
	},
}

func TestPromptCapture_StreamThatEndsCleanlyBeforeItsStopEventIsTruncatedAndMasksACutCredential(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	credential := basicCredential(60)
	cut := credential[:len(credential)-3]
	events := []string{toolUseStreamEvents[0], toolUseStreamEvents[1], textDelta("Use Basic " + cut)}
	for name, read := range streamsReadToTheEnd {
		t.Run(name, func(t *testing.T) {
			serveSSE(t, events, "")
			r, mock := newMeteredAnthropic(t)

			read(t, r)

			record := flushedRecords(t, r, mock, 1)[0]
			assert.Equal(t, true, record["promptsTruncated"])
			reply := record["outputResponse"].(string)
			assert.Contains(t, reply, `Basic ***REDACTED***`)
			assert.NotContains(t, reply, cut[:24])
		})
	}
}

func TestPromptCapture_StreamReadToItsStopEventIsComplete(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	events := append([]string{toolUseStreamEvents[0], toolUseStreamEvents[1], textDelta("Checking.")}, textReplyStopEvents...)
	for name, read := range streamsReadToTheEnd {
		t.Run(name, func(t *testing.T) {
			serveSSE(t, events, "")
			r, mock := newMeteredAnthropic(t)

			read(t, r)

			record := flushedRecords(t, r, mock, 1)[0]
			assert.Equal(t, "Checking.", record["outputResponse"])
			assert.NotContains(t, record, "promptsTruncated")
		})
	}
}
