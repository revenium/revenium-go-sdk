package metering

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sentRecord(t *testing.T, metadata map[string]interface{}) map[string]interface{} {
	t.Helper()
	var record map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&record))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	payload := NewPayload(OperationChat, "gpt-4.1-nano", "OPENAI").Build()
	ApplyMetadata(payload, metadata)
	require.NoError(t, newTestClient(t, srv.URL).SendSync(payload))
	return record
}

func TestCredentialAlias_IsSentAsTheSubscriberCredentialName(t *testing.T) {
	record := sentRecord(t, map[string]interface{}{"credentialAlias": "prod-openai-key"})

	assert.Equal(t, map[string]interface{}{"credential": map[string]interface{}{"name": "prod-openai-key"}}, record["subscriber"])
	assert.NotContains(t, record, "credentialAlias", "the platform ignores a top-level credentialAlias")
}

func TestCredentialAlias_FillsTheNameOfAnUnnamedCredential(t *testing.T) {
	record := sentRecord(t, map[string]interface{}{
		"credentialAlias": "prod-openai-key",
		"subscriber":      map[string]interface{}{"credential": map[string]interface{}{"value": ""}},
	})

	assert.Equal(t, map[string]interface{}{"credential": map[string]interface{}{"name": "prod-openai-key", "value": ""}}, record["subscriber"])
}

func TestCredentialAlias_LeavesSubscribersTheCallerAlreadyIdentified(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"named credential":  {"credential": map[string]interface{}{"name": "caller-key"}},
		"valued credential": {"credential": map[string]interface{}{"value": "pk-123"}},
		"subscriber id":     {"id": "user-1"},
		"subscriber email":  {"email": "user@example.com"},
	}
	for name, subscriber := range cases {
		t.Run(name, func(t *testing.T) {
			record := sentRecord(t, map[string]interface{}{"credentialAlias": "prod-openai-key", "subscriber": subscriber})

			assert.Equal(t, normalizeJSON(t, subscriber), record["subscriber"])
		})
	}
}

func TestCredentialAlias_NeverModifiesTheCallersSubscriber(t *testing.T) {
	credential := map[string]interface{}{}
	subscriber := map[string]interface{}{"credential": credential}

	sentRecord(t, map[string]interface{}{"credentialAlias": "prod-openai-key", "subscriber": subscriber})

	assert.Empty(t, credential)
	assert.Equal(t, map[string]interface{}{"credential": credential}, subscriber)
}

func normalizeJSON(t *testing.T, value interface{}) interface{} {
	t.Helper()
	body, err := json.Marshal(value)
	require.NoError(t, err)
	var decoded interface{}
	require.NoError(t, json.Unmarshal(body, &decoded))
	return decoded
}

func TestCredentialAlias_APaddedAliasIsRecordedUnderTheTrimmedName(t *testing.T) {
	record := sentRecord(t, map[string]interface{}{"credentialAlias": " prod-key "})

	assert.Equal(t, map[string]interface{}{"credential": map[string]interface{}{"name": "prod-key"}}, record["subscriber"])
}

func TestCredentialAlias_ReadsAStringMapCredentialAndKeepsAnUnreadableOne(t *testing.T) {
	named := sentRecord(t, map[string]interface{}{"credentialAlias": "team-key", "subscriber": map[string]interface{}{"credential": map[string]string{"name": "caller-key"}}})
	unnamed := sentRecord(t, map[string]interface{}{"credentialAlias": "team-key", "subscriber": map[string]interface{}{"credential": map[string]string{"value": ""}}})
	unreadable := sentRecord(t, map[string]interface{}{"credentialAlias": "team-key", "subscriber": map[string]interface{}{"credential": "caller-key"}})

	assert.Equal(t, map[string]interface{}{"credential": map[string]interface{}{"name": "caller-key"}}, named["subscriber"])
	assert.Equal(t, map[string]interface{}{"credential": map[string]interface{}{"name": "team-key", "value": ""}}, unnamed["subscriber"])
	assert.Equal(t, map[string]interface{}{"credential": "caller-key"}, unreadable["subscriber"])
}
