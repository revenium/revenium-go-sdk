package enforcement

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const largeTeamID = "large-team"

func perPersonCapWithBreakdown(people int, breachedEmail string) string {
	entries := make([]string, 0, people)
	for i := 0; i < people; i++ {
		email := fmt.Sprintf("developer%06d@enterprise-customer.example.com", i)
		entries = append(entries, fmt.Sprintf(
			`{"groupValue": %q, "displayName": %q, "currentValue": 12.5, "usagePercent": 0.25, "breached": false}`,
			email, email))
	}
	entries = append(entries, fmt.Sprintf(
		`{"groupValue": %q, "displayName": %q, "currentValue": 61, "usagePercent": 1.22, "breached": true}`,
		breachedEmail, breachedEmail))
	return fmt.Sprintf(`{"ruleId": 1, "name": "Per-person monthly cap", "metricType": "TOTAL_COST", "threshold": 50,
		"currentValue": 61, "periodType": "MONTHLY", "action": "BLOCK", "breached": true, "shadowMode": false,
		"groupBy": "SUBSCRIBER", "filters": [], "groupBreakdown": [%s]}`, strings.Join(entries, ","))
}

const breachedModelRule = `{"ruleId": 2, "name": "Block gpt-5", "metricType": "TOTAL_COST", "threshold": 0,
	"currentValue": 1, "periodType": "MONTHLY", "action": "BLOCK", "breached": true, "shadowMode": false,
	"filters": [{"dimension": "MODEL", "operator": "IS", "value": "gpt-5"}]}`

func largeRulesAnswer(people int, breachedEmail string) string {
	return fmt.Sprintf(`{"rules": [%s, %s]}`, perPersonCapWithBreakdown(people, breachedEmail), breachedModelRule)
}

func TestFreshEngineLoadsEveryRuleWhenThePerPersonListHolds30000People(t *testing.T) {
	answer := largeRulesAnswer(30000, "over-cap@example.com")
	require.Greater(t, len(answer), 1<<20, "the answer must be larger than the old 1 MiB read limit")

	rules := testutil.NewMockRulesServer()
	defer rules.Close()
	rules.SetPayload(largeTeamID, answer)

	resetEngines()
	defer Stop()
	e := Start(rules.URL(), "key", largeTeamID)

	var modelBlock *ErrCostLimitExceeded
	require.True(t, errors.As(e.Check(EvalContext{Model: "gpt-5", SubscriberEmail: "someone@example.com"}), &modelBlock),
		"the breached model rule must block once the large answer is loaded")
	assert.Equal(t, "2", modelBlock.RuleID)

	var personBlock *ErrCostLimitExceeded
	require.True(t, errors.As(e.Check(EvalContext{Model: "gpt-4.1-nano", SubscriberEmail: "over-cap@example.com"}), &personBlock),
		"the person over the per-person cap must be blocked")
	assert.Equal(t, "1", personBlock.RuleID)
	assert.Equal(t, 61.0, personBlock.CurrentValue)

	assert.NoError(t, e.Check(EvalContext{Model: "gpt-4.1-nano", SubscriberEmail: "developer000042@enterprise-customer.example.com"}))
}

func recordLogs(t *testing.T) *recordingLogger {
	t.Helper()
	logger := &recordingLogger{}
	previous := core.GetLogger()
	core.SetLogger(logger)
	t.Cleanup(func() { core.SetLogger(previous) })
	return logger
}

func startAgainst(t *testing.T, handler http.HandlerFunc) *Engine {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	resetEngines()
	t.Cleanup(Stop)
	return Start(srv.URL, "key", largeTeamID)
}

func TestLargeRulesAnswerIsRequestedCompressed(t *testing.T) {
	answer := []byte(largeRulesAnswer(30000, "over-cap@example.com"))
	var acceptEncoding atomic.Value
	var sent atomic.Int64
	e := startAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		acceptEncoding.Store(r.Header.Get("Accept-Encoding"))
		counter := &countingWriter{w: w, n: &sent}
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			_, _ = counter.Write(answer)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(counter)
		_, _ = zw.Write(answer)
		_ = zw.Close()
	})

	assert.Contains(t, acceptEncoding.Load(), "gzip")
	assert.Less(t, sent.Load(), int64(len(answer)/5), "the compressed answer must be a fraction of the plain one")
	assert.Error(t, e.Check(EvalContext{Model: "gpt-5"}), "the rules in the compressed answer must load")
}

