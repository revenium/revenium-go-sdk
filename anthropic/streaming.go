package anthropic

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

type StreamingWrapper struct {
	stream         interface{}
	typedStream    *ssestream.Stream[anthropic.MessageStreamEventUnion]
	config         *Config
	metadata       map[string]interface{}
	startTime      time.Time
	firstTokenTime *time.Time
	mu             sync.Mutex
	closed         bool
	messagesAPI    *MessagesInterface

	inputTokens          int
	outputTokens         int
	totalTokens          int
	cacheCreationTokens  int
	cacheReadTokens      int
	cacheWrites          cacheWriteSplit
	passes               []billedPass
	reasoningTokens      int
	model                string
	provider             string
	stopReason           string
	accumulatedTextParts []string
	hasVision            bool
	// usageReported is set once the provider reports token counts, which
	// replace the estimate a stream starts with.
	usageReported bool
	progress      streamProgress

	current interface{}
	request anthropic.MessageNewParams
	reply   *streamedReply
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

func (sw *StreamingWrapper) captureRequest(params anthropic.MessageNewParams) {
	sw.request = params
	if !prompt.ShouldCapturePrompts(sw.metadata) {
		return
	}
	if capturer, ok := sw.stream.(replyCapturer); ok {
		capturer.captureReply()
	} else if sw.typedStream != nil {
		sw.reply = newStreamedReply()
	}
}

// Next reads each event exactly once, so usage and the captured reply do not depend on
// whether, or how often, the caller reads Current.
func (sw *StreamingWrapper) Next() bool {
	if sw.typedStream != nil {
		if !sw.typedStream.Next() {
			sw.markExhausted()
			return false
		}
		sw.processTypedEvent(sw.typedStream.Current())
		return true
	}
	if !sw.callReflected("Next") {
		sw.markExhausted()
		return false
	}
	event := sw.reflectedCurrent()
	sw.setCurrent(event)
	sw.processReflectEvent(event)
	return true
}

func (sw *StreamingWrapper) markExhausted() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.progress.exhausted = true
}

func (sw *StreamingWrapper) setCurrent(event interface{}) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.current = event
}

func (sw *StreamingWrapper) Current() interface{} {
	if sw.typedStream != nil {
		return sw.typedStream.Current()
	}
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.current
}

func (sw *StreamingWrapper) callReflected(name string) bool {
	method := sw.reflectedMethod(name)
	if !method.IsValid() {
		return false
	}
	result := method.Call(nil)
	if len(result) == 0 {
		return false
	}
	advanced, _ := result[0].Interface().(bool)
	return advanced
}

func (sw *StreamingWrapper) reflectedCurrent() interface{} {
	method := sw.reflectedMethod("Current")
	if !method.IsValid() {
		return nil
	}
	if result := method.Call(nil); len(result) > 0 {
		return result[0].Interface()
	}
	return nil
}

func (sw *StreamingWrapper) reflectedMethod(name string) reflect.Value {
	if sw.stream == nil {
		return reflect.Value{}
	}
	streamVal := reflect.ValueOf(sw.stream)
	if streamVal.Kind() != reflect.Ptr {
		return reflect.Value{}
	}
	return streamVal.MethodByName(name)
}

func (sw *StreamingWrapper) processTypedEvent(event anthropic.MessageStreamEventUnion) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if sw.reply != nil {
		sw.reply.observe(event)
	}

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
		sw.usageReported = true
		startEvent := event.AsMessageStart()
		sw.inputTokens = int(startEvent.Message.Usage.InputTokens)
		sw.outputTokens = int(startEvent.Message.Usage.OutputTokens)
		sw.totalTokens = sw.inputTokens + sw.outputTokens
		sw.cacheReadTokens = int(startEvent.Message.Usage.CacheReadInputTokens)
		sw.cacheCreationTokens = int(startEvent.Message.Usage.CacheCreationInputTokens)
		sw.cacheWrites = cacheWriteSplitOf(startEvent.Message.Usage.CacheCreation)
		sw.takeBilledPasses(startEvent.Message.Usage.RawJSON())
		if string(startEvent.Message.Model) != "" {
			sw.model = string(startEvent.Message.Model)
		}

	case "message_delta":
		sw.outputTokens = int(event.Usage.OutputTokens)
		if event.Usage.InputTokens > 0 {
			sw.inputTokens = int(event.Usage.InputTokens)
		}
		if event.Usage.CacheCreationInputTokens > 0 {
			sw.takeCacheWriteTotal(int(event.Usage.CacheCreationInputTokens), event.Usage.RawJSON())
		}
		if event.Usage.CacheReadInputTokens > 0 {
			sw.cacheReadTokens = int(event.Usage.CacheReadInputTokens)
		}
		sw.totalTokens = sw.inputTokens + sw.outputTokens
		if event.Usage.OutputTokensDetails.ThinkingTokens > 0 {
			sw.reasoningTokens = int(event.Usage.OutputTokensDetails.ThinkingTokens)
		}
		if event.Delta.StopReason != "" {
			sw.stopReason = string(event.Delta.StopReason)
		}
		sw.takeBilledPasses(event.Usage.RawJSON())

	case "message_stop":
		sw.progress.stopEventSeen = true
	}
}

func (sw *StreamingWrapper) takeCacheWriteTotal(total int, usageJSON string) {
	if total != sw.cacheCreationTokens {
		sw.cacheWrites = cacheWriteSplitInUsage(usageJSON)
	}
	sw.cacheCreationTokens = total
}

