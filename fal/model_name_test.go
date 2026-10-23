package fal

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
)

type warningRecorder struct {
	mu       sync.Mutex
	level    core.LogLevel
	warnings []string
}

func (l *warningRecorder) Debug(string, ...interface{}) {}
func (l *warningRecorder) Info(string, ...interface{})  {}
func (l *warningRecorder) Error(string, ...interface{}) {}
func (l *warningRecorder) Warn(message string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warnings = append(l.warnings, fmt.Sprintf(message, args...))
}
func (l *warningRecorder) SetLevel(level core.LogLevel) { l.level = level }
func (l *warningRecorder) GetLevel() core.LogLevel      { return l.level }

func (l *warningRecorder) modelNameWarnings() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var matched []string
	for _, w := range l.warnings {
		if strings.HasPrefix(w, "Model name") {
			matched = append(matched, w)
		}
	}
	return matched
}

func recordWarnings(t *testing.T) *warningRecorder {
	t.Helper()
	original := core.GetLogger()
	recorder := &warningRecorder{level: core.LogLevelDebug}
	core.SetLogger(recorder)
	t.Cleanup(func() { core.SetLogger(original) })
	return recorder
}

func TestRun_PartnerModelIsRecordedUnderItsOwnNamespaceWithoutAWarning(t *testing.T) {
	warnings := recordWarnings(t)

	record := meterFalCall(t, "bria/embed-product", singleImageOutput, map[string]interface{}{"image_url": "https://x/in.png"})

	assert.Equal(t, "fal_ai/bria/embed-product", record["model"])
	assert.NotContains(t, record["model"], "fal-ai/bria")
	assert.Empty(t, warnings.modelNameWarnings())
}

func TestRun_FalOwnModelIsRecordedAsBefore(t *testing.T) {
	warnings := recordWarnings(t)

	record := meterFalCall(t, "fal-ai/flux/dev", `{"images":[{"url":"https://x/i.png","width":512,"height":512}]}`, map[string]interface{}{"prompt": "cat"})

	assert.Equal(t, "fal_ai/fal-ai/flux/dev", record["model"])
	assert.Empty(t, warnings.modelNameWarnings())
}

func TestNormalizeModelName(t *testing.T) {
	cases := map[string]string{
		"fal-ai/flux/dev":           "fal_ai/fal-ai/flux/dev",
		"fal_ai/fal-ai/flux/dev":    "fal_ai/fal-ai/flux/dev",
		"bria/embed-product":        "fal_ai/bria/embed-product",
		"fal_ai/bria/embed-product": "fal_ai/bria/embed-product",
	}
	for model, want := range cases {
		t.Run(model, func(t *testing.T) {
			warnings := recordWarnings(t)
			assert.Equal(t, want, normalizeModelName(model))
			assert.Empty(t, warnings.modelNameWarnings())
		})
	}
}

func TestNormalizeModelName_WarnsOnlyForAnIdWithoutANamespace(t *testing.T) {
	warnings := recordWarnings(t)

	assert.Equal(t, "fal_ai/fal-ai/flux", normalizeModelName("flux"))
	assert.Len(t, warnings.modelNameWarnings(), 1)
}
