package anthropic

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	smithydocument "github.com/aws/smithy-go/document"
)

const converseRemedy = "InvokeModel can carry it: leave WithBedrockUseConverse and Config.BedrockUseConverse off and REVENIUM_BEDROCK_USE_CONVERSE unset"

type converseRequest struct {
	messages         []brtypes.Message
	system           []brtypes.SystemContentBlock
	inference        *brtypes.InferenceConfiguration
	toolConfig       *brtypes.ToolConfiguration
	additionalFields map[string]interface{}
}

func converseFieldCannotCarry(name string) error {
	if invokeModelFields[name] {
		return converseCannotCarry(name)
	}
	return fmt.Errorf("%w: %s on Converse or InvokeModel", ErrBedrockCannotCarry, name)
}

func converseCannotCarry(part string) error {
	return fmt.Errorf("%w: %s on Converse; %s", ErrBedrockCannotCarry, part, converseRemedy)
}

func buildConverseRequest(params anthropic.MessageNewParams) (*converseRequest, error) {
	fields, err := jsonObjectOf(params)
	if err != nil {
		return nil, fmt.Errorf("encode request for bedrock converse: %w", err)
	}
	req := &converseRequest{additionalFields: map[string]interface{}{}}
	for _, name := range sortedKeys(fields) {
		if err := req.take(name, fields[name]); err != nil {
			return nil, err
		}
	}
	return req, nil
}

func (r *converseRequest) take(name string, raw json.RawMessage) error {
	var err error
	if anthropicOnlyFields[name] {
		return nil
	}
	switch name {
	case "messages":
		r.messages, err = converseMessages(raw)
	case "system":
		r.system, err = converseSystem(raw)
	case "max_tokens":
		err = r.takeMaxTokens(raw)
	case "temperature":
		r.inferenceConfig().Temperature, err = float32Of(raw)
	case "top_p":
		r.inferenceConfig().TopP, err = float32Of(raw)
	case "stop_sequences":
		err = json.Unmarshal(raw, &r.inferenceConfig().StopSequences)
	case "top_k", "thinking":
		r.additionalFields[name], err = valueOf(raw)
	case "tools":
		r.toolConfiguration().Tools, err = converseTools(raw)
	case "tool_choice":
		r.toolConfiguration().ToolChoice, err = converseToolChoice(raw)
	default:
		err = converseFieldCannotCarry(name)
	}
	return err
}

func (r *converseRequest) takeMaxTokens(raw json.RawMessage) error {
	var maxTokens int32
	if err := json.Unmarshal(raw, &maxTokens); err != nil {
		return err
	}
	if maxTokens != 0 {
		r.inferenceConfig().MaxTokens = aws.Int32(maxTokens)
	}
	return nil
}

func (r *converseRequest) inferenceConfig() *brtypes.InferenceConfiguration {
	if r.inference == nil {
		r.inference = &brtypes.InferenceConfiguration{}
	}
	return r.inference
}

func (r *converseRequest) toolConfiguration() *brtypes.ToolConfiguration {
	if r.toolConfig == nil {
		r.toolConfig = &brtypes.ToolConfiguration{}
	}
	return r.toolConfig
}

func (r *converseRequest) additionalModelRequestFields() document.Interface {
	if len(r.additionalFields) == 0 {
		return nil
	}
	return document.NewLazyDocument(r.additionalFields)
}

func (r *converseRequest) converseInput(modelID string) *bedrockruntime.ConverseInput {
	return &bedrockruntime.ConverseInput{
		ModelId:                      aws.String(modelID),
		Messages:                     r.messages,
		System:                       r.system,
		InferenceConfig:              r.inference,
		ToolConfig:                   r.toolConfig,
		AdditionalModelRequestFields: r.additionalModelRequestFields(),
	}
}

func (r *converseRequest) converseStreamInput(modelID string) *bedrockruntime.ConverseStreamInput {
	return &bedrockruntime.ConverseStreamInput{
		ModelId:                      aws.String(modelID),
		Messages:                     r.messages,
		System:                       r.system,
		InferenceConfig:              r.inference,
		ToolConfig:                   r.toolConfig,
		AdditionalModelRequestFields: r.additionalModelRequestFields(),
	}
}

