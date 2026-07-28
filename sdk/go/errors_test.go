package axernsdk

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestErrorHelpers(t *testing.T) {
	notFound := mapRPCError(status.Error(codes.NotFound, "missing"), "op", "alloc-1")
	if !IsNotFound(notFound) {
		t.Fatalf("IsNotFound = false for %v", notFound)
	}
	if IsPermissionDenied(notFound) || IsTimeout(notFound) || IsAlreadyExists(notFound) {
		t.Fatalf("unexpected helper match for %v", notFound)
	}
	if !IsPermissionDenied(status.Error(codes.Unauthenticated, "auth")) {
		t.Fatal("IsPermissionDenied = false for unauthenticated")
	}
	if !IsTimeout(status.Error(codes.DeadlineExceeded, "deadline")) {
		t.Fatal("IsTimeout = false for deadline exceeded")
	}
	if !IsTimeout(context.DeadlineExceeded) {
		t.Fatal("IsTimeout = false for context deadline exceeded")
	}
	if !IsUnavailable(status.Error(codes.Unavailable, "unavailable")) {
		t.Fatal("IsUnavailable = false for unavailable")
	}
	if !IsValidation(validationError("field", "bad")) {
		t.Fatal("IsValidation = false for ValidationError")
	}
	if !IsValidation(&PathError{Message: "path is required"}) {
		t.Fatal("IsValidation = false for PathError")
	}
	if !IsValidation(ErrInvalidSource) {
		t.Fatal("IsValidation = false for ErrInvalidSource")
	}
}

func TestSandboxCapabilityInfo(t *testing.T) {
	err := mapRPCError(
		status.Error(
			codes.FailedPrecondition,
			"sandboxd browser status failed: sandboxd /browser/status returned status 503 "+
				"(unavailable): browser crashed; sandboxd user process state=running; "+
				"providers 1/1 available; browser provider degraded: browser crashed; "+
				"missing dependencies: chromium (not found)",
		),
		"sandbox browser status",
		"alloc-1",
	)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error is not RPCError: %v", err)
	}
	want := &SandboxCapabilityErrorInfo{
		Capability:          "browser",
		Provider:            "browser",
		ProviderState:       "degraded",
		Reason:              "browser crashed",
		MissingDependencies: []string{"chromium (not found)"},
	}
	if !reflect.DeepEqual(rpcErr.Capability, want) {
		t.Fatalf("Capability = %#v, want %#v", rpcErr.Capability, want)
	}
	if got := SandboxCapabilityInfo("plain failure"); got != nil {
		t.Fatalf("SandboxCapabilityInfo = %#v, want nil", got)
	}
}

func TestExecErrorAccessors(t *testing.T) {
	err := &ExecError{
		Argv: []string{"false"},
		Result: ExecResult{
			ExitCode: 7,
			Stdout:   []byte("out"),
			Stderr:   []byte("err"),
		},
	}
	if !errors.As(err, new(*ExecError)) {
		t.Fatal("ExecError does not support errors.As")
	}
	if err.ExitCode() != 7 || err.StdoutString() != "out" || err.StderrString() != "err" {
		t.Fatalf("unexpected accessors exit=%d stdout=%q stderr=%q", err.ExitCode(), err.StdoutString(), err.StderrString())
	}
}
