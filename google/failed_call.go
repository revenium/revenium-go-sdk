package google

import (
	"errors"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"google.golang.org/genai"
)

// callFailure describes err for its usage record, reading the HTTP status and
// the status name, such as "RESOURCE_EXHAUSTED", of a genai API error.
func callFailure(err error) metering.CallFailure {
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		return metering.CallFailure{Err: err, StatusCode: apiErr.Code, Kind: core.ErrorKindFrom(apiErr.Status)}
	}
	return metering.FailureOf(err)
}
