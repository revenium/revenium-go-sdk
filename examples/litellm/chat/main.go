package main

import (
	"context"
	"fmt"
	"os"

	reveniumlitellm "github.com/revenium/revenium-go-sdk/litellm"
)

func main() {
	if err := reveniumlitellm.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize: %v\n", err)
		os.Exit(1)
	}

	client, err := reveniumlitellm.GetClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	resp, err := client.Chat().Completions().New(context.Background(), reveniumlitellm.ChatCompletionRequest{
		Model: "openai/gpt-4o-mini",
		Messages: []reveniumlitellm.ChatMessage{
			{Role: "user", Content: "What is the capital of France?"},
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "completion error: %v\n", err)
		os.Exit(1)
	}

	if len(resp.Choices) > 0 {
		fmt.Println(resp.Choices[0].Message.Content)
	}
}
