package tunnelkernel

import (
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const SetupErrorDomain = "axern.control.tunnel"

// SetupErrorReason is a stable public reason for a failed tunnel setup RPC.
type SetupErrorReason string

const (
	SetupControlUnavailable SetupErrorReason = "TUNNEL_CONTROL_UNAVAILABLE"
	SetupAllocationInactive SetupErrorReason = "ALLOCATION_INACTIVE"
	SetupRelayUnavailable   SetupErrorReason = "TUNNEL_RELAY_UNAVAILABLE"
	SetupSessionFailed      SetupErrorReason = "TUNNEL_SESSION_FAILED"
	SetupSessionRevoked     SetupErrorReason = "TUNNEL_SESSION_REVOKED"
	SetupSessionExpired     SetupErrorReason = "TUNNEL_SESSION_EXPIRED"
	SetupReadyTimeout       SetupErrorReason = "TUNNEL_READY_TIMEOUT"
)

// SetupError preserves the gRPC status category and attaches a machine-readable
// reason. Setup errors deliberately carry no token, payload, or session metadata.
func SetupError(code codes.Code, reason SetupErrorReason, message string) error {
	st := grpcstatus.New(code, message)
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason: string(reason),
		Domain: SetupErrorDomain,
	})
	if err != nil {
		return st.Err()
	}
	return withDetails.Err()
}
