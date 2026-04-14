package main

import (
	"context"
	"fmt"
	"log"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	reveniumanthropic "github.com/revenium/revenium-go-sdk/anthropic"
)

func main() {
	if err := reveniumanthropic.Initialize(); err != nil {
		log.Fatal(err)
	}
	client, err := reveniumanthropic.GetClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	msg, err := client.Messages().CreateMessage(context.Background(), anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Explain the concept of middleware in software architecture in 2-3 sentences.")),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, block := range msg.Content {
		if block.Type == "text" {
			fmt.Println("Response:", block.Text)
		}
	}

	fmt.Println("\nModel:", msg.Model)
	fmt.Println("Stop reason:", msg.StopReason)
	fmt.Printf("Usage: input=%d output=%d\n", msg.Usage.InputTokens, msg.Usage.OutputTokens)
}
