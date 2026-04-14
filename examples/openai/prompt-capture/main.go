package main

import (
	"context"
	"fmt"
	"log"
	"os"

	openai "github.com/openai/openai-go/v3"
	reveniumopenai "github.com/revenium/revenium-go-sdk/openai"
)

func main() {
	os.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")

	if err := reveniumopenai.Initialize(); err != nil {
		log.Fatal(err)
	}
	client, err := reveniumopenai.GetClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	resp, err := client.Chat().Completions().New(context.Background(), openai.ChatCompletionNewParams{
		Model: "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("What is the capital of France?"),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:", resp.Choices[0].Message.Content)
	fmt.Println("\nPrompt and response captured for analysis.")
}
