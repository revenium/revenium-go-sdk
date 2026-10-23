package enforcement

import (
	"context"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
)

func TestNewEvalContext(t *testing.T) {
	withMetadata := func(md map[string]interface{}) context.Context {
		return core.WithUsageMetadata(context.Background(), md)
	}

	cases := []struct {
		name string
		ctx  context.Context
		want EvalContext
	}{
		{
			name: "subscriber set on the context keeps both id and email",
			ctx: core.WithSubscriber(context.Background(), &core.Subscriber{
				ID: "sub_123", Email: "alice@example.com",
			}),
			want: EvalContext{SubscriberID: "sub_123", SubscriberEmail: "alice@example.com"},
		},
		{
			name: "subscriber in usage metadata is used when the context has none",
			ctx: withMetadata(map[string]interface{}{
				"subscriber": map[string]interface{}{
					"id":         "sub_9",
					"email":      "bob@example.com",
					"credential": map[string]interface{}{"name": "prod-key", "value": "sk-live"},
				},
			}),
			want: EvalContext{SubscriberID: "sub_9", SubscriberEmail: "bob@example.com", Credential: "prod-key"},
		},
		{
			name: "context subscriber wins over the metadata subscriber as one identity",
			ctx: core.WithSubscriber(withMetadata(map[string]interface{}{
				"subscriber": map[string]interface{}{"id": "sub_9", "email": "bob@example.com"},
			}), &core.Subscriber{Email: "alice@example.com"}),
			want: EvalContext{SubscriberEmail: "alice@example.com"},
		},
		{
			name: "context subscriber keeps the metadata credential",
			ctx: core.WithSubscriber(withMetadata(map[string]interface{}{
				"subscriber": map[string]interface{}{
					"id":         "sub_9",
					"credential": map[string]interface{}{"name": "prod-key"},
				},
			}), &core.Subscriber{Email: "alice@example.com"}),
			want: EvalContext{SubscriberEmail: "alice@example.com", Credential: "prod-key"},
		},
		{
			name: "credential value is read when it has no name",
			ctx: withMetadata(map[string]interface{}{
				"subscriber": map[string]interface{}{"credential": map[string]interface{}{"value": "key-alias"}},
			}),
			want: EvalContext{Credential: "key-alias", credentialValueOnly: true},
		},
		{
			name: "request dimensions come from the usage metadata",
			ctx: withMetadata(map[string]interface{}{
				"productName": "assistant", "organizationName": "acme", "agent": "batch-bot", "taskType": "summarize",
			}),
			want: EvalContext{ProductName: "assistant", Organization: "acme", Agent: "batch-bot", TaskType: "summarize"},
		},
		{
			name: "product and organization ids stand in for missing names, as on the usage record",
			ctx:  withMetadata(map[string]interface{}{"productId": "prod-1", "organizationId": "org-1"}),
			want: EvalContext{ProductName: "prod-1", Organization: "org-1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.want
			want.Model, want.Provider = "gpt-4.1-nano", "OPENAI"
			assert.Equal(t, want, NewEvalContext(tc.ctx, "gpt-4.1-nano", "OPENAI"))
		})
	}
}
