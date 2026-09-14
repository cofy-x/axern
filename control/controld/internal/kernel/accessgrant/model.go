package accessgrantkernel

import "time"

type Record struct {
	GrantID             string
	AllocationID        string
	NodeID              string
	ValidationTokenHash string
	ExpiresAt           time.Time
	Revision            int64
	Revoked             bool
}

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
