package ollama

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapStopReasonToRevenium(t *testing.T) {
	cases := map[string]string{
		"stop":           "END",
		"length":         "TOKEN_LIMIT",
		"tool_calls":     "END",
		"function_call":  "END",
		"content_filter": "ERROR",
		"null":           "END",
		"":               "END",
		"unknown":        "END",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, mapStopReasonToRevenium(in))
		})
	}
}
