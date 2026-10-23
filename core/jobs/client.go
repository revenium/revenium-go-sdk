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
	"strings"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
)

const (
	jobsBasePath     = "/profitstream/v2/api/jobs"
	writeKeyGuidance = "job and outcome calls need a write key (rev_sk_); Revenium always refuses a metering key (rev_mk_) for them. " +
		"Keep the metering key in REVENIUM_METERING_API_KEY for usage records and give the job client a separate write key, for example from REVENIUM_WRITE_API_KEY"
)

var sharedHTTPClient = newJobHTTPClient()

// newJobHTTPClient builds the client for job and outcome calls with a fixed
// 10-second timeout. REVENIUM_API_TIMEOUT covers usage records and tool events
// only, so this client must not follow it.
func newJobHTTPClient() *http.Client {
	return core.NewReveniumHTTPClient(10 * time.Second)
}

type JobClientConfig struct {
	APIKey  string
	BaseURL string
	TeamID  string
}

type JobClient struct {
	config         JobClientConfig
	retryConfig    *resilience.RetryConfig
	breaker        *resilience.CircuitBreaker
	releaseBreaker func()
}

// NewJobClient builds a client for the job and outcome API. It needs a write
// key (rev_sk_): Revenium refuses a metering key (rev_mk_) on every job call,
// so one is rejected here instead of failing each call later.
func NewJobClient(cfg JobClientConfig) (*JobClient, error) {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.TeamID = strings.TrimSpace(cfg.TeamID)
	if cfg.APIKey == "" {
		return nil, core.NewConfigError("a write API key (rev_sk_) is required for job and outcome calls", nil)
	}
	if err := core.ValidateAPIKeyCharactersFor("JobClientConfig.APIKey", cfg.APIKey); err != nil {
		return nil, err
	}
	if !core.IsValidAPIKeyFormat(cfg.APIKey) {
		return nil, core.NewConfigError("JobClientConfig.APIKey must be a Revenium write key starting with 'rev_sk_' (older keys start with 'hak_')", nil)
	}
	if core.IsMeteringAPIKey(cfg.APIKey) {
		return nil, core.NewConfigError(writeKeyGuidance, nil)
	}
	baseURL, err := core.ResolveValidReveniumBaseURL(cfg.BaseURL, "JobClientConfig.BaseURL")
	if err != nil {
		return nil, err
	}
	cfg.BaseURL = baseURL
	if cfg.TeamID == "" {
		return nil, core.NewConfigError("team ID is required for job operations", nil)
	}
	breaker, release := resilience.AcquireCircuitBreaker(resilience.JobsEndpoint, cfg.BaseURL, cfg.APIKey)
	return &JobClient{config: cfg, breaker: breaker, releaseBreaker: release}, nil
}

// Close lets the client's Revenium key forget its outage and Retry-After
// state once no other client uses the key. Call it when the client is no
// longer needed; calls made after Close still work.
func (c *JobClient) Close() {
	c.releaseBreaker()
}

