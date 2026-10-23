package openai

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	whisperDefaultBody     = `{"text":"hello","usage":{"type":"duration","seconds":42}}`
	whisperVerboseBody     = `{"task":"transcribe","language":"english","duration":37.5,"text":"hello","segments":[]}`
	tokenTranscriptionBody = `{"text":"hello","usage":{"type":"tokens","input_tokens":320,"input_token_details":{"audio_tokens":300,"text_tokens":20},"output_tokens":25,"total_tokens":345}}`
	tokensWithoutDetails   = `{"text":"hello","usage":{"type":"tokens","input_tokens":310,"output_tokens":25,"total_tokens":335}}`
	translationJSONBody    = `{"text":"hello"}`
	translationVerboseBody = `{"task":"translate","language":"spanish","duration":42,"text":"hello","segments":[]}`
)

type audioProviderStub struct {
	mu              sync.Mutex
	transcription   string
	responseFormats []string
}

func (s *audioProviderStub) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(1<<20))
		format := r.FormValue("response_format")
		s.mu.Lock()
		s.responseFormats = append(s.responseFormats, format)
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/audio/transcriptions"):
			_, _ = w.Write([]byte(s.transcription))
		case format == "verbose_json":
			_, _ = w.Write([]byte(translationVerboseBody))
		default:
			_, _ = w.Write([]byte(translationJSONBody))
		}
	})
}

func (s *audioProviderStub) formatsSent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.responseFormats...)
}

type audioProviderCase struct {
	name   string
	server func(http.Handler) *httptest.Server
	newSDK func(t *testing.T, providerServer *httptest.Server, meteringURL string) *ReveniumOpenAI
}

func audioProviders() []audioProviderCase {
	return []audioProviderCase{
		{name: "openai", server: httptest.NewServer, newSDK: newTestOpenAIFromServer},
		{name: "azure", server: httptest.NewTLSServer, newSDK: newTestAzureOpenAI},
	}
}

func recordAudioCall(t *testing.T, provider audioProviderCase, stub *audioProviderStub, call func(r *ReveniumOpenAI) error) map[string]interface{} {
	t.Helper()
	return recordProviderCall(t, provider, stub.handler(t), call)
}

func recordProviderCall(t *testing.T, provider audioProviderCase, handler http.Handler, call func(r *ReveniumOpenAI) error) map[string]interface{} {
	t.Helper()
	providerServer := provider.server(handler)
	defer providerServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := provider.newSDK(t, providerServer, mock.URL())
	defer r.Close()

	require.NoError(t, call(r))
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	return payloads[0]
}

func transcribe(model openaisdk.AudioModel, format openaisdk.AudioResponseFormat) func(r *ReveniumOpenAI) error {
	return func(r *ReveniumOpenAI) error {
		_, err := r.Audio().Transcriptions().Create(context.Background(), openaisdk.AudioTranscriptionNewParams{
			File:           openaisdk.File(bytes.NewReader([]byte("wav")), "a.wav", "audio/wav"),
			Model:          model,
			ResponseFormat: format,
		})
		return err
	}
}

func translate(format openaisdk.AudioTranslationNewParamsResponseFormat, text *string) func(r *ReveniumOpenAI) error {
	return func(r *ReveniumOpenAI) error {
		resp, err := r.Audio().Translations().Create(context.Background(), openaisdk.AudioTranslationNewParams{
			File:           openaisdk.File(bytes.NewReader([]byte("wav")), "a.wav", "audio/wav"),
			Model:          openaisdk.AudioModelWhisper1,
			ResponseFormat: format,
		})
		if resp != nil {
			*text = resp.Text
		}
		return err
	}
}

