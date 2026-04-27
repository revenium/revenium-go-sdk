package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	reveniumrunway "github.com/revenium/revenium-go-sdk/runway"
)

func main() {
	if err := reveniumrunway.Initialize(); err != nil {
		log.Fatal(err)
	}
	client, err := reveniumrunway.GetClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	result, err := client.ImageToVideo(context.Background(),
		&reveniumrunway.ImageToVideoRequest{
			PromptImage: "https://example.com/your-image.jpg",
			PromptText:  "a slow cinematic zoom out, dramatic lighting",
			Model:       "gen3a_turbo",
			Duration:    5,
		},
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}

	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Result:", string(output))
}
