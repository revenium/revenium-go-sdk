package jobs

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"strconv"
	"strings"
)

const defaultOutcomeCurrency = "USD"

var (
	storedOutcomeValueScale = new(big.Rat).SetInt64(10_000_000_000)
	half                    = big.NewRat(1, 2)
)

func (c *JobClient) jobStoringReport(jobID string, sent *JobOutcome) (*JobResource, bool) {
	if len(sent.Metrics) > 0 {
		return nil, false
	}
	job, err := c.GetJob(jobID)
	if err != nil || !storesReportUnamended(job, sent) {
		return nil, false
	}
	if sent.ReportedBy != "" && !c.historyShowsOnlyReportBy(jobID, sent.ReportedBy) {
		return nil, false
	}
	return job, true
}

func (c *JobClient) historyShowsOnlyReportBy(jobID, reporter string) bool {
	history, err := c.GetJobOutcomeHistory(jobID)
	return err == nil && len(history) == 1 && history[0].ReportedBy != nil && *history[0].ReportedBy == reporter
}

func storesReportUnamended(job *JobResource, sent *JobOutcome) bool {
	if !job.HasOutcome || job.OutcomeAmendmentCount == nil || *job.OutcomeAmendmentCount != 0 {
		return false
	}
	return sameEnum(string(sent.ExecutionStatus), job.ExecutionStatus) &&
		sameEnum(string(sent.OutcomeType), job.OutcomeType) &&
		sameEnum(storedCurrencyFor(sent), job.OutcomeCurrency) &&
		sameOutcomeReason(sent.OutcomeReason, job.OutcomeReason) &&
		sameOutcomeValue(sent.OutcomeValue, storedOutcomeValue(job)) &&
		sameMetadata(sent.Metadata, job.OutcomeMetadata)
}

func storedCurrencyFor(sent *JobOutcome) string {
	if sent.OutcomeValue == nil {
		return ""
	}
	if sent.OutcomeCurrency == "" {
		return defaultOutcomeCurrency
	}
	return sent.OutcomeCurrency
}

func sameEnum(sent string, stored *string) bool {
	if sent == "" {
		return stored == nil || *stored == ""
	}
	return stored != nil && strings.EqualFold(*stored, sent)
}

func sameOutcomeReason(sent string, stored *string) bool {
	if strings.TrimSpace(sent) == "" {
		return stored == nil
	}
	return stored != nil && *stored == sent
}

func storedOutcomeValue(job *JobResource) *big.Rat {
	text := string(job.exactOutcomeValue)
	if text == "" && job.OutcomeValue != nil {
		text = strconv.FormatFloat(*job.OutcomeValue, 'f', -1, 64)
	}
	if text == "" {
		return nil
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil
	}
	return value
}

func sameOutcomeValue(sent *float64, stored *big.Rat) bool {
	if sent == nil || stored == nil {
		return sent == nil && stored == nil
	}
	return storedFormOf(*sent).Cmp(stored) == 0
}

// Revenium stores the decimal text it receives in a numeric(38,10) column,
// which rounds to 10 places half away from zero.
func storedFormOf(value float64) *big.Rat {
	exact, _ := new(big.Rat).SetString(strconv.FormatFloat(value, 'f', -1, 64))
	scaled := new(big.Rat).Mul(exact, storedOutcomeValueScale)
	negative := scaled.Sign() < 0
	scaled.Abs(scaled).Add(scaled, half)
	rounded := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	if negative {
		rounded.Neg(rounded)
	}
	return new(big.Rat).SetFrac(rounded, storedOutcomeValueScale.Num())
}

func sameMetadata(sent string, stored *string) bool {
	if sent == "" || stored == nil {
		return sent == "" && stored == nil
	}
	sentJSON, sentOK := decodeExactJSON(sent)
	storedJSON, storedOK := decodeExactJSON(*stored)
	if !sentOK || !storedOK {
		return sent == *stored
	}
	return sameJSON(sentJSON, storedJSON)
}

func decodeExactJSON(text string) (any, bool) {
	decoder := json.NewDecoder(bytes.NewReader([]byte(text)))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil, false
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return value, true
}

func sameJSON(sent, stored any) bool {
	switch s := sent.(type) {
	case map[string]any:
		t, ok := stored.(map[string]any)
		if !ok || len(s) != len(t) {
			return false
		}
		for key, value := range s {
			other, present := t[key]
			if !present || !sameJSON(value, other) {
				return false
			}
		}
		return true
	case []any:
		t, ok := stored.([]any)
		if !ok || len(s) != len(t) {
			return false
		}
		for i := range s {
			if !sameJSON(s[i], t[i]) {
				return false
			}
		}
		return true
	case json.Number:
		t, ok := stored.(json.Number)
		return ok && sameNumber(s, t)
	default:
		return sent == stored
	}
}

func sameNumber(sent, stored json.Number) bool {
	a, okA := new(big.Rat).SetString(string(sent))
	b, okB := new(big.Rat).SetString(string(stored))
	return okA && okB && a.Cmp(b) == 0
}