// ReportJobOutcome records a job's final outcome. A job has one outcome:
// a second report returns OutcomeAlreadyReportedError; change it with
// AmendJobOutcome. A report that times out is sent once more, because a
// report is safe to repeat: Revenium refuses a second one. When an earlier
// attempt of this same call failed in a way that may still have stored it,
// such as a timeout or a 5xx, the job is read back once and the call
// succeeds if Revenium holds exactly this outcome, unamended. An outcome with
// Metrics is never confirmed that way, because Revenium does not return them.
func (c *JobClient) ReportJobOutcome(jobID string, outcome *JobOutcome) (*JobResource, error) {
	if outcome == nil {
		return nil, core.NewValidationError("job outcome must not be nil", nil)
	}
	if jobID == "" {
		return nil, core.NewValidationError("job ID must not be empty", nil)
	}
	if err := validateMetrics(outcome.Metrics); err != nil {
		return nil, err
	}

	path := outcomePath(jobID)
	var result JobResource
	var attempts []error
	timeouts := &timeoutResendPolicy{}
	err := c.doRetrying(timeouts, func() error {
		attempt := c.doRequestWithBody(http.MethodPost, path, nil, outcome, &result)
		attempts = append(attempts, attempt)
		timeouts.record(attempt)
		return attempt
	})
	if isConflictError(err) {
		attemptsBeforeTheConflict := attempts[:len(attempts)-1]
		if anyMayHaveBeenApplied(attemptsBeforeTheConflict) {
			if stored, ok := c.jobStoringReport(jobID, outcome); ok {
				return stored, nil
			}
		}
		return nil, alreadyReportedError(jobID, err)
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// AppendOutcomeMetrics adds per-job metric values to a job after its outcome
// was reported, for values that arrive later. Each key must be declared on the
// job type with resolution PER_JOB. A value for a key the job already has
// replaces it and needs a Reason. It is retried only when Revenium answers
// 429, because a 5xx or a lost connection can arrive after the values were
// stored. The SDK cannot read per-job metrics back, so to resend after such
// an error give every entry a Reason: a stored key is then replaced, never
// counted twice.
func (c *JobClient) AppendOutcomeMetrics(jobID string, metrics []OutcomeMetric) error {
	if jobID == "" {
		return core.NewValidationError("job ID must not be empty", nil)
	}
	if len(metrics) == 0 {
		return core.NewValidationError("metrics must contain at least one entry to append", nil)
	}
	if err := validateMetrics(metrics); err != nil {
		return err
	}
	path := outcomePath(jobID) + "/metrics"
	return c.doWriteNotSafeToRepeat(func() error {
		return c.doRequestWithBody(http.MethodPost, path, nil, metrics, nil)
	})
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
	err := c.doReadSafeToRepeat(func() error {
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
	err := c.doReadSafeToRepeat(func() error {
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
	err := c.doReadSafeToRepeat(func() error {
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
	err := c.doReadSafeToRepeat(func() error {
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
	err := c.doReadSafeToRepeat(func() error {
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
	err := c.doReadSafeToRepeat(func() error {
		return c.doRequest(http.MethodGet, path, query, &result)
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// AmendJobOutcome changes a reported outcome. Set ExpectedEntityVersion to
// guard against overwriting a change you have not seen: a stale version
// returns OutcomeAmendConflictError carrying the version Revenium holds. An
// amendment is not safe to repeat, so it is retried only when Revenium answers
// 429. After a 5xx or a lost connection, GetJobOutcomeHistory shows whether
// it was stored: a stored amendment is the latest entry, with your Reason and
// outcome fields.
func (c *JobClient) AmendJobOutcome(jobID string, amendment *JobOutcomeAmendment) (*JobResource, error) {
	if jobID == "" {
		return nil, core.NewValidationError("job ID must not be empty", nil)
	}
	if amendment == nil {
		return nil, core.NewValidationError("amendment must not be nil", nil)
	}
	if strings.TrimSpace(amendment.Reason) == "" {
		return nil, core.NewValidationError("amendment.Reason is required and must be non-blank", nil)
	}
	if amendment.ExpectedEntityVersion != nil && *amendment.ExpectedEntityVersion < 0 {
		return nil, core.NewValidationError("amendment.ExpectedEntityVersion must be zero or greater", nil)
	}
	if err := validateMetrics(amendment.Metrics); err != nil {
		return nil, err
	}

	var result JobResource
	err := c.doWriteNotSafeToRepeat(func() error {
		return c.doRequestWithBody(http.MethodPatch, outcomePath(jobID), nil, amendment, &result)
	})
	if err != nil {
		if isUnprocessableError(err) {
			return nil, &OutcomeNotReportedError{
				JobID:   jobID,
				Message: fmt.Sprintf("job %s has no outcome to amend", jobID),
			}
		}
		if isConflictError(err) {
			return nil, amendConflictError(jobID, err)
		}
		return nil, err
	}
	return &result, nil
}

func (c *JobClient) GetJobOutcomeHistory(jobID string) ([]JobOutcomeAmendmentEntry, error) {
	if jobID == "" {
		return nil, core.NewValidationError("job ID must not be empty", nil)
	}

	path := fmt.Sprintf("%s/%s/outcome/history", jobsBasePath, url.PathEscape(jobID))
	var result []JobOutcomeAmendmentEntry
	err := c.doReadSafeToRepeat(func() error {
		return c.doRequest(http.MethodGet, path, nil, &result)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func outcomePath(jobID string) string {
	return fmt.Sprintf("%s/%s/outcome", jobsBasePath, url.PathEscape(jobID))
}

// doReadSafeToRepeat runs a read, which is safe to repeat, so it is also
// retried after a timeout the shared classifier refuses, such as an
// http.Client timeout, and keeps the same retries on every Go version.
func (c *JobClient) doReadSafeToRepeat(fn func() error) error {
	config := c.callRetryConfig()
	config.RetryAlso = timedOutWaitingForRevenium
	return resilience.RetryThroughBreaker(c.breaker, nil, config, fn)
}

func (c *JobClient) doWriteNotSafeToRepeat(fn func() error) error {
	config := c.callRetryConfig()
	config.RetryOnly = refusedBeforeApplying
	return resilience.RetryThroughBreaker(c.breaker, nil, config, fn)
}

func (c *JobClient) doRetrying(policy *timeoutResendPolicy, fn func() error) error {
	config := c.callRetryConfig()
	config.RetryOnly = policy.allowsRetry
	config.RetryAlso = policy.widensRetry
	return resilience.RetryThroughBreaker(c.breaker, nil, config, fn)
}

func (c *JobClient) callRetryConfig() *resilience.RetryConfig {
	config := resilience.DefaultRetryConfig()
	if c.retryConfig != nil {
		*config = *c.retryConfig
	}
	return config
}

func (c *JobClient) buildURL(path string, extra url.Values) string {
	query := url.Values{}
	query.Set("teamId", c.config.TeamID)
	for k, vs := range extra {
		for _, v := range vs {
			query.Add(k, v)
		}
	}

	return c.config.BaseURL + path + "?" + query.Encode()
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
		revErr := resilience.ResponseError("job", resp.StatusCode, resp.Header, body)
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return asKeyRefusal(revErr)
		}
		if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusUnprocessableEntity {
			revErr = revErr.WithDetails("responseBody", string(body))
		}
		return revErr
	}

	if out != nil && len(body) > 0 {
		if jsonErr := json.Unmarshal(body, out); jsonErr != nil {
			return core.NewMeteringError("failed to parse job API response", jsonErr)
		}
	}

	core.Debug("[JOBS] Request completed successfully")
	return nil
}

func asKeyRefusal(mapped *core.ReveniumError) *core.ReveniumError {
	refusal := core.NewAuthError(fmt.Sprintf("%s. Check that the key is a valid write key for this team: %s", mapped.Message, writeKeyGuidance), mapped.Err)
	refusal.StatusCode = mapped.StatusCode
	refusal.RetryAfter = mapped.RetryAfter
	refusal.Details = mapped.Details
	return refusal
}

func isConflictError(err error) bool {
	var revErr *core.ReveniumError
	return errors.As(err, &revErr) && revErr.StatusCode == http.StatusConflict
}

func isUnprocessableError(err error) bool {
	var revErr *core.ReveniumError
	return errors.As(err, &revErr) && revErr.StatusCode == http.StatusUnprocessableEntity
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
