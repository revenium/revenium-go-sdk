package openai

import (
	"testing"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
)

func TestImageRecord_CarriesAttribution(t *testing.T) {
	response := `{"created":1,"data":[{"url":"https://x/i.png"}]}`

	for _, provider := range audioProviders() {
		t.Run(provider.name, func(t *testing.T) {
			record := recordProviderCall(t, provider, imageResponder(response), func(r *ReveniumOpenAI) error {
				_, err := r.Images().Generate(attributedContext(), openaisdk.ImageGenerateParams{
					Prompt: "a cat",
					Model:  openaisdk.ImageModelDallE3,
				})
				return err
			})

			assert.Equal(t, float64(1), record["actualImageCount"])
			assertAttribution(t, record)
		})
	}
}
