package leasekernel

import commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"

func ParseType(value string) commonv1.LeaseType {
	if n, ok := commonv1.LeaseType_value[value]; ok {
		return commonv1.LeaseType(n)
	}
	return commonv1.LeaseType_LEASE_TYPE_UNSPECIFIED
}
