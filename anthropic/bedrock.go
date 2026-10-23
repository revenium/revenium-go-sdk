package anthropic

import (
	"bytes"
	"context"
	"encoding/base64"
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
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/revenium/revenium-go-sdk/core"
)

type StreamTokenAccumulator interface {
	GetAccumulatedTokens() (input, output, total int64)
	GetCacheTokens() (creation, read int64)
	GetStopReason() string
}

type BedrockClient interface {
	InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
	InvokeModelWithResponseStream(ctx context.Context, params *bedrockruntime.InvokeModelWithResponseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelWithResponseStreamOutput, error)
	Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
	ConverseStream(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error)
}

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
	if strings.HasPrefix(modelName, "arn:aws:bedrock:") {
		return modelName
	}

	if strings.Contains(modelName, "anthropic.") {
		return modelName
	}

	if config != nil && config.AWSModelARNBase != "" {
		fullARN, err := ConstructFullBedrockARN(config.AWSModelARNBase, modelName)
		if err != nil {
			log.Printf("Warning: Failed to construct Bedrock ARN: %v. Using standard format.", err)
		} else {
			return fullARN
		}
	}

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

const bedrockAnthropicVersion = "bedrock-2023-05-31"

type BedrockAdapter struct {
	config *Config
	client BedrockClient
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
	if cfg.bedrockHTTPClient != nil {
		opts = append(opts, awsconfig.WithHTTPClient(cfg.bedrockHTTPClient))
	}

	// Add credentials if provided
	if cfg.AWSAccessKeyID != "" && cfg.AWSSecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				cfg.AWSAccessKeyID,
				cfg.AWSSecretAccessKey,
				cfg.AWSSessionToken,
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
	payloadJSON, err := invokeModelBody(params)
	if err != nil {
		return nil, err
	}

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

	var bedrockResp map[string]interface{}
	if err := json.Unmarshal(invokeOutput.Body, &bedrockResp); err != nil {
		core.Debug("Failed to unmarshal Bedrock response: %v", err)
		return nil, fmt.Errorf("failed to unmarshal bedrock response: %w", err)
	}

	core.Debug("Bedrock response received successfully")

	anthropicResp := ba.TransformResponseFromBedrockFormat(bedrockResp)
	if anthropicResp == nil {
		return nil, fmt.Errorf("failed to transform bedrock response")
	}

	core.Debug("Successfully created message via Bedrock")
	return anthropicResp, nil
}

// CreateMessageStream creates a streaming message using AWS Bedrock
func (ba *BedrockAdapter) CreateMessageStream(ctx context.Context, params anthropic.MessageNewParams) (interface{}, error) {
	payloadJSON, err := invokeModelBody(params)
	if err != nil {
		return nil, err
	}

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

	wrapper := newBedrockStreamingWrapper(streamOutput, modelID)

	core.Debug("Successfully created streaming message via Bedrock")
	return wrapper, nil
}

func (ba *BedrockAdapter) CreateMessageConverse(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	modelID := GetBedrockModelID(string(params.Model), ba.config)

	request, err := buildConverseRequest(params)
	if err != nil {
		return nil, err
	}

	core.Debug("Calling Bedrock Converse API")
	out, err := ba.client.Converse(ctx, request.converseInput(modelID))
	if err != nil {
		return nil, fmt.Errorf("bedrock converse api error: %w", err)
	}

	msg := &anthropic.Message{
		Type: "message",
		Role: "assistant",
	}

	if reqID, ok := awsmiddleware.GetRequestIDMetadata(out.ResultMetadata); ok {
		msg.ID = reqID
	}

	msg.Model = anthropic.Model(recordedBedrockModel(modelID))

	if msgOut, ok := out.Output.(*brtypes.ConverseOutputMemberMessage); ok {
		content, err := anthropicContentFromConverse(msgOut.Value.Content)
		if err != nil {
			return nil, err
		}
		msg.Content = content
	}

	msg.StopReason = anthropic.StopReason(convertConverseStopReason(out.StopReason))
	if out.Usage != nil {
		msg.Usage.InputTokens = int64(aws.ToInt32(out.Usage.InputTokens))
		msg.Usage.OutputTokens = int64(aws.ToInt32(out.Usage.OutputTokens))
		msg.Usage.CacheReadInputTokens = int64(aws.ToInt32(out.Usage.CacheReadInputTokens))
		msg.Usage.CacheCreationInputTokens = int64(aws.ToInt32(out.Usage.CacheWriteInputTokens))
		msg.Usage.CacheCreation = converseCacheCreation(out.Usage)
	}

	core.Debug("Successfully created message via Bedrock Converse")
	return msg, nil
}

