package anthropic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/testutil"
)

const (
	advisorModel     = "claude-opus-5"
	executorModel    = "claude-haiku-4-5-20251001"
	turnInputTokens  = 30
	turnOutputTokens = 3
	turnCacheReads   = 4

	executorPass   = `{"type":"message","input_tokens":5,"output_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":4}`
	advisorEntry   = `{"type":"advisor_message","model":"claude-opus-5","input_tokens":823,"output_tokens":1612,"cache_creation_input_tokens":40,"cache_read_input_tokens":7,"cache_creation":{"ephemeral_5m_input_tokens":40,"ephemeral_1h_input_tokens":0}}`
	compactedEntry = `{"type":"compaction","input_tokens":1800,"output_tokens":350,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}`
	finalPass      = `{"type":"message","input_tokens":25,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}`
)

var (
	advisedIterations     = "[" + strings.Join([]string{executorPass, advisorEntry, compactedEntry, finalPass}, ",") + "]"
	messageOnlyIterations = "[" + executorPass + "," + finalPass + "]"
)

func turnUsageJSON(iterations string) string {
	usage := fmt.Sprintf(`"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":0`,
		turnInputTokens, turnOutputTokens, turnCacheReads)
	if iterations != "" {
		usage += `,"iterations":` + iterations
	}
	return "{" + usage + "}"
}

func turnMessageJSON(iterations string) string {
	return `{"id":"msg_adv","type":"message","role":"assistant","model":"` + executorModel + `",` +
		`"content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","stop_sequence":null,"usage":` + turnUsageJSON(iterations) + `}`
}

func turnStreamEvents(iterations string) []string {
	start := `{"type":"message_start","message":{"id":"msg_adv","type":"message","role":"assistant","model":"` + executorModel + `",` +
		`"content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1,"cache_read_input_tokens":4,"cache_creation_input_tokens":0}}}`
	return []string{
		start,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":` + turnUsageJSON(iterations) + `}`,
		`{"type":"message_stop"}`,
	}
}

func serveTurn(t *testing.T, iterations string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !body.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(turnMessageJSON(iterations)))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range turnStreamEvents(iterations) {
			var typed struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal([]byte(event), &typed)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typed.Type, event)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
}

func advisedBetaParams() anthropicsdk.BetaMessageNewParams {
	params := betaParams()
	params.Tools = []anthropicsdk.BetaToolUnionParam{{OfAdvisorTool20260301: &anthropicsdk.BetaAdvisorTool20260301Param{Model: advisorModel}}}
	return params
}

type turnSurface struct {
	name     string
	streamed bool
	call     func(t *testing.T, r *ReveniumAnthropic, ctx context.Context)
}

var turnSurfaces = []turnSurface{
	{"messages", false, func(t *testing.T, r *ReveniumAnthropic, ctx context.Context) {
		_, err := r.Messages().CreateMessage(ctx, messageParams("claude-haiku-4-5"))
		require.NoError(t, err)
	}},
	{"messages stream", true, func(t *testing.T, r *ReveniumAnthropic, ctx context.Context) {
		stream, err := r.Messages().CreateMessageStream(ctx, messageParams("claude-haiku-4-5"))
		require.NoError(t, err)
		drainStream(t, stream)
	}},
	{"beta messages", false, func(t *testing.T, r *ReveniumAnthropic, ctx context.Context) {
		_, err := r.BetaMessages().CreateMessage(ctx, advisedBetaParams())
		require.NoError(t, err)
	}},
	{"beta messages stream", true, func(t *testing.T, r *ReveniumAnthropic, ctx context.Context) {
		stream, err := r.BetaMessages().CreateMessageStream(ctx, advisedBetaParams())
		require.NoError(t, err)
		for stream.Next() {
			_ = stream.Current()
		}
		require.NoError(t, stream.Err())
		require.NoError(t, stream.Close())
	}},
}

func flushedRecords(t *testing.T, r *ReveniumAnthropic, mock *testutil.MockMeteringServer, want int) []map[string]interface{} {
	t.Helper()
	records := recordsAfterFlush(r, mock)
	require.Len(t, records, want)
	return records
}

