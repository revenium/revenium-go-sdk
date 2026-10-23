package openai

import (
	"net/url"
	"strings"
)

const (
	azureV1PathSuffix     = "/openai/v1"
	azureLegacyPathSuffix = "/openai"
)

func IsAzureEndpoint(url string) bool {
	return isAzureURL(url)
}

func isAzureV1Endpoint(endpoint string) bool {
	return strings.HasSuffix(endpointPath(endpoint), azureV1PathSuffix)
}

func azureResourceEndpoint(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(strings.ToLower(path), azureLegacyPathSuffix) {
		return endpoint
	}
	parsed.Path = path[:len(path)-len(azureLegacyPathSuffix)]
	parsed.RawPath = ""
	return parsed.String()
}

func endpointPath(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return strings.TrimRight(strings.ToLower(parsed.Path), "/")
}

func parseDeploymentModels(raw string) map[string]string {
	models := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		deployment, model, ok := strings.Cut(pair, "=")
		deployment, model = strings.TrimSpace(deployment), strings.TrimSpace(model)
		if ok && deployment != "" && model != "" {
			models[deployment] = model
		}
	}
	return models
}
