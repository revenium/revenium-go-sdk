package openai

import (
	"context"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/openai/openai-go/v3"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
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
	requestedModel := string(params.Model)
	model := t.config.mediaModel(t.provider, requestedModel)
	providerStr := t.provider.String()
	if err := t.parent.enforcer.Check(enforcement.NewEvalContext(ctx, requestedModel, providerStr)); err != nil {
		core.Debug("Audio.Transcriptions.Create blocked by enforcement: %v", err)
		return nil, err
	}
	requestTime := time.Now()

	resp, err := t.client.Audio.Transcriptions.New(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := buildAudioErrorPayload(model, metering.SubtypeTranscription, metadata, duration, t.provider, requestTime, err)
		t.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)
	usage := parseAudioUsage(resp.RawJSON())
	responseFormat := string(params.ResponseFormat)

	builder := t.provider.mediaRecord(metering.OperationAudio, model).
		WithTiming(requestTime, duration).
		WithOperationSubtype(metering.SubtypeTranscription).
		WithAudioDetails(metering.AudioDetails{Language: resp.Language, ResponseFormat: responseFormat})

	payload := usage.applyTo(builder, model, metering.SubtypeTranscription, responseFormat).Build()
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
	requestedModel := string(params.Model)
	model := t.config.mediaModel(t.provider, requestedModel)
	providerStr := t.provider.String()
	if err := t.parent.enforcer.Check(enforcement.NewEvalContext(ctx, requestedModel, providerStr)); err != nil {
		core.Debug("Audio.Translations.Create blocked by enforcement: %v", err)
		return nil, err
	}
	requestTime := time.Now()

	resp, err := t.client.Audio.Translations.New(ctx, withDurationReporting(params))
	if err != nil {
		duration := time.Since(requestTime)
		payload := buildAudioErrorPayload(model, metering.SubtypeTranslation, metadata, duration, t.provider, requestTime, err)
		t.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)
	usage := parseAudioUsage(resp.RawJSON())
	responseFormat := string(params.ResponseFormat)

	builder := t.provider.mediaRecord(metering.OperationAudio, model).
		WithTiming(requestTime, duration).
		WithOperationSubtype(metering.SubtypeTranslation).
		WithAudioDetails(metering.AudioDetails{TargetLanguage: "en", ResponseFormat: responseFormat})

	payload := usage.applyTo(builder, model, metering.SubtypeTranslation, responseFormat).Build()

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
	requestedModel := string(params.Model)
	model := s.config.mediaModel(s.provider, requestedModel)
	providerStr := s.provider.String()
	if err := s.parent.enforcer.Check(enforcement.NewEvalContext(ctx, requestedModel, providerStr)); err != nil {
		core.Debug("Audio.Speech.Create blocked by enforcement: %v", err)
		return nil, err
	}
	requestTime := time.Now()

	resp, err := s.client.Audio.Speech.New(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := buildAudioErrorPayload(model, metering.SubtypeTTS, metadata, duration, s.provider, requestTime, err)
		s.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)

	speed := 1.0
	if params.Speed.Valid() {
		speed = params.Speed.Value
	}

	payload := s.provider.mediaRecord(metering.OperationAudio, model).
		WithTiming(requestTime, duration).
		WithOperationSubtype(metering.SubtypeTTS).
		WithBillingUnit(metering.BillingPerCharacter).
		WithCharacterCount(int64(utf8.RuneCountInString(params.Input))).
		WithAudioDetails(metering.AudioDetails{
			Voice:          audioVoiceLabel(params.Voice),
			Speed:          &speed,
			ResponseFormat: string(params.ResponseFormat),
		}).
		Build()

	metering.ApplyMetadata(payload, metadata)
	s.parent.metering.Send(payload)

	return resp, nil
}

func audioVoiceLabel(voice openai.AudioSpeechNewParamsVoiceUnion) string {
	if voice.OfAudioSpeechNewsVoiceID != nil {
		return voice.OfAudioSpeechNewsVoiceID.ID
	}
	if voice.OfAudioSpeechNewsVoiceString2.Valid() {
		return voice.OfAudioSpeechNewsVoiceString2.Value
	}
	return voice.OfString.Value
}

func buildAudioErrorPayload(model, subtype string, md map[string]interface{}, duration time.Duration, provider Provider, requestTime time.Time, err error) *metering.MeteringPayload {
	builder := provider.mediaRecord(metering.OperationAudio, model).
		WithTiming(requestTime, duration).
		WithOperationSubtype(subtype)
	return failedCallRecord(builder, err, md)
}
