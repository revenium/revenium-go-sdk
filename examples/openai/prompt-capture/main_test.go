package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/testutil"
)

const stubAnswer = `{"id":"chatcmpl-example","object":"chat.completion","created":1700000000,"model":"gpt-4o-mini",` +
	`"choices":[{"index":0,"message":{"role":"assistant","content":"Paris."},"finish_reason":"stop"}],` +
	`"usage":{"prompt_tokens":14,"completion_tokens":2,"total_tokens":16}}`

func TestExampleRecordsThePromptAndResponseItSaysItCaptured(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(stubAnswer))
	}))
	defer provider.Close()
	revenium := testutil.NewMockMeteringServer()
	defer revenium.Close()

	t.Setenv("REVENIUM_ENV_FILES_DISABLE", "true")
	t.Setenv("OPENAI_API_KEY", "sk-example-test")
	t.Setenv("OPENAI_BASE_URL", provider.URL)
	t.Setenv("REVENIUM_METERING_API_KEY", "hak_example_test")
	t.Setenv("REVENIUM_METERING_BASE_URL", revenium.URL())
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")

	main()

	if !revenium.WaitForPayloads(1, 2*time.Second) {
		t.Fatal("the example sent no usage record")
	}
	record := revenium.GetPayloads()[0]
	inputMessages, _ := record["inputMessages"].(string)
	var messages []map[string]interface{}
	if err := json.Unmarshal([]byte(inputMessages), &messages); err != nil || len(messages) != 1 {
		t.Fatalf("inputMessages = %q, want the one user message", inputMessages)
	}
	if content, _ := messages[0]["content"].(string); !strings.Contains(content, "What is the capital of France?") {
		t.Errorf("captured prompt = %q", content)
	}
	if record["outputResponse"] != "Paris." {
		t.Errorf("outputResponse = %v, want the response", record["outputResponse"])
	}
}
