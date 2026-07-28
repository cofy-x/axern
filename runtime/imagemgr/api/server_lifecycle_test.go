package api

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
)

func TestServeHTTPStopsAndRemovesSocketOnCancellation(t *testing.T) {
	worker := mustNewHttpWorker(t, newMockManager())
	tempDir, err := os.MkdirTemp("/tmp", "imagemgr-api-")
	if err != nil {
		t.Fatalf("create short socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	socketPath := filepath.Join(tempDir, "imagemgr.sock")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- worker.ServeHTTP(ctx, socketPath)
	}()

	waitForUnixSocket(t, socketPath)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeHTTP() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ServeHTTP did not stop after context cancellation")
	}
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket path remains after shutdown: %v", err)
	}
}

func TestServeHTTPRefusesToReplaceRegularFile(t *testing.T) {
	worker := mustNewHttpWorker(t, newMockManager())
	socketPath := filepath.Join(t.TempDir(), "imagemgr.sock")
	if err := os.WriteFile(socketPath, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	err := worker.ServeHTTP(t.Context(), socketPath)
	if err == nil || !strings.Contains(err.Error(), "refusing to replace non-socket") {
		t.Fatalf("ServeHTTP() error = %v, want non-socket refusal", err)
	}
	data, readErr := os.ReadFile(socketPath)
	if readErr != nil || string(data) != "keep" {
		t.Fatalf("sentinel changed: data=%q err=%v", data, readErr)
	}
}

func TestServeHTTPWaitsForActiveRequestsDuringShutdown(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	mgr := newMockManager()
	mgr.listDaemonsFunc = func() []imagefsd.DaemonInfo {
		close(requestStarted)
		<-releaseRequest
		return nil
	}
	worker := mustNewHttpWorker(t, mgr)
	tempDir, err := os.MkdirTemp("/tmp", "imagemgr-api-")
	if err != nil {
		t.Fatalf("create short socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	socketPath := filepath.Join(tempDir, "imagemgr.sock")
	ctx, cancel := context.WithCancel(t.Context())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- worker.ServeHTTP(ctx, socketPath)
	}()
	waitForUnixSocket(t, socketPath)

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}}
	requestDone := make(chan error, 1)
	go func() {
		response, err := client.Get("http://imagemgr/list_daemons")
		if err == nil {
			_ = response.Body.Close()
		}
		requestDone <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not reach handler")
	}
	cancel()
	select {
	case err := <-serveDone:
		t.Fatalf("ServeHTTP returned while request was active: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseRequest)
	if err := <-requestDone; err != nil {
		t.Fatalf("active request failed during graceful shutdown: %v", err)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("ServeHTTP() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ServeHTTP did not return after active request completed")
	}
}

func waitForUnixSocket(t *testing.T, socketPath string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			_ = conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("unix socket did not become ready: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
