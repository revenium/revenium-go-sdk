package metering

import (
	"regexp"

	"github.com/revenium/revenium-go-sdk/core"
)

// The platform validates agenticJobId on both completions and tool events with
// @field:Pattern(JobValidation.AGENTIC_JOB_ID_PATTERN). An id that fails the
// pattern makes the API answer 400 and the entire event — with its cost — is
// lost, not just the attribution. So an id we know the platform would reject is
// dropped here and the event is sent without it, matching the Python SDK
// (_resolve_agentic_job_id) and the Node CLI.
//
// Only agenticJobId is checked. agenticJobName, agenticJobType and
// agenticJobVersion have no server-side pattern, so they stay plain DTO fields:
// validating them here would reject values the platform accepts.
//
// The server pattern is
//
//	^(?!types$|conversion-funnel$)[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$
//
// Go's RE2 has no lookahead, so the negative lookahead is expressed as an
// explicit reserved-value check and the rest as the regexp below. Keep the two
// in sync with JobValidation.AGENTIC_JOB_ID_PATTERN.
const agenticJobIDPattern = `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$`

var agenticJobIDRegexp = regexp.MustCompile(agenticJobIDPattern)

// reservedAgenticJobIDs mirrors the lookahead of the server pattern. The match
// is exact and case-sensitive on the server, so "Types" is a legal id.
var reservedAgenticJobIDs = map[string]struct{}{
	"types":             {},
	"conversion-funnel": {},
}

// isValidAgenticJobID reports whether the platform would accept id.
func isValidAgenticJobID(id string) bool {
	if _, reserved := reservedAgenticJobIDs[id]; reserved {
		return false
	}
	return agenticJobIDRegexp.MatchString(id)
}

// sanitizeAgenticJobID returns id unchanged when the platform would accept it,
// and "" otherwise, logging the drop at debug level. source names the set point
// so a caller can find where the bad id came from.
func sanitizeAgenticJobID(id, source string) string {
	if id == "" || isValidAgenticJobID(id) {
		return id
	}
	core.Debug(
		"[METERING] dropping agenticJobId %q from %s: it does not match the platform pattern %s "+
			"(or is a reserved value); the event is sent without job attribution instead of being rejected with a 400",
		id, source, agenticJobIDPattern,
	)
	return ""
}
