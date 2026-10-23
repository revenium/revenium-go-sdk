package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackgroundWaits_StopCancelsRunningWaitsAndWaitsForThem(t *testing.T) {
	var waits BackgroundWaits
	var settled atomic.Int32
	started := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		waits.Start(func(stop context.Context) {
			started <- struct{}{}
			<-stop.Done()
			settled.Add(1)
		})
	}
	<-started
	<-started

	waits.Stop()

	assert.Equal(t, int32(2), settled.Load())
}

func TestBackgroundWaits_StartAfterStopRunsSynchronouslyWithACancelledContext(t *testing.T) {
	var waits BackgroundWaits
	waits.Stop()

	ran := false
	waits.Start(func(stop context.Context) {
		require.Error(t, stop.Err())
		ran = true
	})

	assert.True(t, ran)
}

func TestBackgroundWaits_StopWithNothingRunningReturnsAtOnce(t *testing.T) {
	var waits BackgroundWaits
	done := make(chan struct{})
	go func() {
		waits.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return")
	}
}
