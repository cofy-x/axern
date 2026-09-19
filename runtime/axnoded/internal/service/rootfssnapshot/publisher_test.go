package rootfssnapshot

import (
	"errors"
	"net/http"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func TestResponseErrorCausePreservesSnapshotFailureClass(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{status: http.StatusBadRequest, want: errord.ErrInvalidArgument},
		{status: http.StatusRequestEntityTooLarge, want: errord.ErrResourceExhausted},
		{status: http.StatusServiceUnavailable, want: errord.ErrUnavailable},
		{status: http.StatusInternalServerError},
	}
	for _, test := range tests {
		if got := responseErrorCause(test.status); !errors.Is(got, test.want) || (got == nil) != (test.want == nil) {
			t.Fatalf("responseErrorCause(%d) = %v, want %v", test.status, got, test.want)
		}
	}
}