func recordsBySubtype(records []map[string]interface{}) map[string]map[string]interface{} {
	bySubtype := map[string]map[string]interface{}{}
	for _, record := range records {
		subtype, _ := record["operationSubtype"].(string)
		bySubtype[subtype] = record
	}
	return bySubtype
}

func assertPassRecord(t *testing.T, main, pass map[string]interface{}, index int, subtype, model string, tokens [5]float64) {
	t.Helper()
	assert.Equal(t, fmt.Sprintf("%s-iteration-%d", main["transactionId"], index), pass["transactionId"])
	assert.Equal(t, main["transactionId"], pass["parentTransactionId"])
	assert.Equal(t, subtype, pass["operationSubtype"])
	assert.Equal(t, model, pass["model"])
	assert.Equal(t, tokens[0], pass["inputTokenCount"])
	assert.Equal(t, tokens[1], pass["outputTokenCount"])
	assert.Equal(t, tokens[0]+tokens[1], pass["totalTokenCount"])
	assert.Equal(t, tokens[2], pass["cacheCreationTokenCount"])
	assert.Equal(t, tokens[3], pass["cacheReadTokenCount"])
	assert.Equal(t, tokens[4], pass["reasoningTokenCount"])
	assert.Equal(t, "END", pass["stopReason"])
	for _, inherited := range []string{"provider", "isStreamed", "operationType", "requestTime", "traceId", "subscriber", "costType"} {
		assert.Equal(t, main[inherited], pass[inherited], inherited)
	}
}

func TestAdvisorAndCompactionPassesAreRecordedAtTheirOwnModel(t *testing.T) {
	for _, surface := range turnSurfaces {
		t.Run(surface.name, func(t *testing.T) {
			serveTurn(t, advisedIterations)
			r, mock := newMeteredAnthropic(t)
			ctx := core.WithUsageMetadata(asSubscriber("go-bughunt@example.com"), map[string]interface{}{"traceId": "trace-adv"})

			surface.call(t, r, ctx)

			records := recordsBySubtype(flushedRecords(t, r, mock, 3))
			main, advisor, compaction := records[""], records[advisorPass], records[compactionPass]
			require.NotNil(t, main)
			assert.Equal(t, float64(turnInputTokens), main["inputTokenCount"])
			assert.Equal(t, float64(turnOutputTokens), main["outputTokenCount"])
			assert.Equal(t, executorModel, main["model"])
			assert.Equal(t, surface.streamed, main["isStreamed"])
			assert.Equal(t, "trace-adv", main["traceId"])
			assert.NotContains(t, main, "parentTransactionId")

			require.NotNil(t, advisor)
			assertPassRecord(t, main, advisor, 1, advisorPass, advisorModel, [5]float64{823, 1612, 40, 7, 0})
			assert.Equal(t, 40.0, advisor["cacheCreation5mTokenCount"])
			assert.Equal(t, 0.0, advisor["cacheCreation1hTokenCount"])

			require.NotNil(t, compaction)
			assertPassRecord(t, main, compaction, 2, compactionPass, executorModel, [5]float64{1800, 350, 0, 0, 0})
			assert.NotContains(t, compaction, "cacheCreation5mTokenCount")
			assert.NotContains(t, compaction, "cacheCreation1hTokenCount")
		})
	}
}

func TestCallsWithoutBilledPassesSendOneUnchangedRecord(t *testing.T) {
	for name, iterations := range map[string]string{"no iterations": "", "empty": "[]", "executor passes only": messageOnlyIterations} {
		for _, surface := range turnSurfaces {
			t.Run(name+"/"+surface.name, func(t *testing.T) {
				serveTurn(t, iterations)
				r, mock := newMeteredAnthropic(t)

				surface.call(t, r, context.Background())

				record := flushedRecords(t, r, mock, 1)[0]
				assert.Equal(t, float64(turnInputTokens), record["inputTokenCount"])
				assert.Equal(t, float64(turnOutputTokens), record["outputTokenCount"])
				assert.NotContains(t, record, "operationSubtype")
				assert.NotContains(t, record, "parentTransactionId")
			})
		}
	}
}

