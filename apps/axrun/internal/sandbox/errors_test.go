package sandbox

import (
	"errors"
	"testing"

	axernsdk "github.com/cofy-x/axern/sdk/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsFatalSandboxError(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		fatal bool
	}{
		{
			name:  "sandbox death",
			err:   &SandboxDeathError{Reason: "allocation not found", Cause: errors.New("gone")},
			fatal: true,
		},
		{
			name:  "sandbox not started",
			err:   axernsdk.ErrSandboxNotStarted,
			fatal: true,
		},
		{
			name:  "grpc not found",
			err:   status.Error(codes.NotFound, "allocation missing"),
			fatal: true,
		},
		{
			name:  "grpc unavailable",
			err:   status.Error(codes.Unavailable, "node unavailable"),
			fatal: true,
		},
		{
			name:  "grpc invalid argument",
			err:   status.Error(codes.InvalidArgument, "bad request"),
			fatal: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFatalSandboxError(tc.err); got != tc.fatal {
				t.Fatalf("IsFatalSandboxError() = %t, want %t", got, tc.fatal)
			}
		})
	}
}

func TestClassifyFatalReason(t *testing.T) {
	if reason := ClassifyFatalReason(&SandboxDeathError{Reason: "allocation not found", Cause: errors.New("gone")}); reason != "allocation not found" {
		t.Fatalf("reason = %q", reason)
	}
	if reason := ClassifyFatalReason(axernsdk.ErrSandboxNotStarted); reason != "sandbox not started" {
		t.Fatalf("reason = %q", reason)
	}
	if reason := ClassifyFatalReason(status.Error(codes.NotFound, "allocation missing")); reason != "allocation not found" {
		t.Fatalf("reason = %q", reason)
	}
	if reason := ClassifyFatalReason(status.Error(codes.Unavailable, "node down")); reason != "node unavailable" {
		t.Fatalf("reason = %q", reason)
	}
	if reason := ClassifyFatalReason(status.Error(codes.PermissionDenied, "forbidden")); reason != "" {
		t.Fatalf("reason = %q", reason)
	}
}
