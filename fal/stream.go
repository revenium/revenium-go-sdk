package fal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

// Stream opens a streaming fal request, returning a channel of events. The channel is closed when the stream ends.
func (r *ReveniumFal) Stream(ctx context.Context, endpointID string, input map[string]interface{}, metadata map[string]interface{}) (<-chan StreamEvent, error) {
	contextMeta := core.GetUsageMetadata(ctx)
	mergedMeta := mergeMetadataMaps(contextMeta, metadata)
	startTime := time.Now()
	enabledSnapshot := r.enabled.Load()

	body, err := json.Marshal(input)
	if err != nil {
		return nil, core.NewProviderError("failed to marshal input", err)
	}

	url := endpointURL(r.streamBaseURL(), endpointID) + "/stream"
	respBody, _, err := r.falClient.rawStream(ctx, "POST", url, body, nil)
	if err != nil {
		return nil, err
	}

	events := make(chan StreamEvent, 16)
	var closeOnce sync.Once
	closeChan := func() { closeOnce.Do(func() { close(events) }) }

	cancelCtx, cancelWatch := context.WithCancel(ctx)
	go func() {
		<-cancelCtx.Done()
		_ = respBody.Close()
	}()

	go func() {
		defer cancelWatch()
		defer closeChan()

		reader := bufio.NewReaderSize(respBody, 64*1024)
		var finalData map[string]interface{}

		send := func(ev StreamEvent) bool {
			select {
			case events <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for {
			line, readErr := readSSELine(reader)
			if readErr == io.EOF {
				if enabledSnapshot {
					duration := time.Since(startTime)
					r.meterStreamResult(endpointID, finalData, mergedMeta, duration, startTime, input)
				}
				send(StreamEvent{Data: finalData, Done: true})
				return
			}
			if readErr != nil {
				send(StreamEvent{Error: readErr, Done: true})
				return
			}

			data, ok := parseSSEData(line)
			if !ok {
				continue
			}
			if data == "[DONE]" {
				if enabledSnapshot {
					duration := time.Since(startTime)
					r.meterStreamResult(endpointID, finalData, mergedMeta, duration, startTime, input)
				}
				send(StreamEvent{Data: finalData, Done: true})
				return
			}

			parsed := map[string]interface{}{}
			if err := json.Unmarshal([]byte(data), &parsed); err != nil {
				core.Debug("Failed to parse fal stream chunk: %v", err)
				continue
			}
			finalData = parsed
			if !send(StreamEvent{Data: parsed, Partial: true}) {
				return
			}
		}
	}()

	return events, nil
}

func (r *ReveniumFal) meterStreamResult(endpointID string, result map[string]interface{}, metadata map[string]interface{}, duration time.Duration, startTime time.Time, input map[string]interface{}) {
	op := DetectMediaType(endpointID, result)
	prompt := promptFromInput(input)
	payload := buildPayloadFromResult(endpointID, op, result, input, metadata, duration, startTime, r.config.CapturePrompts, prompt)
	if payload != nil {
		r.metering.Send(payload)
	}
}

func (r *ReveniumFal) streamBaseURL() string {
	if r.config != nil && r.config.FalBaseURL != "" {
		return r.config.FalBaseURL
	}
	return "https://fal.run"
}

func readSSELine(reader *bufio.Reader) (string, error) {
	var buf bytes.Buffer
	for {
		frag, err := reader.ReadSlice('\n')
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

func parseSSEData(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, ":") {
		return "", false
	}
	if !strings.HasPrefix(trimmed, "data: ") {
		return "", false
	}
	return trimmed[6:], true
}
