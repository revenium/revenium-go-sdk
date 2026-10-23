package metering

import "github.com/revenium/revenium-go-sdk/core"

const environmentSource = "the environment"

// recordDefaults returns the values the client puts on records that leave a
// field unset, checked once here so a value the platform would refuse is
// reported once instead of on every record.
func recordDefaults(cfg MeteringClientConfig) map[string]interface{} {
	loaded := core.RecordDefaults(&core.ReveniumConfig{OrgID: cfg.OrganizationName, ProductID: cfg.ProductName})
	checked := make(map[string]interface{}, len(loaded))
	for key, value := range loaded {
		if key == retryNumberKey {
			if retry, isCount := value.(int); isCount && !isSendableRetryNumber(retry) {
				warnDroppedRetryNumber(retry, environmentSource)
				continue
			}
			checked[key] = value
			continue
		}
		text, isText := value.(string)
		if stringFields[key] == nil || !isText {
			core.Warn("[METERING] ignoring the default for %s: it is not a record field the SDK fills", key)
			continue
		}
		if text = guardFieldValue(key, text, environmentSource); text != "" {
			checked[key] = text
		}
	}
	return checked
}

func fillUnsetFields(payload *MeteringPayload, defaults map[string]interface{}) {
	for key, value := range defaults {
		if key == retryNumberKey {
			if retry, ok := value.(int); ok && payload.RetryNumber == nil {
				payload.RetryNumber = &retry
			}
			continue
		}
		if field := (*stringFields[key])(payload); core.IsBlank(*field) {
			*field = value.(string)
		}
	}
}
