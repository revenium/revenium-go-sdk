package metering

import (
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

const (
	SubtypeGeneration    = "generation"
	SubtypeEdit          = "edit"
	SubtypeVariation     = "variation"
	SubtypeUpscale       = "upscale"
	SubtypeInpainting    = "inpainting"
	SubtypeExtend        = "extend"
	SubtypeTranscription = "transcription"
	SubtypeTranslation   = "translation"
	SubtypeSynthesis     = "synthesis"
	SubtypeSpeech        = "speech"
	SubtypeTTS           = "tts"
	SubtypeRealtime      = "realtime"
)

var acceptedOperationSubtypes = map[OperationType]map[string]struct{}{
	OperationImage: subtypeSet(SubtypeGeneration, SubtypeEdit, SubtypeVariation, SubtypeUpscale, SubtypeInpainting),
	OperationAudio: subtypeSet(SubtypeTranscription, SubtypeTranslation, SubtypeSynthesis, SubtypeSpeech, SubtypeTTS, SubtypeRealtime),
	OperationVideo: subtypeSet(SubtypeGeneration, SubtypeUpscale, SubtypeExtend, SubtypeEdit),
}

func subtypeSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return set
}

func applyOperationSubtype(p *MeteringPayload, value string) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return
	}
	accepted, multimodal := acceptedOperationSubtypes[OperationType(p.OperationType)]
	if !multimodal {
		return
	}
	if _, ok := accepted[normalized]; !ok {
		core.Warn("Ignoring unsupported operationSubtype '%s' for %s; keeping '%s'", value, p.OperationType, p.OperationSubtype)
		return
	}
	p.OperationSubtype = normalized
}
