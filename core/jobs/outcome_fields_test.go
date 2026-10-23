package jobs

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedRequest struct {
	method string
	path   string
	body   string
}

type scriptedServer struct {
	mu       sync.Mutex
	requests []recordedRequest
	answers  []func(w http.ResponseWriter)
}

func newScriptedServer(t *testing.T, answers ...func(w http.ResponseWriter)) (*scriptedServer, *JobClient) {
	t.Helper()
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	script := &scriptedServer{answers: answers}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		script.mu.Lock()
		script.requests = append(script.requests, recordedRequest{r.Method, r.URL.Path, string(body)})
		answer := script.answers[min(len(script.requests), len(script.answers))-1]
		script.mu.Unlock()
		answer(w)
	}))
	t.Cleanup(srv.Close)
	client := newTestClient(t, srv.URL)
	client.retryConfig = &resilience.RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	t.Cleanup(client.Close)
	return script, client
}

func (s *scriptedServer) recorded() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedRequest(nil), s.requests...)
}

func answer(status int, body string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func decodeBody(t *testing.T, raw string) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &body))
	return body
}

const storedJob = `{"id":"j1","agenticJobId":"loan-77","hasOutcome":true,"executionStatus":"FAILED",` +
	`"outcomeType":"UNSUCCESSFUL","outcomeReason":"Customer unreachable","outcomeUpdateCount":0,"entityVersion":1}`

const platformAlreadyReported = `{"status":409,"error":"Conflict","message":"Outcome already reported",` +
	`"details":{"guidance":"Use PATCH /v2/api/jobs/loan-77/outcome to update","reportedAt":"2026-10-13T10:00:00Z","updateCount":"2"}}`

func failedLoan() *JobOutcome {
	return &JobOutcome{
		ExecutionStatus: ExecutionStatusFailed,
		OutcomeType:     OutcomeUnsuccessful,
		OutcomeReason:   "Customer unreachable",
	}
}

func TestReportJobOutcome_SendsTheReasonAndMetricsUnderThePlatformFieldNames(t *testing.T) {
	script, client := newScriptedServer(t, answer(http.StatusOK, storedJob))
	recordedAt := time.Date(2026, 10, 13, 9, 30, 0, 0, time.UTC)
	outcome := failedLoan()
	outcome.Metrics = []OutcomeMetric{
		{Key: "quality_rate", Value: 0.93},
		{Key: "minutes_saved", Value: 12, Provenance: MetricMeasured, RecordedBy: "grader", Source: "qa", Reason: "graded", RecordedAt: &recordedAt},
	}

	job, err := client.ReportJobOutcome("loan-77", outcome)

	require.NoError(t, err)
	body := decodeBody(t, script.recorded()[0].body)
	assert.Equal(t, "Customer unreachable", body["outcomeReason"])
	assert.NotContains(t, body, "metadata")
	assert.Equal(t, []any{
		map[string]any{"key": "quality_rate", "value": 0.93},
		map[string]any{"key": "minutes_saved", "value": 12.0, "provenance": "MEASURED", "recordedBy": "grader",
			"source": "qa", "reason": "graded", "recordedAt": "2026-10-13T09:30:00Z"},
	}, body["metrics"])
	require.NotNil(t, job.OutcomeReason)
	assert.Equal(t, "Customer unreachable", *job.OutcomeReason)
	require.NotNil(t, job.EntityVersion)
	assert.Equal(t, int64(1), *job.EntityVersion)
	require.NotNil(t, job.OutcomeAmendmentCount)
	assert.Equal(t, 0, *job.OutcomeAmendmentCount)
}

func TestReportJobOutcome_RefusesABlankMetricKeyBeforeSending(t *testing.T) {
	script, client := newScriptedServer(t, answer(http.StatusOK, storedJob))
	outcome := failedLoan()
	outcome.Metrics = []OutcomeMetric{{Key: " ", Value: 1}}

	_, err := client.ReportJobOutcome("loan-77", outcome)

	require.Error(t, err)
	assert.True(t, core.IsValidationError(err))
	assert.Empty(t, script.recorded())
}

