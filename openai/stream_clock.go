package openai

import (
	"time"

	"github.com/openai/openai-go/v3"
)

// streamClock times a streamed call from before the request is sent to the end of the
// stream, so a caller that closes the stream late does not stretch the recorded duration.
type streamClock struct {
	start      time.Time
	firstToken *time.Time
	end        time.Time
	finished   map[int64]bool
}

// choiceProgress maps each choice index a chunk carries to whether it finished in that chunk.
type choiceProgress map[int64]bool

func startStreamClock() streamClock {
	return streamClock{start: time.Now()}
}

// observe ends the call when every choice seen so far has finished; a choice still
// streaming, or one that appears later, leaves the end to the stream end or Close().
func (c *streamClock) observe(hasOutput bool, progress choiceProgress) {
	if c.finished == nil {
		c.finished = map[int64]bool{}
	}
	now := time.Now()
	if hasOutput && c.firstToken == nil {
		c.firstToken = &now
	}
	finishedNow := false
	for index, finished := range progress {
		c.finished[index] = c.finished[index] || finished
		finishedNow = finishedNow || finished
	}
	if !c.allChoicesFinished() {
		c.end = time.Time{}
	} else if finishedNow {
		c.end = now
	}
}

func (c *streamClock) allChoicesFinished() bool {
	for _, finished := range c.finished {
		if !finished {
			return false
		}
	}
	return len(c.finished) > 0
}

func (c *streamClock) markExhausted() {
	if c.end.IsZero() {
		c.end = time.Now()
	}
}

func (c *streamClock) duration() time.Duration {
	if c.end.IsZero() {
		return time.Since(c.start)
	}
	return c.end.Sub(c.start)
}

func (c *streamClock) timeToFirstToken() int64 {
	if c.firstToken == nil {
		return 0
	}
	return c.firstToken.Sub(c.start).Milliseconds()
}

func chatChunkHasOutput(chunk openai.ChatCompletionChunk) bool {
	for _, choice := range chunk.Choices {
		delta := choice.Delta
		if delta.Content != "" || delta.Refusal != "" || len(delta.ToolCalls) > 0 ||
			delta.FunctionCall.Name != "" || delta.FunctionCall.Arguments != "" {
			return true
		}
	}
	return false
}

func chatChoiceProgress(chunk openai.ChatCompletionChunk) choiceProgress {
	progress := choiceProgress{}
	for _, choice := range chunk.Choices {
		progress[choice.Index] = choice.FinishReason != ""
	}
	return progress
}

func textChunkHasOutput(chunk openai.Completion) bool {
	for _, choice := range chunk.Choices {
		if choice.Text != "" {
			return true
		}
	}
	return false
}

func textChoiceProgress(chunk openai.Completion) choiceProgress {
	progress := choiceProgress{}
	for _, choice := range chunk.Choices {
		progress[choice.Index] = choice.FinishReason != ""
	}
	return progress
}
