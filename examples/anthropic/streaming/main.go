package main

import (
	"context"
	"fmt"
	"os"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	reveniumanthropic "github.com/revenium/revenium-go-sdk/anthropic"
)

func main() {
	if err := reveniumanthropic.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize: %v\n", err)
		os.Exit(1)
	}

	client, err := reveniumanthropic.GetClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	result, err := client.Messages().CreateMessageStream(context.Background(), anthropic.MessageNewParams{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Write a haiku about Go programming")),
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "streaming error: %v\n", err)
		os.Exit(1)
	}

	sw := result.(*reveniumanthropic.StreamingWrapper)
	for sw.Next() {
		_ = sw.Current()
	}
	if err := sw.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "\nstream error: %v\n", err)
		os.Exit(1)
	}
	sw.Close()

	msg := reveniumanthropic.ReconstructResponseFromChunks(sw)
	if msg != nil {
		for _, block := range msg.Content {
			if block.Type == "text" {
				fmt.Println(block.Text)
			}
		}
	}
}
