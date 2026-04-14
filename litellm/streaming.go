package litellm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

type StreamingResponse struct {
	reader   io.ReadCloser
	buffered *bufio.Reader
	metadata map[string]interface{}
	model    string
	metering *metering.MeteringClient
	enabled  bool
	mu       sync.Mutex

	current *StreamChunk
	done    bool
	err     error

	inputTokens         int64
	outputTokens        int64
	totalTokens         int64
	reasoningTokens     int64
	cacheReadTokens     int64
	cacheCreationTokens int64

	startTime      time.Time
	firstTokenTime *time.Time

	finishReason      string
	systemFingerprint string

	capturePrompts       bool
	maxPromptSize        int
	accumulatedContent   strings.Builder
	accumulatedToolCalls map[int]*toolCallAccumulator
	responseID           string
	responseCreated      int64
}

type toolCallAccumulator struct {
	index     int
	id        string
	callType  string
	name      string
	arguments strings.Builder
}

func newStreamingResponse(reader io.ReadCloser, metadata map[string]interface{}, model string, mc *metering.MeteringClient, enabled bool) *StreamingResponse {
	s := &StreamingResponse{
		reader:               reader,
		buffered:             bufio.NewReaderSize(reader, 64*1024),
		metadata:             metadata,
		startTime:            time.Now(),
		model:                model,
		metering:             mc,
		enabled:              enabled,
		capturePrompts:       prompt.ShouldCapturePrompts(metadata),
		maxPromptSize:        prompt.GetMaxPromptSize(),
		accumulatedToolCalls: map[int]*toolCallAccumulator{},
	}
	return s
}

func (s *StreamingResponse) Next() bool {
	if s.done || s.err != nil {
		return false
	}

	for {
		line, err := s.readLine()
		if err != nil {
			if err == io.EOF {
				s.done = true
			} else {
				s.err = err
			}
			return false
		}
		chunk := s.processSSELine(line)
		if chunk != nil {
			s.current = chunk
			return true
		}
		if s.done {
			return false
		}
	}
}

func (s *StreamingResponse) readLine() (string, error) {
	var buf bytes.Buffer
	for {
		frag, err := s.buffered.ReadSlice('\n')
		buf.Write(frag)
		if err == nil {
			return strings.TrimRight(buf.String(), "\r\n"), nil
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			if buf.Len() == 0 {
				return "", io.EOF
			}
			return strings.TrimRight(buf.String(), "\r\n"), nil
		}
		return "", err
	}
}

func (s *StreamingResponse) Current() *StreamChunk {
	return s.current
}

func (s *StreamingResponse) Err() error {
	return s.err
}

func (s *StreamingResponse) Close() error {
	closeErr := s.reader.Close()
	duration := time.Since(s.startTime)

	s.mu.Lock()
	defer s.mu.Unlock()

	modelName := ExtractModelName(s.model)
	provider := ExtractProvider(s.model)
	modelSource := ExtractModelSource(s.model)

	if s.err != nil {
		payload := metering.NewPayload(metering.OperationChat, modelName, provider).
			WithTiming(s.startTime, duration).
			WithTokens(0, 0, 0).
			WithReasoningTokens(0, 0, 0).
			WithStreaming(true, 0, nil).
			WithModelSource(modelSource).
			WithError(s.err.Error()).
			Build()
		metering.ApplyMetadata(payload, s.metadata)
		if s.enabled {
			s.metering.Send(payload)
		}
		return closeErr
	}

	timeToFirstToken := int64(0)
	var completionStartTime *time.Time
	if s.firstTokenTime != nil {
		timeToFirstToken = s.firstTokenTime.Sub(s.startTime).Milliseconds()
		completionStartTime = s.firstTokenTime
	}

	finishReason := s.finishReason
	if finishReason == "" {
		finishReason = "stop"
	}
	stopReason := string(MapFinishReason(finishReason, core.StopReasonEnd))

	payload := metering.NewPayload(metering.OperationChat, modelName, provider).
		WithTiming(s.startTime, duration).
		WithTokens(s.inputTokens, s.outputTokens, s.totalTokens).
		WithReasoningTokens(s.reasoningTokens, s.cacheCreationTokens, s.cacheReadTokens).
		WithStreaming(true, timeToFirstToken, completionStartTime).
		WithStopReason(stopReason).
		WithModelSource(modelSource).
		WithSystemFingerprint(s.systemFingerprint).
		Build()
	metering.ApplyMetadata(payload, s.metadata)
	if s.enabled {
		s.metering.Send(payload)
	}

	return closeErr
}

