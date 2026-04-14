package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/revenium/revenium-go-sdk/core/metering"
)

func main() {
	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey: os.Getenv("REVENIUM_METERING_API_KEY"),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer mc.Close()

	payload := metering.NewToolEvent("weather-api").
		WithOperation("get_forecast").
		WithDuration(245 * time.Millisecond).
		WithSuccess(true).
		WithAgent("customer-support").
		WithOrganization("acme-corp").
		WithTraceID("session-123").
		Build()

	mc.SendToolEvent(payload)
	mc.Flush()

	fmt.Println("Tool event sent successfully.")
	fmt.Printf("Transaction ID: %s\n", payload.TransactionID)
	fmt.Printf("Tool: %s / %s\n", payload.ToolID, payload.Operation)
	fmt.Printf("Duration: %dms\n", payload.DurationMs)
}
