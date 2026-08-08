package runtime

import (
	"errors"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
)

func TestRuntimeDeleteTargetAbsentUsesRuntimeOutput(t *testing.T) {
	missing := &ocicli.CommandError{Err: errors.New("exit status 1"), Output: "container sandbox does not exist"}
	if !runtimeDeleteTargetAbsent(missing, "sandbox") {
		t.Fatal("expected missing container output to be treated as an idempotent delete")
	}

	unrelated := &ocicli.CommandError{Err: errors.New("exit status 1"), Output: "rootfs path not found"}
	if runtimeDeleteTargetAbsent(unrelated, "sandbox") {
		t.Fatal("unrelated not-found output must not authorize storage cleanup")
	}
}
