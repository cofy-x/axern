package leasekernel

import "time"

// Record is the durable node-facing authorization fact. Plaintext client
// credentials and routing data deliberately do not belong here.
type Record struct {
	LeaseID             string
	AllocationID        string
	NodeID              string
	ValidationTokenHash string
	ExpiresAt           time.Time
	Revision            int64
	Revoked             bool
}

// IssuedGrant contains the one-time plaintext credential returned to gatewayd.
type IssuedGrant struct {
	Record
	PlaintextToken string
}

func IsExpired(record *Record, now time.Time) bool {
	return record == nil || !record.ExpiresAt.After(now)
}

func IsRevokedOrExpired(record *Record, now time.Time) bool {
	return record == nil || record.Revoked || IsExpired(record, now)
}
