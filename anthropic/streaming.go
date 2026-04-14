package anthropic

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/revenium/revenium-go-sdk/core/metering"
)

type StreamingWrapper struct {
	stream         interface{}
	typedStream    *ssestream.Stream[anthropic.MessageStreamEventUnion]
	config         *Config
	metadata       map[string]interface{}
	startTime      time.Time
	firstTokenTime *time.Time
	mu             sync.Mutex
	messagesAPI    *MessagesInterface

	inputTokens          int
	outputTokens         int
	totalTokens          int
	cacheCreationTokens  int
	cacheReadTokens      int
	model                string
	provider             string
	stopReason           string
	accumulatedTextParts []string
	hasVision            bool
}

func newStreamingWrapper(stream interface{}, cfg *Config, md map[string]interface{}, m *MessagesInterface, model, provider string, hasVision bool, startTime time.Time) *StreamingWrapper {
	w := &StreamingWrapper{
		config:      cfg,
		metadata:    md,
		startTime:   startTime,
		messagesAPI: m,
		model:       model,
		provider:    provider,
		hasVision:   hasVision,
	}
	if typed, ok := stream.(*ssestream.Stream[anthropic.MessageStreamEventUnion]); ok {
		w.typedStream = typed
	} else {
		w.stream = stream
	}
	return w
}

func (sw *StreamingWrapper) Next() bool {
	if sw.typedStream != nil {
		return sw.typedStream.Next()
	}

	if sw.stream == nil {
		return false
	}

	streamVal := reflect.ValueOf(sw.stream)
	if streamVal.Kind() == reflect.Ptr {
		nextMethod := streamVal.MethodByName("Next")
		if nextMethod.IsValid() {
			result := nextMethod.Call(nil)
			if len(result) > 0 {
				if b, ok := result[0].Interface().(bool); ok {
					return b
				}
			}
		}
	}

	return false
}

func (sw *StreamingWrapper) Current() interface{} {
	if sw.typedStream != nil {
		event := sw.typedStream.Current()
		sw.processTypedEvent(event)
		return event
	}

	if sw.stream == nil {
		return nil
	}

	streamVal := reflect.ValueOf(sw.stream)
	if streamVal.Kind() == reflect.Ptr {
		currentMethod := streamVal.MethodByName("Current")
		if currentMethod.IsValid() {
			result := currentMethod.Call(nil)
			if len(result) > 0 {
				event := result[0].Interface()
				sw.processReflectEvent(event)
				return event
			}
		}
	}
	return nil
}

func (sw *StreamingWrapper) processTypedEvent(event anthropic.MessageStreamEventUnion) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	switch event.Type {
	case "content_block_delta":
		if sw.firstTokenTime == nil && event.Delta.Text != "" {
			now := time.Now()
			sw.firstTokenTime = &now
		}
		if event.Delta.Text != "" {
			sw.accumulatedTextParts = append(sw.accumulatedTextParts, event.Delta.Text)
		}

	case "message_start":
		startEvent := event.AsMessageStart()
		if startEvent.Message.Usage.InputTokens > 0 {
			sw.inputTokens = int(startEvent.Message.Usage.InputTokens)
			sw.cacheCreationTokens = int(startEvent.Message.Usage.CacheCreationInputTokens)
			sw.cacheReadTokens = int(startEvent.Message.Usage.CacheReadInputTokens)
		}
		if string(startEvent.Message.Model) != "" {
			sw.model = string(startEvent.Message.Model)
		}

	case "message_delta":
		sw.outputTokens = int(event.Usage.OutputTokens)
		if event.Usage.InputTokens > 0 {
			sw.inputTokens = int(event.Usage.InputTokens)
		}
		if event.Usage.CacheCreationInputTokens > 0 {
			sw.cacheCreationTokens = int(event.Usage.CacheCreationInputTokens)
		}
		if event.Usage.CacheReadInputTokens > 0 {
			sw.cacheReadTokens = int(event.Usage.CacheReadInputTokens)
		}
		sw.totalTokens = sw.inputTokens + sw.outputTokens
		if event.Delta.StopReason != "" {
			sw.stopReason = string(event.Delta.StopReason)
		}
	}
}

