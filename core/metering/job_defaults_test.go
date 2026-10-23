package metering

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearJobDefaults(t *testing.T) {
	t.Helper()
	clearRecordDefaults(t)
	for _, name := range []string{core.EnvTicketID, core.EnvAgenticJobID, core.EnvAgenticJobName, core.EnvAgenticJobType, core.EnvAgenticJobVersion} {
		t.Setenv(name, "")
	}
}

func TestMeteringClient_RecordsCarryTheJobAndTicketFromTheEnvironment(t *testing.T) {
	clearJobDefaults(t)
	t.Setenv(core.EnvAgenticJobID, "invoice-batch-7")
	t.Setenv(core.EnvAgenticJobName, "Invoice batch")
	t.Setenv(core.EnvAgenticJobType, "batch")
	t.Setenv(core.EnvAgenticJobVersion, "v2")
	t.Setenv(core.EnvTicketID, "ENG-42")

	record := recordSentBy(t, MeteringClientConfig{}, nil)

	assert.Equal(t, map[string]interface{}{
		"agenticJobId":      "invoice-batch-7",
		"agenticJobName":    "Invoice batch",
		"agenticJobType":    "batch",
		"agenticJobVersion": "v2",
		"ticketId":          "ENG-42",
	}, pick(record, "agenticJobId", "agenticJobName", "agenticJobType", "agenticJobVersion", "ticketId"))
}

func TestMeteringClient_CallsOwnJobAndTicketWinOverTheEnvironment(t *testing.T) {
	clearJobDefaults(t)
	t.Setenv(core.EnvAgenticJobID, "invoice-batch-7")
	t.Setenv(core.EnvTicketID, "ENG-42")

	record := recordSentBy(t, MeteringClientConfig{}, map[string]interface{}{"agenticJobId": "job-2", "ticketId": "ENG-7"})

	assert.Equal(t, "job-2", record["agenticJobId"])
	assert.Equal(t, "ENG-7", record["ticketId"])
}

func TestMeteringClient_AnEnvironmentJobIDThePlatformWouldRefuseIsLeftOff(t *testing.T) {
	clearJobDefaults(t)
	t.Setenv(core.EnvAgenticJobID, "order:42")

	record := recordSentBy(t, MeteringClientConfig{}, nil)

	assert.NotContains(t, record, "agenticJobId")
}

func TestMeteringClient_ToolEventsCarryTheJobFromTheEnvironment(t *testing.T) {
	clearJobDefaults(t)
	t.Setenv(core.EnvAgenticJobID, "invoice-batch-7")
	var event map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&event))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	require.NoError(t, newTestClient(t, srv.URL).SendToolEventSync(NewToolEvent("web_search").Build()))

	assert.Equal(t, "invoice-batch-7", event["agenticJobId"])
}
