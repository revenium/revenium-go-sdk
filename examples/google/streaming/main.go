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

	stream := client.Models().GenerateContentStream(
		context.Background(),
		"gemini-2.0-flash",
		[]*genai.Content{genai.NewContentFromText("Write a short poem about technology.", "user")},
		nil,
	)

	fmt.Print("Response: ")
	for resp, err := range stream {
		if err != nil {
			log.Fatal(err)
		}
		for _, candidate := range resp.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					fmt.Print(part.Text)
				}
			}
		}
	}

	fmt.Println("\n\nStreaming complete! Usage data sent to Revenium.")
}
