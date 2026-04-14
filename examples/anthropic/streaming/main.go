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

	result, err := client.Messages().CreateMessageStream(context.Background(), anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Write a short poem about technology.")),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	sw := result.(*reveniumanthropic.StreamingWrapper)

	fmt.Print("Response: ")
	for sw.Next() {
		_ = sw.Current()
	}
	if err := sw.Err(); err != nil {
		log.Fatal(err)
	}
	sw.Close()

	msg := reveniumanthropic.ReconstructResponseFromChunks(sw)
	if msg != nil {
		for _, block := range msg.Content {
			if block.Type == "text" {
				fmt.Print(block.Text)
			}
		}
	}

	fmt.Println("\n\nStreaming complete! Usage data sent to Revenium.")
}