func idempotencyKeysByTransaction(mock *testutil.MockMeteringServer) map[string]string {
	keys := map[string]string{}
	for _, request := range mock.GetRequests() {
		keys[request.Payload["transactionId"].(string)] = request.Headers.Get("Idempotency-Key")
	}
	return keys
}

func TestPassRecordsDeriveTheirIdsFromTheCallsOwn(t *testing.T) {
	serveTurn(t, advisedIterations)
	r, mock := newMeteredAnthropic(t)
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
		"transactionId": "tx-1", "idempotencyKey": "blk", "parentTransactionId": "outer-tx",
		"totalCost": 0.5, "inputTokenCost": 0.25,
	})

	_, err := r.BetaMessages().CreateMessage(ctx, advisedBetaParams())
	require.NoError(t, err)

	records := recordsBySubtype(flushedRecords(t, r, mock, 3))
	assert.Equal(t, "outer-tx", records[""]["parentTransactionId"])
	assert.Equal(t, 0.5, records[""]["totalCost"])
	for _, pass := range []map[string]interface{}{records[advisorPass], records[compactionPass]} {
		assert.Equal(t, "tx-1", pass["parentTransactionId"])
		assert.NotContains(t, pass, "totalCost")
		assert.NotContains(t, pass, "inputTokenCost")
	}
	assert.Equal(t, map[string]string{
		"tx-1":             "blk",
		"tx-1-iteration-1": "blk-iteration-1",
		"tx-1-iteration-2": "blk-iteration-2",
	}, idempotencyKeysByTransaction(mock))
}

func TestIterationIdempotencyKeyStaysWithinTheBackendLimit(t *testing.T) {
	fits := strings.Repeat("k", maxIdempotencyKeyLength-len("-iteration-1"))
	assert.Equal(t, fits+"-iteration-1", iterationIdempotencyKey(fits, 1))

	tooLong := fits + "k"
	digest := sha256.Sum256([]byte(tooLong))
	hashed := iterationIdempotencyKey(tooLong, 1)
	assert.Equal(t, hex.EncodeToString(digest[:])+"-iteration-1", hashed)
	assert.LessOrEqual(t, len(hashed), maxIdempotencyKeyLength)
}

func TestAdvisorPassWithoutAModelIsPricedAtTheAdvisorTool(t *testing.T) {
	unnamedAdvisor := strings.Replace(advisorEntry, `"model":"claude-opus-5",`, "", 1)
	serveTurn(t, "["+executorPass+","+unnamedAdvisor+"]")
	r, mock := newMeteredAnthropic(t)

	_, err := r.BetaMessages().CreateMessage(context.Background(), advisedBetaParams())
	require.NoError(t, err)

	records := recordsBySubtype(flushedRecords(t, r, mock, 2))
	assert.Equal(t, advisorModel, records[advisorPass]["model"])
}

func TestBedrockInvokeModelRecordsAdvisorAndCompactionPasses(t *testing.T) {
	streamed := map[bool]func(t *testing.T, r *ReveniumAnthropic){
		false: func(t *testing.T, r *ReveniumAnthropic) {
			_, err := r.Messages().CreateMessage(context.Background(), messageParams(executorModel))
			require.NoError(t, err)
		},
		true: func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams(executorModel))
			require.NoError(t, err)
			drainStream(t, stream)
		},
	}
	for isStream, call := range streamed {
		t.Run(fmt.Sprintf("streamed=%v", isStream), func(t *testing.T) {
			stub := newBedrockStub(t)
			stub.respondWith(turnMessageJSON(advisedIterations))
			stub.streamWith(invokeStreamChunks(turnStreamEvents(advisedIterations)...))
			mock := newMeteringMock(t)
			r := newBedrockAnthropic(t, bedrockConfig(mock.URL()))

			call(t, r)

			records := recordsBySubtype(flushedRecords(t, r, mock, 3))
			main := records[""]
			assert.Equal(t, "Amazon Bedrock", main["provider"])
			assert.Equal(t, float64(turnInputTokens), main["inputTokenCount"])
			assertPassRecord(t, main, records[advisorPass], 1, advisorPass, advisorModel, [5]float64{823, 1612, 40, 7, 0})
			assertPassRecord(t, main, records[compactionPass], 2, compactionPass, main["model"].(string), [5]float64{1800, 350, 0, 0, 0})
		})
	}
}

