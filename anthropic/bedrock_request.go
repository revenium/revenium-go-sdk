package anthropic

import (
	"encoding/json"
	"errors"
	"sort"
)

// ErrBedrockCannotCarry is returned, wrapped with the part it names, when a
// request holds something the Bedrock API in use has no field for. The
// request is refused rather than sent without that part.
var ErrBedrockCannotCarry = errors.New("bedrock cannot carry this part of the request")

// anthropicOnlyFields are left out of the Bedrock body rather than refused:
// model, which Bedrock takes in the request URL instead, and metadata and
// service_tier, which steer Anthropic's own service and have no Bedrock
// counterpart. Removing model from this set makes every Bedrock call fail.
var anthropicOnlyFields = map[string]bool{"model": true, "metadata": true, "service_tier": true}

type jsonObject map[string]json.RawMessage

func jsonObjectOf(v interface{}) (jsonObject, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var object jsonObject
	err = json.Unmarshal(encoded, &object)
	return object, err
}

func sortedKeys(object jsonObject) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
