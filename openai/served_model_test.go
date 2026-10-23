package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	requestedNano = "gpt-4.1-nano"
	servedNano    = "gpt-4.1-nano-2025-04-14"
)

func servedModelField(served string) string {
	if served == "" {
		return ""
	}
	return fmt.Sprintf(`"model":%q,`, served)
}

func servingModelHandler(t *testing.T, served string) http.Handler {
	model := servedModelField(served)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		path := r.URL.Path
		if !body.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(servedAnswer(path, model)))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range servedEvents(path, model) {
			writeSSE(t, w, "data: "+event+"\n\n")
		}
		writeSSE(t, w, "data: [DONE]\n\n")
	})
}

func servedAnswer(path, model string) string {
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		return `{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,` + model +
			`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`
	case strings.HasSuffix(path, "/completions"):
		return `{"id":"cmpl-1","object":"text_completion","created":1700000000,` + model +
			`"choices":[{"index":0,"text":"hi","finish_reason":"stop","logprobs":null}],` +
			`"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`
	default:
		return servedResponse(model)
	}
}

func servedResponse(model string) string {
	return `{"id":"resp_1","object":"response","created_at":1700000000,` + model +
		`"status":"completed","output":[],"usage":{"input_tokens":3,"input_tokens_details":{"cached_tokens":0},` +
		`"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":4}}`
}

func servedEvents(path, model string) []string {
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		return []string{
			`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,` + model + `"choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`,
			`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,` + model + `"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`,
		}
	case strings.HasSuffix(path, "/completions"):
		return []string{
			`{"id":"cmpl-1","object":"text_completion","created":1700000000,` + model + `"choices":[{"index":0,"text":"hi","finish_reason":"stop"}]}`,
			`{"id":"cmpl-1","object":"text_completion","created":1700000000,` + model + `"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`,
		}
	default:
		return []string{`{"type":"response.completed","sequence_number":1,"response":` + servedResponse(model) + `}`}
	}
}

type surfaceCall func(r *ReveniumOpenAI, model string) error

func openAISurfaces() map[string]surfaceCall {
	return map[string]surfaceCall{
		"chat": func(r *ReveniumOpenAI, model string) error {
			_, err := r.Chat().Completions().New(context.Background(), chatParams(model))
			return err
		},
		"streamed chat": func(r *ReveniumOpenAI, model string) error {
			stream, err := r.Chat().Completions().NewStreaming(context.Background(), chatParams(model))
			if err != nil {
				return err
			}
			for stream.Next() {
			}
			return stream.Close()
		},
		"text completion": func(r *ReveniumOpenAI, model string) error {
			_, err := r.Completions().New(context.Background(), textParams(model))
			return err
		},
		"streamed text completion": func(r *ReveniumOpenAI, model string) error {
			stream, err := r.Completions().NewStreaming(context.Background(), textParams(model))
			if err != nil {
				return err
			}
			for stream.Next() {
			}
			return stream.Close()
		},
		"responses": func(r *ReveniumOpenAI, model string) error {
			_, err := r.Responses().Create(context.Background(), responses.ResponseNewParams{Model: model})
			return err
		},
		"streamed responses": func(r *ReveniumOpenAI, model string) error {
			stream, err := r.Responses().CreateStreaming(context.Background(), responses.ResponseNewParams{Model: model})
			if err != nil {
				return err
			}
			for stream.Next() {
			}
			return stream.Close()
		},
	}
}

func textParams(model string) openaisdk.CompletionNewParams {
	return openaisdk.CompletionNewParams{
		Model:  openaisdk.CompletionNewParamsModel(model),
		Prompt: openaisdk.CompletionNewParamsPromptUnion{OfString: openaisdk.String("say hi")},
	}
}

type clientFactory func(providerServer *httptest.Server, meteringURL string) *ReveniumOpenAI

func recordedModel(t *testing.T, providerServer *httptest.Server, newClient clientFactory, requested string, call surfaceCall) interface{} {
	t.Helper()
	defer providerServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newClient(providerServer, mock.URL())
	t.Cleanup(func() { _ = r.Close() })

	require.NoError(t, call(r, requested))
	r.Flush()
	record := assertSingleRecordAfterTimeout(t, mock)
	require.Nil(t, record["errorReason"], "the call must succeed for its record to name a served model")
	return record["model"]
}

