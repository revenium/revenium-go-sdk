package main

import (
	"context"
	"fmt"
	"log"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/revenium/revenium-go-sdk/core"
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

	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
		"traceId":              fmt.Sprintf("session-%d", time.Now().UnixMilli()),
		"organizationName":     "acme-corp",
		"subscriptionId":       "plan-enterprise-2024",
		"productName":          "ai-assistant-pro",
		"taskType":             "doc-summary",
		"agent":                "customer-support",
		"responseQualityScore": 0.95,
	})

	ctx = core.WithSubscriber(ctx, &core.Subscriber{
		ID:    "user-123",
		Email: "user@example.com",
	})

	resp, err := client.Chat().Completions().New(ctx, openai.ChatCompletionNewParams{
		Model: "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("Summarize the benefits of API monetization."),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:", resp.Choices[0].Message.Content)
	fmt.Printf("\nUsage: prompt=%d completion=%d total=%d\n",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
}
