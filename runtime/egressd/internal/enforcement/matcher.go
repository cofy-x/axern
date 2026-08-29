package enforcement

import (
	"net/netip"
	"strings"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func domainMatches(rule, name string) bool {
	rule = strings.TrimSuffix(strings.ToLower(rule), ".")
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	if strings.HasPrefix(rule, "*.") {
		suffix := strings.TrimPrefix(rule, "*.")
		return name != suffix && strings.HasSuffix(name, "."+suffix)
	}
	return name == rule
}

func domainAllowed(policy *commonv1.NetworkEgressPolicy, name string) bool {
	strict := policy.GetStrict()
	if strict == nil {
		return false
	}
	for _, rule := range strict.GetAllowedDomains() {
		if domainMatches(rule, name) {
			return true
		}
	}
	return false
}

func dnsDenied(policy *commonv1.NetworkEgressPolicy, name string) bool {
	if deny := policy.GetDnsDeny(); deny != nil {
		for _, rule := range deny.GetDeniedDomains() {
			if domainMatches(rule, name) {
				return true
			}
		}
		return false
	}
	return !domainAllowed(policy, name)
}

type authorization struct {
	domain string
	expiry time.Time
}

func (a authorization) valid(name string, now time.Time) bool {
	return now.Before(a.expiry) && domainMatches(a.domain, name)
}

func normalizedSource(value string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return addr.Unmap().String()
}