func openAIClient(t *testing.T) clientFactory {
	return func(providerServer *httptest.Server, meteringURL string) *ReveniumOpenAI {
		return newTestOpenAI(t, providerServer.URL, meteringURL)
	}
}

func TestOpenAISurfaces_RecordTheModelTheProviderServed(t *testing.T) {
	for name, call := range openAISurfaces() {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, servedNano, recordedModel(t, httptest.NewServer(servingModelHandler(t, servedNano)), openAIClient(t), requestedNano, call))
		})
	}
}

func TestOpenAISurfaces_RecordTheRequestedModelWhenTheResponseNamesNone(t *testing.T) {
	for name, call := range openAISurfaces() {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, requestedNano, recordedModel(t, httptest.NewServer(servingModelHandler(t, "")), openAIClient(t), requestedNano, call))
		})
	}
}

func TestAzureChatSurfaces_RecordTheDeploymentNameNotTheServedModel(t *testing.T) {
	azureClient := func(providerServer *httptest.Server, meteringURL string) *ReveniumOpenAI {
		return newAzureTestOpenAI(t, providerServer.URL, providerServer.Client(), meteringURL)
	}
	surfaces := openAISurfaces()
	for _, name := range []string{"chat", "streamed chat", "text completion", "streamed text completion"} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, "my-deployment", recordedModel(t, httptest.NewTLSServer(servingModelHandler(t, servedNano)), azureClient, "my-deployment", surfaces[name]))
		})
	}
}

func TestChatCompletions_DatedModelRuleBlocksTheShortNameTheProviderServesUnderIt(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	providerServer := httptest.NewServer(servingModelHandler(t, servedNano))
	t.Cleanup(providerServer.Close)
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	datedModelRule := `"filters": [{"dimension": "MODEL", "operator": "IS", "value": "` + servedNano + `"}]`
	rules.SetPayload(enforcementTestTeam, breachedRule(datedModelRule))

	completions := newEnforcedOpenAI(t, providerServer.URL, rules.URL(), mock.URL(), enforcementTestTeam).Chat().Completions()

	_, err := completions.New(context.Background(), chatParams(requestedNano))
	var exceeded *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &exceeded), "a breached rule naming the dated snapshot must block the short name, got %v", err)
}

func TestChatCompletions_RecordCarriesTheDatedNameAModelRuleNames(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	providerServer := httptest.NewServer(servingModelHandler(t, servedNano))
	t.Cleanup(providerServer.Close)
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	unbreachedRule := strings.Replace(breachedRule(`"filters": [{"dimension": "MODEL", "operator": "IS", "value": "`+servedNano+`"}]`),
		`"breached": true`, `"breached": false`, 1)
	rules.SetPayload(enforcementTestTeam, unbreachedRule)

	r := newEnforcedOpenAI(t, providerServer.URL, rules.URL(), mock.URL(), enforcementTestTeam)
	_, err := r.Chat().Completions().New(context.Background(), chatParams(requestedNano))
	require.NoError(t, err)
	r.Flush()

	assert.Equal(t, servedNano, assertSingleRecordAfterTimeout(t, mock)["model"])
}

func TestAzureChat_AnISNOTCapNamingTheDeploymentExemptsIt(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	providerServer := httptest.NewServer(servingModelHandler(t, servedNano))
	t.Cleanup(providerServer.Close)
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	rules.SetPayload(enforcementTestTeam, breachedRule(`"filters": [{"dimension": "MODEL", "operator": "IS_NOT", "value": "my-deployment"}]`))

	r := newEnforcedOpenAI(t, providerServer.URL, rules.URL(), mock.URL(), enforcementTestTeam)
	r.provider = ProviderAzure
	_, err := r.Chat().Completions().New(context.Background(), chatParams("my-deployment"))
	assert.NoError(t, err, "an Azure chat call is recorded under its deployment name, so a cap excluding that name exempts it")
}
