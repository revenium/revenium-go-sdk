package fal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRun_VideoFamiliesOutsideTheOldDefaultsTableAreRecordedWithALength(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		input    map[string]interface{}
		want     float64
	}{
		{"sora-2 default", "fal-ai/sora-2/text-to-video", map[string]interface{}{"prompt": "p"}, 4},
		{"sora-2 requested", "fal-ai/sora-2/text-to-video", map[string]interface{}{"prompt": "p", "duration": 12}, 12},
		{"wan v2.2 default frames", "fal-ai/wan/v2.2-a14b/text-to-video", map[string]interface{}{"prompt": "p"}, 81.0 / 16},
		{"wan v2.2 requested frames and rate", "fal-ai/wan/v2.2-a14b/text-to-video", map[string]interface{}{"prompt": "p", "num_frames": 161, "frames_per_second": 23}, 7},
		{"wan v2.2 requested frames at the default rate", "fal-ai/wan/v2.2-a14b/text-to-video", map[string]interface{}{"prompt": "p", "num_frames": 160}, 10},
		{"hunyuan-video default frames", "fal-ai/hunyuan-video", map[string]interface{}{"prompt": "p"}, 129.0 / 24},
		{"hunyuan-video requested frames", "fal-ai/hunyuan-video", map[string]interface{}{"prompt": "p", "num_frames": "85"}, 85.0 / 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := meterFalCall(t, tt.endpoint, videoFileOutput, tt.input)

			assert.Equal(t, "VIDEO", record["operationType"])
			assert.InDelta(t, tt.want, record["durationSeconds"], 1e-9)
			assert.InDelta(t, tt.want, record["requestedDurationSeconds"], 1e-9)
			assert.Equal(t, "END", record["stopReason"])
			assert.NotContains(t, record, "errorReason")
		})
	}
}

func TestRun_VideoLengthFalReportsWinsOverTheRequestedOne(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   float64
	}{
		{"duration", `{"video":{"url":"https://x/v.mp4","duration":6.5}}`, 6.5},
		{"frames over rate", `{"video":{"url":"https://x/v.mp4","num_frames":121,"fps":24}}`, 121.0 / 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := meterFalCall(t, "fal-ai/sora-2/text-to-video", tt.output, map[string]interface{}{"prompt": "p", "duration": 8})

			assert.InDelta(t, tt.want, record["durationSeconds"], 1e-9)
			assert.EqualValues(t, 8, record["requestedDurationSeconds"])
		})
	}
}

func TestRun_VideoWithNoLengthAnywhereIsRecordedWithAReason(t *testing.T) {
	record := meterFalCall(t, "fal-ai/brand-new-video/text-to-video", videoFileOutput, map[string]interface{}{"prompt": "p"})

	assert.Equal(t, "VIDEO", record["operationType"])
	assert.Contains(t, record, "durationSeconds", "the video endpoint rejects a record with no length")
	assert.EqualValues(t, 0, record["durationSeconds"])
	assert.EqualValues(t, 0, record["requestedDurationSeconds"])
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Contains(t, record["errorReason"], "video_length_unknown")
	assert.Contains(t, record["errorReason"], "fal-ai/brand-new-video/text-to-video")
}

func TestRun_TypedGoNumbersInTheRequestAreRead(t *testing.T) {
	type frameCount uint16
	tests := []struct {
		name     string
		endpoint string
		input    map[string]interface{}
		want     float64
	}{
		{"int32 frames, uint8 rate", "fal-ai/wan/v2.2-a14b/text-to-video", map[string]interface{}{"prompt": "p", "num_frames": int32(161), "frames_per_second": uint8(23)}, 7},
		{"int64 frames, float32 fps", "fal-ai/wan/v2.2-a14b/text-to-video", map[string]interface{}{"prompt": "p", "num_frames": int64(160), "fps": float32(20)}, 8},
		{"named integer frames at the default rate", "fal-ai/wan/v2.2-a14b/text-to-video", map[string]interface{}{"prompt": "p", "num_frames": frameCount(160)}, 10},
		{"int16 duration", "fal-ai/sora-2/text-to-video", map[string]interface{}{"prompt": "p", "duration": int16(12)}, 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := meterFalCall(t, tt.endpoint, videoFileOutput, tt.input)

			assert.InDelta(t, tt.want, record["durationSeconds"], 1e-9)
			assert.InDelta(t, tt.want, record["requestedDurationSeconds"], 1e-9)
		})
	}
}
