package openai

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openaisdk "github.com/openai/openai-go/v3"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func multimodalOpenAIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/audio/speech"):
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("mp3-bytes"))
		case strings.HasSuffix(r.URL.Path, "/audio/transcriptions"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"text":"hello","duration":1.5,"language":"en"}`))
		case strings.HasSuffix(r.URL.Path, "/audio/translations"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"text":"hello"}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"created":1700000000,"data":[{"url":"https://x/i.png"}]}`))
		}
	})
}

func failingOpenAIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"boom","type":"invalid_request_error"}}`))
	})
}

func capturedMultimodalPayload(t *testing.T, ctx context.Context, provider http.Handler, call func(ctx context.Context, r *ReveniumOpenAI) error) (map[string]interface{}, error) {
	t.Helper()
	openaiServer := httptest.NewServer(provider)
	defer openaiServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestOpenAI(t, openaiServer.URL, mock.URL())
	defer r.Close()

	err := call(ctx, r)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	return payloads[0], err
}

type multimodalPath struct {
	name string
	op   string
	want string
	call func(ctx context.Context, r *ReveniumOpenAI) error
}

func audioFile() openaisdk.AudioTranscriptionNewParams {
	return openaisdk.AudioTranscriptionNewParams{
		File:  openaisdk.File(bytes.NewReader([]byte("wav")), "a.wav", "audio/wav"),
		Model: openaisdk.AudioModelWhisper1,
	}
}

func multimodalPaths() []multimodalPath {
	return []multimodalPath{
		{
			name: "audio transcription",
			op:   "AUDIO",
			want: "transcription",
			call: func(ctx context.Context, r *ReveniumOpenAI) error {
				_, err := r.Audio().Transcriptions().Create(ctx, audioFile())
				return err
			},
		},
		{
			name: "audio translation",
			op:   "AUDIO",
			want: "translation",
			call: func(ctx context.Context, r *ReveniumOpenAI) error {
				_, err := r.Audio().Translations().Create(ctx, openaisdk.AudioTranslationNewParams{
					File:  openaisdk.File(bytes.NewReader([]byte("wav")), "a.wav", "audio/wav"),
					Model: openaisdk.AudioModelWhisper1,
				})
				return err
			},
		},
		{
			name: "audio speech ships as tts",
			op:   "AUDIO",
			want: "tts",
			call: func(ctx context.Context, r *ReveniumOpenAI) error {
				resp, err := r.Audio().Speech().Create(ctx, openaisdk.AudioSpeechNewParams{
					Input: "hello",
					Model: openaisdk.SpeechModelTTS1,
					Voice: openaisdk.AudioSpeechNewParamsVoiceUnion{
						OfAudioSpeechNewsVoiceString2: openaisdk.String(string(openaisdk.AudioSpeechNewParamsVoiceString2Alloy)),
					},
				})
				if resp != nil {
					resp.Body.Close()
				}
				return err
			},
		},
		{
			name: "image generation",
			op:   "IMAGE",
			want: "generation",
			call: func(ctx context.Context, r *ReveniumOpenAI) error {
				_, err := r.Images().Generate(ctx, openaisdk.ImageGenerateParams{
					Prompt: "a cat",
					Model:  openaisdk.ImageModelDallE2,
				})
				return err
			},
		},
		{
			name: "image edit",
			op:   "IMAGE",
			want: "edit",
			call: func(ctx context.Context, r *ReveniumOpenAI) error {
				_, err := r.Images().Edit(ctx, openaisdk.ImageEditParams{
					Image:  openaisdk.ImageEditParamsImageUnion{OfFile: openaisdk.File(bytes.NewReader([]byte("png")), "i.png", "image/png")},
					Prompt: "a hat",
					Model:  openaisdk.ImageModelDallE2,
				})
				return err
			},
		},
		{
			name: "image variation",
			op:   "IMAGE",
			want: "variation",
			call: func(ctx context.Context, r *ReveniumOpenAI) error {
				_, err := r.Images().CreateVariation(ctx, openaisdk.ImageNewVariationParams{
					Image: openaisdk.File(bytes.NewReader([]byte("png")), "i.png", "image/png"),
					Model: openaisdk.ImageModelDallE2,
				})
				return err
			},
		},
	}
}

func TestMultimodal_TopLevelOperationSubtype(t *testing.T) {
	for _, tt := range multimodalPaths() {
		t.Run(tt.name, func(t *testing.T) {
			p, err := capturedMultimodalPayload(t, context.Background(), multimodalOpenAIHandler(), tt.call)
			require.NoError(t, err)
			assert.Equal(t, tt.op, p["operationType"])
			assert.Equal(t, tt.want, p["operationSubtype"])
			assert.Equal(t, "END", p["stopReason"])
			if attrs, ok := p["attributes"].(map[string]interface{}); ok {
				assert.NotContains(t, attrs, "operationSubtype")
			}
		})
	}
}

func TestMultimodal_ErrorPayloadKeepsOperationSubtype(t *testing.T) {
	for _, tt := range multimodalPaths() {
		t.Run(tt.name, func(t *testing.T) {
			p, err := capturedMultimodalPayload(t, context.Background(), failingOpenAIHandler(), tt.call)
			require.Error(t, err)
			assert.Equal(t, tt.op, p["operationType"])
			assert.Equal(t, tt.want, p["operationSubtype"])
			assert.Equal(t, "ERROR", p["stopReason"])
			assert.NotEmpty(t, p["errorReason"])
		})
	}
}

func TestMultimodal_MetadataOverridesDetectedSubtype(t *testing.T) {
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
		"operationSubtype": "speech",
	})
	p, err := capturedMultimodalPayload(t, ctx, multimodalOpenAIHandler(), func(ctx context.Context, r *ReveniumOpenAI) error {
		_, err := r.Audio().Transcriptions().Create(ctx, audioFile())
		return err
	})
	require.NoError(t, err)
	assert.Equal(t, "speech", p["operationSubtype"])
}

func TestMultimodal_UnsupportedMetadataOverrideKeepsDetected(t *testing.T) {
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
		"operationSubtype": "speech_synthesis",
	})
	p, err := capturedMultimodalPayload(t, ctx, multimodalOpenAIHandler(), func(ctx context.Context, r *ReveniumOpenAI) error {
		_, err := r.Audio().Transcriptions().Create(ctx, audioFile())
		return err
	})
	require.NoError(t, err)
	assert.Equal(t, "transcription", p["operationSubtype"])
}

func TestAudioVoiceLabel(t *testing.T) {
	tests := []struct {
		name  string
		voice openaisdk.AudioSpeechNewParamsVoiceUnion
		want  string
	}{
		{
			name:  "preset voice",
			voice: openaisdk.AudioSpeechNewParamsVoiceUnion{OfAudioSpeechNewsVoiceString2: openaisdk.String(string(openaisdk.AudioSpeechNewParamsVoiceString2Alloy))},
			want:  "alloy",
		},
		{
			name:  "free-form voice",
			voice: openaisdk.AudioSpeechNewParamsVoiceUnion{OfString: openaisdk.String("verse")},
			want:  "verse",
		},
		{
			name:  "custom voice id",
			voice: openaisdk.AudioSpeechNewParamsVoiceUnion{OfAudioSpeechNewsVoiceID: &openaisdk.AudioSpeechNewParamsVoiceID{ID: "voice_1234"}},
			want:  "voice_1234",
		},
		{
			name:  "unset voice",
			voice: openaisdk.AudioSpeechNewParamsVoiceUnion{},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, audioVoiceLabel(tt.voice))
		})
	}
}
