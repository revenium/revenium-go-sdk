package metering

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	metadataMapName         = `(?:\w*[mM]eta(?:data)?|md)`
	notAFieldOfAnotherValue = `(?:^|[^.\w])`
)

var (
	indexedMetadataRead = regexp.MustCompile(notAFieldOfAnotherValue + metadataMapName + `\[\s*(?:"([A-Za-z_]+)"|([A-Za-z_]\w*))\s*\]`)
	helperMetadataRead  = regexp.MustCompile(`\b\w+\(\s*\b` + metadataMapName + `\s*,\s*((?:"[A-Za-z_]+"\s*,?\s*)+)\)`)
	quotedKey           = regexp.MustCompile(`"([A-Za-z_]+)"`)
	assignment          = regexp.MustCompile(`^\s*=[^=]`)
	stringConstant      = regexp.MustCompile(`\b([A-Za-z_]\w*)\s+(?:string\s+)?=\s+"([A-Za-z_]+)"`)
)

type metadataRead struct{ key, path string }

func repositoryGoSources(t *testing.T) map[string]string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		t.Skip("runs only in a checkout of the whole repository")
	}
	sources := map[string]string{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "examples" || entry.Name() == "scripts") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		sources[path] = string(source)
		return err
	})
	require.NoError(t, err)
	return sources
}

// metadataReads finds the keys read (not written) from a usage-metadata map,
// a variable rather than a struct field named like metadata, meta or md: an
// index with a quoted key or a string
// constant, and a helper call whose first argument is the map followed by
// quoted keys, such as firstString(metadata, "agent").
func metadataReads(sources map[string]string) []metadataRead {
	constants := map[string]string{}
	for _, source := range sources {
		for _, match := range stringConstant.FindAllStringSubmatch(source, -1) {
			constants[match[1]] = match[2]
		}
	}
	var reads []metadataRead
	for path, source := range sources {
		for _, at := range indexedMetadataRead.FindAllStringSubmatchIndex(source, -1) {
			if assignment.MatchString(source[at[1]:]) {
				continue
			}
			match := submatches(source, at)
			if key := match[1]; key != "" {
				reads = append(reads, metadataRead{key, path})
			} else if key, ok := constants[match[2]]; ok {
				reads = append(reads, metadataRead{key, path})
			}
		}
		for _, match := range helperMetadataRead.FindAllStringSubmatch(source, -1) {
			for _, key := range quotedKey.FindAllStringSubmatch(match[1], -1) {
				reads = append(reads, metadataRead{key[1], path})
			}
		}
	}
	return reads
}

func submatches(source string, at []int) []string {
	match := make([]string, len(at)/2)
	for i := range match {
		if at[2*i] >= 0 {
			match[i] = source[at[2*i]:at[2*i+1]]
		}
	}
	return match
}

func TestEveryMetadataKeyTheSDKReadsIsRecognized(t *testing.T) {
	reads := metadataReads(repositoryGoSources(t))
	require.NotEmpty(t, reads)

	for _, read := range reads {
		assert.True(t, isRecognizedMetadataKey(read.key), "%q is read in %s but would be reported as a key Revenium does not record; add it to metadataKeysReadOutsideFieldTables", read.key, read.path)
	}
}

func TestMetadataReads_FindsLiteralConstantAndHelperReads(t *testing.T) {
	reads := metadataReads(map[string]string{"x.go": `
const brandNewKey = "brandNewKey"
func f(metadata map[string]interface{}, md map[string]interface{}) {
	_ = metadata["literalKey"]
	_ = md[brandNewKey]
	_ = firstString(metadata, "helperKey", "otherHelperKey")
	_ = headers["notMetadata"]
	_ = payload.UsageMetadata["nestedField"]
	metadata["written"] = 1
}`})

	var keys []string
	for _, read := range reads {
		keys = append(keys, read.key)
	}
	assert.ElementsMatch(t, []string{"literalKey", "brandNewKey", "helperKey", "otherHelperKey"}, keys)
}
