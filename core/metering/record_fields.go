package metering

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

type recordEndpoint uint8

const (
	completionRecord recordEndpoint = 1 << iota
	imageRecord
	audioRecord
	videoRecord

	everyRecord        = completionRecord | imageRecord | audioRecord | videoRecord
	imageOrVideoRecord = imageRecord | videoRecord
)

func recordEndpointOf(operationType string) recordEndpoint {
	switch OperationType(operationType) {
	case OperationImage:
		return imageRecord
	case OperationAudio:
		return audioRecord
	case OperationVideo:
		return videoRecord
	default:
		return completionRecord
	}
}

// recordField is one usage-metadata key the platform accepts on the records
// of endpoints. The checks mirror the platform's own validation, which
// refuses the whole record with a 400 for one bad optional value, so a value
// that would fail is left off with a warning and the call is still recorded.
type recordField struct {
	endpoints recordEndpoint
	apply     func(p *MeteringPayload, key string, value interface{})
}

const (
	maxAgentVersionUnits = 64
	maxShortSlugLength   = 16
	maxPromptIDLength    = 64
	maxClientSurface     = 64
	maxCallSourceLength  = 128
	maxPromptLength      = math.MaxInt32
)

var (
	slugPattern       = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	callSourcePattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	surfacePattern    = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	pricingTiers      = map[string]struct{}{"STANDARD": {}, "BATCH": {}}
)

var recordFields = map[string]recordField{
	"agentVersion":         {everyRecord, text(func(p *MeteringPayload) *string { return &p.AgentVersion }, truncatedTo(maxAgentVersionUnits))},
	"squadId":              {everyRecord, text(func(p *MeteringPayload) *string { return &p.SquadID }, asIs)},
	"squadName":            {everyRecord, text(func(p *MeteringPayload) *string { return &p.SquadName }, asIs)},
	"squadRole":            {everyRecord, text(func(p *MeteringPayload) *string { return &p.SquadRole }, asIs)},
	"pricingTier":          {everyRecord, text(func(p *MeteringPayload) *string { return &p.PricingTier }, pricingTier)},
	"requestedServiceTier": {everyRecord, text(func(p *MeteringPayload) *string { return &p.RequestedServiceTier }, asIs)},
	"actualServiceTier":    {everyRecord, text(func(p *MeteringPayload) *string { return &p.ActualServiceTier }, asIs)},
	"billingSkipped":       {everyRecord, flag(func(p *MeteringPayload, v bool) { p.BillingSkipped = &v })},

	"subscriptionTier":           {completionRecord, text(func(p *MeteringPayload) *string { return &p.SubscriptionTier }, asIs)},
	"costMultiplier":             {completionRecord, finiteNumber(func(p *MeteringPayload, v float64) { p.CostMultiplier = &v })},
	"codingAssistantAccountUuid": {completionRecord, text(func(p *MeteringPayload) *string { return &p.CodingAssistantAccountUUID }, asIs)},
	"effort":                     {completionRecord, text(func(p *MeteringPayload) *string { return &p.Effort }, matching(slugPattern, maxShortSlugLength))},
	"speed":                      {completionRecord, text(func(p *MeteringPayload) *string { return &p.SpeedMode }, matching(slugPattern, maxShortSlugLength))},
	"querySource":                {completionRecord, text(func(p *MeteringPayload) *string { return &p.QuerySource }, matching(callSourcePattern, maxCallSourceLength))},
	"subagentType":               {completionRecord, text(func(p *MeteringPayload) *string { return &p.SubagentType }, matching(callSourcePattern, maxCallSourceLength))},
	"promptId":                   {completionRecord, text(func(p *MeteringPayload) *string { return &p.PromptID }, matching(slugPattern, maxPromptIDLength))},
	"clientSurface":              {completionRecord, text(func(p *MeteringPayload) *string { return &p.ClientSurface }, matching(surfacePattern, maxClientSurface))},
	"promptLength":               {completionRecord, wholeNumber(0, maxPromptLength, func(p *MeteringPayload, v int64) { p.PromptLength = &v })},
	"skillName":                  {completionRecord, text(func(p *MeteringPayload) *string { return &p.SkillName }, asIs)},
	"skillInvocationTrigger":     {completionRecord, text(func(p *MeteringPayload) *string { return &p.SkillInvocationTrigger }, asIs)},
	"skillSource":                {completionRecord, text(func(p *MeteringPayload) *string { return &p.SkillSource }, asIs)},
	"skillKind":                  {completionRecord, text(func(p *MeteringPayload) *string { return &p.SkillKind }, asIs)},
	"skillPluginName":            {completionRecord, text(func(p *MeteringPayload) *string { return &p.SkillPluginName }, asIs)},
	"skillMarketplaceName":       {completionRecord, text(func(p *MeteringPayload) *string { return &p.SkillMarketplaceName }, asIs)},

	"sourceTransactionId": {imageOrVideoRecord, text(func(p *MeteringPayload) *string { return &p.SourceTransactionID }, asIs)},
	"priorityTier":        {imageOrVideoRecord, text(func(p *MeteringPayload) *string { return &p.PriorityTier }, asIs)},
}

