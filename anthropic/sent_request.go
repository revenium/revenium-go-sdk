package anthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

// sentRequest keeps the body of a tool runner's last request, read as it is sent. The
// runner changes Params around every request (it adds tool results before it, and the
// reply or a compaction summary after it), so the body is the only exact record of the
// conversation a turn sent, which is what the Python SDK captures from its wrapped call.
type sentRequest struct {
	mu   sync.RWMutex
	body []byte
}

func (s *sentRequest) middleware() option.RequestOption {
	return option.WithMiddleware(func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		if req.GetBody != nil && prompt.ShouldCapturePrompts(core.GetUsageMetadata(req.Context())) {
			s.keep(req)
		}
		return next(req)
	})
}

func (s *sentRequest) keep(req *http.Request) {
	body, err := req.GetBody()
	if err != nil {
		return
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = data
}

func (s *sentRequest) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = nil
}

func (s *sentRequest) capture(reply string) metering.ChatCapture {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var sent struct {
		System   json.RawMessage `json:"system"`
		Messages json.RawMessage `json:"messages"`
	}
	if len(s.body) == 0 || json.Unmarshal(s.body, &sent) != nil {
		return metering.ChatCapture{Output: reply}
	}
	return turnCapture(systemTexts(sent.System), sent.Messages, reply)
}

// systemTexts is a request's system prompt as its text blocks; a string is one block.
func systemTexts(raw json.RawMessage) []string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []string{text}
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	texts := make([]string, len(blocks))
	for i, block := range blocks {
		texts[i] = block.Text
	}
	return texts
}