func (s *StreamingResponse) processSSELine(line string) *StreamChunk {
	trimmed := strings.TrimSpace(line)

	if trimmed == "" || strings.HasPrefix(trimmed, ":") {
		return nil
	}

	if !strings.HasPrefix(trimmed, "data: ") {
		return nil
	}

	data := trimmed[6:]

	if data == "[DONE]" {
		core.Debug("Stream completed")
		s.done = true
		return nil
	}

	var chunk StreamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		core.Debug("Failed to parse stream chunk: %v", err)
		return nil
	}

	s.processChunkData(&chunk)
	return &chunk
}

func (s *StreamingResponse) processChunkData(chunk *StreamChunk) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.responseID == "" && chunk.ID != "" {
		s.responseID = chunk.ID
	}
	if s.responseCreated == 0 && chunk.Created != 0 {
		s.responseCreated = chunk.Created
	}

	if s.firstTokenTime == nil && len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
		now := time.Now()
		s.firstTokenTime = &now
	}

	usage := chunk.Usage
	if usage == nil && chunk.XGroq != nil && chunk.XGroq.Usage != nil {
		usage = chunk.XGroq.Usage
	}
	if usage != nil && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
		s.inputTokens = usage.PromptTokens
		s.outputTokens = usage.CompletionTokens
		s.totalTokens = usage.TotalTokens
		if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens > 0 {
			s.reasoningTokens = usage.CompletionTokensDetails.ReasoningTokens
		}
		if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens > 0 {
			s.cacheReadTokens = usage.PromptTokensDetails.CachedTokens
		}
	}

	if len(chunk.Choices) > 0 {
		choice := chunk.Choices[0]
		if choice.FinishReason != "" {
			s.finishReason = choice.FinishReason
		}
		if s.capturePrompts {
			s.accumulateDelta(&choice.Delta)
		}
	}

	if chunk.SystemFingerprint != "" {
		s.systemFingerprint = chunk.SystemFingerprint
	}
}

func (s *StreamingResponse) accumulateDelta(delta *StreamDelta) {
	if delta == nil {
		return
	}
	if delta.Content != "" {
		remaining := s.maxPromptSize - s.accumulatedContent.Len()
		if remaining > 0 {
			if len(delta.Content) <= remaining {
				s.accumulatedContent.WriteString(delta.Content)
			} else {
				s.accumulatedContent.WriteString(delta.Content[:remaining])
			}
		}
	}
	for _, tc := range delta.ToolCalls {
		acc, ok := s.accumulatedToolCalls[tc.Index]
		if !ok {
			acc = &toolCallAccumulator{index: tc.Index, callType: "function"}
			s.accumulatedToolCalls[tc.Index] = acc
		}
		if tc.ID != "" {
			acc.id = tc.ID
		}
		if tc.Type != "" {
			acc.callType = tc.Type
		}
		if tc.Function != nil {
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				remaining := s.maxPromptSize - acc.arguments.Len()
				if remaining > 0 {
					if len(tc.Function.Arguments) <= remaining {
						acc.arguments.WriteString(tc.Function.Arguments)
					} else {
						acc.arguments.WriteString(tc.Function.Arguments[:remaining])
					}
				}
			}
		}
	}
}

// ReconstructResponse assembles a full ChatCompletionResponse from the streamed chunks
// Returns nil when prompt capture is disabled or no content has been accumulated
func (s *StreamingResponse) ReconstructResponse() *ChatCompletionResponse {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.capturePrompts {
		return nil
	}

	content := s.accumulatedContent.String()
	if content == "" && len(s.accumulatedToolCalls) == 0 {
		return nil
	}

	message := ResponseMessage{Role: "assistant", Content: content}
	if len(s.accumulatedToolCalls) > 0 {
		indices := make([]int, 0, len(s.accumulatedToolCalls))
		for idx := range s.accumulatedToolCalls {
			indices = append(indices, idx)
		}
		sort.Ints(indices)
		tools := make([]ToolCall, 0, len(indices))
		for _, idx := range indices {
			acc := s.accumulatedToolCalls[idx]
			callType := acc.callType
			if callType == "" {
				callType = "function"
			}
			tools = append(tools, ToolCall{
				ID:   acc.id,
				Type: callType,
				Function: ToolCallFunction{
					Name:      acc.name,
					Arguments: acc.arguments.String(),
				},
			})
		}
		message.ToolCalls = tools
	}

	finish := s.finishReason
	if finish == "" {
		finish = "stop"
	}

	created := s.responseCreated
	if created == 0 {
		created = time.Now().Unix()
	}

	id := s.responseID
	if id == "" {
		id = "unknown"
	}

	return &ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   s.model,
		Choices: []Choice{{Index: 0, Message: message, FinishReason: finish}},
		Usage: &TokenUsage{
			PromptTokens:     s.inputTokens,
			CompletionTokens: s.outputTokens,
			TotalTokens:      s.totalTokens,
		},
		SystemFingerprint: s.systemFingerprint,
	}
}
