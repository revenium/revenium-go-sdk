package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

const amendConflictRecovery = "Re-check the outcome you mean to write (GetJobOutcomeHistory shows what changed), " +
	"then resend the amendment with ExpectedEntityVersion set to CurrentEntityVersion, or to GetJob's EntityVersion " +
	"when CurrentEntityVersion is nil. The SDK does not resend an amendment on its own"

// Revenium's 409 names the current version only in its message prose ("current version is N"), so this tracks that wording; a structured currentEntityVersion wins when present.
var currentVersionInMessage = regexp.MustCompile(`current version is\s+(\d+)`)

type conflictBody map[string]any

func readConflictBody(err error) conflictBody {
	var revErr *core.ReveniumError
	if !errors.As(err, &revErr) {
		return nil
	}
	text, _ := revErr.GetDetails()["responseBody"].(string)
	var body conflictBody
	if json.Unmarshal([]byte(text), &body) != nil {
		return nil
	}
	return body
}

func (b conflictBody) field(names ...string) any {
	details, _ := b["details"].(map[string]any)
	for _, source := range []map[string]any{details, b} {
		for _, name := range names {
			if value, ok := source[name]; ok && value != nil {
				return value
			}
		}
	}
	return nil
}

func (b conflictBody) message(fallback string) string {
	for _, key := range []string{"message", "error"} {
		if text, ok := b[key].(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return fallback
}

func (b conflictBody) currentEntityVersion() *int64 {
	if version, ok := wholeNumber(b.field("currentEntityVersion", "entityVersion")); ok {
		return &version
	}
	for _, key := range []string{"message", "error"} {
		text, _ := b[key].(string)
		if match := currentVersionInMessage.FindStringSubmatch(text); match != nil {
			if version, err := strconv.ParseInt(match[1], 10, 64); err == nil {
				return &version
			}
		}
	}
	return nil
}

func wholeNumber(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		if v < 0 || v != float64(int64(v)) {
			return 0, false
		}
		return int64(v), true
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n, err == nil && n >= 0
	default:
		return 0, false
	}
}

func alreadyReportedError(jobID string, err error) *OutcomeAlreadyReportedError {
	body := readConflictBody(err)
	reported := &OutcomeAlreadyReportedError{
		JobID:   jobID,
		Message: body.message(fmt.Sprintf("outcome already reported for job %s", jobID)),
	}
	if reportedAt, ok := body.field("reportedAt").(string); ok && reportedAt != "" {
		reported.ReportedAt = &reportedAt
	}
	if count, ok := wholeNumber(body.field("updateCount", "amendmentCount")); ok {
		reported.AmendmentCount = int(count)
	}
	return reported
}

func amendConflictError(jobID string, err error) *OutcomeAmendConflictError {
	body := readConflictBody(err)
	return &OutcomeAmendConflictError{
		JobID:                jobID,
		Message:              strings.TrimRight(body.message(fmt.Sprintf("concurrent amendment conflict for job %s", jobID)), ". ") + ". " + amendConflictRecovery,
		CurrentEntityVersion: body.currentEntityVersion(),
	}
}