func converseMessages(raw json.RawMessage) ([]brtypes.Message, error) {
	var messages []jsonObject
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, err
	}
	result := make([]brtypes.Message, 0, len(messages))
	for _, message := range messages {
		if err := allowOnly(message, "message", "role", "content"); err != nil {
			return nil, err
		}
		var role string
		if err := json.Unmarshal(message["role"], &role); err != nil {
			return nil, err
		}
		content, err := converseContent(message["content"])
		if err != nil {
			return nil, err
		}
		result = append(result, brtypes.Message{Role: brtypes.ConversationRole(role), Content: content})
	}
	return result, nil
}

func converseContent(raw json.RawMessage) ([]brtypes.ContentBlock, error) {
	if text, isText := plainText(raw); isText {
		return []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: text}}, nil
	}
	var blocks []jsonObject
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	var content []brtypes.ContentBlock
	for _, block := range blocks {
		converted, err := converseContentBlock(block)
		if err != nil {
			return nil, err
		}
		content = append(content, converted)
		cachePoint, err := cachePointAfter(block)
		if err != nil {
			return nil, err
		}
		if cachePoint != nil {
			content = append(content, &brtypes.ContentBlockMemberCachePoint{Value: *cachePoint})
		}
	}
	return content, nil
}

func converseContentBlock(block jsonObject) (brtypes.ContentBlock, error) {
	switch blockType := stringField(block, "type"); blockType {
	case "text":
		text, err := converseText(block)
		return &brtypes.ContentBlockMemberText{Value: text}, err
	case "image":
		image, err := converseImage(block)
		return &brtypes.ContentBlockMemberImage{Value: image}, err
	case "tool_use":
		return converseToolUse(block)
	case "tool_result":
		return converseToolResult(block)
	case "thinking":
		return converseThinking(block)
	case "redacted_thinking":
		return converseRedactedThinking(block)
	default:
		return nil, converseCannotCarry(blockType + " content block")
	}
}

func converseText(block jsonObject) (string, error) {
	if err := allowOnly(block, "text block", "type", "text", "cache_control"); err != nil {
		return "", err
	}
	return stringField(block, "text"), nil
}

func converseImage(block jsonObject) (brtypes.ImageBlock, error) {
	if err := allowOnly(block, "image block", "type", "source", "cache_control"); err != nil {
		return brtypes.ImageBlock{}, err
	}
	var source jsonObject
	if err := json.Unmarshal(block["source"], &source); err != nil {
		return brtypes.ImageBlock{}, err
	}
	if sourceType := stringField(source, "type"); sourceType != "base64" {
		return brtypes.ImageBlock{}, converseCannotCarry(sourceType + " image source")
	}
	data, err := base64.StdEncoding.DecodeString(stringField(source, "data"))
	if err != nil {
		return brtypes.ImageBlock{}, fmt.Errorf("decode image data for bedrock converse: %w", err)
	}
	return brtypes.ImageBlock{
		Format: brtypes.ImageFormat(strings.TrimPrefix(stringField(source, "media_type"), "image/")),
		Source: &brtypes.ImageSourceMemberBytes{Value: data},
	}, nil
}

func converseToolUse(block jsonObject) (brtypes.ContentBlock, error) {
	if err := allowOnly(block, "tool_use block", "type", "id", "name", "input", "cache_control"); err != nil {
		return nil, err
	}
	input, err := toolInput(block["input"])
	if err != nil {
		return nil, err
	}
	return &brtypes.ContentBlockMemberToolUse{Value: brtypes.ToolUseBlock{
		ToolUseId: aws.String(stringField(block, "id")),
		Name:      aws.String(stringField(block, "name")),
		Input:     document.NewLazyDocument(input),
	}}, nil
}

func toolInput(raw json.RawMessage) (interface{}, error) {
	if len(raw) == 0 {
		return map[string]interface{}{}, nil
	}
	return valueOf(raw)
}

