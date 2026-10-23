package anthropic

// InvokeModel keeps Claude's own stop_reason (usually end_turn) when a guardrail
// blocks the call and reports the block only in this top-level field, unlike
// Converse, which reports it as the stop reason.
const invokeModelGuardrailActionKey = "amazon-bedrock-guardrailAction"

const guardrailIntervenedStopReason = "guardrail_intervened"

func guardrailIntervened(body map[string]interface{}) bool {
	action, _ := body[invokeModelGuardrailActionKey].(string)
	return action == "INTERVENED"
}
