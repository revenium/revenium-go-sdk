// Runnable examples for the Revenium Go SDK. This is a separate module so the
// examples can depend on the published provider submodules without those
// dependencies becoming part of the repository root, which publishes no code.
// It is intentionally never tagged and never published.
module github.com/revenium/revenium-go-sdk/examples

go 1.25.0

require (
	github.com/anthropics/anthropic-sdk-go v1.76.0
	github.com/openai/openai-go/v3 v3.66.0
	github.com/revenium/revenium-go-sdk/anthropic v1.1.4
	github.com/revenium/revenium-go-sdk/core v1.1.7
	github.com/revenium/revenium-go-sdk/fal v1.1.4
	github.com/revenium/revenium-go-sdk/google v1.1.4
	github.com/revenium/revenium-go-sdk/litellm v1.1.4
	github.com/revenium/revenium-go-sdk/openai v1.1.4
	github.com/revenium/revenium-go-sdk/perplexity v1.1.4
	github.com/revenium/revenium-go-sdk/runway v1.1.4
	google.golang.org/genai v1.71.0
)

require (
	cloud.google.com/go v0.116.0 // indirect
	cloud.google.com/go/auth v0.9.3 // indirect
	cloud.google.com/go/compute/metadata v0.5.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.23.1 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.12.0 // indirect
	github.com/aws/aws-sdk-go-v2 v1.47.0 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.2 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.33.4 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.20.4 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.0 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/bedrockruntime v1.41.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.10.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.38.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.43.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.50.0 // indirect
	github.com/aws/smithy-go v1.28.1 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/invopop/jsonschema v0.14.0 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/standard-webhooks/standard-webhooks/libraries v0.0.1 // indirect
	github.com/tidwall/gjson v1.19.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.2 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240903143218-8af14fe29dc1 // indirect
	google.golang.org/grpc v1.66.2 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)

// Never published, so these are safe and intentional: the examples must exercise
// the working tree, not the released artifacts.
replace (
	github.com/revenium/revenium-go-sdk/anthropic => ../anthropic
	github.com/revenium/revenium-go-sdk/core => ../core
	github.com/revenium/revenium-go-sdk/fal => ../fal
	github.com/revenium/revenium-go-sdk/google => ../google
	github.com/revenium/revenium-go-sdk/litellm => ../litellm
	github.com/revenium/revenium-go-sdk/openai => ../openai
	github.com/revenium/revenium-go-sdk/perplexity => ../perplexity
	github.com/revenium/revenium-go-sdk/runway => ../runway
)
