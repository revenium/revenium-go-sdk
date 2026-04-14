package google

import (
	"math"

	"google.golang.org/genai"
)

func ExtractConfidenceScore(resp *genai.GenerateContentResponse) *float64 {
	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0] == nil {
		return nil
	}

	candidate := resp.Candidates[0]

	if candidate.AvgLogprobs != 0 {
		score := clampScore(math.Exp(candidate.AvgLogprobs))
		return &score
	}

	if candidate.GroundingMetadata != nil {
		if scores := extractGroundingSupportScores(candidate.GroundingMetadata.GroundingSupports); scores != nil {
			return scores
		}

		if candidate.GroundingMetadata.RetrievalMetadata != nil {
			score := clampScore(float64(candidate.GroundingMetadata.RetrievalMetadata.GoogleSearchDynamicRetrievalScore))
			return &score
		}
	}

	return nil
}

func extractGroundingSupportScores(supports []*genai.GroundingSupport) *float64 {
	if len(supports) == 0 {
		return nil
	}

	var total float64
	var count int
	for _, s := range supports {
		for _, cs := range s.ConfidenceScores {
			total += float64(cs)
			count++
		}
	}

	if count == 0 {
		return nil
	}

	score := clampScore(total / float64(count))
	return &score
}

func clampScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
