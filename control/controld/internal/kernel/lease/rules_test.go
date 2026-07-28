package leasekernel

import (
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestHashTokenTrimsPlaintext(t *testing.T) {
	if HashToken(" token ") != HashToken("token") {
		t.Fatal("hash should trim token before hashing")
	}
}

func TestRevokedOrExpired(t *testing.T) {
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	active := &commonv1.ExecutionLease{ExpiresAt: timestamppb.New(now.Add(time.Minute))}
	if IsRevokedOrExpired(active, now) {
		t.Fatal("active lease reported revoked or expired")
	}
	expired := &commonv1.ExecutionLease{ExpiresAt: timestamppb.New(now.Add(-time.Second))}
	if !IsRevokedOrExpired(expired, now) {
		t.Fatal("expired lease not rejected")
	}
	revoked := &commonv1.ExecutionLease{Revoked: true, ExpiresAt: timestamppb.New(now.Add(time.Minute))}
	if !IsRevokedOrExpired(revoked, now) {
		t.Fatal("revoked lease not rejected")
	}
}
