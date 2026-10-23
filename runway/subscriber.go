package runway

import (
	"context"

	"github.com/revenium/revenium-go-sdk/core"
)

func recordMetadata(ctx context.Context, metadata *UsageMetadata) map[string]interface{} {
	return core.CloneMetadata(core.UsageMetadataOver(ctx, usageMetadataToMap(metadata)))
}