type countingWriter struct {
	w io.Writer
	n *atomic.Int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	written, err := c.w.Write(p)
	c.n.Add(int64(written))
	return written, err
}

func TestUndecodableRulesAnswerIsReportedAtErrorLevel(t *testing.T) {
	logs := recordLogs(t)

	e := startAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"rules": [` + breachedModelRule))
	})

	assert.NoError(t, e.Check(EvalContext{Model: "gpt-5"}))
	assert.Contains(t, logs.errors(), "initial rule fetch failed")
	assert.Contains(t, logs.errors(), "decode rules")
	assert.NotContains(t, logs.warnings(), "decode rules")
}

func TestRulesAnswerCutOffMidBodyIsReportedAtErrorLevel(t *testing.T) {
	logs := recordLogs(t)

	startAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = w.Write([]byte(`{"rules": [`))
	})

	assert.Contains(t, logs.errors(), "read body")
}

func TestRefusedRuleFetchStaysAWarning(t *testing.T) {
	logs := recordLogs(t)

	startAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	assert.Contains(t, logs.warnings(), "unexpected status 403")
	assert.Empty(t, logs.errors())
}

func rulesResponse(body []byte, contentEncoding string) *http.Response {
	header := http.Header{}
	if contentEncoding != "" {
		header.Set("Content-Encoding", contentEncoding)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(bytes.NewReader(body))}
}

func gzipped(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(data)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func assertUnreadable(t *testing.T, set ruleSet, err error, reason string) {
	t.Helper()
	require.ErrorIs(t, err, errUnreadableRules)
	assert.Contains(t, err.Error(), reason)
	assert.Empty(t, set.rules, "no rules may load from an answer that could not be read")
}

func TestCompressedAnswerIsBoundedByItsDecodedSize(t *testing.T) {
	answer := []byte(largeRulesAnswer(2000, "over-cap@example.com"))
	compressed := gzipped(t, answer)
	limit := int64(len(answer) - 1)
	require.Less(t, int64(len(compressed)), limit, "the compressed body must fit the bound so only its decoded size can exceed it")

	set, err := readRuleSet(rulesResponse(compressed, "gzip"), limit)

	assertUnreadable(t, set, err, fmt.Sprintf("larger than %d bytes", limit))
}

func TestCompressedAnswerAtTheBoundLoads(t *testing.T) {
	answer := []byte(largeRulesAnswer(2000, "over-cap@example.com"))

	set, err := readRuleSet(rulesResponse(gzipped(t, answer), "gzip"), int64(len(answer)))

	require.NoError(t, err)
	assert.Len(t, set.rules, 2)
}

func TestCorruptCompressedAnswerIsUnreadable(t *testing.T) {
	compressed := gzipped(t, []byte(largeRulesAnswer(2000, "over-cap@example.com")))

	set, err := readRuleSet(rulesResponse(compressed[:len(compressed)/2], "gzip"), maxRulesAnswerBytes)

	assertUnreadable(t, set, err, "read body")
}

func TestAnswerLabelledGzipButSentPlainIsUnreadable(t *testing.T) {
	set, err := readRuleSet(rulesResponse([]byte(`{"rules": [`+breachedModelRule+`]}`), "gzip"), maxRulesAnswerBytes)

	assertUnreadable(t, set, err, "open gzip body")
}

func TestPlainAnswerOverTheBoundIsUnreadable(t *testing.T) {
	answer := []byte(`{"rules": [` + breachedModelRule + `]}`)
	limit := int64(len(answer) - 1)

	set, err := readRuleSet(rulesResponse(answer, ""), limit)

	assertUnreadable(t, set, err, fmt.Sprintf("larger than %d bytes", limit))
}

func TestPlainAnswerAtTheBoundLoads(t *testing.T) {
	answer := []byte(`{"rules": [` + breachedModelRule + `]}`)

	set, err := readRuleSet(rulesResponse(answer, ""), int64(len(answer)))

	require.NoError(t, err)
	assert.Len(t, set.rules, 1)
}
