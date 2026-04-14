package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
)

const jobsBasePath = "/profitstream/v2/api/jobs"

var sharedHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
	},
}

type JobClientConfig struct {
	APIKey  string
	BaseURL string
	TeamID  string
}

type JobClient struct {
	config      JobClientConfig
	retryConfig *resilience.RetryConfig
}

func NewJobClient(cfg JobClientConfig) (*JobClient, error) {
	if cfg.APIKey == "" {
		return nil, core.NewConfigError("metering API key is required", nil)
	}
	if cfg.TeamID == "" {
		return nil, core.NewConfigError("team ID is required for job operations", nil)
	}
	return &JobClient{config: cfg}, nil
}

func (c *JobClient) ReportJobOutcome(jobID string, outcome *JobOutcome) (*JobResource, error) {
	if outcome == nil {
		return nil, core.NewValidationError("job outcome must not be nil", nil)
	}
	if jobID == "" {
		return nil, core.NewValidationError("job ID must not be empty", nil)
	}

	path := fmt.Sprintf("%s/%s/outcome", jobsBasePath, url.PathEscape(jobID))
	var result JobResource
	err := c.doWithCircuitBreaker(func() error {
		return c.doRequestWithBody(http.MethodPost, path, nil, outcome, &result)
	})
	if err != nil {
		if isConflictError(err) {
			core.Debug("[JOBS] 409 Conflict - outcome already reported")
			return nil, nil
		}
		return nil, err
	}
	return &result, nil
}

func (c *JobClient) ListJobs(params *ListJobsParams) (*PagedResponse[JobResource], error) {
	query := url.Values{}
	if params != nil {
		if params.Type != "" {
			query.Set("type", params.Type)
		}
		if params.ExecutionStatus != "" {
			query.Set("executionStatus", string(params.ExecutionStatus))
		}
		if params.OutcomeType != "" {
			query.Set("outcomeType", string(params.OutcomeType))
		}
		if params.StartDate != "" {
			query.Set("startDate", params.StartDate)
		}
		if params.EndDate != "" {
			query.Set("endDate", params.EndDate)
		}
		if params.Page != nil {
			query.Set("page", strconv.Itoa(*params.Page))
		}
		if params.Size != nil {
			query.Set("size", strconv.Itoa(*params.Size))
		}
		if params.Sort != "" {
			query.Set("sort", params.Sort)
		}
	}

	var raw json.RawMessage
	err := c.doWithCircuitBreaker(func() error {
		return c.doRequest(http.MethodGet, jobsBasePath, query, &raw)
	})
	if err != nil {
		return nil, err
	}
	return normalizePagedResponse[JobResource](raw)
}