func (ba *BedrockAdapter) CreateMessageStreamConverse(ctx context.Context, params anthropic.MessageNewParams) (*ConverseStreamingWrapper, error) {
	modelID := GetBedrockModelID(string(params.Model), ba.config)

	request, err := buildConverseRequest(params)
	if err != nil {
		return nil, err
	}

	core.Debug("Calling Bedrock ConverseStream API")
	out, err := ba.client.ConverseStream(ctx, request.converseStreamInput(modelID))
	if err != nil {
		return nil, fmt.Errorf("bedrock conversestream api error: %w", err)
	}

	wrapper := newConverseStreamingWrapper(out, modelID)
	core.Debug("Successfully created streaming message via Bedrock ConverseStream")
	return wrapper, nil
}

func anthropicContentFromConverse(blocks []brtypes.ContentBlock) ([]anthropic.ContentBlockUnion, error) {
	converted := make([]map[string]interface{}, 0, len(blocks))
	for _, block := range blocks {
		switch b := block.(type) {
		case *brtypes.ContentBlockMemberText:
			converted = append(converted, map[string]interface{}{"type": "text", "text": b.Value})
		case *brtypes.ContentBlockMemberToolUse:
			toolUse, err := anthropicToolUse(b.Value)
			if err != nil {
				return nil, err
			}
			converted = append(converted, toolUse)
		case *brtypes.ContentBlockMemberReasoningContent:
			switch reasoning := b.Value.(type) {
			case *brtypes.ReasoningContentBlockMemberReasoningText:
				converted = append(converted, map[string]interface{}{
					"type":      "thinking",
					"thinking":  aws.ToString(reasoning.Value.Text),
					"signature": aws.ToString(reasoning.Value.Signature),
				})
			case *brtypes.ReasoningContentBlockMemberRedactedContent:
				converted = append(converted, map[string]interface{}{
					"type": "redacted_thinking",
					"data": base64.StdEncoding.EncodeToString(reasoning.Value),
				})
			}
		}
	}
	encoded, err := json.Marshal(converted)
	if err != nil {
		return nil, err
	}
	var content []anthropic.ContentBlockUnion
	err = json.Unmarshal(encoded, &content)
	return content, err
}

func anthropicToolUse(toolUse brtypes.ToolUseBlock) (map[string]interface{}, error) {
	input := json.RawMessage(`{}`)
	if toolUse.Input != nil {
		encoded, err := toolUse.Input.MarshalSmithyDocument()
		if err != nil {
			return nil, fmt.Errorf("decode bedrock tool input: %w", err)
		}
		input = encoded
	}
	return map[string]interface{}{
		"type":  "tool_use",
		"id":    aws.ToString(toolUse.ToolUseId),
		"name":  aws.ToString(toolUse.Name),
		"input": input,
	}, nil
}

func convertConverseStopReason(reason brtypes.StopReason) string {
	switch reason {
	case brtypes.StopReasonEndTurn:
		return "end_turn"
	case brtypes.StopReasonMaxTokens:
		return "max_tokens"
	case brtypes.StopReasonStopSequence:
		return "stop_sequence"
	case brtypes.StopReasonToolUse:
		return "tool_use"
	case brtypes.StopReasonContentFiltered:
		return "content_filtered"
	case brtypes.StopReasonGuardrailIntervened:
		return guardrailIntervenedStopReason
	default:
		return string(reason)
	}
}

