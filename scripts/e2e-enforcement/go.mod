module github.com/revenium/revenium-go-sdk/scripts/e2e-enforcement

go 1.25.0

require (
	github.com/openai/openai-go/v3 v3.66.0
	github.com/revenium/revenium-go-sdk/core v1.1.7
	github.com/revenium/revenium-go-sdk/openai v0.0.0
)

require (
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.23.1 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.12.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/tidwall/gjson v1.19.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

replace (
	github.com/revenium/revenium-go-sdk/core => ../../core
	github.com/revenium/revenium-go-sdk/openai => ../../openai
)
