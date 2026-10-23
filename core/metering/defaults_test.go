package metering

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var recordDefaultVariables = []string{
	core.EnvOrganizationName, core.EnvOrgID, core.EnvProductName, core.EnvProductID,
	core.EnvEnvironment, "ENVIRONMENT", "DEPLOYMENT_ENV",
	core.EnvRegion, "AWS_REGION", "AWS_DEFAULT_REGION", "AZURE_REGION", "GCP_REGION", "GOOGLE_CLOUD_REGION",
	core.EnvTraceType, core.EnvTraceName, core.EnvParentTransactionID, core.EnvTransactionName,
	core.EnvCredentialAlias, core.EnvRetryNumber,
}

func clearRecordDefaults(t *testing.T) {
	t.Helper()
	for _, name := range recordDefaultVariables {
		t.Setenv(name, "")
		require.NoError(t, os.Unsetenv(name))
	}
}

func recordSentBy(t *testing.T, cfg MeteringClientConfig, metadata map[string]interface{}) map[string]interface{} {
	t.Helper()
	var record map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&record))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	cfg.APIKey, cfg.BaseURL = "test-key", srv.URL
	client, err := NewMeteringClient(cfg)
	require.NoError(t, err)
	payload := NewPayload(OperationChat, "gpt-4.1-nano", "OPENAI").Build()
	ApplyMetadata(payload, metadata)
	require.NoError(t, client.SendSync(payload))
	return record
}

func TestMeteringClient_RecordsCarryTheEnvironmentDefaults(t *testing.T) {
	clearRecordDefaults(t)
	t.Setenv(core.EnvEnvironment, "production")
	t.Setenv(core.EnvTraceType, "checkout")
	t.Setenv(core.EnvOrganizationName, "acme")
	t.Setenv(core.EnvProductName, "storefront")
	t.Setenv(core.EnvRegion, "us-east-1")
	t.Setenv(core.EnvTraceName, "Checkout")
	t.Setenv(core.EnvParentTransactionID, "parent-1")
	t.Setenv(core.EnvTransactionName, "refund")
	t.Setenv(core.EnvRetryNumber, "1")
	t.Setenv(core.EnvCredentialAlias, "prod-key")

	record := recordSentBy(t, MeteringClientConfig{}, nil)

	assert.Equal(t, map[string]interface{}{
		"environment":         "production",
		"traceType":           "checkout",
		"organizationName":    "acme",
		"productName":         "storefront",
		"region":              "us-east-1",
		"traceName":           "Checkout",
		"parentTransactionId": "parent-1",
		"transactionName":     "refund",
		"retryNumber":         float64(1),
		"subscriber":          map[string]interface{}{"credential": map[string]interface{}{"name": "prod-key"}},
	}, pick(record, "environment", "traceType", "organizationName", "productName", "region", "traceName",
		"parentTransactionId", "transactionName", "retryNumber", "subscriber"))
}

func TestMeteringClient_CallMetadataWinsOverEnvironmentDefaults(t *testing.T) {
	clearRecordDefaults(t)
	t.Setenv(core.EnvEnvironment, "production")
	t.Setenv(core.EnvTraceType, "checkout")
	t.Setenv(core.EnvRetryNumber, "1")

	record := recordSentBy(t, MeteringClientConfig{}, map[string]interface{}{"environment": "staging", "retryNumber": 3})

	assert.Equal(t, "staging", record["environment"])
	assert.Equal(t, "checkout", record["traceType"])
	assert.Equal(t, float64(3), record["retryNumber"])
}

func TestMeteringClient_ConfiguredOrganizationAndProductWinOverTheEnvironment(t *testing.T) {
	clearRecordDefaults(t)
	t.Setenv(core.EnvOrganizationName, "from-env")
	t.Setenv(core.EnvProductName, "from-env")

	record := recordSentBy(t, MeteringClientConfig{OrganizationName: "acme", ProductName: "storefront"}, nil)

	assert.Equal(t, "acme", record["organizationName"])
	assert.Equal(t, "storefront", record["productName"])
}

func TestMeteringClient_RequestTimeoutComesFromTheEnvironment(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	defer close(release)
	t.Setenv(core.EnvAPITimeout, "50ms")
	client := newTestClient(t, srv.URL)
	client.retryConfig = &resilience.RetryConfig{MaxRetries: 0}

	err := client.SendSync(NewPayload(OperationChat, "gpt-4.1-nano", "OPENAI").Build())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "within 50ms")
}

func TestMeteringClient_DebugLoggingShowsTheOutgoingPayload(t *testing.T) {
	logger := useRecordingLogger(t)
	logger.SetLevel(core.LogLevelDebug)

	recordSentBy(t, MeteringClientConfig{}, map[string]interface{}{"agent": "support-bot"})

	var payloadLines []string
	for _, line := range logger.Lines() {
		if strings.Contains(line, "Sending payload") {
			payloadLines = append(payloadLines, line)
		}
	}
	require.Len(t, payloadLines, 1)
	assert.Contains(t, payloadLines[0], `"agent":"support-bot"`)
}

func TestMeteringClient_DefaultsReplaceAFieldSetToSpaces(t *testing.T) {
	clearRecordDefaults(t)
	t.Setenv(core.EnvEnvironment, "production")
	var record map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&record))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	payload := NewPayload(OperationChat, "gpt-4.1-nano", "OPENAI").Build()
	payload.Environment = "   "

	require.NoError(t, newTestClient(t, srv.URL).SendSync(payload))

	assert.Equal(t, "production", record["environment"])
}

func TestRecordDefaults_EveryEnvironmentDefaultIsAFieldTheClientFills(t *testing.T) {
	logger := useRecordingLogger(t)
	for _, name := range append(recordDefaultVariables, core.EnvTicketID, core.EnvAgenticJobID, core.EnvAgenticJobName, core.EnvAgenticJobType, core.EnvAgenticJobVersion) {
		t.Setenv(name, "1")
	}

	assert.Len(t, recordDefaults(MeteringClientConfig{}), len(core.RecordDefaults(&core.ReveniumConfig{OrgID: "acme", ProductID: "storefront"})))
	assert.Empty(t, logger.Lines())
}

func TestMeteringClient_AnEnvironmentJobIDThePlatformWouldRefuseIsReportedOnce(t *testing.T) {
	clearRecordDefaults(t)
	t.Setenv(core.EnvAgenticJobID, "order:42")
	logger := useRecordingLogger(t)
	logger.SetLevel(core.LogLevelDebug)
	client := newTestClient(t, unreachableBaseURL())

	client.prepareRecord(NewPayload(OperationChat, "gpt-4.1-nano", "OPENAI").Build())
	client.prepareRecord(NewPayload(OperationChat, "gpt-4.1-nano", "OPENAI").Build())

	var reports int
	for _, line := range logger.Lines() {
		if strings.Contains(line, "dropping agenticJobId") {
			reports++
		}
	}
	assert.Equal(t, 1, reports)
}

func TestMeteringClient_ShutdownWaitsAtLeastTheRequestTimeout(t *testing.T) {
	short, err := NewMeteringClient(MeteringClientConfig{APIKey: "test-key", BaseURL: unreachableBaseURL(), RequestTimeout: time.Second})
	require.NoError(t, err)
	t.Setenv(core.EnvAPITimeout, "30s")
	long, err := NewMeteringClient(MeteringClientConfig{APIKey: "test-key", BaseURL: unreachableBaseURL()})
	require.NoError(t, err)

	assert.Equal(t, defaultBufferFlushTimeout, short.buffer.flushTimeout)
	assert.Equal(t, 30*time.Second, long.buffer.flushTimeout)
}