func TestAmendJobOutcome_SendsTheVersionGuardReasonAndMetrics(t *testing.T) {
	script, client := newScriptedServer(t, answer(http.StatusOK, storedJob))
	version := int64(0)
	cleared := ""

	_, err := client.AmendJobOutcome("loan-77", &JobOutcomeAmendment{
		Reason:                "late correction",
		ExpectedEntityVersion: &version,
		OutcomeReason:         &cleared,
		Metrics:               []OutcomeMetric{{Key: "quality_rate", Value: 0.5, Reason: "regraded"}},
	})

	require.NoError(t, err)
	body := decodeBody(t, script.recorded()[0].body)
	assert.Equal(t, 0.0, body["expectedEntityVersion"])
	assert.Equal(t, "", body["outcomeReason"])
	assert.Equal(t, []any{map[string]any{"key": "quality_rate", "value": 0.5, "reason": "regraded"}}, body["metrics"])
}

func TestAmendJobOutcome_LeavesTheStoredReasonAndVersionAloneWhenUnset(t *testing.T) {
	script, client := newScriptedServer(t, answer(http.StatusOK, storedJob))

	_, err := client.AmendJobOutcome("loan-77", &JobOutcomeAmendment{Reason: "fix", ExecutionStatus: ExecutionStatusSuccess})

	require.NoError(t, err)
	body := decodeBody(t, script.recorded()[0].body)
	assert.NotContains(t, body, "outcomeReason")
	assert.NotContains(t, body, "expectedEntityVersion")
	assert.NotContains(t, body, "metrics")
}

func TestAmendJobOutcome_AStaleVersionReturnsThePlatformsConflictWithTheCurrentVersion(t *testing.T) {
	_, client := newScriptedServer(t, answer(http.StatusConflict,
		`{"status":409,"error":"Conflict","message":"Outcome has changed since entity version 3; current version is 4. Refetch and retry."}`))
	version := int64(3)

	result, err := client.AmendJobOutcome("loan-77", &JobOutcomeAmendment{Reason: "fix", ExpectedEntityVersion: &version})

	assert.Nil(t, result)
	var conflict *OutcomeAmendConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Contains(t, conflict.Message, "Outcome has changed since entity version 3; current version is 4. Refetch and retry. Re-check")
	require.NotNil(t, conflict.CurrentEntityVersion)
	assert.Equal(t, int64(4), *conflict.CurrentEntityVersion)
}

func TestAmendJobOutcome_PrefersAStructuredCurrentVersion(t *testing.T) {
	_, client := newScriptedServer(t, answer(http.StatusConflict,
		`{"message":"Outcome has changed; current version is 4.","details":{"currentEntityVersion":"7"}}`))

	_, err := client.AmendJobOutcome("loan-77", &JobOutcomeAmendment{Reason: "fix"})

	var conflict *OutcomeAmendConflictError
	require.ErrorAs(t, err, &conflict)
	require.NotNil(t, conflict.CurrentEntityVersion)
	assert.Equal(t, int64(7), *conflict.CurrentEntityVersion)
}

func TestAmendJobOutcome_RefusesANegativeVersionBeforeSending(t *testing.T) {
	script, client := newScriptedServer(t, answer(http.StatusOK, storedJob))
	version := int64(-1)

	_, err := client.AmendJobOutcome("loan-77", &JobOutcomeAmendment{Reason: "fix", ExpectedEntityVersion: &version})

	require.Error(t, err)
	assert.True(t, core.IsValidationError(err))
	assert.Empty(t, script.recorded())
}

func TestAppendOutcomeMetrics_PostsTheEntriesAsABareArray(t *testing.T) {
	script, client := newScriptedServer(t, answer(http.StatusCreated, ""))

	err := client.AppendOutcomeMetrics("loan-77", []OutcomeMetric{{Key: "quality_rate", Value: 0.8, Provenance: MetricAttested}})

	require.NoError(t, err)
	requests := script.recorded()
	require.Len(t, requests, 1)
	assert.Equal(t, http.MethodPost, requests[0].method)
	assert.Equal(t, "/profitstream/v2/api/jobs/loan-77/outcome/metrics", requests[0].path)
	assert.JSONEq(t, `[{"key":"quality_rate","value":0.8,"provenance":"ATTESTED"}]`, requests[0].body)
}

