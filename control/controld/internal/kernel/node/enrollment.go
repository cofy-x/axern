package nodekernel

import (
	"context"
	"time"
)

// EnrollmentLifetime bounds first registration and recovery of a lost reply.
// Replaying the same CSR may recover the committed reply, never mint another
// identity or extend the registration window.
const EnrollmentLifetime = time.Hour

type EnrollmentRequest struct {
	NodeID string
	Token  string
	CSRDER []byte
}

type EnrollmentSigner interface {
	SignNode(csrDER []byte, nodeID string, now time.Time) ([]byte, error)
}

type EnrollmentStore interface {
	Enroll(context.Context, EnrollmentRequest, EnrollmentSigner) ([]byte, error)
	RenewCertificate(context.Context, string, []byte, time.Time, EnrollmentSigner) ([]byte, error)
}
