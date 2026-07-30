package auth

import (
	"context"
	"crypto/sha256"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func CertificateFingerprint(ctx context.Context) (string, error) {
	peerInfo, ok := peer.FromContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "verified client certificate is required")
	}
	tlsInfo, ok := peerInfo.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return "", status.Error(codes.Unauthenticated, "verified client certificate is required")
	}
	fingerprint := sha256.Sum256(tlsInfo.State.VerifiedChains[0][0].Raw)
	return fmt.Sprintf("%x", fingerprint[:]), nil
}