func (sw *StreamingWrapper) processReflectEvent(event interface{}) {
	if event == nil {
		return
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	if isContentEvent(event) {
		if sw.firstTokenTime == nil {
			now := time.Now()
			sw.firstTokenTime = &now
		}
		if text := extractTextFromContentEvent(event); text != "" {
			sw.accumulatedTextParts = append(sw.accumulatedTextParts, text)
		}
	}

	if isMessageDeltaEvent(event) {
		usage := extractUsageFromEvent(event)
		if usage != nil {
			sw.inputTokens = int(usage.InputTokens)
			sw.outputTokens = int(usage.OutputTokens)
			sw.totalTokens = sw.inputTokens + sw.outputTokens
			if usage.CacheCreationInputTokens > 0 {
				sw.cacheCreationTokens = int(usage.CacheCreationInputTokens)
			}
			if usage.CacheReadInputTokens > 0 {
				sw.cacheReadTokens = int(usage.CacheReadInputTokens)
			}
		}

		stopReasonStr := extractStopReasonFromEvent(event)
		if stopReasonStr != "" {
			sw.stopReason = stopReasonStr
		}
	}
}

func (sw *StreamingWrapper) Err() error {
	if sw.typedStream != nil {
		return sw.typedStream.Err()
	}

	if sw.stream == nil {
		return nil
	}

	streamVal := reflect.ValueOf(sw.stream)
	if streamVal.Kind() == reflect.Ptr {
		errMethod := streamVal.MethodByName("Err")
		if errMethod.IsValid() {
			result := errMethod.Call(nil)
			if len(result) > 0 {
				if err, ok := result[0].Interface().(error); ok {
					return err
				}
			}
		}
	}
	return nil
}

func (sw *StreamingWrapper) SetInputTokens(tokens int) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.inputTokens = tokens
	sw.totalTokens = sw.inputTokens + sw.outputTokens
}

func (sw *StreamingWrapper) SetModel(model string) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.model = model
}

func (sw *StreamingWrapper) GetTokenCounts() (input, output, total int) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.inputTokens, sw.outputTokens, sw.totalTokens
}

func (sw *StreamingWrapper) Close() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	var err error
	if sw.typedStream != nil {
		err = sw.typedStream.Close()
	} else if sw.stream != nil {
		streamVal := reflect.ValueOf(sw.stream)
		if streamVal.Kind() == reflect.Ptr {
			closeMethod := streamVal.MethodByName("Close")
			if closeMethod.IsValid() {
				result := closeMethod.Call(nil)
				if len(result) > 0 {
					if e, ok := result[0].Interface().(error); ok {
						err = e
					}
				}
			}
		}
	}

	duration := time.Since(sw.startTime)
	timeToFirstToken := int64(0)
	var completionStartTime *time.Time
	if sw.firstTokenTime != nil {
		timeToFirstToken = sw.firstTokenTime.Sub(sw.startTime).Milliseconds()
		completionStartTime = sw.firstTokenTime
	}

	normalizedProvider := NormalizeProviderName(sw.provider)

	stopReason := "END"
	if sw.stopReason != "" {
		stopReason = MapStopReasonToRevenium(sw.stopReason)
	}

	payload := metering.NewPayload(metering.OperationChat, sw.model, normalizedProvider).
		WithTiming(sw.startTime, duration).
		WithTokens(int64(sw.inputTokens), int64(sw.outputTokens), int64(sw.totalTokens)).
		WithReasoningTokens(0, int64(sw.cacheCreationTokens), int64(sw.cacheReadTokens)).
		WithStreaming(true, timeToFirstToken, completionStartTime).
		WithStopReason(stopReason).
		Build()

	if sw.hasVision {
		payload.Attributes = map[string]interface{}{"hasVision": true}
	}

	metering.ApplyMetadata(payload, sw.metadata)
	sw.messagesAPI.parent.metering.Send(payload)

	return err
}

