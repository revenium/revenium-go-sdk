package enforcement

import (
	"context"
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

// EvalContext carries the identifiers used to match enforcement rules
// against an in-flight request. Each field holds the value the platform
// reads for the dimension of the same name; an empty field means the
// request carries no value for it.
type EvalContext struct {
	SubscriberID    string
	SubscriberEmail string
	ProductName     string
	Model           string
	Provider        string
	Agent           string
	Organization    string
	TaskType        string
	Credential      string

	credentialOpaque      bool
	credentialValueOnly   bool
	servedModelMayBeDated bool
}

// NewEvalContext reads the request's identifiers from ctx the way they
// reach the usage record: the subscriber set with core.WithSubscriber, or
// else the `subscriber` entry of the usage metadata, plus the product,
// organization, agent and task type from the usage metadata.
func NewEvalContext(ctx context.Context, model, provider string) EvalContext {
	return NewEvalContextFromMetadata(ctx, core.GetUsageMetadata(ctx), model, provider)
}

// NewEvalContextFromMetadata builds the context from an explicit metadata map
// for call sites whose usage record is built from merged or typed metadata
// rather than from the context's usage metadata alone. The typed subscriber
// set with core.WithSubscriber still takes precedence, as it does in records.
func NewEvalContextFromMetadata(ctx context.Context, metadata map[string]interface{}, model, provider string) EvalContext {
	subscriber := metadataMap(metadata, "subscriber")
	credential, opaque := core.SubscriberCredential(subscriber)
	ec := EvalContext{
		Model:        model,
		Provider:     provider,
		ProductName:  firstString(metadata, "productName", "productId"),
		Organization: firstString(metadata, "organizationName", "organizationId"),
		Agent:        firstString(metadata, "agent"),
		TaskType:     firstString(metadata, "taskType"),
		Credential:   firstString(credential, "name", "value"),

		credentialOpaque:    opaque,
		credentialValueOnly: firstString(credential, "name") == "" && firstString(credential, "value") != "",
	}
	if sub := core.GetSubscriber(ctx); sub != nil && (sub.ID != "" || sub.Email != "") {
		ec.SubscriberID, ec.SubscriberEmail = sub.ID, sub.Email
	} else {
		ec.SubscriberID, ec.SubscriberEmail = firstString(subscriber, "id"), firstString(subscriber, "email")
	}
	return ec.withCredentialAlias(firstString(metadata, "credentialAlias"))
}

// ServedModelMayBeDated marks a call to a model the provider can serve under a
// snapshot date the request did not name (OpenAI serves gpt-4.1-nano as
// gpt-4.1-nano-2025-04-14), so the platform may meter that dated name for the
// call, and MODEL filters judge it against every name the platform may meter.
// An unmarked call (fal, Runway, Ollama, Groq, Perplexity, Google, Bedrock,
// OpenAI images and audio, Azure chat recorded under its deployment name) is
// recorded under exactly the requested name, so MODEL filters compare that name
// exactly and an IS_NOT filter naming it exempts the call.
func (ec EvalContext) ServedModelMayBeDated() EvalContext {
	ec.servedModelMayBeDated = true
	return ec
}

// withCredentialAlias names the credential by alias when the record would:
// see core.CredentialAliasFor and core.SubscriberCredential.
func (ec EvalContext) withCredentialAlias(alias string) EvalContext {
	if ec.credentialOpaque {
		return ec
	}
	if alias = core.CredentialAliasFor(alias, ec.SubscriberID, ec.SubscriberEmail, ec.Credential); alias != "" {
		ec.Credential = alias
	}
	return ec
}

func metadataMap(metadata map[string]interface{}, key string) map[string]interface{} {
	nested, _ := metadata[key].(map[string]interface{})
	return nested
}

func firstString(metadata map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if s, ok := metadata[key].(string); ok && !core.IsBlank(s) {
			return s
		}
	}
	return ""
}

func (ec EvalContext) subscriberValue() subscriberValue {
	if strings.TrimSpace(ec.SubscriberEmail) != "" {
		return subscriberValue{value: ec.SubscriberEmail, fromEmail: true}
	}
	return subscriberValue{value: ec.SubscriberID}
}

// groupKeys are the keys a caller's entry in a per-person breakdown may be
// filed under: the platform files spend under the subscriber id when there
// is one and under the normalized email otherwise, and a caller may send
// only one of the two.
func (ec EvalContext) groupKeys() []string {
	keys := make([]string, 0, 3)
	for _, key := range []string{ec.SubscriberID, normalizeEmail(ec.SubscriberEmail), ec.SubscriberEmail} {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func (ec EvalContext) caseFolded() EvalContext {
	for _, field := range []*string{&ec.Model, &ec.Provider, &ec.Agent, &ec.TaskType, &ec.ProductName, &ec.Organization, &ec.Credential} {
		*field = strings.ToLower(*field)
	}
	return ec
}

func (ec EvalContext) valueOf(dimension Dimension) (string, bool) {
	switch dimension {
	case DimensionModel:
		return ec.Model, true
	case DimensionProduct:
		return ec.ProductName, true
	case DimensionAgent:
		return ec.Agent, true
	case DimensionTaskType:
		return ec.TaskType, true
	case DimensionOrganization:
		return ec.Organization, true
	case DimensionCredential:
		return ec.Credential, true
	default:
		return "", false
	}
}
