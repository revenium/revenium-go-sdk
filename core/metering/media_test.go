package metering

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wireFields(t *testing.T, p *MeteringPayload) map[string]interface{} {
	t.Helper()
	body, err := json.Marshal(p)
	require.NoError(t, err)
	var fields map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &fields))
	return fields
}

func TestImagePricingFields_AreSentAtTopLevel(t *testing.T) {
	p := NewPayload(OperationImage, "gpt-image-1", "openai").
		WithImageBilling(1, 1).
		WithBillingUnit(BillingPerImage).
		WithImageDetails(ImageDetails{Resolution: "1024x1024", Quality: "high", Style: "vivid", Format: "b64_json"}).
		Build()

	fields := wireFields(t, p)
	assert.Equal(t, "PER_IMAGE", fields["billingUnit"])
	assert.Equal(t, "1024x1024", fields["resolution"])
	assert.Equal(t, "high", fields["quality"])
	assert.Equal(t, "vivid", fields["style"])
	assert.Equal(t, "b64_json", fields["format"])
	assert.NotContains(t, fields, "attributes")
}

func TestAudioPricingFields_AreSentAtTopLevel(t *testing.T) {
	speed := 1.25
	p := NewPayload(OperationAudio, "tts-1", "openai").
		WithBillingUnit(BillingPerCharacter).
		WithCharacterCount(11).
		WithInputAudioTokens(300).
		WithAudioDetails(AudioDetails{Language: "en", TargetLanguage: "en", ResponseFormat: "mp3", Voice: "alloy", Speed: &speed}).
		Build()

	fields := wireFields(t, p)
	assert.Equal(t, "PER_CHARACTER", fields["billingUnit"])
	assert.Equal(t, float64(11), fields["characterCount"])
	assert.Equal(t, float64(300), fields["inputAudioTokenCount"])
	assert.Equal(t, "en", fields["language"])
	assert.Equal(t, "en", fields["targetLanguage"])
	assert.Equal(t, "mp3", fields["responseFormat"])
	assert.Equal(t, "alloy", fields["voice"])
	assert.Equal(t, 1.25, fields["speed"])
}

func TestMediaFields_StayOffRecordsThatDoNotSetThem(t *testing.T) {
	fields := wireFields(t, NewPayload(OperationChat, "gpt-4.1-nano", "OPENAI").Build())

	for _, name := range []string{"billingUnit", "resolution", "quality", "style", "format", "characterCount", "inputAudioTokenCount", "language", "targetLanguage", "responseFormat", "voice", "speed"} {
		assert.NotContains(t, fields, name)
	}
}
