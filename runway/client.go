package runway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

// RunwayClient is the HTTP client for interacting with Runway API
type RunwayClient struct {
	config     *Config
	httpClient *http.Client
}

// NewRunwayClient creates a new Runway API client
func NewRunwayClient(config *Config) *RunwayClient {
	timeout := config.RequestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}

	return &RunwayClient{
		config: config,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// CreateImageToVideo creates an image-to-video generation task
func (c *RunwayClient) CreateImageToVideo(ctx context.Context, req *ImageToVideoRequest) (*TaskResponse, error) {
	endpoint := "/v1/image_to_video"
	return c.createTask(ctx, endpoint, req)
}

// CreateVideoToVideo creates a video-to-video generation task
func (c *RunwayClient) CreateVideoToVideo(ctx context.Context, req *VideoToVideoRequest) (*TaskResponse, error) {
	endpoint := "/v1/video_to_video"
	return c.createTask(ctx, endpoint, req)
}

// CreateVideoUpscale creates a video upscaling task
func (c *RunwayClient) CreateVideoUpscale(ctx context.Context, req *VideoUpscaleRequest) (*TaskResponse, error) {
	endpoint := "/v1/video_upscale"
	return c.createTask(ctx, endpoint, req)
}

// GetTaskStatus retrieves the status of a task
func (c *RunwayClient) GetTaskStatus(ctx context.Context, taskID string) (*TaskStatusResponse, error) {
	endpoint := fmt.Sprintf("/v1/tasks/%s", taskID)

	req, err := c.newRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var response TaskStatusResponse
	if err := c.doRequest(req, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// WaitForTaskCompletion polls a task until it completes or times out
func (c *RunwayClient) WaitForTaskCompletion(ctx context.Context, taskID string, pollingConfig *PollingConfig) (*TaskStatusResponse, error) {
	if pollingConfig == nil {
		pollingConfig = DefaultPollingConfig()
	}

	startTime := time.Now()
	interval := pollingConfig.InitialInterval
	attempts := 0

	for {
		attempts++

		// Check timeout
		if time.Since(startTime) > pollingConfig.Timeout {
			return nil, NewTaskError(fmt.Sprintf("task polling timeout after %v", pollingConfig.Timeout), nil)
		}

		// Check max attempts
		if attempts > pollingConfig.MaxAttempts {
			return nil, NewTaskError(fmt.Sprintf("max polling attempts (%d) exceeded", pollingConfig.MaxAttempts), nil)
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Poll task status
		status, err := c.GetTaskStatus(ctx, taskID)
		if err != nil {
			core.Warn("Failed to get task status (attempt %d): %v", attempts, err)
			// Continue polling on transient errors
			time.Sleep(interval)
			continue
		}

		core.Debug("Task %s status: %s (attempt %d)", taskID, status.Status, attempts)

		// Check if task is complete
		switch status.Status {
		case TaskStatusSucceeded:
			core.Info("Task %s completed successfully", taskID)
			return status, nil
		case TaskStatusFailed:
			errorMsg := "unknown error"
			if status.Error != nil {
				errorMsg = *status.Error
			}
			return status, NewTaskError(fmt.Sprintf("task failed: %s", errorMsg), nil)
		case TaskStatusCanceled:
			return status, NewTaskError("task was canceled", nil)
		}

		// Task is still pending or running, wait before next poll
		time.Sleep(interval)

		// Increase interval with exponential backoff (up to max)
		interval = time.Duration(float64(interval) * 1.5)
		if interval > pollingConfig.MaxInterval {
			interval = pollingConfig.MaxInterval
		}
	}
}

// createTask is a helper to create a task via POST request
func (c *RunwayClient) createTask(ctx context.Context, endpoint string, reqBody interface{}) (*TaskResponse, error) {
	req, err := c.newRequest(ctx, "POST", endpoint, reqBody)
	if err != nil {
		return nil, err
	}

	var response TaskResponse
	if err := c.doRequest(req, &response); err != nil {
		return nil, err
	}

	core.Debug("Created task %s with status %s", response.ID, response.Status)
	return &response, nil
}

// newRequest creates a new HTTP request with proper headers
func (c *RunwayClient) newRequest(ctx context.Context, method, endpoint string, body interface{}) (*http.Request, error) {
	url := c.config.RunwayBaseURL + endpoint

	var bodyReader io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, core.NewProviderError("failed to marshal request body", err)
		}
		bodyReader = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, core.NewProviderError("failed to create HTTP request", err)
	}

	// Set required headers
	req.Header.Set("Authorization", "Bearer "+c.config.RunwayAPIKey)
	req.Header.Set("X-Runway-Version", c.config.RunwayVersion)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "revenium-middleware-runway-go/1.0")

	return req, nil
}

// doRequest executes an HTTP request and decodes the response
func (c *RunwayClient) doRequest(req *http.Request, result interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return core.NewNetworkError("HTTP request failed", err)
	}
	defer resp.Body.Close()

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return core.NewNetworkError("failed to read response body", err)
	}

	// Check for HTTP errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to parse error response
		var runwayError RunwayErrorResponse
		if json.Unmarshal(bodyBytes, &runwayError) == nil && runwayError.Error.Message != "" {
			return core.NewProviderError(
				fmt.Sprintf("Runway API error (%d): %s", resp.StatusCode, runwayError.Error.Message),
				nil,
			).WithDetails("code", runwayError.Error.Code).WithDetails("type", runwayError.Error.Type)
		}

		// Generic error if we can't parse the response
		return core.NewProviderError(
			fmt.Sprintf("Runway API returned status %d: %s", resp.StatusCode, string(bodyBytes)),
			nil,
		)
	}

	// Decode successful response
	if result != nil {
		if err := json.Unmarshal(bodyBytes, result); err != nil {
			return core.NewProviderError("failed to decode response", err)
		}
	}

	return nil
}

// Close closes the HTTP client
func (c *RunwayClient) Close() error {
	// Nothing to clean up for HTTP client
	return nil
}