func runnerTurnJSON(id, stopReason, content, iterations string) string {
	return `{"id":"` + id + `","type":"message","role":"assistant","model":"` + executorModel + `","content":` + content +
		`,"stop_reason":"` + stopReason + `","stop_sequence":null,"usage":` + turnUsageJSON(iterations) + `}`
}

func messageAsStreamEvents(t *testing.T, message string) []string {
	t.Helper()
	var decoded struct {
		ID         string            `json:"id"`
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason"`
		Usage      json.RawMessage   `json:"usage"`
	}
	require.NoError(t, json.Unmarshal([]byte(message), &decoded))
	events := []string{`{"type":"message_start","message":{"id":"` + decoded.ID + `","type":"message","role":"assistant","model":"` + executorModel +
		`","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1}}}`}
	for i, block := range decoded.Content {
		events = append(events,
			fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":%s}`, i, block),
			fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i))
	}
	return append(events,
		`{"type":"message_delta","delta":{"stop_reason":"`+decoded.StopReason+`","stop_sequence":null},"usage":`+string(decoded.Usage)+`}`,
		`{"type":"message_stop"}`)
}

func serveTurnSequence(t *testing.T, messages ...string) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		message := messages[min(int(calls.Add(1))-1, len(messages)-1)]
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !body.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(message))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range messageAsStreamEvents(t, message) {
			var typed struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal([]byte(event), &typed)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typed.Type, event)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
}

func TestToolRunnersRecordEachTurnsPassesAgainstThatTurn(t *testing.T) {
	unnamedAdvisor := strings.Replace(advisorEntry, `"model":"claude-opus-5",`, "", 1)
	toolUseTurn := runnerTurnJSON("msg_turn_1", "tool_use", `[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}]`,
		"["+executorPass+","+unnamedAdvisor+"]")
	finalTurn := runnerTurnJSON("msg_turn_2", "end_turn", `[{"type":"text","text":"done"}]`, "["+compactedEntry+","+finalPass+"]")

	runners := map[string]func(t *testing.T, r *ReveniumAnthropic, params anthropicsdk.BetaToolRunnerParams){
		"tool runner": func(t *testing.T, r *ReveniumAnthropic, params anthropicsdk.BetaToolRunnerParams) {
			runner := r.BetaMessages().NewToolRunner([]anthropicsdk.BetaTool{&lookupTool{}}, params)
			runner.Params.Tools = append(runner.Params.Tools, advisedBetaParams().Tools...)
			_, err := runner.RunToCompletion(context.Background())
			require.NoError(t, err)
		},
		"streaming tool runner": func(t *testing.T, r *ReveniumAnthropic, params anthropicsdk.BetaToolRunnerParams) {
			runner := r.BetaMessages().NewToolRunnerStreaming([]anthropicsdk.BetaTool{&lookupTool{}}, params)
			runner.Params.Tools = append(runner.Params.Tools, advisedBetaParams().Tools...)
			for events, err := range runner.AllStreaming(context.Background()) {
				require.NoError(t, err)
				for _, eventErr := range events {
					require.NoError(t, eventErr)
				}
			}
		},
	}
	for name, run := range runners {
		t.Run(name, func(t *testing.T) {
			serveTurnSequence(t, toolUseTurn, finalTurn)
			r, mock := newMeteredAnthropic(t)

			run(t, r, runnerParams())

			records := recordsAfterFlush(r, mock)
			require.Len(t, records, 4)
			var mains []map[string]interface{}
			bySubtype := recordsBySubtype(records)
			for _, record := range records {
				if _, isPass := record["operationSubtype"]; !isPass {
					mains = append(mains, record)
				}
			}
			require.Len(t, mains, 2)
			advisor, compaction := bySubtype[advisorPass], bySubtype[compactionPass]
			require.NotNil(t, advisor)
			require.NotNil(t, compaction)

			toolUseMain, finalMain := mains[0], mains[1]
			if advisor["parentTransactionId"] != toolUseMain["transactionId"] {
				toolUseMain, finalMain = finalMain, toolUseMain
			}
			assertPassRecord(t, toolUseMain, advisor, 1, advisorPass, advisorModel, [5]float64{823, 1612, 40, 7, 0})
			assertPassRecord(t, finalMain, compaction, 0, compactionPass, executorModel, [5]float64{1800, 350, 0, 0, 0})
		})
	}
}