// TransformRequestToBedrockFormat returns the InvokeModel body for params as
// Go values: the request's fields that Bedrock's Claude body accepts, plus
// the Bedrock anthropic_version. Integers that fit in int64 are int64,
// other integers stay json.Number so they keep their exact value, and other
// numbers are float64 (json.Number when outside the float64 range). It
// returns nil when params cannot be encoded or hold a field Bedrock refuses.
func (ba *BedrockAdapter) TransformRequestToBedrockFormat(params anthropic.MessageNewParams) map[string]interface{} {
	body, err := invokeModelBody(params)
	if err != nil {
		core.Debug("Failed to build the Bedrock request: %v", err)
		return nil
	}
	values, err := goValuesOf(body)
	if err != nil {
		return nil
	}
	return values
}

// invokeModelFields are the Messages API fields Bedrock's Claude InvokeModel
// body accepts; it answers any other key with a ValidationException.
var invokeModelFields = map[string]bool{
	"messages": true, "max_tokens": true, "system": true, "temperature": true, "top_p": true, "top_k": true,
	"tools": true, "tool_choice": true, "stop_sequences": true, "thinking": true, "output_config": true,
}

func invokeModelBody(params anthropic.MessageNewParams) ([]byte, error) {
	fields, err := jsonObjectOf(params)
	if err != nil {
		return nil, fmt.Errorf("encode request for bedrock: %w", err)
	}
	body := jsonObject{}
	for _, name := range sortedKeys(fields) {
		switch {
		case anthropicOnlyFields[name]:
		case invokeModelFields[name]:
			body[name] = fields[name]
		default:
			return nil, invokeModelCannotCarry(name)
		}
	}
	body["anthropic_version"] = json.RawMessage(`"` + bedrockAnthropicVersion + `"`)
	return json.Marshal(body)
}

func invokeModelCannotCarry(part string) error {
	return fmt.Errorf("%w: %s on InvokeModel, whose Claude request body has no such field", ErrBedrockCannotCarry, part)
}

func goValuesOf(encoded []byte) (map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var values map[string]interface{}
	if err := decoder.Decode(&values); err != nil {
		return nil, err
	}
	for key, value := range values {
		values[key] = withGoNumbers(value)
	}
	return values, nil
}

func withGoNumbers(value interface{}) interface{} {
	switch v := value.(type) {
	case json.Number:
		if integer, err := v.Int64(); err == nil {
			return integer
		}
		if isIntegerLiteral(v) {
			return v
		}
		if decimal, err := v.Float64(); err == nil {
			return decimal
		}
		return v
	case map[string]interface{}:
		for key, item := range v {
			v[key] = withGoNumbers(item)
		}
	case []interface{}:
		for i, item := range v {
			v[i] = withGoNumbers(item)
		}
	}
	return value
}

func isIntegerLiteral(number json.Number) bool {
	return !strings.ContainsAny(number.String(), ".eE")
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

	if stopReason, ok := bedrockResp["stop_reason"].(string); ok {
		reflect.ValueOf(msg).Elem().FieldByName("StopReason").SetString(stopReason)
	}
	if guardrailIntervened(bedrockResp) {
		msg.StopReason = guardrailIntervenedStopReason
	}

	if usage, ok := bedrockResp["usage"].(map[string]interface{}); ok {
		msg.Usage = decodeInvokeModelUsage(usage)
	}

	core.Debug("Transformed Bedrock response to Anthropic format")
	return msg
}

func decodeInvokeModelUsage(usage map[string]interface{}) anthropic.Usage {
	var decoded anthropic.Usage
	encoded, err := json.Marshal(usage)
	if err == nil {
		err = json.Unmarshal(encoded, &decoded)
	}
	if err != nil {
		core.Warn("Bedrock InvokeModel usage could not be read: %v", err)
	}
	return decoded
}

// FallbackToAnthropic falls back to Anthropic native API on Bedrock error
func (ba *BedrockAdapter) FallbackToAnthropic(ctx context.Context, params anthropic.MessageNewParams, client anthropic.Client) (*anthropic.Message, error) {
	// Call Anthropic API directly
	return client.Messages.New(ctx, params)
}

