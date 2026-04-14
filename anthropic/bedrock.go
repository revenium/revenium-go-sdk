package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	"github.com/revenium/revenium-go-sdk/core"
)

// ValidateBedrockBaseARN validates that the AWS_MODEL_ARN_ID has the correct base format
// Expected format: arn:aws:bedrock:{region}:{account-id}
// Returns error if the ARN is too long, too short, or has incorrect format
func ValidateBedrockBaseARN(arnBase string) error {
	if arnBase == "" {
		return errors.New("AWS_MODEL_ARN_ID is empty")
	}

	// Expected format: arn:aws:bedrock:{region}:{account-id}
	// Example: arn:aws:bedrock:us-east-1:123456789012
	arnPattern := regexp.MustCompile(`^arn:aws:bedrock:[a-z]{2}-[a-z]+-\d+:\d{12}$`)

	if !arnPattern.MatchString(arnBase) {
		// Check if it's too long (contains inference-profile or model info)
		if strings.Contains(arnBase, "inference-profile") || strings.Contains(arnBase, "anthropic") {
			return fmt.Errorf("AWS_MODEL_ARN_ID is too long. Expected format: arn:aws:bedrock:{region}:{account-id}, got: %s", arnBase)
		}

		// Check if it's too short
		parts := strings.Split(arnBase, ":")
		if len(parts) < 5 {
			return fmt.Errorf("AWS_MODEL_ARN_ID is too short. Expected format: arn:aws:bedrock:{region}:{account-id}, got: %s", arnBase)
		}

		// Generic format error
		return fmt.Errorf("AWS_MODEL_ARN_ID has incorrect format. Expected: arn:aws:bedrock:{region}:{account-id}, got: %s", arnBase)
	}

	return nil
}

// ConstructFullBedrockARN constructs the full Bedrock ARN from base ARN and model name
// Base ARN format: arn:aws:bedrock:{region}:{account-id}
// Model name: {model-name} (e.g., from Anthropic SDK)
// Result: arn:aws:bedrock:{region}:{account-id}:inference-profile/us.anthropic.{model}-v1:0
func ConstructFullBedrockARN(arnBase string, modelName string) (string, error) {
	// Validate base ARN first
	if err := ValidateBedrockBaseARN(arnBase); err != nil {
		return "", err
	}

	if modelName == "" {
		return "", errors.New("model name is required to construct full Bedrock ARN")
	}

	// Construct full ARN
	fullARN := fmt.Sprintf("%s:inference-profile/us.anthropic.%s-v1:0", arnBase, modelName)
	return fullARN, nil
}

// GetBedrockModelID converts Anthropic model names to Bedrock ARNs
// If AWS_MODEL_ARN_ID is configured, it constructs the full ARN automatically
// Otherwise, it uses the standard Bedrock format: anthropic.{model_name}
// If the input is already a full ARN, it returns it unchanged.
func GetBedrockModelID(modelName string, config *Config) string {
	// If it's already a full ARN, return as-is
	if strings.HasPrefix(modelName, "arn:aws:bedrock:") {
		return modelName
	}

	// If AWS_MODEL_ARN_ID is configured, construct full ARN
	if config != nil && config.AWSModelARNBase != "" {
		fullARN, err := ConstructFullBedrockARN(config.AWSModelARNBase, modelName)
		if err != nil {
			// Log error but continue with fallback
			log.Printf("Warning: Failed to construct Bedrock ARN: %v. Using standard format.", err)
		} else {
			return fullARN
		}
	}

	// If it already has the anthropic. prefix, return as-is
	if strings.HasPrefix(modelName, "anthropic.") {
		return modelName
	}

	// Otherwise, add the standard Bedrock format prefix
	return fmt.Sprintf("anthropic.%s", modelName)
}

