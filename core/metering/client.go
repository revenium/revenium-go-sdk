package metering

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/prompt"
	"github.com/revenium/revenium-go-sdk/core/resilience"
)

var sharedHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
	},
}

type MeteringClientConfig struct {
	APIKey  string
	BaseURL string
}

type MeteringClient struct {
	config      MeteringClientConfig
	wg          sync.WaitGroup
	retryConfig *resilience.RetryConfig
}

func NewMeteringClient(cfg MeteringClientConfig) (*MeteringClient, error) {
	if cfg.APIKey == "" {
		return nil, core.NewConfigError("metering API key is required", nil)
	}
	return &MeteringClient{config: cfg}, nil
}

func (c *MeteringClient) Send(payload *MeteringPayload) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				core.Error("panic in metering send: %v", r)
			}
		}()
		if err := c.SendSync(payload); err != nil {
			core.Error("failed to send metering data: %v", err)
		}
	}()
}

func (c *MeteringClient) SendSync(payload *MeteringPayload) error {
	cb := resilience.GetGlobalCircuitBreaker()
	retryConfig := c.retryConfig
	if retryConfig == nil {
		retryConfig = resilience.DefaultRetryConfig()
	}
	return cb.Execute(func() error {
		return resilience.WithRetry(context.Background(), func() error {
			return c.sendRequest(payload)
		}, retryConfig)
	})
}

func (c *MeteringClient) SendToolEvent(payload *ToolEventPayload) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				core.Error("panic in tool event send: %v", r)
			}
		}()
		if err := c.SendToolEventSync(payload); err != nil {
			core.Error("failed to send tool event data: %v", err)
		}
	}()
}

func (c *MeteringClient) SendToolEventSync(payload *ToolEventPayload) error {
	cb := resilience.GetGlobalCircuitBreaker()
	retryConfig := c.retryConfig
	if retryConfig == nil {
		retryConfig = resilience.DefaultRetryConfig()
	}
	return cb.Execute(func() error {
		return resilience.WithRetry(context.Background(), func() error {
			return c.sendToolEventRequest(payload)
		}, retryConfig)
	})
}

func setCommonHeaders(req *http.Request, apiKey, idempotencyKey string) {
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("User-Agent", "revenium-go-sdk/1.0")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
}

func (c *MeteringClient) sendToolEventRequest(payload *ToolEventPayload) error {
	if payload == nil {
		return core.NewValidationError("tool event payload must not be nil", nil)
	}

	url := ToolEventEndpoint(c.config.BaseURL)

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return core.NewMeteringError("failed to marshal tool event payload", err)
	}

	core.Debug("[METERING] Sending tool event to %s: %s", url, string(jsonData))

	req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return core.NewMeteringError("failed to create tool event request", err)
	}

	setCommonHeaders(req, c.config.APIKey, payload.IdempotencyKey)

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return core.NewNetworkError("tool event request failed", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := fmt.Sprintf("tool event API returned %d: %s", resp.StatusCode, string(body))
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

	core.Debug("[METERING] Tool event sent successfully")
	return nil
}

func printPayloadSummary(jsonData []byte) {
	format := prompt.ShouldPrintSummary()
	if format == "" {
		return
	}
	var m map[string]interface{}
	if err := json.Unmarshal(jsonData, &m); err != nil {
		return
	}
	prompt.PrintUsageSummary(m, format)
}

func (c *MeteringClient) Flush() {
	c.wg.Wait()
}

func (c *MeteringClient) Close() error {
	c.Flush()
	return nil
}

func (c *MeteringClient) sendRequest(payload *MeteringPayload) error {
	url := MeteringEndpoint(c.config.BaseURL, OperationType(payload.OperationType))

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return core.NewMeteringError("failed to marshal metering payload", err)
	}

	core.Debug("[METERING] Sending payload to %s: %s", url, string(jsonData))

	req, err := http.NewRequestWithContext(context.Background(), "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return core.NewMeteringError("failed to create metering request", err)
	}

	setCommonHeaders(req, c.config.APIKey, payload.IdempotencyKey)

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return core.NewNetworkError("metering request failed", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := fmt.Sprintf("metering API returned %d: %s", resp.StatusCode, string(body))
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

	core.Debug("[METERING] Metering data sent successfully")
	printPayloadSummary(jsonData)
	return nil
}
