package sandboxd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

func TestWaitForSandboxdReadyEnrichesMetadata(t *testing.T) {
	bundlePath, err := os.MkdirTemp("/tmp", "axsd-bundle-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(bundlePath)
	socketPath := filepath.Join(bundlePath, "axern", "sandboxd", "axern-sandboxd.sock")
	shutdown := serveUnixAt(t, socketPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/diagnostics":
			fmt.Fprint(w, `{"protocolVersion":1,"ready":true,"status":{"daemonPid":7,"uptimeSeconds":1,"socketPath":"/mnt/axern-sandboxd.sock","userProcess":{"state":"running"}},"capabilities":["health","status","supervisor"]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer shutdown()

	meta := &apipb.ContainerMetadata{}
	err = WaitReadyForContainer(context.Background(), bundlePath, meta)
	if err != nil {
		t.Fatalf("WaitReadyForContainer() error = %v", err)
	}
	if meta.Labels[LabelReady] != "true" || meta.Labels[LabelSocket] != socketPath {
		t.Fatalf("labels = %#v", meta.Labels)
	}
	if meta.Labels[LabelCapabilities] != "health,status,supervisor" || meta.Labels[LabelUserState] != "running" {
		t.Fatalf("labels = %#v", meta.Labels)
	}
}

func TestWaitForSandboxdReadyFailsClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := WaitReadyForContainer(ctx, t.TempDir(), &apipb.ContainerMetadata{})
	if err == nil {
		t.Fatal("error = nil, want ready failure")
	}
	if !strings.Contains(err.Error(), "sandboxd ready check failed") {
		t.Fatalf("error = %v, want ready check detail", err)
	}
}

func TestWaitForSandboxdReadyRequiresMetadata(t *testing.T) {
	err := WaitReadyForContainer(context.Background(), t.TempDir(), nil)
	if err == nil {
		t.Fatal("error = nil, want metadata error")
	}
}

func serveUnixAt(t *testing.T, socketPath string, handler http.Handler) func() {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() {
		_ = server.Serve(listener)
		close(done)
	}()
	return func() {
		_ = server.Close()
		<-done
		_ = os.Remove(socketPath)
	}
}
