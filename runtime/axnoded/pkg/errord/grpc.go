package errord

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ToGRPC maps an axnoded error class into a grpc error, using the original
// error message as a description.
//
// Further information may be extracted from certain errors depending on their
// type.
//
// If the error is unmapped, the original error will be returned to be handled
// by the regular grpc error handling stack.
func ToGRPC(err error) error {
	if err == nil {
		return nil
	}

	if isGRPCError(err) {
		// error has already been mapped to grpc
		return err
	}

	switch {
	case IsInvalidArgument(err):
		return status.Errorf(codes.InvalidArgument, "%s", err.Error())
	case IsNotFound(err):
		return status.Errorf(codes.NotFound, "%s", err.Error())
	case IsAlreadyExists(err):
		return status.Errorf(codes.AlreadyExists, "%s", err.Error())
	case IsFailedPrecondition(err):
		return status.Errorf(codes.FailedPrecondition, "%s", err.Error())
	case IsUnavailable(err):
		return status.Errorf(codes.Unavailable, "%s", err.Error())
	case IsResourceExhausted(err):
		return status.Errorf(codes.ResourceExhausted, "%s", err.Error())
	case IsNotImplemented(err):
		return status.Errorf(codes.Unimplemented, "%s", err.Error())
	case IsCanceled(err):
		return status.Errorf(codes.Canceled, "%s", err.Error())
	case IsDeadlineExceeded(err):
		return status.Errorf(codes.DeadlineExceeded, "%s", err.Error())
	}

	return err
}

// ToGRPCf maps the error to grpc error codes, assembling the formatting string
// and combining it with the target error string.
//
// This is equivalent to ToGRPC(fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)).
func ToGRPCf(err error, format string, args ...interface{}) error {
	return ToGRPC(fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err))
}

// FromGRPC returns the underlying error from a grpc service based on the grpc error code
func FromGRPC(err error) error {
	if err == nil {
		return nil
	}

	var cls error // divide these into error classes, becomes the cause

	switch code(err) {
	case codes.InvalidArgument:
		cls = ErrInvalidArgument
	case codes.AlreadyExists:
		cls = ErrAlreadyExists
	case codes.NotFound:
		cls = ErrNotFound
	case codes.Unavailable:
		cls = ErrUnavailable
	case codes.ResourceExhausted:
		cls = ErrResourceExhausted
	case codes.FailedPrecondition:
		cls = ErrFailedPrecondition
	case codes.Unimplemented:
		cls = ErrNotImplemented
	case codes.Canceled:
		cls = context.Canceled
	case codes.DeadlineExceeded:
		cls = context.DeadlineExceeded
	default:
		cls = ErrUnknown
	}

	msg := rebaseMessage(cls, err)
	if msg != "" {
		err = fmt.Errorf("%s: %w", msg, cls)
	} else {
		err = cls
	}

	return err
}

// rebaseMessage removes the repeats for an error at the end of an error
// string. This will happen when taking an error over grpc then remapping it.
//
// Effectively, we just remove the string of cls from the end of err if it
// appears there.
func rebaseMessage(cls error, err error) string {
	desc := errDesc(err)
	clss := cls.Error()
	if desc == clss {
		return ""
	}

	return strings.TrimSuffix(desc, ": "+clss)
}

func isGRPCError(err error) bool {
	_, ok := status.FromError(err)
	return ok
}

func code(err error) codes.Code {
	if s, ok := status.FromError(err); ok {
		return s.Code()
	}
	return codes.Unknown
}

func errDesc(err error) string {
	if s, ok := status.FromError(err); ok {
		return s.Message()
	}
	return err.Error()
}
