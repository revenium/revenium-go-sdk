package groq

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapStopReasonToRevenium(t *testing.T) {
	cases := map[string]string{
		"stop":           "END",
		"length":         "TOKEN_LIMIT",
		"tool_calls":     "END_SEQUENCE",
		"TOOL_CALLS":     "END_SEQUENCE",
		"function_call":  "END_SEQUENCE",
		"FUNCTION_CALL":  "END_SEQUENCE",
		"content_filter": "ERROR",
		"null":           "END",
		"":               "END",
		"unknown_value":  "END",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, mapStopReasonToRevenium(in))
		})
	}
}
