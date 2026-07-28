package bpfnet

import "errors"

type attachErrorKind int

const (
	attachErrorUnknown attachErrorKind = iota
	attachErrorTCProbe
	attachErrorReconcile
)

type dataplaneAttachError struct {
	kind attachErrorKind
	err  error
}

func (e *dataplaneAttachError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *dataplaneAttachError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func markTCProbeError(err error) error {
	if err == nil {
		return nil
	}
	return &dataplaneAttachError{kind: attachErrorTCProbe, err: err}
}

func splitAttachError(err error) (tcProbeErr string, reconcileErr string) {
	var typed *dataplaneAttachError
	if errors.As(err, &typed) {
		switch typed.kind {
		case attachErrorTCProbe:
			return typed.Error(), ""
		case attachErrorReconcile:
			return "", typed.Error()
		}
	}
	if err != nil {
		reconcileErr = err.Error()
	}
	return tcProbeErr, reconcileErr
}