// ConvertBedrockARNToAnthropicModel converts a Bedrock ARN or model ID to an Anthropic model name
// This is used when falling back from Bedrock to Anthropic API
// Examples:
//   - arn:aws:bedrock:us-east-1:123456789:inference-profile/us.anthropic.{model-name}-v1:0 -> {model-name}, nil
//   - anthropic.{model-name}-v2:0 -> {model-name}, nil
//   - {model-name} -> {model-name}, nil (passthrough)
//   - invalid-format -> "", error
func ConvertBedrockARNToAnthropicModel(bedrockModel string) (string, error) {
	// If it's already a standard Anthropic model name (no ARN or prefix), return as-is
	if !strings.Contains(bedrockModel, "arn:aws:bedrock") && !strings.HasPrefix(bedrockModel, "anthropic.") && !strings.Contains(bedrockModel, "inference-profile") {
		return bedrockModel, nil
	}

	// Extract model name from ARN format
	// ARN format: arn:aws:bedrock:region:account:inference-profile/us.anthropic.{model-name}-v1:0
	if strings.Contains(bedrockModel, "arn:aws:bedrock") {
		// Split by "/" to get the inference profile part
		parts := strings.Split(bedrockModel, "/")
		if len(parts) >= 2 {
			// Get the last part which contains the model identifier
			modelPart := parts[len(parts)-1]

			// Remove region prefix (e.g., "us.anthropic." or "eu.anthropic.")
			modelPart = strings.TrimPrefix(modelPart, "us.anthropic.")
			modelPart = strings.TrimPrefix(modelPart, "eu.anthropic.")
			modelPart = strings.TrimPrefix(modelPart, "ap.anthropic.")

			// Remove version suffix (e.g., "-v1:0" or ":0")
			modelPart = strings.Split(modelPart, "-v")[0]
			modelPart = strings.Split(modelPart, ":")[0]

			return modelPart, nil
		}
	}

	// Handle Bedrock model ID format: anthropic.{model-name}-v2:0
	if strings.HasPrefix(bedrockModel, "anthropic.") {
		modelName := strings.TrimPrefix(bedrockModel, "anthropic.")
		// Remove version suffix
		modelName = strings.Split(modelName, "-v")[0]
		modelName = strings.Split(modelName, ":")[0]
		return modelName, nil
	}

	// If we can't parse it, return an error instead of a hardcoded default
	return "", fmt.Errorf("could not parse Bedrock model ID '%s': unrecognized format", bedrockModel)
}

// BedrockAdapter handles AWS Bedrock integration
type BedrockAdapter struct {
	config *Config
	client *bedrockruntime.Client
	awsCfg aws.Config
}

// NewBedrockAdapter creates a new Bedrock adapter
func NewBedrockAdapter(cfg *Config) (*BedrockAdapter, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	// Load AWS configuration
	awsCfg, err := loadAWSConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create Bedrock Runtime client
	client := bedrockruntime.NewFromConfig(awsCfg)

	adapter := &BedrockAdapter{
		config: cfg,
		client: client,
		awsCfg: awsCfg,
	}

	core.Debug("Bedrock adapter initialized successfully")
	return adapter, nil
}

// loadAWSConfig loads AWS configuration from environment or config
func loadAWSConfig(cfg *Config) (aws.Config, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.AWSRegion),
	}

	// Add credentials if provided
	if cfg.AWSAccessKeyID != "" && cfg.AWSSecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				cfg.AWSAccessKeyID,
				cfg.AWSSecretAccessKey,
				"", // session token (optional)
			),
		))
		core.Debug("Using static AWS credentials")
	} else if cfg.AWSProfile != "" {
		// Use named profile
		opts = append(opts, awsconfig.WithSharedConfigProfile(cfg.AWSProfile))
		core.Debug("Using AWS profile")
	} else {
		// Use default credentials chain (env vars, IAM role, etc.)
		core.Debug("Using default AWS credentials chain")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return awsCfg, nil
}

// CreateMessage creates a message using AWS Bedrock
func (ba *BedrockAdapter) CreateMessage(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	// Step 1: Transform Anthropic request to Bedrock format
	bedrockPayload := ba.TransformRequestToBedrockFormat(params)

	// Step 2: Marshal payload to JSON
	payloadJSON, err := json.Marshal(bedrockPayload)
	if err != nil {
		core.Debug("Failed to marshal Bedrock payload: %v", err)
		return nil, fmt.Errorf("failed to marshal bedrock payload: %w", err)
	}

	// Log at DEBUG level (without showing payload content for security)
	core.Debug("Bedrock payload prepared")

	// Step 3: Call Bedrock API with model mapping
	modelID := GetBedrockModelID(string(params.Model), ba.config)
	core.Debug("Calling Bedrock API")

	invokeOutput, err := ba.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		Body:        payloadJSON,
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
	})

	if err != nil {
		core.Debug("Bedrock API error: %v", err)
		return nil, fmt.Errorf("bedrock api error: %w", err)
	}

	// Step 4: Parse Bedrock response
	var bedrockResp map[string]interface{}
	if err := json.Unmarshal(invokeOutput.Body, &bedrockResp); err != nil {
		core.Debug("Failed to unmarshal Bedrock response: %v", err)
		return nil, fmt.Errorf("failed to unmarshal bedrock response: %w", err)
	}

	// Log the response at DEBUG level
	core.Debug("Bedrock response received successfully")

	// Step 5: Transform Bedrock response to Anthropic format
	anthropicResp := ba.TransformResponseFromBedrockFormat(bedrockResp)
	if anthropicResp == nil {
		return nil, fmt.Errorf("failed to transform bedrock response")
	}

	core.Debug("Successfully created message via Bedrock")
	return anthropicResp, nil
}

