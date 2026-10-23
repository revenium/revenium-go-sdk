package runway

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoRecordCarriesCredentialAliasAsSubscriberCredentialName(t *testing.T) {
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

	_, err = r.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png"}, &UsageMetadata{CredentialAlias: "runway-prod-key"})
	require.NoError(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	record := mock.GetPayloads()[0]
	assert.Equal(t, map[string]interface{}{"credential": map[string]interface{}{"name": "runway-prod-key"}}, record["subscriber"])
	assert.NotContains(t, record, "credentialAlias")
}
