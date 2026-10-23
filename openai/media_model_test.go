package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/azure"
	"github.com/openai/openai-go/v3/option"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const imageReply = `{"created":1,"data":[{"url":"https://x/i.png"}]}`

type sentModelRecorder struct {
	mu     sync.Mutex
	models []string
	paths  []string
}

func (s *sentModelRecorder) handler(t *testing.T, reply http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		model := requestModel(t, r)
		s.mu.Lock()
		s.models = append(s.models, model)
		s.paths = append(s.paths, r.URL.Path)
		s.mu.Unlock()
		reply.ServeHTTP(w, r)
	})
}

func (s *sentModelRecorder) sent() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.models...), append([]string(nil), s.paths...)
}

func requestModel(t *testing.T, r *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	mediaType, mediaParams, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	require.NoError(t, err)
	if strings.HasPrefix(mediaType, "multipart/") {
		form, err := multipart.NewReader(bytes.NewReader(body), mediaParams["boundary"]).ReadForm(1 << 20)
		require.NoError(t, err)
		if values := form.Value["model"]; len(values) > 0 {
			return values[0]
		}
		return ""
	}
	var payload struct {
		Model string `json:"model"`
	}
	require.NoError(t, json.Unmarshal(body, &payload))
	return payload.Model
}

type redirectTransport struct{ target *url.URL }

func (rt redirectTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.URL.Scheme, r.URL.Host = rt.target.Scheme, rt.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

func newDirectOpenAISDK(t *testing.T, providerServer *httptest.Server, meteringURL string) *ReveniumOpenAI {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "https://api.openai.com/v1/")
	target, err := url.Parse(providerServer.URL)
	require.NoError(t, err)
	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{APIKey: "hak_test_key_123", BaseURL: meteringURL})
	require.NoError(t, err)
	cfg := &Config{OpenAIAPIKey: "sk-test"}
	client := openaisdk.NewClient(append(buildClientOptions(cfg, ProviderOpenAI),
		option.WithHTTPClient(&http.Client{Transport: redirectTransport{target: target}}), option.WithMaxRetries(0))...)
	return &ReveniumOpenAI{client: client, config: cfg, provider: ProviderOpenAI, metering: mc}
}

var directOpenAI = audioProviderCase{name: "openai", server: httptest.NewServer, newSDK: newDirectOpenAISDK}

func imageFile() io.Reader {
	return openaisdk.File(bytes.NewReader([]byte("png")), "a.png", "image/png")
}

