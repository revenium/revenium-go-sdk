package metering

// BillingUnit is the unit the platform prices a media record by. The platform keys its
// media price lookup on it, so a record with the wrong unit finds no price.
type BillingUnit string

const (
	BillingPerImage     BillingUnit = "PER_IMAGE"
	BillingPerSecond    BillingUnit = "PER_SECOND"
	BillingPerCharacter BillingUnit = "PER_CHARACTER"
	BillingPerToken     BillingUnit = "PER_TOKEN"
)

type ImageDetails struct {
	Resolution string
	Quality    string
	Style      string
	Format     string
}

type AudioDetails struct {
	Language       string
	TargetLanguage string
	ResponseFormat string
	Voice          string
	Speed          *float64
}

func (b *PayloadBuilder) WithBillingUnit(unit BillingUnit) *PayloadBuilder {
	b.payload.BillingUnit = unit
	return b
}

func (b *PayloadBuilder) WithImageDetails(details ImageDetails) *PayloadBuilder {
	b.payload.Resolution = details.Resolution
	b.payload.Quality = details.Quality
	b.payload.Style = details.Style
	b.payload.Format = details.Format
	return b
}

func (b *PayloadBuilder) WithAudioDetails(details AudioDetails) *PayloadBuilder {
	b.payload.Language = details.Language
	b.payload.TargetLanguage = details.TargetLanguage
	b.payload.ResponseFormat = details.ResponseFormat
	b.payload.Voice = details.Voice
	b.payload.Speed = details.Speed
	return b
}

func (b *PayloadBuilder) WithResolution(resolution string) *PayloadBuilder {
	b.payload.Resolution = resolution
	return b
}

func (b *PayloadBuilder) WithCharacterCount(count int64) *PayloadBuilder {
	b.payload.CharacterCount = &count
	return b
}

func (b *PayloadBuilder) WithInputAudioTokens(count int64) *PayloadBuilder {
	b.payload.InputAudioTokenCount = &count
	return b
}
