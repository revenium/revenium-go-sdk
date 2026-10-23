package groq

import "github.com/revenium/revenium-go-sdk/core"

// ErrStreamingNotSupported is returned by ChatCompletions, before any request is sent, when
// ChatCompletionRequest.Stream is true: the middleware reads the reply as one JSON object and
// cannot meter a streamed one
var ErrStreamingNotSupported = core.NewValidationError(
	"Groq streaming is not supported by the Revenium middleware: set ChatCompletionRequest.Stream to false "+
		"(no request was sent to the provider)", nil)
