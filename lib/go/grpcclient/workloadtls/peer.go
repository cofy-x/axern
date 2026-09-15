package workloadtls

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// PeerIdentity checks authority at operation time, not merely handshake time.
// Stream owners must end authorization no later than the returned deadline.
func PeerIdentity(ctx context.Context, cluster string, now time.Time) (Identity, time.Time, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return Identity{}, time.Time{}, fmt.Errorf("verified workload peer required")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.VerifiedChains[0]) == 0 {
		return Identity{}, time.Time{}, fmt.Errorf("verified workload peer required")
	}
	chain := info.State.VerifiedChains[0]
	deadline := chain[0].NotAfter
	for _, cert := range chain {
		if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
			return Identity{}, time.Time{}, fmt.Errorf("workload certificate authority has expired")
		}
		if cert.NotAfter.Before(deadline) {
			deadline = cert.NotAfter
		}
	}
	identity, err := FromCertificate(chain[0], cluster)
	return identity, deadline, err
}
