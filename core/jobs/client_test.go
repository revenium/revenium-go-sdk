package jobs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, serverURL string) *JobClient {
	t.Helper()
	client, err := NewJobClient(JobClientConfig{
		APIKey:  "hak_test_key",
		BaseURL: serverURL,
		TeamID:  "team-123",
	})
	require.NoError(t, err)
	return client
}

func TestNewJobClientRequiresAPIKey(t *testing.T) {
	_, err := NewJobClient(JobClientConfig{TeamID: "team-1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API key")
}

func TestNewJobClientRequiresTeamID(t *testing.T) {
	_, err := NewJobClient(JobClientConfig{APIKey: "hak_test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "team ID")
}

func TestNewJobClientSuccess(t *testing.T) {
	client, err := NewJobClient(JobClientConfig{
		APIKey: "hak_test",
		TeamID: "team-1",
	})
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestReportJobOutcomeSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/profitstream/v2/api/jobs/test-job/outcome")
		assert.Equal(t, "team-123", r.URL.Query().Get("teamId"))
		assert.Equal(t, "hak_test_key", r.Header.Get("x-api-key"))

		var body JobOutcome
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, ExecutionStatusSuccess, body.ExecutionStatus)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(JobResource{
			ID:           "JMwX9g4",
			AgenticJobID: "test-job",
			HasOutcome:   true,
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	result, err := client.ReportJobOutcome("test-job", &JobOutcome{
		ExecutionStatus: ExecutionStatusSuccess,
		OutcomeType:     OutcomeConverted,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "JMwX9g4", result.ID)
	assert.True(t, result.HasOutcome)
}

func TestReportJobOutcomeConflict409(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(JobResource{
			ID:           "existing",
			AgenticJobID: "test-job",
			HasOutcome:   true,
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	result, err := client.ReportJobOutcome("test-job", &JobOutcome{
		ExecutionStatus: ExecutionStatusSuccess,
	})

	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestReportJobOutcomeNilOutcome(t *testing.T) {
	client := newTestClient(t, "http://localhost")
	_, err := client.ReportJobOutcome("job-1", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must not be nil")
}

func TestReportJobOutcomeEmptyJobID(t *testing.T) {
	client := newTestClient(t, "http://localhost")
	_, err := client.ReportJobOutcome("", &JobOutcome{ExecutionStatus: ExecutionStatusSuccess})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestListJobsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/profitstream/v2/api/jobs")
		assert.Equal(t, "team-123", r.URL.Query().Get("teamId"))
		assert.Equal(t, "loan", r.URL.Query().Get("type"))

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(PagedResponse[JobResource]{
			Content: []JobResource{
				{ID: "1", AgenticJobID: "j1", HasOutcome: false},
				{ID: "2", AgenticJobID: "j2", HasOutcome: true},
			},
			Page: PageInfo{Size: 20, TotalElements: 2, TotalPages: 1, Number: 0},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	result, err := client.ListJobs(&ListJobsParams{Type: "loan"})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Content, 2)
	assert.Equal(t, "j1", result.Content[0].AgenticJobID)
	assert.Equal(t, 2, result.Page.TotalElements)
}

func TestListJobsHALFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"_embedded": {
				"jobResourceList": [
					{"id": "1", "agenticJobId": "j1", "hasOutcome": false}
				]
			},
			"page": {"size": 20, "totalElements": 1, "totalPages": 1, "number": 0}
		}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	result, err := client.ListJobs(nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Content, 1)
	assert.Equal(t, "j1", result.Content[0].AgenticJobID)
}

func TestListJobsWithPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.URL.Query().Get("page"))
		assert.Equal(t, "10", r.URL.Query().Get("size"))
		assert.Equal(t, "created,asc", r.URL.Query().Get("sort"))

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(PagedResponse[JobResource]{
			Content: []JobResource{},
			Page:    PageInfo{Size: 10, TotalElements: 15, TotalPages: 2, Number: 1},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	page := 1
	size := 10
	result, err := client.ListJobs(&ListJobsParams{
		Page: &page,
		Size: &size,
		Sort: "created,asc",
	})

	require.NoError(t, err)
	assert.Equal(t, 2, result.Page.TotalPages)
}

func TestGetJobSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/profitstream/v2/api/jobs/my-job-123")

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(JobResource{
			ID:           "abc",
			AgenticJobID: "my-job-123",
			Source:       "TELEMETRY",
			HasOutcome:   false,
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	result, err := client.GetJob("my-job-123")

	require.NoError(t, err)
	assert.Equal(t, "my-job-123", result.AgenticJobID)
	assert.Equal(t, "TELEMETRY", result.Source)
}

func TestGetJobEmptyID(t *testing.T) {
	client := newTestClient(t, "http://localhost")
	_, err := client.GetJob("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestGetJobTypesSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/profitstream/v2/api/jobs/types")

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]string{"document_processing", "loan_processing", "support"})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	result, err := client.GetJobTypes()

	require.NoError(t, err)
	assert.Len(t, result, 3)
	assert.Contains(t, result, "loan_processing")
}

func TestGetJobROISuccess(t *testing.T) {
	roi := 19900.0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/profitstream/v2/api/jobs/job-1/roi")

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(JobROIResource{
			AgenticJobID:     "job-1",
			TotalCost:        2.50,
			ROI:              &roi,
			HasOutcome:       true,
			TransactionCount: 15,
			TotalTokens:      8000,
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	result, err := client.GetJobROI("job-1")

	require.NoError(t, err)
	assert.Equal(t, 2.50, result.TotalCost)
	require.NotNil(t, result.ROI)
	assert.Equal(t, 19900.0, *result.ROI)
	assert.Equal(t, int64(15), result.TransactionCount)
}

func TestGetJobTransactionsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/profitstream/v2/api/jobs/job-1/transactions")

		agent := "DocExtractor"
		duration := int64(5000)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(JobTimelineResource{
			Transactions: []JobTimelineEvent{
				{
					TransactionID: "txn-001",
					Timestamp:     "2026-03-05T10:00:00Z",
					Agent:         &agent,
					Duration:      &duration,
				},
			},
			TotalCount: 1,
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	result, err := client.GetJobTransactions("job-1")

	require.NoError(t, err)
	assert.Equal(t, 1, result.TotalCount)
	require.Len(t, result.Transactions, 1)
	assert.Equal(t, "txn-001", result.Transactions[0].TransactionID)
}

func TestGetConversionFunnelSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/profitstream/v2/api/jobs/conversion-funnel")
		assert.Equal(t, "loan", r.URL.Query().Get("jobType"))
		assert.Equal(t, "2026-01-01T00:00:00Z", r.URL.Query().Get("startDate"))

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ConversionFunnelResource{
			TotalJobs:      100,
			SuccessfulJobs: 85,
			ConvertedJobs:  60,
			SuccessRate:    0.85,
			ConversionRate: 0.71,
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	result, err := client.GetConversionFunnel(&ConversionFunnelParams{
		StartDate: "2026-01-01T00:00:00Z",
		JobType:   "loan",
	})

	require.NoError(t, err)
	assert.Equal(t, 100, result.TotalJobs)
	assert.Equal(t, 0.85, result.SuccessRate)
	assert.Equal(t, 0.71, result.ConversionRate)
}

func TestClientSendsCorrectHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "hak_test_key", r.Header.Get("x-api-key"))
		assert.Equal(t, "application/json; charset=utf-8", r.Header.Get("Content-Type"))
		assert.Equal(t, "revenium-go-sdk/1.0", r.Header.Get("User-Agent"))

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]string{})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, _ = client.GetJobTypes()
}

func TestClientHandles404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "Job not found"}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.GetJob("nonexistent")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestClientHandles500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`Internal Server Error`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.GetJob("some-job")
	assert.Error(t, err)
}

func TestNormalizePagedResponseStandardFormat(t *testing.T) {
	raw := json.RawMessage(`{
		"content": [{"id": "1", "agenticJobId": "j1", "hasOutcome": false}],
		"page": {"size": 20, "totalElements": 1, "totalPages": 1, "number": 0}
	}`)

	result, err := normalizePagedResponse[JobResource](raw)
	require.NoError(t, err)
	assert.Len(t, result.Content, 1)
	assert.Equal(t, "j1", result.Content[0].AgenticJobID)
}

func TestNormalizePagedResponseHALFormat(t *testing.T) {
	raw := json.RawMessage(`{
		"_embedded": {"jobResourceList": [{"id": "1", "agenticJobId": "j1", "hasOutcome": true}]},
		"page": {"size": 10, "totalElements": 1, "totalPages": 1, "number": 0}
	}`)

	result, err := normalizePagedResponse[JobResource](raw)
	require.NoError(t, err)
	assert.Len(t, result.Content, 1)
	assert.True(t, result.Content[0].HasOutcome)
}

func TestNormalizePagedResponseEmptyResponse(t *testing.T) {
	raw := json.RawMessage(`{}`)

	result, err := normalizePagedResponse[JobResource](raw)
	require.NoError(t, err)
	assert.Empty(t, result.Content)
}

func TestJobIDURLEncoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.RequestURI, "job%20with%20spaces")

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(JobResource{
			ID:           "enc",
			AgenticJobID: "job with spaces",
		})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	result, err := client.GetJob("job with spaces")

	require.NoError(t, err)
	assert.Equal(t, "job with spaces", result.AgenticJobID)
}
