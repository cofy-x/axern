package terminal

import "context"

type credentialIdentity struct{ fingerprint, kind string }
type credentialContextKey struct{}

// WithCredential is populated by the gateway's protocol authenticator, never
// from caller-supplied routing or internal gRPC metadata.
func WithCredential(ctx context.Context, fingerprint, kind string) context.Context {
	return context.WithValue(ctx, credentialContextKey{}, credentialIdentity{fingerprint: fingerprint, kind: kind})
}
