package runway

import "encoding/json"

func capturedPrompt(result *VideoGenerationResult) string {
	prompt, _ := result.Metadata["_capturedPrompt"].(string)
	return prompt
}

func capturedOutput(result *VideoGenerationResult) string {
	if len(result.OutputURLs) == 0 {
		return ""
	}
	outputJSON, err := json.Marshal(result.OutputURLs)
	if err != nil {
		return ""
	}
	return string(outputJSON)
}
