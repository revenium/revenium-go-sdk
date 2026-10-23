package openai

import (
	"context"

	"github.com/revenium/revenium-go-sdk/core/enforcement"
)

// checkCostControls judges a call against the cost-control rules before it
// reaches the provider. recordsServedName says the call's record will carry the
// model the provider served, which may be a dated snapshot of the requested
// name (gpt-4.1-nano served as gpt-4.1-nano-2025-04-14), so MODEL filters
// follow the dated-name rules; a call recorded under exactly the requested name
// compares that name exactly, so an IS_NOT filter naming it exempts the call.
func (r *ReveniumOpenAI) checkCostControls(ctx context.Context, model string, provider Provider, recordsServedName bool) error {
	evalCtx := enforcement.NewEvalContext(ctx, model, provider.String())
	if recordsServedName {
		evalCtx = evalCtx.ServedModelMayBeDated()
	}
	return r.enforcer.Check(evalCtx)
}

// recordsServedChatModel says whether a chat or text completion record carries
// the model the provider served. Azure records the deployment name the caller
// sent, which names the caller's deployment rather than a model snapshot.
func (p Provider) recordsServedChatModel() bool {
	return !p.IsAzure()
}

// chatRecordModel is the model a chat or text completion record carries: the
// served name, as the Python SDK records it, except on Azure (see
// recordsServedChatModel).
func (p Provider) chatRecordModel(requested, served string) string {
	if !p.recordsServedChatModel() {
		return requested
	}
	return servedOrRequested(served, requested)
}

// servedOrRequested falls back to the requested name when a response does not
// say which model served it.
func servedOrRequested(served, requested string) string {
	if served == "" {
		return requested
	}
	return served
}