func TestAppendOutcomeMetrics_RefusesNothingToAppendOrABlankKey(t *testing.T) {
	script, client := newScriptedServer(t, answer(http.StatusCreated, ""))

	assert.Error(t, client.AppendOutcomeMetrics("loan-77", nil))
	assert.Error(t, client.AppendOutcomeMetrics("loan-77", []OutcomeMetric{{Key: "", Value: 1}}))
	assert.Error(t, client.AppendOutcomeMetrics("", []OutcomeMetric{{Key: "k", Value: 1}}))
	assert.Empty(t, script.recorded())
}

var writesNotSafeToRepeat = map[string]func(*JobClient) error{
	"amendment": func(c *JobClient) error {
		_, err := c.AmendJobOutcome("loan-77", &JobOutcomeAmendment{Reason: "fix"})
		return err
	},
	"late metrics": func(c *JobClient) error {
		return c.AppendOutcomeMetrics("loan-77", []OutcomeMetric{{Key: "k", Value: 1}})
	},
}

func dropConnection(w http.ResponseWriter) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func TestWritesThatAreNotSafeToRepeat_AreNotResentAfterAnAnswerThatMayHaveBeenApplied(t *testing.T) {
	failures := map[string]func(w http.ResponseWriter){
		"gateway error":   answer(http.StatusBadGateway, "bad gateway"),
		"server error":    answer(http.StatusInternalServerError, "boom"),
		"connection lost": dropConnection,
	}
	for writeName, write := range writesNotSafeToRepeat {
		for failureName, failure := range failures {
			t.Run(writeName+" after "+failureName, func(t *testing.T) {
				script, client := newScriptedServer(t, failure, answer(http.StatusOK, storedJob))

				err := write(client)

				require.Error(t, err)
				assert.Len(t, script.recorded(), 1)
			})
		}
	}
}

func TestWritesThatAreNotSafeToRepeat_AreResentAfterA429(t *testing.T) {
	for name, write := range writesNotSafeToRepeat {
		t.Run(name, func(t *testing.T) {
			script, client := newScriptedServer(t, answer(http.StatusTooManyRequests, ""), answer(http.StatusOK, storedJob))

			require.NoError(t, write(client))
			assert.Len(t, script.recorded(), 2)
		})
	}
}

func TestWritesThatAreNotSafeToRepeat_KeepTheServersErrorWhenTheBreakerOpens(t *testing.T) {
	for name, write := range writesNotSafeToRepeat {
		t.Run(name, func(t *testing.T) {
			script, client := newScriptedServer(t, answer(http.StatusTooManyRequests, `{"error":"slow down"}`))

			err := write(client)

			assert.ErrorIs(t, err, resilience.ErrCircuitOpen)
			var revErr *core.ReveniumError
			require.ErrorAs(t, err, &revErr)
			assert.Equal(t, http.StatusTooManyRequests, revErr.StatusCode)
			assert.Contains(t, err.Error(), "slow down")
			assert.Len(t, script.recorded(), 3, "the call stops once its own failures opened the breaker")
		})
	}
}

func TestWritesThatAreNotSafeToRepeat_HonourAServerRequestedPauseLikeEveryOtherCall(t *testing.T) {
	for name, write := range writesNotSafeToRepeat {
		t.Run(name, func(t *testing.T) {
			script, client := newScriptedServer(t, func(w http.ResponseWriter) {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
			})

			err := write(client)

			var revErr *core.ReveniumError
			require.ErrorAs(t, err, &revErr)
			assert.Equal(t, time.Minute, revErr.RetryAfter.Round(time.Second))
			assert.ErrorIs(t, write(client), resilience.ErrCircuitOpen, "the next write waits out the pause")
			assert.Len(t, script.recorded(), 1)
		})
	}
}

