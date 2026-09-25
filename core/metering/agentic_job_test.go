package metering

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// maxLenAgenticJobID is 255 characters, the longest id the server pattern
// accepts (one leading alphanumeric plus 254). tooLongAgenticJobID is 256.
var (
	maxLenAgenticJobID  = "a" + strings.Repeat("b", 254)
	tooLongAgenticJobID = "a" + strings.Repeat("b", 255)
)

// agenticJobIDCases is shared by every set point that accepts an agentic job
// id (ApplyMetadata, ApplyToolEventMetadata, ToolEventBuilder.WithAgenticJobID)
// so the three cannot drift apart. An id that fails the platform pattern must
// be dropped, because sending it costs the whole event to a 400.
var agenticJobIDCases = []struct {
	name  string
	id    string
	valid bool
}{
	{name: "typical id", id: "job-abc.123_x", valid: true},
	{name: "255 characters", id: maxLenAgenticJobID, valid: true},
	{name: "single alphanumeric", id: "a", valid: true},
	{name: "single digit", id: "7", valid: true},
	// The server reserved words are matched exactly, so case differences are legal ids.
	{name: "reserved word in a different case", id: "Types", valid: true},

	{name: "leading hyphen", id: "-job", valid: false},
	{name: "leading dot", id: ".job", valid: false},
	{name: "contains a slash", id: "job/abc", valid: false},
	{name: "contains a space", id: "job abc", valid: false},
	{name: "256 characters", id: tooLongAgenticJobID, valid: false},
	{name: "reserved word types", id: "types", valid: false},
	{name: "reserved word conversion-funnel", id: "conversion-funnel", valid: false},
	{name: "trailing newline", id: "job-abc\n", valid: false},
	{name: "empty", id: "", valid: false},
}

func TestIsValidAgenticJobID(t *testing.T) {
	for _, tt := range agenticJobIDCases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidAgenticJobID(tt.id))
		})
	}
}

func TestSanitizeAgenticJobID(t *testing.T) {
	for _, tt := range agenticJobIDCases {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeAgenticJobID(tt.id, "test")
			if tt.valid {
				assert.Equal(t, tt.id, got, "a valid id must pass through verbatim")
			} else {
				assert.Empty(t, got)
			}
		})
	}
}
