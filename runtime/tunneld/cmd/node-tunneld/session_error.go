package main

import (
	"errors"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

type sessionStatusError struct {
	status tunnelcontrolv1.TunnelSessionStatus
	err    error
	// reason is the safe diagnostic persisted in the public TunnelSession.
	reason string
}

func (e sessionStatusError) Error() string {
	if e.reason != "" {
		return e.reason
	}
	return e.err.Error()
}

func (e sessionStatusError) Unwrap() error {
	return e.err
}

func statusForSessionError(err error) tunnelcontrolv1.TunnelSessionStatus {
	var sessionErr sessionStatusError
	if errors.As(err, &sessionErr) {
		return sessionErr.status
	}
	return tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED
}

// sessionReportReason is the only error text sent to controld. Causes may
// contain internal relay targets, certificate paths, or runtime diagnostics;
// those belong in the node log, not the public TunnelSession.
func sessionReportReason(err error) string {
	var sessionErr sessionStatusError
	if errors.As(err, &sessionErr) && sessionErr.reason != "" {
		return sessionErr.reason
	}
	if statusForSessionError(err) == tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED {
		return "tunnel data plane temporarily unavailable"
	}
	return "tunnel session setup failed"
}

func degradedSessionError(err error) error {
	if err == nil {
		return nil
	}
	return sessionStatusError{status: tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED, err: err}
}

func failedSessionError(err error, reason string) error {
	if err == nil {
		return nil
	}
	return sessionStatusError{status: tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED, err: err, reason: reason}
}
