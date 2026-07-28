package errord

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCRoundTrip(t *testing.T) {
	errShouldLeaveAlone := errors.New("unknown to package")

	for _, testcase := range []struct {
		input error
		cause error
		str   string
	}{
		{
			input: ErrAlreadyExists,
			cause: ErrAlreadyExists,
		},
		{
			input: ErrNotFound,
			cause: ErrNotFound,
		},
		//nolint:dupword
		{
			input: fmt.Errorf("test test test: %w", ErrFailedPrecondition),
			cause: ErrFailedPrecondition,
			str:   "test test test: failed precondition",
		},
		{
			input: status.Errorf(codes.Unavailable, "should be not available"),
			cause: ErrUnavailable,
			str:   "should be not available: unavailable",
		},
		{
			input: fmt.Errorf("interface pool: %w", ErrResourceExhausted),
			cause: ErrResourceExhausted,
			str:   "interface pool: resource exhausted",
		},
		{
			input: errShouldLeaveAlone,
			cause: ErrUnknown,
			str:   errShouldLeaveAlone.Error() + ": " + ErrUnknown.Error(),
		},
		{
			input: context.Canceled,
			cause: context.Canceled,
			str:   "context canceled",
		},
		{
			input: fmt.Errorf("this is a test cancel: %w", context.Canceled),
			cause: context.Canceled,
			str:   "this is a test cancel: context canceled",
		},
		{
			input: context.DeadlineExceeded,
			cause: context.DeadlineExceeded,
			str:   "context deadline exceeded",
		},
		{
			input: fmt.Errorf("this is a test deadline exceeded: %w", context.DeadlineExceeded),
			cause: context.DeadlineExceeded,
			str:   "this is a test deadline exceeded: context deadline exceeded",
		},
	} {
		t.Run(testcase.input.Error(), func(t *testing.T) {
			t.Logf("input: %v", testcase.input)
			gerr := ToGRPC(testcase.input)
			t.Logf("grpc: %v", gerr)
			ferr := FromGRPC(gerr)
			t.Logf("recovered: %v", ferr)

			if !errors.Is(ferr, testcase.cause) {
				t.Fatalf("unexpected cause: !errors.Is(%v, %v)", ferr, testcase.cause)
			}

			expected := testcase.str
			if expected == "" {
				expected = testcase.cause.Error()
			}
			if ferr.Error() != expected {
				t.Fatalf("unexpected string: %q != %q", ferr.Error(), expected)
			}
		})
	}

}
