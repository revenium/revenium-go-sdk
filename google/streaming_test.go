package google

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/genai"
)

func TestExtractFinishReason_NilResponse(t *testing.T) {
	assert.Equal(t, genai.FinishReason(""), ExtractFinishReason(nil))
}

func TestExtractFinishReason_NoCandidates(t *testing.T) {
	resp := &genai.GenerateContentResponse{}
	assert.Equal(t, genai.FinishReason(""), ExtractFinishReason(resp))
}

func TestExtractFinishReason_FromCandidate(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{FinishReason: genai.FinishReasonStop},
		},
	}
	assert.Equal(t, genai.FinishReasonStop, ExtractFinishReason(resp))
}

func TestExtractConfidenceScore_NilResponse(t *testing.T) {
	assert.Nil(t, ExtractConfidenceScore(nil))
}

func TestExtractConfidenceScore_NoCandidates(t *testing.T) {
	resp := &genai.GenerateContentResponse{}
	assert.Nil(t, ExtractConfidenceScore(resp))
}

func TestExtractConfidenceScore_FromAvgLogprobs(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{AvgLogprobs: -0.5}},
	}
	score := ExtractConfidenceScore(resp)
	if assert.NotNil(t, score) {
		assert.InDelta(t, math.Exp(-0.5), *score, 0.0001)
	}
}

func TestExtractConfidenceScore_FromGroundingSupports(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			GroundingMetadata: &genai.GroundingMetadata{
				GroundingSupports: []*genai.GroundingSupport{
					{ConfidenceScores: []float32{0.8, 0.6}},
					{ConfidenceScores: []float32{0.7}},
				},
			},
		}},
	}
	score := ExtractConfidenceScore(resp)
	if assert.NotNil(t, score) {
		assert.InDelta(t, 0.7, *score, 0.0001)
	}
}

func TestClampScore(t *testing.T) {
	assert.Equal(t, 0.0, clampScore(-0.5))
	assert.Equal(t, 1.0, clampScore(1.5))
	assert.Equal(t, 0.5, clampScore(0.5))
	assert.Equal(t, 0.0, clampScore(0))
	assert.Equal(t, 1.0, clampScore(1))
}

func TestExtractGroundingSupportScores_Empty(t *testing.T) {
	assert.Nil(t, extractGroundingSupportScores(nil))
	assert.Nil(t, extractGroundingSupportScores([]*genai.GroundingSupport{}))
	assert.Nil(t, extractGroundingSupportScores([]*genai.GroundingSupport{{ConfidenceScores: nil}}))
}

func TestExtractGroundingSupportScores_Average(t *testing.T) {
	supports := []*genai.GroundingSupport{
		{ConfidenceScores: []float32{0.5, 0.5}},
		{ConfidenceScores: []float32{0.5, 0.5}},
	}
	score := extractGroundingSupportScores(supports)
	if assert.NotNil(t, score) {
		assert.InDelta(t, 0.5, *score, 0.0001)
	}
}
