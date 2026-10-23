package openai

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const documentedAzureAPIVersion = "2024-02-15-preview"

type recordedRequest struct {
	path         string
	apiVersion   string
	apiKey       string
	auth         string
	organization string
}

type countingProvider struct {
	mu       sync.Mutex
	requests []recordedRequest
	status   int
}

func (p *countingProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.requests = append(p.requests, recordedRequest{
		path:         r.URL.Path,
		apiVersion:   r.URL.Query().Get("api-version"),
		apiKey:       r.Header.Get("Api-Key"),
		auth:         r.Header.Get("Authorization"),
		organization: r.Header.Get("OpenAI-Organization"),
	})
	p.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if p.status != 0 {
		w.WriteHeader(p.status)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request","type":"invalid_request_error"}}`))
		return
	}
	_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"gpt-4o",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
}

func (p *countingProvider) received() []recordedRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]recordedRequest(nil), p.requests...)
}

func publicOpenAIStub(t *testing.T) *countingProvider {
	t.Helper()
	public := &countingProvider{}
	server := httptest.NewServer(public)
	t.Cleanup(server.Close)
	t.Setenv("OPENAI_BASE_URL", server.URL)
	t.Setenv("OPENAI_API_KEY", "sk-public-openai")
	return public
}

func azureStub(t *testing.T) (*countingProvider, *httptest.Server) {
	t.Helper()
	azure := &countingProvider{}
	server := httptest.NewTLSServer(azure)
	t.Cleanup(server.Close)
	return azure, server
}

func chatThrough(t *testing.T, cfg *Config, provider Provider, httpClient *http.Client) error {
	t.Helper()
	client := openaisdk.NewClient(append(buildClientOptions(cfg, provider), option.WithHTTPClient(httpClient), option.WithMaxRetries(0))...)
	_, err := client.Chat.Completions.New(context.Background(), chatParams("my-deployment"))
	return err
}

func TestAzureWithoutAPIVersion_CallsTheAzureEndpointWithTheDocumentedDefault(t *testing.T) {
	public := publicOpenAIStub(t)
	azure, azureServer := azureStub(t)
	cfg := &Config{AzureAPIKey: "azure-key", AzureEndpoint: azureServer.URL, OpenAIAPIKey: "sk-public-openai"}
	require.Equal(t, ProviderAzure, DetectProvider(cfg))

	require.NoError(t, chatThrough(t, cfg, DetectProvider(cfg), azureServer.Client()))

	assert.Empty(t, public.received())
	require.Len(t, azure.received(), 1)
	call := azure.received()[0]
	assert.Equal(t, "/openai/deployments/my-deployment/chat/completions", call.path)
	assert.Equal(t, documentedAzureAPIVersion, call.apiVersion)
	assert.Equal(t, "azure-key", call.apiKey)
	assert.Empty(t, call.auth)
}

func TestAzureBaseURL_SendsTheCallToThatURL(t *testing.T) {
	public := publicOpenAIStub(t)
	azure, azureServer := azureStub(t)
	cfg := &Config{AzureAPIKey: "azure-key", BaseURL: azureServer.URL, AzureAPIVersion: "2024-10-21"}

	require.NoError(t, chatThrough(t, cfg, ProviderAzure, azureServer.Client()))

	assert.Empty(t, public.received())
	require.Len(t, azure.received(), 1)
	assert.Equal(t, "2024-10-21", azure.received()[0].apiVersion)
	assert.Equal(t, "azure-key", azure.received()[0].apiKey)
}

func TestAzureV1Endpoint_CallsOpenAIPathsUnderTheV1URLWithTheAzureKey(t *testing.T) {
	cases := []struct {
		name        string
		config      func(serverURL string) *Config
		wantVersion string
	}{
		{name: "v1 base URL", config: func(serverURL string) *Config {
			return &Config{AzureAPIKey: "azure-key", BaseURL: serverURL + "/openai/v1"}
		}},
		{name: "v1 endpoint with a trailing slash", config: func(serverURL string) *Config {
			return &Config{AzureAPIKey: "azure-key", AzureEndpoint: serverURL + "/openai/v1/"}
		}},
		{name: "v1 endpoint with an explicit preview version", config: func(serverURL string) *Config {
			return &Config{AzureAPIKey: "azure-key", AzureEndpoint: serverURL + "/openai/v1", AzureAPIVersion: "preview"}
		}, wantVersion: "preview"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			public := publicOpenAIStub(t)
			t.Setenv("OPENAI_ORG_ID", "org-public")
			azure, azureServer := azureStub(t)

			require.NoError(t, chatThrough(t, tc.config(azureServer.URL), ProviderAzure, azureServer.Client()))

			assert.Empty(t, public.received())
			require.Len(t, azure.received(), 1)
			call := azure.received()[0]
			assert.Equal(t, "/openai/v1/chat/completions", call.path)
			assert.Equal(t, tc.wantVersion, call.apiVersion)
			assert.Equal(t, "Bearer azure-key", call.auth)
			assert.Empty(t, call.organization)
		})
	}
}

func TestAzureV1BaseURL_IsDetectedAsAzure(t *testing.T) {
	assert.Equal(t, ProviderAzure, DetectProvider(&Config{BaseURL: "https://res.openai.azure.com/openai/v1", AzureAPIKey: "k"}))
}

func TestAzureEndpointEndingInOpenAI_CallsTheDeploymentPathOnce(t *testing.T) {
	for _, suffix := range []string{"/openai/", "/OpenAI"} {
		t.Run(suffix, func(t *testing.T) {
			azure, azureServer := azureStub(t)
			cfg := &Config{AzureAPIKey: "azure-key", AzureEndpoint: azureServer.URL + suffix}

			require.NoError(t, chatThrough(t, cfg, ProviderAzure, azureServer.Client()))

			require.Len(t, azure.received(), 1)
			assert.Equal(t, "/openai/deployments/my-deployment/chat/completions", azure.received()[0].path)
			assert.Equal(t, documentedAzureAPIVersion, azure.received()[0].apiVersion)
			assert.Equal(t, "azure-key", azure.received()[0].apiKey)
		})
	}
}

