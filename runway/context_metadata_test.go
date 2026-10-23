package runway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type videoCall func(ctx context.Context, r *ReveniumRunway, metadata *UsageMetadata) error

func everyVideoCall() map[string]videoCall {
	return map[string]videoCall{
		"image to video": func(ctx context.Context, r *ReveniumRunway, metadata *UsageMetadata) error {
			_, err := r.ImageToVideo(ctx, &ImageToVideoRequest{PromptImage: "https://x/i.png"}, metadata)
			return err
		},
		"video to video": func(ctx context.Context, r *ReveniumRunway, metadata *UsageMetadata) error {
			_, err := r.VideoToVideo(ctx, &VideoToVideoRequest{PromptVideo: "https://x/v.mp4"}, metadata)
			return err
		},
		"upscale": func(ctx context.Context, r *ReveniumRunway, metadata *UsageMetadata) error {
			_, err := r.UpscaleVideo(ctx, &VideoUpscaleRequest{PromptVideo: "https://x/v.mp4", SourceDurationSeconds: 8}, metadata)
			return err
		},
	}
}

func recordOf(t *testing.T, call videoCall, ctx context.Context, metadata *UsageMetadata) map[string]interface{} {
	t.Helper()
	client, mock := newStubbedRunway(t)
	require.NoError(t, call(ctx, client, metadata))
	client.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	return mock.GetPayloads()[0]
}

func TestVideoRecordsCarryUsageMetadataSetOnlyOnTheContext(t *testing.T) {
	for name, call := range everyVideoCall() {
		t.Run(name, func(t *testing.T) {
			ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
				"productName":      "video-app",
				"organizationName": "acme",
				"agent":            "agent-x",
				"traceId":          "trace-vd",
				"agenticJobId":     "job-vd-1",
			})

			record := recordOf(t, call, ctx, nil)

			assert.Equal(t, "video-app", record["productName"])
			assert.Equal(t, "acme", record["organizationName"])
			assert.Equal(t, "agent-x", record["agent"])
			assert.Equal(t, "trace-vd", record["traceId"])
			assert.Equal(t, "job-vd-1", record["agenticJobId"])
		})
	}
}

func TestVideoRecordsTakeTheContextValueWhenTheArgumentSetsTheSameField(t *testing.T) {
	for name, call := range everyVideoCall() {
		t.Run(name, func(t *testing.T) {
			ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"productName": "from-context"})

			record := recordOf(t, call, ctx, &UsageMetadata{ProductName: "from-argument", Agent: "argument-agent"})

			assert.Equal(t, "from-context", record["productName"])
			assert.Equal(t, "argument-agent", record["agent"])
		})
	}
}

func TestVideoCallsAreBlockedByARuleOnTheProductSetOnTheContext(t *testing.T) {
	for name, call := range everyVideoCall() {
		t.Run(name, func(t *testing.T) {
			enforcement.Stop()
			t.Cleanup(enforcement.Stop)
			provider := newRunwayStub(t)
			rules := testutil.NewMockRulesServer()
			t.Cleanup(rules.Close)
			meteringServer := testutil.NewMockMeteringServer()
			t.Cleanup(meteringServer.Close)
			rules.SetPayload("team-a", `{"rules": [{"ruleId": 9, "threshold": 10, "currentValue": 12, "breached": true,
				"shadowMode": false, "action": "BLOCK",
				"filters": [{"dimension": "PRODUCT", "operator": "IS", "value": "video-app"}]}]}`)
			client := newEnforcedRunway(t, rules.URL(), meteringServer.URL(), provider.server.URL, "team-a")
			ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"productName": "video-app"})

			err := call(ctx, client, nil)

			var ece *enforcement.ErrCostLimitExceeded
			require.True(t, errors.As(err, &ece), "a breached rule on the context product should block, got %v", err)
			assert.Equal(t, int32(0), provider.hits.Load())
		})
	}
}
