package leasekernel

import (
	"testing"
	"time"
)

func TestHashTokenTrimsPlaintext(t *testing.T) {
	if HashToken(" token ") != HashToken("token") {
		t.Fatal("hash should trim token before hashing")
	}
}

func TestRevokedOrExpired(t *testing.T) {
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	active := &Record{ExpiresAt: now.Add(time.Minute)}
	if IsRevokedOrExpired(active, now) {
		t.Fatal("active lease reported revoked or expired")
	}
	expired := &Record{ExpiresAt: now.Add(-time.Second)}
	if !IsRevokedOrExpired(expired, now) {
		t.Fatal("expired lease not rejected")
	}
	revoked := &Record{Revoked: true, ExpiresAt: now.Add(time.Minute)}
	if !IsRevokedOrExpired(revoked, now) {
		t.Fatal("revoked lease not rejected")
	}
}