func TestImageWithoutModel_RecordsOpenAIsDefaultAndSendsTheRequestUnchanged(t *testing.T) {
	cases := []struct {
		name       string
		call       func(r *ReveniumOpenAI) error
		wantSent   string
		wantRecord string
	}{
		{name: "generation", wantRecord: "dall-e-2", call: generateImage(openaisdk.ImageGenerateParams{Prompt: "a cat"})},
		{name: "generation with a dall-e size", wantRecord: "dall-e-2", call: generateImage(openaisdk.ImageGenerateParams{
			Prompt: "a cat", Size: openaisdk.ImageGenerateParamsSize512x512,
		})},
		{name: "generation with a GPT-image-only size", wantRecord: "gpt-image-1", call: generateImage(openaisdk.ImageGenerateParams{
			Prompt: "a cat", Size: openaisdk.ImageGenerateParamsSize1536x1024,
		})},
		{name: "generation with automatic size", wantRecord: "gpt-image-1", call: generateImage(openaisdk.ImageGenerateParams{
			Prompt: "a cat", Size: openaisdk.ImageGenerateParamsSizeAuto,
		})},
		{name: "generation with a GPT-image-only option", wantRecord: "gpt-image-1", call: generateImage(openaisdk.ImageGenerateParams{
			Prompt: "a cat", Quality: openaisdk.ImageGenerateParamsQualityHigh,
		})},
		{name: "edit", wantRecord: "gpt-image-1.5", call: func(r *ReveniumOpenAI) error {
			_, err := r.Images().Edit(context.Background(), openaisdk.ImageEditParams{
				Prompt: "a hat",
				Image:  openaisdk.ImageEditParamsImageUnion{OfFile: imageFile()},
			})
			return err
		}},
		{name: "variation", wantRecord: "dall-e-2", call: func(r *ReveniumOpenAI) error {
			_, err := r.Images().CreateVariation(context.Background(), openaisdk.ImageNewVariationParams{Image: imageFile()})
			return err
		}},
		{name: "named model", wantSent: "dall-e-3", wantRecord: "dall-e-3", call: generateImage(openaisdk.ImageGenerateParams{
			Prompt: "a cat", Model: openaisdk.ImageModelDallE3,
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &sentModelRecorder{}
			p := recordProviderCall(t, directOpenAI, recorder.handler(t, imageResponder(imageReply)), tc.call)

			sent, _ := recorder.sent()
			require.Equal(t, []string{tc.wantSent}, sent)
			assert.Equal(t, tc.wantRecord, p["model"])
			assert.Equal(t, "PER_IMAGE", p["billingUnit"])
		})
	}
}

func TestImageWithoutModel_ThroughAProxyRecordsNoModel(t *testing.T) {
	recorder := &sentModelRecorder{}
	p := recordProviderCall(t, audioProviders()[0], recorder.handler(t, imageResponder(imageReply)),
		generateImage(openaisdk.ImageGenerateParams{Prompt: "a cat"}))

	sent, _ := recorder.sent()
	assert.Equal(t, []string{""}, sent)
	assert.Equal(t, "", p["model"])
}

var azureMediaDeployments = map[string]string{
	"my-dalle":   "dall-e-3",
	"my-tts":     "tts-1",
	"my-whisper": "whisper-1",
}

func newAzureMediaSDK(t *testing.T, providerServer *httptest.Server, meteringURL string) *ReveniumOpenAI {
	t.Helper()
	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{APIKey: "hak_test_key_123", BaseURL: meteringURL})
	require.NoError(t, err)
	client := openaisdk.NewClient(
		azure.WithEndpoint(providerServer.URL, "2024-10-21"),
		azure.WithAPIKey("azure-test-key"),
		option.WithHTTPClient(providerServer.Client()),
		option.WithMaxRetries(0),
	)
	cfg := &Config{AzureDeploymentModels: azureMediaDeployments}
	return &ReveniumOpenAI{client: client, config: cfg, provider: ProviderAzure, metering: mc}
}

func speakWith(model openaisdk.SpeechModel) func(r *ReveniumOpenAI) error {
	return func(r *ReveniumOpenAI) error {
		resp, err := r.Audio().Speech().Create(context.Background(), openaisdk.AudioSpeechNewParams{
			Input: "hi",
			Model: model,
			Voice: openaisdk.AudioSpeechNewParamsVoiceUnion{
				OfAudioSpeechNewsVoiceString2: openaisdk.String(string(openaisdk.AudioSpeechNewParamsVoiceString2Alloy)),
			},
		})
		if resp != nil {
			resp.Body.Close()
		}
		return err
	}
}

func translateWith(model openaisdk.AudioModel) func(r *ReveniumOpenAI) error {
	return func(r *ReveniumOpenAI) error {
		_, err := r.Audio().Translations().Create(context.Background(), openaisdk.AudioTranslationNewParams{
			File:  openaisdk.File(bytes.NewReader([]byte("wav")), "a.wav", "audio/wav"),
			Model: model,
		})
		return err
	}
}

