package main

import (
	"context"
	"fmt"
	"log"

	reveniumgoogle "github.com/revenium/revenium-go-sdk/google"
	"google.golang.org/genai"
)

func main() {
	if err := reveniumgoogle.Initialize(); err != nil {
		log.Fatal(err)
	}
	client, err := reveniumgoogle.GetClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	resp, err := client.Models().GenerateContent(
		context.Background(),
		"gemini-2.0-flash",
		[]*genai.Content{genai.NewContentFromText("Explain the concept of middleware in software architecture in 2-3 sentences.", "user")},
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}

	for _, candidate := range resp.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				fmt.Println("Response:", part.Text)
			}
		}
	}

	fmt.Println("\nModel:", resp.ModelVersion)
	if resp.UsageMetadata != nil {
		fmt.Printf("Usage: prompt=%d candidates=%d total=%d\n",
			resp.UsageMetadata.PromptTokenCount, resp.UsageMetadata.CandidatesTokenCount, resp.UsageMetadata.TotalTokenCount)
	}
}
