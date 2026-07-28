package pgtunnel

import "time"

func normalizeTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultSessionTTL
	}
	if ttl < minSessionTTL {
		return minSessionTTL
	}
	if ttl > maxSessionTTL {
		return maxSessionTTL
	}
	return ttl
}
