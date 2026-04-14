package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	reveniumfal "github.com/revenium/revenium-go-sdk/fal"
)

func main() {
	if err := reveniumfal.Initialize(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize: %v\n", err)
		os.Exit(1)
	}

	client, err := reveniumfal.GetClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	result, err := client.Run(context.Background(),
		"fal-ai/flux/schnell",
		map[string]interface{}{
			"prompt":     "a futuristic cityscape at sunset",
			"image_size": "landscape_16_9",
		},
		nil,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generation error: %v\n", err)
		os.Exit(1)
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(output))
}
