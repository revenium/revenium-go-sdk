package openai

import "strings"

func IsAzureEndpoint(url string) bool {
	url = strings.ToLower(url)
	return strings.Contains(url, "azure.com") ||
		strings.Contains(url, "openai.azure.com") ||
		strings.Contains(url, ".azure.") ||
		strings.Contains(url, "azureopenai")
}