func converseToolResult(block jsonObject) (brtypes.ContentBlock, error) {
	if err := allowOnly(block, "tool_result block", "type", "tool_use_id", "content", "is_error", "cache_control"); err != nil {
		return nil, err
	}
	content, err := converseToolResultContent(block["content"])
	if err != nil {
		return nil, err
	}
	status := brtypes.ToolResultStatusSuccess
	if boolField(block, "is_error") {
		status = brtypes.ToolResultStatusError
	}
	return &brtypes.ContentBlockMemberToolResult{Value: brtypes.ToolResultBlock{
		ToolUseId: aws.String(stringField(block, "tool_use_id")),
		Content:   content,
		Status:    status,
	}}, nil
}

func converseToolResultContent(raw json.RawMessage) ([]brtypes.ToolResultContentBlock, error) {
	if len(raw) == 0 {
		return []brtypes.ToolResultContentBlock{&brtypes.ToolResultContentBlockMemberText{Value: ""}}, nil
	}
	if text, isText := plainText(raw); isText {
		return []brtypes.ToolResultContentBlock{&brtypes.ToolResultContentBlockMemberText{Value: text}}, nil
	}
	var blocks []jsonObject
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	content := make([]brtypes.ToolResultContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if _, cached := block["cache_control"]; cached {
			return nil, converseCannotCarry("cache_control inside a tool_result")
		}
		converted, err := converseToolResultBlock(block)
		if err != nil {
			return nil, err
		}
		content = append(content, converted)
	}
	return content, nil
}

func converseToolResultBlock(block jsonObject) (brtypes.ToolResultContentBlock, error) {
	switch blockType := stringField(block, "type"); blockType {
	case "text":
		text, err := converseText(block)
		return &brtypes.ToolResultContentBlockMemberText{Value: text}, err
	case "image":
		image, err := converseImage(block)
		return &brtypes.ToolResultContentBlockMemberImage{Value: image}, err
	default:
		return nil, converseCannotCarry(blockType + " block inside a tool_result")
	}
}

func converseThinking(block jsonObject) (brtypes.ContentBlock, error) {
	if err := allowOnly(block, "thinking block", "type", "thinking", "signature"); err != nil {
		return nil, err
	}
	return &brtypes.ContentBlockMemberReasoningContent{
		Value: &brtypes.ReasoningContentBlockMemberReasoningText{Value: brtypes.ReasoningTextBlock{
			Text:      aws.String(stringField(block, "thinking")),
			Signature: aws.String(stringField(block, "signature")),
		}},
	}, nil
}

func converseRedactedThinking(block jsonObject) (brtypes.ContentBlock, error) {
	if err := allowOnly(block, "redacted_thinking block", "type", "data"); err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(stringField(block, "data"))
	if err != nil {
		return nil, fmt.Errorf("decode redacted_thinking data for bedrock converse: %w", err)
	}
	return &brtypes.ContentBlockMemberReasoningContent{
		Value: &brtypes.ReasoningContentBlockMemberRedactedContent{Value: data},
	}, nil
}

func converseSystem(raw json.RawMessage) ([]brtypes.SystemContentBlock, error) {
	var blocks []jsonObject
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	var system []brtypes.SystemContentBlock
	for _, block := range blocks {
		text, err := converseText(block)
		if err != nil {
			return nil, err
		}
		system = append(system, &brtypes.SystemContentBlockMemberText{Value: text})
		cachePoint, err := cachePointAfter(block)
		if err != nil {
			return nil, err
		}
		if cachePoint != nil {
			system = append(system, &brtypes.SystemContentBlockMemberCachePoint{Value: *cachePoint})
		}
	}
	return system, nil
}

func converseTools(raw json.RawMessage) ([]brtypes.Tool, error) {
	var definitions []jsonObject
	if err := json.Unmarshal(raw, &definitions); err != nil {
		return nil, err
	}
	var tools []brtypes.Tool
	for _, definition := range definitions {
		tool, err := converseTool(definition)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
		cachePoint, err := cachePointAfter(definition)
		if err != nil {
			return nil, err
		}
		if cachePoint != nil {
			tools = append(tools, &brtypes.ToolMemberCachePoint{Value: *cachePoint})
		}
	}
	return tools, nil
}

