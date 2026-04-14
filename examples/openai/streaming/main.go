package main

import (
	"context"
	"fmt"
	"os"

	openai "github.com/openai/openai-go/v3"
	reveniumopenai "github.com/revenium/revenium-go-sdk/openai"
)

func main() {
	if err := reveniumopenai.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize: %v\n", err)
		os.Exit(1)
	}

	client, err := reveniumopenai.GetClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	stream, err := client.Chat().Completions().NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model: "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("Write a haiku about Go programming"),
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "streaming error: %v\n", err)
		os.Exit(1)
	}

	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			fmt.Print(chunk.Choices[0].Delta.Content)
		}
	}
	if err := stream.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "\nstream error: %v\n", err)
		os.Exit(1)
	}
	if err := stream.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "\nclose error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
}
