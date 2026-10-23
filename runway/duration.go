package runway

import (
	"fmt"
	"math"

	"github.com/revenium/revenium-go-sdk/core"
)

// defaultVideoSecondsByModel is the length Runway renders when `duration` is omitted; a finished task
// carries no length. gen4_turbo/gen3a_turbo: Runway API reference, 2025-04 revision. veo3: fixed at 8.
// veo3.1: Google's Veo default. grok_imagine_1_5_lite: Runway docs. Update when Runway adds a model.
var defaultVideoSecondsByModel = map[string]float64{
	"gen4_turbo":            10,
	"gen3a_turbo":           10,
	"veo3":                  8,
	"veo3.1":                8,
	"veo3.1_fast":           8,
	"grok_imagine_1_5_lite": 8,
}

// billedVideoSeconds is the length a video record bills: the length the caller asked for, else the
// length of the source video for a model whose output follows its input, else the model's default.
// It is 0 when none is known; Runway reports no length on a finished task, so the record then says
// so through unknownVideoLengthReason instead of billing a guessed length.
func billedVideoSeconds(model string, requestedSeconds, sourceSeconds float64) float64 {
	if isBillableSeconds(requestedSeconds) {
		return requestedSeconds
	}
	if isBillableSeconds(sourceSeconds) {
		return sourceSeconds
	}
	if seconds, ok := defaultVideoSecondsByModel[model]; ok {
		return seconds
	}
	core.Warn("%s. Set Duration, or SourceDurationSeconds on VideoToVideo, to bill the real length", unknownVideoLengthReason(model))
	return 0
}

func unknownVideoLengthReason(model string) string {
	return fmt.Sprintf("video_length_unknown: no length was requested and Runway model %s has no known default length, so the video is recorded with 0 seconds", model)
}

// isBillableSeconds accepts only a finite, positive length: NaN and +Inf also fail the
// "<= 0" test, and a record carrying them would be rejected or priced at nothing.
func isBillableSeconds(seconds float64) bool {
	return seconds > 0 && !math.IsNaN(seconds) && !math.IsInf(seconds, 0)
}
