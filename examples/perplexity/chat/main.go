package main

import (
	"context"
	"fmt"
	"log"

	openai "github.com/openai/openai-go/v3"
	reveniumperplexity "github.com/revenium/revenium-go-sdk/perplexity"
)

func main() {
	if err := reveniumperplexity.Initialize(); err != nil {
		log.Fatal(err)
	}
	client, err := reveniumperplexity.GetClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	resp, err := client.Chat().Completions().New(context.Background(), openai.ChatCompletionNewParams{
		Model: "sonar",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("What are the key differences between REST and GraphQL APIs?"),
		},
		MaxTokens: openai.Int(500),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:", resp.Choices[0].Message.Content)
	fmt.Println("\nModel:", resp.Model)
	fmt.Printf("Usage: prompt=%d completion=%d total=%d\n",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
}
