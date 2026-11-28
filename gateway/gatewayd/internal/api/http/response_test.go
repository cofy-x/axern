package httpapi

import (
	"net/http"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTerminalHTTPStatusPreservesTypedAccessRejection(t *testing.T) {
	for code, want := range map[codes.Code]int{
		codes.FailedPrecondition: http.StatusGone,
		codes.NotFound:           http.StatusNotFound,
		codes.Unauthenticated:    http.StatusUnauthorized,
		codes.PermissionDenied:   http.StatusForbidden,
		codes.Unavailable:        http.StatusServiceUnavailable,
		codes.DeadlineExceeded:   http.StatusGatewayTimeout,
	} {
		if got := statusFromError(status.Error(code, "synthetic message")); got != want {
			t.Errorf("%s returned HTTP %d, want %d", code, got, want)
		}
	}
}
