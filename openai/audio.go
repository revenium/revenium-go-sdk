package openai

import (
	"context"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/openai/openai-go/v3"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

type AudioInterface struct {
	client   openai.Client
	config   *Config
	provider Provider
	parent   *ReveniumOpenAI
}

func (a *AudioInterface) Transcriptions() *TranscriptionsInterface {
	return &TranscriptionsInterface{
		client:   a.client,
		config:   a.config,
		provider: a.provider,
		parent:   a.parent,
	}
}

func (a *AudioInterface) Translations() *TranslationsInterface {
	return &TranslationsInterface{
		client:   a.client,
		config:   a.config,
		provider: a.provider,
		parent:   a.parent,
	}
}

func (a *AudioInterface) Speech() *SpeechInterface {
	return &SpeechInterface{
		client:   a.client,
		config:   a.config,
		provider: a.provider,
		parent:   a.parent,
	}
}

type TranscriptionsInterface struct {
	client   openai.Client
	config   *Config
	provider Provider
	parent   *ReveniumOpenAI
}

func (t *TranscriptionsInterface) Create(ctx context.Context, params openai.AudioTranscriptionNewParams) (*openai.AudioTranscriptionNewResponseUnion, error) {
	metadata := core.GetUsageMetadata(ctx)
	model := string(params.Model)
	providerStr := t.provider.String()
	requestTime := time.Now()

	resp, err := t.client.Audio.Transcriptions.New(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := buildAudioErrorPayload(model, metadata, duration, providerStr, requestTime, err.Error())
		t.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)

	attrs := map[string]interface{}{
		"billing_unit":     "per_minute",
		"operationSubtype": "transcription",
		"language":         resp.Language,
		"response_format":  string(params.ResponseFormat),
	}

	builder := metering.NewPayload(metering.OperationAudio, model, providerStr).
		WithTiming(requestTime, duration).
		WithAttributes(attrs)

	if resp.Duration > 0 {
		builder = builder.WithAudioDuration(resp.Duration)
	}

	payload := builder.Build()
	metering.ApplyMetadata(payload, metadata)
	t.parent.metering.Send(payload)

	return resp, nil
}

type TranslationsInterface struct {
	client   openai.Client
	config   *Config
	provider Provider
	parent   *ReveniumOpenAI
}

func (t *TranslationsInterface) Create(ctx context.Context, params openai.AudioTranslationNewParams) (*openai.Translation, error) {
	metadata := core.GetUsageMetadata(ctx)
	model := string(params.Model)
	providerStr := t.provider.String()
	requestTime := time.Now()

	resp, err := t.client.Audio.Translations.New(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := buildAudioErrorPayload(model, metadata, duration, providerStr, requestTime, err.Error())
		t.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)

	attrs := map[string]interface{}{
		"billing_unit":     "per_minute",
		"operationSubtype": "translation",
		"target_language":  "en",
		"response_format":  string(params.ResponseFormat),
	}

	payload := metering.NewPayload(metering.OperationAudio, model, providerStr).
		WithTiming(requestTime, duration).
		WithAttributes(attrs).
		Build()

	metering.ApplyMetadata(payload, metadata)
	t.parent.metering.Send(payload)

	return resp, nil
}

type SpeechInterface struct {
	client   openai.Client
	config   *Config
	provider Provider
	parent   *ReveniumOpenAI
}

func (s *SpeechInterface) Create(ctx context.Context, params openai.AudioSpeechNewParams) (*http.Response, error) {
	metadata := core.GetUsageMetadata(ctx)
	model := string(params.Model)
	providerStr := s.provider.String()
	requestTime := time.Now()

	resp, err := s.client.Audio.Speech.New(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := buildAudioErrorPayload(model, metadata, duration, providerStr, requestTime, err.Error())
		s.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)

	speed := 1.0
	if params.Speed.Valid() {
		speed = params.Speed.Value
	}

	attrs := map[string]interface{}{
		"billing_unit":             "per_character",
		"operationSubtype":        "speech_synthesis",
		"requested_character_count": utf8.RuneCountInString(params.Input),
		"voice":                    string(params.Voice),
		"speed":                    speed,
		"response_format":          string(params.ResponseFormat),
	}

	payload := metering.NewPayload(metering.OperationAudio, model, providerStr).
		WithTiming(requestTime, duration).
		WithAttributes(attrs).
		Build()

	metering.ApplyMetadata(payload, metadata)
	s.parent.metering.Send(payload)

	return resp, nil
}

func buildAudioErrorPayload(model string, md map[string]interface{}, duration time.Duration, provider string, requestTime time.Time, errorReason string) *metering.MeteringPayload {
	payload := metering.NewPayload(metering.OperationAudio, model, provider).
		WithTiming(requestTime, duration).
		WithError(errorReason).
		Build()

	metering.ApplyMetadata(payload, md)
	return payload
}
