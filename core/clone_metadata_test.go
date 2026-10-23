package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCloneMetadata_NestedValuesAreIndependentOfTheOriginal(t *testing.T) {
	original := map[string]interface{}{
		"productName": "p",
		"subscriber":  map[string]interface{}{"email": "a@example.com", "credential": map[string]interface{}{"name": "k"}},
		"tags":        []interface{}{"x", map[string]interface{}{"y": "z"}},
		"labels":      map[string]string{"team": "t"},
		"ids":         []string{"1"},
	}

	clone := CloneMetadata(original)
	original["productName"] = "changed"
	original["subscriber"].(map[string]interface{})["email"] = "b@example.com"
	original["subscriber"].(map[string]interface{})["credential"].(map[string]interface{})["name"] = "other"
	original["tags"].([]interface{})[1].(map[string]interface{})["y"] = "changed"
	original["labels"].(map[string]string)["team"] = "changed"
	original["ids"].([]string)[0] = "changed"

	assert.Equal(t, map[string]interface{}{
		"productName": "p",
		"subscriber":  map[string]interface{}{"email": "a@example.com", "credential": map[string]interface{}{"name": "k"}},
		"tags":        []interface{}{"x", map[string]interface{}{"y": "z"}},
		"labels":      map[string]string{"team": "t"},
		"ids":         []string{"1"},
	}, clone)
}

func TestCloneMetadata_NilStaysNil(t *testing.T) {
	assert.Nil(t, CloneMetadata(nil))
}
