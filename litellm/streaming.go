package litellm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

type StreamingResponse struct {
	reader      io.ReadCloser
	buffered    *bufio.Reader
	metadata    map[string]interface{}
	model       string
	servedModel string
	deployment  string
	headers     http.Header
	metering    *metering.MeteringClient
	enabled     bool
	mu          sync.Mutex
	closed      bool

	current *StreamChunk
	done    bool
	err     error

	inputTokens         int64
	outputTokens        int64
	totalTokens         int64
	reasoningTokens     int64
	cacheReadTokens     int64
	cacheCreationTokens int64
	cacheCreation5m     *int64
	cacheCreation1h     *int64
	reportedCost        *float64

	startTime      time.Time
	firstTokenTime *time.Time

	finishReason      string
	systemFingerprint string

	reconstructReply     bool
	maxPromptSize        int
	reply                *prompt.ReplyCollector
	messages             []ChatMessage
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
		reconstructReply:     prompt.ShouldCapturePrompts(metadata),
		maxPromptSize:        prompt.GetMaxPromptSize(),
		reply:                metering.ReplyCollectorFor(metadata),
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
		if s.done || s.err != nil {
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
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	closeErr := s.reader.Close()
	duration := time.Since(s.startTime)

	meteredModel := recordedModel(s.servedModel, s.model, s.deployment, s.headers)
	modelName := ExtractModelName(meteredModel)
	provider := ExtractProvider(meteredModel)
	modelSource := ExtractModelSource(meteredModel)

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
		WithTokens(billableInputTokens(meteredModel, s.inputTokens, s.cacheReadTokens, s.cacheCreationTokens), s.outputTokens, s.totalTokens).
		WithReasoningTokens(s.reasoningTokens, s.cacheCreationTokens, s.cacheReadTokens).
		WithCacheCreationByTTL(s.cacheCreation5m, s.cacheCreation1h).
		WithCosts(nil, nil, nil, nil, reportedCost(s.reportedCost, s.headers)).
		WithStreaming(true, timeToFirstToken, completionStartTime).
		WithStopReason(stopReason).
		WithModelSource(modelSource).
		WithSystemFingerprint(s.systemFingerprint).
		Build()
	if s.err != nil {
		metering.ApplyFailedCall(payload, metering.FailureOf(s.err), s.metadata)
	} else {
		metering.ApplyMetadata(payload, s.metadata)
		metering.ApplyChatCapture(payload, s.metadata, func() metering.ChatCapture {
			return metering.OpenAIChatCapture(s.messages, "").WithStreamedReply(s.reply)
		})
	}
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

	if err := streamErrorEvent(data); err != nil {
		s.err = err
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

// streamErrorEvent is the error a proxy sends mid-stream as
// data: {"error": ...}, which carries no chunk fields.
func streamErrorEvent(data string) error {
	var event struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal([]byte(data), &event) != nil || len(event.Error) == 0 || string(event.Error) == "null" {
		return nil
	}
	return core.NewProviderError("LiteLLM proxy sent an error mid-stream: "+data, nil).WithProviderReply([]byte(data))
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
	if s.servedModel == "" && chunk.Model != "" {
		s.servedModel = chunk.Model
	}

	if s.firstTokenTime == nil && len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
		now := time.Now()
		s.firstTokenTime = &now
	}

	usage := chunk.Usage
	if usage == nil && chunk.XGroq != nil && chunk.XGroq.Usage != nil {
		usage = chunk.XGroq.Usage
	}
	if usage != nil {
		if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
			s.inputTokens = usage.PromptTokens
			s.outputTokens = usage.CompletionTokens
			s.totalTokens = usage.TotalTokens
			if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens > 0 {
				s.reasoningTokens = usage.CompletionTokensDetails.ReasoningTokens
			}
		}
		if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens > 0 {
			s.cacheReadTokens = usage.PromptTokensDetails.CachedTokens
		}
		if cacheCreationTokens := usage.CacheCreationTokenCount(); cacheCreationTokens > 0 {
			s.cacheCreationTokens = cacheCreationTokens
		}
		if fiveMinute, oneHour := usage.CacheCreationByTTL(); fiveMinute != nil || oneHour != nil {
			s.cacheCreation5m, s.cacheCreation1h = fiveMinute, oneHour
		}
		if cost := usage.ReportedCost(); cost != nil {
			s.reportedCost = cost
		}
	}

	for i := range chunk.Choices {
		choice := &chunk.Choices[i]
		if choice.Index != 0 {
			continue
		}
		if s.reply != nil {
			s.accumulateDelta(&choice.Delta)
		}
		if choice.FinishReason != "" {
			s.finishReason = choice.FinishReason
			s.reply.End()
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
	s.reply.Add(delta.Content)
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

	if !s.reconstructReply {
		return nil
	}

	content := prompt.TruncateString(s.reply.Text(), s.maxPromptSize)
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
			Cost:             s.reportedCost,
		},
		SystemFingerprint: s.systemFingerprint,
	}
}