func advisorPassFixture() billedPass {
	return billedPass{index: 1, kind: advisorPass, model: advisorModel, inputTokens: 823, outputTokens: 1612}
}

func TestPassRecordsOfCallsWithoutAnIdempotencyKeyStayDistinct(t *testing.T) {
	first := &metering.MeteringPayload{TransactionID: "tx-a", Model: executorModel}
	second := &metering.MeteringPayload{TransactionID: "tx-b", Model: executorModel}

	firstPass := iterationRecord(first, advisorPassFixture(), "")
	secondPass := iterationRecord(second, advisorPassFixture(), "")

	assert.Equal(t, "tx-a-iteration-1", firstPass.IdempotencyKey)
	assert.Equal(t, "tx-b-iteration-1", secondPass.IdempotencyKey)
}

func TestPassRecordSharesNoMutableStateWithTheCallsRecord(t *testing.T) {
	temperature := 0.2
	main := &metering.MeteringPayload{
		TransactionID:  "tx-1",
		IdempotencyKey: "key-1",
		Model:          executorModel,
		Temperature:    &temperature,
		Attributes:     map[string]interface{}{"hasVision": true},
		Subscriber:     map[string]interface{}{"email": "go-bughunt@example.com", "credential": map[string]interface{}{"name": "k"}},
	}

	pass := iterationRecord(main, advisorPassFixture(), "")
	pass.Attributes["extra"] = 1
	pass.Subscriber["email"] = "someone-else@example.com"
	pass.Subscriber["credential"].(map[string]interface{})["name"] = "changed"
	*pass.Temperature = 0.9

	assert.Equal(t, map[string]interface{}{"hasVision": true}, main.Attributes)
	assert.Equal(t, map[string]interface{}{"email": "go-bughunt@example.com", "credential": map[string]interface{}{"name": "k"}}, main.Subscriber)
	assert.Equal(t, 0.2, *main.Temperature)
	assert.Equal(t, "key-1-iteration-1", pass.IdempotencyKey)
}

var (
	passOwnFields = fieldSet("IdempotencyKey", "TransactionID", "ParentTransactionID", "OperationSubtype", "Model",
		"InputTokenCount", "OutputTokenCount", "TotalTokenCount", "ReasoningTokenCount", "CacheCreationTokenCount",
		"CacheReadTokenCount", "CacheCreation5mTokenCount", "CacheCreation1hTokenCount", "StopReason")
	inheritedFromCall = fieldSet("CostType", "IsStreamed", "OperationType", "Provider", "MiddlewareSource", "ModelSource",
		"RequestTime", "ResponseTime", "RequestDuration", "CompletionStartTime", "TimeToFirstToken", "SystemFingerprint",
		"Temperature", "Attributes", "OrganizationName", "ProductName", "TaskType", "Agent", "AgentVersion",
		"SquadID", "SquadName", "SquadRole", "SubscriptionID", "TraceID", "TraceType", "TraceName", "TransactionName",
		"TicketID", "AgenticJobID", "AgenticJobName", "AgenticJobType", "AgenticJobVersion", "Environment", "Region",
		"RetryNumber", "CredentialAlias", "Subscriber", "TaskID", "CodingAssistantAccountUUID",
		"Effort", "SpeedMode", "QuerySource", "SubagentType", "PromptID", "PromptLength", "ClientSurface",
		"SkillName", "SkillInvocationTrigger", "SkillSource", "SkillKind", "SkillPluginName", "SkillMarketplaceName")
	clearedOnPass = fieldSet("ErrorReason", "ErrorCode", "CompletionStatus", "FailureCode", "ResponseQualityScore", "MediationLatency",
		"InputTokenCost", "OutputTokenCost", "CacheCreationTokenCost", "CacheReadTokenCost", "TotalCost",
		"InputMessages", "OutputResponse", "PromptsTruncated", "SystemPrompt",
		"BillingSkipped", "PricingTier", "RequestedServiceTier", "ActualServiceTier", "PriorityTier",
		"SubscriptionTier", "CostMultiplier", "SourceTransactionID",
		"ActualImageCount", "RequestedImageCount", "DurationSeconds", "RequestedDurationSeconds", "BillingUnit",
		"Resolution", "Quality", "Style", "Format", "CharacterCount", "InputAudioTokenCount", "Language",
		"TargetLanguage", "ResponseFormat", "Voice", "Speed", "VideoJobID", "AudioJobID")
)

