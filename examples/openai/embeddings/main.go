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

	resp, err := client.Embeddings().Create(context.Background(), openai.EmbeddingNewParams{
		Model: "text-embedding-3-small",
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: openai.String("The quick brown fox jumps over the lazy dog"),
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "embedding error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Embedding dimensions: %d\n", len(resp.Data[0].Embedding))
	fmt.Printf("Usage tokens: %d\n", resp.Usage.TotalTokens)
}