func TestReportJobOutcome_ReadsThePlatformsAlreadyReportedDetails(t *testing.T) {
	_, client := newScriptedServer(t, answer(http.StatusConflict, platformAlreadyReported))

	_, err := client.ReportJobOutcome("loan-77", failedLoan())

	var reported *OutcomeAlreadyReportedError
	require.ErrorAs(t, err, &reported)
	assert.Equal(t, "Outcome already reported", reported.Message)
	require.NotNil(t, reported.ReportedAt)
	assert.Equal(t, "2026-10-13T10:00:00Z", *reported.ReportedAt)
	assert.Equal(t, 2, reported.AmendmentCount)
}

func TestReportJobOutcome_A409OnTheFirstAttemptIsAlwaysAlreadyReported(t *testing.T) {
	script, client := newScriptedServer(t, answer(http.StatusConflict, platformAlreadyReported), answer(http.StatusOK, storedJob))

	_, err := client.ReportJobOutcome("loan-77", failedLoan())

	var reported *OutcomeAlreadyReportedError
	require.ErrorAs(t, err, &reported)
	assert.Len(t, script.recorded(), 1, "nothing is read back when no attempt may have been stored")
}

func TestReportJobOutcome_A409AfterAGatewayErrorSucceedsWhenTheStoredOutcomeIsThisOne(t *testing.T) {
	script, client := newScriptedServer(t,
		answer(http.StatusBadGateway, "bad gateway"),
		answer(http.StatusConflict, platformAlreadyReported),
		answer(http.StatusOK, storedJob))

	job, err := client.ReportJobOutcome("loan-77", failedLoan())

	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "j1", job.ID)
	requests := script.recorded()
	require.Len(t, requests, 3)
	assert.Equal(t, http.MethodGet, requests[2].method)
	assert.Equal(t, "/profitstream/v2/api/jobs/loan-77", requests[2].path)
}

func TestReportJobOutcome_A409AfterAGatewayErrorIsAConflictWhenTheStoredOutcomeDiffers(t *testing.T) {
	stored := map[string]string{
		"another status":  `{"hasOutcome":true,"executionStatus":"SUCCESS","outcomeType":"UNSUCCESSFUL","outcomeReason":"Customer unreachable","outcomeUpdateCount":0}`,
		"another reason":  `{"hasOutcome":true,"executionStatus":"FAILED","outcomeType":"UNSUCCESSFUL","outcomeReason":"Timeout","outcomeUpdateCount":0}`,
		"an extra value":  `{"hasOutcome":true,"executionStatus":"FAILED","outcomeType":"UNSUCCESSFUL","outcomeReason":"Customer unreachable","outcomeValue":5,"outcomeUpdateCount":0}`,
		"amended since":   `{"hasOutcome":true,"executionStatus":"FAILED","outcomeType":"UNSUCCESSFUL","outcomeReason":"Customer unreachable","outcomeUpdateCount":1}`,
		"unreadable read": `not json`,
	}
	for name, job := range stored {
		t.Run(name, func(t *testing.T) {
			_, client := newScriptedServer(t,
				answer(http.StatusServiceUnavailable, "unavailable"),
				answer(http.StatusConflict, platformAlreadyReported),
				answer(http.StatusOK, job))

			result, err := client.ReportJobOutcome("loan-77", failedLoan())

			assert.Nil(t, result)
			var reported *OutcomeAlreadyReportedError
			require.ErrorAs(t, err, &reported)
		})
	}
}

func TestReportJobOutcome_A409AfterAGatewayErrorStaysAConflictWhenMetricsWereSent(t *testing.T) {
	script, client := newScriptedServer(t,
		answer(http.StatusBadGateway, "bad gateway"),
		answer(http.StatusConflict, platformAlreadyReported),
		answer(http.StatusOK, storedJob))
	outcome := failedLoan()
	outcome.Metrics = []OutcomeMetric{{Key: "quality_rate", Value: 0.93}}

	result, err := client.ReportJobOutcome("loan-77", outcome)

	assert.Nil(t, result)
	var reported *OutcomeAlreadyReportedError
	require.ErrorAs(t, err, &reported, "Revenium cannot show whether the metrics were stored, so the conflict stands")
	assert.Len(t, script.recorded(), 2)
}