func TestAzureMediaRecords_NameTheCatalogModelTheDeploymentMapsTo(t *testing.T) {
	transcriptionStub := &audioProviderStub{transcription: whisperDefaultBody}
	cases := []struct {
		name           string
		reply          http.Handler
		call           func(r *ReveniumOpenAI) error
		wantDeployment string
		wantModel      string
	}{
		{name: "image", reply: imageResponder(imageReply), wantDeployment: "my-dalle", wantModel: "dall-e-3",
			call: generateImage(openaisdk.ImageGenerateParams{Prompt: "a cat", Model: "my-dalle"})},
		{name: "speech", reply: speechResponder(), wantDeployment: "my-tts", wantModel: "tts-1", call: speakWith("my-tts")},
		{name: "transcription", reply: transcriptionStub.handler(t), wantDeployment: "my-whisper", wantModel: "whisper-1",
			call: transcribe("my-whisper", "")},
		{name: "translation", reply: (&audioProviderStub{}).handler(t), wantDeployment: "my-whisper", wantModel: "whisper-1",
			call: translateWith("my-whisper")},
		{name: "unmapped deployment", reply: imageResponder(imageReply), wantDeployment: "dall-e-3", wantModel: "dall-e-3",
			call: generateImage(openaisdk.ImageGenerateParams{Prompt: "a cat", Model: openaisdk.ImageModelDallE3})},
	}
	azureCase := audioProviderCase{name: "azure", server: httptest.NewTLSServer, newSDK: newAzureMediaSDK}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			var mu sync.Mutex
			reply := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				paths = append(paths, r.URL.Path)
				mu.Unlock()
				tc.reply.ServeHTTP(w, r)
			})
			p := recordProviderCall(t, azureCase, reply, tc.call)

			mu.Lock()
			defer mu.Unlock()
			require.Len(t, paths, 1)
			assert.True(t, strings.HasPrefix(paths[0], "/openai/deployments/"+tc.wantDeployment+"/"), paths[0])
			assert.Equal(t, tc.wantModel, p["model"])
			assert.Equal(t, "azure", p["provider"])
		})
	}
}

func TestAzureFailedMediaRecord_NamesTheCatalogModel(t *testing.T) {
	failing := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"boom","type":"invalid_request_error"}}`))
	})
	azureCase := audioProviderCase{name: "azure", server: httptest.NewTLSServer, newSDK: newAzureMediaSDK}
	p := recordProviderCall(t, azureCase, failing, func(r *ReveniumOpenAI) error {
		require.Error(t, speakWith("my-tts")(r))
		return nil
	})
	assert.Equal(t, "tts-1", p["model"])
	assert.Equal(t, "ERROR", p["stopReason"])
}

func TestDeploymentModels_ApplyOnlyToAzure(t *testing.T) {
	cfg := &Config{AzureDeploymentModels: azureMediaDeployments}
	assert.Equal(t, "dall-e-3", cfg.mediaModel(ProviderAzure, "my-dalle"))
	assert.Equal(t, "my-dalle", cfg.mediaModel(ProviderOpenAI, "my-dalle"))
	assert.Equal(t, "my-dalle", cfg.mediaModel(ProviderXAI, "my-dalle"))
	var unset *Config
	assert.Equal(t, "my-dalle", unset.mediaModel(ProviderAzure, "my-dalle"))
}

func TestDeploymentModels_LoadFromTheEnvironment(t *testing.T) {
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_MODELS", " my-dalle = dall-e-3 ,my-tts=tts-1,,broken, =x,y= ")
	cfg := &Config{}
	require.NoError(t, cfg.loadFromEnv())
	assert.Equal(t, map[string]string{"my-dalle": "dall-e-3", "my-tts": "tts-1"}, cfg.AzureDeploymentModels)

	explicit := &Config{AzureDeploymentModels: map[string]string{"a": "b"}}
	require.NoError(t, explicit.loadFromEnv())
	assert.Equal(t, map[string]string{"a": "b"}, explicit.AzureDeploymentModels)
}

func TestWithAzureDeploymentModels_SetsTheMapping(t *testing.T) {
	cfg := &Config{}
	WithAzureDeploymentModels(map[string]string{"my-dalle": "dall-e-3"})(cfg)
	assert.Equal(t, "dall-e-3", cfg.mediaModel(ProviderAzure, "my-dalle"))
}
