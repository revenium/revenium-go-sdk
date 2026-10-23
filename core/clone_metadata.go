package core

// CloneMetadata returns a deep copy of a usage-metadata map, copying nested maps and slices, so a
// record built later from the copy is not affected by the caller reusing or changing the original.
func CloneMetadata(metadata map[string]interface{}) map[string]interface{} {
	if metadata == nil {
		return nil
	}
	clone := make(map[string]interface{}, len(metadata))
	for key, value := range metadata {
		clone[key] = cloneMetadataValue(value)
	}
	return clone
}

func cloneMetadataValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return CloneMetadata(v)
	case map[string]string:
		clone := make(map[string]string, len(v))
		for key, s := range v {
			clone[key] = s
		}
		return clone
	case []interface{}:
		clone := make([]interface{}, len(v))
		for i, item := range v {
			clone[i] = cloneMetadataValue(item)
		}
		return clone
	case []string:
		return append([]string(nil), v...)
	default:
		return v
	}
}