func TestReportJobOutcome_A409AfterA429IsAlreadyReported(t *testing.T) {
	script, client := newScriptedServer(t,
		answer(http.StatusTooManyRequests, ""),
		answer(http.StatusConflict, platformAlreadyReported),
		answer(http.StatusOK, storedJob))

	_, err := client.ReportJobOutcome("loan-77", failedLoan())

	var reported *OutcomeAlreadyReportedError
	require.ErrorAs(t, err, &reported, "a 429 proves the first attempt was not stored")
	assert.Len(t, script.recorded(), 2)
}

func TestGetJobOutcomeHistory_ReadsThePlatformsHistoryRows(t *testing.T) {
	_, client := newScriptedServer(t, answer(http.StatusOK, `[`+
		`{"sequence":1,"executionStatus":"FAILED","outcomeReason":"Customer unreachable","reportedAt":"2026-10-13T10:00:00Z"},`+
		`{"sequence":2,"executionStatus":"SUCCESS","reason":"late correction","reportedAt":"2026-10-13T11:00:00Z"}]`))

	history, err := client.GetJobOutcomeHistory("loan-77")

	require.NoError(t, err)
	require.Len(t, history, 2)
	assert.Equal(t, 1, history[0].AmendmentSequence)
	require.NotNil(t, history[0].OutcomeReason)
	assert.Equal(t, "Customer unreachable", *history[0].OutcomeReason)
	assert.Equal(t, 2, history[1].AmendmentSequence)
	require.NotNil(t, history[1].Reason)
	assert.Equal(t, "late correction", *history[1].Reason)
}

func storedOutcome(fields map[string]any) *JobResource {
	job := map[string]any{"hasOutcome": true, "outcomeUpdateCount": 0, "executionStatus": "SUCCESS"}
	for key, value := range fields {
		job[key] = value
	}
	raw, _ := json.Marshal(job)
	var resource JobResource
	_ = json.Unmarshal(raw, &resource)
	return &resource
}

func ptr[T any](v T) *T { return &v }

