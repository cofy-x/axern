package service

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func startProviderOperationErrorTestServer(t *testing.T, capability string, operationPath string, reason string) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "axprovider-op-")
	require.NoError(t, err)
	socketPath := filepath.Join(dir, "s.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc(operationPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"error":{"code":"unavailable","message":%q}}`, reason)
	})
	mux.HandleFunc("/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprintf(w, `{"protocolVersion":1,"ready":true,"status":{"daemonPid":7,"uptimeSeconds":1.5,"socketPath":%q,"userProcess":{"state":"running","pid":11}},"capabilities":["%s","file","process"],"providerSummary":{"total":1,"available":1,"degraded":1,"unavailable":0},"providers":[{"name":%q,"state":"degraded","available":true,"reason":%q,"capabilities":[%q]}]}`, socketPath, capability, capability, reason, capability)
	})
	server := &http.Server{Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	return socketPath, func() {
		_ = server.Close()
		_ = os.RemoveAll(dir)
		if err := <-errCh; err != nil {
			t.Fatalf("provider operation error test server: %v", err)
		}
	}
}

func startUnavailableProviderTestServer(t *testing.T, capability string, reason string) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "axprovider-")
	require.NoError(t, err)
	socketPath := filepath.Join(dir, "s.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprintf(w, `{"protocolVersion":1,"ready":true,"status":{"daemonPid":7,"uptimeSeconds":1.5,"socketPath":%q,"userProcess":{"state":"running","pid":11}},"capabilities":["file","process"],"providers":[{"name":%q,"state":"unavailable","available":false,"reason":%q,"capabilities":[%q],"dependencies":[{"name":"provider_dependency","available":false,"reason":%q}]}]}`, socketPath, capability, reason, capability, reason)
	})
	server := &http.Server{Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	return socketPath, func() {
		_ = server.Close()
		_ = os.RemoveAll(dir)
		if err := <-errCh; err != nil {
			t.Fatalf("provider test server: %v", err)
		}
	}
}
