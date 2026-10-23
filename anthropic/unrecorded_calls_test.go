package anthropic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadmeNamesAnthropicCallsThatAreNotRecorded(t *testing.T) {
	section := unrecordedCallsSection(t, "Anthropic")
	for _, surface := range []string{"`GetAnthropicClient()`", "`Messages.Batches`", "`Beta.Messages.Batches`", "`Completions`", "`Beta.Sessions`", "`GetAnthropicClient().Beta.Messages`"} {
		assert.Contains(t, section, surface)
	}
}

func unrecordedCallsSection(t *testing.T, provider string) string {
	t.Helper()
	section := callsNotRecordedIn(t, "README.md")
	require.Equal(t, section, callsNotRecordedIn(t, "llms-full.txt"),
		"README.md and llms-full.txt must list the same calls as not recorded")
	intro, providerParts, _ := strings.Cut(section, "\n### ")
	_, providerPart, found := strings.Cut("\n### "+providerParts, "\n### "+provider+"\n")
	require.True(t, found, "the section must have a part for %s", provider)
	providerPart, _, _ = strings.Cut(providerPart, "\n### ")
	return strings.Join(strings.Fields(intro+" "+providerPart), " ")
}

func callsNotRecordedIn(t *testing.T, document string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", document))
	require.NoError(t, err)
	_, section, found := strings.Cut(string(content), "\n## Calls That Are Not Recorded\n")
	require.True(t, found, "%s must have a section listing the calls that are not recorded", document)
	section, _, _ = strings.Cut(section, "\n## ")
	return section
}