func ReconstructResponseFromChunks(wrapper *StreamingWrapper) *anthropic.Message {
	wrapper.mu.Lock()
	defer wrapper.mu.Unlock()

	msg := &anthropic.Message{
		Role:       "assistant",
		Model:      anthropic.Model(wrapper.model),
		StopReason: anthropic.StopReason(wrapper.stopReason),
		Usage: anthropic.Usage{
			InputTokens:              int64(wrapper.inputTokens),
			OutputTokens:             int64(wrapper.outputTokens),
			CacheCreationInputTokens: int64(wrapper.cacheCreationTokens),
			CacheReadInputTokens:     int64(wrapper.cacheReadTokens),
		},
	}

	if len(wrapper.accumulatedTextParts) > 0 {
		fullText := strings.Join(wrapper.accumulatedTextParts, "")
		textJSON, err := json.Marshal(fullText)
		if err == nil {
			contentJSON := `[{"type":"text","text":` + string(textJSON) + `}]`
			var content []anthropic.ContentBlockUnion
			if err := json.Unmarshal([]byte(contentJSON), &content); err == nil {
				msg.Content = content
			}
		}
	}

	return msg
}

func isContentEvent(event interface{}) bool {
	if event == nil {
		return false
	}

	eventValue := reflect.ValueOf(event)
	if eventValue.Kind() == reflect.Ptr {
		eventValue = eventValue.Elem()
	}

	deltaField := eventValue.FieldByName("Delta")
	if deltaField.IsValid() && !deltaField.IsZero() {
		deltaValue := deltaField.Interface()
		if deltaValue == nil {
			return false
		}

		deltaReflect := reflect.ValueOf(deltaValue)
		if deltaReflect.Kind() == reflect.Ptr {
			deltaReflect = deltaReflect.Elem()
		}

		textField := deltaReflect.FieldByName("Text")
		if textField.IsValid() && textField.Kind() == reflect.String && textField.String() != "" {
			return true
		}
	}

	return false
}

func extractTextFromContentEvent(event interface{}) string {
	if event == nil {
		return ""
	}

	eventValue := reflect.ValueOf(event)
	if eventValue.Kind() == reflect.Ptr {
		eventValue = eventValue.Elem()
	}

	deltaField := eventValue.FieldByName("Delta")
	if deltaField.IsValid() && !deltaField.IsZero() {
		deltaReflect := reflect.ValueOf(deltaField.Interface())
		if deltaReflect.Kind() == reflect.Ptr {
			deltaReflect = deltaReflect.Elem()
		}

		textField := deltaReflect.FieldByName("Text")
		if textField.IsValid() && textField.Kind() == reflect.String {
			return textField.String()
		}
	}

	return ""
}

func isMessageDeltaEvent(event interface{}) bool {
	if event == nil {
		return false
	}

	eventValue := reflect.ValueOf(event)
	if eventValue.Kind() == reflect.Ptr {
		eventValue = eventValue.Elem()
	}

	typeField := eventValue.FieldByName("Type")
	if typeField.IsValid() && typeField.Kind() == reflect.String && typeField.String() == "message_delta" {
		return true
	}

	return false
}

func extractUsageFromEvent(event interface{}) *anthropic.MessageDeltaUsage {
	if event == nil {
		return nil
	}

	eventValue := reflect.ValueOf(event)
	if eventValue.Kind() == reflect.Ptr {
		eventValue = eventValue.Elem()
	}

	usageField := eventValue.FieldByName("Usage")
	if usageField.IsValid() && !usageField.IsZero() {
		if usage, ok := usageField.Interface().(anthropic.MessageDeltaUsage); ok {
			return &usage
		}
	}

	return nil
}

func extractStopReasonFromEvent(event interface{}) string {
	if event == nil {
		return ""
	}

	eventValue := reflect.ValueOf(event)
	if eventValue.Kind() == reflect.Ptr {
		eventValue = eventValue.Elem()
	}

	deltaField := eventValue.FieldByName("Delta")
	if deltaField.IsValid() && !deltaField.IsZero() {
		stopReasonField := deltaField.FieldByName("StopReason")
		if stopReasonField.IsValid() && stopReasonField.Kind() == reflect.String && stopReasonField.String() != "" {
			return stopReasonField.String()
		}
	}

	return ""
}

func estimateInputTokens(params anthropic.MessageNewParams) int {
	totalChars := 0

	for _, msg := range params.Messages {
		for _, block := range msg.Content {
			if block.OfText != nil {
				totalChars += len(block.OfText.Text)
			}
		}
	}

	estimatedTokens := totalChars / 4
	if estimatedTokens < 1 {
		estimatedTokens = 10
	}

	return estimatedTokens
}
