package sandbox

import (
	"net"
	"strings"
)

var reservedNames = []string{"test", "example", "invalid", "localhost", "example.com", "example.net", "example.org"}

func isLocal(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// isReserved reports whether host can never resolve to a real server
// (RFC 2606 and RFC 6761), which tests use on purpose for an address that
// does not answer.
func isReserved(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, name := range reservedNames {
		if host == name || strings.HasSuffix(host, "."+name) {
			return true
		}
	}
	return false
}

// isHarmlessLookup also accepts a local or reserved name followed by one of
// the search domains, because the resolver tries "revenium.invalid.lan"
// after "revenium.invalid" fails. Only a configured search domain may be
// removed: any other trailing labels make a real host such as
// "test.revenium.ai".
func isHarmlessLookup(name string, searchDomains []string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	if isLocal(name) || isReserved(name) {
		return true
	}
	for _, domain := range searchDomains {
		if trimmed, found := strings.CutSuffix(name, "."+domain); found && (isLocal(trimmed) || isReserved(trimmed)) {
			return true
		}
	}
	return false
}

// searchDomains returns the search and domain entries of a resolv.conf.
func searchDomains(resolvConf string) []string {
	var domains []string
	for _, line := range strings.Split(resolvConf, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || (fields[0] != "search" && fields[0] != "domain") {
			continue
		}
		for _, domain := range fields[1:] {
			domains = append(domains, strings.ToLower(strings.TrimSuffix(domain, ".")))
		}
	}
	return domains
}
