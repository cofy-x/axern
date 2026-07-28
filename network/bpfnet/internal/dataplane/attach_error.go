package dataplane

import "errors"

type attachErrorKind int

const (
	attachErrorUnknown attachErrorKind = iota
	attachErrorTCProbe
	attachErrorReconcile
)

type attachError struct {
	kind attachErrorKind
	err  error
}

func (e *attachError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *attachError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func markTCProbeError(err error) error {
	if err == nil {
		return nil
	}
	return &attachError{kind: attachErrorTCProbe, err: err}
}

func markReconcileError(err error) error {
	if err == nil {
		return nil
	}
	return &attachError{kind: attachErrorReconcile, err: err}
}

func SplitAttachError(err error) (tcProbeErr string, reconcileErr string) {
	var typed *attachError
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
