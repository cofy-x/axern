package sandboxd

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestListenUnixCreatesPrivateSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "axsd-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	socketPath := filepath.Join(dir, "sandboxd.sock")
	listener, err := listenUnix(socketPath)
	if err != nil {
		t.Fatalf("listenUnix() error = %v", err)
	}
	defer listener.Close()

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("socket mode = %v, want 0600", info.Mode().Perm())
	}
	if _, ok := listener.(*net.UnixListener); !ok {
		t.Fatalf("listener type = %T, want *net.UnixListener", listener)
	}
}