func TestStoresReportUnamended_FollowsHowReveniumStoresEachField(t *testing.T) {
	cases := []struct {
		name   string
		sent   JobOutcome
		stored map[string]any
		match  bool
	}{
		{"metadata compared as JSON", JobOutcome{Metadata: `{"a":1,"b":2}`}, map[string]any{"outcomeMetadata": `{"b":2, "a":1}`}, true},
		{"different metadata", JobOutcome{Metadata: `{"a":1}`}, map[string]any{"outcomeMetadata": `{"a":2}`}, false},
		{"metadata stored but not sent", JobOutcome{}, map[string]any{"outcomeMetadata": `{"a":1}`}, false},
		{"status and type in another case", JobOutcome{ExecutionStatus: "success", OutcomeType: "converted"}, map[string]any{"outcomeType": "CONVERTED"}, true},
		{"type stored but not sent", JobOutcome{}, map[string]any{"outcomeType": "CONVERTED"}, false},
		{"blank reason is stored as none", JobOutcome{OutcomeReason: "   "}, map[string]any{}, true},
		{"reason must match exactly", JobOutcome{OutcomeReason: "Timeout"}, map[string]any{"outcomeReason": "timeout"}, false},
		{"value rounded to ten places", JobOutcome{OutcomeValue: ptr(0.123456789012), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": 0.1234567890, "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, true},
		{"value with trailing zeros", JobOutcome{OutcomeValue: ptr(150.0), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": json.Number("150.0000000000"), "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, true},
		{"different value", JobOutcome{OutcomeValue: ptr(150.0), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": 150.01, "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, false},
		{"large value differing within the tenth decimal", JobOutcome{OutcomeValue: ptr(1000000000000.0), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": 1000000000000.0005, "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, false},
		{"half-way value rounds away from zero", JobOutcome{OutcomeValue: ptr(2.00000000025), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": json.Number("2.0000000003"), "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, true},
		{"half-way value is not rounded to even", JobOutcome{OutcomeValue: ptr(2.00000000025), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": json.Number("2.0000000002"), "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, false},
		{"negative half-way value rounds away from zero", JobOutcome{OutcomeValue: ptr(-2.00000000025), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": json.Number("-2.0000000003"), "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, true},
		{"negative half-way value is not rounded towards zero", JobOutcome{OutcomeValue: ptr(-2.00000000025), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": json.Number("-2.0000000002"), "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, false},
		{"half-way decimal rounds up even where the binary value lies below it", JobOutcome{OutcomeValue: ptr(0.00000000015), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": json.Number("0.0000000002"), "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, true},
		{"negative half-way decimal where the binary value lies below it", JobOutcome{OutcomeValue: ptr(-0.00000000015), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": json.Number("-0.0000000002"), "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, true},
		{"stored value a float64 cannot hold at ten places", JobOutcome{OutcomeValue: ptr(1000000000000.0), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": json.Number("1000000000000.0000000001"), "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, false},
		{"stored value with every decimal place", JobOutcome{OutcomeValue: ptr(1000000000000.0), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": json.Number("1000000000000.0000000000"), "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, true},
		{"large metadata integers compared exactly", JobOutcome{Metadata: `{"id":9007199254740993}`}, map[string]any{"outcomeMetadata": `{"id":9007199254740992}`}, false},
		{"sent metadata with trailing data", JobOutcome{Metadata: `{"id":1}}`}, map[string]any{"outcomeMetadata": `{"id":1}`}, false},
		{"equal large metadata integers in another key order", JobOutcome{Metadata: `{"id":9007199254740993,"n":[1,2.50]}`}, map[string]any{"outcomeMetadata": `{"n":[1,2.5],"id":9007199254740993}`}, true},
		{"currency defaults to USD with a value", JobOutcome{OutcomeValue: ptr(5.0), OutcomeType: OutcomeConverted}, map[string]any{"outcomeValue": 5, "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, true},
		{"currency is dropped without a value", JobOutcome{OutcomeCurrency: "EUR"}, map[string]any{}, true},
		{"different currency", JobOutcome{OutcomeValue: ptr(5.0), OutcomeType: OutcomeConverted, OutcomeCurrency: "EUR"}, map[string]any{"outcomeValue": 5, "outcomeType": "CONVERTED", "outcomeCurrency": "USD"}, false},
		{"amended since", JobOutcome{}, map[string]any{"outcomeUpdateCount": 1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sent := tc.sent
			if sent.ExecutionStatus == "" {
				sent.ExecutionStatus = ExecutionStatusSuccess
			}

			assert.Equal(t, tc.match, storesReportUnamended(storedOutcome(tc.stored), &sent))
		})
	}
}

func TestReportJobOutcome_ConfirmsAnExplicitReporterAgainstTheHistory(t *testing.T) {
	history := func(rows string) func(w http.ResponseWriter) { return answer(http.StatusOK, rows) }
	cases := []struct {
		name    string
		history string
		success bool
	}{
		{"same reporter", `[{"sequence":1,"executionStatus":"FAILED","reportedBy":"loan-agent","reportedAt":"t"}]`, true},
		{"another reporter", `[{"sequence":1,"executionStatus":"FAILED","reportedBy":"someone-else","reportedAt":"t"}]`, false},
		{"amended since", `[{"sequence":1,"reportedBy":"loan-agent","reportedAt":"t"},{"sequence":2,"reportedAt":"t"}]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			script, client := newScriptedServer(t,
				answer(http.StatusBadGateway, "bad gateway"),
				answer(http.StatusConflict, platformAlreadyReported),
				answer(http.StatusOK, storedJob),
				history(tc.history))
			outcome := failedLoan()
			outcome.ReportedBy = "loan-agent"

			job, err := client.ReportJobOutcome("loan-77", outcome)

			requests := script.recorded()
			require.Len(t, requests, 4)
			assert.Equal(t, "/profitstream/v2/api/jobs/loan-77/outcome/history", requests[3].path)
			if tc.success {
				require.NoError(t, err)
				assert.NotNil(t, job)
				return
			}
			var reported *OutcomeAlreadyReportedError
			require.ErrorAs(t, err, &reported)
		})
	}
}

func TestReportJobOutcome_ReadsTheJobBackThroughTheSharedRetryAndBreaker(t *testing.T) {
	script, client := newScriptedServer(t,
		answer(http.StatusBadGateway, "bad gateway"),
		answer(http.StatusConflict, platformAlreadyReported),
		answer(http.StatusServiceUnavailable, "unavailable"),
		answer(http.StatusOK, storedJob))

	job, err := client.ReportJobOutcome("loan-77", failedLoan())

	require.NoError(t, err)
	assert.NotNil(t, job)
	assert.Len(t, script.recorded(), 4, "the read-back is retried like any other job read")
}

func withShortRequestTimeout(t *testing.T) {
	t.Helper()
	original := sharedHTTPClient
	sharedHTTPClient = core.NewReveniumHTTPClient(200 * time.Millisecond)
	t.Cleanup(func() { sharedHTTPClient = original })
}

func neverAnswer(t *testing.T) func(w http.ResponseWriter) {
	t.Helper()
	var mu sync.Mutex
	var held []net.Conn
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range held {
			_ = conn.Close()
		}
	})
	return func(w http.ResponseWriter) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		mu.Lock()
		held = append(held, conn)
		mu.Unlock()
	}
}

func TestReportJobOutcome_ATimeoutIsSentOnceMoreAndConfirmedByReadingTheJobBack(t *testing.T) {
	withShortRequestTimeout(t)
	script, client := newScriptedServer(t,
		neverAnswer(t),
		answer(http.StatusConflict, platformAlreadyReported),
		answer(http.StatusOK, storedJob))

	job, err := client.ReportJobOutcome("loan-77", failedLoan())

	require.NoError(t, err)
	require.NotNil(t, job)
	requests := script.recorded()
	require.Len(t, requests, 3)
	assert.Equal(t, http.MethodGet, requests[2].method)
}

func TestReportJobOutcome_IsSentOnlyOnceMoreAfterTimeouts(t *testing.T) {
	withShortRequestTimeout(t)
	script, client := newScriptedServer(t, neverAnswer(t))

	_, err := client.ReportJobOutcome("loan-77", failedLoan())

	var timeout net.Error
	require.ErrorAs(t, err, &timeout, "the caller still gets the timeout itself, got %v", err)
	assert.True(t, timeout.Timeout())
	assert.Len(t, script.recorded(), 2, "a timed-out report is sent once more, never again, on every Go release")
}

func TestWritesThatAreNotSafeToRepeat_AreNotResentAfterATimeout(t *testing.T) {
	for name, write := range writesNotSafeToRepeat {
		t.Run(name, func(t *testing.T) {
			withShortRequestTimeout(t)
			script, client := newScriptedServer(t, neverAnswer(t))

			require.Error(t, write(client))
			assert.Len(t, script.recorded(), 1)
		})
	}
}

var reads = map[string]func(*JobClient) error{
	"ListJobs": func(c *JobClient) error {
		_, err := c.ListJobs(nil)
		return err
	},
	"GetJob": func(c *JobClient) error {
		_, err := c.GetJob("loan-77")
		return err
	},
	"GetJobTypes": func(c *JobClient) error {
		_, err := c.GetJobTypes()
		return err
	},
	"GetJobROI": func(c *JobClient) error {
		_, err := c.GetJobROI("loan-77")
		return err
	},
	"GetJobTransactions": func(c *JobClient) error {
		_, err := c.GetJobTransactions("loan-77")
		return err
	},
	"GetConversionFunnel": func(c *JobClient) error {
		_, err := c.GetConversionFunnel(nil)
		return err
	},
	"GetJobOutcomeHistory": func(c *JobClient) error {
		_, err := c.GetJobOutcomeHistory("loan-77")
		return err
	},
}

func TestReads_AreRetriedTheConfiguredNumberOfTimesAfterAClientTimeout(t *testing.T) {
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			withShortRequestTimeout(t)
			script, client := newScriptedServer(t, neverAnswer(t))
			client.retryConfig.MaxRetries = 2

			err := read(client)

			var timeout net.Error
			require.ErrorAs(t, err, &timeout, "the caller still gets the timeout itself, got %v", err)
			assert.True(t, timeout.Timeout())
			assert.Len(t, script.recorded(), 3, "one request and two retries, on every Go release")
		})
	}
}