// CreateMessageStream creates a streaming message using AWS Bedrock
func (ba *BedrockAdapter) CreateMessageStream(ctx context.Context, params anthropic.MessageNewParams) (interface{}, error) {
	// Step 1: Transform Anthropic request to Bedrock format
	bedrockPayload := ba.TransformRequestToBedrockFormat(params)

	// Step 2: Marshal payload to JSON
	payloadJSON, err := json.Marshal(bedrockPayload)
	if err != nil {
		core.Debug("Failed to marshal Bedrock payload: %v", err)
		return nil, fmt.Errorf("failed to marshal bedrock payload: %w", err)
	}

	// Step 3: Call Bedrock streaming API with model mapping
	modelID := GetBedrockModelID(string(params.Model), ba.config)
	core.Debug("Calling Bedrock streaming API")

	streamOutput, err := ba.client.InvokeModelWithResponseStream(ctx, &bedrockruntime.InvokeModelWithResponseStreamInput{
		ModelId:     aws.String(modelID),
		Body:        payloadJSON,
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
	})

	if err != nil {
		core.Debug("Bedrock streaming API error: %v", err)
		return nil, fmt.Errorf("bedrock streaming api error: %w", err)
	}

	// Step 4: Create a streaming wrapper for Bedrock events
	wrapper := &BedrockStreamingWrapper{
		stream:    streamOutput,
		modelID:   modelID,
		startTime: time.Now(),
	}

	core.Debug("Successfully created streaming message via Bedrock")
	return wrapper, nil
}

// TransformRequestToBedrockFormat converts an Anthropic request to Bedrock format
func (ba *BedrockAdapter) TransformRequestToBedrockFormat(params anthropic.MessageNewParams) map[string]interface{} {
	// Build Bedrock request payload
	payload := map[string]interface{}{
		"messages":          transformMessages(params.Messages),
		"anthropic_version": "bedrock-2023-05-31",
	}

	// Add optional parameters if provided
	if params.MaxTokens != 0 {
		payload["max_tokens"] = params.MaxTokens
	}

	// Note: Temperature, TopP, TopK use param.Opt type in Anthropic SDK
	// We'll add them if they're set (non-zero values)
	// This is a simplified approach - in production you'd need to check the Opt type properly

	if len(params.StopSequences) > 0 {
		payload["stop_sequences"] = params.StopSequences
	}

	core.Debug("Transformed Anthropic request to Bedrock format")
	return payload
}

// transformMessages converts Anthropic messages to Bedrock format
func transformMessages(messages []anthropic.MessageParam) []map[string]interface{} {
	var bedrockMessages []map[string]interface{}

	for _, msg := range messages {
		bedrockMsg := map[string]interface{}{}

		// Determine role
		bedrockMsg["role"] = string(msg.Role)

		// Extract content from message content blocks using JSON marshaling
		content := []map[string]interface{}{}

		// Marshal the Content to JSON and unmarshal to extract the structure
		if msg.Content != nil {
			contentJSON, err := json.Marshal(msg.Content)
			if err == nil {
				var contentBlocks []map[string]interface{}
				if err := json.Unmarshal(contentJSON, &contentBlocks); err == nil {
					content = contentBlocks
				}
			}
		}

		// Fallback if no content was extracted
		if len(content) == 0 {
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": "",
			})
		}

		bedrockMsg["content"] = content
		bedrockMessages = append(bedrockMessages, bedrockMsg)
	}

	return bedrockMessages
}