func fieldSet(names ...string) map[string]bool {
	set := map[string]bool{}
	for _, name := range names {
		set[name] = true
	}
	return set
}

func nonZeroValue(t *testing.T, typ reflect.Type, name string) reflect.Value {
	t.Helper()
	value := reflect.New(typ).Elem()
	switch typ.Kind() {
	case reflect.String:
		value.SetString("set-" + name)
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(7)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(7.5)
	case reflect.Ptr:
		pointee := reflect.New(typ.Elem())
		pointee.Elem().Set(nonZeroValue(t, typ.Elem(), name))
		value.Set(pointee)
	case reflect.Map:
		value.Set(reflect.MakeMap(typ))
		value.SetMapIndex(nonZeroValue(t, typ.Key(), name), nonZeroValue(t, typ.Elem(), name))
	case reflect.Slice:
		value.Set(reflect.Append(reflect.MakeSlice(typ, 0, 1), nonZeroValue(t, typ.Elem(), name)))
	case reflect.Interface:
		value.Set(reflect.ValueOf("set-" + name))
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			if typ.Field(i).IsExported() {
				value.Field(i).Set(nonZeroValue(t, typ.Field(i).Type, name+"."+typ.Field(i).Name))
			}
		}
	default:
		t.Fatalf("MeteringPayload.%s has a kind this guard cannot fill: %s", name, typ.Kind())
	}
	return value
}

func fillEveryField(t *testing.T, payload *metering.MeteringPayload) {
	t.Helper()
	value := reflect.ValueOf(payload).Elem()
	for i := 0; i < value.NumField(); i++ {
		value.Field(i).Set(nonZeroValue(t, value.Type().Field(i).Type, value.Type().Field(i).Name))
	}
}

func sharesStorage(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice:
		return !a.IsNil() && a.Pointer() == b.Pointer()
	default:
		return false
	}
}

func TestEveryRecordFieldIsClassifiedForPassRecords(t *testing.T) {
	main := &metering.MeteringPayload{}
	fillEveryField(t, main)

	pass := iterationRecord(main, advisorPassFixture(), "")

	mainValue, passValue := reflect.ValueOf(main).Elem(), reflect.ValueOf(pass).Elem()
	for i := 0; i < mainValue.NumField(); i++ {
		name := mainValue.Type().Field(i).Name
		switch {
		case passOwnFields[name]:
		case inheritedFromCall[name]:
			assert.Equal(t, mainValue.Field(i).Interface(), passValue.Field(i).Interface(), "%s should be the call's", name)
			assert.False(t, sharesStorage(mainValue.Field(i), passValue.Field(i)), "%s must be a copy, not the call's own", name)
		case clearedOnPass[name]:
			assert.True(t, passValue.Field(i).IsZero(), "%s belongs to the call alone and must not be repeated on its passes", name)
		default:
			t.Errorf("MeteringPayload.%s is not classified for pass records: add it to inheritedFromCall or clearedOnPass", name)
		}
	}
}

