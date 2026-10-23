package enforcement

import "github.com/revenium/revenium-go-sdk/core"

// callDefaults are the organization, product and credential alias a client
// puts on records that name none, so the cost-control check judges a call
// by the same attribution its record carries.
type callDefaults struct {
	organization    string
	product         string
	credentialAlias string
}

func callDefaultsFor(rc *core.ReveniumConfig) callDefaults {
	defaults := core.RecordDefaults(rc)
	return callDefaults{
		organization:    stringOf(defaults["organizationName"]),
		product:         stringOf(defaults["productName"]),
		credentialAlias: stringOf(defaults["credentialAlias"]),
	}
}

func (d callDefaults) complete(ec EvalContext) EvalContext {
	if core.IsBlank(ec.Organization) {
		ec.Organization = d.organization
	}
	if core.IsBlank(ec.ProductName) {
		ec.ProductName = d.product
	}
	return ec.withCredentialAlias(d.credentialAlias)
}

func stringOf(value interface{}) string {
	s, _ := value.(string)
	return s
}
