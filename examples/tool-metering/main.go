package main

import (
	"fmt"
	"os"
	"time"

	"github.com/revenium/revenium-go-sdk/core/metering"
)

func main() {
	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey: os.Getenv("REVENIUM_METERING_API_KEY"),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create metering client: %v\n", err)
		os.Exit(1)
	}
	defer mc.Close()

	payload := metering.NewToolEvent("weather-api").
		WithOperation("get_forecast").
		WithDuration(245 * time.Millisecond).
		WithSuccess(true).
		WithAgent("my-agent").
		WithTraceID("session-123").
		Build()

	mc.SendToolEvent(payload)

	mc.Flush()
	fmt.Println("Tool event sent successfully")
}
