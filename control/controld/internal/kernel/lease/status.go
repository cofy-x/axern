package leasekernel

import (
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func IsExpired(l *commonv1.ExecutionLease, now time.Time) bool {
	if l == nil || l.GetExpiresAt() == nil {
		return true
	}
	return l.GetExpiresAt().AsTime().Before(now)
}

func IsRevokedOrExpired(l *commonv1.ExecutionLease, now time.Time) bool {
	return l == nil || l.GetRevoked() || IsExpired(l, now)
}
