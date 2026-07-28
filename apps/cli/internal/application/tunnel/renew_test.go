package tunnel

import (
	"testing"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRenewInterval(t *testing.T) {
	tests := []struct {
		name string
		ttl  time.Duration
		want time.Duration
	}{
		{name: "default", ttl: 0, want: 15 * time.Minute},
		{name: "half ttl", ttl: 10 * time.Minute, want: 5 * time.Minute},
		{name: "minimum interval", ttl: time.Minute, want: 30 * time.Second},
		{name: "floor interval", ttl: 20 * time.Second, want: 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renewInterval(tt.ttl); got != tt.want {
				t.Fatalf("renewInterval(%s) = %s, want %s", tt.ttl, got, tt.want)
			}
		})
	}
}

func TestSessionLeaseTTLUsesServerNormalizedLease(t *testing.T) {
	createdAt := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	session := &tunnelcontrolv1.TunnelSession{
		CreatedAt: timestamppb.New(createdAt),
		ExpiresAt: timestamppb.New(createdAt.Add(time.Hour)),
	}
	if got := sessionLeaseTTL(session, 100*time.Hour); got != time.Hour {
		t.Fatalf("sessionLeaseTTL() = %s, want 1h", got)
	}
}

func TestSessionLeaseTTLFallsBack(t *testing.T) {
	if got := sessionLeaseTTL(nil, 5*time.Minute); got != 5*time.Minute {
		t.Fatalf("sessionLeaseTTL(nil) = %s, want 5m", got)
	}
	if got := sessionLeaseTTL(nil, 0); got != defaultRenewTTL {
		t.Fatalf("sessionLeaseTTL(nil, 0) = %s, want %s", got, defaultRenewTTL)
	}
}