func applyRecordFields(payload *MeteringPayload, metadata map[string]interface{}) {
	endpoint := recordEndpointOf(payload.OperationType)
	if endpoint == completionRecord {
		applySystemPrompt(payload, metadata)
	}
	for key, field := range recordFields {
		value, ok := metadata[key]
		if !ok || field.endpoints&endpoint == 0 {
			continue
		}
		field.apply(payload, key, value)
	}
}

type textCheck func(key, value string) (string, error)

func text(field func(*MeteringPayload) *string, check textCheck) func(*MeteringPayload, string, interface{}) {
	return func(p *MeteringPayload, key string, value interface{}) {
		s, ok := value.(string)
		if !ok {
			warnLeftOff(key, "must be text")
			return
		}
		if core.IsBlank(s) {
			return
		}
		checked, err := check(key, s)
		if err != nil {
			warnLeftOff(key, err.Error())
			return
		}
		*field(p) = checked
	}
}

func asIs(_, value string) (string, error) {
	return value, nil
}

func truncatedTo(maxUnits int) textCheck {
	return func(key, value string) (string, error) {
		return truncateUTF16(key, value, maxUnits), nil
	}
}

func matching(pattern *regexp.Regexp, maxLength int) textCheck {
	return func(_, value string) (string, error) {
		if len(value) > maxLength || !pattern.MatchString(value) {
			return "", fmt.Errorf("must be 1 to %d characters matching %s", maxLength, pattern)
		}
		return value, nil
	}
}

func pricingTier(_, value string) (string, error) {
	tier := strings.ToUpper(strings.TrimSpace(value))
	if _, ok := pricingTiers[tier]; !ok {
		return "", fmt.Errorf("must be STANDARD or BATCH")
	}
	return tier, nil
}

func flag(set func(*MeteringPayload, bool)) func(*MeteringPayload, string, interface{}) {
	return func(p *MeteringPayload, key string, value interface{}) {
		switch v := value.(type) {
		case bool:
			set(p, v)
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "true":
				set(p, true)
			case "false":
				set(p, false)
			case "":
			default:
				warnLeftOff(key, "must be true or false")
			}
		default:
			warnLeftOff(key, "must be true or false")
		}
	}
}

func finiteNumber(set func(*MeteringPayload, float64)) func(*MeteringPayload, string, interface{}) {
	return func(p *MeteringPayload, key string, value interface{}) {
		f, ok := metadataNumber(value)
		if !ok || !isFinite(f) {
			warnLeftOff(key, "must be a finite number")
			return
		}
		set(p, f)
	}
}

func wholeNumber(min, max int64, set func(*MeteringPayload, int64)) func(*MeteringPayload, string, interface{}) {
	return func(p *MeteringPayload, key string, value interface{}) {
		f, ok := metadataNumber(value)
		if !ok || f != math.Trunc(f) || f < float64(min) || f > float64(max) {
			warnLeftOff(key, fmt.Sprintf("must be a whole number from %d to %d", min, max))
			return
		}
		set(p, int64(f))
	}
}

func warnLeftOff(key, rule string) {
	core.Warn("[METERING] leaving %s off the usage record: it %s; the call is still recorded", key, rule)
}
