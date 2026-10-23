package jobs

import (
	"errors"
	"net/http"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
)

func TestRefusedBeforeApplying_CountsOnlyAThrottledHTTPAnswer(t *testing.T) {
	throttled := core.NewNetworkError("job API returned 429", nil)
	throttled.StatusCode = http.StatusTooManyRequests
	gateway := core.NewMeteringError("job API returned 502", nil)
	gateway.StatusCode = http.StatusBadGateway

	assert.True(t, refusedBeforeApplying(throttled))
	assert.False(t, refusedBeforeApplying(gateway))
	assert.False(t, refusedBeforeApplying(core.NewNetworkError("upstream overloaded, rate limit", nil)), "a throttling-like message with no HTTP answer proves nothing")
	assert.False(t, refusedBeforeApplying(errors.New("too many requests")))
	assert.False(t, refusedBeforeApplying(nil))
}
