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

	stream := client.Models().GenerateContentStream(
		context.Background(),
		"gemini-2.0-flash",
		[]*genai.Content{genai.NewContentFromText("Write a haiku about Go programming", "user")},
		nil,
	)

	for resp, err := range stream {
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nstream error: %v\n", err)
			os.Exit(1)
		}
		for _, candidate := range resp.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					fmt.Print(part.Text)
				}
			}
		}
	}

	fmt.Println()
}
