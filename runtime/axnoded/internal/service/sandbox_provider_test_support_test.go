package service

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	"github.com/stretchr/testify/require"
)

func storeRunningBrowserContainer(t *testing.T, s *sandboxService, id string, socketPath string, capabilities string) {
	t.Helper()
	s.containerManager.StoreMetadata(id, &apipb.ContainerMetadata{
		ID:             id,
		RuntimeHandler: "runsc",
		Labels: map[string]string{
			runtimesandboxd.LabelReady:        "true",
			runtimesandboxd.LabelSocket:       socketPath,
			runtimesandboxd.LabelCapabilities: capabilities,
		},
	})
	time.Sleep(200 * time.Millisecond)
}

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

func startBrowserTestServer(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "axbrowser-")
	require.NoError(t, err)
	socketPath := filepath.Join(dir, "s.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.RemoveAll(dir)
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/browser/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, `{"available":true,"command":"chromium","running":false}`)
	})
	mux.HandleFunc("/browser/open", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, `{"available":true,"command":"chromium","running":true,"pid":88,"url":"data:text/html,open"}`)
	})
	mux.HandleFunc("/browser/close", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, `{"available":true,"command":"chromium","running":false}`)
	})
	mux.HandleFunc("/browser/navigate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, `{"available":true,"command":"chromium","running":true,"pid":88,"url":"data:text/html,navigate"}`)
	})
	for _, path := range []string{"/browser/resize", "/browser/click", "/browser/type", "/browser/wait"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			fmt.Fprint(w, `{"available":true,"command":"chromium","running":true,"pid":88}`)
		})
	}
	mux.HandleFunc("/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, `{"protocolVersion":1,"ready":true,"status":{"daemonPid":7,"uptimeSeconds":1.5,"socketPath":"`+socketPath+`","userProcess":{"state":"running","pid":11}},"capabilities":["browser","file","process"],"providers":[{"name":"browser","state":"available","available":true,"capabilities":["browser"]}],"browser":{"available":true,"command":"chromium","running":true,"pid":88,"url":"data:text/html,open"}}`)
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
			t.Fatalf("browser test server: %v", err)
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
