package main

import (
	"context"
	"fmt"
	"log"

	openai "github.com/openai/openai-go/v3"
	reveniumopenai "github.com/revenium/revenium-go-sdk/openai"
)

func main() {
	if err := reveniumopenai.Initialize(); err != nil {
		log.Fatal(err)
	}
	client, err := reveniumopenai.GetClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	stream, err := client.Chat().Completions().NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model: "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("Write a short poem about technology."),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Print("Response: ")
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			fmt.Print(chunk.Choices[0].Delta.Content)
		}
	}
	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("\n\nStreaming complete! Usage data sent to Revenium.")
}
