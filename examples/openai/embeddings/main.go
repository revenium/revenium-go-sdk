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

	resp, err := client.Embeddings().Create(context.Background(), openai.EmbeddingNewParams{
		Model: "text-embedding-3-small",
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: openai.String("Revenium provides AI usage tracking and monetization."),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Embedding dimensions:", len(resp.Data[0].Embedding))
	fmt.Println("Model:", resp.Model)
	fmt.Printf("Usage: prompt=%d total=%d\n", resp.Usage.PromptTokens, resp.Usage.TotalTokens)
}
