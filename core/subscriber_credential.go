package core

// SubscriberCredential returns the credential entry of a usage-metadata
// subscriber in the shapes the SDK reads (map[string]interface{} or
// map[string]string). opaque reports an entry of any other type: the caller
// set a credential the SDK cannot read, so the record keeps it as given and
// neither the record nor the cost-control check lets an alias replace it.
// The usage record and the cost-control context both read the credential
// through it.
func SubscriberCredential(subscriber map[string]interface{}) (credential map[string]interface{}, opaque bool) {
	switch entry := subscriber["credential"].(type) {
	case nil:
		return nil, false
	case map[string]interface{}:
		return entry, false
	case map[string]string:
		converted := make(map[string]interface{}, len(entry))
		for key, value := range entry {
			converted[key] = value
		}
		return converted, false
	default:
		return nil, true
	}
}