type BedrockStreamingWrapper struct {
	stream       *bedrockruntime.InvokeModelWithResponseStreamOutput
	events       <-chan brtypes.ResponseStream
	currentEvent interface{}
	currentText  string
	streamErr    error
	done         bool
	modelID      string
	startTime    time.Time
	mu           sync.Mutex

	inputTokens         int64
	outputTokens        int64
	cacheReadTokens     int64
	cacheCreationTokens int64
	cacheWrites         cacheWriteSplit
	passes              []billedPass
	stopReason          string
	guardrailIntervened bool
	stopEventSeen       bool
	reply               *streamedReply
}

func newBedrockStreamingWrapper(stream *bedrockruntime.InvokeModelWithResponseStreamOutput, modelID string) *BedrockStreamingWrapper {
	w := &BedrockStreamingWrapper{
		stream:    stream,
		modelID:   modelID,
		startTime: time.Now(),
	}
	if stream != nil && stream.GetStream() != nil {
		w.events = stream.GetStream().Events()
	}
	return w
}

func (bsw *BedrockStreamingWrapper) Next() bool {
	bsw.mu.Lock()
	defer bsw.mu.Unlock()

	if bsw.done || bsw.events == nil {
		return false
	}

	event, ok := <-bsw.events
	if !ok {
		bsw.done = true
		if bsw.stream != nil && bsw.stream.GetStream() != nil {
			bsw.streamErr = bsw.stream.GetStream().Err()
		}
		return false
	}

	bsw.currentEvent = event
	bsw.currentText = ""

	if chunk, ok := event.(*brtypes.ResponseStreamMemberChunk); ok && chunk.Value.Bytes != nil {
		var parsed map[string]interface{}
		if err := json.Unmarshal(chunk.Value.Bytes, &parsed); err == nil {
			bsw.takeStreamEvent(parsed)
		}
		if bsw.reply != nil {
			bsw.observeReply(chunk.Value.Bytes)
		}
	}

	return true
}

func (bsw *BedrockStreamingWrapper) observeReply(encoded []byte) {
	var event anthropic.MessageStreamEventUnion
	if err := event.UnmarshalJSON(encoded); err == nil {
		bsw.reply.observe(event)
	}
}

func (bsw *BedrockStreamingWrapper) captureReply() {
	bsw.mu.Lock()
	defer bsw.mu.Unlock()
	bsw.reply = newStreamedReply()
}

func (bsw *BedrockStreamingWrapper) capturedReply() (string, bool) {
	bsw.mu.Lock()
	defer bsw.mu.Unlock()
	if bsw.reply == nil {
		return "", true
	}
	return bsw.reply.text()
}

func (bsw *BedrockStreamingWrapper) takeStreamEvent(event map[string]interface{}) {
	if event["type"] == "message_stop" {
		bsw.stopEventSeen = true
	}
	if delta, ok := event["delta"].(map[string]interface{}); ok {
		if text, ok := delta["text"].(string); ok {
			bsw.currentText = text
		}
		if sr, ok := delta["stop_reason"].(string); ok && sr != "" {
			bsw.stopReason = sr
		}
	}
	for _, usage := range usageObjectsInStreamEvent(event) {
		bsw.takeCacheWrites(usage, "cache_creation_input_tokens", cacheWriteSplitOf(decodeCacheCreation(usage)))
		takeReportedCount(&bsw.inputTokens, usage, "input_tokens")
		takeReportedCount(&bsw.outputTokens, usage, "output_tokens")
		takeReportedCount(&bsw.cacheReadTokens, usage, "cache_read_input_tokens")
		if passes, reported := billedPassesInDecodedUsage(usage); reported {
			bsw.passes = passes
		}
	}
	if guardrailIntervened(event) {
		bsw.guardrailIntervened = true
	}
	if metrics, ok := event["amazon-bedrock-invocationMetrics"].(map[string]interface{}); ok {
		takeReportedCount(&bsw.inputTokens, metrics, "inputTokenCount")
		takeReportedCount(&bsw.outputTokens, metrics, "outputTokenCount")
		takeReportedCount(&bsw.cacheReadTokens, metrics, "cacheReadInputTokenCount")
		bsw.takeCacheWrites(metrics, "cacheWriteInputTokenCount", cacheWriteSplit{})
	}
}

