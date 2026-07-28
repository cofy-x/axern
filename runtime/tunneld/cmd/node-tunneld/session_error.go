package main

import (
	"errors"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

type sessionStatusError struct {
	status tunnelcontrolv1.TunnelSessionStatus
	err    error
}

func (e sessionStatusError) Error() string {
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

func degradedSessionError(err error) error {
	if err == nil {
		return nil
	}
	return sessionStatusError{status: tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED, err: err}
}