func TestTranscription_RecordsBillableQuantityForEveryResponseShape(t *testing.T) {
	cases := []struct {
		name         string
		model        openaisdk.AudioModel
		format       openaisdk.AudioResponseFormat
		body         string
		wantSeconds  interface{}
		wantInput    float64
		wantOutput   float64
		wantTotal    float64
		wantAudioIn  interface{}
		wantBillUnit string
	}{
		{name: "whisper default format", model: openaisdk.AudioModelWhisper1, body: whisperDefaultBody, wantSeconds: float64(42), wantBillUnit: "PER_SECOND"},
		{name: "whisper verbose format", model: openaisdk.AudioModelWhisper1, format: openaisdk.AudioResponseFormatVerboseJSON, body: whisperVerboseBody, wantSeconds: 37.5, wantBillUnit: "PER_SECOND"},
		{name: "token-billed model bills only the audio input tokens", model: openaisdk.AudioModelGPT4oTranscribe, body: tokenTranscriptionBody, wantInput: 320, wantOutput: 25, wantTotal: 345, wantAudioIn: float64(300), wantBillUnit: "PER_TOKEN"},
		{name: "token-billed model without input details falls back to input tokens", model: openaisdk.AudioModelGPT4oTranscribe, body: tokensWithoutDetails, wantInput: 310, wantOutput: 25, wantTotal: 335, wantAudioIn: float64(310), wantBillUnit: "PER_TOKEN"},
	}

	for _, provider := range audioProviders() {
		for _, tc := range cases {
			t.Run(provider.name+"/"+tc.name, func(t *testing.T) {
				stub := &audioProviderStub{transcription: tc.body}
				p := recordAudioCall(t, provider, stub, transcribe(tc.model, tc.format))

				assert.Equal(t, "transcription", p["operationSubtype"])
				assert.Equal(t, tc.wantSeconds, p["durationSeconds"])
				assert.Equal(t, tc.wantInput, p["inputTokenCount"])
				assert.Equal(t, tc.wantOutput, p["outputTokenCount"])
				assert.Equal(t, tc.wantTotal, p["totalTokenCount"])
				assert.Equal(t, tc.wantAudioIn, p["inputAudioTokenCount"])
				assert.Equal(t, tc.wantBillUnit, p["billingUnit"])
				assert.NotContains(t, p, "attributes")
			})
		}
	}
}

func TestTranslation_RecordsAudioLengthForJSONResponseFormats(t *testing.T) {
	cases := []struct {
		name       string
		format     openaisdk.AudioTranslationNewParamsResponseFormat
		wantFormat interface{}
	}{
		{name: "default format", format: "", wantFormat: nil},
		{name: "json format", format: openaisdk.AudioTranslationNewParamsResponseFormatJSON, wantFormat: "json"},
		{name: "verbose format", format: openaisdk.AudioTranslationNewParamsResponseFormatVerboseJSON, wantFormat: "verbose_json"},
	}

	for _, provider := range audioProviders() {
		for _, tc := range cases {
			t.Run(provider.name+"/"+tc.name, func(t *testing.T) {
				stub := &audioProviderStub{}
				var text string
				p := recordAudioCall(t, provider, stub, translate(tc.format, &text))

				assert.Equal(t, []string{"verbose_json"}, stub.formatsSent(), "the SDK must ask for the format that reports the audio length")
				assert.Equal(t, "hello", text)
				assert.Equal(t, "translation", p["operationSubtype"])
				assert.Equal(t, float64(42), p["durationSeconds"])
				assert.Equal(t, "PER_SECOND", p["billingUnit"])
				assert.Equal(t, "en", p["targetLanguage"])
				assert.Equal(t, tc.wantFormat, p["responseFormat"], "the record keeps the format the caller asked for")
			})
		}
	}
}

type recordingLogger struct {
	core.Logger
	mu    sync.Mutex
	warns []string
}

func (l *recordingLogger) Warn(message string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, fmt.Sprintf(message, args...))
}

func (l *recordingLogger) warnings() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.warns...)
}

func TestAudio_MissingQuantityIsRecordedAndWarnedOnce(t *testing.T) {
	previous := core.GetLogger()
	logger := &recordingLogger{Logger: previous}
	core.SetLogger(logger)
	defer core.SetLogger(previous)
	missingAudioQuantityWarning = sync.Once{}

	provider := audioProviders()[0]
	for i := 0; i < 2; i++ {
		stub := &audioProviderStub{transcription: `{"text":"hello"}`}
		p := recordAudioCall(t, provider, stub, transcribe(openaisdk.AudioModelWhisper1, ""))
		assert.Equal(t, float64(0), p["durationSeconds"], "a per-second record must carry a length, even zero, or the platform drops it")
		assert.Equal(t, "PER_SECOND", p["billingUnit"])
		assert.Equal(t, float64(0), p["inputTokenCount"])
	}

	var quantityWarnings []string
	for _, w := range logger.warnings() {
		if strings.Contains(w, "no audio duration or token usage") {
			quantityWarnings = append(quantityWarnings, w)
		}
	}
	require.Len(t, quantityWarnings, 1)
	assert.Contains(t, quantityWarnings[0], "whisper-1")
}

func TestTranslation_WithoutAnAudioLengthSendsAZeroLength(t *testing.T) {
	for _, provider := range audioProviders() {
		t.Run(provider.name, func(t *testing.T) {
			var text string
			stub := &audioProviderStub{}

			p := recordAudioCall(t, provider, stub, translate(openaisdk.AudioTranslationNewParamsResponseFormatText, &text))

			assert.Equal(t, []string{"text"}, stub.formatsSent())
			assert.Equal(t, float64(0), p["durationSeconds"])
			assert.Equal(t, "PER_SECOND", p["billingUnit"])
		})
	}
}
