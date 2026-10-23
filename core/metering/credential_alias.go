package metering

import "github.com/revenium/revenium-go-sdk/core"

// subscriberWithCredentialAlias returns the subscriber the record carries once
// the credential alias is applied as subscriber.credential.name, the only
// place Revenium stores a credential name, when core.CredentialAliasFor gives one.
// The caller's maps are never modified.
func subscriberWithCredentialAlias(subscriber map[string]interface{}, alias string) map[string]interface{} {
	credential, opaque := core.SubscriberCredential(subscriber)
	if opaque {
		return subscriber
	}
	alias = core.CredentialAliasFor(alias, stringValue(subscriber["id"]), stringValue(subscriber["email"]), stringValue(credential["name"]), stringValue(credential["value"]))
	if alias == "" {
		return subscriber
	}
	named := make(map[string]interface{}, len(credential)+1)
	for key, value := range credential {
		named[key] = value
	}
	named["name"] = alias
	applied := make(map[string]interface{}, len(subscriber)+1)
	for key, value := range subscriber {
		applied[key] = value
	}
	applied["credential"] = named
	return applied
}

func stringValue(value interface{}) string {
	s, _ := value.(string)
	return s
}
