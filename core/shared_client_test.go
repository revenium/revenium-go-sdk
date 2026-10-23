package core

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProviderClient struct {
	revenium *ReveniumConfig
	closed   bool
}

func (c *fakeProviderClient) Close() error {
	c.closed = true
	return nil
}

func newFakeShared() *SharedClient[*fakeProviderClient] {
	return NewSharedClient("NewReveniumFake", func(c *fakeProviderClient) *ReveniumConfig { return c.revenium })
}

func builtFrom(revenium *ReveniumConfig, builds *atomic.Int32) func() (*fakeProviderClient, error) {
	return func() (*fakeProviderClient, error) {
		builds.Add(1)
		return &fakeProviderClient{revenium: revenium}, nil
	}
}

func TestSharedClient_ACallThatArrivesWhileTheFirstSetupRunsGetsTheRepeatAnswer(t *testing.T) {
	cases := []struct {
		name        string
		second      *ReveniumConfig
		wantChanged string
	}{
		{name: "bare call", second: nil},
		{name: "same key", second: &ReveniumConfig{APIKey: "hak_customer_a"}},
		{name: "other key", second: &ReveniumConfig{APIKey: "hak_customer_b"}, wantChanged: EnvAPIKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearReveniumEnv(t)
			t.Setenv(EnvTeamID, "")
			shared := newFakeShared()
			active := MergeReveniumConfig(&ReveniumConfig{APIKey: "hak_customer_a", BaseURL: "https://api.dev.hcapp.io"}, LoadReveniumConfig())
			var builds atomic.Int32
			firstBuilding := make(chan struct{})
			finishFirst := make(chan struct{})
			firstDone := make(chan error, 1)
			go func() {
				firstDone <- shared.Initialize(NamedAccount(active), func() (*fakeProviderClient, error) {
					builds.Add(1)
					close(firstBuilding)
					<-finishFirst
					return &fakeProviderClient{revenium: active}, nil
				})
			}()
			<-firstBuilding
			secondDone := make(chan error, 1)
			go func() {
				secondDone <- shared.Initialize(tc.second, func() (*fakeProviderClient, error) {
					builds.Add(1)
					return nil, NewConfigError(EnvAPIKey+" is required", nil)
				})
			}()
			require.Eventually(t, func() bool { return shared.waitingSetups.Load() == 1 }, 2*time.Second, time.Millisecond)
			close(finishFirst)

			require.NoError(t, <-firstDone)
			err := <-secondDone
			assert.Equal(t, int32(1), builds.Load(), "the second call never builds a client of its own")
			installed, getErr := shared.Get()
			require.NoError(t, getErr)
			assert.Same(t, active, installed.revenium)
			if tc.wantChanged == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.True(t, IsConfigError(err))
			assert.Contains(t, err.Error(), tc.wantChanged)
			assert.Contains(t, err.Error(), "NewReveniumFake")
		})
	}
}

func TestSharedClient_AFailedFirstSetupLeavesTheNextCallToBuild(t *testing.T) {
	shared := newFakeShared()
	var builds atomic.Int32

	err := shared.Initialize(nil, func() (*fakeProviderClient, error) { return nil, errors.New("provider key missing") })
	require.Error(t, err)
	assert.False(t, shared.IsInitialized())

	require.NoError(t, shared.Initialize(nil, builtFrom(&ReveniumConfig{APIKey: "hak_customer_a"}, &builds)))
	assert.True(t, shared.IsInitialized())
	assert.Equal(t, int32(1), builds.Load())
}

func TestSharedClient_GetBeforeSetupAndReset(t *testing.T) {
	shared := newFakeShared()
	var builds atomic.Int32

	_, err := shared.Get()
	require.Error(t, err)
	assert.True(t, IsConfigError(err))

	require.NoError(t, shared.Initialize(nil, builtFrom(&ReveniumConfig{APIKey: "hak_customer_a"}, &builds)))
	installed, _ := shared.Current()
	require.NoError(t, shared.ResetAndClose())
	assert.True(t, installed.closed)
	assert.False(t, shared.IsInitialized())
	assert.NoError(t, shared.ResetAndClose(), "nothing installed, nothing to close")
}
