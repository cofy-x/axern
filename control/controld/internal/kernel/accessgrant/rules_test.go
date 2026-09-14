package accessgrantkernel

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
	if IsRevokedOrExpired(&Record{ExpiresAt: now.Add(time.Minute)}, now) {
		t.Fatal("active grant reported revoked or expired")
	}
	if !IsRevokedOrExpired(&Record{ExpiresAt: now.Add(-time.Second)}, now) {
		t.Fatal("expired grant not rejected")
	}
	if !IsRevokedOrExpired(&Record{Revoked: true, ExpiresAt: now.Add(time.Minute)}, now) {
		t.Fatal("revoked grant not rejected")
	}
}
