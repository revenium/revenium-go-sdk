package openai

import (
	"context"
	"net/http"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var catalogProviders = map[string]string{"openai": "openai", "azure": "azure"}

func imageResponder(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
}

func speechResponder() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("mp3-bytes"))
	})
}

func generateImage(params openaisdk.ImageGenerateParams) func(r *ReveniumOpenAI) error {
	return func(r *ReveniumOpenAI) error {
		_, err := r.Images().Generate(context.Background(), params)
		return err
	}
}

func speak(input string) func(r *ReveniumOpenAI) error {
	return func(r *ReveniumOpenAI) error {
		resp, err := r.Audio().Speech().Create(context.Background(), openaisdk.AudioSpeechNewParams{
			Input: input,
			Model: openaisdk.SpeechModelTTS1,
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

func TestImageRecord_CarriesPricingFieldsAtTopLevel(t *testing.T) {
	cases := []struct {
		name        string
		params      openaisdk.ImageGenerateParams
		response    string
		wantSize    string
		wantQuality string
		wantTokens  [3]float64
	}{
		{
			name:        "gpt-image-1 high 1024x1024",
			params:      openaisdk.ImageGenerateParams{Prompt: "a cat", Model: openaisdk.ImageModelGPTImage1, Size: openaisdk.ImageGenerateParamsSize1024x1024, Quality: openaisdk.ImageGenerateParamsQualityHigh},
			response:    `{"created":1,"data":[{"b64_json":"x"}],"quality":"high","size":"1024x1024","usage":{"input_tokens":12,"input_tokens_details":{"image_tokens":0,"text_tokens":12},"output_tokens":4160,"total_tokens":4172}}`,
			wantSize:    "1024x1024",
			wantQuality: "high",
			wantTokens:  [3]float64{12, 4160, 4172},
		},
		{
			name:        "dall-e-3 hd",
			params:      openaisdk.ImageGenerateParams{Prompt: "a cat", Model: openaisdk.ImageModelDallE3, Size: openaisdk.ImageGenerateParamsSize1024x1024, Quality: openaisdk.ImageGenerateParamsQualityHD},
			response:    `{"created":1,"data":[{"url":"https://x/i.png"}]}`,
			wantSize:    "1024x1024",
			wantQuality: "hd",
		},
		{
			name:        "gpt-image auto settings take the rendered size and quality",
			params:      openaisdk.ImageGenerateParams{Prompt: "a cat", Model: "gpt-image-1-mini", Size: openaisdk.ImageGenerateParamsSizeAuto, Quality: openaisdk.ImageGenerateParamsQualityAuto},
			response:    `{"created":1,"data":[{"b64_json":"x"}],"quality":"low","size":"1024x1536","usage":{"input_tokens":9,"output_tokens":272,"total_tokens":281}}`,
			wantSize:    "1024x1536",
			wantQuality: "low",
			wantTokens:  [3]float64{9, 272, 281},
		},
	}

	for _, provider := range audioProviders() {
		for _, tc := range cases {
			t.Run(provider.name+"/"+tc.name, func(t *testing.T) {
				p := recordProviderCall(t, provider, imageResponder(tc.response), generateImage(tc.params))

				assert.Equal(t, catalogProviders[provider.name], p["provider"])
				assert.Equal(t, "PER_IMAGE", p["billingUnit"])
				assert.Equal(t, tc.wantSize, p["resolution"])
				assert.Equal(t, tc.wantQuality, p["quality"])
				assert.Equal(t, float64(1), p["actualImageCount"])
				assert.Equal(t, tc.wantTokens[0], p["inputTokenCount"])
				assert.Equal(t, tc.wantTokens[1], p["outputTokenCount"])
				assert.Equal(t, tc.wantTokens[2], p["totalTokenCount"])
				assert.NotContains(t, p, "attributes")
			})
		}
	}
}

func TestSpeechRecord_IsBilledPerCharacterAtTopLevel(t *testing.T) {
	for _, provider := range audioProviders() {
		t.Run(provider.name, func(t *testing.T) {
			p := recordProviderCall(t, provider, speechResponder(), speak("héllo there"))

			assert.Equal(t, catalogProviders[provider.name], p["provider"])
			assert.Equal(t, "tts", p["operationSubtype"])
			assert.Equal(t, "PER_CHARACTER", p["billingUnit"])
			assert.Equal(t, float64(11), p["characterCount"])
			assert.Equal(t, "alloy", p["voice"])
			assert.Equal(t, float64(1), p["speed"])
			assert.NotContains(t, p, "attributes")
		})
	}
}

func TestTranscriptionRecord_UsesCatalogProvider(t *testing.T) {
	for _, provider := range audioProviders() {
		t.Run(provider.name, func(t *testing.T) {
			stub := &audioProviderStub{transcription: whisperVerboseBody}
			p := recordAudioCall(t, provider, stub, transcribe(openaisdk.AudioModelWhisper1, openaisdk.AudioResponseFormatVerboseJSON))

			assert.Equal(t, catalogProviders[provider.name], p["provider"])
			assert.Equal(t, "english", p["language"])
			assert.Equal(t, "verbose_json", p["responseFormat"])
		})
	}
}

func TestFailedMediaRecords_UseCatalogProvider(t *testing.T) {
	failing := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"boom","type":"invalid_request_error"}}`))
	})
	calls := map[string]func(r *ReveniumOpenAI) error{
		"image":  generateImage(openaisdk.ImageGenerateParams{Prompt: "a cat", Model: openaisdk.ImageModelDallE3}),
		"speech": speak("hi"),
	}
	for _, provider := range audioProviders() {
		for name, call := range calls {
			t.Run(provider.name+"/"+name, func(t *testing.T) {
				p := recordProviderCall(t, provider, failing, func(r *ReveniumOpenAI) error {
					require.Error(t, call(r))
					return nil
				})
				assert.Equal(t, catalogProviders[provider.name], p["provider"])
				assert.Equal(t, "ERROR", p["stopReason"])
			})
		}
	}
}

func TestMediaProvider_SpellsTheCatalogNames(t *testing.T) {
	assert.Equal(t, "openai", ProviderOpenAI.MediaProvider())
	assert.Equal(t, "azure", ProviderAzure.MediaProvider())
}
