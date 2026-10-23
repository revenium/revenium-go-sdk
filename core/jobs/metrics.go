package jobs

import (
	"fmt"
	"math"
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

func validateMetrics(metrics []OutcomeMetric) error {
	for i, metric := range metrics {
		if strings.TrimSpace(metric.Key) == "" {
			return core.NewValidationError(fmt.Sprintf("metrics[%d].Key must be non-blank", i), nil)
		}
		if math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) {
			return core.NewValidationError(fmt.Sprintf("metrics[%d].Value must be a finite number", i), nil)
		}
	}
	return nil
}
