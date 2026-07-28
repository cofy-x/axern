package output

import commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"

type ExecutionLeaseJSON struct {
	LeaseID        string `json:"lease_id"`
	AllocationID   string `json:"allocation_id,omitempty"`
	NodeID         string `json:"node_id,omitempty"`
	Attempt        int64  `json:"attempt,omitempty"`
	LeaseType      string `json:"lease_type"`
	PlaintextToken string `json:"plaintext_token,omitempty"`
	Revision       int64  `json:"revision,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	Revoked        bool   `json:"revoked,omitempty"`
}

func NewExecutionLeaseJSON(lease *commonv1.ExecutionLease) *ExecutionLeaseJSON {
	if lease == nil {
		return nil
	}
	return &ExecutionLeaseJSON{
		LeaseID:        lease.GetLeaseID(),
		AllocationID:   lease.GetAllocationID(),
		NodeID:         lease.GetNodeID(),
		Attempt:        lease.GetAttempt(),
		LeaseType:      leaseTypeLabel(lease.GetLeaseType()),
		PlaintextToken: lease.GetPlaintextToken(),
		Revision:       lease.GetRevision(),
		ExpiresAt:      FormatProtoTimestamp(lease.GetExpiresAt()),
		Revoked:        lease.GetRevoked(),
	}
}

func leaseTypeLabel(leaseType commonv1.LeaseType) string {
	return trimEnumPrefix(leaseType.String(), "LEASE_TYPE_")
}
