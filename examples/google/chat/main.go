package main

import (
	"context"
	"fmt"
	"os"

	reveniumgoogle "github.com/revenium/revenium-go-sdk/google"
	"google.golang.org/genai"
)

func main() {
	if err := reveniumgoogle.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize: %v\n", err)
		os.Exit(1)
	}

	client, err := reveniumgoogle.GetClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	resp, err := client.Models().GenerateContent(
		context.Background(),
		"gemini-2.0-flash",
		[]*genai.Content{genai.NewContentFromText("What is the capital of France?", "user")},
		nil,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generation error: %v\n", err)
		os.Exit(1)
	}

	for _, candidate := range resp.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				fmt.Println(part.Text)
			}
		}
	}
}
