package metering

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var uuidV4Regex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestGenerateTransactionID_Format(t *testing.T) {
	id := GenerateTransactionID()
	require.Regexp(t, uuidV4Regex, id)
}

func TestGenerateTransactionID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := GenerateTransactionID()
		assert.False(t, seen[id], "duplicate ID: %s", id)
		seen[id] = true
	}
}