// TransformResponseFromBedrockFormat converts a Bedrock response to Anthropic format
// Note: This creates a basic Message structure. Full type compatibility requires
// using reflection or the Anthropic SDK's internal constructors.
func (ba *BedrockAdapter) TransformResponseFromBedrockFormat(bedrockResp map[string]interface{}) *anthropic.Message {
	if bedrockResp == nil {
		return nil
	}

	// Create Anthropic Message response
	msg := &anthropic.Message{
		Type: "message",
		Role: "assistant",
	}

	// Extract ID
	if id, ok := bedrockResp["id"].(string); ok {
		msg.ID = id
	}

	// Extract model (convert to anthropic.Model type)
	if model, ok := bedrockResp["model"].(string); ok {
		reflect.ValueOf(msg).Elem().FieldByName("Model").SetString(model)
	}

	// Extract content - store as raw interface{} and let reflection handle it
	if contentArray, ok := bedrockResp["content"].([]interface{}); ok {
		// Convert to JSON and back to preserve the structure
		contentJSON, _ := json.Marshal(contentArray)

		// Get the Content field type and create a value of that type
		contentField := reflect.ValueOf(msg).Elem().FieldByName("Content")
		if contentField.IsValid() && contentField.CanSet() {
			// Create a new value of the correct type
			contentValue := reflect.New(contentField.Type())
			if err := json.Unmarshal(contentJSON, contentValue.Interface()); err == nil {
				contentField.Set(contentValue.Elem())
			}
		}
	}

	// Extract stop reason
	if stopReason, ok := bedrockResp["stop_reason"].(string); ok {
		reflect.ValueOf(msg).Elem().FieldByName("StopReason").SetString(convertBedrockStopReason(stopReason))
	}

	// Extract usage information
	if usage, ok := bedrockResp["usage"].(map[string]interface{}); ok {
		inputTokens := int64(0)
		outputTokens := int64(0)

		if it, ok := usage["input_tokens"].(float64); ok {
			inputTokens = int64(it)
		}
		if ot, ok := usage["output_tokens"].(float64); ok {
			outputTokens = int64(ot)
		}

		// Set usage fields via reflection
		usageField := reflect.ValueOf(msg).Elem().FieldByName("Usage")
		if usageField.IsValid() && usageField.CanSet() {
			usageField.FieldByName("InputTokens").SetInt(inputTokens)
			usageField.FieldByName("OutputTokens").SetInt(outputTokens)
		}
	}

	core.Debug("Transformed Bedrock response to Anthropic format")
	return msg
}

// convertBedrockStopReason converts Bedrock stop reason to Anthropic format
func convertBedrockStopReason(bedrockReason string) string {
	switch bedrockReason {
	case "end_turn":
		return "end_turn"
	case "max_tokens":
		return "max_tokens"
	case "stop_sequence":
		return "stop_sequence"
	default:
		return "end_turn"
	}
}

// FallbackToAnthropic falls back to Anthropic native API on Bedrock error
func (ba *BedrockAdapter) FallbackToAnthropic(ctx context.Context, params anthropic.MessageNewParams, client anthropic.Client) (*anthropic.Message, error) {
	// Call Anthropic API directly
	return client.Messages.New(ctx, params)
}

// BedrockStreamingWrapper wraps Bedrock streaming response for compatibility with Anthropic streaming
type BedrockStreamingWrapper struct {
	stream    *bedrockruntime.InvokeModelWithResponseStreamOutput
	modelID   string
	startTime time.Time
	mu        sync.Mutex
}

// Next returns the next event from the Bedrock stream
func (bsw *BedrockStreamingWrapper) Next(ctx context.Context) bool {
	if bsw.stream == nil {
		return false
	}

	// Read next event from stream
	// The EventStream field contains the streaming events
	// This is a simplified implementation - in production you'd need to properly
	// handle the event stream from Bedrock using the EventStream interface
	return false
}

// Current returns the current event
func (bsw *BedrockStreamingWrapper) Current() interface{} {
	// Return current event
	return nil
}

// Err returns any error that occurred during streaming
func (bsw *BedrockStreamingWrapper) Err() error {
	// Return error if any
	return nil
}

// Close closes the stream
func (bsw *BedrockStreamingWrapper) Close() error {
	if bsw.stream != nil {
		// Use reflection to access the private eventStream field
		streamField := reflect.ValueOf(bsw.stream).Elem().FieldByName("eventStream")
		if streamField.IsValid() {
			// Try to call Close method if it exists
			if closeMethod := streamField.MethodByName("Close"); closeMethod.IsValid() {
				result := closeMethod.Call(nil)
				if len(result) > 0 {
					if err, ok := result[0].Interface().(error); ok {
						return err
					}
				}
			}
		}
	}
	return nil
}

