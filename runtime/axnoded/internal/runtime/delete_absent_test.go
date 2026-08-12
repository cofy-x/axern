package runtime

import (
	"errors"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
)

func TestRuntimeContainerAbsentUsesRuntimeOutput(t *testing.T) {
	missing := &ocicli.CommandError{Err: errors.New("exit status 1"), Output: "container sandbox does not exist"}
	if !runtimeContainerAbsent(missing, "sandbox") {
		t.Fatal("expected missing container output to identify the absent runtime container")
	}

	unrelated := &ocicli.CommandError{Err: errors.New("exit status 1"), Output: "rootfs path not found"}
	if runtimeContainerAbsent(unrelated, "sandbox") {
		t.Fatal("unrelated not-found output must not authorize storage cleanup")
	}
}
