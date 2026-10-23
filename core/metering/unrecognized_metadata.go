package metering

import (
	"sync"

	"github.com/revenium/revenium-go-sdk/core"
)

// metadataKeysReadOutsideFieldTables lists the usage-metadata keys the SDK
// reads anywhere other than stringFields, floatFields, recordFields and the
// tool-event field tables. A key missing here is reported as one Revenium does
// not record. TestEveryMetadataKeyTheSDKReadsIsRecognized scans the
// repository's non-test Go code for reads from a map named like metadata,
// meta or md (an index with a quoted key or a string constant, or a helper
// call such as firstString(metadata, "agent")) and fails on a key that is not
// recognized; a read through another variable name is not seen.
var metadataKeysReadOutsideFieldTables = map[string]struct{}{
	retryNumberKey:     {},
	"subscriber":       {},
	"operationSubtype": {},
	systemPromptKey:    {},
	"capturePrompts":   {},
	"model":            {},
	"_capturedPrompt":  {},
	"usageMetadata":    {},
}

// maxWarnedMetadataKeys bounds the memory kept to warn once per key, since
// the keys come from the caller and may be unbounded (an id used as a key).
const maxWarnedMetadataKeys = 256

var (
	warnedMetadataKeysMu sync.Mutex
	warnedMetadataKeys   = map[string]struct{}{}
)

func isRecognizedMetadataKey(key string) bool {
	if _, ok := stringFields[key]; ok {
		return true
	}
	if _, ok := floatFields[key]; ok {
		return true
	}
	if _, ok := recordFields[key]; ok {
		return true
	}
	if isToolEventMetadataKey(key) {
		return true
	}
	_, ok := metadataKeysReadOutsideFieldTables[key]
	return ok
}

func isToolEventMetadataKey(key string) bool {
	for _, fields := range [][]toolEventFieldMapping{canonicalToolEventFields, aliasToolEventFields} {
		for _, field := range fields {
			if field.key == key {
				return true
			}
		}
	}
	return false
}

func warnUnrecognizedKeys(metadata map[string]interface{}) {
	for key := range metadata {
		if isRecognizedMetadataKey(key) {
			continue
		}
		if firstSightOf(key) {
			core.Warn("[METERING] usage metadata key %q is not a field Revenium records, so it is ignored; see the README metadata table for the supported keys", key)
		}
	}
}

func firstSightOf(key string) bool {
	warnedMetadataKeysMu.Lock()
	defer warnedMetadataKeysMu.Unlock()
	if _, warned := warnedMetadataKeys[key]; warned || len(warnedMetadataKeys) >= maxWarnedMetadataKeys {
		return false
	}
	warnedMetadataKeys[key] = struct{}{}
	return true
}