func converseTool(definition jsonObject) (brtypes.Tool, error) {
	if toolType := stringField(definition, "type"); toolType != "" && toolType != "custom" {
		return nil, converseCannotCarry(toolType + " tool")
	}
	if err := allowOnly(definition, "tool definition", "type", "name", "description", "input_schema", "cache_control"); err != nil {
		return nil, err
	}
	schema, err := valueOf(definition["input_schema"])
	if err != nil {
		return nil, err
	}
	spec := brtypes.ToolSpecification{
		Name:        aws.String(stringField(definition, "name")),
		InputSchema: &brtypes.ToolInputSchemaMemberJson{Value: document.NewLazyDocument(schema)},
	}
	if description := stringField(definition, "description"); description != "" {
		spec.Description = aws.String(description)
	}
	return &brtypes.ToolMemberToolSpec{Value: spec}, nil
}

func converseToolChoice(raw json.RawMessage) (brtypes.ToolChoice, error) {
	var choice jsonObject
	if err := json.Unmarshal(raw, &choice); err != nil {
		return nil, err
	}
	if boolField(choice, "disable_parallel_tool_use") {
		return nil, converseCannotCarry("tool_choice disable_parallel_tool_use")
	}
	switch choiceType := stringField(choice, "type"); choiceType {
	case "auto":
		return &brtypes.ToolChoiceMemberAuto{Value: brtypes.AutoToolChoice{}}, nil
	case "any":
		return &brtypes.ToolChoiceMemberAny{Value: brtypes.AnyToolChoice{}}, nil
	case "tool":
		return &brtypes.ToolChoiceMemberTool{Value: brtypes.SpecificToolChoice{Name: aws.String(stringField(choice, "name"))}}, nil
	default:
		return nil, converseCannotCarry("tool_choice " + choiceType)
	}
}

func cachePointAfter(block jsonObject) (*brtypes.CachePointBlock, error) {
	raw, present := block["cache_control"]
	if !present {
		return nil, nil
	}
	var cacheControl jsonObject
	if err := json.Unmarshal(raw, &cacheControl); err != nil {
		return nil, err
	}
	cachePoint := &brtypes.CachePointBlock{Type: brtypes.CachePointTypeDefault}
	switch ttl := brtypes.CacheTTL(stringField(cacheControl, "ttl")); ttl {
	case "":
	case brtypes.CacheTTLFiveMinutes, brtypes.CacheTTLOneHour:
		cachePoint.Ttl = ttl
	default:
		return nil, converseCannotCarry("cache_control ttl " + string(ttl))
	}
	return cachePoint, nil
}

func allowOnly(object jsonObject, what string, allowed ...string) error {
	for _, key := range sortedKeys(object) {
		if !containsString(allowed, key) {
			return converseCannotCarry(key + " in a " + what)
		}
	}
	return nil
}

// valueOf decodes raw for an AWS document, keeping each number as the exact
// literal the caller wrote; a float64 would round integers above 2^53.
func valueOf(raw json.RawMessage) (interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return withExactNumbers(value), nil
}

func withExactNumbers(value interface{}) interface{} {
	switch v := value.(type) {
	case json.Number:
		return smithydocument.Number(v)
	case map[string]interface{}:
		for key, item := range v {
			v[key] = withExactNumbers(item)
		}
	case []interface{}:
		for i, item := range v {
			v[i] = withExactNumbers(item)
		}
	}
	return value
}

func float32Of(raw json.RawMessage) (*float32, error) {
	var value float32
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return aws.Float32(value), nil
}

func plainText(raw json.RawMessage) (string, bool) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", false
	}
	return text, true
}

func stringField(object jsonObject, name string) string {
	text, _ := plainText(object[name])
	return text
}

func boolField(object jsonObject, name string) bool {
	var value bool
	if raw, present := object[name]; present {
		if err := json.Unmarshal(raw, &value); err != nil {
			return false
		}
	}
	return value
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
