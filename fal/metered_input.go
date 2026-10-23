package fal

import (
	"encoding/json"
	"reflect"
	"strconv"
)

var meteredScalarInputs = append([]string{"duration", "num_images"}, append(frameCountInputs, frameRateInputs...)...)

// meteredInputFields keeps only the request values a usage record is built from, so a record sent
// after the call returns neither holds the request's media nor reads maps the caller may reuse.
// Every record is built from this subset, so it must cover what promptFromInput,
// requestedVideoSeconds and requestedImageCount read.
func meteredInputFields(input map[string]interface{}) map[string]interface{} {
	fields := map[string]interface{}{}
	if prompt := promptFromInput(input); prompt != "" {
		fields["prompt"] = prompt
	}
	for _, key := range meteredScalarInputs {
		value := input[key]
		if _, isText := value.(string); isText {
			fields[key] = value
		} else if _, isNumber := numberValue(value); isNumber {
			fields[key] = value
		}
	}
	return fields
}

// numberValue reads any value encoding/json sends as a JSON number: every Go integer and float kind,
// named types built on them, and json.Number. A typed value such as int32(161) reaches fal as 161, so
// the record must read it too.
func numberValue(value interface{}) (float64, bool) {
	if number, ok := value.(json.Number); ok {
		parsed, err := number.Float64()
		return parsed, err == nil
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(v.Uint()), true
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	}
	return 0, false
}

func requestedImageCount(input map[string]interface{}) int {
	count, _ := numberValue(input["num_images"])
	if text, ok := input["num_images"].(string); ok {
		count, _ = strconv.ParseFloat(text, 64)
	}
	if count < 1 {
		return 1
	}
	return int(count)
}