func takeReportedCount(count *int64, source map[string]interface{}, key string) {
	if reported, ok := source[key].(float64); ok && reported > 0 {
		*count = int64(reported)
	}
}

func (bsw *BedrockStreamingWrapper) Current() interface{} {
	bsw.mu.Lock()
	defer bsw.mu.Unlock()
	return bsw.currentEvent
}

func (bsw *BedrockStreamingWrapper) Err() error {
	bsw.mu.Lock()
	defer bsw.mu.Unlock()
	return bsw.streamErr
}

func (bsw *BedrockStreamingWrapper) GetAccumulatedTokens() (input, output, total int64) {
	bsw.mu.Lock()
	defer bsw.mu.Unlock()
	return bsw.inputTokens, bsw.outputTokens, bsw.inputTokens + bsw.outputTokens
}

func (bsw *BedrockStreamingWrapper) GetCacheTokens() (creation, read int64) {
	bsw.mu.Lock()
	defer bsw.mu.Unlock()
	return bsw.cacheCreationTokens, bsw.cacheReadTokens
}

func (bsw *BedrockStreamingWrapper) takeCacheWrites(source map[string]interface{}, totalKey string, split cacheWriteSplit) {
	if split.isReported() {
		bsw.cacheWrites = split
	}
	total, ok := source[totalKey].(float64)
	if !ok || total <= 0 {
		return
	}
	if int64(total) != bsw.cacheCreationTokens && !split.isReported() {
		bsw.cacheWrites = cacheWriteSplit{}
	}
	bsw.cacheCreationTokens = int64(total)
}

func (bsw *BedrockStreamingWrapper) reportedCacheWriteSplit() cacheWriteSplit {
	bsw.mu.Lock()
	defer bsw.mu.Unlock()
	return bsw.cacheWrites
}

func (bsw *BedrockStreamingWrapper) reportedBilledPasses() []billedPass {
	bsw.mu.Lock()
	defer bsw.mu.Unlock()
	return bsw.passes
}

func (bsw *BedrockStreamingWrapper) GetStopReason() string {
	bsw.mu.Lock()
	defer bsw.mu.Unlock()
	if bsw.guardrailIntervened {
		return guardrailIntervenedStopReason
	}
	return bsw.stopReason
}

func (bsw *BedrockStreamingWrapper) reachedStopEvent() bool {
	bsw.mu.Lock()
	defer bsw.mu.Unlock()
	return bsw.stopEventSeen
}

func (bsw *BedrockStreamingWrapper) Close() error {
	bsw.mu.Lock()
	defer bsw.mu.Unlock()
	if bsw.stream != nil && bsw.stream.GetStream() != nil {
		return bsw.stream.GetStream().Close()
	}
	return nil
}

type ConverseStreamingWrapper struct {
	stream       *bedrockruntime.ConverseStreamOutput
	events       <-chan brtypes.ConverseStreamOutput
	currentEvent interface{}
	currentText  string
	streamErr    error
	done         bool
	modelID      string
	requestID    string
	startTime    time.Time
	mu           sync.Mutex

	inputTokens         int64
	outputTokens        int64
	cacheReadTokens     int64
	cacheCreationTokens int64
	cacheWrites         cacheWriteSplit
	stopReason          string
	stopEventSeen       bool
	reply               *streamedReply
}

func newConverseStreamingWrapper(stream *bedrockruntime.ConverseStreamOutput, modelID string) *ConverseStreamingWrapper {
	w := &ConverseStreamingWrapper{
		stream:    stream,
		modelID:   modelID,
		startTime: time.Now(),
	}
	if stream != nil && stream.GetStream() != nil {
		w.events = stream.GetStream().Events()
	}
	if reqID, ok := awsmiddleware.GetRequestIDMetadata(stream.ResultMetadata); ok {
		w.requestID = reqID
	}
	return w
}