func (c *JobClient) GetJob(jobID string) (*JobResource, error) {
	if jobID == "" {
		return nil, core.NewValidationError("job ID must not be empty", nil)
	}

	path := fmt.Sprintf("%s/%s", jobsBasePath, url.PathEscape(jobID))
	var result JobResource
	err := c.doWithCircuitBreaker(func() error {
		return c.doRequest(http.MethodGet, path, nil, &result)
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *JobClient) GetJobTypes() ([]string, error) {
	path := jobsBasePath + "/types"
	var result []string
	err := c.doWithCircuitBreaker(func() error {
		return c.doRequest(http.MethodGet, path, nil, &result)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *JobClient) GetJobROI(jobID string) (*JobROIResource, error) {
	if jobID == "" {
		return nil, core.NewValidationError("job ID must not be empty", nil)
	}

	path := fmt.Sprintf("%s/%s/roi", jobsBasePath, url.PathEscape(jobID))
	var result JobROIResource
	err := c.doWithCircuitBreaker(func() error {
		return c.doRequest(http.MethodGet, path, nil, &result)
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *JobClient) GetJobTransactions(jobID string) (*JobTimelineResource, error) {
	if jobID == "" {
		return nil, core.NewValidationError("job ID must not be empty", nil)
	}

	path := fmt.Sprintf("%s/%s/transactions", jobsBasePath, url.PathEscape(jobID))
	var result JobTimelineResource
	err := c.doWithCircuitBreaker(func() error {
		return c.doRequest(http.MethodGet, path, nil, &result)
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *JobClient) GetConversionFunnel(params *ConversionFunnelParams) (*ConversionFunnelResource, error) {
	query := url.Values{}
	if params != nil {
		if params.StartDate != "" {
			query.Set("startDate", params.StartDate)
		}
		if params.EndDate != "" {
			query.Set("endDate", params.EndDate)
		}
		if params.JobType != "" {
			query.Set("jobType", params.JobType)
		}
	}

	path := jobsBasePath + "/conversion-funnel"
	var result ConversionFunnelResource
	err := c.doWithCircuitBreaker(func() error {
		return c.doRequest(http.MethodGet, path, query, &result)
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *JobClient) doWithCircuitBreaker(fn func() error) error {
	cb := resilience.GetGlobalCircuitBreaker()
	retryConfig := c.retryConfig
	if retryConfig == nil {
		retryConfig = resilience.DefaultRetryConfig()
	}
	return cb.Execute(func() error {
		return resilience.WithRetry(context.Background(), fn, retryConfig)
	})
}

func (c *JobClient) buildURL(path string, extra url.Values) string {
	base := core.NormalizeReveniumBaseURL(c.config.BaseURL)
	if base == "" {
		base = core.DefaultReveniumBaseURL
	}

	query := url.Values{}
	query.Set("teamId", c.config.TeamID)
	for k, vs := range extra {
		for _, v := range vs {
			query.Add(k, v)
		}
	}

	return base + path + "?" + query.Encode()
}

func (c *JobClient) doRequest(method, path string, query url.Values, out interface{}) error {
	fullURL := c.buildURL(path, query)

	core.Debug("[JOBS] %s %s", method, fullURL)

	req, err := http.NewRequestWithContext(context.Background(), method, fullURL, nil)
	if err != nil {
		return core.NewMeteringError("failed to create job request", err)
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("x-api-key", c.config.APIKey)
	req.Header.Set("User-Agent", "revenium-go-sdk/1.0")

	return c.executeAndParse(req, out)
}

func (c *JobClient) doRequestWithBody(method, path string, query url.Values, body interface{}, out interface{}) error {
	fullURL := c.buildURL(path, query)

	jsonData, err := json.Marshal(body)
	if err != nil {
		return core.NewMeteringError("failed to marshal job request body", err)
	}

	core.Debug("[JOBS] %s %s: %s", method, fullURL, string(jsonData))

	req, err := http.NewRequestWithContext(context.Background(), method, fullURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return core.NewMeteringError("failed to create job request", err)
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("x-api-key", c.config.APIKey)
	req.Header.Set("User-Agent", "revenium-go-sdk/1.0")

	return c.executeAndParse(req, out)
}

func (c *JobClient) executeAndParse(req *http.Request, out interface{}) error {
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return core.NewNetworkError("job request failed", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := fmt.Sprintf("job API returned %d: %s", resp.StatusCode, string(body))
		classification := resilience.ClassifyHTTPResponse(resp.StatusCode, string(body))

		switch classification {
		case resilience.ClassificationThrottled:
			revErr := core.NewNetworkError(msg, nil)
			revErr.StatusCode = resp.StatusCode
			return revErr.WithDetails("throttled", true)
		case resilience.ClassificationRetryable:
			revErr := core.NewMeteringError(msg, nil)
			revErr.StatusCode = resp.StatusCode
			return revErr
		default:
			revErr := core.NewValidationError(msg, nil)
			revErr.StatusCode = resp.StatusCode
			return revErr
		}
	}

	if out != nil && len(body) > 0 {
		if jsonErr := json.Unmarshal(body, out); jsonErr != nil {
			return core.NewMeteringError("failed to parse job API response", jsonErr)
		}
	}

	core.Debug("[JOBS] Request completed successfully")
	return nil
}

func isConflictError(err error) bool {
	var revErr *core.ReveniumError
	return errors.As(err, &revErr) && revErr.StatusCode == http.StatusConflict
}

type halPagedResponse[T any] struct {
	Embedded struct {
		JobResourceList []T `json:"jobResourceList"`
	} `json:"_embedded"`
	Page PageInfo `json:"page"`
}

func normalizePagedResponse[T any](raw json.RawMessage) (*PagedResponse[T], error) {
	var standard PagedResponse[T]
	if err := json.Unmarshal(raw, &standard); err == nil && standard.Content != nil {
		return &standard, nil
	}

	var hal halPagedResponse[T]
	if err := json.Unmarshal(raw, &hal); err == nil && hal.Embedded.JobResourceList != nil {
		return &PagedResponse[T]{
			Content: hal.Embedded.JobResourceList,
			Page:    hal.Page,
		}, nil
	}

	return &PagedResponse[T]{
		Content: []T{},
		Page:    PageInfo{},
	}, nil
}