func TestAzureResourceEndpoint_DropsTheTrailingOpenAIPathInAnyCase(t *testing.T) {
	cases := map[string]string{
		"https://res.openai.azure.com/openai":         "https://res.openai.azure.com",
		"https://res.openai.azure.com/openai/":        "https://res.openai.azure.com",
		"https://res.openai.azure.com/OpenAI":         "https://res.openai.azure.com",
		"https://res.openai.azure.com/OPENAI/":        "https://res.openai.azure.com",
		"https://res.openai.azure.com/openai?x=1":     "https://res.openai.azure.com?x=1",
		"https://gw.example.com/team/OpenAI/?x=1":     "https://gw.example.com/team?x=1",
		"https://res.openai.azure.com":                "https://res.openai.azure.com",
		"https://res.openai.azure.com/":               "https://res.openai.azure.com/",
		"https://res.openai.azure.com/notopenai/path": "https://res.openai.azure.com/notopenai/path",
	}
	for endpoint, want := range cases {
		assert.Equal(t, want, azureResourceEndpoint(endpoint), endpoint)
	}
}

func TestAzureBaseURLWithoutAzureKey_FailsAtStartNamingTheMissingKey(t *testing.T) {
	_, err := NewReveniumOpenAI(&Config{
		OpenAIAPIKey: "sk-public-openai",
		BaseURL:      "https://my-resource.openai.azure.com",
		Revenium:     &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: "http://127.0.0.1:1"},
	})

	require.Error(t, err)
	for _, remedy := range []string{"Config.AzureAPIKey", "Config.AzureDisabled", "AZURE_OPENAI_API_KEY", "WithAzureAPIKey", "REVENIUM_AZURE_DISABLE=true", "WithAzureDisabled(true)"} {
		assert.Contains(t, err.Error(), remedy)
	}
}

func TestAzureBaseURLWithoutAzureKey_TheNamedConfigFieldsClearTheError(t *testing.T) {
	base := Config{
		OpenAIAPIKey: "sk-public-openai",
		BaseURL:      "https://my-resource.openai.azure.com",
		Revenium:     &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: "http://127.0.0.1:1"},
	}
	withKey, withDetectionOff := base, base
	withKey.AzureAPIKey = "azure-key"
	withDetectionOff.AzureDisabled = true

	for name, cfg := range map[string]*Config{"AzureAPIKey": &withKey, "AzureDisabled": &withDetectionOff} {
		t.Run(name, func(t *testing.T) {
			r, err := NewReveniumOpenAI(cfg)
			require.NoError(t, err)
			_ = r.Close()
		})
	}
}

func TestAzureBaseURLWithoutAzureKey_TheNamedEnvValueClearsTheErrorForInitialize(t *testing.T) {
	t.Setenv("REVENIUM_AZURE_DISABLE", "true")
	cfg := &Config{BaseURL: "https://my-resource.openai.azure.com"}
	require.NoError(t, cfg.loadFromEnv())

	assert.Equal(t, ProviderOpenAI, DetectProvider(cfg))
	assert.NoError(t, cfg.validateAzure(DetectProvider(cfg)))
}

func TestAzureFailedCall_IsSentOnceAndRecordedOnceAsAzure(t *testing.T) {
	azure := &countingProvider{status: http.StatusBadRequest}
	azureServer := httptest.NewTLSServer(azure)
	defer azureServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newAzureTestOpenAI(t, azureServer.URL, azureServer.Client(), mock.URL())

	_, err := r.Chat().Completions().New(context.Background(), chatParams("my-deployment"))

	require.Error(t, err)
	r.Flush()
	record := assertSingleRecordAfterTimeout(t, mock)
	assert.Len(t, azure.received(), 1)
	assert.Equal(t, "AZURE", record["provider"])
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, "my-deployment", record["model"])
}

func envExampleValues(t *testing.T) map[string]string {
	t.Helper()
	file, err := os.Open("../.env.example")
	require.NoError(t, err)
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if found {
			values[key] = value
		}
	}
	require.NoError(t, scanner.Err())
	return values
}

func TestEnvExample_WithOnlyTheOpenAIKeyFilledInUsesOpenAI(t *testing.T) {
	for _, name := range []string{"AZURE_OPENAI_API_KEY", "AZURE_OPENAI_ENDPOINT", "AZURE_OPENAI_API_VERSION", "REVENIUM_AZURE_DISABLE"} {
		t.Setenv(name, "")
	}
	example := envExampleValues(t)
	for name, value := range example {
		t.Setenv(name, value)
	}
	t.Setenv("OPENAI_API_KEY", "sk-real-key")

	cfg := &Config{}
	require.NoError(t, cfg.loadFromEnv())

	assert.Equal(t, ProviderOpenAI, DetectProvider(cfg))
	assert.NotContains(t, example, "GOOGLE_CLOUD_PROJECT")
}

func TestProxyWhoseHostMentionsAzure_StartsAsAnOpenAIClient(t *testing.T) {
	r, err := NewReveniumOpenAI(&Config{
		OpenAIAPIKey: "sk-proxy",
		BaseURL:      "https://azureopenai.proxy.example.com/v1",
		Revenium:     &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: "http://127.0.0.1:1"},
	})
	require.NoError(t, err)
	defer r.Close()

	assert.Equal(t, ProviderOpenAI, r.GetProvider())
}
