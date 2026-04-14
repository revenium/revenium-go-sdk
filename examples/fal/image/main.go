package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	reveniumfal "github.com/revenium/revenium-go-sdk/fal"
)

func main() {
	if err := reveniumfal.Initialize(); err != nil {
		log.Fatal(err)
	}
	client, err := reveniumfal.GetClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	result, err := client.Run(context.Background(),
		"fal-ai/flux/schnell",
		map[string]interface{}{
			"prompt":     "a futuristic cityscape at sunset, cyberpunk style",
			"image_size": "landscape_16_9",
		},
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}

	output, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println("Result:", string(output))
}
