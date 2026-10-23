package fal

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

// endpointDefaultDuration is one family's default length: seconds for a family whose request takes a
// duration, or frames at fps for a family whose request takes num_frames.
type endpointDefaultDuration struct {
	pattern *regexp.Regexp
	seconds float64
	frames  float64
	fps     float64
}

func (d endpointDefaultDuration) defaultSeconds() float64 {
	if d.seconds > 0 {
		return d.seconds
	}
	return d.frames / d.fps
}

// defaultVideoSeconds is the `duration` (or `num_frames` and `frames_per_second`) default from each
// family's fal OpenAPI schema (https://fal.ai/api/openapi/queue/openapi.json?endpoint_id=<id>, read
// 2026-10-08; sora-2, wan v2.2 and hunyuan-video read 2026-10-15, sora-2 from its fal API reference
// page). fal video output rarely carries a length. Hunyuan's schema has no frame rate; 24 is the rate
// the model renders at. The first matching pattern wins, so a narrower pattern precedes a wider one.
// Update when fal changes a default or a new family is metered.
var defaultVideoSeconds = []endpointDefaultDuration{
	{pattern: regexp.MustCompile(`(?i)veo2`), seconds: 5},
	{pattern: regexp.MustCompile(`(?i)veo3`), seconds: 8},
	{pattern: regexp.MustCompile(`(?i)kling-video/`), seconds: 5},
	{pattern: regexp.MustCompile(`(?i)minimax/hailuo`), seconds: 6},
	{pattern: regexp.MustCompile(`(?i)luma-dream-machine/ray-2`), seconds: 5},
	{pattern: regexp.MustCompile(`(?i)pixverse/`), seconds: 5},
	{pattern: regexp.MustCompile(`(?i)seedance/`), seconds: 5},
	{pattern: regexp.MustCompile(`(?i)wan-25`), seconds: 5},
	{pattern: regexp.MustCompile(`(?i)pika/`), seconds: 5},
	{pattern: regexp.MustCompile(`(?i)vidu/q2`), seconds: 4},
	{pattern: regexp.MustCompile(`(?i)ltx-2/`), seconds: 6},
	{pattern: regexp.MustCompile(`(?i)sora-2/`), seconds: 4},
	{pattern: regexp.MustCompile(`(?i)wan/v2\.2-5b`), frames: 81, fps: 24},
	{pattern: regexp.MustCompile(`(?i)wan/v2\.2`), frames: 81, fps: 16},
	{pattern: regexp.MustCompile(`(?i)wan-t2v`), frames: 81, fps: 16},
	{pattern: regexp.MustCompile(`(?i)hunyuan-video-v1\.5`), frames: 121, fps: 24},
	{pattern: regexp.MustCompile(`(?i)hunyuan-video`), frames: 129, fps: 24},
}

var (
	frameCountInputs = []string{"num_frames"}
	frameRateInputs  = []string{"frames_per_second", "fps"}
)

// requestedVideoSeconds is the length the request asked for: its duration, else its frame count over
// its frame rate (each falling back to the family default), else the family's default length. It is 0
// when none is known; the record then says so through unknownVideoLengthReason.
func requestedVideoSeconds(endpointID string, input map[string]interface{}) float64 {
	raw := input["duration"]
	if seconds, ok := parseVideoSeconds(raw); ok {
		return seconds
	}
	family, known := endpointDefault(endpointID)
	if seconds, ok := requestedFrameSeconds(input, family); ok {
		return seconds
	}
	if known {
		return family.defaultSeconds()
	}
	if raw != nil {
		core.Warn("Could not read video duration %v for %s", raw, endpointID)
	}
	return 0
}

func requestedFrameSeconds(input map[string]interface{}, family endpointDefaultDuration) (float64, bool) {
	frames, setFrames := firstPositive(input, frameCountInputs)
	fps, setFPS := firstPositive(input, frameRateInputs)
	if !setFrames && !setFPS {
		return 0, false
	}
	if !setFrames {
		frames = family.frames
	}
	if !setFPS {
		fps = family.fps
	}
	if frames <= 0 || fps <= 0 {
		return 0, false
	}
	return frames / fps, true
}

func firstPositive(input map[string]interface{}, keys []string) (float64, bool) {
	for _, key := range keys {
		if value, ok := parseVideoSeconds(input[key]); ok {
			return value, true
		}
	}
	return 0, false
}

func unknownVideoLengthReason(endpointID string) string {
	return fmt.Sprintf("video_length_unknown: fal reported no length for %s and the request set no duration the SDK can read, so the video is recorded with 0 seconds", endpointID)
}

func endpointDefault(endpointID string) (endpointDefaultDuration, bool) {
	for _, d := range defaultVideoSeconds {
		if d.pattern.MatchString(endpointID) {
			return d, true
		}
	}
	return endpointDefaultDuration{}, false
}

func endpointDefaultVideoSeconds(endpointID string) (float64, bool) {
	family, ok := endpointDefault(endpointID)
	if !ok {
		return 0, false
	}
	return family.defaultSeconds(), true
}

func parseVideoSeconds(value interface{}) (float64, bool) {
	seconds, ok := numberValue(value)
	if text, isText := value.(string); isText {
		trimmed := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(text)), "s")
		parsed, err := strconv.ParseFloat(trimmed, 64)
		seconds, ok = parsed, err == nil
	}
	if !ok || seconds <= 0 {
		return 0, false
	}
	return seconds, true
}
