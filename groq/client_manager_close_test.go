package groq

import (
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientManager_CloseAllClosesEveryClientEvenWhenOneReportsLostRecords(t *testing.T) {
	t.Setenv(core.EnvTeamID, "")
	down := httptest.NewServer(nil)
	down.Close()
	manager := NewClientManager()
	var sends sync.WaitGroup
	for _, key := range []string{"hak_customer_a", "hak_customer_b"} {
		client, err := manager.GetReveniumClient(key, &Config{
			GroqAPIKey: "gsk-test",
			Revenium:   &core.ReveniumConfig{APIKey: key, BaseURL: down.URL},
		})
		require.NoError(t, err)
		sends.Add(1)
		go func() {
			defer sends.Done()
			_ = client.metering.SendSync(metering.NewPayload(metering.OperationChat, "model", "PROVIDER").Build())
		}()
	}
	sends.Wait()

	err := manager.CloseAll()

	require.Error(t, err)
	assert.Equal(t, 2, len(splitJoined(err)), "both clients reported their undelivered record")
	assert.Empty(t, manager.reveniumClients, "the cache is cleared even though closing reported errors")
}

func splitJoined(err error) []error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return joined.Unwrap()
	}
	return []error{err}
}