func (sw *StreamingWrapper) takeBilledPasses(usageJSON string) {
	if passes, reported := billedPassesInUsage(usageJSON); reported {
		sw.passes = passes
	}
}

func (sw *StreamingWrapper) processReflectEvent(event interface{}) {
	if event == nil {
		return
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	if isEventOfType(event, "message_stop") {
		sw.progress.stopEventSeen = true
	}

	if isContentEvent(event) {
		if sw.firstTokenTime == nil {
			now := time.Now()
			sw.firstTokenTime = &now
		}
		if text := extractTextFromContentEvent(event); text != "" {
			sw.accumulatedTextParts = append(sw.accumulatedTextParts, text)
		}
	}

	if isEventOfType(event, "message_delta") {
		usage := extractUsageFromEvent(event)
		if usage != nil {
			sw.usageReported = true
			sw.inputTokens = int(usage.InputTokens)
			sw.outputTokens = int(usage.OutputTokens)
			sw.totalTokens = sw.inputTokens + sw.outputTokens
			if usage.CacheCreationInputTokens > 0 {
				sw.cacheCreationTokens = int(usage.CacheCreationInputTokens)
			}
			if usage.CacheReadInputTokens > 0 {
				sw.cacheReadTokens = int(usage.CacheReadInputTokens)
			}
			if usage.OutputTokensDetails.ThinkingTokens > 0 {
				sw.reasoningTokens = int(usage.OutputTokensDetails.ThinkingTokens)
			}
			sw.takeBilledPasses(usage.RawJSON())
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

	if sw.closed {
		return nil
	}
	sw.closed = true

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

	if acc, ok := sw.stream.(StreamTokenAccumulator); ok {
		input, output, total := acc.GetAccumulatedTokens()
		if input > 0 || output > 0 {
			sw.usageReported = true
			sw.inputTokens = int(input)
			sw.outputTokens = int(output)
			sw.totalTokens = int(total)
		}
		creation, read := acc.GetCacheTokens()
		if creation > 0 {
			sw.cacheCreationTokens = int(creation)
		}
		if read > 0 {
			sw.cacheReadTokens = int(read)
		}
		if sr := acc.GetStopReason(); sr != "" && sw.stopReason == "" {
			sw.stopReason = sr
		}
	}
	if reporter, ok := sw.stream.(stopEventReporter); ok && reporter.reachedStopEvent() {
		sw.progress.stopEventSeen = true
	}
	if reporter, ok := sw.stream.(cacheWriteSplitReporter); ok {
		if split := reporter.reportedCacheWriteSplit(); split.isReported() {
			sw.cacheWrites = split
		}
	}
	if reporter, ok := sw.stream.(billedPassReporter); ok {
		if passes := reporter.reportedBilledPasses(); passes != nil {
			sw.passes = passes
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

	streamErr := sw.Err()
	if streamErr != nil && !sw.usageReported {
		sw.inputTokens, sw.totalTokens = 0, sw.outputTokens
	}

	stopReason := "END"
	if sw.stopReason != "" {
		stopReason = MapStopReasonToRevenium(sw.stopReason)
	}
	if sw.progress.abandonedBeforeStop() {
		stopReason = string(core.StopReasonCancelled)
	}

	payload := metering.NewPayload(metering.OperationChat, sw.model, normalizedProvider).
		WithTiming(sw.startTime, duration).
		WithTokens(int64(sw.inputTokens), int64(sw.outputTokens), int64(sw.totalTokens)).
		WithReasoningTokens(int64(sw.reasoningTokens), int64(sw.cacheCreationTokens), int64(sw.cacheReadTokens)).
		WithCacheCreationByTTL(sw.cacheWrites.fiveMinute, sw.cacheWrites.oneHour).
		WithStreaming(true, timeToFirstToken, completionStartTime).
		WithStopReason(stopReason).
		Build()

	if sw.hasVision {
		payload.Attributes = map[string]interface{}{"hasVision": true}
	}

	if streamErr != nil {
		metering.ApplyFailedCall(payload, callFailure(streamErr), sw.metadata)
		sendTurnRecords(sw.messagesAPI.parent.metering, payload, sw.passes, "")
		return err
	}

	metering.ApplyMetadata(payload, sw.metadata)
	metering.ApplyChatCapture(payload, sw.metadata, func() metering.ChatCapture {
		capture := messageCapture(sw.request, "")
		capture.Output, capture.OutputCut = sw.capturedReply()
		return capture
	})
	sendTurnRecords(sw.messagesAPI.parent.metering, payload, sw.passes, "")

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

func isEventOfType(event interface{}, eventType string) bool {
	if event == nil {
		return false
	}

	eventValue := reflect.ValueOf(event)
	if eventValue.Kind() == reflect.Ptr {
		eventValue = eventValue.Elem()
	}
	if eventValue.Kind() != reflect.Struct {
		return false
	}

	typeField := eventValue.FieldByName("Type")
	return typeField.IsValid() && typeField.Kind() == reflect.String && typeField.String() == eventType
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

func (sw *StreamingWrapper) capturedReply() (text string, cut bool) {
	if capturer, ok := sw.stream.(replyCapturer); ok {
		return capturer.capturedReply()
	}
	if sw.reply != nil {
		return sw.reply.text()
	}
	return "", true
}
