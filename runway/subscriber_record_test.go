package runway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func completedTaskHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") {
			_, _ = w.Write([]byte(`{"id":"task-sub","status":"SUCCEEDED","output":["https://x/v.mp4"],"createdAt":"2026-10-08T00:00:00Z"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"task-sub","status":"PENDING"}`))
	})
}

func TestVideoRecordsCarryTypedSubscriberOverArgumentSubscriber(t *testing.T) {
	calls := map[string]func(ctx context.Context, r *ReveniumRunway, metadata *UsageMetadata) error{
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
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			runwayServer := httptest.NewServer(completedTaskHandler())
			defer runwayServer.Close()
			mock := testutil.NewMockMeteringServer()
			defer mock.Close()

			r, err := NewReveniumRunway(&Config{
				RunwayAPIKey:  "rw-test",
				RunwayBaseURL: runwayServer.URL,
				Revenium:      &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL()},
			})
			require.NoError(t, err)
			defer r.Close()
			ctx := core.WithSubscriber(context.Background(), &core.Subscriber{ID: "rt-sub-42", Email: "rt-sub@example.com"})

			err = call(ctx, r, &UsageMetadata{Subscriber: map[string]interface{}{"id": "argument-user"}})
			require.NoError(t, err)
			r.Flush()

			require.True(t, mock.WaitForPayloads(1, 2*time.Second))
			assert.Equal(t, map[string]interface{}{"id": "rt-sub-42", "email": "rt-sub@example.com"}, mock.GetPayloads()[0]["subscriber"])
		})
	}
}

func TestVideoRecordKeepsArgumentSubscriberWithoutTypedSubscriber(t *testing.T) {
	runwayServer := httptest.NewServer(completedTaskHandler())
	defer runwayServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r, err := NewReveniumRunway(&Config{
		RunwayAPIKey:  "rw-test",
		RunwayBaseURL: runwayServer.URL,
		Revenium:      &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL()},
	})
	require.NoError(t, err)
	defer r.Close()

	_, err = r.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png"},
		&UsageMetadata{Subscriber: map[string]interface{}{"id": "argument-user"}})
	require.NoError(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assert.Equal(t, map[string]interface{}{"id": "argument-user"}, mock.GetPayloads()[0]["subscriber"])
}
