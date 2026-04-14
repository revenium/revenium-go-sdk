package fal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

// FalClient handles raw HTTP communication with the Fal.ai API
type FalClient struct {
	config     *Config
	httpClient *http.Client
}

func endpointURL(base, endpointID string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(endpointID, "/")
}

func truncateBody(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...[truncated]"
}

// getEndpointPath strips a leading "fal-ai/" segment so the legacy typed methods can prepend it themselves.
// New generic methods (Run/Subscribe/Stream) take the literal endpointID and bypass this helper.
func getEndpointPath(model string) string {
	const falPrefix = "fal-ai/"
	if strings.HasPrefix(model, falPrefix) {
		return strings.TrimPrefix(model, falPrefix)
	}
	return model
}

// NewFalClient creates a new Fal.ai HTTP client
func NewFalClient(config *Config) (*FalClient, error) {
	if config == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &FalClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.RequestTimeout,
		},
	}, nil
}

// doRequest executes an authenticated HTTP request against fal.ai and returns the raw response body and headers
func (c *FalClient) doRequest(ctx context.Context, method, fullURL string, body []byte, extraHeaders map[string]string) ([]byte, http.Header, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewBuffer(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, nil, core.NewNetworkError("failed to create request", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Key %s", c.config.FalAPIKey))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	core.Debug("HTTP %s %s", method, fullURL)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, core.NewNetworkError("request failed", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, core.NewNetworkError("failed to read response", err)
	}

	core.Debug("HTTP Response: %d", resp.StatusCode)

	if resp.StatusCode >= 400 {
		var falErr FalError
		if err := json.Unmarshal(respBody, &falErr); err == nil && (falErr.ErrorText != "" || falErr.Message != "") {
			falErr.Status = resp.StatusCode
			return respBody, resp.Header, core.NewProviderError(fmt.Sprintf("Fal.ai API error: %s", falErr.Error()), &falErr)
		}
		return respBody, resp.Header, core.NewProviderError(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncateBody(respBody, 256)), nil)
	}

	return respBody, resp.Header, nil
}

// rawStream opens a streaming HTTP request returning the body reader and headers; caller must close the reader.
func (c *FalClient) rawStream(ctx context.Context, method, fullURL string, body []byte, extraHeaders map[string]string) (io.ReadCloser, http.Header, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewBuffer(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, nil, core.NewNetworkError("failed to create request", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Key %s", c.config.FalAPIKey))
	req.Header.Set("Accept", "text/event-stream")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	core.Debug("HTTP %s %s (stream)", method, fullURL)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, core.NewNetworkError("request failed", err)
	}

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, resp.Header, core.NewProviderError(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncateBody(respBody, 256)), nil)
	}

	return resp.Body, resp.Header, nil
}
