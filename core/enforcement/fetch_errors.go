package enforcement

import (
	"fmt"
	"net/http"

	"github.com/revenium/revenium-go-sdk/core"
)

var (
	addressHint = fmt.Sprintf("no rules endpoint at this address: set %s, or %s when it is unset, to the Revenium API address with no path (the SDK adds %s{teamId})",
		core.EnvEnforcementBaseURL, core.EnvBaseURL, core.EnforcementRulesPathPrefix)
	keyScopeHint = fmt.Sprintf("the key may not read this team's rules: check that %s is the team the key belongs to, or set %s to a read key (rev_rk_) for this team",
		core.EnvTeamID, core.EnvEnforcementAPIKey)
)

// refusedFetchError names the setting to change for the refusals a
// misconfiguration produces, so the repeating warning is actionable.
func refusedFetchError(status int, body []byte) error {
	err := fmt.Errorf("unexpected status %d: %s", status, string(body))
	switch status {
	case http.StatusNotFound:
		return fmt.Errorf("%w; %s", err, addressHint)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w; %s", err, keyScopeHint)
	default:
		return err
	}
}