func (csw *ConverseStreamingWrapper) Next() bool {
	csw.mu.Lock()
	defer csw.mu.Unlock()

	if csw.done || csw.events == nil {
		return false
	}

	event, ok := <-csw.events
	if !ok {
		csw.done = true
		if csw.stream != nil && csw.stream.GetStream() != nil {
			csw.streamErr = csw.stream.GetStream().Err()
		}
		return false
	}

	csw.currentEvent = event
	csw.currentText = ""

	switch ev := event.(type) {
	case *brtypes.ConverseStreamOutputMemberContentBlockDelta:
		if textDelta, ok := ev.Value.Delta.(*brtypes.ContentBlockDeltaMemberText); ok {
			csw.currentText = textDelta.Value
		}
	case *brtypes.ConverseStreamOutputMemberMetadata:
		if ev.Value.Usage != nil {
			if ev.Value.Usage.InputTokens != nil {
				csw.inputTokens = int64(*ev.Value.Usage.InputTokens)
			}
			if ev.Value.Usage.OutputTokens != nil {
				csw.outputTokens = int64(*ev.Value.Usage.OutputTokens)
			}
			if ev.Value.Usage.CacheReadInputTokens != nil {
				csw.cacheReadTokens = int64(*ev.Value.Usage.CacheReadInputTokens)
			}
			if ev.Value.Usage.CacheWriteInputTokens != nil {
				csw.cacheCreationTokens = int64(*ev.Value.Usage.CacheWriteInputTokens)
			}
			csw.cacheWrites = cacheWriteSplitOf(converseCacheCreation(ev.Value.Usage))
		}
	case *brtypes.ConverseStreamOutputMemberMessageStop:
		csw.stopReason = convertConverseStopReason(ev.Value.StopReason)
		csw.stopEventSeen = true
	}
	if csw.reply != nil {
		csw.reply.observeConverse(event)
	}

	return true
}

func (csw *ConverseStreamingWrapper) captureReply() {
	csw.mu.Lock()
	defer csw.mu.Unlock()
	csw.reply = newStreamedReply()
}

func (csw *ConverseStreamingWrapper) capturedReply() (string, bool) {
	csw.mu.Lock()
	defer csw.mu.Unlock()
	if csw.reply == nil {
		return "", true
	}
	return csw.reply.text()
}

func (csw *ConverseStreamingWrapper) GetAccumulatedTokens() (input, output, total int64) {
	csw.mu.Lock()
	defer csw.mu.Unlock()
	return csw.inputTokens, csw.outputTokens, csw.inputTokens + csw.outputTokens
}

func (csw *ConverseStreamingWrapper) GetCacheTokens() (creation, read int64) {
	csw.mu.Lock()
	defer csw.mu.Unlock()
	return csw.cacheCreationTokens, csw.cacheReadTokens
}

func (csw *ConverseStreamingWrapper) reportedCacheWriteSplit() cacheWriteSplit {
	csw.mu.Lock()
	defer csw.mu.Unlock()
	return csw.cacheWrites
}

func (csw *ConverseStreamingWrapper) GetStopReason() string {
	csw.mu.Lock()
	defer csw.mu.Unlock()
	return csw.stopReason
}

func (csw *ConverseStreamingWrapper) reachedStopEvent() bool {
	csw.mu.Lock()
	defer csw.mu.Unlock()
	return csw.stopEventSeen
}

func (csw *ConverseStreamingWrapper) Current() interface{} {
	csw.mu.Lock()
	defer csw.mu.Unlock()
	return csw.currentEvent
}

func (csw *ConverseStreamingWrapper) Err() error {
	csw.mu.Lock()
	defer csw.mu.Unlock()
	return csw.streamErr
}

func (csw *ConverseStreamingWrapper) Close() error {
	csw.mu.Lock()
	defer csw.mu.Unlock()
	if csw.stream != nil && csw.stream.GetStream() != nil {
		return csw.stream.GetStream().Close()
	}
	return nil
}

func usageObjectsInStreamEvent(event map[string]interface{}) []map[string]interface{} {
	var usages []map[string]interface{}
	if message, ok := event["message"].(map[string]interface{}); ok {
		if usage, ok := message["usage"].(map[string]interface{}); ok {
			usages = append(usages, usage)
		}
	}
	if usage, ok := event["usage"].(map[string]interface{}); ok {
		usages = append(usages, usage)
	}
	return usages
}
