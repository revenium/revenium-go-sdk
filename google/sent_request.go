package google

import (
	"context"
	"net/http"
	"sync/atomic"
)

type sentRequestKey struct{}

// sentRequest notes whether a call's request reached the HTTP transport. genai
// refuses some calls before sending anything, such as GenerateImages on the
// Gemini API, and a call Google never received is not recorded as a failure.
type sentRequest struct {
	sent atomic.Bool
}

func watchSending(ctx context.Context) (context.Context, *sentRequest) {
	watch := &sentRequest{}
	return context.WithValue(ctx, sentRequestKey{}, watch), watch
}

func (s *sentRequest) left() bool {
	return s.sent.Load()
}

type sendingTransport struct {
	base http.RoundTripper
}

func (t *sendingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if watch, ok := req.Context().Value(sentRequestKey{}).(*sentRequest); ok {
		watch.sent.Store(true)
	}
	return t.base.RoundTrip(req)
}