func TestPassesOfACallWithASpeedModeAreRecordedWithIt(t *testing.T) {
	serveTurn(t, advisedIterations)
	r, mock := newMeteredAnthropic(t)
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"speed": "fast", "squadId": "squad-1"})

	_, err := r.BetaMessages().CreateMessage(ctx, advisedBetaParams())
	require.NoError(t, err)

	records := recordsBySubtype(flushedRecords(t, r, mock, 3))
	for _, subtype := range []string{"", advisorPass, compactionPass} {
		assert.Equal(t, "fast", records[subtype]["speed"], subtype)
		assert.Equal(t, "squad-1", records[subtype]["squadId"], subtype)
	}
}

type subscriberTeam struct {
	Members []string
}

func TestPassRecordKeepsItsValuesWhenTheCallersTypedMetadataChangesLater(t *testing.T) {
	roles := []string{"admin"}
	quotas := map[string]int{"daily": 10}
	team := &subscriberTeam{Members: []string{"ana"}}
	main := &metering.MeteringPayload{
		TransactionID: "tx-1",
		Model:         executorModel,
		Subscriber:    map[string]interface{}{"email": "go-bughunt@example.com", "roles": roles, "team": team},
		Attributes:    map[string]interface{}{"quotas": quotas, "tags": []interface{}{map[string]int{"weight": 1}}},
	}

	pass := iterationRecord(main, advisorPassFixture(), "")
	roles[0] = "guest"
	quotas["daily"] = 0
	team.Members[0] = "bob"
	main.Attributes["tags"].([]interface{})[0].(map[string]int)["weight"] = 9

	assert.Equal(t, []string{"admin"}, pass.Subscriber["roles"])
	assert.Equal(t, []string{"ana"}, pass.Subscriber["team"].(*subscriberTeam).Members)
	assert.Equal(t, map[string]int{"daily": 10}, pass.Attributes["quotas"])
	assert.Equal(t, map[string]int{"weight": 1}, pass.Attributes["tags"].([]interface{})[0])
}

type teamNode struct {
	Name     string
	Parent   *teamNode `json:"-"`
	Children []*teamNode
}

func TestPassRecordCopiesCallerMetadataThatRefersBackToItself(t *testing.T) {
	selfReferencing := map[string]interface{}{"name": "loop"}
	selfReferencing["self"] = selfReferencing
	root := &teamNode{Name: "platform"}
	root.Children = []*teamNode{{Name: "billing", Parent: root}}
	main := &metering.MeteringPayload{
		TransactionID: "tx-1",
		Model:         executorModel,
		Attributes:    map[string]interface{}{"loop": selfReferencing},
		Subscriber:    map[string]interface{}{"email": "go-bughunt@example.com", "team": root},
	}

	pass := iterationRecord(main, advisorPassFixture(), "")

	copiedLoop := pass.Attributes["loop"].(map[string]interface{})
	assert.Equal(t, reflect.ValueOf(copiedLoop).Pointer(), reflect.ValueOf(copiedLoop["self"]).Pointer(), "the copy keeps the cycle")
	assert.NotEqual(t, reflect.ValueOf(selfReferencing).Pointer(), reflect.ValueOf(copiedLoop).Pointer(), "the copy is not the caller's map")
	copiedRoot := pass.Subscriber["team"].(*teamNode)
	assert.Same(t, copiedRoot, copiedRoot.Children[0].Parent, "the copy keeps the back-pointer")
	assert.NotSame(t, root, copiedRoot)

	selfReferencing["name"] = "changed"
	root.Children[0].Name = "changed"
	assert.Equal(t, "loop", copiedLoop["name"])
	assert.Equal(t, "billing", copiedRoot.Children[0].Name)
}

func TestACallWithSelfReferencingMetadataStillRecordsItsPasses(t *testing.T) {
	serveTurn(t, advisedIterations)
	r, mock := newMeteredAnthropic(t)
	root := &teamNode{Name: "platform"}
	root.Children = []*teamNode{{Name: "billing", Parent: root}}
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
		"subscriber": map[string]interface{}{"email": "go-bughunt@example.com", "team": root},
	})

	_, err := r.BetaMessages().CreateMessage(ctx, advisedBetaParams())
	require.NoError(t, err)

	records := recordsBySubtype(flushedRecords(t, r, mock, 3))
	assert.Contains(t, records, advisorPass)
	assert.Contains(t, records, compactionPass)
}
