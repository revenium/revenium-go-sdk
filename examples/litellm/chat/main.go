package main

import (
	"context"
	"fmt"
	"log"

	reveniumlitellm "github.com/revenium/revenium-go-sdk/litellm"
)

func main() {
	if err := reveniumlitellm.Initialize(); err != nil {
		log.Fatal(err)
	}
	client, err := reveniumlitellm.GetClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	resp, err := client.Chat().Completions().New(context.Background(), reveniumlitellm.ChatCompletionRequest{
		Model: "openai/gpt-4o-mini",
		Messages: []reveniumlitellm.ChatMessage{
			{Role: "user", Content: "Explain the concept of middleware in software architecture in 2-3 sentences."},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	if len(resp.Choices) > 0 {
		fmt.Println("Response:", resp.Choices[0].Message.Content)
	}
	fmt.Println("\nModel:", resp.Model)
	fmt.Printf("Usage: prompt=%d completion=%d total=%d\n",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
}
