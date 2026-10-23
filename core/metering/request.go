package metering

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
)

const maxResponseBody = 64 * 1024

var defaultSender = &recordSender{httpClient: sharedHTTPClient, timeout: core.DefaultMeteringTimeout}

func senderWithTimeout(timeout time.Duration) *recordSender {
	if timeout <= 0 || timeout == defaultSender.timeout {
		return defaultSender
	}
	return &recordSender{httpClient: sharedHTTPClient, timeout: timeout}
}

// recordSender owns each request's deadline, so a request that ran out of
// time talking to a slow Revenium is told apart from a caller's own expired
// context and is retried and held like a refused connection.
type recordSender struct {
	httpClient *http.Client
	timeout    time.Duration
}

// post sends one usage record. Problems found while building the request are
// config errors: nothing reached Revenium, so retrying, buffering or counting
// them against Revenium's health would only hide them.
func (s *recordSender) post(endpoint string, body []byte, headers map[string]string, label string) error {
	req, err := newRecordRequest(endpoint, body, headers)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	resp, err := s.httpClient.Do(req.WithContext(ctx))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return core.NewNetworkError(fmt.Sprintf("%s request got no answer from Revenium within %s", label, s.timeout), nil)
		}
		return core.NewNetworkError(label+" request failed", withoutURLSecrets(err))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return resilience.ResponseError(label, resp.StatusCode, resp.Header, respBody)
}

const redactedURLPart = "REDACTED"

// withoutURLSecrets hides the user info and query of the address in a
// failed request's error. net/http masks only the password, and this error is
// logged at the default level and returned to SendSync callers.
func withoutURLSecrets(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	return &url.Error{Op: urlErr.Op, URL: redactURL(urlErr.URL), Err: urlErr.Err}
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return redactedURLPart
	}
	if u.User != nil {
		u.User = url.User(redactedURLPart)
	}
	if u.RawQuery != "" || u.ForceQuery {
		u.RawQuery = redactedURLPart
	}
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

func newRecordRequest(endpoint string, body []byte, headers map[string]string) (*http.Request, error) {
	if err := core.ValidateReveniumBaseURL(core.EnvBaseURL, endpoint); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, core.NewConfigError("usage record request could not be built", err)
	}
	for name, value := range headers {
		if !isSendableHeaderValue(value) {
			return nil, core.NewConfigError(fmt.Sprintf("the %s header contains a line break or control character, so the usage record cannot be sent", name), nil)
		}
		req.Header.Set(name, value)
	}
	return req, nil
}

func isSendableHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		b := value[i]
		if (b < ' ' && b != '\t') || b == 0x7f {
			return false
		}
	}
	return true
}
