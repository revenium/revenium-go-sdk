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

	resp, err := client.Chat().Completions().New(context.Background(), openai.ChatCompletionNewParams{
		Model: "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("What is the capital of France?"),
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "completion error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(resp.Choices[0].Message.Content)
}
