package app

import (
	"os"
	"testing"
)

func TestListenUnixAppliesExplicitPrincipalBoundary(t *testing.T) {
	placeholder, err := os.CreateTemp("", "axnoded-sock-")
	if err != nil {
		t.Fatalf("create short socket path: %v", err)
	}
	socketPath := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		t.Fatalf("close socket placeholder: %v", err)
	}
	if err := os.Remove(socketPath); err != nil {
		t.Fatalf("remove socket placeholder: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	listener, err := listenUnix(socketPath, 0o600)
	if err != nil {
		t.Fatalf("listenUnix() error = %v", err)
	}
	defer listener.Close()
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat operator socket: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("operator socket mode = %04o, want 0600", got)
	}
}
